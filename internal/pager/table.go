package pager

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	extast "github.com/yuin/goldmark/extension/ast"
)

const tableCellWidth = 40
const tableLinkPrefix = "readmd-table-"

// Only top-level tables use this adapter. Containers remain whole stock blocks.
func renderTable(node *extast.Table, source []byte, options glamansi.Options, id int, cellWidths ...int) (string, error) {
	return renderThemedTable(node, source, options, id, nil, config.TextStyle{}, cellWidths...)
}

func renderThemedTable(node *extast.Table, source []byte, options glamansi.Options, id int, composer *themeComposer, borderStyle config.TextStyle, cellWidths ...int) (string, error) {
	cellWidth := tableCellWidth
	if len(cellWidths) > 0 {
		cellWidth = cellWidths[0]
	}
	options.InlineTableLinks = true
	r := glamansi.NewRenderer(options)
	ctx := glamansi.NewRenderContext(options)
	base := &glamansi.BlockElement{Block: &bytes.Buffer{}, Style: options.Styles.Document}
	if err := base.Render(io.Discard, ctx); err != nil {
		return "", err
	}
	var rows [][]string
	widths := make([]int, len(node.Alignments))
	serial := 0
	for row := node.FirstChild(); row != nil; row = row.NextSibling() {
		cells := make([]string, len(widths))
		col := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			var b bytes.Buffer
			for child := cell.FirstChild(); child != nil; child = child.NextSibling() {
				e := tableInline(r.NewElement(child, source).Renderer, id, &serial, options.Styles.ImageText)
				e = composer.inline(e)
				if styled, ok := e.(glamansi.StyleOverriderElementRenderer); ok {
					if err := styled.StyleOverrideRender(&b, ctx, options.Styles.Table.StylePrimitive); err != nil {
						return "", err
					}
				} else if e != nil {
					if err := e.Render(&b, ctx); err != nil {
						return "", err
					}
				}
			}
			cellText := b.String()
			if composer != nil {
				var err error
				cellText, err = composer.compose(cellText)
				if err != nil {
					return "", err
				}
			}
			cells[col] = expandTableTabs(cellText)
			// Empty destinations are inert resets, placed outside complete cells.
			// ANSI cuts replay these bounds on every wrapped continuation.
			if bytes.Contains(cell.Lines().Value(source), []byte("[^")) {
				cells[col] = ansi.SetHyperlink("", "id="+tableCellID(id, len(rows), col)) + cells[col] + ansi.SetHyperlink("", "id="+tableCellEnd)
			}
			for _, line := range strings.Split(cells[col], "\n") {
				preferred := ansi.StringWidth(line)
				if row.Kind() != extast.KindTableHeader {
					preferred = min(preferred, cellWidth)
				}
				widths[col] = max(widths[col], preferred)
				for _, word := range tableWords(line) {
					widths[col] = max(widths[col], word.end-word.start)
				}
			}
			col++
		}
		rows = append(rows, cells)
	}
	multiline := false
	for _, row := range rows[1:] {
		for col, cell := range row {
			row[col] = wrapTableCell(cell, widths[col])
			multiline = multiline || strings.Contains(row[col], "\n")
		}
	}
	border := lipgloss.RoundedBorder()
	rules := options.Styles.Table
	if rules.RowSeparator != nil && *rules.RowSeparator == "-" && rules.ColumnSeparator != nil && *rules.ColumnSeparator == "|" {
		border = lipgloss.ASCIIBorder()
	}
	if sgr := textSGR(borderStyle, composer != nil && composer.notty); sgr != "" {
		border = styleTableBorder(border, sgr)
	}
	t := table.New().Headers(rows[0]...).Rows(rows[1:]...).Wrap(true).
		Border(border).BorderRow(multiline).
		StyleFunc(func(_, col int) lipgloss.Style {
			alignment := lipgloss.Left
			switch node.Alignments[col] {
			case extast.AlignCenter:
				alignment = lipgloss.Center
			case extast.AlignRight:
				alignment = lipgloss.Right
			}
			return lipgloss.NewStyle().Width(widths[col]+2).Padding(0, 1).Align(alignment)
		})
	margin := 0
	if options.Styles.Document.Margin != nil {
		margin = int(*options.Styles.Document.Margin)
	}
	return options.Styles.Document.BlockPrefix + "\n" + padMargin(t.String(), margin) + "\n" + options.Styles.Document.BlockSuffix, nil
}

// Cuts replay the original ANSI controls on each line, closing styles and OSC
// links before cell padding/rails. No SGR state reconstruction is necessary.
func wrapTableCell(cell string, width int) string {
	var lines []string
	for _, line := range strings.Split(cell, "\n") {
		words := tableWords(line)
		if len(words) == 0 {
			lines = append(lines, line)
			continue
		}
		start, end := words[0].start, words[0].end
		for _, word := range words[1:] {
			if word.end-start > width {
				lines = append(lines, ansi.Cut(line, start, end))
				start = word.start
			}
			end = word.end
		}
		lines = append(lines, ansi.Cut(line, start, end))
	}
	return strings.Join(lines, "\n")
}

type tableWord struct{ start, end int } // Display columns, not source offsets.

func tableWords(s string) []tableWord {
	var words []tableWord
	col, start := 0, -1
	for rest := ansi.Strip(s); rest != ""; {
		cluster, width := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		rest = rest[len(cluster):]
		// Nonbreaking spaces (including Glamour's inline-code padding) stay
		// attached to their token. Never split a CJK run or hyphenated token.
		space := strings.TrimSpace(cluster) == "" && cluster != "\u00a0" && cluster != "\u202f"
		if space && start >= 0 {
			words = append(words, tableWord{start, col})
			start = -1
		} else if !space && start < 0 {
			start = col
		}
		col += width
	}
	if start >= 0 {
		words = append(words, tableWord{start, col})
	}
	return words
}

// Public Glamour elements retain inline Markdown formatting; only table link
// presentation changes: label-only OSC 8, with an identity per source occurrence.
func tableInline(e glamansi.ElementRenderer, tableID int, serial *int, imageStyle glamansi.StylePrimitive) glamansi.ElementRenderer {
	switch e := e.(type) {
	case *glamansi.EmphasisElement:
		for i, child := range e.Children {
			e.Children[i] = tableInline(child, tableID, serial, imageStyle)
		}
	case *glamansi.LinkElement:
		for i, child := range e.Children {
			e.Children[i] = tableLinkLabel(child, imageStyle)
		}
		if !e.SkipText {
			e.SkipHref = true
		}
		*serial++
		return tableLinkElement{e, fmt.Sprintf("%s%d-%d", tableLinkPrefix, tableID, *serial)}
	case *glamansi.ImageElement:
		e.TextOnly = e.Text != ""
		*serial++
		return tableLinkElement{e, fmt.Sprintf("%s%d-%d", tableLinkPrefix, tableID, *serial)}
	}
	return e
}

func tableLinkLabel(e glamansi.ElementRenderer, imageStyle glamansi.StylePrimitive) glamansi.ElementRenderer {
	switch e := e.(type) {
	case *glamansi.EmphasisElement:
		for i, child := range e.Children {
			e.Children[i] = tableLinkLabel(child, imageStyle)
		}
	case *glamansi.ImageElement:
		label := e.Text
		if label == "" {
			label = e.URL
		}
		imageStyle.Format = strings.TrimSuffix(imageStyle.Format, " →")
		return &glamansi.BaseElement{Token: label, Style: imageStyle}
	}
	return e
}

func expandTableTabs(s string) string {
	var b strings.Builder
	tab := lipgloss.NewStyle().Render("\t")
	state := byte(0)
	for rest := s; rest != ""; {
		seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
		rest, state = rest[n:], next
		if seq == "\t" {
			seq = tab
		}
		b.WriteString(seq)
	}
	return b.String()
}

type tableLinkElement struct {
	glamansi.ElementRenderer
	id string
}

func (e tableLinkElement) Render(w io.Writer, ctx glamansi.RenderContext) error {
	var b bytes.Buffer
	if err := e.ElementRenderer.Render(&b, ctx); err != nil {
		return err
	}
	state := byte(0)
	for rest := b.String(); rest != ""; {
		seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
		rest, state = rest[n:], next
		if _, dest, ok := parseOSC8(seq); ok && dest != "" {
			seq = ansi.SetHyperlink(dest, "id="+e.id)
		}
		if _, err := io.WriteString(w, seq); err != nil {
			return err
		}
	}
	return nil
}

func styleTableBorder(b lipgloss.Border, sgr string) lipgloss.Border {
	for _, field := range []*string{&b.Top, &b.Bottom, &b.Left, &b.Right, &b.TopLeft, &b.TopRight, &b.BottomLeft, &b.BottomRight, &b.MiddleLeft, &b.MiddleRight, &b.Middle, &b.MiddleTop, &b.MiddleBottom} {
		if *field != "" {
			*field = sgr + *field + "\x1b[m"
		}
	}
	return b
}

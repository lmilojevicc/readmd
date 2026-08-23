package pager

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	ext "github.com/yuin/goldmark/extension"
	astext "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var md = goldmark.New(
	goldmark.WithExtensions(
		ext.GFM,
		ext.DefinitionList,
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
)

type edit struct {
	start, end  int
	replacement string
}

func applyEdits(src string, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	prev := 0
	for _, e := range edits {
		b.WriteString(src[prev:e.start])
		b.WriteString(e.replacement)
		prev = e.end
	}
	b.WriteString(src[prev:])
	return b.String()
}

func lineStart(src string, i int) int {
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	return i
}

func lineEnd(src string, i int) int {
	for i < len(src) && src[i] != '\n' {
		i++
	}
	return i
}

func collapseTables(src string) string {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	var edits []edit
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		t, ok := n.(*astext.Table)
		if !ok {
			return ast.WalkContinue, nil
		}
		headers := headerNames(t, bsrc)
		rows := bodyRows(t, bsrc)
		first, last := tableExtent(t)
		if first < 0 || last <= first {
			return ast.WalkContinue, nil
		}
		start, end := lineStart(src, first), lineEnd(src, last)
		if len(rows) == 0 && end < len(src) && src[end] == '\n' {
			if de := lineEnd(src, end+1); isDelimiterRow(src[end+1 : de]) {
				end = de
			}
		}
		if end < len(src) && src[end] == '\n' {
			end++
		}
		var rec strings.Builder
		writeRecords(&rec, headers, rows)
		edits = append(edits, edit{start, end, rec.String()})
		return ast.WalkContinue, nil
	})
	if len(edits) == 0 {
		return src
	}
	return applyEdits(src, edits)
}

func isDelimiterRow(line string) bool {
	s := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '>':
			return -1
		}
		return r
	}, line)
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case '|', '-', ':':
		default:
			return false
		}
	}
	return true
}

func tableExtent(t *astext.Table) (int, int) {
	first, last := -1, -1
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			tc, ok := cell.(*astext.TableCell)
			if !ok || tc.Lines().Len() == 0 {
				continue
			}
			seg := tc.Lines().At(0)
			if first < 0 || seg.Start < first {
				first = seg.Start
			}
			if seg.Stop > last {
				last = seg.Stop
			}
		}
	}
	return first, last
}

var mdEscapes = strings.NewReplacer(
	"\\`", "`", "\\\\", "\\", "\\*", "*", "\\_", "_",
	"\\{", "{", "\\}", "}", "\\[", "[", "\\]", "]",
	"\\<", "<", "\\>", ">", "\\(", "(", "\\)", ")",
	"\\#", "#", "\\+", "+", "\\-", "-",
	"\\.", ".", "\\!", "!", "\\|", "|", "\\~", "~",
)

func cellText(cell *astext.TableCell, src []byte) string {
	var b strings.Builder
	writeInline(&b, cell.FirstChild(), src)
	return strings.TrimSpace(b.String())
}

func writeInline(b *strings.Builder, n ast.Node, src []byte) {
	for c := n; c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			b.WriteString(mdEscapes.Replace(string(c.Segment.Value(src))))
			if c.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeSpan:
			b.WriteByte('`')
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if t, ok := t.(*ast.Text); ok {
					b.Write(t.Segment.Value(src))
				}
			}
			b.WriteByte('`')
		case *ast.Link:
			fmt.Fprintf(b, "[%s](%s)", inlineText(c, src), wrapDest(string(c.Destination)))
		case *ast.Image:
			fmt.Fprintf(b, "![%s](%s)", inlineText(c, src), wrapDest(string(c.Destination)))
		case *ast.AutoLink:
			b.Write(c.URL(src))
		case *ast.Emphasis:
			m := "*"
			if c.Level == 2 {
				m = "**"
			}
			b.WriteString(m)
			writeInline(b, c.FirstChild(), src)
			b.WriteString(m)
		case *astext.Strikethrough:
			b.WriteString("~~")
			writeInline(b, c.FirstChild(), src)
			b.WriteString("~~")
		case *ast.RawHTML:
			for i := range c.Segments.Len() {
				seg := c.Segments.At(i)
				b.Write(seg.Value(src))
			}
		default:
			writeInline(b, c.FirstChild(), src)
		}
	}
}

func inlineText(n ast.Node, src []byte) string {
	var b strings.Builder
	writeInline(&b, n.FirstChild(), src)
	return b.String()
}

func wrapDest(dest string) string {
	if strings.ContainsAny(dest, " \t") && !strings.HasPrefix(dest, "<") {
		return "<" + dest + ">"
	}
	return dest
}

func headerNames(t *astext.Table, src []byte) []string {
	h, ok := t.FirstChild().(*astext.TableHeader)
	if !ok {
		return nil
	}
	var names []string
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		tc, ok := c.(*astext.TableCell)
		if !ok {
			continue
		}
		names = append(names, cellText(tc, src))
	}
	return names
}

func bodyRows(t *astext.Table, src []byte) [][]string {
	var rows [][]string
	for r := t.FirstChild(); r != nil; r = r.NextSibling() {
		row, ok := r.(*astext.TableRow)
		if !ok {
			continue
		}
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			tc, ok := c.(*astext.TableCell)
			if !ok {
				continue
			}
			cells = append(cells, cellText(tc, src))
		}
		rows = append(rows, cells)
	}
	return rows
}

func writeRecords(out *strings.Builder, headers []string, rows [][]string) {
	for _, row := range rows {
		n := min(len(headers), len(row))
		wrote := false
		for j := range n {
			val := row[j]
			if val == "" {
				continue
			}
			name := headers[j]
			if name == "" {
				name = fmt.Sprintf("col %d", j+1)
			}
			prefix := "- "
			if wrote {
				prefix = "  "
			}
			fmt.Fprintf(out, "%s%s: %s\n", prefix, name, val)
			wrote = true
		}
	}
}

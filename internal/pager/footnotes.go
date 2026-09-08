package pager

import (
	"sort"
	"strings"

	glamstyles "charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"

	tea "charm.land/bubbletea/v2"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	ext "github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var footnoteMD = goldmark.New(goldmark.WithExtensions(ext.GFM, ext.Footnote))

type sourceFootnoteMarker struct {
	label      string
	cell       string
	start      int
	definition bool
	reference  bool
}

type sourceRange struct{ start, end int }

type documentLocation struct {
	y, x   int
	reader bool
}

func (m *Model) currentLocation() documentLocation {
	return documentLocation{
		y: m.vp.YOffset(), x: m.vp.XOffset(),
		reader: m.reader,
	}
}

func (m *Model) jumpFootnote(target linkTarget) {
	m.locations = append(m.locations, m.currentLocation())
	m.vp.SetYOffset(target.definition.line)
	m.revealTargetRegion(target.definition)
	m.flash = "footnote " + target.footnote
}

func (m *Model) revealTargetRegion(region targetRegion) {
	x, width := m.vp.XOffset(), m.vp.Width()
	if width <= 0 || (region.start >= x && region.end <= x+width) {
		return
	}
	m.vp.SetXOffset(region.start)
	m.clampXWidest()
}

func (m *Model) backLocation() tea.Cmd {
	if len(m.locations) == 0 {
		return nil
	}
	last := len(m.locations) - 1
	loc := m.locations[last]
	m.locations = m.locations[:last]
	changed := m.reader != loc.reader
	m.reader = loc.reader
	m.pendingLocation = &loc
	m.syncVPWidth()
	if changed {
		return m.requestRender()
	}
	m.applyPendingLocation()
	return nil
}

func (m *Model) applyPendingLocation() {
	if m.pendingLocation == nil {
		return
	}
	loc := *m.pendingLocation
	m.pendingLocation = nil
	m.vp.SetYOffset(loc.y)
	m.vp.SetXOffset(loc.x)
	m.clampXWidest()
}

func renderedTargets(src string, base, stripped []string) []linkTarget {
	links := suppressFootnoteReferenceLinks(parseRenderedLinkTargets(src, base), usedFootnoteLabels(src))
	return append(links, parseFootnoteTargets(src, base)...)
}

func usedFootnoteLabels(src string) map[string]bool {
	doc := footnoteMD.Parser().Parse(text.NewReader([]byte(src)))
	labels := map[string]bool{}
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if fn, ok := n.(*extast.Footnote); ok && fn.Index > 0 {
				labels[string(fn.Ref)] = true
			}
		}
		return ast.WalkContinue, nil
	})
	return labels
}

func suppressFootnoteReferenceLinks(links []linkTarget, labels map[string]bool) []linkTarget {
	out := links[:0]
	for _, link := range links {
		if len(link.texts) > 0 && strings.HasPrefix(link.texts[0], "^") && labels[strings.TrimPrefix(link.texts[0], "^")] {
			continue
		}
		out = append(out, link)
	}
	return out
}

type footnoteOccurrence struct {
	marker  sourceFootnoteMarker
	regions []targetRegion
}

func parseFootnoteTargets(src string, rendered []string) []linkTarget {
	occurrences := mappedFootnoteMarkers(src, rendered)
	definitions := map[string]targetRegion{}
	for _, o := range occurrences {
		if o.marker.definition {
			definitions[o.marker.label] = o.regions[0]
		}
	}
	var targets []linkTarget
	for _, o := range occurrences {
		if o.marker.reference {
			targets = append(targets, linkTarget{kind: targetFootnote, id: "footnote:" + o.marker.label, footnote: o.marker.label, regions: o.regions, definition: definitions[o.marker.label]})
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		a, b := targets[i].regions[0], targets[j].regions[0]
		if a.line != b.line {
			return a.line < b.line
		}
		return a.start < b.start
	})
	return targets
}

func mappedFootnoteMarkers(src string, rendered []string) []footnoteOccurrence {
	markers, ok := sourceFootnoteMarkers(src)
	if !ok || len(markers) == 0 {
		return nil
	}
	cells := renderedTableCells(rendered)
	stripped := make([]string, len(rendered))
	for i, line := range rendered {
		stripped[i] = ansi.Strip(line)
	}
	byLabel := map[string][]int{}
	for i, marker := range markers {
		byLabel[marker.label] = append(byLabel[marker.label], i)
	}

	var occurrences []footnoteOccurrence
	for label, indexes := range byLabel {
		definition := -1
		for _, i := range indexes {
			if markers[i].definition {
				if definition >= 0 {
					definition = -2
					break
				}
				definition = i
			}
		}
		if definition < 0 {
			continue
		}
		regions := renderedFootnoteMarkers(stripped, label)
		if len(regions) != len(indexes) {
			continue
		}
		// Table visual order is line-major, not source cell-major. Associate
		// occurrences only within their source cell; prose keeps its old order.
		byCell := map[string][][]targetRegion{}
		for _, region := range regions {
			cell := footnoteRegionCell(region, cells)
			byCell[cell] = append(byCell[cell], region)
		}
		regionBySource := map[int][]targetRegion{}
		for _, i := range indexes {
			cell := markers[i].cell
			if len(byCell[cell]) == 0 {
				break
			}
			regionBySource[i] = byCell[cell][0]
			byCell[cell] = byCell[cell][1:]
		}
		if len(regionBySource) != len(indexes) {
			continue
		}
		for _, i := range indexes {
			if markers[i].reference || markers[i].definition {
				occurrences = append(occurrences, footnoteOccurrence{markers[i], regionBySource[i]})
			}
		}
	}
	return occurrences
}

func sourceFootnoteMarkers(src string) ([]sourceFootnoteMarker, bool) {
	bsrc := []byte(src)
	doc := footnoteMD.Parser().Parse(text.NewReader(bsrc))
	definitions := map[int]string{}
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if fn, ok := n.(*extast.Footnote); ok && fn.Index > 0 {
				definitions[fn.Index] = string(fn.Ref)
			}
		}
		return ast.WalkContinue, nil
	})
	var expected []string
	var codeRanges, htmlRanges []sourceRange
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *extast.FootnoteLink:
			if label := definitions[n.Index]; label != "" {
				expected = append(expected, label)
			}
		case *ast.FencedCodeBlock:
			codeRanges = appendSegments(codeRanges, n.Lines())
		case *ast.CodeBlock:
			codeRanges = appendSegments(codeRanges, n.Lines())
		case *ast.CodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					codeRanges = append(codeRanges, sourceRange{t.Segment.Start, t.Segment.Stop})
				}
			}
		case *ast.HTMLBlock:
			htmlRanges = appendSegments(htmlRanges, n.Lines())
		case *ast.RawHTML:
			htmlRanges = appendSegments(htmlRanges, n.Segments)
		}
		return ast.WalkContinue, nil
	})
	if len(expected) == 0 {
		return nil, true
	}
	used := map[string]bool{}
	for _, label := range definitions {
		used[label] = true
	}
	markers := scanSourceFootnoteMarkers(src, used)
	filtered := markers[:0]
	for _, marker := range markers {
		if !inSourceRanges(marker.start, htmlRanges) {
			filtered = append(filtered, marker)
		}
	}
	markers = filtered
	cells := sourceTableCells(src)
	for i := range markers {
		for id, ranges := range cells {
			if inSourceRanges(markers[i].start, ranges) {
				markers[i].cell = id
				break
			}
		}
	}
	var candidates []int
	definitionCount := map[string]int{}
	for i := range markers {
		marker := &markers[i]
		if escapedAt(src, marker.start) || inSourceRanges(marker.start, codeRanges) {
			continue
		}
		marker.definition = isDefinitionMarker(src, marker.start, marker.label)
		if marker.definition {
			definitionCount[marker.label]++
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) != len(expected) {
		return nil, false
	}
	for i, markerIndex := range candidates {
		if markers[markerIndex].label != expected[i] {
			return nil, false
		}
		markers[markerIndex].reference = true
	}
	for label := range used {
		if definitionCount[label] != 1 {
			return nil, false
		}
	}
	return markers, true
}

func appendSegments(ranges []sourceRange, segments *text.Segments) []sourceRange {
	for i := 0; i < segments.Len(); i++ {
		seg := segments.At(i)
		ranges = append(ranges, sourceRange{seg.Start, seg.Stop})
	}
	return ranges
}

func scanSourceFootnoteMarkers(src string, used map[string]bool) []sourceFootnoteMarker {
	var out []sourceFootnoteMarker
	for from := 0; from < len(src); {
		rel := strings.Index(src[from:], "[^")
		if rel < 0 {
			break
		}
		start := from + rel
		closeRel := strings.IndexByte(src[start+2:], ']')
		if closeRel < 0 {
			break
		}
		end := start + 2 + closeRel
		label := src[start+2 : end]
		if !strings.ContainsAny(label, "\r\n[]") && used[label] {
			out = append(out, sourceFootnoteMarker{label: label, start: start})
		}
		from = end + 1
	}
	return out
}

func isDefinitionMarker(src string, start int, label string) bool {
	end := start + len(label) + 3
	if end >= len(src) || src[end] != ':' {
		return false
	}
	line := lineStart(src, start)
	prefix := src[line:start]
	return len(prefix) <= 3 && strings.Trim(prefix, " ") == ""
}

func escapedAt(src string, start int) bool {
	backslashes := 0
	for i := start - 1; i >= 0 && src[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func inSourceRanges(pos int, ranges []sourceRange) bool {
	for _, r := range ranges {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

// A wrapped occurrence retains each fragment's display columns. Only exact
// token bytes across adjacent content-row edges are joined; blank rows and
// container rails cannot be skipped. Source counts still gate the whole label.
func renderedFootnoteMarkers(lines []string, label string) [][]targetRegion {
	token := "[^" + label + "]"
	var out [][]targetRegion
	for line, content := range lines {
		for from := 0; from < len(content); {
			rel := strings.Index(content[from:], "[")
			if rel < 0 {
				break
			}
			start := from + rel
			from = start + 1
			var regions []targetRegion
			remaining := token
			for row, offset := line, start; row < len(lines); row++ {
				part := strings.TrimRight(lines[row][offset:], " ")
				if strings.HasPrefix(part, remaining) {
					part = remaining
				} else if part == "" || !strings.HasPrefix(remaining, part) {
					break
				}
				col := ansi.StringWidth(lines[row][:offset])
				regions = append(regions, targetRegion{row, col, col + ansi.StringWidth(part)})
				remaining = strings.TrimPrefix(remaining, part)
				if remaining == "" {
					out = append(out, regions)
					break
				}
				if row+1 < len(lines) {
					offset = len(lines[row+1]) - len(strings.TrimLeft(lines[row+1], " "))
				}
			}
		}
	}
	return out
}

// Only semantically validated occurrences are escaped in render-only Markdown.
// Otherwise a one-word definition can be consumed as a reference-link URL.
func protectFootnoteMarkers(src string) string {
	markers, ok := sourceFootnoteMarkers(src)
	if !ok {
		return src
	}
	var edits []edit
	for _, m := range markers {
		if m.reference || m.definition {
			edits = append(edits, edit{m.start, m.start, "\\"})
		}
	}
	return applyEdits(src, edits)
}

func styleFootnoteMarkers(src, out, base string, theme config.Theme) string {
	lines := strings.Split(out, "\n")
	occurrences := mappedFootnoteMarkers(src, lines)
	st := paletteConfig
	if base != paletteStyleName {
		if value, ok := glamstyles.DefaultStyles[base]; ok {
			st = *value
		} else {
			st = glamstyles.NoTTYStyleConfig
		}
	}
	reference := mergeText(primitiveStyle(st.Link), config.TextStyle{Underline: boolPtr(true)})
	definition := mergeText(primitiveStyle(st.LinkText), config.TextStyle{Bold: boolPtr(true), Underline: boolPtr(false)})
	reference = mergeText(reference, theme.Footnotes.Reference)
	definition = mergeText(definition, theme.Footnotes.Definition)
	// Apply rightmost regions first so each region's coordinates stay immutable.
	var regions []struct {
		targetRegion
		style config.TextStyle
	}
	for _, o := range occurrences {
		style := reference
		if o.marker.definition {
			style = definition
		}
		for _, r := range o.regions {
			regions = append(regions, struct {
				targetRegion
				style config.TextStyle
			}{r, style})
		}
	}
	sort.Slice(regions, func(i, j int) bool {
		a, b := regions[i], regions[j]
		if a.line != b.line {
			return a.line < b.line
		}
		return a.start > b.start
	})
	for _, r := range regions {
		lines[r.line] = styleRegion(lines[r.line], r.start, r.end, textSGR(r.style, base == "notty" || base == ""))
	}
	return strings.Join(lines, "\n")
}

// Unlike search highlighting, role overrides compose with the underlying
// attributes and replay every stock SGR within the validated region.
func styleRegion(line string, start, end int, overlay string) string {
	var out strings.Builder
	var active []string
	col, state, inside := 0, byte(0), false
	for rest := line; rest != ""; {
		seq, w, n, next := ansi.DecodeSequence(rest, state, nil)
		rest, state = rest[n:], next
		if inside && col >= end {
			out.WriteString("\x1b[m" + strings.Join(active, ""))
			inside = false
		}
		if !inside && col >= start && col < end && w > 0 {
			out.WriteString(overlay)
			inside = true
		}
		out.WriteString(seq)
		if isSGR(seq) {
			active = pushSGR(active, seq)
			if inside {
				out.WriteString(overlay)
			}
		}
		col += w
	}
	if inside {
		out.WriteString("\x1b[m" + strings.Join(active, ""))
	}
	return out.String()
}

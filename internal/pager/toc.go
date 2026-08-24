package pager

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	tea "charm.land/bubbletea/v2"
)

type heading struct {
	level   int
	text    string
	line    int
	srcLine int
}

func extractHeadings(src string) []heading {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	var heads []heading
	prev := 0
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		line := prev
		if h.Lines().Len() > 0 {
			line = strings.Count(src[:h.Lines().At(0).Start], "\n")
		}
		prev = line
		heads = append(heads, heading{level: h.Level, text: plainText(h, bsrc), srcLine: line})
		return ast.WalkContinue, nil
	})
	return heads
}

func writePlain(b *strings.Builder, n ast.Node, src []byte) {
	for c := n; c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
			if c.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeSpan:
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if t, ok := t.(*ast.Text); ok {
					b.Write(t.Segment.Value(src))
				}
			}
		case *ast.Link:
			writePlain(b, c.FirstChild(), src)
			b.WriteByte(' ')
			b.Write(c.Destination)
		default:
			writePlain(b, c.FirstChild(), src)
		}
	}
}

func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	writePlain(&b, n.FirstChild(), src)
	return b.String()
}

func normHeading(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case '#', '*', '_', '`', '~', '>', '|':
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// mapHeadings assigns each heading the rendered line where its text starts,
// scanning lines with a monotonic cursor so duplicate headings resolve in
// document order. Headings never match text from before their own run of
// non-empty rendered lines.
func mapHeadings(heads []heading, lines []string) {
	const maxRun = 4096
	norm := make([]string, len(heads))
	for i, h := range heads {
		norm[i] = normHeading(h.text)
	}
	hi, lastEnd, runStart := 0, 0, 0
	var acc string
	var starts []int
	reset := func() {
		acc = ""
		starts = starts[:0]
		lastEnd = 0
	}
	for j, line := range lines {
		s := normHeading(line)
		if s == "" {
			reset()
			continue
		}
		if acc == "" {
			runStart = j
		}
		starts = append(starts, len(acc))
		if acc != "" {
			acc += " "
		}
		acc += s
		for hi < len(heads) && lastEnd <= len(acc) {
			if norm[hi] == "" {
				if hi > 0 {
					heads[hi].line = heads[hi-1].line
				}
				hi++
				continue
			}
			k := strings.Index(acc[lastEnd:], norm[hi])
			if k < 0 {
				break
			}
			a := lastEnd + k
			row := 0
			for row+1 < len(starts) && starts[row+1] <= a {
				row++
			}
			heads[hi].line = runStart + row
			lastEnd = a + len(norm[hi])
			hi++
		}
		if len(acc) > maxRun {
			reset()
		}
	}
	for ; hi < len(heads); hi++ {
		if norm[hi] == "" && hi > 0 {
			heads[hi].line = heads[hi-1].line
			continue
		}
		heads[hi].line = len(lines)
	}
}

const tocMaxWidth = 40

var (
	tocSelStyle    = lipgloss.NewStyle().Reverse(true)
	tocSectionMark = lipgloss.NewStyle().Bold(true).MarginLeft(1)
)

func (m *Model) tocPanelWidth() int { return min(tocMaxWidth, max(0, m.width-4)) }

func (m *Model) currentSection() int {
	cur := -1
	for i, h := range m.heads {
		if h.line > m.vp.YOffset() {
			break
		}
		cur = i
	}
	return cur
}

func (m *Model) openTOC() {
	if len(m.heads) == 0 || m.tocPanelWidth() == 0 {
		return
	}
	m.tocOpen = true
	if m.tocSel >= len(m.heads) {
		m.tocSel = max(0, len(m.heads)-1)
	}
}

func (m *Model) handleTOCKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "o":
		m.tocOpen = false
	case "enter":
		if m.tocSel < len(m.heads) {
			m.vp.SetYOffset(m.heads[m.tocSel].line)
		}
		m.tocOpen = false
	case "j", "down":
		if m.tocSel < len(m.heads)-1 {
			m.tocSel++
		}
	case "k", "up":
		if m.tocSel > 0 {
			m.tocSel--
		}
	case "g", "home":
		m.tocSel = 0
	case "G", "end", "pgdown":
		m.tocSel = max(0, len(m.heads)-1)
	}
	return nil
}

func (m *Model) tocRows(width, height int) []string {
	rows := make([]string, height)
	if len(m.heads) == 0 || width == 0 || height == 0 {
		return rows
	}
	top := max(0, m.tocSel-height+1)
	cur := m.currentSection()
	for row := 0; row < height && top+row < len(m.heads); row++ {
		idx := top + row
		h := m.heads[idx]
		label := ansi.Truncate(strings.Repeat("  ", max(0, h.level-1))+h.text, width-1, "…")
		switch idx {
		case m.tocSel:
			label += strings.Repeat(" ", max(0, width-ansi.StringWidth(label)))
			rows[row] = tocSelStyle.Render(label)
		case cur:
			rows[row] = tocSectionMark.Render(label)
		default:
			rows[row] = label
		}
	}
	return rows
}

func (m *Model) applyOverlay(body string) string {
	pw := m.tocPanelWidth()
	if pw == 0 || len(m.heads) == 0 {
		return body
	}
	x0 := 0
	if pw*2 > m.width {
		x0 = max(0, (m.width-pw)/2)
	}
	return m.overlay(body, x0, pw, m.tocRows(pw, len(strings.Split(body, "\n"))))
}

func (m *Model) overlay(body string, x0, pw int, rows []string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		if i >= len(rows) || rows[i] == "" {
			continue
		}
		left := ansi.Cut(lines[i], 0, x0)
		left += strings.Repeat(" ", x0-ansi.StringWidth(left))
		lines[i] = left + rows[i] + ansi.TruncateLeft(lines[i], x0+pw, "")
	}
	return strings.Join(lines, "\n")
}

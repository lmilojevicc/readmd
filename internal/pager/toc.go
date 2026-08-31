package pager

import (
	"fmt"
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

const (
	tocTitle    = "Outline"
	tocMaxWidth = 56
	tocMaxRows  = 20
)

var (
	tocSelStyle    = lipgloss.NewStyle().Reverse(true)
	tocSectionMark = lipgloss.NewStyle().Bold(true)
)

func (m *Model) tocFits() bool { return m.width-4 >= 3 && m.height-4 >= 3 }

// Keep four document rows visible above and below the centered panel when the
// terminal is tall enough; those rows make selection previews legible.
func (m *Model) tocVisible() int { return max(3, min(m.height-11, tocMaxRows)) }

func (m *Model) tocMinLevel() int {
	level := 6
	for _, h := range m.heads {
		level = min(level, h.level)
	}
	return level
}

func (m *Model) tocInnerWidth() int {
	avail := m.width - 4
	if avail < 3 {
		return 0
	}
	minLevel := m.tocMinLevel()
	content := 0
	for _, h := range m.heads {
		content = max(content, 2+2*max(0, h.level-minLevel)+ansi.StringWidth(h.text))
	}
	return min(max(content, min(48, avail-2)), min(tocMaxWidth, avail-2))
}

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

func (m *Model) filteredHeadings() []int {
	filter := strings.ToLower(m.tocFilter)
	out := make([]int, 0, len(m.heads))
	for i, h := range m.heads {
		if filter == "" || strings.Contains(strings.ToLower(h.text), filter) {
			out = append(out, i)
		}
	}
	return out
}

func selectedHeadingPosition(heads []int, selected int) int {
	for i, idx := range heads {
		if idx == selected {
			return i
		}
	}
	return -1
}

func (m *Model) openTOC() {
	if len(m.heads) == 0 || !m.tocFits() {
		return
	}
	m.tocOpen = true
	m.tocFilter = ""
	m.tocPrompt = false
	m.tocPreview = false
	m.tocSel = max(0, m.currentSection())
}

func (m *Model) closeTOC() {
	m.tocOpen = false
	m.tocFilter = ""
	m.tocPrompt = false
	m.tocPreview = false
}

func (m *Model) moveTOC(delta int) {
	heads := m.filteredHeadings()
	if len(heads) == 0 {
		return
	}
	pos := selectedHeadingPosition(heads, m.tocSel)
	if pos < 0 {
		pos = 0
	}
	next := min(max(0, pos+delta), len(heads)-1)
	if heads[next] != m.tocSel {
		m.tocSel = heads[next]
		m.tocPreview = true
	}
}

func (m *Model) moveTOCEdge(end bool) {
	heads := m.filteredHeadings()
	if len(heads) == 0 {
		return
	}
	next := heads[0]
	if end {
		next = heads[len(heads)-1]
	}
	if next != m.tocSel {
		m.tocSel = next
		m.tocPreview = true
	}
}

func (m *Model) setTOCFilter(filter string) {
	old := m.tocSel
	m.tocFilter = filter
	heads := m.filteredHeadings()
	if len(heads) == 0 {
		m.tocPreview = false
		return
	}
	if selectedHeadingPosition(heads, old) >= 0 {
		return
	}
	m.tocSel = heads[len(heads)-1]
	for _, idx := range heads {
		if idx >= old {
			m.tocSel = idx
			break
		}
	}
	m.tocPreview = true
}

func (m *Model) handleTOCKey(msg tea.KeyMsg) tea.Cmd {
	if m.tocPrompt {
		switch msg.String() {
		case "esc":
			m.tocPrompt = false
			m.setTOCFilter("")
		case "enter":
			m.tocPrompt = false
		case "backspace", "ctrl+h":
			if r := []rune(m.tocFilter); len(r) > 0 {
				m.setTOCFilter(string(r[:len(r)-1]))
			}
		default:
			if kp, ok := msg.(tea.KeyPressMsg); ok {
				if rs := []rune(kp.Text); len(rs) > 0 && printable(rs) {
					m.setTOCFilter(m.tocFilter + string(rs))
				}
			}
		}
		return nil
	}
	switch msg.String() {
	case "esc":
		if m.tocFilter != "" {
			m.setTOCFilter("")
			return nil
		}
		m.closeTOC()
	case "q", "o":
		m.closeTOC()
	case "enter":
		if m.tocSel >= 0 && m.tocSel < len(m.heads) && len(m.filteredHeadings()) > 0 {
			line := m.heads[m.tocSel].line
			if m.rendering {
				m.anchor = &anchorState{y: line, total: len(m.stripped), heads: m.heads}
			}
			m.vp.SetYOffset(line)
			m.vp.SetXOffset(0)
			m.closeTOC()
		}
	case "/":
		m.tocPrompt = true
	case "j", "down":
		m.moveTOC(1)
	case "k", "up":
		m.moveTOC(-1)
	case "g", "home":
		m.moveTOCEdge(false)
	case "G", "end":
		m.moveTOCEdge(true)
	case "pgdown":
		m.moveTOC(m.tocVisible())
	case "pgup":
		m.moveTOC(-m.tocVisible())
	}
	return nil
}

// tocPreviewBody is the experimental preview seam: it renders a copied
// viewport at the selected heading and never mutates the real viewport.
func (m *Model) tocPreviewBody() string {
	if !m.tocOpen || !m.tocPreview || m.tocSel < 0 || m.tocSel >= len(m.heads) {
		return m.vp.View()
	}
	preview := m.vp
	preview.SetYOffset(m.heads[m.tocSel].line)
	preview.SetXOffset(0)
	return preview.View()
}

func (m *Model) tocRows() []string {
	innerW := m.tocInnerWidth()
	if innerW <= 0 || !m.tocFits() {
		return nil
	}
	heads := m.filteredHeadings()
	visible := m.tocVisible()
	pos := selectedHeadingPosition(heads, m.tocSel)
	top := 0
	if pos >= 0 {
		top = min(max(0, pos-visible/2), max(0, len(heads)-visible))
	}
	rows := make([]string, 0, visible+2)
	rows = append(rows, modalTop(tocTitle, innerW))
	minLevel := m.tocMinLevel()
	cur := m.currentSection()
	for row := 0; row < visible; row++ {
		label := ""
		if len(heads) == 0 && row == 0 {
			label = "  No matching headings"
		} else if top+row < len(heads) {
			idx := heads[top+row]
			h := m.heads[idx]
			prefix := "  "
			if idx == cur {
				prefix = "• "
			}
			label = prefix + strings.Repeat("  ", max(0, h.level-minLevel)) + h.text
			label = ansi.Truncate(label, innerW, "…")
			label += strings.Repeat(" ", max(0, innerW-ansi.StringWidth(label)))
			switch idx {
			case m.tocSel:
				label = tocSelStyle.Render(label)
			case cur:
				label = tocSectionMark.Render(label)
			}
		}
		rows = append(rows, modalRow(label, innerW))
	}
	seg := ""
	if m.tocPrompt {
		seg = "/" + m.tocFilter
	} else {
		shown := 0
		if pos >= 0 {
			shown = pos + 1
		}
		filter := ""
		if m.tocFilter != "" {
			filter = " · /" + m.tocFilter
		}
		seg = fmt.Sprintf(" %d of %d%s · / filter · enter jump · esc cancel ", shown, len(heads), filter)
	}
	return append(rows, modalBottom(innerW, seg))
}

func (m *Model) applyOverlay(body string) string {
	rows := m.tocRows()
	if len(rows) == 0 {
		return body
	}
	pw := ansi.StringWidth(rows[0])
	x0 := max(0, (m.width-pw)/2)
	y0 := max(0, (len(strings.Split(body, "\n"))-len(rows))/2)
	return m.overlay(body, x0, y0, pw, rows)
}

func (m *Model) overlay(body string, x0, y0, pw int, rows []string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		if i < y0 || i >= y0+len(rows) || rows[i-y0] == "" {
			continue
		}
		left := ansi.Cut(lines[i], 0, x0)
		left += strings.Repeat(" ", x0-ansi.StringWidth(left))
		lines[i] = left + rows[i-y0] + ansi.TruncateLeft(lines[i], x0+pw, "")
	}
	return strings.Join(lines, "\n")
}

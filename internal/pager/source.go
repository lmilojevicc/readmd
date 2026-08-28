package pager

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

// toggleSource switches rendered/source views. Both directions anchor the
// reading position on the nearest heading above the top line.
func (m *Model) toggleSource() tea.Cmd {
	m.anchor = &anchorState{y: m.vp.YOffset(), total: len(m.stripped), heads: m.heads}
	if m.srcView {
		m.srcView = false
		m.syncVPWidth()
		return m.requestRender()
	}
	m.srcView = true
	m.syncVPWidth()
	m.applySource()
	return nil
}

// applySource rebuilds the viewport from sanitized raw markdown: one logical
// line per row, plain default-fg, no glamour. Heads are re-based onto source
// lines so TOC jumps and search operate directly on the raw text.
func (m *Model) applySource() {
	lines := sourceLines(m.source)
	heads := extractHeadings(m.source)
	for i := range heads {
		heads[i].line = heads[i].srcLine
	}
	m.syncView(lines, lines, heads, nil)
}

func sourceLines(src string) []string {
	lines := strings.Split(sanitize(src), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

func widestLine(lines []string) int {
	widest := 0
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > widest {
			widest = w
		}
	}
	return widest
}

// clampXWidest clamps the x offset against m.widest, which is refreshed
// wherever m.stripped is replaced.
func (m *Model) clampXWidest() {
	if off, maxX := m.vp.XOffset(), max(0, m.widest-m.vp.Width()); off > maxX {
		m.vp.SetXOffset(maxX)
	}
}

// syncVPWidth pins the viewport to the reader column while reader mode is
// active and restores full terminal width otherwise.
func (m *Model) syncVPWidth() {
	if on, effW := m.readerFrame(); on {
		m.vp.SetWidth(effW)
	} else {
		m.vp.SetWidth(m.width)
	}
	m.clampXWidest()
}

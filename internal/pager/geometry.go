package pager

import "github.com/charmbracelet/x/ansi"

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

package pager

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const mouseWheelStep = 3
const targetPanNotice = "adjust pan or press 0 to pick targets"

type targetRowBounds struct{ left, right int }

// Stock Cut retains a wide grapheme intersected by XOffset. Use its actual
// origin, and decline target coordinates if the slice would wrap physically.
func (m *Model) targetViewportRows() ([]targetRowBounds, bool) {
	x, width := m.vp.XOffset(), m.vp.Width()
	rows := make([]targetRowBounds, m.vp.Height())
	for i := range rows {
		line := m.vp.YOffset() + i
		rows[i] = targetRowBounds{x, x}
		if line >= len(m.base) {
			continue
		}
		content := m.base[line]
		cutWidth := ansi.StringWidth(ansi.Cut(content, x, x+width))
		if cutWidth > width {
			return nil, false
		}
		col, state := 0, byte(0)
		for rest := content; rest != ""; {
			_, w, n, next := ansi.DecodeSequence(rest, state, nil)
			if n <= 0 {
				break
			}
			if col+w > x {
				break
			}
			col += w
			state, rest = next, rest[n:]
		}
		rows[i] = targetRowBounds{col, col + cutWidth}
	}
	return rows, true
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if !m.mouse || m.search.active {
		return nil
	}
	if m.targets.active {
		switch m.picker {
		case pickerVimium:
			return nil
		}
		switch msg.Button {
		case tea.MouseWheelDown:
			m.moveTargetFocus(mouseWheelStep)
		case tea.MouseWheelUp:
			m.moveTargetFocus(-mouseWheelStep)
		case tea.MouseWheelRight:
			m.moveTargetFocus(m.targetVisibleRows())
		case tea.MouseWheelLeft:
			m.moveTargetFocus(-m.targetVisibleRows())
		}
		return nil
	}
	step := mouseWheelStep
	switch msg.Button {
	case tea.MouseWheelDown:
		switch {
		case m.helpOpen:
			m.scrollHelp(step)
		case m.tocOpen:
			m.moveTOC(step)
		default:
			m.vp.ScrollDown(step)
		}
	case tea.MouseWheelUp:
		switch {
		case m.helpOpen:
			m.scrollHelp(-step)
		case m.tocOpen:
			m.moveTOC(-step)
		default:
			m.vp.ScrollUp(step)
		}
	case tea.MouseWheelLeft:
		if !m.helpOpen && !m.tocOpen {
			m.vp.ScrollLeft(m.hStep())
		}
	case tea.MouseWheelRight:
		if !m.helpOpen && !m.tocOpen {
			m.vp.ScrollRight(m.hStep())
		}
	}
	return nil
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if !m.mouse || msg.Button != tea.MouseLeft || msg.Mod.Contains(tea.ModShift) ||
		m.search.active || m.tocOpen || m.helpOpen {
		return nil
	}
	if m.targets.active && m.picker == pickerVimium {
		plan := m.targetPlan()
		for _, hit := range plan.hits {
			if msg.Y == hit.line && msg.X >= hit.start && msg.X < hit.end {
				return m.activateTarget(hit.target)
			}
		}
		if msg.Y >= plan.surfaceTop {
			return nil
		}
	}
	if m.targets.active && m.picker == pickerList && msg.Y >= m.targetPanelY() {
		if target, ok := m.targetPanelHit(msg.X, msg.Y); ok {
			return m.activateTarget(target)
		}
		return nil
	}
	if msg.Y < 0 || msg.Y >= m.vp.Height() {
		return nil
	}
	if target, ok := m.mouseTargetAt(msg.X, msg.Y); ok {
		return m.activateTarget(target)
	}
	return nil
}

func (m *Model) mouseTargetAt(x, y int) (hintTarget, bool) {
	margin := 0
	if on, _ := m.readerFrame(); on {
		_, margin = m.readerGeom(true)
		if x < margin || x >= margin+m.vp.Width() {
			return hintTarget{}, false
		}
	}
	rows := m.targets.rows
	if !m.targets.active {
		var safe bool
		rows, safe = m.targetViewportRows()
		if !safe {
			m.errMsg, m.flash = "", targetPanNotice
			return hintTarget{}, false
		}
	}
	if y < 0 || y >= len(rows) || x < margin || x >= margin+m.vp.Width() {
		return hintTarget{}, false
	}
	row := rows[y]
	docX := row.left + x - margin
	docY := m.vp.YOffset() + y
	links := m.links
	if m.targets.active {
		candidates := m.targets.targets
		if m.picker == pickerVimium {
			candidates = m.targetCandidates()
		}
		links = make([]linkTarget, len(candidates))
		for i := range candidates {
			links[i] = candidates[i].linkTarget
		}
	}
	for _, target := range links {
		for _, reg := range target.regions {
			if reg.line != docY {
				continue
			}
			visibleStart, visibleEnd := max(reg.start, row.left), min(reg.end, row.right)
			if visibleStart < visibleEnd && docX >= visibleStart && docX < visibleEnd {
				return hintTarget{linkTarget: target}, true
			}
		}
	}
	return hintTarget{}, false
}

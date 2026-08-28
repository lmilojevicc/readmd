package pager

import tea "charm.land/bubbletea/v2"

const mouseWheelStep = 3

type hintStripHit struct {
	start, end int
	target     hintTarget
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if !m.mouse || m.targets.active || m.search.active {
		return nil
	}
	step := mouseWheelStep
	switch msg.Button {
	case tea.MouseWheelDown:
		switch {
		case m.helpOpen:
			m.scrollHelp(step)
		case m.tocOpen:
			m.tocSel = min(max(0, len(m.heads)-1), m.tocSel+step)
		default:
			m.vp.ScrollDown(step)
		}
	case tea.MouseWheelUp:
		switch {
		case m.helpOpen:
			m.scrollHelp(-step)
		case m.tocOpen:
			m.tocSel = max(0, m.tocSel-step)
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
		m.search.active || m.tocOpen || m.helpOpen || m.srcView {
		return nil
	}
	if m.targets.active && m.chrome().hint && msg.Y == m.vp.Height() {
		for _, hit := range m.hintStripHits() {
			if msg.X >= hit.start && msg.X < hit.end {
				return m.activateTarget(hit.target)
			}
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
		_, margin = readerGeom(m.width, true)
		if x < margin || x >= margin+m.vp.Width() {
			return hintTarget{}, false
		}
	}
	docX := m.vp.XOffset() + x - margin
	docY := m.vp.YOffset() + y
	links := m.links
	if m.targets.active {
		candidates := m.targetCandidates()
		links = make([]linkTarget, len(candidates))
		for i := range candidates {
			links[i] = candidates[i].linkTarget
		}
	}
	left, right := m.vp.XOffset(), m.vp.XOffset()+m.vp.Width()
	for _, target := range links {
		for _, reg := range target.regions {
			if reg.line != docY {
				continue
			}
			visibleStart, visibleEnd := max(reg.start, left), min(reg.end, right)
			if visibleStart < visibleEnd && docX >= visibleStart && docX < visibleEnd {
				return hintTarget{linkTarget: target}, true
			}
		}
	}
	return hintTarget{}, false
}

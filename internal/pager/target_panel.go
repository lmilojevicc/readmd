package pager

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) targetVisibleRows() int {
	if m.targets.panelHeight == 1 {
		return 1
	}
	return m.targets.panelHeight - 4 // borders and two focus-detail rows
}

func (m *Model) targetPanelY() int { return m.height - 1 - m.targets.panelHeight }

func (m *Model) focusedTarget() (hintTarget, bool) {
	candidates := m.targetCandidates()
	if m.targets.focus < 0 || m.targets.focus >= len(candidates) {
		return hintTarget{}, false
	}
	return candidates[m.targets.focus], true
}

func (m *Model) resetTargetFocus() {
	m.targets.focus, m.targets.top, m.targets.detailPage = 0, 0, 0
	m.applySearchView()
}

func (m *Model) moveTargetFocus(delta int) {
	count := len(m.targetCandidates())
	if count == 0 {
		return
	}
	m.targets.focus = min(max(0, m.targets.focus+delta), count-1)
	m.targets.top = min(m.targets.top, m.targets.focus)
	m.targets.top = max(m.targets.top, m.targets.focus-m.targetVisibleRows()+1)
	m.targets.detailPage = 0
	m.applySearchView()
}

func targetText(target hintTarget) string {
	text := strings.Join(target.texts, " ")
	if dest := compactLinkText(printedLinkDestination(target.dest)); dest != "" && !strings.HasPrefix(target.id, tableLinkPrefix) {
		suffix := ""
		for i := len(target.texts) - 1; i > 0; i-- {
			suffix = compactLinkText(target.texts[i]) + suffix
			if suffix == dest {
				text = strings.Join(target.texts[:i], " ")
				break
			}
			if !strings.HasSuffix(dest, suffix) {
				break
			}
		}
	}
	if target.kind == targetFootnote {
		text = "footnote " + target.footnote
	}
	if text == "" {
		text = target.dest
	}
	return panelText(text)
}

// Panel content is plain text, never a second set of terminal hyperlinks.
func panelText(text string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(text))), " ")
}

func targetContext(target hintTarget) string {
	reg := target.regions[0]
	return fmt.Sprintf("%d:%d", reg.line+1, reg.start+1)
}

func targetAction(target hintTarget) string {
	if target.kind == targetFootnote {
		return fmt.Sprintf("Jump to footnote %s (line %d)", panelText(target.footnote), target.definition.line+1)
	}
	return "Open " + panelText(target.dest)
}

func (m *Model) targetDetailLines() []string {
	target, ok := m.focusedTarget()
	if !ok {
		return []string{"No match; Backspace edits, Esc cancels"}
	}
	width := m.width - 4
	if m.targets.panelHeight == 1 {
		width = m.width - len(target.label) - 3
	}
	text := targetText(target) + " @" + targetContext(target) + " | " + targetAction(target)
	return strings.Split(ansi.Hardwrap(text, max(1, width), true), "\n")
}

func (m *Model) targetDetailPageCount() int {
	rows := 2
	if m.targets.panelHeight == 1 {
		rows = 1
	}
	return max(1, (len(m.targetDetailLines())+rows-1)/rows)
}

func (m *Model) targetPanelRows() []string {
	candidates := m.targetCandidates()
	details := m.targetDetailLines()
	page := min(m.targets.detailPage, m.targetDetailPageCount()-1)
	if m.targets.panelHeight == 1 {
		line := "No match; Backspace edits"
		if target, ok := m.focusedTarget(); ok {
			line = targetHL + target.label + "\x1b[m " + details[page]
		}
		return []string{panelPad(line, m.width)}
	}
	inner := m.width - 2
	rows := []string{modalTop("Pick target", inner)}
	for i := range m.targetVisibleRows() {
		index := m.targets.top + i
		line := ""
		if index < len(candidates) {
			target := candidates[index]
			key := target.label
			if index == m.targets.focus {
				key = targetHL + key + "\x1b[m"
			}
			context := " @" + targetContext(target)
			labelWidth := max(1, (inner-len(target.label)-5)/2-ansi.StringWidth(context))
			label := panelPad(ansi.Truncate(targetText(target), labelWidth, "…"), labelWidth) + context
			action := targetDescription(target.dest)
			if target.kind == targetFootnote {
				action = "jump: " + panelText(target.footnote)
			}
			line = " " + key + " " + label + "  " + panelText(action)
			line = ansi.Truncate(line, inner-1, "…")
		} else if len(candidates) == 0 && i == 0 {
			line = " No matching hints"
		}
		rows = append(rows, modalRow(line, inner))
	}
	for i := range 2 {
		line := ""
		if index := page*2 + i; index < len(details) {
			line = " " + details[index]
		}
		rows = append(rows, modalRow(line, inner))
	}
	segment := ""
	if pages := m.targetDetailPageCount(); pages > 1 {
		segment = fmt.Sprintf(" detail %d/%d Ctrl+←/→ ", page+1, pages)
	}
	return append(rows, modalBottom(inner, segment))
}

func panelPad(line string, width int) string {
	line = ansi.Truncate(line, width, "…")
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

func (m *Model) targetStatus() string {
	selected := 0
	if _, ok := m.focusedTarget(); ok {
		selected = m.targets.focus + 1
	}
	prefix := strings.ToUpper(m.targets.prefix)
	if prefix == "" {
		prefix = "-"
	}
	count := fmt.Sprintf("%d/%d", selected, len(m.targetCandidates()))
	if len(m.targetCandidates()) != len(m.targets.targets) {
		count += fmt.Sprintf(" (%d)", len(m.targets.targets))
	}
	controls := " Tab/Enter/Esc"
	if m.targets.panelHeight == 1 && m.targetDetailPageCount() > 1 {
		// Compact panels keep the essential controls visible; detail paging
		// is also documented by the normal panel and help entry.
		controls = " Tab/Enter/Esc ^←/→"
	}
	available := max(1, m.width-ansi.StringWidth(count+controls)-1)
	return panelPad(ansi.Truncate("hint:"+prefix, available, "…")+" "+count+controls, m.width)
}

func (m *Model) applyTargetPanel(body string) string {
	lines := strings.Split(body, "\n")
	for i, row := range m.targetPanelRows() {
		if y := m.targetPanelY() + i; y >= 0 && y < len(lines) {
			lines[y] = row
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) targetPanelHit(x, y int) (hintTarget, bool) {
	if x < 0 || x >= m.width || y < m.targetPanelY() || y >= m.height-1 {
		return hintTarget{}, false
	}
	if m.targets.panelHeight == 1 {
		return m.focusedTarget()
	}
	row := y - m.targetPanelY() - 1
	if x == 0 || x == m.width-1 || row < 0 || row >= m.targetVisibleRows() {
		return hintTarget{}, false
	}
	index := m.targets.top + row
	candidates := m.targetCandidates()
	if index >= len(candidates) {
		return hintTarget{}, false
	}
	return candidates[index], true
}

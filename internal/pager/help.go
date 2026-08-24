package pager

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

// helpEntries is the single source of truth for keybindings: it drives both
// the overlay renderer and TestHelpTableCoversKeymap, which asserts every
// listed key is actually handled.
type helpEntry struct {
	key   string
	desc  string
	group string
}

var helpEntries = []helpEntry{
	{"j", "scroll down", "Navigation"},
	{"k", "scroll up", "Navigation"},
	{"d", "half page down", "Navigation"},
	{"u", "half page up", "Navigation"},
	{"ctrl+d", "half page down", "Navigation"},
	{"ctrl+u", "half page up", "Navigation"},
	{"f", "page down", "Navigation"},
	{"b", "page up", "Navigation"},
	{"space", "page down", "Navigation"},
	{"g", "go to top", "Navigation"},
	{"G", "go to bottom", "Navigation"},
	{"h", "pan left (nowrap)", "Navigation"},
	{"l", "pan right (nowrap)", "Navigation"},
	{"0", "reset pan (nowrap)", "Navigation"},
	{"w", "toggle wrap", "Modes"},
	{"s", "rendered/source view", "Modes"},
	{"T", "collapse tables", "Modes"},
	{"r", "toggle reader column", "Modes"},
	{"o", "outline (table of contents)", "Modes"},
	{"/", "search forward", "Search"},
	{"n", "next match", "Search"},
	{"N", "previous match", "Search"},
	{"R", "reload file", "Other"},
	{"c", "copy raw markdown", "Other"},
	{"e", "edit document in $EDITOR", "Other"},
	{"?", "help", "Other"},
	{"q", "quit", "Other"},
	{"esc", "clear search / quit", "Other"},
}

const helpTitle = "Keybindings"

var helpGroupStyle = lipgloss.NewStyle().Bold(true).MarginLeft(1)

func (m *Model) toggleHelp() {
	if !m.helpOpen && !m.helpFits() {
		return
	}
	m.helpOpen = !m.helpOpen
	m.helpTop = 0
}

func (m *Model) helpFits() bool { return m.width-4 >= 3 && m.height-4 >= 3 }

func (m *Model) helpVisible() int { return max(0, m.height-6) }

// Any key closes the help; j/k scroll when the listing overflows the modal.
func (m *Model) handleHelpKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "j", "down":
		m.scrollHelp(1)
	case "k", "up":
		m.scrollHelp(-1)
	default:
		m.helpOpen = false
	}
}

func (m *Model) scrollHelp(d int) {
	maxTop := max(0, len(formatHelp())-m.helpVisible())
	m.helpTop = min(max(0, m.helpTop+d), maxTop)
}

func formatHelp() []string {
	keyPad := 0
	for _, e := range helpEntries {
		if len(e.key) > keyPad {
			keyPad = len(e.key)
		}
	}
	var out []string
	cur := ""
	for _, e := range helpEntries {
		if e.group != cur {
			cur = e.group
			out = append(out, helpGroupStyle.Render(e.group))
		}
		out = append(out, strings.Repeat(" ", keyPad-len(e.key))+e.key+"  "+e.desc)
	}
	return out
}

func (m *Model) applyHelp(body string) string {
	rows := m.helpRows()
	if len(rows) == 0 {
		return body
	}
	mw := 0
	for _, r := range rows {
		mw = max(mw, ansi.StringWidth(r))
	}
	return m.overlay(body, max(0, (m.width-mw)/2), mw, rows)
}

func (m *Model) helpRows() []string {
	all := formatHelp()
	availW, availH := m.width-4, m.height-4
	if availW < 3 || availH < 3 {
		return nil
	}
	innerW := 0
	for _, l := range all {
		innerW = max(innerW, ansi.StringWidth(l))
	}
	innerW = min(innerW, availW-2)
	top := min(m.helpTop, max(0, len(all)-m.helpVisible()))
	end := min(len(all), top+m.helpVisible())
	box := make([]string, 0, end-top+2)
	// Palette index 6 (cyan): visible against rendered text without painting
	// backgrounds, keeping the starship no-fill invariant.
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	head := "╭" + strings.Repeat("─", innerW) + "╮"
	if fill := innerW - len(helpTitle) - 3; fill >= 1 {
		head = "╭─ " + helpTitle + " " + strings.Repeat("─", fill) + "╮"
	}
	box = append(box, border.Render(head))
	for _, l := range all[top:end] {
		l = ansi.Truncate(l, innerW, "")
		l += strings.Repeat(" ", max(0, innerW-ansi.StringWidth(l)))
		box = append(box, border.Render("│")+l+border.Render("│"))
	}
	foot := "╰" + strings.Repeat("─", innerW) + "╯"
	if len(all) > m.helpVisible() {
		hint := fmt.Sprintf(" %d of %d ", min(end, len(all)), len(all))
		if fill := innerW - ansi.StringWidth(hint); fill >= 1 {
			foot = "╰" + strings.Repeat("─", fill) + hint + "╯"
		}
	}
	return append(box, border.Render(foot))
}

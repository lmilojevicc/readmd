package pager

import (
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
	{"t", "table of contents", "Modes"},
	{"/", "search forward", "Search"},
	{"n", "next match", "Search"},
	{"N", "previous match", "Search"},
	{"r", "reload file", "Other"},
	{"?", "help", "Other"},
	{"q", "quit", "Other"},
	{"esc", "quit", "Other"},
}

const helpMaxWidth = 34

var helpGroupStyle = lipgloss.NewStyle().Bold(true).MarginLeft(1)

func (m *Model) toggleHelp() {
	if !m.helpOpen && m.helpPanelWidth() == 0 {
		return
	}
	m.helpOpen = !m.helpOpen
	m.helpTop = 0
}

// Any key closes the help; j/k scroll when the listing overflows the panel.
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
	maxTop := max(0, len(formatHelp())-m.vp.Height())
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
		out = append(out, "  "+e.key+strings.Repeat(" ", keyPad-len(e.key))+"  "+e.desc)
	}
	return out
}

func (m *Model) helpPanelWidth() int {
	return min(helpMaxWidth, max(0, m.width-4))
}

func (m *Model) applyHelp(body string) string {
	pw := m.helpPanelWidth()
	if pw == 0 {
		return body
	}
	return m.overlay(body, pw, m.helpRows(pw, len(strings.Split(body, "\n"))))
}

func (m *Model) helpRows(pw, height int) []string {
	rows := make([]string, height)
	if pw == 0 || height == 0 {
		return rows
	}
	all := formatHelp()
	top := min(m.helpTop, max(0, len(all)-height))
	for i := 0; i < height && top+i < len(all); i++ {
		rows[i] = ansi.Truncate(all[top+i], pw, "…")
	}
	return rows
}

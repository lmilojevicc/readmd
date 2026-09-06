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
	{"h", "pan left when wide", "Navigation"},
	{"l", "pan right when wide", "Navigation"},
	{"0", "reset horizontal pan", "Navigation"},
	{"p", "pick: hint/Tab/Enter/Esc; Ctrl+←/→ detail", "Navigation"},
	{"Backspace", "edit target / return footnote", "Navigation"},
	{"m", "toggle mouse capture", "Modes"},
	{"s", "rendered/source view", "Modes"},
	{"r", "toggle reader column", "Modes"},
	{"o", "outline: / filter; Enter jump; Esc/q/o cancel", "Modes"},
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

func (m *Model) toggleHelp() {
	if !m.helpOpen && !m.helpFits() {
		return
	}
	m.helpOpen = !m.helpOpen
	m.resetHelpView()
}

func (m *Model) resetHelpView() {
	m.helpTop = 0
	m.helpFilter = ""
	m.helpPrompt = false
}

func (m *Model) helpFits() bool { return m.width-4 >= 3 && m.height-4 >= 3 }

// helpVisible caps the listing at 20 content rows (22 with borders) so the
// modal stays a compact centered card on tall terminals; short terminals
// shrink it via the height-6 bound.
func (m *Model) helpVisible() int { return max(3, min(m.height-6, 20)) }

// While the filter prompt is open every printable key is literal input; j/k
// only scroll once the prompt is closed. Esc ladder: prompt/filter clears
// first, then the modal closes.
func (m *Model) handleHelpKey(msg tea.KeyMsg) {
	if m.helpPrompt {
		switch msg.String() {
		case "esc":
			m.helpFilter = ""
			m.helpPrompt = false
			m.helpTop = 0
		case "enter":
			m.helpPrompt = false
		case "backspace", "ctrl+h":
			if r := []rune(m.helpFilter); len(r) > 0 {
				m.helpFilter = string(r[:len(r)-1])
				m.helpTop = 0
			}
		default:
			if kp, ok := msg.(tea.KeyPressMsg); ok {
				if rs := []rune(kp.Text); len(rs) > 0 && printable(rs) {
					m.helpFilter += string(rs)
					m.helpTop = 0
				}
			}
		}
		return
	}
	switch msg.String() {
	case "j", "down":
		m.scrollHelp(1)
	case "k", "up":
		m.scrollHelp(-1)
	case "/":
		m.helpPrompt = true
	case "esc":
		if m.helpFilter != "" {
			m.helpFilter = ""
			m.helpTop = 0
			return
		}
		m.helpOpen = false
	default:
		m.helpOpen = false
		m.resetHelpView()
	}
}

func (m *Model) scrollHelp(d int) {
	maxTop := max(0, len(formatHelp(m.helpFilter))-m.helpVisible())
	m.helpTop = min(max(0, m.helpTop+d), maxTop)
}

func formatHelp(filter string) []string {
	f := strings.ToLower(filter)
	keyPad := 0
	for _, e := range helpEntries {
		if len(e.key) > keyPad {
			keyPad = len(e.key)
		}
	}
	var out []string
	cur := ""
	for _, e := range helpEntries {
		if f != "" && !helpEntryMatches(e, f) {
			continue
		}
		if e.group != cur {
			cur = e.group
			// Bold via attribute-on/off codes: a full reset here would kill
			// the modal background for the rest of the row.
			out = append(out, "\x1b[1m "+e.group+"\x1b[22m")
		}
		out = append(out, strings.Repeat(" ", keyPad-len(e.key))+e.key+"  "+e.desc)
	}
	return out
}

func helpEntryMatches(e helpEntry, f string) bool {
	return strings.Contains(strings.ToLower(e.key), f) ||
		strings.Contains(strings.ToLower(e.desc), f)
}

var modalBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

func modalTop(title string, innerW int) string {
	head := "╭" + strings.Repeat("─", innerW) + "╮"
	if fill := innerW - ansi.StringWidth(title) - 3; fill >= 1 {
		head = "╭─ " + title + " " + strings.Repeat("─", fill) + "╮"
	}
	return modalBorderStyle.Render(head)
}

func modalRow(content string, innerW int) string {
	content = ansi.Truncate(content, innerW, "")
	content += strings.Repeat(" ", max(0, innerW-ansi.StringWidth(content)))
	return modalBorderStyle.Render("│") + content + modalBorderStyle.Render("│")
}

func modalBottom(innerW int, segment string) string {
	foot := modalBorderStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")
	if segment == "" {
		return foot
	}
	segment = ansi.Truncate(segment, innerW-1, "…")
	if fill := innerW - ansi.StringWidth(segment); fill >= 1 {
		return modalBorderStyle.Render("╰"+strings.Repeat("─", fill)) + segment +
			modalBorderStyle.Render("╯")
	}
	return foot
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
	x := max(0, (m.width-mw)/2)
	y := max(0, (m.height-len(rows))/2)
	return m.overlay(body, x, y, mw, rows)
}

// helpRows builds the modal at a FIXED size: width derives from the
// unfiltered entry list (filtering never resizes the container) and the
// height is always helpVisible()+2 content rows, blank-filled when the
// filtered list runs short.
func (m *Model) helpRows() []string {
	all := formatHelp(m.helpFilter)
	full := formatHelp("")
	availW, availH := m.width-4, m.height-4
	if availW < 3 || availH < 3 {
		return nil
	}
	innerW := 0
	for _, l := range full {
		innerW = max(innerW, ansi.StringWidth(l))
	}
	innerW = min(max(innerW, min(56, availW-2)), availW-2)
	visible := m.helpVisible()
	top := min(m.helpTop, max(0, len(all)-visible))
	end := min(len(all), top+visible)
	box := make([]string, 0, visible+2)
	box = append(box, modalTop(helpTitle, innerW))
	for i := 0; i < visible; i++ {
		l := ""
		if top+i < end {
			l = all[top+i]
		}
		box = append(box, modalRow(l, innerW))
	}
	seg := ""
	if m.helpPrompt {
		seg = "/" + m.helpFilter
	} else if len(all) > visible {
		seg = fmt.Sprintf(" %d of %d ", min(end, len(all)), len(all))
	}
	return append(box, modalBottom(innerW, seg))
}

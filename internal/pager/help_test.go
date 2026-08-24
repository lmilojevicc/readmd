package pager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

func keyMsg(name string) tea.KeyPressMsg {
	switch name {
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		return tea.KeyPressMsg{Code: rune(name[0]), Text: name}
	}
}

func TestHelpOverlayFlow(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	m.vp.SetYOffset(7)
	y := m.vp.YOffset()

	press(m, "?")
	if !m.helpOpen {
		t.Fatal("? opens help overlay")
	}
	v := m.View().Content
	for _, want := range []string{"Navigation", "scroll down", "half page down"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help listing lacks %q:\n%s", want, v)
		}
	}
	assertModalShape(t, v)
	for range 100 {
		pressKey(m, keyMsg("j"))
	}
	// The last group header can sit just above the clamped window; reaching
	// the end of the listing is what this asserts.
	if !strings.Contains(m.View().Content, "clear search / quit") {
		t.Fatal("scrolling should reach the last help entry")
	}
	if !strings.Contains(m.View().Content, fmt.Sprintf(" %d of %d ", len(formatHelp()), len(formatHelp()))) {
		t.Fatal("scrolled modal should show the N of M hint")
	}
	press(m, "?")
	if m.helpOpen {
		t.Fatal("? closes help")
	}
	if m.vp.YOffset() != y {
		t.Fatal("help browsing must not move the viewport")
	}

	press(m, "x")
	if m.helpOpen || m.vp.YOffset() != y {
		t.Fatal("any key closes help at the exact position")
	}

	for _, closer := range []string{"esc", "q", "?"} {
		press(m, "?")
		if closer == "esc" || closer == "q" {
			pressKey(m, keyMsg(closer))
		} else {
			press(m, "?")
		}
		if m.helpOpen || m.vp.YOffset() != y {
			t.Fatalf("%q must close help without moving", closer)
		}
	}
}

// Regression: entry rows were padded to the inner panel but never truncated,
// so at widths 7-29 a long entry painted past the terminal edge.
func TestHelpRowsFitNarrowTerminal(t *testing.T) {
	longest := 0
	for _, l := range formatHelp() {
		longest = max(longest, ansi.StringWidth(l))
	}
	if longest <= 6 {
		t.Fatal("precondition: listing must exceed the width-12 inner panel")
	}
	m := newRenderedModel(t, srcDoc, 12, 20)
	press(m, "?")
	if !m.helpOpen {
		t.Fatal("? opens help at width 12")
	}
	for _, l := range strings.Split(m.View().Content, "\n") {
		if w := ansi.StringWidth(l); w > 12 {
			t.Fatalf("rendered line is %d cols (> 12): %q", w, ansi.Strip(l))
		}
	}
}

// assertModalShape checks the lazygit-style popup: a rounded bordered box with
// the title in its top border, horizontally centered over the body, colored
// only from the ANSI palette.
func assertModalShape(t *testing.T, v string) {
	t.Helper()
	lines := strings.Split(v, "\n")
	top := -1
	for i, l := range lines {
		if s := ansi.Strip(l); strings.Contains(s, "╭─ Keybindings") && strings.Contains(s, "╮") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatalf("modal top border with title missing:\n%s", v)
	}
	stripped := []rune(ansi.Strip(lines[top]))
	l, r := -1, -1
	for i, rr := range stripped {
		switch rr {
		case '╭':
			if l < 0 {
				l = i
			}
		case '╮':
			if r < 0 {
				r = i
			}
		}
	}
	if r < l {
		t.Fatalf("malformed modal top border:\n%s", string(stripped))
	}
	if indent := l; indent != (60-(r-l+1))/2 {
		t.Fatalf("modal not centered (indent %d):\n%s", indent, string(stripped))
	}
	bottom := -1
	for i := top + 1; i < len(lines); i++ {
		if strings.Contains(ansi.Strip(lines[i]), "╰") && strings.Contains(ansi.Strip(lines[i]), "╯") {
			bottom = i
			break
		}
	}
	if bottom < 0 || bottom-top < 3 {
		t.Fatalf("modal bottom border missing:\n%s", v)
	}
	if !strings.Contains(v, "\x1b[36m") {
		t.Fatal("modal border should use palette cyan (SGR 36)")
	}
	for _, bad := range []string{"\x1b[38;2;", "\x1b[48;2;", "\x1b[48;5;", "\x1b[48;"} {
		if strings.Contains(v, bad) {
			t.Errorf("modal contains painted-background/truecolor escape %q", bad)
		}
	}
}

func TestOverlayRefusesTinyPanels(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		w, h int
		open func(*Model) bool
	}{
		{"help zero width", "?", 4, 10, func(m *Model) bool { return m.helpOpen }},
		{"toc zero width", "o", 4, 10, func(m *Model) bool { return m.tocOpen }},
		{"help short height", "?", 40, 5, func(m *Model) bool { return m.helpOpen }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRenderedModel(t, srcDoc, tc.w, tc.h)
			y0 := m.vp.YOffset()
			press(m, tc.key)
			if tc.open(m) {
				t.Fatal("must refuse to open when the panel does not fit")
			}
			press(m, "j")
			if m.vp.YOffset() <= y0 {
				t.Fatal("keys must reach the pager after refusal")
			}
		})
	}
}

func TestHelpClosesWithoutSideEffect(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	press(m, "?")
	press(m, "w")
	if m.helpOpen || !m.wrapMode {
		t.Fatal("w under help closes it and must not toggle wrap")
	}
	press(m, "?")
	pressKey(m, keyMsg("/"))
	if m.helpOpen || m.search.active {
		t.Fatal("/ under help closes it and must not open search")
	}
	press(m, "?")
	press(m, "j")
	if !m.helpOpen {
		t.Fatal("j scrolls help instead of closing")
	}
}

func TestHelpScrollClamps(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	press(m, "?")
	all := len(formatHelp())
	maxTop := max(0, all-m.helpVisible())

	for range all + 5 {
		pressKey(m, keyMsg("j"))
	}
	if m.helpTop != maxTop {
		t.Fatalf("j clamp: top %d, want %d", m.helpTop, maxTop)
	}
	pressKey(m, keyMsg("k"))
	if m.helpTop != maxTop-1 {
		t.Fatalf("k from bottom: top %d, want %d", m.helpTop, maxTop-1)
	}
	for range all + 10 {
		pressKey(m, keyMsg("k"))
	}
	if m.helpTop != 0 {
		t.Fatalf("k clamp: top %d, want 0", m.helpTop)
	}
}

// TestHelpTableCoversKeymap is the drift guard: every helpEntries row must
// have a real effect when pressed in normal mode. A new table row fails here
// until its binding is implemented (and this switch extended); removing a
// binding while keeping the row fails the effect assertion.
func TestHelpTableCoversKeymap(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range helpEntries {
		if seen[e.key] {
			t.Errorf("duplicate help entry %q", e.key)
		}
		seen[e.key] = true
		t.Run(e.key, func(t *testing.T) {
			m := newRenderedModel(t, srcDoc, 60, 12)
			assertKeyEffect(t, m, e.key)
		})
	}
}

var needleDoc = strings.Repeat("needle here\n\n", 15) + strings.Repeat("tail filler\n\n", 15)

func assertKeyEffect(t *testing.T, m *Model, key string) {
	t.Helper()
	switch key {
	case "h", "l", "0":
		m = newRenderedModel(t, "| "+strings.Repeat("x", 200)+" |\n| - |\n", 60, 12)
	}
	switch key {
	case "j":
		y0 := m.vp.YOffset()
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() <= y0 {
			t.Fatal("j must scroll down")
		}
	case "k":
		m.vp.SetYOffset(1)
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() != 0 {
			t.Fatal("k must scroll up")
		}
	case "d", "ctrl+d":
		y0 := m.vp.YOffset()
		pressKey(m, keyMsg(key))
		if y := m.vp.YOffset(); y <= y0 {
			t.Fatal("half page down must move")
		}
	case "u", "ctrl+u":
		m.vp.GotoBottom()
		y0 := m.vp.YOffset()
		pressKey(m, keyMsg(key))
		if y := m.vp.YOffset(); y >= y0 {
			t.Fatal("half page up must move")
		}
	case "f", "space":
		y0 := m.vp.YOffset()
		pressKey(m, keyMsg(key))
		if y := m.vp.YOffset(); y <= y0 {
			t.Fatal("page down must move")
		}
	case "b":
		m.vp.GotoBottom()
		bottom := m.vp.YOffset()
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() >= bottom {
			t.Fatal("page up must move")
		}
	case "g":
		m.vp.SetYOffset(5)
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() != 0 {
			t.Fatal("g must go to top")
		}
	case "G":
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() <= 0 {
			t.Fatal("G must go to bottom")
		}
	case "h", "l", "0":
		settle(t, m, press(m, "w"))
		switch key {
		case "l":
			x0 := m.vp.XOffset()
			pressKey(m, keyMsg(key))
			if m.vp.XOffset() <= x0 {
				t.Fatal("l must pan right in nowrap")
			}
		case "h":
			pressKey(m, keyMsg("l"))
			x0 := m.vp.XOffset()
			pressKey(m, keyMsg(key))
			if m.vp.XOffset() >= x0 {
				t.Fatal("h must pan left in nowrap")
			}
		case "0":
			pressKey(m, keyMsg("l"))
			pressKey(m, keyMsg(key))
			if m.vp.XOffset() != 0 {
				t.Fatal("0 must reset pan")
			}
		}
	case "w":
		before := m.wrapMode
		cmd := press(m, key)
		if cmd == nil || m.wrapMode == before {
			t.Fatal("w must toggle wrap and re-render")
		}
		settle(t, m, cmd)
	case "s":
		press(m, key)
		if !m.srcView || m.wrapMode {
			t.Fatal("s must enter source nowrap")
		}
		settle(t, m, press(m, key))
		if m.srcView || !m.wrapMode {
			t.Fatal("s must restore rendered wrap")
		}
	case "T":
		before := m.collapsed
		cmd := press(m, key)
		if cmd == nil || m.collapsed == before {
			t.Fatal("T must toggle collapse and re-render")
		}
		settle(t, m, cmd)
	case "o":
		press(m, key)
		if !m.tocOpen {
			t.Fatal("o must open the outline")
		}
		press(m, "t")
		if !m.tocOpen {
			t.Fatal("lowercase t is unbound and must not disturb the outline")
		}
	case "/":
		press(m, key)
		if !m.search.active {
			t.Fatal("/ must open search prompt")
		}
	case "n":
		m = newRenderedModel(t, needleDoc, 60, 12)
		m.openSearch()
		typeQuery(m, "needle")
		pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		first := m.vp.YOffset()
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() <= first {
			t.Fatal("n must advance to next match")
		}
	case "N":
		m = newRenderedModel(t, needleDoc, 60, 12)
		m.openSearch()
		typeQuery(m, "needle")
		pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		ms := m.search.matches
		last := ms[len(ms)-1].line
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() != last {
			t.Fatalf("N from first match wraps to last: %d, want %d", m.vp.YOffset(), last)
		}
	case "r":
		src := m.source
		cmd := press(m, key)
		if cmd == nil || !m.reader || m.source != src {
			t.Fatal("r must toggle the reader column and re-render without reloading")
		}
		settle(t, m, cmd)
		if v := m.View().Content; !strings.Contains(v, "reader") {
			t.Fatalf("status bar must show the reader indicator:\n%s", v)
		}
		settle(t, m, press(m, key))
		if m.reader {
			t.Fatal("second r must leave reader mode")
		}
	case "R":
		m.path = "doc.md"
		m.readFile = func(string) ([]byte, error) { return []byte("# Fresh\n"), nil }
		settle(t, m, press(m, key))
		if m.source != "# Fresh\n" {
			t.Fatal("R must reload the file")
		}
	case "c":
		cmd := press(m, key)
		if cmd == nil || m.flash != "copied" {
			t.Fatalf("c must stage the OSC 52 copy and flash copied (cmd=%v flash=%q)", cmd, m.flash)
		}
		if msg := cmd(); msg == nil {
			t.Fatal("clipboard command must yield a message")
		}
	case "e":
		if cmd := press(m, key); cmd != nil || m.flash != "cannot edit stdin" {
			t.Fatalf("stdin documents cannot be edited (cmd=%v flash=%q)", cmd, m.flash)
		}
	case "?":
		press(m, key)
		if !m.helpOpen {
			t.Fatal("? must open help")
		}
		press(m, key)
		if m.helpOpen {
			t.Fatal("? must close help")
		}
	case "q", "esc":
		if cmd := pressKey(m, keyMsg(key)); cmd == nil {
			t.Fatalf("%s must quit", key)
		}
	default:
		t.Fatalf("help entry %q has no effect assertion", key)
	}
}

func TestQuestionOpensHelpNotSearch(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	press(m, "?")
	if !m.helpOpen || m.search.active {
		t.Fatal("? opens help, not search")
	}
	press(m, "esc")
	press(m, "/")
	if !m.search.active {
		t.Fatal("/ opens the search prompt")
	}
	if p := m.searchPrompt(); !strings.HasPrefix(p, "/") {
		t.Fatalf("prompt is forward-only: %q", p)
	}
}

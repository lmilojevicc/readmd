package pager

import (
	"strings"
	"testing"

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
	for range 100 {
		pressKey(m, keyMsg("j"))
	}
	if !strings.Contains(m.View().Content, "Other") {
		t.Fatal("scrolling should reveal the last group")
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

func TestOverlayRefusesZeroPanelWidth(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		open func(*Model) bool
	}{
		{"help", "?", func(m *Model) bool { return m.helpOpen }},
		{"toc", "t", func(m *Model) bool { return m.tocOpen }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRenderedModel(t, srcDoc, 4, 10)
			y0 := m.vp.YOffset()
			press(m, tc.key)
			if tc.open(m) {
				t.Fatal("must refuse to open when the panel width is 0")
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
	height := len(strings.Split(bodyOf(m), "\n"))
	all := len(formatHelp())
	maxTop := max(0, all-height)

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
	case "t":
		press(m, key)
		if !m.tocOpen {
			t.Fatal("t must open TOC")
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
		m.path = "doc.md"
		m.readFile = func(string) ([]byte, error) { return []byte("# Fresh\n"), nil }
		settle(t, m, press(m, key))
		if m.source != "# Fresh\n" {
			t.Fatal("r must reload the file")
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

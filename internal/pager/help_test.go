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
	case "Backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
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
	if !strings.Contains(m.View().Content, fmt.Sprintf(" %d of %d ", len(formatHelp("")), len(formatHelp("")))) {
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
	for _, l := range formatHelp("") {
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
	for _, l := range lines { // modal rows only; the status bar carries chips
		if strings.Contains(l, "│") && paletteBgSGRRe.MatchString(l) {
			t.Fatal("modal rows must stay transparent (no painted background)")
		}
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
	press(m, "x")
	if m.helpOpen {
		t.Fatal("an unbound key under help must only close it")
	}
	press(m, "?")
	pressKey(m, keyMsg("/"))
	if !m.helpOpen || !m.helpPrompt || m.search.active {
		t.Fatal("/ under help opens the filter prompt, never search")
	}
	typeQuery(m, "search")
	if m.search.active || m.search.query != "" {
		t.Fatal("typing into the modal filter must not touch search state")
	}
	if len(formatHelp(m.helpFilter)) >= len(formatHelp("")) {
		t.Fatal("filter must narrow the listing")
	}
	pressKey(m, keyMsg("esc"))
	pressKey(m, keyMsg("esc"))
	if m.helpOpen || m.search.active {
		t.Fatal("esc ladder: clear filter first, close modal second")
	}
	press(m, "?")
	topBefore := m.helpTop
	press(m, "j")
	if !m.helpOpen || m.helpTop == topBefore {
		t.Fatal("with the prompt closed, j scrolls the listing")
	}
}

func TestHelpScrollClamps(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	press(m, "?")
	all := len(formatHelp(""))
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

func TestHelpFilter(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	press(m, "?")
	total := len(formatHelp(""))

	pressKey(m, keyMsg("/"))
	if !m.helpPrompt {
		t.Fatal("/ opens the filter prompt inside the modal")
	}
	typeQuery(m, "rea")
	filtered := len(formatHelp(m.helpFilter))
	if filtered == 0 || filtered >= total {
		t.Fatalf("filter must narrow the listing: %d of %d", filtered, total)
	}
	if v := m.View().Content; strings.Contains(v, fmt.Sprintf(" %d of %d ", total, total)) {
		t.Fatal("footer must reflect the filtered set while filtering")
	}
	if v := m.View().Content; !strings.Contains(ansi.Strip(v), "/rea") {
		t.Fatal("prompt renders in the modal footer")
	}
	pressKey(m, keyMsg("j"))
	pressKey(m, keyMsg("k"))
	if !strings.HasSuffix(m.helpFilter, "jk") || m.helpTop != 0 {
		t.Fatalf("j/k are literal prompt input while filtering (q=%q top=%d)",
			m.helpFilter, m.helpTop)
	}
	for range 2 {
		pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.helpFilter != "rea" {
		t.Fatalf("backspace edits the filter: %q", m.helpFilter)
	}

	pressKey(m, keyMsg("esc"))
	if m.helpFilter != "" || m.helpPrompt || !m.helpOpen {
		t.Fatal("esc ladder: clear the filter first, keep the modal open")
	}
	pressKey(m, keyMsg("esc"))
	if m.helpOpen {
		t.Fatal("second esc closes the modal")
	}

	press(m, "?")
	pressKey(m, keyMsg("/"))
	typeQuery(m, "page")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.helpPrompt {
		t.Fatal("enter commits the filter and closes the prompt")
	}
	maxTop := max(0, len(formatHelp(m.helpFilter))-m.helpVisible())
	for range total + 5 {
		pressKey(m, keyMsg("j"))
	}
	if m.helpTop != maxTop {
		t.Fatalf("scroll clamps to the filtered set: top %d, want %d", m.helpTop, maxTop)
	}
	pressKey(m, keyMsg("x"))
	if m.helpOpen || m.helpFilter != "" || m.helpPrompt {
		t.Fatal("closing the modal resets filter state")
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
	case "t":
		m = newRenderedModel(t, "[site](https://example.com)\n", 60, 12)
	case "Backspace":
		m.vp.SetYOffset(4)
		m.locations = append(m.locations, documentLocation{y: 1})
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
		switch key {
		case "l":
			x0 := m.vp.XOffset()
			pressKey(m, keyMsg(key))
			if m.vp.XOffset() <= x0 {
				t.Fatal("l must pan right")
			}
		case "h":
			pressKey(m, keyMsg("l"))
			x0 := m.vp.XOffset()
			pressKey(m, keyMsg(key))
			if m.vp.XOffset() >= x0 {
				t.Fatal("h must pan left")
			}
		case "0":
			pressKey(m, keyMsg("l"))
			pressKey(m, keyMsg(key))
			if m.vp.XOffset() != 0 {
				t.Fatal("0 must reset pan")
			}
		}
	case "t":
		press(m, key)
		if !m.targets.active || len(m.targets.targets) != 1 {
			t.Fatal("t must open visible target hints")
		}
	case "Backspace":
		pressKey(m, keyMsg(key))
		if m.vp.YOffset() != 1 || len(m.locations) != 0 {
			t.Fatal("Backspace must restore the last internal location")
		}
	case "m":
		press(m, key)
		if m.mouse || m.flash != "mouse off" {
			t.Fatalf("m must disable mouse capture: mouse=%v flash=%q", m.mouse, m.flash)
		}
	case "s":
		press(m, key)
		if !m.srcView {
			t.Fatal("s must enter source view")
		}
		settle(t, m, press(m, key))
		if m.srcView {
			t.Fatal("s must restore rendered view")
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
		press(m, "x")
		if !m.tocOpen {
			t.Fatal("an unbound key must not disturb the outline")
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

func TestHelpFilterFooterTruncatesLongPrompt(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 100, 30)
	press(m, "?")
	m.helpPrompt = true
	m.helpFilter = strings.Repeat("x", 120)
	rows := m.helpRows()
	foot := rows[len(rows)-1]
	if w := ansi.StringWidth(foot); w != ansi.StringWidth(rows[0]) {
		t.Fatalf("footer must match the modal width: %d vs %d", w, ansi.StringWidth(rows[0]))
	}
	if !strings.Contains(foot, "…") {
		t.Fatalf("long filter must truncate into the footer: %q", foot)
	}
}

func TestHelpModalFixedSizeAndCentered(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 100, 30)
	press(m, "?")
	full := m.helpRows()
	if len(full) == 0 {
		t.Fatal("precondition: modal rows rendered")
	}
	fullW := ansi.StringWidth(full[0])

	press(m, "/")
	typeQuery(m, "half page") // narrows the list sharply
	narrow := m.helpRows()
	if len(narrow) != len(full) || ansi.StringWidth(narrow[0]) != fullW {
		t.Fatalf("filtering must not resize the container: %dx%d -> %dx%d",
			fullW, len(full), ansi.StringWidth(narrow[0]), len(narrow))
	}

	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // clear filter
	body := m.View().Content
	lines := strings.Split(body, "\n")
	first := -1
	for i, l := range lines {
		if strings.Contains(l, "Keybindings") {
			first = i
			break
		}
	}
	if first <= 0 {
		t.Fatalf("modal must be vertically centered, title at line %d", first)
	}

	for _, l := range full {
		if !strings.Contains(l, "Navigation") {
			continue
		}
		if strings.Contains(l, "\x1b[100m") {
			t.Fatalf("modal rows must stay transparent: %q", l)
		}
		if !strings.Contains(l, "\x1b[1m") || !strings.Contains(l, "\x1b[22m") {
			t.Fatalf("header bold must use attribute codes, not a full reset: %q", l)
		}
		return
	}
	t.Fatal("no Navigation header in full listing")
}

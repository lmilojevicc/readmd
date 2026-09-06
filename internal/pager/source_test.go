package pager

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

var srcDoc = "# Alpha\n\nintro text here\n\n## Beta\n\n" +
	strings.Repeat("beta body line\n", 12) + "\n## Gamma\n\n" + strings.Repeat("gamma tail line\n", 12)

func srcLineOf(src, needle string) int {
	return strings.Count(src[:strings.Index(src, needle)], "\n")
}

func bodyOf(m *Model) string {
	lines := strings.Split(m.View().Content, "\n")
	return strings.Join(lines[:max(0, len(lines)-1)], "\n")
}

func TestHeadingSourceLines(t *testing.T) {
	src := "# Top\n\ntitle\n======\n\n##\n\n## Deep\n\nbody\n"
	for _, tc := range []struct {
		text string
		want int
	}{
		{"Top", 0},
		{"title", 2},
		{"Deep", 7},
	} {
		var got = -1
		for _, h := range extractHeadings(src) {
			if h.text == tc.text {
				got = h.srcLine
			}
		}
		if got != tc.want {
			t.Errorf("%s: srcLine %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestSourceToggle(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 10)
	if cmd := press(m, "s"); cmd != nil {
		t.Fatal("entering source is synchronous")
	}
	if !m.srcView {
		t.Fatal("source view must be active")
	}
	body := bodyOf(m)
	if !strings.Contains(body, "# Alpha") || !strings.Contains(body, "## Beta") {
		t.Fatalf("raw markdown markers missing:\n%s", ansi.Strip(body))
	}
	if ansi.Strip(body) != body {
		t.Fatal("source view must be plain default-fg")
	}
	if v := m.View().Content; !strings.Contains(v, "source") {
		t.Fatalf("status bar lacks source indicator:\n%s", v)
	}
	settle(t, m, press(m, "s"))
	if m.srcView {
		t.Fatal("exit must restore rendered view")
	}
	if v := m.View().Content; !strings.Contains(v, "render") {
		t.Fatalf("status bar lacks render indicator:\n%s", v)
	}
}

func TestSourceToggleAnchors(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 10)
	betaR := m.heads[1].line
	betaS := srcLineOf(srcDoc, "## Beta")

	m.vp.SetYOffset(betaR + 3)
	press(m, "s")
	if got := m.vp.YOffset(); got != betaS {
		t.Fatalf("rendered→source: at %d, want Beta source line %d", got, betaS)
	}
	settle(t, m, press(m, "s"))
	if got := m.vp.YOffset(); got != betaR {
		t.Fatalf("source→rendered: at %d, want Beta rendered line %d", got, betaR)
	}

	press(m, "s")
	m.vp.SetYOffset(betaS + 2)
	settle(t, m, press(m, "s"))
	if got := m.vp.YOffset(); got != betaR {
		t.Fatalf("source→rendered from below Beta: at %d, want %d", got, betaR)
	}
}

func TestSourceAnchorFractionFallback(t *testing.T) {
	doc := strings.Repeat("filler line\n\n", 60)
	m := newRenderedModel(t, doc, 60, 10)
	m.vp.SetYOffset(50)
	press(m, "s")
	total := len(m.stripped)
	if total == 0 {
		t.Fatal("no source lines")
	}
	if y := m.vp.YOffset(); y <= 25 || y >= total {
		t.Fatalf("fraction fallback out of range: %d (total %d)", y, total)
	}
}

func TestSourceTOCJumpAndSearch(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 10)
	press(m, "s")

	press(m, "o")
	if !m.tocOpen {
		t.Fatal("TOC opens in source mode")
	}
	press(m, "j")
	press(m, "j")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	wantGamma := srcLineOf(srcDoc, "## Gamma")
	if m.vp.YOffset() != wantGamma {
		t.Fatalf("TOC jump in source: at %d, want %d", m.vp.YOffset(), wantGamma)
	}

	press(m, "/")
	typeQuery(m, "gamma")
	if m.search.count != 13 {
		t.Fatalf("search counts raw lines: %d, want 13", m.search.count)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.vp.YOffset() != wantGamma {
		t.Fatalf("commit lands on first raw match: at %d, want %d", m.vp.YOffset(), wantGamma)
	}
	press(m, "n")
	if m.vp.YOffset() <= wantGamma {
		t.Fatal("n moves to next raw match")
	}
	press(m, "N")
	if m.vp.YOffset() != wantGamma {
		t.Fatalf("N returns: at %d, want %d", m.vp.YOffset(), wantGamma)
	}
}

func TestSourceResizeKeepsRawContent(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 10)
	press(m, "s")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	*m = *nm.(*Model)
	if cmd != nil {
		t.Fatal("resize in source must not re-render glamour")
	}
	if !m.srcView || !strings.Contains(bodyOf(m), "# Alpha") {
		t.Fatal("resize must keep raw source content")
	}
}

func TestSourceResizeClampUsesFreshWidest(t *testing.T) {
	m := newRenderedModel(t, "| "+strings.Repeat("x", 100)+" |\n", 60, 10)
	press(m, "s")
	for range 5 {
		press(m, "l")
	}
	if m.vp.XOffset() == 0 {
		t.Fatal("precondition: panning did not move x")
	}
	m.path = "doc.md"
	m.readFile = func(string) ([]byte, error) { return []byte("short\n"), nil }
	settle(t, m, press(m, "R"))
	if x := m.vp.XOffset(); x != 0 {
		t.Fatalf("reload must re-clamp against the new widest line: %d", x)
	}
	for range 5 {
		press(m, "l")
	}
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	*m = *nm.(*Model)
	if cmd != nil {
		t.Fatal("resize in source must not re-render glamour")
	}
	if x := m.vp.XOffset(); x != 0 {
		t.Fatalf("resize must clamp against the cached widest line: %d", x)
	}
}

func TestSourceReloadAppliesInPlace(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 10)
	press(m, "s")
	y := m.vp.YOffset()
	m.path = "doc.md"
	m.readFile = func(string) ([]byte, error) { return []byte("# Replaced\n\nnew body\n"), nil }
	settle(t, m, press(m, "R"))
	if !m.srcView || !strings.Contains(bodyOf(m), "# Replaced") {
		t.Fatal("reload in source mode must refresh raw content")
	}
	if m.vp.YOffset() > y {
		t.Fatal("reload should keep reading position")
	}
}

func TestSourceUnboundKeys(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 10)
	press(m, "s")
	if cmd := press(m, "w"); cmd != nil {
		t.Fatal("w must remain unbound in source mode")
	}
	if cmd := press(m, "T"); cmd != nil {
		t.Fatal("T must remain unbound in source mode")
	}
}

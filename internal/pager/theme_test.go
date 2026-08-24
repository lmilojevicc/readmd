package pager

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbletea/v2"

	"charm.land/glamour/v2/styles"
)

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

func hasColorSGR(s string) bool {
	for _, m := range sgrRe.FindAllStringSubmatch(s, -1) {
		if m[1] == "" {
			continue
		}
		for _, p := range strings.Split(m[1], ";") {
			n, _ := strconv.Atoi(p)
			switch {
			case n >= 30 && n <= 37, n >= 90 && n <= 97,
				n >= 40 && n <= 47, n >= 100 && n <= 107,
				n == 38 || n == 48:
				return true
			}
		}
	}
	return false
}

var paletteSGRRe = regexp.MustCompile(`\x1b\[(3[0-7]|9[0-7])m`)

// paletteBgSGRRe covers palette-index background SGRs. Amended starship
// invariant: painted backgrounds are sanctioned ONLY for the search match
// highlights (43 match / 45 current match) and the status-bar brand chip
// (45); document content — headings, tables, code, alerts — stays bg-free,
// and truecolor/256-index backgrounds are banned everywhere.
var paletteBgSGRRe = regexp.MustCompile(`\x1b\[(4[0-7]|10[0-7])m`)

const themeSample = "# Heading\n\n| A | B |\n| - | - |\n| x | y |\n\n```go\nx := 1\n```\n\n> [!CAUTION]\n> danger ahead\n"

func TestPaletteTerminalInvariant(t *testing.T) {
	out, _, _, err := renderDoc(imgCtx{}, themeSample, 40, true, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	if !paletteSGRRe.MatchString(out) {
		t.Error("palette style: expected ANSI palette SGRs (30-37/90-97)")
	}
	for _, bad := range []string{"\x1b[38;2;", "\x1b[48;2;", "\x1b[48;5;"} {
		if strings.Contains(out, bad) {
			t.Errorf("palette style: output contains %q (truecolor/background escape)", bad)
		}
	}

	var styled string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(l), "Heading") {
			ms := findMatches([]string{ansi.Strip(l)}, "Heading")
			styled = highlightLine(l, []span{{ms[0].start, ms[0].end}}, -1)
			break
		}
	}
	if !strings.Contains(styled, matchHL) || !paletteBgSGRRe.MatchString(styled) {
		t.Errorf("search highlight: expected palette bg SGRs, got %q", styled)
	}
	for _, bad := range []string{"\x1b[38;2;", "\x1b[48;2;", "\x1b[48;5;"} {
		if strings.Contains(styled, bad) {
			t.Errorf("search highlight: output contains %q", bad)
		}
	}
}

func TestThemeColors(t *testing.T) {
	paletteOut, _, _, err := renderDoc(imgCtx{}, themeSample, 40, true, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		style     string
		wantColor bool
	}{
		{styles.DarkStyle, true},
		{styles.LightStyle, true},
		{styles.NoTTYStyle, false},
	} {
		t.Run(tc.style, func(t *testing.T) {
			out, _, _, err := renderDoc(imgCtx{}, themeSample, 40, true, tc.style)
			if err != nil {
				t.Fatal(err)
			}
			if got := hasColorSGR(out); got != tc.wantColor {
				t.Errorf("style %s: color SGR = %v, want %v", tc.style, got, tc.wantColor)
			}
			if tc.wantColor && (out == paletteOut || !hasColorSGR(out)) {
				t.Errorf("style %s: expected hex-styled output differing from palette style", tc.style)
			}
		})
	}
}

func TestResolveStyleDefault(t *testing.T) {
	def, err := resolveStyle("")
	if err != nil {
		t.Fatal(err)
	}
	auto, err := resolveStyle("auto")
	if err != nil {
		t.Fatal(err)
	}
	if def != paletteStyleName || auto != paletteStyleName {
		t.Errorf("default/auto = %q/%q, want both %q", def, auto, paletteStyleName)
	}
}

func TestResolveStyleExplicit(t *testing.T) {
	for _, name := range []string{"dark", "light", "notty", "auto"} {
		if _, err := resolveStyle(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"bogus", "Dark", "notty "} {
		if _, err := resolveStyle(name); err == nil {
			t.Errorf("%q: expected error", name)
		}
	}
}

func TestSetStyle(t *testing.T) {
	m := New("x", "t")
	if err := m.SetStyle("dark"); err != nil {
		t.Fatal(err)
	}
	if m.style != styles.DarkStyle {
		t.Errorf("style = %q, want %q", m.style, styles.DarkStyle)
	}
	if err := m.SetStyle("nope"); err == nil {
		t.Error("expected error for invalid style")
	}
	if m.style != styles.DarkStyle {
		t.Errorf("failed SetStyle must keep previous style, got %q", m.style)
	}
	if err := m.SetStyle(""); err != nil {
		t.Fatal(err)
	}
	if m.style != paletteStyleName {
		t.Errorf("empty style must default to palette, got %q", m.style)
	}
}

func TestModelStyleFlowsToRender(t *testing.T) {
	m := New(themeSample, "t")
	m.renderW = 80
	if err := m.SetStyle("auto"); err != nil {
		t.Fatal(err)
	}
	rm, ok := m.requestRender()().(renderedMsg)
	if !ok || rm.err != nil {
		t.Fatalf("render failed: %v", rm.err)
	}
	if !hasColorSGR(rm.content) || !paletteSGRRe.MatchString(rm.content) {
		t.Error("auto: expected palette-colored output")
	}
	for _, bad := range []string{"\x1b[38;2;", "\x1b[48;2;", "\x1b[48;5;"} {
		if strings.Contains(rm.content, bad) {
			t.Errorf("auto: output contains %q", bad)
		}
	}
	if err := m.SetStyle("notty"); err != nil {
		t.Fatal(err)
	}
	m.rendering = false
	rm, ok = m.requestRender()().(renderedMsg)
	if !ok || rm.err != nil {
		t.Fatalf("render failed: %v", rm.err)
	}
	if hasColorSGR(rm.content) {
		t.Error("notty: expected attribute-only output")
	}
}

// TestStatusBarBrandChip pins the amended starship invariant: the brand chip's
// magenta background is the only painted background outside search highlights,
// and the rest of the status bar (and the document body) stays bg-free.
func TestStatusBarBrandChip(t *testing.T) {
	m := New(themeSample, "doc.md")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	*m = *nm.(*Model)
	settle(t, m, cmd)
	v := m.View().Content
	split := strings.LastIndexByte(v, '\n')
	body, bar := v[:split], v[split+1:]
	if !strings.HasPrefix(bar, brandChip) {
		t.Fatalf("status bar must start with the readmd chip:\n%q", bar)
	}
	if !strings.Contains(bar, "\x1b[45m\x1b[30m") {
		t.Fatalf("chip must paint palette magenta bg with black fg:\n%q", bar)
	}
	if n := strings.Count(bar, "\x1b[45m"); n != 1 {
		t.Fatalf("bar must paint exactly one magenta bg (the chip), got %d:\n%q", n, bar)
	}
	if paletteBgSGRRe.MatchString(bar[len(brandChip):]) {
		t.Fatalf("no painted background allowed in the bar beyond the chip:\n%q", bar)
	}
	if paletteBgSGRRe.MatchString(body) {
		t.Error("document body must stay background-free without active search")
	}
	for _, s := range []string{body, bar} {
		for _, bad := range []string{"\x1b[38;2;", "\x1b[48;2;", "\x1b[48;5;"} {
			if strings.Contains(s, bad) {
				t.Errorf("status view contains truecolor/256-index escape %q", bad)
			}
		}
	}
}

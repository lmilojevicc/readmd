package pager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

var readerProse = "# Reader Doc\n\n" + strings.Repeat("lorem ipsum dolor sit amet consectetur adipiscing\n\n", 8)

func minLeading(lines []string) int {
	m := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := 0
		for n < len(l) && l[n] == ' ' {
			n++
		}
		if m < 0 || n < m {
			m = n
		}
	}
	return m
}

func TestReaderGeom(t *testing.T) {
	// Values follow the normative formulas eff=min(80, vw-2),
	// margin=(vw-eff)/2 (symmetric block centering).
	for _, tc := range []struct {
		vw         int
		on         bool
		wantW      int
		wantMargin int
	}{
		{100, true, 80, 10},
		{84, true, 80, 2},
		{60, true, 58, 1},
		{20, true, 18, 1},
		{2, true, 1, 0},
		{100, false, 100, 0},
	} {
		t.Run(fmt.Sprintf("vw%d/on=%v", tc.vw, tc.on), func(t *testing.T) {
			w, margin := readerGeom(tc.vw, tc.on)
			if w != tc.wantW || margin != tc.wantMargin {
				t.Fatalf("readerGeom(%d,%v) = (%d,%d), want (%d,%d)",
					tc.vw, tc.on, w, margin, tc.wantW, tc.wantMargin)
			}
		})
	}
}

func TestReaderColumnLayout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		vw         int
		wantW      int
		wantMargin int
	}{
		{"wide", 100, 80, 10},
		{"just above cap", 84, 80, 2},
		{"below cap", 60, 58, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRenderedModel(t, readerProse, tc.vw, 24)
			baseLead := minLeading(m.stripped)
			settle(t, m, press(m, "r"))
			if lead := minLeading(m.stripped); lead != baseLead+tc.wantMargin {
				t.Fatalf("min leading %d, want base %d + margin %d", lead, baseLead, tc.wantMargin)
			}
			if widest := widestLine(m.stripped); widest > tc.wantMargin+tc.wantW {
				t.Fatalf("wrapped body width %d exceeds margin %d + column %d",
					widest, tc.wantMargin, tc.wantW)
			}
			if v := m.View().Content; !strings.Contains(v, "reader") {
				t.Fatalf("status bar lacks reader indicator:\n%s", v)
			}
		})
	}
}

func TestReaderCodeOverflowKeepsMargin(t *testing.T) {
	doc := "```go\n" + strings.Repeat("z", 120) + "\n```\n"
	m := newRenderedModel(t, doc, 100, 20)
	settle(t, m, press(m, "r"))
	pad := strings.Repeat(" ", 10)
	found := false
	for _, l := range m.stripped {
		if !strings.Contains(l, strings.Repeat("z", 120)) {
			continue
		}
		found = true
		if !strings.HasPrefix(l, pad) {
			t.Fatalf("overflowing code line lost the reader margin: %q", l)
		}
		if w := ansi.StringWidth(l); w <= 80 {
			t.Fatalf("code must overflow the reader column, width %d", w)
		}
	}
	if !found {
		t.Fatal("code line missing from reader render")
	}
}

func TestReaderNowrapFixedColumn(t *testing.T) {
	doc := "# Wide\n\n" + strings.Repeat("word ", 60) + "\n"
	m := newRenderedModel(t, doc, 100, 24)
	settle(t, m, press(m, "w")) // start nowrap
	settle(t, m, press(m, "r"))

	if lead := minLeading(m.stripped); lead != 12 { // 10 margin + glamour's 2 padding
		t.Fatalf("reader column must keep its margin in nowrap: lead %d", lead)
	}
	if widest := widestLine(m.stripped); widest > 90 {
		t.Fatalf("reader must render a fixed wrapped column in nowrap, widest %d", widest)
	}
	if v := m.View().Content; !strings.Contains(v, "reader") || strings.Contains(v, "nowrap") {
		t.Fatal("reader mode must report wrapped rendering in the status bar")
	}

	if cmd := press(m, "w"); cmd != nil || m.wrapMode {
		t.Fatalf("w must be frozen in reader mode (cmd=%v wrapMode=%v)", cmd, m.wrapMode)
	}
	if widest := widestLine(m.stripped); widest > 90 {
		t.Fatalf("w must not widen the reader column, widest %d", widest)
	}

	settle(t, m, press(m, "r")) // exit reader: nowrap resumes
	if widest := widestLine(m.stripped); widest <= 90 {
		t.Fatalf("exiting reader must restore nowrap, widest %d", widest)
	}
	step := max(8, m.width/10)
	press(m, "l")
	if off := m.vp.XOffset(); off != step {
		t.Fatalf("l must pan after exiting reader: offset %d, want %d", off, step)
	}
}

func TestReaderToggleVsReload(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 100, 20)
	src := m.source

	cmd := press(m, "r")
	if cmd == nil || !m.reader || m.source != src {
		t.Fatalf("r must toggle reader without reloading (cmd=%v reader=%v src-changed=%v)",
			cmd, m.reader, m.source != src)
	}
	settle(t, m, cmd)

	if cmd := press(m, "R"); cmd != nil {
		t.Fatal("stdin document has nothing to reload")
	}

	m.path = "doc.md"
	m.readFile = func(string) ([]byte, error) { return []byte("# Fresh\n"), nil }
	settle(t, m, press(m, "R"))
	if m.source != "# Fresh\n" {
		t.Fatalf("R must reload the file, source = %q", m.source)
	}
	if !m.reader {
		t.Fatal("reload must not disturb reader mode")
	}

	press(m, "s")
	if cmd := press(m, "r"); cmd != nil || !m.reader {
		t.Fatalf("r must be frozen in source view (cmd=%v reader=%v)", cmd, m.reader)
	}
}

func TestReaderOverlaysCompose(t *testing.T) {
	const vw = 100
	m := newRenderedModel(t, tocDoc, vw, 24)
	settle(t, m, press(m, "r"))

	press(m, "o")
	lines := strings.Split(bodyOf(m), "\n")
	if s := ansi.Strip(lines[0]); !strings.HasPrefix(s, "Alpha") {
		t.Fatalf("toc panel must sit at column 0, got %q", s)
	}
	last := ansi.Strip(lines[len(lines)-1])
	if strings.TrimSpace(last) != "" && !strings.HasPrefix(last, strings.Repeat(" ", 10)) {
		t.Fatalf("body below toc entries must keep the reader margin: %q", last)
	}
	pressKey(m, keyMsg("esc"))

	press(m, "?")
	lines = strings.Split(bodyOf(m), "\n")
	top := -1
	for i, l := range lines {
		if s := ansi.Strip(l); strings.Contains(s, "╭─ Keybindings") && strings.Contains(s, "╮") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatal("help modal missing under reader mode")
	}
	rs := []rune(ansi.Strip(lines[top]))
	l, r := -1, -1
	for i, ch := range rs {
		switch ch {
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
	if want := (vw - (r - l + 1)) / 2; l != want {
		t.Fatalf("help modal indent %d, want %d (centered on terminal, not column)", l, want)
	}
	for _, bad := range []string{"\x1b[48;5;", "\x1b[48;2;"} {
		if strings.Contains(m.View().Content, bad) {
			t.Errorf("modal must stay palette-clean, found %q", bad)
		}
	}
	pressKey(m, keyMsg("esc"))

	m.openSearch()
	typeQuery(m, "beta")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	hit := false
	for _, l := range strings.Split(bodyOf(m), "\n") {
		if !strings.Contains(l, curHL) {
			continue
		}
		hit = true
		if s := ansi.Strip(l); !strings.HasPrefix(s, strings.Repeat(" ", 10)) {
			t.Fatalf("highlighted line must keep the reader margin: %q", s)
		}
	}
	if !hit {
		t.Fatal("current match must be highlighted under reader mode")
	}
}

func TestReaderAnchorPreserved(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 100, 10)
	beta := m.heads[1].line
	m.vp.SetYOffset(beta + 3)
	settle(t, m, press(m, "r"))
	want := m.heads[1].line
	if got := m.vp.YOffset(); got != want {
		t.Fatalf("after toggle: top %d, want anchored at Beta %d", got, want)
	}
	if m.vp.YOffset() > want || want >= m.vp.YOffset()+m.vp.Height() {
		t.Fatal("anchored heading must stay visible")
	}
}

func TestReaderStormCoalesces(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 100, 24)
	cmd1 := press(m, "r")
	if cmd1 == nil {
		t.Fatal("first toggle must start a render")
	}
	cmd2 := press(m, "r")
	if cmd2 != nil {
		t.Fatal("toggle during in-flight render must coalesce")
	}
	if m.reader {
		t.Fatal("flag flips synchronously even when the render coalesces")
	}
	settle(t, m, cmd1)
	if m.rendering {
		t.Fatal("retry did not drain")
	}
	if lead := minLeading(m.stripped); lead != 2 {
		t.Fatalf("storm must settle back to full width, lead %d", lead)
	}
	if v := m.View().Content; strings.Contains(v, "reader") {
		t.Fatalf("status bar must drop the reader indicator:\n%s", v)
	}
}

func TestReaderMarginIsSpacesNotSGR(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 100, 24)
	settle(t, m, press(m, "r"))
	pad := strings.Repeat(" ", 10)
	for i, l := range m.base {
		if esc := strings.IndexByte(l, '\x1b'); esc >= 0 && esc < 10 {
			t.Fatalf("line %d: SGR inside the margin region: %q", i, l)
		}
		if !strings.HasPrefix(l, pad) {
			t.Fatalf("line %d lacks the uniform space margin: %q", i, l)
		}
	}
}

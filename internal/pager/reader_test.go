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
	// Values follow the normative formulas eff=min(120, vw-2),
	// margin=(vw-eff)/2 (symmetric block centering).
	for _, tc := range []struct {
		vw         int
		on         bool
		wantW      int
		wantMargin int
	}{
		{140, true, 120, 10},
		{124, true, 120, 2},
		{100, true, 98, 1},
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
		{"wide", 140, 120, 10},
		{"just above cap", 124, 120, 2},
		{"below cap", 100, 98, 1},
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
	doc := "```go\n" + strings.Repeat("z", 160) + "\n```\n"
	m := newRenderedModel(t, doc, 140, 20)
	settle(t, m, press(m, "r"))
	pad := strings.Repeat(" ", 10)
	found := false
	for _, l := range m.stripped {
		if !strings.Contains(l, strings.Repeat("z", 160)) {
			continue
		}
		found = true
		if !strings.HasPrefix(l, pad) {
			t.Fatalf("overflowing code line lost the reader margin: %q", l)
		}
		if w := ansi.StringWidth(l); w <= 120 {
			t.Fatalf("code must overflow the reader column, width %d", w)
		}
	}
	if !found {
		t.Fatal("code line missing from reader render")
	}
}

// Search highlights live in the stored (unpadded) line content in the nowrap
// reader frame; the margin prefix shifts columns only visually.
func TestReaderNowrapHighlightAlignment(t *testing.T) {
	const vw = 140
	doc := "# Find\n\nneedle here\n\n" + strings.Repeat("tail filler\n\n", 8) +
		strings.Repeat("z", 200) + "\n"
	m := newRenderedModel(t, doc, vw, 24)
	settle(t, m, press(m, "w"))
	settle(t, m, press(m, "r"))

	m.openSearch()
	typeQuery(m, "needle")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.vp.XOffset() != 0 {
		t.Fatalf("precondition: xOffset %d", m.vp.XOffset())
	}
	found := false
	for _, l := range strings.Split(bodyOf(m), "\n") {
		if !strings.Contains(l, curHL) {
			continue
		}
		found = true
		s := ansi.Strip(l)
		if !strings.HasPrefix(s, strings.Repeat(" ", 10)) {
			t.Fatalf("highlighted line must carry the display margin: %q", s)
		}
		if idx := strings.Index(s, "needle"); idx < 0 || idx >= 10+120 {
			t.Fatalf("match must sit inside the pinned column: col %d", idx)
		}
	}
	if !found {
		t.Fatal("current match must be highlighted in reader+nowrap")
	}

	press(m, "l") // pan; highlights stay embedded, frame stays put
	step := max(8, m.width/10)
	if off := m.vp.XOffset(); off != step {
		t.Fatalf("pan precondition: offset %d, want %d", off, step)
	}
	stillHL := false
	for _, l := range strings.Split(m.vp.GetContent(), "\n") {
		if strings.Contains(l, curHL) {
			stillHL = true
		}
	}
	if !stillHL {
		t.Fatal("highlights stay embedded in the cached lines after panning")
	}
	assertMarginFramed(t, m, strings.Repeat(" ", 10), 130)
}

// Reader+nowrap coordinate system (System B): stored lines stay unpadded at
// their unwrapped content width; the viewport is pinned to the reader column
// and the centered margin is a display-only prefix, so content pans within the
// fixed frame.
func TestReaderNowrapViewportFrame(t *testing.T) {
	const vw = 140
	doc := "# Wide\n\n" + strings.Repeat("word ", 60) + "\n"
	m := newRenderedModel(t, doc, vw, 24)
	settle(t, m, press(m, "w")) // start nowrap
	settle(t, m, press(m, "r"))

	if !m.reader || m.wrapMode {
		t.Fatalf("precondition: reader=%v wrap=%v", m.reader, m.wrapMode)
	}
	if lead := minLeading(m.stripped); lead != 2 { // glamour's own margin only
		t.Fatalf("stored lines must use unwrapped content width without padding: lead %d", lead)
	}
	if widest := widestLine(m.stripped); widest <= 120 {
		t.Fatalf("precondition: content overflows the column, widest %d", widest)
	}
	if got := m.vp.Width(); got != 120 {
		t.Fatalf("viewport must pin to the reader column: width %d, want 120", got)
	}
	if v := m.View().Content; !strings.Contains(v, "nowrap") || !strings.Contains(v, "reader") {
		t.Fatalf("status bar must report real wrap state and reader:\n%s", v)
	}
	pad := strings.Repeat(" ", 10) // margin at vw=140: (140-120)/2
	assertMarginFramed(t, m, pad, 130)

	step := max(8, m.width/10)
	press(m, "l")
	if off := m.vp.XOffset(); off != step {
		t.Fatalf("l pans within the pinned column: offset %d, want %d", off, step)
	}
	if got := m.vp.Width(); got != 120 {
		t.Fatalf("frame width must not stretch on pan: %d", got)
	}
	assertMarginFramed(t, m, pad, 130)
	press(m, "h")
	if off := m.vp.XOffset(); off != 0 {
		t.Fatalf("h pans back to column 0: offset %d", off)
	}

	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	*m = *nm.(*Model)
	settle(t, m, cmd)
	if got := m.vp.Width(); got != 120 {
		t.Fatalf("resize must re-pin the frame: vp width %d, want 120", got)
	}
	pad = strings.Repeat(" ", (160-120)/2)
	assertMarginFramed(t, m, pad, 140)

	settle(t, m, press(m, "r")) // exit reader
	if got := m.vp.Width(); got != 160 {
		t.Fatalf("exit must restore the full-width viewport: %d", got)
	}
	if strings.Contains(m.View().Content, "reader") {
		t.Fatal("status bar must drop the reader indicator on exit")
	}
	step = max(8, m.width/10)
	press(m, "l")
	if off := m.vp.XOffset(); off != step {
		t.Fatalf("plain nowrap panning resumes after exit: offset %d, want %d", off, step)
	}
}

// w toggles wrap INSIDE reader mode: wrapped path pins the padded 120-col
// column, toggling back re-frames the viewport.
func TestReaderWrapToggleInsideReader(t *testing.T) {
	m := newRenderedModel(t, readerProse, 140, 24)
	settle(t, m, press(m, "r"))
	settle(t, m, press(m, "w")) // reader + nowrap

	cmd := press(m, "w") // back to reader + wrapped
	if cmd == nil || !m.wrapMode {
		t.Fatalf("w must toggle wrap inside reader (cmd=%v wrap=%v)", cmd, m.wrapMode)
	}
	settle(t, m, cmd)
	if lead := minLeading(m.stripped); lead != 12 { // 10 margin + glamour 2
		t.Fatalf("wrapped reader keeps padded lines: lead %d", lead)
	}
	if widest := widestLine(m.stripped); widest > 130 {
		t.Fatalf("wrapped reader column must cap at margin+120: widest %d", widest)
	}
	if got := m.vp.Width(); got != 140 {
		t.Fatalf("wrapped reader uses the full-width viewport: %d", got)
	}

	settle(t, m, press(m, "w")) // reader + nowrap again
	if m.wrapMode {
		t.Fatal("second w returns to nowrap")
	}
	if got := m.vp.Width(); got != 120 {
		t.Fatalf("nowrap reader re-pins the viewport: %d, want 120", got)
	}
}

func assertMarginFramed(t *testing.T, m *Model, pad string, wantW int) {
	t.Helper()
	for i, l := range strings.Split(bodyOf(m), "\n") {
		s := ansi.Strip(l)
		if !strings.HasPrefix(s, pad) {
			t.Fatalf("line %d lacks the display margin frame: %q", i, s)
		}
		if w := ansi.StringWidth(l); w != wantW {
			t.Fatalf("line %d spans %d cols, want margin+column %d", i, w, wantW)
		}
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
	const vw = 140
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
	m := newRenderedModel(t, tocDoc, 140, 24)
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

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
			if lead := minLeading(m.stripped); lead != baseLead {
				t.Fatalf("stored lines must remain unpadded: lead %d, want %d", lead, baseLead)
			}
			if got := m.vp.Width(); got != tc.wantW {
				t.Fatalf("reader viewport width %d, want %d", got, tc.wantW)
			}
			if v := m.View().Content; !strings.Contains(v, "render reader") {
				t.Fatalf("status bar lacks reader indicator:\n%s", v)
			}
			for _, l := range strings.Split(bodyOf(m), "\n") {
				if s := ansi.Strip(l); !strings.HasPrefix(s, strings.Repeat(" ", tc.wantMargin)) {
					t.Fatalf("reader line lacks %d-cell margin: %q", tc.wantMargin, s)
				}
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
	for _, l := range strings.Split(bodyOf(m), "\n") {
		s := ansi.Strip(l)
		if !strings.Contains(s, strings.Repeat("z", 110)) {
			continue
		}
		found = true
		if !strings.HasPrefix(s, pad) {
			t.Fatalf("overflowing code line lost the reader margin: %q", s)
		}
		if w := ansi.StringWidth(l); w != 130 {
			t.Fatalf("reader frame width %d, want 130", w)
		}
	}
	if !found {
		t.Fatal("code line missing from reader render")
	}
}

// Search highlights live in the stored unpadded line content; the reader
// margin prefix shifts columns only visually.
func TestReaderHighlightAlignment(t *testing.T) {
	const vw = 140
	doc := "# Find\n\nneedle here\n\n" + strings.Repeat("tail filler\n\n", 8) +
		strings.Repeat("z", 200) + "\n"
	m := newRenderedModel(t, doc, vw, 24)
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
		t.Fatal("current match must be highlighted in reader mode")
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

// Reader lines stay unpadded at their natural width; the viewport is pinned
// to the centered reader column and content pans within that fixed frame.
func TestReaderViewportFrame(t *testing.T) {
	const vw = 140
	doc := "# Wide\n\n" + strings.Repeat("word ", 60) + "\n\n```\n" + strings.Repeat("code ", 60) + "\n```\n"
	m := newRenderedModel(t, doc, vw, 24)
	settle(t, m, press(m, "r"))

	if !m.reader {
		t.Fatal("precondition: reader mode")
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
	if v := m.View().Content; !strings.Contains(v, "render reader") {
		t.Fatalf("status bar must report reader mode:\n%s", v)
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
		t.Fatalf("full-width panning resumes after exit: offset %d, want %d", off, step)
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
	if cmd := press(m, "r"); cmd == nil || m.reader {
		t.Fatalf("s must not block reader toggle (cmd=%v reader=%v)", cmd, m.reader)
	}
}

func TestReaderOverlaysCompose(t *testing.T) {
	const vw = 140
	m := newRenderedModel(t, tocDoc, vw, 24)
	settle(t, m, press(m, "r"))

	press(m, "o")
	lines := strings.Split(bodyOf(m), "\n")
	outlineTop := -1
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "╭─ Outline") {
			outlineTop = i
			break
		}
	}
	if outlineTop < 0 {
		t.Fatal("centered outline modal missing under reader mode")
	}
	outline := []rune(ansi.Strip(lines[outlineTop]))
	left, right := -1, -1
	for i, ch := range outline {
		if ch == '╭' && left < 0 {
			left = i
		}
		if ch == '╮' && right < 0 {
			right = i
		}
	}
	if left < 0 || right < left || left != (vw-(right-left+1))/2 {
		t.Fatalf("outline modal not centered: %q", string(outline))
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

func TestReaderMarginIsDisplayOnlySpaces(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 140, 24)
	baseLead := minLeading(m.base)
	settle(t, m, press(m, "r"))
	if lead := minLeading(m.base); lead != baseLead {
		t.Fatalf("stored content must remain unpadded: lead %d, want %d", lead, baseLead)
	}
	pad := strings.Repeat(" ", 10)
	for i, l := range strings.Split(bodyOf(m), "\n") {
		s := ansi.Strip(l)
		if !strings.HasPrefix(s, pad) {
			t.Fatalf("line %d lacks the display margin: %q", i, s)
		}
		if esc := strings.IndexByte(l, '\x1b'); esc >= 0 && esc < 10 {
			t.Fatalf("line %d: SGR inside the margin region: %q", i, l)
		}
	}
}

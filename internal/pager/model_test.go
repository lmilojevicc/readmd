package pager

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

func press(m *Model, s string) tea.Cmd {
	nm, cmd := m.Update(tea.KeyPressMsg{Code: rune(s[0]), Text: s})
	*m = *nm.(*Model)
	return cmd
}

func settle(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		nm, next := m.Update(msg)
		*m = *nm.(*Model)
		cmd = next
	}
}

var longDoc = "short line\n" + strings.Repeat("x", 200) + "\nanother\n"

func TestModelModes(t *testing.T) {
	m := New(longDoc, "t")
	settle(t, m, press(m, "\x00")) // no-op key
	sz := tea.WindowSizeMsg{Width: 80, Height: 24}
	nm, cmd := m.Update(sz)
	*m = *nm.(*Model)
	if cmd == nil {
		t.Fatal("resize should start render")
	}
	settle(t, m, cmd)

	if m.wrapMode {
		t.Fatal("nowrap mode is default")
	}

	before := m.vp.XOffset()
	for range 3 {
		press(m, "l")
	}
	step := max(8, m.width/10)
	if got, want := m.vp.XOffset(), before+3*step; got != want {
		t.Fatalf("after 3*l: offset %d, want %d", got, want)
	}
	press(m, "h")
	if m.vp.XOffset() != before+2*step {
		t.Fatalf("after h: offset %d, want %d", m.vp.XOffset(), before+2*step)
	}
	press(m, "0")
	if m.vp.XOffset() != 0 {
		t.Fatalf("0 must reset offset, got %d", m.vp.XOffset())
	}

	cmd = press(m, "T")
	if !m.collapsed {
		t.Fatal("T should set collapsed")
	}
	settle(t, m, cmd)
	cmd = press(m, "T")
	if m.collapsed {
		t.Fatal("second T should un-collapse")
	}
	settle(t, m, cmd)

	if cmd := press(m, "w"); cmd == nil || !m.wrapMode {
		t.Fatalf("w toggles to wrap (cmd=%v wrapMode=%v)", cmd, m.wrapMode)
	} else {
		settle(t, m, cmd)
	}
	if cmd := press(m, "w"); cmd == nil || m.wrapMode {
		t.Fatalf("w toggles back to nowrap (cmd=%v wrapMode=%v)", cmd, m.wrapMode)
	} else {
		settle(t, m, cmd)
	}
}

func TestWrappedHorizontalPan(t *testing.T) {
	const longMermaid = "```mermaid\n" +
		"flowchart LR\n" +
		"A[Alpha] --> B[Bravo]\n" +
		"B --> C[Charlie]\n" +
		"C --> D[Delta]\n" +
		"D --> E[Echo]\n" +
		"E --> F[Foxtrot]\n" +
		"F --> G[Golf]\n" +
		"G --> H[Hotel]\n" +
		"H --> I[India]\n" +
		"I --> J[Juliet]\n" +
		"J --> K[Kilo]\n" +
		"K --> L[Lima]\n" +
		"```\n"

	for _, tc := range []struct {
		name         string
		doc          string
		wantOverflow bool
	}{
		{"long Mermaid diagram", longMermaid, true},
		{"short prose", "short line\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.doc, "diagram.md")
			m.SetWrap(true)
			nm, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 16})
			*m = *nm.(*Model)
			settle(t, m, cmd)
			if !m.wrapMode || m.reader {
				t.Fatalf("precondition: wrap mode (wrap=%v reader=%v)", m.wrapMode, m.reader)
			}

			before := m.vp.View()
			if tc.wantOverflow {
				if m.widest <= m.vp.Width() {
					t.Fatalf("precondition: rendered diagram width %d must exceed viewport %d", m.widest, m.vp.Width())
				}
				if status := ansi.Strip(m.statusBar()); !strings.Contains(status, "render wrap wide") || strings.Contains(status, "→") {
					t.Fatalf("status must report wrap-wide overflow at offset zero: %q", status)
				}
				press(m, "l")
				right := m.vp.XOffset()
				if right == 0 {
					t.Fatal("l must pan overflowing wrapped content")
				}
				if after := m.vp.View(); after == before {
					t.Fatal("horizontal pan must change the visible diagram slice")
				}
				if status := ansi.Strip(m.statusBar()); !strings.Contains(status, "render wrap wide →"+strconv.Itoa(right)) {
					t.Fatalf("status must report wrapped horizontal offset %d: %q", right, status)
				}
				press(m, "h")
				if off := m.vp.XOffset(); off >= right {
					t.Fatalf("h must pan left from %d, got %d", right, off)
				}
				press(m, "l")
				press(m, "0")
				if off := m.vp.XOffset(); off != 0 {
					t.Fatalf("0 must reset wrapped horizontal pan, got %d", off)
				}
				press(m, "l")
				settle(t, m, press(m, "r"))
				if !m.reader || m.vp.XOffset() != 0 {
					t.Fatalf("wrapped reader mode must retain its unpanned frame (reader=%v offset=%d)", m.reader, m.vp.XOffset())
				}
				press(m, "l")
				if off := m.vp.XOffset(); off != 0 {
					t.Fatalf("wrapped reader mode must ignore horizontal pan, got offset %d", off)
				}
				return
			}

			if m.widest > m.vp.Width() {
				t.Fatalf("precondition: short content width %d exceeds viewport %d", m.widest, m.vp.Width())
			}
			if status := ansi.Strip(m.statusBar()); strings.Contains(status, " wide") {
				t.Fatalf("non-overflowing wrap status must not report wide: %q", status)
			}
			for _, key := range []string{"l", "h", "0"} {
				press(m, key)
			}
			if off := m.vp.XOffset(); off != 0 {
				t.Fatalf("non-overflowing content must stay at offset 0, got %d", off)
			}
			if after := m.vp.View(); after != before {
				t.Fatal("horizontal keys must not disturb non-overflowing content")
			}
		})
	}
}

func TestCollapseClampsXOffset(t *testing.T) {
	for _, tc := range []struct {
		name    string
		doc     string
		pans    int
		wantOff int
	}{
		{"shrinks below offset", "| " + strings.Repeat("x", 200) + " |\n| - |\n", 15, 0},
		{"still wider than offset", "| H |\n| - |\n| " + strings.Repeat("y", 150) + " |\n", 5, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.doc, "t")
			nm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			*m = *nm.(*Model)
			settle(t, m, cmd)
			if m.wrapMode {
				t.Fatal("precondition: default mode must be nowrap")
			}
			for range tc.pans {
				press(m, "l")
			}
			if m.vp.XOffset() == 0 {
				t.Fatal("precondition: pan did not move offset")
			}
			if !m.collapsed {
				settle(t, m, press(m, "T"))
			}
			if got := m.vp.XOffset(); got != tc.wantOff {
				t.Fatalf("after collapse: offset %d, want %d", got, tc.wantOff)
			}
		})
	}
}

func TestRenderedMsgStale(t *testing.T) {
	m := New(longDoc, "t")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	*m = *nm.(*Model)
	if cmd == nil {
		t.Fatal("first resize must render")
	}
	stale := cmd().(renderedMsg)
	if stale.width != 80 {
		t.Fatalf("stale rendered at %d, want 80", stale.width)
	}

	nm, coalesced := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	*m = *nm.(*Model)
	if coalesced != nil {
		t.Fatal("resize while rendering must coalesce")
	}

	nm, retry := m.Update(stale)
	*m = *nm.(*Model)
	if retry == nil {
		t.Fatal("stale result must trigger re-render at current width")
	}
	fresh := retry().(renderedMsg)
	if fresh.width != 40 || fresh.gen != m.gen {
		t.Fatalf("retry rendered at %d gen %d, want width 40 gen %d", fresh.width, fresh.gen, m.gen)
	}
}

func TestViewStatusBar(t *testing.T) {
	m := New(longDoc, "file.md")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	*m = *nm.(*Model)
	settle(t, m, cmd)

	view := func() string { return m.View().Content }
	if v := view(); !strings.Contains(v, "render nowrap") || strings.Contains(v, " wide") || strings.Contains(v, "→") {
		t.Fatalf("default nowrap status must lack wide and offset tags:\n%s", v)
	}
	for range 2 {
		press(m, "l")
	}
	step := max(8, m.width/10)
	if v := view(); !strings.Contains(v, "render nowrap →"+strconv.Itoa(step*2)) || strings.Contains(v, " wide") {
		t.Fatalf("nowrap status must show offset without a wide tag:\n%s", v)
	}

	settle(t, m, press(m, "w"))
	if v := view(); !strings.Contains(v, "render wrap") || strings.Contains(v, "nowrap") {
		t.Fatalf("w must switch status to wrap:\n%s", v)
	}
}

func TestStatusBarLayout(t *testing.T) {
	bar := func(w int) string {
		m := New(longDoc, "doc.md")
		m.SetWrap(true)
		nm, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: 10})
		*m = *nm.(*Model)
		settle(t, m, cmd)
		v := m.View().Content
		return v[strings.LastIndexByte(v, '\n')+1:]
	}
	for _, tc := range []struct {
		name string
		w    int
		want []string
		omit []string
	}{
		{"full bar", 44, []string{brandChip, "doc.md", "%", "help"}, nil},
		{"filename truncates first", 30, []string{brandChip, "%", "…"}, []string{"help"}},
		{"sub-2-char name dropped", 28, []string{brandChip, "%"}, []string{"help", "…"}},
		{"hint dropped before percent", 26, []string{brandChip, "%"}, []string{"help", "doc.md"}},
		{"percent dropped before chip", 22, []string{brandChip, "wrap"}, []string{"%", "help"}},
		{"vw 12 drops the name segment", 12, []string{brandChip}, []string{"%", "help", "wrap", "…"}},
		{"bare chip survives", 9, []string{brandChip}, []string{"%", "help", "wrap"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bar(tc.w)
			if !strings.HasPrefix(b, brandChip) {
				t.Fatalf("bar must open with the brand chip:\n%q", b)
			}
			if w := ansi.StringWidth(b); w > tc.w {
				t.Fatalf("bar must not exceed the terminal: width %d, want ≤ %d:\n%q", w, tc.w, b)
			}
			for _, want := range tc.want {
				if !strings.Contains(b, want) {
					t.Fatalf("bar lacks %q:\n%q", want, b)
				}
			}
			for _, omit := range tc.omit {
				if strings.Contains(b, omit) {
					t.Fatalf("bar must drop %q when narrowing:\n%q", omit, b)
				}
			}
		})
	}
}

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

var longDoc = "```\nshort line\n" + strings.Repeat("x", 200) + "\nanother\n```\n"

func TestModelNavigation(t *testing.T) {
	m := New(longDoc, "t")
	settle(t, m, press(m, "\x00")) // no-op key
	sz := tea.WindowSizeMsg{Width: 80, Height: 24}
	nm, cmd := m.Update(sz)
	*m = *nm.(*Model)
	if cmd == nil {
		t.Fatal("resize should start render")
	}
	settle(t, m, cmd)

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

	for _, key := range []string{"w", "T"} {
		before = m.vp.XOffset()
		if cmd := press(m, key); cmd != nil || m.vp.XOffset() != before {
			t.Fatalf("%s must be unbound (cmd=%v offset=%d)", key, cmd, m.vp.XOffset())
		}
	}

}

func TestHorizontalPan(t *testing.T) {
	const longMermaid = "```mermaid\n" +
		"flowchart LR\n" +
		"A[Alpha] --> B[Bravo] --> C[Charlie] --> D[Delta] --> E[Echo] --> F[Foxtrot]\n" +
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
			nm, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 16})
			*m = *nm.(*Model)
			settle(t, m, cmd)

			before := m.vp.View()
			if tc.wantOverflow {
				if m.widest <= m.vp.Width() {
					t.Fatalf("precondition: rendered diagram width %d must exceed viewport %d", m.widest, m.vp.Width())
				}
				if status := ansi.Strip(m.statusBar()); !strings.Contains(status, "render") || strings.Contains(status, "→") {
					t.Fatalf("status at offset zero: %q", status)
				}
				press(m, "l")
				right := m.vp.XOffset()
				if right == 0 {
					t.Fatal("l must pan overflowing content")
				}
				if after := m.vp.View(); after == before {
					t.Fatal("horizontal pan must change the visible diagram slice")
				}
				if status := ansi.Strip(m.statusBar()); !strings.Contains(status, "render →"+strconv.Itoa(right)) {
					t.Fatalf("status must report horizontal offset %d: %q", right, status)
				}
				press(m, "h")
				if off := m.vp.XOffset(); off >= right {
					t.Fatalf("h must pan left from %d, got %d", right, off)
				}
				press(m, "l")
				press(m, "0")
				if off := m.vp.XOffset(); off != 0 {
					t.Fatalf("0 must reset horizontal pan, got %d", off)
				}
				return
			}

			if m.widest > m.vp.Width() {
				t.Fatalf("precondition: short content width %d exceeds viewport %d", m.widest, m.vp.Width())
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

func TestRenderedMsgStale(t *testing.T) {
	m := New(longDoc, "t")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	*m = *nm.(*Model)
	if cmd == nil {
		t.Fatal("first resize must render")
	}
	stale := cmd().(renderedMsg)

	nm, coalesced := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	*m = *nm.(*Model)
	if coalesced != nil {
		t.Fatal("resize while rendering must coalesce")
	}
	if m.width != 40 || stale.gen >= m.gen {
		t.Fatalf("resize must advance generation at current width: width=%d stale gen=%d current gen=%d", m.width, stale.gen, m.gen)
	}

	nm, retry := m.Update(stale)
	*m = *nm.(*Model)
	if retry == nil {
		t.Fatal("stale result must trigger re-render at current width")
	}
	fresh := retry().(renderedMsg)
	if fresh.gen != m.gen || fresh.gen <= stale.gen {
		t.Fatalf("retry generation %d, want current gen %d after stale gen %d", fresh.gen, m.gen, stale.gen)
	}
}

func TestViewStatusBar(t *testing.T) {
	m := New(longDoc, "file.md")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	*m = *nm.(*Model)
	settle(t, m, cmd)

	view := func() string { return m.View().Content }
	if v := view(); !strings.Contains(v, "render") || strings.Contains(v, "→") {
		t.Fatalf("render status must lack an offset at column zero:\n%s", v)
	}
	for range 2 {
		press(m, "l")
	}
	step := max(8, m.width/10)
	if v := view(); !strings.Contains(v, "render →"+strconv.Itoa(step*2)) {
		t.Fatalf("render status must show horizontal offset:\n%s", v)
	}
}

func TestStatusBarStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *Model)
		want  string
	}{
		{"render", func(*testing.T, *Model) {}, "render"},
		{"render offset", func(_ *testing.T, m *Model) { press(m, "l") }, "render →8"},
		{"reader", func(t *testing.T, m *Model) { settle(t, m, press(m, "r")) }, "render reader"},
		{"reader offset", func(t *testing.T, m *Model) {
			settle(t, m, press(m, "r"))
			press(m, "l")
		}, "render reader →8"},
		{"source", func(_ *testing.T, m *Model) { press(m, "s") }, "source"},
		{"source offset", func(_ *testing.T, m *Model) {
			press(m, "s")
			press(m, "l")
		}, "source →8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(longDoc, "doc.md")
			nm, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
			*m = *nm.(*Model)
			settle(t, m, cmd)
			tc.setup(t, m)
			bar := ansi.Strip(m.statusBar())
			if !strings.Contains(bar, tc.want) {
				t.Fatalf("status %q lacks %q", bar, tc.want)
			}
			for _, obsolete := range []string{"wrap", "nowrap", "wide"} {
				if strings.Contains(bar, obsolete) {
					t.Fatalf("status contains obsolete layout label %q: %q", obsolete, bar)
				}
			}
		})
	}
}

func TestStatusBarLayout(t *testing.T) {
	bar := func(w int) string {
		m := New(longDoc, "doc.md")
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
		{"filename drops first", 30, []string{brandChip, "render", "%", "help"}, []string{"doc.md", "…"}},
		{"sub-2-char name dropped", 28, []string{brandChip, "%"}, []string{"help", "…"}},
		{"hint dropped before percent", 26, []string{brandChip, "%"}, []string{"help", "doc.md"}},
		{"percent dropped before chip", 19, []string{brandChip, "render"}, []string{"%", "help"}},
		{"vw 12 drops the name segment", 12, []string{brandChip}, []string{"%", "help", "render", "…"}},
		{"bare chip survives", 9, []string{brandChip}, []string{"%", "help", "render"}},
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

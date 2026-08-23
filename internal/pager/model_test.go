package pager

import (
	"strconv"
	"strings"
	"testing"

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

	if !m.wrapMode {
		t.Fatal("wrap mode is default")
	}
	press(m, "h")
	if off := m.vp.XOffset(); off != 0 {
		t.Fatalf("h in wrap mode must not pan, got %d", off)
	}

	if cmd := press(m, "w"); cmd == nil {
		t.Fatal("w should re-render")
	} else {
		settle(t, m, cmd)
	}
	if m.wrapMode {
		t.Fatal("w should toggle to nowrap")
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

	if cmd := press(m, "w"); cmd == nil || m.wrapMode != true {
		t.Fatalf("w toggles back to wrap (cmd=%v wrapMode=%v)", cmd, m.wrapMode)
	} else {
		settle(t, m, cmd)
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
			settle(t, m, press(m, "w"))
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
	if v := view(); !strings.Contains(v, "wrap ") {
		t.Fatalf("status bar should show wrap mode:\n%s", v)
	}

	settle(t, m, press(m, "w"))
	if v := view(); !strings.Contains(v, "nowrap") || strings.Contains(v, "→") {
		t.Fatalf("nowrap status bar without offset should lack arrow:\n%s", v)
	}
	for range 2 {
		press(m, "l")
	}
	step := max(8, m.width/10)
	if v := view(); !strings.Contains(v, "nowrap") || !strings.Contains(v, strconv.Itoa(step*2)) {
		t.Fatalf("status bar should show horizontal offset %d:\n%s", step*2, v)
	}
}

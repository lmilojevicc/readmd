package pager

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func sendMouseClick(m *Model, x, y int) tea.Cmd {
	nm, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	*m = *nm.(*Model)
	return cmd
}

func sendMouseWheel(m *Model, button tea.MouseButton) {
	nm, _ := m.Update(tea.MouseWheelMsg{Button: button})
	*m = *nm.(*Model)
}

func applyMouseCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	nm, _ := m.Update(cmd())
	*m = *nm.(*Model)
}

func TestMouseModeDefaultsAndToggle(t *testing.T) {
	m := New("", "doc.md")
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("default mouse mode=%v, want cell motion", got)
	}
	press(m, "m")
	if m.mouse || m.flash != "mouse off" || m.View().MouseMode != tea.MouseModeNone {
		t.Fatalf("mouse off state: enabled=%v flash=%q mode=%v", m.mouse, m.flash, m.View().MouseMode)
	}
	press(m, "m")
	if !m.mouse || m.flash != "mouse on" || m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse on state: enabled=%v flash=%q mode=%v", m.mouse, m.flash, m.View().MouseMode)
	}
}

func TestMouseToggleKeepsOverlayKeyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		check func(*testing.T, *Model)
	}{
		{
			"search captures m",
			func(m *Model) { m.search.active = true },
			func(t *testing.T, m *Model) {
				if !m.mouse || m.search.query != "m" {
					t.Fatalf("mouse=%v query=%q", m.mouse, m.search.query)
				}
			},
		},
		{
			"toc ignores m",
			func(m *Model) { m.tocOpen = true },
			func(t *testing.T, m *Model) {
				if !m.mouse || !m.tocOpen {
					t.Fatalf("mouse=%v toc=%v", m.mouse, m.tocOpen)
				}
			},
		},
		{
			"help closes without toggling",
			func(m *Model) { m.helpOpen = true },
			func(t *testing.T, m *Model) {
				if !m.mouse || m.helpOpen {
					t.Fatalf("mouse=%v help=%v", m.mouse, m.helpOpen)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHintModel(t, 60, 12, []string{"plain"})
			tc.setup(m)
			press(m, "m")
			tc.check(t, m)
		})
	}
}

func TestMouseClickActivatesEveryVisibleLinkRegion(t *testing.T) {
	line := osc8("id=one", "https://example.com/path", "label", "\x1b\\") + " " +
		osc8("id=one", "https://example.com/path", "https://example.com/path", "\x1b\\")
	for _, x := range []int{0, 7} {
		t.Run(string(rune('a'+x)), func(t *testing.T) {
			m := newHintModel(t, 80, 12, []string{line})
			var opened string
			m.openURL = func(dest string) error { opened = dest; return nil }
			cmd := sendMouseClick(m, x, 0)
			if cmd == nil {
				t.Fatalf("click x=%d did not activate", x)
			}
			applyMouseCmd(m, cmd)
			if opened != "https://example.com/path" {
				t.Fatalf("opened=%q", opened)
			}
		})
	}
}

func TestMouseClickFootnoteJumpsAndPushesHistory(t *testing.T) {
	src := "# Top\n\n" + strings.Repeat("intro paragraph\n\n", 8) +
		"reference[^note] tail\n\n" + strings.Repeat("middle paragraph\n\n", 12) +
		"[^note]: definition body\n"
	m := newRenderedModel(t, src, 60, 10)
	var target linkTarget
	for _, candidate := range m.links {
		if candidate.kind == targetFootnote {
			target = candidate
			break
		}
	}
	if len(target.regions) != 1 {
		t.Fatalf("footnote target=%#v", target)
	}
	reg := target.regions[0]
	m.vp.SetYOffset(reg.line)
	before := m.currentLocation()
	if cmd := sendMouseClick(m, reg.start, 0); cmd != nil {
		t.Fatal("internal footnote click must be synchronous")
	}
	visibleEnd := m.vp.YOffset() + m.vp.Height()
	if len(m.locations) != 1 || target.definition.line < m.vp.YOffset() || target.definition.line >= visibleEnd {
		t.Fatalf("history=%d definition=%d viewport=[%d,%d)", len(m.locations), target.definition.line, m.vp.YOffset(), visibleEnd)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.vp.XOffset() != before.x || m.vp.YOffset() != before.y {
		t.Fatalf("return x/y=%d/%d want=%d/%d", m.vp.XOffset(), m.vp.YOffset(), before.x, before.y)
	}
}

func TestMouseHitTestingDisplayColumnsAndClipping(t *testing.T) {
	family := "👨‍👩‍👧‍👦"
	line := strings.Repeat("界", 8) + osc8("id=wide", "https://wide.example", family+"e\u0301", "\x1b\\") + strings.Repeat("z", 20)
	m := newHintModel(t, 12, 8, []string{line})
	reg := m.links[0].regions[0]
	m.vp.SetXOffset(reg.start + 1)
	var opened int
	m.openURL = func(string) error { opened++; return nil }
	cmd := sendMouseClick(m, 1, 0)
	applyMouseCmd(m, cmd)
	if opened != 1 {
		t.Fatalf("partially clipped CJK/emoji target opened=%d region=%#v xoff=%d", opened, reg, m.vp.XOffset())
	}
	if cmd := sendMouseClick(m, 11, 0); cmd != nil || opened != 1 {
		t.Fatal("click outside the visible target activated it")
	}
}

func TestMouseReaderMarginAndStatusRows(t *testing.T) {
	line := linkLine("one", "https://one.example", "link")
	m := newHintModel(t, 140, 10, []string{line})
	m.reader = true
	m.syncVPWidth()
	opened := 0
	m.openURL = func(string) error { opened++; return nil }
	if cmd := sendMouseClick(m, 9, 0); cmd != nil || opened != 0 {
		t.Fatal("reader margin click activated a target")
	}
	applyMouseCmd(m, sendMouseClick(m, 10, 0))
	if opened != 1 {
		t.Fatalf("reader content click opened=%d", opened)
	}
	for _, tc := range []struct {
		name  string
		setup func()
	}{
		{"status row", func() {}},
		{"flash row", func() { m.flash = "notice" }},
		{"search prompt row", func() { m.flash = ""; m.search.active = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			if cmd := sendMouseClick(m, 10, m.vp.Height()); cmd != nil || opened != 1 {
				t.Fatal("non-document row click activated a target")
			}
		})
	}
}

func TestMouseHintStripAndFrozenDocument(t *testing.T) {
	line := linkLine("one", "https://one.example", "link")
	m := newHintModel(t, 60, 12, append([]string{line}, strings.Split(strings.Repeat("filler\n", 20), "\n")...))
	base := append([]string(nil), m.base...)
	opened := 0
	m.openURL = func(string) error { opened++; return nil }
	press(m, "t")
	beforeY := m.vp.YOffset()
	hits := m.hintStripHits()
	if len(hits) != 1 {
		t.Fatalf("hint hits=%#v strip=%q", hits, m.hintStrip())
	}
	applyMouseCmd(m, sendMouseClick(m, hits[0].start, m.vp.Height()))
	if opened != 1 || m.targets.active {
		t.Fatalf("strip activation opened=%d active=%v", opened, m.targets.active)
	}
	for i := range base {
		if base[i] != m.base[i] {
			t.Fatal("mouse hint activation mutated cached ANSI")
		}
	}

	press(m, "t")
	sendMouseWheel(m, tea.MouseWheelDown)
	if m.vp.YOffset() != beforeY {
		t.Fatal("wheel moved the frozen target viewport")
	}
	if cmd := sendMouseClick(m, m.width-1, m.vp.Height()); cmd != nil {
		t.Fatal("hint strip outside an entry activated a target")
	}
	applyMouseCmd(m, sendMouseClick(m, 0, 0))
	if opened != 2 || m.targets.active {
		t.Fatalf("document token activation opened=%d active=%v", opened, m.targets.active)
	}
}

func TestMouseHintStripRejectsTruncatedEntry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
		lines []string
	}{
		{"single long entry", 8, []string{linkLine("one", "https://very-long.example/path", "link")}},
		{
			"ellipsis replaces final cell of otherwise fitting first entry",
			ansi.StringWidth("A one.example"),
			[]string{
				linkLine("one", "https://one.example", "one"),
				linkLine("two", "https://two.example", "two"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHintModel(t, tc.width, 8, tc.lines)
			m.openURL = func(string) error { t.Fatal("truncated strip entry opened"); return nil }
			press(m, "t")
			if hits := m.hintStripHits(); len(hits) != 0 {
				t.Fatalf("truncated strip retained hit region: %#v strip=%q", hits, m.hintStrip())
			}
			if cmd := sendMouseClick(m, 0, m.vp.Height()); cmd != nil || !m.targets.active {
				t.Fatal("truncated hint strip click activated or closed target mode")
			}
		})
	}
}

func TestViewHeightAccountsForHintsAndOpenerFlashes(t *testing.T) {
	line := linkLine("one", "https://one.example", "one")
	for _, tc := range []struct {
		name string
		msg  openedURLMsg
	}{
		{"opener success", openedURLMsg{dest: "https://one.example"}},
		{"opener failure", openedURLMsg{dest: "https://one.example", err: errors.New("boom")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHintModel(t, 40, 4, []string{line, "filler", "filler"})
			nm, _ := m.Update(tc.msg)
			*m = *nm.(*Model)
			if got := len(strings.Split(m.View().Content, "\n")); got != m.height {
				t.Fatalf("view height=%d want=%d content=%q", got, m.height, m.View().Content)
			}
			if m.vp.Height() != 2 {
				t.Fatalf("notice must reserve one row: viewport height=%d", m.vp.Height())
			}
		})
	}

	for _, tc := range []struct {
		name   string
		height int
		lines  []string
	}{
		{"narrow viewport", 3, []string{line, "filler", "filler"}},
		{"target on bottom hint row", 5, []string{"filler", "filler", line, "filler"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHintModel(t, 40, tc.height, tc.lines)
			opened := 0
			m.openURL = func(string) error { opened++; return nil }
			press(m, "t")
			if !m.targets.active || len(m.targets.targets) != 1 {
				t.Fatalf("precondition: active=%v targets=%d", m.targets.active, len(m.targets.targets))
			}
			if tc.name == "target on bottom hint row" {
				reg := m.targets.targets[0].regions[0]
				if reg.line != m.vp.YOffset()+m.vp.Height()-1 {
					t.Fatalf("precondition: target line=%d bottom=%d", reg.line, m.vp.YOffset()+m.vp.Height()-1)
				}
			}
			hintHeight := m.vp.Height()
			label := m.targets.targets[0].label
			if got := len(strings.Split(m.View().Content, "\n")); got != m.height {
				t.Fatalf("hint view height=%d want=%d", got, m.height)
			}

			nm, _ := m.Update(openedURLMsg{dest: "https://one.example"})
			*m = *nm.(*Model)
			if m.targets.active || m.flash == "" || m.vp.Height() != hintHeight {
				t.Fatalf("opener result state active=%v flash=%q viewport=%d want=%d", m.targets.active, m.flash, m.vp.Height(), hintHeight)
			}
			if got := len(strings.Split(m.View().Content, "\n")); got != m.height {
				t.Fatalf("opener result view height=%d want=%d content=%q", got, m.height, m.View().Content)
			}
			if cmd := pressKey(m, tea.KeyPressMsg{Code: rune(label[0]), Text: label[:1]}); cmd != nil || opened != 0 {
				t.Fatalf("stale hint activated after opener result: cmd=%v opened=%d", cmd, opened)
			}
		})
	}
}

func TestMouseShiftClickPreservesSelectionPath(t *testing.T) {
	m := newHintModel(t, 60, 12, []string{linkLine("one", "https://one.example", "link")})
	opened := false
	m.openURL = func(string) error { opened = true; return nil }
	nm, cmd := m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft, Mod: tea.ModShift})
	*m = *nm.(*Model)
	if cmd != nil || opened {
		t.Fatal("shift-click must not activate an application target")
	}
}

func TestMouseTargetClicksSuppressedByOverlaysAndRows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"search prompt", func(m *Model) { m.search.active = true }},
		{"toc", func(m *Model) { m.tocOpen = true }},
		{"help", func(m *Model) { m.helpOpen = true }},
		{"source", func(m *Model) { m.srcView = true }},
		{"mouse disabled", func(m *Model) { m.mouse = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHintModel(t, 60, 12, []string{linkLine("one", "https://one.example", "link")})
			opened := 0
			m.openURL = func(string) error { opened++; return nil }
			tc.setup(m)
			if cmd := sendMouseClick(m, 0, 0); cmd != nil || opened != 0 {
				t.Fatalf("suppressed click cmd=%v opened=%d", cmd, opened)
			}
		})
	}
}

func TestMouseWheelDocumentOverlaysBoundsAndDisabled(t *testing.T) {
	m := newHintModel(t, 60, 8, strings.Split(strings.Repeat("line\n", 40), "\n"))
	sendMouseWheel(m, tea.MouseWheelUp)
	if m.vp.YOffset() != 0 {
		t.Fatal("wheel up moved past top")
	}
	sendMouseWheel(m, tea.MouseWheelDown)
	if m.vp.YOffset() != mouseWheelStep {
		t.Fatalf("wheel down y=%d", m.vp.YOffset())
	}
	sendMouseWheel(m, tea.MouseWheelUp)
	if m.vp.YOffset() != 0 {
		t.Fatalf("wheel up y=%d", m.vp.YOffset())
	}
	m.vp.GotoBottom()
	bottom := m.vp.YOffset()
	sendMouseWheel(m, tea.MouseWheelDown)
	if m.vp.YOffset() != bottom {
		t.Fatalf("wheel down moved past bottom: got=%d want=%d", m.vp.YOffset(), bottom)
	}
	m.vp.GotoTop()
	m.mouse = false
	sendMouseWheel(m, tea.MouseWheelDown)
	if m.vp.YOffset() != 0 {
		t.Fatal("disabled mouse wheel moved document")
	}

	help := newHintModel(t, 60, 12, []string{"plain"})
	help.helpOpen = true
	sendMouseWheel(help, tea.MouseWheelDown)
	if help.helpTop != mouseWheelStep {
		t.Fatalf("help wheel top=%d", help.helpTop)
	}
	sendMouseWheel(help, tea.MouseWheelUp)
	if help.helpTop != 0 {
		t.Fatalf("help wheel up top=%d", help.helpTop)
	}

	toc := newRenderedModel(t, tocDoc, 60, 12)
	toc.openTOC()
	originY := toc.vp.YOffset()
	sendMouseWheel(toc, tea.MouseWheelDown)
	if toc.tocSel != min(mouseWheelStep, len(toc.heads)-1) || !toc.tocPreview {
		t.Fatalf("toc wheel selection=%d preview=%v heads=%d", toc.tocSel, toc.tocPreview, len(toc.heads))
	}
	if toc.vp.YOffset() != originY {
		t.Fatal("toc wheel preview mutated the real viewport")
	}
	sendMouseWheel(toc, tea.MouseWheelUp)
	if toc.tocSel != 0 || !toc.tocPreview {
		t.Fatalf("toc wheel up selection=%d preview=%v", toc.tocSel, toc.tocPreview)
	}
}

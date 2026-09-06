package pager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func crowdedTargetModel(t *testing.T, width int) *Model {
	t.Helper()
	lines := []string{"filler", "filler"}
	for i := range 20 {
		line := "        "
		for j := range 3 {
			n := i*3 + j
			line += linkLine(fmt.Sprint(n), fmt.Sprintf("https://example.com/item?q=%d#part%d", n, n), fmt.Sprintf("item%d", n)) + " "
		}
		lines = append(lines, line+strings.Repeat(" ", 140))
	}
	lines = append(lines, strings.Split(strings.Repeat("filler\n", 30), "\n")...)
	m := newHintModel(t, width, 24, lines)
	m.openURL = func(string) error { t.Fatal("unexpected opener"); return nil }
	m.vp.SetXOffset(2)
	m.vp.SetYOffset(2)
	return m
}

func TestTargetPanelAllRowsReachableAndFrozen(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := crowdedTargetModel(t, width)
			before, height := m.currentLocation(), m.vp.Height()
			base := append([]string(nil), m.base...)
			links := append([]linkTarget(nil), m.links...)
			press(m, "p")
			if len(m.targets.targets) != 60 {
				t.Fatalf("lost original candidates: %d", len(m.targets.targets))
			}
			panelHeight := len(m.targetPanelRows())
			for i := range 60 {
				target, ok := m.focusedTarget()
				if !ok || target.label != hintLabels(60)[i] {
					t.Fatalf("focus %d = %#v", i, target)
				}
				row := m.targetPanelY() + 1 + m.targets.focus - m.targets.top
				hit, ok := m.targetPanelHit(2, row)
				if !ok || hit.label != target.label {
					t.Fatalf("focused row not visible: %d %#v", row, hit)
				}
				if !strings.Contains(ansi.Strip(strings.Split(m.View().Content, "\n")[row]), target.label+" ") {
					t.Fatal("complete hint key missing")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			if m.targets.focus != 58 {
				t.Fatalf("Shift+Tab focus=%d", m.targets.focus)
			}
			sendMouseWheel(m, tea.MouseWheelUp)
			if m.targets.focus != 55 {
				t.Fatal("wheel did not move list focus")
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
			if m.targets.focus != 55-m.targetVisibleRows() {
				t.Fatal("page did not move list focus")
			}
			press(m, "a")
			if len(m.targetCandidates()) != len(hintAlphabet) {
				t.Fatal("prefix not filtered")
			}
			press(m, "z") // AZ exists and activates; use an additional absent prefix below instead.
			if m.targets.active {
				t.Fatal("unique prefix did not activate")
			}
			// The returned opener is intentionally not executed; no browser is used.
			press(m, "p")
			press(m, "z")
			if len(m.targetCandidates()) != 0 || !m.targets.active {
				t.Fatal("zero match must stay editable")
			}
			if cmd := pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
				t.Fatal("zero match activated")
			}
			sendMouseWheel(m, tea.MouseWheelDown)
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
			if len(m.targetCandidates()) != 60 {
				t.Fatal("Backspace lost frozen candidates")
			}
			if m.currentLocation() != before || m.vp.Height() != height || len(m.targetPanelRows()) != panelHeight {
				t.Fatal("picker changed document geometry")
			}
			if !reflect.DeepEqual(m.base, base) || !reflect.DeepEqual(m.links, links) {
				t.Fatal("picker mutated render cache")
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.currentLocation() != before || m.targets.active {
				t.Fatal("Esc did not cancel exactly")
			}
		})
	}
}

func TestTargetPanelLabelsHaveNoCeiling(t *testing.T) {
	for _, count := range []int{1, 26, 27, 676, 677, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			labels := hintLabels(count)
			seen := map[string]bool{}
			if len(labels) != count {
				t.Fatal("candidates truncated")
			}
			for _, label := range labels {
				if seen[label] || len(label) != len(labels[0]) {
					t.Fatal("labels not unique and prefix-free")
				}
				seen[label] = true
			}
		})
	}
}

func TestTargetPanelDescriptionsAndDetailPaging(t *testing.T) {
	for _, width := range []int{30, 40, 80, 120} {
		for _, height := range []int{3, 12} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				dest := "https://example.com/" + strings.Repeat("long/", 24) + "?version=two#fragment"
				m := newHintModel(t, width, height, []string{linkLine("one", dest, "first guide") + " " + linkLine("two", dest, "second guide")})
				m.openURL = func(string) error { t.Fatal("unexpected opener"); return nil }
				press(m, "p")
				if len(m.targets.targets) != 2 {
					t.Fatal("duplicate URL occurrences merged")
				}
				first, second := m.targets.targets[0], m.targets.targets[1]
				if targetText(first) != "first guide" || targetText(second) != "second guide" || targetContext(first) == targetContext(second) {
					t.Fatal("duplicate descriptions lost label/context")
				}
				if !strings.Contains(targetDescription(dest), "?version=two#fragment") {
					t.Fatal("summary silently removed query/fragment")
				}
				var visible strings.Builder
				pages := m.targetDetailPageCount()
				for range pages {
					rows := m.targetPanelRows()
					if height == 3 {
						visible.WriteString(ansi.Strip(rows[0])[2:])
					} else {
						for _, row := range rows[len(rows)-3 : len(rows)-1] {
							visible.WriteString(strings.Trim(ansi.Strip(row), "│ "))
						}
					}
					pressKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
				}
				if !strings.Contains(strings.ReplaceAll(visible.String(), " ", ""), dest) {
					t.Fatalf("full focused destination unavailable: %q", visible.String())
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
				if m.targets.detailPage != max(0, pages-2) {
					t.Fatal("previous detail page failed")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
				if m.targets.focus != 1 || m.targets.detailPage != 0 {
					t.Fatal("new focus did not reset detail page")
				}
			})
		}
	}
}

func TestTargetPanelMouseGeometry(t *testing.T) {
	for _, reader := range []bool{false, true} {
		for _, action := range []string{"row", "border", "blank", "detail", "footer", "status", "document", "covered", "filtered document"} {
			t.Run(fmt.Sprintf("reader=%v/%s", reader, action), func(t *testing.T) {
				m := crowdedTargetModel(t, 140)
				m.reader = reader
				m.syncVPWidth()
				press(m, "p")
				before := m.currentLocation()
				var opened string
				m.openURL = func(dest string) error { opened = dest; return nil }
				margin := 0
				if reader {
					_, margin = readerGeom(m.width, true)
				}
				x, y, want := 2, m.targetPanelY()+1, m.targets.targets[0].dest
				switch action {
				case "border":
					x, want = 0, ""
				case "blank":
					m.targets.prefix = "d"
					m.resetTargetFocus()
					m.moveTargetFocus(7)
					// A narrower filtered set leaves empty rows after resetting focus.
					m.targets.prefix = "dd"
					m.resetTargetFocus()
					y, want = m.targetPanelY()+2, ""
				case "detail":
					y, want = m.targets.savedHeight-3, ""
				case "footer":
					y, want = m.targets.savedHeight-1, ""
				case "status":
					y, want = m.targets.savedHeight, ""
				case "document", "filtered document":
					x, y = margin+8-m.vp.XOffset(), 0
					if action == "filtered document" {
						m.targets.prefix = "z"
						m.resetTargetFocus()
					}
				case "covered":
					// Click the original link beneath the top border, not a list row.
					x, y, want = margin+8-m.vp.XOffset(), m.targetPanelY(), ""
				}
				applyMouseCmd(m, sendMouseClick(m, x, y))
				if opened != want {
					t.Fatalf("click (%d,%d): opened=%q want=%q", x, y, opened, want)
				}
				if m.currentLocation() != before {
					t.Fatal("mouse moved frozen document")
				}
			})
		}
	}
}

func TestTargetPanelCoveredCandidatesEnterAndLiteralHints(t *testing.T) {
	for _, key := range []string{"enter", "j", "k"} {
		t.Run(key, func(t *testing.T) {
			m := crowdedTargetModel(t, 80)
			press(m, "p")
			var opened string
			m.openURL = func(dest string) error { opened = dest; return nil }
			index := 59
			if key != "enter" {
				index = strings.Index(hintAlphabet, key)
			}
			want := m.targets.targets[index].dest
			var cmd tea.Cmd
			if key == "enter" {
				m.moveTargetFocus(index)
				if m.targets.targets[index].regions[0].line-m.vp.YOffset() < m.targetPanelY() {
					t.Fatal("precondition: target must be covered")
				}
				cmd = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				press(m, "a")
				cmd = press(m, key)
			}
			applyMouseCmd(m, cmd)
			if opened != want || m.targets.active {
				t.Fatalf("activation=%q want=%q", opened, want)
			}
		})
	}
}

func TestTargetPanelFocusHighlightComposesWithSearch(t *testing.T) {
	for _, query := range []string{"", "needle", "ee"} {
		t.Run(query, func(t *testing.T) {
			lines := []string{linkLine("one", "https://one.example", "needle") + " " + linkLine("two", "https://two.example", "needle")}
			m := newHintModel(t, 80, 24, lines)
			m.search.query = query
			m.refreshSearch()
			original := m.vp.GetContent()
			press(m, "p")
			for _, code := range []rune{tea.KeyDown, tea.KeyUp} {
				pressKey(m, tea.KeyPressMsg{Code: code})
				spans := m.targetSpans()[0]
				if len(spans) != 1 || spans[0].start != m.targets.targets[m.targets.focus].regions[0].start {
					t.Fatal("more than focused occurrence highlighted")
				}
				content := m.vp.GetContent()
				if query != "" && (countSGRWith(content, "45") == 0 || countSGRWith(content, "43") == 0) {
					t.Fatal("focus erased overlapping search")
				}
				if !reflect.DeepEqual(parseLinkTargets([]string{content}), parseLinkTargets(lines)) {
					t.Fatal("focus changed stock OSC target regions")
				}
				if !reflect.DeepEqual(m.base, lines) {
					t.Fatal("cache mutated")
				}
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.vp.GetContent() != original {
				t.Fatal("focus attributes survived cancellation")
			}
		})
	}
}

func TestTargetPanelSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {20, 8}, {29, 24}, {30, 1}, {30, 2}, {30, 3}, {40, 7}, {40, 8}, {80, 24}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := newHintModel(t, size[0], size[1], []string{linkLine("one", "https://example.com", "guide")})
			m.openURL = func(string) error { t.Fatal("unexpected opener"); return nil }
			press(m, "p")
			want := size[0] >= 30 && size[1] >= 2
			if m.targets.active != want {
				t.Fatalf("active=%v want=%v", m.targets.active, want)
			}
			if !want {
				return
			}
			rows := strings.Split(m.View().Content, "\n")
			if len(rows) != size[1] {
				t.Fatalf("height=%d want=%d", len(rows), size[1])
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > size[0] {
					t.Fatalf("overflow: %q", row)
				}
			}
			if !strings.Contains(ansi.Strip(rows[len(rows)-1]), "Tab/Enter/Esc") {
				t.Fatal("essential controls lost")
			}
			if size[1] < 8 && len(m.targetPanelRows()) != 1 {
				t.Fatal("short terminal must use compact focus row")
			}
		})
	}
}

func TestTargetPanelInvalidation(t *testing.T) {
	for _, event := range []string{"resize height", "resize width", "reload", "reload error", "render", "render error", "opener success", "opener error", "source", "request render", "ctrl+c"} {
		t.Run(event, func(t *testing.T) {
			m := crowdedTargetModel(t, 80)
			press(m, "p")
			var msg tea.Msg
			switch event {
			case "resize height":
				msg = tea.WindowSizeMsg{Width: 80, Height: 12}
			case "resize width":
				msg = tea.WindowSizeMsg{Width: 40, Height: 24}
			case "reload":
				msg = reloadDoneMsg{body: []byte("replacement")}
			case "reload error":
				msg = reloadDoneMsg{err: errors.New("read failed")}
			case "render":
				msg = renderedMsg{content: "replacement", stripped: []string{"replacement"}, gen: m.gen}
			case "render error":
				msg = renderedMsg{err: errors.New("render failed"), gen: m.gen}
			case "opener success":
				msg = openedURLMsg{dest: "https://example.com"}
			case "opener error":
				msg = openedURLMsg{err: errors.New("open failed")}
			case "source":
				m.toggleSource()
			case "request render":
				m.requestRender()
			case "ctrl+c":
				cmd := pressKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				if cmd == nil {
					t.Fatal("ctrl+c swallowed")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("ctrl+c did not quit")
				}
				return
			}
			if msg != nil {
				nm, _ := m.Update(msg)
				*m = *nm.(*Model)
			}
			if m.targets.active || len(m.targets.targets) != 0 || strings.Contains(m.vp.GetContent(), targetHL) {
				t.Fatal("stale picker survived event")
			}
			if cmd := pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
				t.Fatal("stale focused activation survived event")
			}
			if len(strings.Split(m.View().Content, "\n")) != m.height {
				t.Fatal("notice/status boundary drift")
			}
		})
	}
}

// Optional local evidence uses the same production render, target parser and View.
// It never requires example.md and never invokes an opener.
func TestTargetPanelRenderedSamples(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			src := "# Link choices\n\n[Quick start](https://example.com/guide?track=quick#install) and [Full guide](https://example.com/guide?track=full#install).\n\n| Topic | Destination |\n| - | - |\n"
			for i := range 12 {
				src += fmt.Sprintf("| Choice %d | [Guide %d](https://example.com/guide?choice=%d#details) |\n", i, i, i)
			}
			src += "\nSee the note[^note].\n\n[^note]: Internal definition.\n"
			m := New(src, "choices.md")
			if err := m.SetStyle("auto"); err != nil {
				t.Fatal(err)
			}
			nm, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			*m = *nm.(*Model)
			settle(t, m, cmd)
			m.openURL = func(string) error { t.Fatal("unexpected opener"); return nil }
			originalLinks := append([]linkTarget(nil), m.links...)
			press(m, "p")
			if !m.targets.active || len(m.targets.targets) < 2 {
				t.Fatal("real OSC targets unavailable")
			}
			m.moveTargetFocus(1)
			if !reflect.DeepEqual(originalLinks, m.links) || !reflect.DeepEqual(originalLinks, renderedTargets(src, strings.Split(m.vp.GetContent(), "\n"), m.stripped)) {
				t.Fatal("real target metadata changed")
			}
			if dir := os.Getenv("READMD_PICKER_SAMPLES"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				for suffix, content := range map[string]string{"ansi": m.View().Content, "txt": ansi.Strip(m.View().Content), "md": src} {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("bottom-list-%d.%s", width, suffix)), []byte(content), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestTargetPanelTextKeepsTableLabels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target hintTarget
		want   string
	}{
		{"wrapped stock destination", hintTarget{linkTarget: linkTarget{dest: "https://example.com/path", texts: []string{"Full guide", "https://example.com/", "path"}}}, "Full guide"},
		{"table label ending in URL", hintTarget{linkTarget: linkTarget{id: tableLinkPrefix + "one", dest: "https://example.com", texts: []string{"Visit", "https://example.com"}}}, "Visit https://example.com"},
		{"wrapped autolink", hintTarget{linkTarget: linkTarget{dest: "https://example.com/path", texts: []string{"https://example.com/", "path"}}}, "https://example.com/ path"},
		{"footnote", hintTarget{linkTarget: linkTarget{kind: targetFootnote, footnote: "note"}}, "footnote note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetText(tc.target); got != tc.want {
				t.Fatalf("text=%q want=%q", got, tc.want)
			}
		})
	}
}

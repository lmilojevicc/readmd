package pager

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTargetPanelPreservesOriginalNoticeBoundary(t *testing.T) {
	for _, notice := range []string{"flash", "error"} {
		for _, height := range []int{3, 8, 24} {
			t.Run(fmt.Sprintf("%s/%d", notice, height), func(t *testing.T) {
				lines := []string{linkLine("visible", "https://visible.example", "visible")}
				for len(lines) < height-2 {
					lines = append(lines, "filler")
				}
				lines = append(lines, linkLine("covered", "https://covered.example", "covered"))
				m := newHintModel(t, 80, height, lines)
				if notice == "flash" {
					m.flash = "old notice"
				} else {
					m.errMsg = "old notice"
				}
				m.openURL = func(string) error { t.Fatal("unexpected opener"); return nil }
				before := m.View().Content
				location, vpHeight := m.currentLocation(), m.vp.Height()
				press(m, "p")
				if len(m.targets.targets) != 1 || m.targets.targets[0].id != "visible" {
					t.Fatal("notice clearing exposed an extra candidate")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
				sendMouseWheel(m, tea.MouseWheelDown)
				if m.currentLocation() != location || m.vp.Height() != vpHeight {
					t.Fatal("notice changed frozen geometry")
				}
				if len(strings.Split(m.View().Content, "\n")) != height {
					t.Fatal("notice added an extra screen row")
				}
				if cmd := sendMouseClick(m, 0, height-1); cmd != nil {
					t.Fatal("status clicked through")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.View().Content != before {
					t.Fatal("Esc did not restore original notice and document exactly")
				}
			})
		}
	}
}

func TestTargetPanelFootnoteAction(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newRenderedModel(t, "Reference[^note].\n\n"+strings.Repeat("filler\n\n", 15)+"[^note]: Definition body.\n", width, 12)
			m.openURL = func(string) error { t.Fatal("footnote used external opener"); return nil }
			press(m, "p")
			target, ok := m.focusedTarget()
			if !ok || target.kind != targetFootnote || !strings.Contains(targetAction(target), "Jump to footnote note") {
				t.Fatalf("footnote is not described as internal jump: target=%#v links=%#v render=%q", target, m.links, m.stripped)
			}
			before := m.currentLocation()
			if cmd := pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
				t.Fatal("footnote returned external command")
			}
			if len(m.locations) != 1 {
				t.Fatal("footnote did not preserve back history")
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
			if m.currentLocation() != before {
				t.Fatal("footnote return moved document")
			}
		})
	}
}

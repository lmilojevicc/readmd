package pager

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTargetCoordinatesRetainedWideGrapheme(t *testing.T) {
	for _, cluster := range []string{"界", "👨‍👩‍👧‍👦"} {
		for _, reader := range []bool{false, true} {
			for _, safe := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/reader=%v/safe=%v", cluster, reader, safe), func(t *testing.T) {
					line := cluster + " " + linkLine("token", "https://example.com", "token")
					if !safe {
						line += strings.Repeat("x", 80)
					}
					m := newHintModel(t, 40, 12, []string{line, strings.Repeat("x", 100)})
					m.reader = reader
					m.syncVPWidth()
					m.vp.SetXOffset(1)
					location := m.currentLocation()
					margin := 0
					if reader {
						_, margin = readerGeom(m.width, true)
					}
					secondRow := strings.TrimSpace(ansi.Strip(strings.Split(m.vp.View(), "\n")[1]))
					if (secondRow == strings.Repeat("x", m.vp.Width())) != safe {
						t.Fatalf("stock physical second row=%q safe=%v", secondRow, safe)
					}
					opened := 0
					m.openURL = func(string) error { opened++; return nil }
					press(m, "p")
					if m.targets.active != safe {
						t.Fatalf("picker active=%v safe=%v", m.targets.active, safe)
					}
					if !safe {
						if m.flash != targetPanNotice {
							t.Fatal("picker refusal lacks pan recovery")
						}
						if cmd := sendMouseClick(m, margin+3, 0); cmd != nil || opened != 0 {
							t.Fatal("unsafe document coordinates activated")
						}
						if m.currentLocation() != location {
							t.Fatal("refusal altered pan/scroll")
						}
						press(m, "0")
						press(m, "p")
						if !m.targets.active {
							t.Fatal("press 0 did not recover safe picker")
						}
						return
					}
					if m.targets.rows[0].left != 0 {
						t.Fatal("retained grapheme origin not captured")
					}
					if cmd := sendMouseClick(m, margin+2, 0); cmd != nil {
						t.Fatal("space before target mapped one cell too far right")
					}
					applyMouseCmd(m, sendMouseClick(m, margin+3, 0))
					if opened != 1 {
						t.Fatal("visible target missed actual screen origin")
					}
					// Normal document clicks use the same safe origin without a picker.
					applyMouseCmd(m, sendMouseClick(m, margin+3, 0))
					if opened != 2 {
						t.Fatal("normal document origin differs from picker")
					}
				})
			}
		}
	}
}

func TestTargetCoordinatesExcludeWhollyRightClippedGrapheme(t *testing.T) {
	for _, cluster := range []string{"界", "👨‍👩‍👧‍👦"} {
		t.Run(cluster, func(t *testing.T) {
			m := newHintModel(t, 40, 12, []string{strings.Repeat("x", 39) + linkLine("wide", "https://example.com", cluster)})
			m.openURL = func(string) error { t.Fatal("wholly clipped target opened"); return nil }
			press(m, "p")
			if m.targets.active || m.flash != "no visible targets" {
				t.Fatal("wholly clipped grapheme entered picker")
			}
			if cmd := sendMouseClick(m, 39, 0); cmd != nil {
				t.Fatal("blank clipped cell activated target")
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
			press(m, "p")
			if !m.targets.active {
				t.Fatal("panning did not expose target")
			}
		})
	}
}

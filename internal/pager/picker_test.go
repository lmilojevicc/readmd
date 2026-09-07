package pager

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPickerSharedLifecycle(t *testing.T) {
	for _, design := range []pickerDesign{pickerList, pickerVimium} {
		for _, mouse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/mouse=%v", design, mouse), func(t *testing.T) {
				var lines []string
				for i := range len(hintAlphabet) + 1 {
					lines = append(lines, linkLine(fmt.Sprint(i), fmt.Sprintf("https://example.com/%d?q=a#fragment", i), "linked")+"     ")
				}
				m := newHintModel(t, 80, 40, lines)
				m.picker = design
				m.mouse = mouse
				before, location := m.View().Content, m.currentLocation()
				cache := append([]string(nil), m.base...)
				opened := ""
				m.openURL = func(dest string) error { opened = dest; return nil }
				press(m, "p")
				if !m.targets.active || len(m.targets.targets) != 27 {
					t.Fatalf("entry: %+v", m.targets)
				}
				snapshot := append([]hintTarget(nil), m.targets.targets...)
				if m.targets.targets[0].label != "AA" || m.targets.targets[26].label != "SA" {
					t.Fatal("design changed shared labels")
				}
				press(m, "a")
				if !m.targets.active || m.targets.prefix != "a" {
					t.Fatal("prefix did not filter")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
				if m.targets.prefix != "" {
					t.Fatal("Backspace did not edit")
				}
				if !reflect.DeepEqual(snapshot, m.targets.targets) || !reflect.DeepEqual(cache, m.base) {
					t.Fatal("filter mutated frozen candidates/cache")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.View().Content != before || m.currentLocation() != location {
					t.Fatal("Esc did not restore exact original view")
				}
				// Both designs keep j/k/s/p as literal hint letters, not normal commands.
				for _, key := range []string{"j", "k", "s", "p"} {
					press(m, "p")
					cmd := press(m, key)
					if key == "s" {
						if cmd == nil || m.targets.active {
							t.Fatal("unique s prefix did not activate")
						}
						settle(t, m, cmd)
					} else {
						if m.targets.prefix != key || m.currentLocation() != location {
							t.Fatal("hint letter moved viewport")
						}
						pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
					}
				}
				if opened != "https://example.com/26?q=a#fragment" {
					t.Fatalf("URL lost query/fragment: %q", opened)
				}
				press(m, "t")
				press(m, "p")
				if cmd := sendMouseClick(m, 0, m.height-1); cmd != nil {
					t.Fatal("status clicked through")
				}
				if !mouse {
					if cmd := sendMouseClick(m, 0, 0); cmd != nil || !m.targets.active {
						t.Fatal("mouse-off click activated picker")
					}
				}
			})
		}
	}
}

func TestPickerSharedInvalidation(t *testing.T) {
	for _, design := range []pickerDesign{pickerList, pickerVimium} {
		for _, event := range []string{"render pending", "notice", "error notice", "reload", "reload error", "opener", "resize", "render error"} {
			t.Run(fmt.Sprintf("%d/%s", design, event), func(t *testing.T) {
				m := newHintModel(t, 80, 12, []string{linkLine("one", "https://one.example", "one")})
				m.picker = design
				if event == "render pending" {
					m.rendering = true
					press(m, "p")
					if m.targets.active || m.flash != "targets unavailable" {
						t.Fatal("in-flight render admitted picker")
					}
					return
				}
				press(m, "p")
				if !m.targets.active {
					t.Fatal("precondition")
				}
				switch event {
				case "notice":
					m.flash = "notice"
					m.View()
				case "error notice":
					m.errMsg = "notice"
					m.View()
				case "reload":
					m.Update(reloadDoneMsg{body: []byte("replacement")})
				case "reload error":
					m.Update(reloadDoneMsg{err: errors.New("failed")})
				case "opener":
					m.Update(openedURLMsg{dest: "https://example.com"})
				case "resize":
					m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
				case "render error":
					m.Update(renderedMsg{err: errors.New("failed"), gen: m.gen})
				}
				if m.targets.active {
					t.Fatal("event left stale picker active")
				}
			})
		}
	}
}

func TestPickerSharedFootnoteAndPrompts(t *testing.T) {
	for _, design := range []pickerDesign{pickerList, pickerVimium} {
		for _, mode := range []string{"footnote", "search", "outline", "help"} {
			t.Run(fmt.Sprintf("%d/%s", design, mode), func(t *testing.T) {
				m := newRenderedModel(t, "# Topic\n\nReference[^n].\n\n"+strings.Repeat("filler\n\n", 20)+"[^n]: Definition body.\n", 80, 12)
				m.picker = design
				m.openURL = func(string) error { t.Fatal("internal reference opened externally"); return nil }
				if mode != "footnote" {
					switch mode {
					case "search":
						press(m, "/")
					case "outline":
						press(m, "o")
						press(m, "/")
					case "help":
						press(m, "?")
						press(m, "/")
					}
					for _, key := range []string{"s", "p", "m"} {
						press(m, key)
					}
					if m.targets.active || !m.mouse {
						t.Fatal("prompt forwarded picker/mouse keys")
					}
					got := m.search.query
					if mode == "outline" {
						got = m.tocFilter
					}
					if mode == "help" {
						got = m.helpFilter
					}
					if got != "spm" {
						t.Fatalf("prompt input=%q", got)
					}
					return
				}
				before := m.currentLocation()
				press(m, "p")
				if len(m.targets.targets) != 1 || m.targets.targets[0].kind != targetFootnote {
					t.Fatalf("footnote omitted: targets=%#v links=%#v rendered=%q", m.targets.targets, m.links, m.stripped)
				}
				press(m, strings.ToLower(m.targets.targets[0].label))
				if m.targets.active || len(m.locations) != 1 {
					t.Fatal("footnote activation failed")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
				if m.currentLocation() != before {
					t.Fatal("footnote back lost original geometry")
				}
			})
		}
	}
}

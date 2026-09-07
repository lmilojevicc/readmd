package pager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"readmd/internal/config"
)

func TestConfigureStartup(t *testing.T) {
	for _, picker := range []string{"list", "vimium"} {
		for _, enabled := range []bool{false, true} {
			for _, supported := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/enabled=%v/supported=%v", picker, enabled, supported), func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					t.Setenv("XDG_CONFIG_HOME", t.TempDir())
					for _, key := range []string{"TMUX", "KITTY_WINDOW_ID", "WEZTERM_PANE", "GHOSTTY_RESOURCES_DIR"} {
						t.Setenv(key, "")
					}
					t.Setenv("TERM", "xterm")
					if supported {
						t.Setenv("TERM", "xterm-kitty")
					}
					c := config.Defaults()
					c.Style = "light"
					c.Picker = picker
					c.Mouse = enabled
					c.Reader = true
					c.ReaderWidth = 72
					c.TableCellWidth = 19
					c.Images = enabled
					c.RemoteImages = enabled
					m := New("# Heading\n", "doc.md")
					m.SetPath("docs/doc.md")
					if err := m.Configure(c); err != nil {
						t.Fatal(err)
					}
					defer m.Close()
					wantPicker := pickerList
					if picker == "vimium" {
						wantPicker = pickerVimium
					}
					if m.style != "light" || m.picker != wantPicker || m.mouse != enabled || !m.reader || m.readerWidth != 72 || m.tableCellWidth != 19 || m.imgCfg.NoImages == enabled || m.imgCfg.NoRemote == enabled || m.imgCfg.DocDir != "docs" || m.gfx != (enabled && supported) {
						t.Fatalf("startup settings not applied: %+v", m)
					}
					wantMouse := tea.MouseModeNone
					if enabled {
						wantMouse = tea.MouseModeCellMotion
					}
					if m.View().MouseMode != wantMouse {
						t.Fatal("empty View ignored mouse config")
					}
					_, cmd := m.Update(tea.WindowSizeMsg{Width: 140, Height: 14})
					settle(t, m, cmd)
					if m.vp.Width() != 72 || m.View().MouseMode != wantMouse {
						t.Fatal("rendered View ignored initial settings")
					}
					press(m, "m")
					settle(t, m, press(m, "r"))
					if m.mouse == enabled || m.reader || c.Mouse != enabled || !c.Reader {
						t.Fatal("session toggle failed or mutated startup settings")
					}
				})
			}
		}
	}
}

func TestConfiguredReaderGeometry(t *testing.T) {
	for _, preference := range []int{1, 60, 120, 10000} {
		for _, width := range []int{1, 2, 40, 140} {
			t.Run(fmt.Sprintf("%d/%d", preference, width), func(t *testing.T) {
				m := New("", "")
				m.reader = true
				m.readerWidth = preference
				m.width = width
				m.syncVPWidth()
				want := max(1, min(preference, width-2))
				w, margin := m.readerGeom(true)
				if w != want || m.vp.Width() != want || margin != max(0, (width-want)/2) {
					t.Fatalf("w=%d vp=%d margin=%d", w, m.vp.Width(), margin)
				}
			})
		}
	}
}

func TestConfiguredTableWidthAndRenderSnapshot(t *testing.T) {
	prose := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango"
	atom := "AtomicEndpoint.internal.example:8443"
	src := "| Body | Atomic | Complete Header Wider Than Preference |\n| - | - | - |\n| " + prose + " | " + atom + " | x |\n"
	for _, style := range []string{"auto", "notty", "dark", "light"} {
		for _, reader := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reader=%v", style, reader), func(t *testing.T) {
				var previousRows int
				for _, cap := range []int{18, 40, 70} {
					c := config.Defaults()
					c.Style = style
					c.Reader = reader
					c.ReaderWidth = 65
					c.TableCellWidth = cap
					c.Images = false
					m := New(src, "table.md")
					if err := m.Configure(c); err != nil {
						t.Fatal(err)
					}
					defer m.Close()
					_, cmd := m.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
					// A command must own its complete settings snapshot, not read the model.
					m.tableCellWidth = 9
					m.readerWidth = 3
					settle(t, m, cmd)
					widths, headers, rows := tableGrid(t, strings.Join(m.base, "\n"), 1)
					if widths[0] != cap+2 || widths[1] != len(atom)+2 || headers[2] != "Complete Header Wider Than Preference" || rows[0][0] != prose || rows[0][1] != atom {
						t.Fatalf("cap=%d widths=%v headers=%v rows=%v", cap, widths, headers, rows)
					}
					if previousRows > 0 && len(m.base) >= previousRows {
						t.Fatalf("cap=%d did not reduce actual frame rows: %d >= %d", cap, len(m.base), previousRows)
					}
					previousRows = len(m.base)
					if reader && m.vp.Width() != 65 {
						t.Fatal("configured reader viewport lost initial geometry")
					}
					// Reader framing never changes table cells or prose rendering width.
					wrapWidth := 140
					if reader {
						wrapWidth = 0
					}
					want, _, _, err := renderDoc(imgCtx{}, src, wrapWidth, m.style, cap)
					if err != nil || strings.Join(m.base, "\n") != want {
						t.Fatal("async render differs from configured direct table render")
					}
				}
			})
		}
	}
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprintf("default/%d", width), func(t *testing.T) {
			defaultOut, err := Render(src, width)
			if err != nil {
				t.Fatal(err)
			}
			explicit, _, _, err := renderDoc(imgCtx{}, src, width, "notty", 40)
			if err != nil || defaultOut != explicit {
				t.Fatal("stable Render default changed")
			}
		})
	}
}

func TestConfiguredReaderProseRemainsNatural(t *testing.T) {
	src := "# Topic\n\n" + strings.Repeat("ordinary prose flows naturally ", 20) + "\n"
	var baseline []string
	for _, cap := range []int{30, 72, 120} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			c := config.Defaults()
			c.Reader = true
			c.ReaderWidth = cap
			c.Images = false
			m := New(src, "reader.md")
			if err := m.Configure(c); err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 140, Height: 12})
			settle(t, m, cmd)
			if baseline == nil {
				baseline = append([]string(nil), m.base...)
			} else if !reflect.DeepEqual(m.base, baseline) {
				t.Fatal("reader width reflowed prose")
			}
			if m.vp.Width() != cap || m.widest <= cap {
				t.Fatal("reader width did not only change viewport geometry")
			}
			margin := strings.Repeat(" ", (140-cap)/2)
			if !strings.HasPrefix(bodyOf(m), margin) {
				t.Fatal("configured centered margin missing")
			}
		})
	}
}

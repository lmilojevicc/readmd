package pager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

func TestTableFullFrame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		rows    [][]string
	}{
		{"header only", []string{"Header", ""}, nil},
		{"empty header only", []string{"", ""}, nil},
		{"empty cells", []string{"", "N", ""}, [][]string{{"", "", ""}}},
		{"single-line rows", []string{"Name", "N", "Code"}, [][]string{{"東京 👨‍👩‍👧‍👦", "7", "a \\| b"}, {"e\u0301", "", "p\\|q"}}},
		{"multiline final row", []string{"Narrative", "N"}, [][]string{{"first", "x"}, {strings.Repeat("alpha bravo charlie delta ", 5) + "FINAL", ""}}},
	} {
		for _, style := range []string{"auto", styles.DarkStyle, styles.LightStyle, styles.NoTTYStyle} {
			t.Run(tc.name+"/"+style, func(t *testing.T) {
				resolved, err := resolveStyle(style)
				if err != nil {
					t.Fatal(err)
				}
				var first string
				for _, width := range []int{40, 80, 120} {
					out, _, _, err := renderDoc(imgCtx{}, markdownTable(tc.headers, tc.rows), width, resolved)
					if err != nil {
						t.Fatal(err)
					}
					lines, separators := tableFrame(t, out)
					widths, headers, rows := tableGrid(t, out, len(tc.rows))
					if !reflect.DeepEqual(headers, tc.headers) {
						t.Fatalf("headers=%q want %q", headers, tc.headers)
					}
					for r, row := range tc.rows {
						for c, cell := range row {
							want := strings.ReplaceAll(cell, "\\|", "|")
							if rows[r][c] != want {
								t.Fatalf("row%d col%d=%q want %q", r, c, rows[r][c], want)
							}
						}
					}
					topLeft, topRight, bottomLeft, bottomRight := "╭", "╮", "╰", "╯"
					horizontal, vertical, top, middle, bottom, left, right := "─", "│", "┬", "┼", "┴", "├", "┤"
					if style == styles.NoTTYStyle {
						topLeft, topRight, bottomLeft, bottomRight = "+", "+", "+", "+"
						horizontal, vertical, top, middle, bottom, left, right = "-", "|", "+", "+", "+", "+", "+"
					}
					rule := func(left, junction, right string) string {
						parts := make([]string, len(widths))
						for i, width := range widths {
							parts[i] = strings.Repeat(horizontal, width)
						}
						return strings.Repeat(" ", separators[0]) + left + strings.Join(parts, junction) + right
					}
					for _, check := range []struct {
						line int
						want string
					}{{0, rule(topLeft, top, topRight)}, {2, rule(left, middle, right)}, {len(lines) - 1, rule(bottomLeft, bottom, bottomRight)}} {
						if lines[check.line] != check.want {
							t.Fatalf("line%d=%q want %q", check.line, lines[check.line], check.want)
						}
					}
					dividers := 0
					for _, line := range lines[1 : len(lines)-1] {
						if line == rule(left, middle, right) {
							dividers++
							continue
						}
						for _, col := range separators {
							if ansi.Cut(line, col, col+1) != vertical {
								t.Fatalf("missing rail at %d: %q", col, line)
							}
						}
					}
					wantDividers := 1
					if tc.name == "multiline final row" {
						wantDividers = 2
						if !strings.Contains(lines[len(lines)-2], "FINAL") {
							t.Fatal("bottom frame truncated final continuation")
						}
					} else if len(lines) != len(tc.rows)+4 {
						t.Fatalf("unexpected physical rows: %q", lines)
					}
					if dividers != wantDividers {
						t.Fatalf("dividers=%d want %d", dividers, wantDividers)
					}
					if first != "" && out != first {
						t.Fatalf("viewport%d changed intrinsic dimensions", width)
					}
					first = out
				}
			})
		}
	}
}

func TestTableFrameAlignment(t *testing.T) {
	for _, style := range []string{paletteStyleName, styles.DarkStyle, styles.LightStyle, styles.NoTTYStyle} {
		t.Run(style, func(t *testing.T) {
			out, _, _, err := renderDoc(imgCtx{}, "| Left | Center | Right |\n| :--- | :---: | ---: |\n| x | 中 | z |\n", 40, style)
			if err != nil {
				t.Fatal(err)
			}
			lines, separators := tableFrame(t, out)
			for col, want := range []string{" x    ", "   中   ", "     z "} {
				if got := ansi.Cut(lines[3], separators[col]+1, separators[col+1]); got != want {
					t.Fatalf("col%d=%q want %q", col, got, want)
				}
			}
		})
	}
}

func TestTableFrameNavigation(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, reader := range []bool{false, true} {
			for _, style := range []string{paletteStyleName, styles.DarkStyle, styles.LightStyle, styles.NoTTYStyle} {
				t.Run(fmt.Sprintf("w%d/reader%v/%s", width, reader, style), func(t *testing.T) {
					dest := "https://example.org/full?target=preserved"
					label := strings.Repeat("alpha bravo charlie delta ", 3) + "pipe \\| END"
					tableSource := markdownTable([]string{"[FIRST](" + dest + ") [^n]", "Atomic", "[LAST](" + dest + ")"}, [][]string{
						{"[" + label + "](" + dest + ") [^n]", strings.Repeat("token", 26), "last [^n]"},
						{"end", "x", "[" + label + "](" + dest + ")"},
					})
					src := tableSource + "\n" + strings.Repeat("filler\n\n", 15) + "[^n]: note body\n\n" + strings.Repeat("tail\n\n", 8)
					m := newRenderedModel(t, src, width, 14)
					m.style = style
					settle(t, m, m.requestRender())
					if reader {
						settle(t, m, press(m, "r"))
					}
					out, _, _, err := renderDoc(imgCtx{}, tableSource, width, style)
					if err != nil {
						t.Fatal(err)
					}
					lines, separators := tableFrame(t, out)
					top := -1
					for i, line := range m.stripped {
						if line == lines[0] {
							top = i
							break
						}
					}
					if top < 0 || len(m.links) != 7 || countFootnoteTargets(m.links) != 3 {
						t.Fatalf("missing frame/targets: top=%d links=%#v", top, m.links)
					}
					first := m.links[0].regions[0]
					if first.line != top+1 || first.start != separators[0]+2 || ansi.Cut(m.stripped[first.line], first.start, first.end) != "FIRST" {
						t.Fatalf("header link did not follow actual outer frame: %#v", first)
					}
					bounds := renderedTableCells(m.base)[tableCellID(0, 1, 0)]
					if len(bounds) < 2 || bounds[0].line != top+3 || bounds[0].start != separators[0]+2 {
						t.Fatalf("cell provenance did not follow frame: %#v", bounds)
					}
					original := strings.Join(m.base, "\n")
					margin := 0
					if reader {
						_, margin = readerGeom(width, true)
					}
					// Check every rule column and every content rail, including clipped
					// corners under pan. Literal pipes inside links remain actionable.
					for y, line := range lines {
						cols := separators
						if y == 0 || y == len(lines)-1 || line == lines[2] {
							cols = nil
							for x := separators[0]; x <= separators[len(separators)-1]; x++ {
								cols = append(cols, x)
							}
						}
						for _, x := range cols {
							for _, target := range m.links {
								for _, reg := range target.regions {
									if reg.line == top+y && reg.start <= x && x < reg.end {
										t.Fatalf("border became target: (%d,%d) %#v", x, top+y, target)
									}
								}
							}
							m.vp.SetYOffset(top + y)
							m.vp.SetXOffset(x)
							before := m.currentLocation()
							if cmd := sendMouseClick(m, margin+x-m.vp.XOffset(), top+y-m.vp.YOffset()); cmd != nil || m.currentLocation() != before || len(m.locations) != 0 {
								t.Fatalf("border click activated (%d,%d)", x, top+y)
							}
						}
					}
					for _, target := range m.links {
						for _, reg := range target.regions {
							m.vp.SetYOffset(reg.line)
							m.vp.SetXOffset(reg.start)
							before := m.currentLocation()
							opened := ""
							m.openURL = func(s string) error { opened = s; return nil }
							settle(t, m, sendMouseClick(m, margin+reg.start-m.vp.XOffset(), reg.line-m.vp.YOffset()))
							if target.kind == targetFootnote {
								if opened != "" || m.vp.YOffset() != target.definition.line {
									t.Fatal("framed footnote click missed definition")
								}
								pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
							} else if opened != dest {
								t.Fatal("framed link click missed destination")
							}
							opened = ""
							press(m, "p")
							hint := ""
							for _, h := range m.targets.targets {
								if (target.kind == targetExternal && h.id == target.id) || (target.kind == targetFootnote && reflect.DeepEqual(h.regions, target.regions)) {
									hint = h.label
								}
							}
							if hint == "" {
								t.Fatal("framed cell target not hintable")
							}
							settle(t, m, press(m, hint))
							if target.kind == targetFootnote {
								if opened != "" || m.vp.YOffset() != target.definition.line {
									t.Fatal("framed footnote hint missed definition")
								}
								pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
							} else if opened != dest {
								t.Fatal("framed link hint missed destination")
							}
							if m.currentLocation() != before {
								t.Fatal("target activation changed origin")
							}
						}
					}
					m.vp.SetYOffset(top)
					press(m, "0")
					if !strings.Contains(ansi.Strip(m.vp.View()), "╭") && style != styles.NoTTYStyle {
						t.Fatal("left cap not reachable")
					}
					if !strings.Contains(ansi.Strip(m.vp.View()), "FIRST") {
						t.Fatal("first column not reachable")
					}
					lastReachable := false
					for i := 0; i < separators[len(separators)-1]; i++ {
						press(m, "l")
						lastReachable = lastReachable || strings.Contains(ansi.Strip(m.vp.View()), "LAST")
					}
					if !lastReachable || (style != styles.NoTTYStyle && !strings.Contains(ansi.Strip(m.vp.View()), "╮")) {
						t.Fatal("last column/cap not reachable under l pan")
					}
					pan := m.vp.XOffset()
					press(m, "h")
					if m.vp.XOffset() >= pan {
						t.Fatal("h did not pan back")
					}
					press(m, "0")
					if m.vp.XOffset() != 0 || strings.Join(m.base, "\n") != original {
						t.Fatal("pan changed cache or failed to reset")
					}
				})
			}
		}
	}
}

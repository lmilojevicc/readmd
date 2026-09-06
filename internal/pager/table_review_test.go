package pager

import (
	"fmt"
	"strings"
	"testing"

	"reflect"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
)

// This probe exercises inert OSC 8 resets strictly OUTSIDE complete cell content.
// They carry metadata, never surround an inline element inside a real link.
func TestTableCellBoundaryOSCProof(t *testing.T) {
	for _, dest := range []string{"https://example.org/full?query=kept&other=yes", "mailto:person@example.org"} {
		t.Run(dest, func(t *testing.T) {
			original := ansi.SetHyperlink(dest, "id=real") + strings.Repeat("alpha bravo charlie delta ", 5) + "end" + ansi.ResetHyperlink()
			begin, end := ansi.SetHyperlink("", "id=readmd-cell-0-1-0"), ansi.SetHyperlink("", "id=readmd-cell-end")
			build := func(cell string) string {
				return table.New().Headers("H", "N").Row(wrapTableCell(cell, 40), "x").StyleFunc(func(_, col int) lipgloss.Style {
					w := 42
					if col == 1 {
						w = 3
					}
					return lipgloss.NewStyle().Width(w).Padding(0, 1)
				}).String()
			}
			plain, tagged := build(original), build(begin+original+end)
			if ansi.Strip(plain) != ansi.Strip(tagged) {
				t.Fatal("metadata changed visible output")
			}
			a, b := rawLinkRegions(strings.Split(plain, "\n")), rawLinkRegions(strings.Split(tagged, "\n"))
			if len(a) < 3 || !reflect.DeepEqual(a, b) {
				t.Fatalf("metadata altered real targets: %#v != %#v", a, b)
			}
			for _, reg := range b {
				line := strings.Split(tagged, "\n")[reg.line]
				if strings.Count(line, begin) != 1 || strings.Count(line, end) < 1 {
					t.Fatalf("continuation lost boundary metadata: %q", line)
				}
				start, finish, col, state := -1, -1, 0, byte(0)
				for rest := line; rest != ""; {
					seq, w, n, next := ansi.DecodeSequence(rest, state, nil)
					rest, state = rest[n:], next
					if seq == begin {
						start = col
					}
					if seq == end {
						finish = col
					}
					col += w
				}
				if start != reg.start || finish != reg.end {
					t.Fatalf("recovered bounds %d:%d != real region %#v", start, finish, reg)
				}
			}
			if !reflect.DeepEqual(parseLinkTargets(strings.Split(plain, "\n")), parseLinkTargets(strings.Split(tagged, "\n"))) {
				t.Fatal("metadata became actionable")
			}
		})
	}
}

func TestTableLinkedBadgesReview(t *testing.T) {
	for _, label := range []string{"badge", "build passing"} {
		for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
			t.Run(label+"/"+style, func(t *testing.T) {
				dest := "https://docs.example/full/path?query=complete&other=preserved"
				badge := "[![" + label + "](https://img.example/badge.svg)](" + dest + ")"
				src := markdownTable([]string{"Badge", "N"}, [][]string{{"before " + badge + " middle " + badge + " after", "x"}})
				m := newRenderedModel(t, src, 80, 15)
				m.style = style
				settle(t, m, m.requestRender())
				visible := strings.Join(m.stripped, "\n")
				if strings.Contains(visible, "https://") {
					t.Fatalf("spurious badge URL: %q", visible)
				}
				if len(m.links) != 2 {
					t.Fatalf("badge targets=%#v", m.links)
				}
				_, _, rows := tableGrid(t, strings.Join(m.base, "\n"), 1)
				if rows[0][0] != "before "+label+" middle "+label+" after" {
					t.Fatalf("badge label/context=%q", rows)
				}
				original := strings.Join(m.base, "\n")
				for _, target := range m.links {
					if target.dest != dest || !strings.Contains(strings.Join(target.texts, " "), label) {
						t.Fatalf("badge destination/label=%#v", target)
					}
					reg := target.regions[0]
					m.vp.SetYOffset(reg.line)
					m.vp.SetXOffset(reg.start)
					opened := ""
					m.openURL = func(s string) error { opened = s; return nil }
					settle(t, m, sendMouseClick(m, reg.start-m.vp.XOffset(), reg.line-m.vp.YOffset()))
					if opened != dest {
						t.Fatalf("mouse opened=%q", opened)
					}
					opened = ""
					press(m, "t")
					hint := ""
					for _, h := range m.targets.targets {
						if h.id == target.id {
							hint = h.label
						}
					}
					if hint == "" {
						t.Fatal("badge hint missing")
					}
					settle(t, m, press(m, hint))
					if opened != dest {
						t.Fatalf("hint opened=%q", opened)
					}
				}
				if original != strings.Join(m.base, "\n") {
					t.Fatal("badge activation changed cache")
				}
			})
		}
	}
}

func TestTableTabsBeforeMetricsReview(t *testing.T) {
	for _, tc := range []struct {
		name, header, cell, want string
		multiline                bool
	}{
		{"header", "a\tb", "x", "x", false},
		{"body", "H", "a\tb", "a    b", false},
		{"code", "H", "`a\tb`", "a    b", false},
		{"multiline", "H", "a\tb " + strings.Repeat("word ", 15) + "end", "a    b", true},
	} {
		for _, width := range []int{40, 80, 120, 0} {
			for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
				t.Run(fmt.Sprintf("%s/w%d/%s", tc.name, width, style), func(t *testing.T) {
					src := markdownTable([]string{tc.header, "Other"}, [][]string{{tc.cell, "first"}, {"next", "second"}})
					out, _, _, err := renderDoc(imgCtx{}, src, width, style)
					if err != nil {
						t.Fatal(err)
					}
					lines := strings.Split(strings.Trim(ansi.Strip(out), "\n"), "\n")
					if !strings.Contains(lines[1], strings.ReplaceAll(tc.header, "\t", "    ")) || !strings.Contains(lines[1], "Other") {
						t.Fatalf("broken header: %q", lines)
					}
					if len(lines) < 6 || !strings.Contains(lines[3], tc.want) {
						t.Fatalf("exact expanded cell missing: %q", lines)
					}
					if !tc.multiline && len(lines) != 6 {
						t.Fatalf("short tab made extra rows: %q", lines)
					}
					if tc.multiline && strings.Count(strings.Join(lines[2:len(lines)-1], "\n"), lines[2]) != 2 {
						t.Fatalf("missing logical row separator: %q", lines)
					}
					_, _, rows := tableGrid(t, out, 2)
					want := strings.Trim(tc.cell, "`")
					want = strings.Join(strings.Fields(want), " ")
					if rows[0][0] != want || rows[0][1] != "first" || rows[1][0] != "next" || rows[1][1] != "second" {
						t.Fatalf("lost cell tokens: %q", rows)
					}
				})
			}
		}
	}
}

func TestTableFootnoteCellOrderReview(t *testing.T) {
	for _, literal := range []string{"`[^n]`", `\[^n]`} {
		for _, width := range []int{40, 80, 120} {
			for _, reader := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/w%d/reader%v", literal, width, reader), func(t *testing.T) {
					src := markdownTable([]string{"Narrative", "Literal"}, [][]string{{strings.Repeat("ordinary readable explanation ", 7) + "real [^n]", literal}}) + "\n" + strings.Repeat("filler\n\n", 15) + "[^n]: note body\n\n" + strings.Repeat("tail\n\n", 8)
					m := newRenderedModel(t, src, width, 10)
					if reader {
						settle(t, m, press(m, "r"))
					}
					if countFootnoteTargets(m.links) != 1 {
						t.Fatalf("footnotes=%#v", m.links)
					}
					original := strings.Join(m.base, "\n")
					for _, target := range m.links {
						if target.kind != targetFootnote {
							continue
						}
						reg := target.regions[0]
						if !strings.Contains(m.stripped[reg.line], "real [^n]") {
							t.Fatalf("literal received real reference target: %#v line=%q", target, m.stripped[reg.line])
						}
						m.vp.SetYOffset(reg.line)
						m.vp.SetXOffset(reg.start)
						before := m.currentLocation()
						margin := 0
						if reader {
							_, margin = readerGeom(width, true)
						}
						if cmd := sendMouseClick(m, margin+reg.start-m.vp.XOffset(), reg.line-m.vp.YOffset()); cmd != nil {
							t.Fatal("footnote opened externally")
						}
						if m.vp.YOffset() != target.definition.line || len(m.locations) != 1 {
							t.Fatal("mouse missed definition")
						}
						pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
						if m.currentLocation() != before {
							t.Fatal("Backspace changed origin")
						}
						press(m, "t")
						hint := ""
						for _, h := range m.targets.targets {
							if h.kind == targetFootnote {
								hint = h.label
							}
						}
						if hint == "" {
							t.Fatal("real reference hint absent")
						}
						press(m, hint)
						if m.vp.YOffset() != target.definition.line {
							t.Fatal("hint missed definition")
						}
						pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
					}
					if original != strings.Join(m.base, "\n") {
						t.Fatal("footnote navigation changed cache")
					}
				})
			}
		}
	}
}

func TestExpandTableTabsPreservesControls(t *testing.T) {
	for _, controls := range []string{
		ansi.SetHyperlink("https://example.org/path\tpart?x=1", "id=\tkept"),
		"\x1bP0;1|payload\tkept\x1b\\",
		"\x1b]0;title\tkept\a",
	} {
		t.Run(fmt.Sprintf("%q", controls), func(t *testing.T) {
			got := expandTableTabs("a\tb" + controls + "c\td")
			if got != "a    b"+controls+"c    d" {
				t.Fatalf("visible tab expansion changed controls: %q", got)
			}
		})
	}
}

func TestTableFootnoteInterleavedCellsReview(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, reader := range []bool{false, true} {
			for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
				t.Run(fmt.Sprintf("w%d/reader%v/%s", width, reader, style), func(t *testing.T) {
					long := strings.Repeat("ordinary readable explanation ", 7)
					src := "Outside real-n[^n] and `literal-n[^n]`.\n\n" + markdownTable([]string{"No notes", "N"}, [][]string{{"first table", "x"}}) + "\n" +
						markdownTable([]string{"Header real-m[^m]", "Literal", "Last"}, [][]string{
							{long + "real-n[^n]", "`literal-n[^n]` real-m[^m]", "real-n[^n] " + long + "real-m[^m]"},
							{"real-m[^m] `literal-m[^m]` real-m[^m]", long + "real-n[^n]", `literal-n\[^n] literal-m\[^m]`},
						}) + "\nOutside real-m[^m] and literal-m\\[^m].\n\n" +
						markdownTable([]string{"Another", "N"}, [][]string{{long + "real-n[^n]", "`literal-n[^n]`"}}) + "\n" +
						strings.Repeat("filler\n\n", 15) + "[^m]: definition-m body\n\n[^n]: definition-n body\n\n" + strings.Repeat("tail\n\n", 8)
					m := newRenderedModel(t, src, width, 10)
					m.style = style
					settle(t, m, m.requestRender())
					if reader {
						settle(t, m, press(m, "r"))
					}
					if countFootnoteTargets(m.links) != 11 {
						markers, ok := sourceFootnoteMarkers(src)
						t.Fatalf("footnotes=%d want11: %#v; markers=%#v ok=%v render=%q", countFootnoteTargets(m.links), m.links, markers, ok, m.stripped)
					}
					original := strings.Join(m.base, "\n")
					margin := 0
					if reader {
						_, margin = readerGeom(width, true)
					}
					for _, target := range m.links {
						if target.kind != targetFootnote {
							continue
						}
						reg := target.regions[0]
						if !strings.HasSuffix(ansi.Cut(m.stripped[reg.line], 0, reg.start), "real-"+target.footnote) {
							t.Fatalf("literal actionable: %#v line=%q", target, m.stripped[reg.line])
						}
						if !strings.Contains(m.stripped[target.definition.line], "definition-"+target.footnote) {
							t.Fatalf("wrong definition: %#v", target)
						}
						m.vp.SetYOffset(reg.line)
						m.vp.SetXOffset(reg.start + 1)
						before := m.currentLocation()
						if cmd := sendMouseClick(m, margin+reg.start+1-m.vp.XOffset(), reg.line-m.vp.YOffset()); cmd != nil {
							t.Fatal("reference opened externally")
						}
						if m.vp.YOffset() != target.definition.line || len(m.locations) != 1 {
							t.Fatal("mouse missed definition")
						}
						pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
						if m.currentLocation() != before {
							t.Fatal("Backspace changed reader/pan position")
						}
						press(m, "t")
						hint := ""
						for _, h := range m.targets.targets {
							if reflect.DeepEqual(h.regions, target.regions) {
								hint = h.label
							}
						}
						if hint == "" {
							t.Fatal("reference not hintable")
						}
						press(m, hint)
						if m.vp.YOffset() != target.definition.line {
							t.Fatal("hint missed definition")
						}
						pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
					}
					for _, label := range []string{"n", "m"} {
						for _, regions := range renderedFootnoteMarkers(m.stripped, label) {
							reg := regions[0]
							if !strings.HasSuffix(ansi.Cut(m.stripped[reg.line], 0, reg.start), "literal-"+label) {
								continue
							}
							m.vp.SetYOffset(reg.line)
							m.vp.SetXOffset(reg.start)
							before := m.currentLocation()
							if cmd := sendMouseClick(m, margin+reg.start-m.vp.XOffset(), reg.line-m.vp.YOffset()); cmd != nil || len(m.locations) != 0 || before != m.currentLocation() {
								t.Fatal("literal click activated a target")
							}
							press(m, "t")
							for _, h := range m.targets.targets {
								for _, hit := range h.regions {
									if hit.line == reg.line && hit.start < reg.end && hit.end > reg.start {
										t.Fatal("literal marker is hintable")
									}
								}
							}
							press(m, "esc")
						}
					}
					if original != strings.Join(m.base, "\n") {
						t.Fatal("navigation mutated cached ANSI")
					}
					settle(t, m, press(m, "s"))
					settle(t, m, press(m, "s"))
					if original != strings.Join(m.base, "\n") || countFootnoteTargets(m.links) != 11 || m.source != src {
						t.Fatal("source round trip changed cache/source/targets")
					}
					if targets := parseFootnoteTargets(src, m.stripped); len(targets) != 0 {
						t.Fatal("missing cell provenance did not decline ambiguity")
					}
				})
			}
		}
	}
}

func TestTableCellProvenancePreservesRealLinks(t *testing.T) {
	for _, dest := range []string{"https://example.org/full/path?query=complete&other=preserved", "mailto:person@example.org?subject=full%20subject"} {
		for _, reader := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reader%v", dest, reader), func(t *testing.T) {
				label := strings.Repeat("alpha bravo charlie delta ", 5) + "end"
				src := markdownTable([]string{"Links", "Other"}, [][]string{{"[" + label + "](" + dest + ") [^n]", "`[^n]`"}}) + "\n[^n]: note body\n"
				m := newRenderedModel(t, src, 40, 10)
				if reader {
					settle(t, m, press(m, "r"))
				}
				if len(m.links) != 2 || countFootnoteTargets(m.links) != 1 {
					t.Fatalf("synthetic/missing targets: %#v", m.links)
				}
				bounds := renderedTableCells(m.base)[tableCellID(0, 1, 0)]
				if len(bounds) < 3 {
					t.Fatalf("missing continuation bounds: %#v", bounds)
				}
				var recovered []string
				for _, bound := range bounds {
					recovered = append(recovered, ansi.Cut(m.stripped[bound.line], bound.start, bound.end))
				}
				if strings.Join(recovered, " ") != label+" [^n]" {
					t.Fatalf("cell continuation recovery=%q", recovered)
				}
				original := strings.Join(m.base, "\n")
				for _, target := range m.links {
					if target.kind != targetExternal {
						continue
					}
					if target.dest != dest || strings.Join(target.texts, " ") != label {
						t.Fatalf("provenance changed link: %#v", target)
					}
					for _, reg := range target.regions {
						m.vp.SetYOffset(reg.line)
						m.vp.SetXOffset(reg.start + 1)
						margin := 0
						if reader {
							_, margin = readerGeom(m.width, true)
						}
						opened := ""
						m.openURL = func(s string) error { opened = s; return nil }
						settle(t, m, sendMouseClick(m, margin+reg.start+1-m.vp.XOffset(), reg.line-m.vp.YOffset()))
						if opened != dest {
							t.Fatalf("mouse opened=%q", opened)
						}
						opened = ""
						press(m, "t")
						hint := ""
						for _, h := range m.targets.targets {
							if h.id == target.id {
								hint = h.label
							}
						}
						if hint == "" {
							t.Fatal("continuation link hint missing")
						}
						settle(t, m, press(m, hint))
						if opened != dest {
							t.Fatalf("hint opened=%q", opened)
						}
					}
				}
				if original != strings.Join(m.base, "\n") {
					t.Fatal("provenance/navigation changed cache")
				}
			})
		}
	}
}

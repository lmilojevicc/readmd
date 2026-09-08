package pager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/text"
)

func themeRows(s string) [][]themeCell {
	cells := themeCells(s)
	var rows [][]themeCell
	for _, line := range strings.Split(s, "\n") {
		width := ansi.StringWidth(line)
		rows = append(rows, cells[:width])
		cells = cells[width:]
	}
	return rows
}

func TestThemeLinkAncestorScope(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, ancestor := range []struct{ name, open, close, yaml string }{
			{"strong", "**", "**", "strong: {bg: 4, italic: true}"},
			{"emphasis", "*", "*", "emphasis: {bg: 4, italic: true}"},
			{"combination", "***", "***", "strong: {bg: 4}\nemphasis: {bg: 2, italic: true}"},
		} {
			for _, link := range []struct{ name, src, token, target string }{
				{"autolink", "<https://example.com/full?q=1&v=2>", "https://example.com/full?q=1&v=2", "https://example.com/full?q=1&v=2"},
				{"email", "<a@example.com>", "a@example.com", "mailto:a@example.com"},
				{"generated URL", "[LABEL](https://example.com/full?q=1&v=2)", "https://example.com/full?q=1&v=2", "https://example.com/full?q=1&v=2"},
				{"struck label", "[~~LABEL~~](https://example.com)", "LABEL", "https://example.com"},
			} {
				for _, clear := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/clear=%t", base, ancestor.name, link.name, clear), func(t *testing.T) {
						src := ancestor.open + link.src + ancestor.close + " AFTER"
						yaml := ancestor.yaml
						if clear {
							yaml += "\nlinks: {fg: default, bg: none, bold: false, italic: false, underline: false, strikethrough: false}\nstrikethrough: {strikethrough: false}"
						}
						before := renderThemeTest(t, src, base, "", 120)
						out := renderThemeTest(t, src, base, yaml, 120)
						if ansi.Strip(before) != ansi.Strip(out) {
							t.Fatalf("visible layout changed:\n%q\n%q", ansi.Strip(before), ansi.Strip(out))
						}
						if !reflect.DeepEqual(themeOSC8(before), themeOSC8(out)) || !strings.Contains(out, link.target+"\a") {
							t.Fatal("OSC8 changed")
						}
						c := themeToken(t, out, link.token)
						bg := "44"
						if base == "notty" || clear {
							bg = "none"
						}
						if c.bg != bg || c.italic == clear {
							t.Fatalf("URL lost ancestor/clear: %+v output=%q", c, out)
						}
						if clear && (c.fg != "default" || c.bold || c.underline || c.strike) {
							t.Fatalf("explicit clears lost: %+v", c)
						}
						if tail := themeToken(t, out, "AFTER"); tail.bg != "none" || tail.italic {
							t.Fatalf("ancestor leaked: %+v", tail)
						}
					})
				}
			}
		}
	}
}

func TestThemeLinkPrecompositionParity(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, width := range []int{40, 80, 120} {
			for _, context := range []string{"%s", "# %s", "> %s", "- %s", "> - %s", "| A | B |\n|---|---|\n| %s | tail |"} {
				for _, inline := range []string{
					`**[A &amp; B &#92;* C \\path 界 é 👩‍💻](https://example.com/a?q=1&b=2) END**`,
					`*PRE [A **B** ~~C~~](https://example.com/escaped\_path) END*`,
					`**PRE <https://example.com> *<a@example.com>* END**`,
					"**" + strings.Repeat("long words ", 10) + "[LABEL](https://example.com) END**",
				} {
					t.Run(fmt.Sprintf("%s/%d/%s/%s", base, width, context, inline), func(t *testing.T) {
						src := fmt.Sprintf(context, inline) + "\n\nFOLLOWING"
						before := renderThemeTest(t, src, base, "", width)
						after := renderThemeTest(t, src, base, "strong: {bg: 4}\nemphasis: {bg: 4}", width)
						if ansi.Strip(before) != ansi.Strip(after) {
							t.Fatalf("precomposition changed layout:\n%q\n%q", ansi.Strip(before), ansi.Strip(after))
						}
						if !reflect.DeepEqual(themeOSC8(before), themeOSC8(after)) {
							t.Fatal("precomposition changed OSC8")
						}
						for _, token := range []string{"LABEL", "https://example.com", "a@example.com"} {
							if !strings.Contains(ansi.Strip(after), token) {
								continue
							}
							bg := "44"
							if base == "notty" {
								bg = "none"
							}
							if c := themeToken(t, after, token); c.bg != bg {
								t.Fatalf("%s lost role: %+v", token, c)
							}
						}
					})
				}
			}
		}
	}
}

func TestThemeContinuationMargins(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, context := range []string{"%s", "> %s", "- %s", "> - %s", "- > %s"} {
			for _, heading := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/heading=%t", base, context, heading), func(t *testing.T) {
					content := strings.Repeat("word ", 30) + "END"
					if heading {
						content = "# " + content
					} else {
						content = "**" + content + "**"
					}
					// Quotes/lists keep intrinsic width, but explicit quote source breaks
					// still exercise continuation indentation while an inline role is open.
					if strings.Contains(context, ">") && !heading {
						content = strings.Replace(content, "word word", "word\nword", 1)
					}
					continuation := strings.ReplaceAll(strings.TrimSuffix(context, "%s"), "- ", "  ")
					src := fmt.Sprintf(context, strings.ReplaceAll(content, "\n", "\n"+continuation)) + "\n\nFOLLOWING"
					before := renderThemeTest(t, src, base, "", 40)
					after := renderThemeTest(t, src, base, "strong: {bg: 4}", 40)
					if ansi.Strip(before) != ansi.Strip(after) {
						t.Fatal("layout changed")
					}
					oldRows, rows := themeRows(before), themeRows(after)
					for row, cells := range rows {
						old := oldRows[row]
						contentStart, contentEnd := -1, -1
						for col, c := range cells {
							if c.text == "w" && contentStart < 0 {
								contentStart = col
							}
							if c.text != " " {
								contentEnd = col + 1
							}
						}
						if contentStart < 0 {
							continue
						}
						for col, c := range cells {
							if col < contentStart || heading || col >= contentEnd {
								if c != old[col] {
									t.Errorf("stock state changed row=%d col=%d got=%+v want=%+v", row, col, c, old[col])
								}
							} else if !heading {
								bg := "44"
								if base == "notty" {
									bg = "none"
								}
								if c.bg != bg {
									t.Errorf("content role missing row=%d col=%d: %+v", row, col, c)
								}
							}
						}
					}
					if c := themeToken(t, after, "FOLLOWING"); c.bg != "none" {
						t.Fatalf("following prose background: %+v", c)
					}
				})
			}
		}
	}
}

func TestThemeStrikeState(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, tc := range []struct {
			name, src, yaml string
			tokens          map[string]bool
		}{
			{"true", "~~MARK~~ AFTER", "strikethrough: {strikethrough: true}", map[string]bool{"MARK": true, "AFTER": false}},
			{"false", "~~MARK~~ AFTER", "strikethrough: {strikethrough: false}", map[string]bool{"MARK": false, "AFTER": false}},
			{"restore parent", "**BEFORE ~~INNER~~ RESTORED** AFTER", "strong: {strikethrough: true}\nstrikethrough: {strikethrough: false}", map[string]bool{"BEFORE": true, "INNER": false, "RESTORED": true, "AFTER": false}},
			{"restore strike", "~~BEFORE *INNER* RESTORED~~ AFTER", "strikethrough: {strikethrough: true}\nemphasis: {strikethrough: false}", map[string]bool{"BEFORE": true, "INNER": false, "RESTORED": true, "AFTER": false}},
			{"false parent", "*BEFORE ~~INNER~~ RESTORED* AFTER", "emphasis: {strikethrough: false}\nstrikethrough: {strikethrough: true}", map[string]bool{"BEFORE": false, "INNER": true, "RESTORED": false, "AFTER": false}},
		} {
			t.Run(base+"/"+tc.name, func(t *testing.T) {
				out := renderThemeTest(t, tc.src, base, tc.yaml, 80)
				for token, strike := range tc.tokens {
					if c := themeToken(t, out, token); c.strike != strike {
						t.Fatalf("%s strike=%t want=%t output=%q", token, c.strike, strike, out)
					}
				}
			})
		}
	}
}

func TestThemeStockStrikeLinkParity(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, src := range []string{"~~[LABEL](https://example.com)~~ AFTER", "~~<https://example.com>~~ AFTER", "~~<a@example.com>~~ AFTER", "**~~[LABEL](https://example.com)~~** AFTER"} {
			t.Run(base+"/"+src, func(t *testing.T) {
				before := renderThemeTest(t, src, base, "", 80)
				after := renderThemeTest(t, src, base, "strikethrough: {fg: 1, strikethrough: false}", 80)
				if ansi.Strip(before) != ansi.Strip(after) || !reflect.DeepEqual(themeOSC8(before), themeOSC8(after)) {
					t.Fatal("stock strike flattening changed")
				}
				t.Logf("stock visible=%q OSC8=%v", ansi.Strip(before), themeOSC8(before))
				if strings.Contains(src, "LABEL") {
					fg := "31"
					if base == "notty" {
						fg = "default"
					}
					if c := themeToken(t, after, "LABEL"); c.fg != fg || c.strike {
						t.Fatalf("displayed label role missing: %+v", c)
					}
				}
			})
		}
	}
}

func TestThemeUnconfiguredFootnoteDefaults(t *testing.T) {
	const src = "Reference[^n] AFTER\n\n[^n]: Definition BODY\n"
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, empty := range []string{"unconfigured", "", "{}"} {
			t.Run(base+"/"+empty, func(t *testing.T) {
				var out string
				if empty == "unconfigured" {
					var err error
					out, _, _, err = renderThemedDoc(imgCtx{}, src, 80, base, config.Theme{}, 12)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					out = renderThemeTest(t, src, base, empty, 80)
				}
				lines := strings.Split(out, "\n")
				rows := themeRows(out)
				refs, defs := 0, 0
				st := paletteConfig
				if base != paletteStyleName {
					st = *styles.DefaultStyles[base]
				}
				for _, occurrence := range mappedFootnoteMarkers(src, lines) {
					fg := primitiveStyle(st.Link).FG
					if occurrence.marker.definition {
						defs++
						fg = primitiveStyle(st.LinkText).FG
					} else {
						refs++
					}
					wantFG := "default"
					if fg != nil && base != "notty" {
						wantFG = themeCells(textSGR(config.TextStyle{FG: fg}, false) + "X")[0].fg
					}
					for _, region := range occurrence.regions {
						for _, c := range rows[region.line][region.start:region.end] {
							if c.fg != wantFG || c.bg != "none" || c.underline == occurrence.marker.definition || occurrence.marker.definition && !c.bold {
								t.Fatalf("default semantic state missing: definition=%t cell=%+v", occurrence.marker.definition, c)
							}
						}
					}
				}
				if refs != 1 || defs != 1 {
					t.Fatalf("refs=%d definitions=%d", refs, defs)
				}
				for _, token := range []string{"Reference", "AFTER", "Definition", "BODY"} {
					if c := themeToken(t, out, token); c.bold || c.underline {
						t.Fatalf("default role leaked to %s: %+v", token, c)
					}
				}
			})
		}
	}
}

func TestThemeNestedTableUnicodeInvariance(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, width := range []int{40, 80, 120} {
			for _, prefix := range []string{"> ", "  "} {
				t.Run(fmt.Sprintf("%s/%d/%q", base, width, prefix), func(t *testing.T) {
					src := "| **界見出し** | *é 👩‍💻* |\n|---|---|\n| **" + strings.Repeat("界 é 👩‍💻 ", 15) + "MARK** | [**界 é 👩‍💻**](https://example.com/full?q=1&v=2) |\n| ~~界é👩‍💻~~ | <a@example.com> |\n"
					src = prefix + strings.ReplaceAll(strings.TrimSuffix(src, "\n"), "\n", "\n"+prefix) + "\n"
					if prefix == "  " {
						src = "- container\n\n" + src
					}
					before := renderThemeTest(t, src, base, "", width)
					after := renderThemeTest(t, src, base, "strong: {fg: 1, italic: true}\nemphasis: {underline: true}", width)
					if ansi.Strip(before) != ansi.Strip(after) {
						t.Fatalf("Unicode marker layout changed:\n%q\n%q", ansi.Strip(before), ansi.Strip(after))
					}
					if !reflect.DeepEqual(themeOSC8(before), themeOSC8(after)) {
						t.Fatal("Unicode markers changed OSC8/footer dedup")
					}
					oldRows, rows := themeRows(before), themeRows(after)
					if len(oldRows) != len(rows) {
						t.Fatal("row count changed")
					}
					for row := range rows {
						if len(rows[row]) != len(oldRows[row]) {
							t.Fatal("display geometry changed")
						}
					}
					if c := themeToken(t, after, "MARK"); !c.italic || base != "notty" && c.fg != "31" {
						t.Fatalf("annotation not active: %+v", c)
					}
					if strings.Contains(after, "\x1b]777;readmd-") {
						t.Fatal("marker leak")
					}
				})
			}
		}
	}
}

func TestThemeHeadingPaddingOverrides(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, width := range []int{40, 80, 120} {
			for _, bg := range []string{"none", "4"} {
				t.Run(fmt.Sprintf("%s/%d/%s", base, width, bg), func(t *testing.T) {
					src := "# " + strings.Repeat("word ", 45) + "END\n\nFOLLOWING"
					before := renderThemeTest(t, src, base, "", width)
					after := renderThemeTest(t, src, base, "headings: {h1: {bg: "+bg+"}}\nstrong: {italic: false}", width)
					if ansi.Strip(before) != ansi.Strip(after) {
						t.Fatal("heading layout changed")
					}
					oldRows, rows := themeRows(before), themeRows(after)
					for row, cells := range rows {
						content := false
						for _, c := range cells {
							content = content || c.text == "w"
						}
						if !content {
							continue
						}
						end := len(cells)
						for end > 0 && cells[end-1].text == " " {
							end--
						}
						for col, c := range cells {
							want := "none"
							// The stock painted H1 prefix/suffix and continuation padding belong
							// to the heading. Unpainted document margins remain outside it.
							owned := col >= 2 && col < end || oldRows[row][col].bg != "none"
							if owned && base != "notty" && bg != "none" {
								want = "44"
							}
							if c.bg != want {
								t.Fatalf("heading padding row=%d col=%d bg=%s want=%s old=%+v", row, col, c.bg, want, oldRows[row][col])
							}
						}
					}
				})
			}
		}
	}
}

func TestThemeSourceWhitespaceBoundaries(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, width := range []int{40, 80, 120} {
			for _, src := range []string{
				"**LEFT  RIGHT** AFTER",
				"**LEFT  \nRIGHT** AFTER",
				"**LEFT\\\nRIGHT** AFTER",
				"**LEFT\nRIGHT** AFTER",
				"> **LEFT  \n> RIGHT** AFTER",
				"**LEFT**  AFTER\n\nFINAL",
				"**LEFT `CODE  SPACE` RIGHT** AFTER",
				"**<https://example.com/" + strings.Repeat("long-path-", 18) + "> RIGHT** AFTER",
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", base, width, src), func(t *testing.T) {
					before := renderThemeTest(t, src, base, "", width)
					after := renderThemeTest(t, src, base, "strong: {bg: 4, italic: true}", width)
					if ansi.Strip(before) != ansi.Strip(after) {
						t.Fatalf("source-space layout changed:\n%q\n%q", ansi.Strip(before), ansi.Strip(after))
					}
					if !reflect.DeepEqual(themeOSC8(before), themeOSC8(after)) {
						t.Fatal("wrapped OSC8 changed")
					}
					if c := themeToken(t, after, "AFTER"); c.bg != "none" || c.italic {
						t.Fatalf("role closure leaked: %+v", c)
					}
					if strings.Contains(ansi.Strip(after), "LEFT  RIGHT") {
						cells := themeCells(after)
						for i, c := range cells {
							if c.text != "L" || i+10 > len(cells) {
								continue
							}
							if cells[i+4].text == " " && cells[i+5].text == " " {
								for _, space := range cells[i+4 : i+6] {
									bg := "44"
									if base == "notty" {
										bg = "none"
									}
									if space.bg != bg || !space.italic {
										t.Fatalf("real source spaces lost attributes: %+v", space)
									}
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestThemeComposerTerminalBoundary(t *testing.T) {
	for _, suffix := range []string{"", "\n", "\n\n"} {
		for _, padding := range []bool{false, true} {
			t.Run(fmt.Sprintf("suffix=%q/padding=%t", suffix, padding), func(t *testing.T) {
				options := glamansi.Options{Styles: paletteConfig}
				theme, err := config.ParseTheme([]byte("strong: {bg: 4}"))
				if err != nil {
					t.Fatal(err)
				}
				c, err := newThemeComposer(theme, &options, false)
				if err != nil {
					t.Fatal(err)
				}
				input := "\x1b[1m" + c.mark(roleStrong, true) + "X  " + c.mark(roleStrong, false) + "\x1b[m"
				if padding {
					input += "  "
				}
				input += suffix
				out, err := c.compose(input)
				if err != nil {
					t.Fatal(err)
				}
				cells := themeCells(out)
				for _, space := range cells[1:3] {
					if space.bg != "44" {
						t.Fatal("source-owned terminal spaces lost background")
					}
				}
				for _, space := range cells[3:] {
					if space.bg != "none" {
						t.Fatal("terminal padding painted")
					}
				}
			})
		}
	}
}

func BenchmarkThemeScopeRender(b *testing.B) {
	mixed := "# Heading **strong [link](https://example.com)**\n\nA paragraph with *emphasis*, ~~strike~~, `code`, 界 é 👩‍💻 and **strong words**.\n\n> A quote with **<https://example.com>**\n\n| A | B |\n|---|---|\n| **cell** | [label](https://example.com) |\n\n"
	for _, tc := range []struct{ name, src string }{
		{"mixed", strings.Repeat(mixed, 20)},
		{"large-span", "**" + strings.Repeat("word ", 13107) + "END**"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			theme, err := config.ParseTheme([]byte("strong: {bg: 4}\nemphasis: {italic: false}"))
			if err != nil {
				b.Fatal(err)
			}
			options := glamansi.Options{Styles: paletteConfig, WordWrap: 80}
			c, err := newThemeComposer(theme, &options, false)
			if err != nil {
				b.Fatal(err)
			}
			source := []byte(tc.src)
			doc := md.Parser().Parse(text.NewReader(source))
			c.annotate(doc, &source)
			if err := c.prepareLinks(doc, &source, options); err != nil {
				b.Fatal(err)
			}
			expanded := len(source)
			out, _, _, err := renderThemedDoc(imgCtx{}, tc.src, 80, paletteStyleName, theme, 12)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.src)))
			b.ResetTimer()
			for b.Loop() {
				if _, _, _, err := renderThemedDoc(imgCtx{}, tc.src, 80, paletteStyleName, theme, 12); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(tc.src)), "source-B")
			b.ReportMetric(float64(expanded), "intermediate-B")
			b.ReportMetric(float64(len(out)), "output-B")
		})
	}
}

func TestThemePrecomposedSourcePositions(t *testing.T) {
	const src = "# HEAD **[LABEL](https://example.com)**\n\n**Reference[^n] <a@example.com> END**\n\n[^n]: Definition body\n\n## NEXT\n\nFollowing prose\n"
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, width := range []int{40, 80, 120} {
			t.Run(fmt.Sprintf("%s/%d", base, width), func(t *testing.T) {
				before := renderThemeTest(t, src, base, "", width)
				after := renderThemeTest(t, src, base, "strong: {bg: 4}", width)
				oldLines, lines := strings.Split(before, "\n"), strings.Split(after, "\n")
				if !reflect.DeepEqual(parseRenderedLinkTargets(src, oldLines), parseRenderedLinkTargets(src, lines)) {
					t.Fatal("source link regions changed")
				}
				oldNotes, notes := parseFootnoteTargets(src, oldLines), parseFootnoteTargets(src, lines)
				if len(notes) != 1 || !reflect.DeepEqual(oldNotes, notes) {
					t.Fatal("footnote navigation changed")
				}
				oldHeads, heads := extractHeadings(src), extractHeadings(src)
				mapHeadings(oldHeads, oldLines)
				mapHeadings(heads, lines)
				if !reflect.DeepEqual(oldHeads, heads) || heads[0].srcLine != 0 || heads[1].srcLine != 6 {
					t.Fatalf("heading/source offsets changed: %+v", heads)
				}
			})
		}
	}
}

func TestThemeWrappedLinkCells(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		for _, width := range []int{40, 80, 120} {
			for _, link := range []string{
				"<https://example.com/" + strings.Repeat("long-path-", 24) + ">",
				"[" + strings.Repeat("LABEL ", 30) + "](https://example.com)",
				"<" + strings.Repeat("email", 40) + "@example.com>",
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", base, width, link), func(t *testing.T) {
					src := "***" + link + "*** AFTER"
					before := renderThemeTest(t, src, base, "", width)
					after := renderThemeTest(t, src, base, "strong: {bg: 4}\nemphasis: {italic: true}", width)
					if ansi.Strip(before) != ansi.Strip(after) || !reflect.DeepEqual(themeOSC8(before), themeOSC8(after)) {
						t.Fatal("wrapped link geometry/OSC8 changed")
					}
					rows := themeRows(after)
					targets := parseLinkTargets(strings.Split(after, "\n"))
					regions := 0
					for _, target := range targets {
						for _, region := range target.regions {
							regions++
							for _, c := range rows[region.line][region.start:region.end] {
								bg := "44"
								if base == "notty" {
									bg = "none"
								}
								if c.bg != bg || !c.italic {
									t.Fatalf("wrapped link cell lost ancestor: %+v", c)
								}
							}
						}
					}
					if regions < 2 {
						t.Fatal("fixture did not wrap its link")
					}
				})
			}
		}
	}
}

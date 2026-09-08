package pager

import (
	"bytes"
	"fmt"
	"github.com/alecthomas/chroma/v2"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func themeRenderProbe(t *testing.T, src string, style glamansi.StyleConfig, width int, marked bool) (string, *themeComposer) {
	t.Helper()
	options := glamansi.Options{Styles: style, WordWrap: width, PreserveNewLines: true}
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	var c *themeComposer
	if marked {
		var err error
		c, err = newThemeComposer(config.Theme{Strong: config.TextStyle{Bold: boolPtr(true)}}, &options, false)
		if err != nil {
			t.Fatal(err)
		}
		c.active = false
		c.annotate(doc, &source)
	}
	var b bytes.Buffer
	r := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(glamansi.NewRenderer(options), 1000)))
	if err := r.Render(&b, source, doc); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if marked {
		var err error
		out, err = c.compose(out)
		if err != nil {
			t.Fatalf("%v: raw %q", err, b.String())
		}
	}
	return out, c
}

func TestThemeMarkerInvariance(t *testing.T) {
	for _, base := range []string{paletteStyleName, styles.NoTTYStyle, styles.DarkStyle, styles.LightStyle} {
		t.Run(base, func(t *testing.T) {
			st := paletteConfig
			if base != paletteStyleName {
				st = *styles.DefaultStyles[base]
			}
			for _, width := range []int{40, 80, 120} {
				t.Run(strconv.Itoa(width), func(t *testing.T) {
					for _, src := range []string{
						"# Heading **strong *emphasis*** [link](https://example.com/full?q=1) `界👩‍💻`\n\nplain ~~strike~~ after\n",
						"> ## Heading [label](https://example.com)\n>\n> - **one *two*** `three`\n>   - [x] Checked\n",
						"| A | B |\n|---|---|\n| **strong** | [label](https://example.com) `code` |\n",
						"> | A | B |\n> |---|---|\n> | **strong** | [label](https://example.com) `code` |\n",
						"> | A | B |\n> |---|---|\n> | [label](https://example.com) | [**label**](https://example.com) |\n",
						"> | A | B |\n> |---|---|\n> | [la*bel*](https://example.com) | [label](https://example.com/other) |\n",
						"> | A | B |\n> |---|---|\n> | <a@example.com> | <https://example.com/auto> |\n",
						"> | A | B |\n> |---|---|\n> | ![label](image.png) | ![**label**](image.png) |\n",
					} {
						before, _ := themeRenderProbe(t, src, st, width, false)
						after, c := themeRenderProbe(t, src, st, width, true)
						if ansi.Strip(before) != ansi.Strip(after) {
							t.Fatalf("visible layout changed:\n%q\n%q", ansi.Strip(before), ansi.Strip(after))
						}
						if strings.Join(themeOSC8(before), "") != strings.Join(themeOSC8(after), "") {
							t.Fatal("marker annotation changed OSC8 controls")
						}
						if strings.Contains(after, c.prefix) {
							t.Fatal("marker leaked")
						}
					}
				})
			}
		})
	}
}

type themeCell struct {
	text, fg, bg                    string
	bold, italic, underline, strike bool
}

func themeCells(s string) []themeCell {
	var out []themeCell
	cur := themeCell{fg: "default", bg: "none"}
	state := byte(0)
	for s != "" {
		seq, w, n, ns := ansi.DecodeSequence(s, state, nil)
		if n <= 0 {
			panic("decode")
		}
		s, state = s[n:], ns
		if isSGR(seq) {
			args := strings.Split(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m"), ";")
			for i := 0; i < len(args); i++ {
				v, _ := strconv.Atoi(args[i])
				switch {
				case v == 0:
					cur = themeCell{fg: "default", bg: "none"}
				case v == 1:
					cur.bold = true
				case v == 22:
					cur.bold = false
				case v == 3:
					cur.italic = true
				case v == 23:
					cur.italic = false
				case v == 4:
					cur.underline = true
				case v == 24:
					cur.underline = false
				case v == 9:
					cur.strike = true
				case v == 29:
					cur.strike = false
				case v == 39:
					cur.fg = "default"
				case v == 49:
					cur.bg = "none"
				case v == 38 || v == 48:
					size := 2
					if i+1 < len(args) && args[i+1] == "2" {
						size = 4
					}
					if i+size >= len(args) {
						panic(seq)
					}
					value := strings.Join(args[i:i+size+1], ";")
					if v == 38 {
						cur.fg = value
					} else {
						cur.bg = value
					}
					i += size
				case v >= 30 && v <= 37 || v >= 90 && v <= 97:
					cur.fg = strconv.Itoa(v)
				case v >= 40 && v <= 47 || v >= 100 && v <= 107:
					cur.bg = strconv.Itoa(v)
				}
			}
		} else if w > 0 {
			c := cur
			c.text = seq
			for j := 0; j < w; j++ {
				out = append(out, c)
			}
		}
	}
	return out
}
func themeToken(t *testing.T, s, token string) themeCell {
	t.Helper()
	cells := themeCells(s)
	for i := range cells {
		var b strings.Builder
		for j := i; j < len(cells); j++ {
			b.WriteString(cells[j].text)
			if b.String() == token {
				return cells[i]
			}
			if b.Len() > len(token) {
				break
			}
		}
	}
	t.Fatalf("token %q missing in %q", token, s)
	return themeCell{}
}

func renderThemeTest(t *testing.T, src, base, yaml string, width int) string {
	t.Helper()
	theme, err := config.ParseTheme([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	out, _, _, err := renderThemedDoc(imgCtx{}, src, width, base, theme, 12)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b]777;readmd-") {
		t.Fatal("marker leaked")
	}
	return out
}

func TestThemeNestedRoles(t *testing.T) {
	src := "# HEAD **STRONG *EMPH*** [LINK](https://example.com/full?q=1) `CODE` END\n\n> ## QUOTE **INNER** [NEST](https://example.com/nest) `SPAN`\n\nTAIL\n"
	yaml := "headings: {h1: {fg: 1, bg: 4, bold: false}, h2: {fg: 2}}\nstrong: {fg: 3, bold: false}\nemphasis: {fg: default, italic: false}\nlinks: {fg: 6, bg: none, bold: false, underline: true}\ninline_code: {fg: default, bg: none}\n"
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		t.Run(base, func(t *testing.T) {
			out := renderThemeTest(t, src, base, yaml, 120)
			for _, tc := range []struct {
				token, fg, bg string
				bold, italic  bool
			}{{"HEAD", "31", "44", false, false}, {"STRONG", "33", "44", false, false}, {"EMPH", "default", "44", false, false}, {"LINK", "36", "none", false, false}, {"CODE", "default", "none", false, false}, {"END", "31", "44", false, false}, {"QUOTE", "32", "none", false, false}, {"INNER", "33", "none", false, false}, {"SPAN", "default", "none", false, false}} {
				c := themeToken(t, out, tc.token)
				if base == "notty" {
					tc.fg, tc.bg = "default", "none"
				}
				// H2 base bold is intentionally inherited; only explicit false clears it.
				if c.fg != tc.fg || c.bg != tc.bg || (tc.token != "QUOTE" && tc.token != "SPAN" && c.bold != tc.bold) || c.italic != tc.italic {
					t.Fatalf("%s state=%+v want=%+v output=%q", tc.token, c, tc, out)
				}
			}
			if !strings.Contains(out, "https://example.com/full?q=1\a") {
				t.Fatal("OSC8 target altered")
			}
		})
	}
}

func TestThemeDefaultsAndFalse(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, src, token  string
		fg, bg                  string
		bold, italic, underline bool
	}{
		{name: "omit strong inherits", yaml: "headings: {h1: {fg: 2}}", src: "# A **MARK** Z", token: "MARK", fg: "32", bg: "none", bold: true},
		{name: "clear heading", yaml: "headings: {h1: {fg: default, bg: none, bold: false}}", src: "# MARK", token: "MARK", fg: "default", bg: "none"},
		{name: "disable emphasis", yaml: "emphasis: {italic: false}", src: "*MARK*", token: "MARK", fg: "default", bg: "none"},
		{name: "disable strong", yaml: "strong: {bold: false}", src: "**MARK**", token: "MARK", fg: "default", bg: "none"},
		{name: "RGB zero", yaml: "strong: {fg: '#000001', bg: '#000002'}", src: "**MARK**", token: "MARK", fg: "38;2;0;0;1", bg: "48;2;0;0;2", bold: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := renderThemeTest(t, tc.src, paletteStyleName, tc.yaml, 80)
			c := themeToken(t, out, tc.token)
			if c.fg != tc.fg || c.bg != tc.bg || c.bold != tc.bold || c.italic != tc.italic || c.underline != tc.underline {
				t.Fatalf("%+v output=%q", c, out)
			}
		})
	}
}

func TestThemeCheckboxGlyphs(t *testing.T) {
	for _, glyph := range []string{"[x]", "\uf14a", "✅", "👩‍💻", "é"} {
		t.Run(glyph, func(t *testing.T) {
			yaml := fmt.Sprintf("tasks: {checked: {glyph: '%s', fg: 1, bold: true}, unchecked: {glyph: '[ ]', fg: 3}}", glyph)
			out := renderThemeTest(t, "- [x] CHECKED\n- [ ] UNCHECKED\n\n> - [x] NESTED\n", paletteStyleName, yaml, 80)
			for _, token := range []string{"CHECKED", "UNCHECKED", "NESTED"} {
				c := themeToken(t, out, token)
				if c.fg != "default" || c.bold {
					t.Fatalf("glyph styling leaked: %+v", c)
				}
			}
			if !strings.Contains(ansi.Strip(out), glyph+" CHECKED") {
				t.Fatal("glyph changed")
			}
			colored := 0
			for _, c := range themeCells(out) {
				if c.fg == "31" {
					colored++
				}
			}
			if colored != 2*ansi.StringWidth(glyph) {
				t.Fatalf("colored columns=%d want %d", colored, 2*ansi.StringWidth(glyph))
			}
		})
	}
}

func TestThemeCodeRectangles(t *testing.T) {
	for _, tc := range []struct{ name, src string }{{"fence", "```go\nvar x = 123\n\n// 界 👩‍💻\n```\n\nOUTSIDE"}, {"quote", "> ```go\n> var x = 123\n>\n> // 界 👩‍💻\n> ```\n\nOUTSIDE"}, {"list", "- item\n\n  ```go\n  var x = 123\n\n  // 界 👩‍💻\n  ```\n\nOUTSIDE"}, {"indented", "    var x = 123\n\n    // 界 👩‍💻\n\nOUTSIDE"}, {"unknown", "```unknown\nvar x = 123\n\n// 界 👩‍💻\n```\n\nOUTSIDE"}, {"empty", "```go\n```\n\nOUTSIDE"}, {"blank", "```go\n\n\n```\n\nOUTSIDE"}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
				t.Run(base, func(t *testing.T) {
					before := renderThemeTest(t, tc.src, base, "", 40)
					after := renderThemeTest(t, tc.src, base, "code_block: {bg: 24}", 40)
					if trimTrailing(ansi.Strip(before)) != trimTrailing(ansi.Strip(after)) {
						t.Fatalf("code text/reflow changed\n%q\n%q", ansi.Strip(before), ansi.Strip(after))
					}
					first, last, start, end := -1, -1, -1, -1
					lines := strings.Split(after, "\n")
					for row, line := range lines {
						a, z := -1, -1
						for col, c := range themeCells(line) {
							if c.bg == "48;5;24" {
								if a < 0 {
									a = col
								}
								z = col + 1
							}
						}
						if a < 0 {
							continue
						}
						if first < 0 {
							first, start, end = row, a, z
						}
						last = row
						if a != start || z != end {
							t.Fatalf("nonrectangular row=%d [%d,%d), want [%d,%d)", row, a, z, start, end)
						}
						cells := themeCells(line)
						for _, c := range cells[a:z] {
							if c.bg != "48;5;24" {
								t.Fatal("background hole")
							}
						}
					}
					if base == "notty" {
						if first >= 0 {
							t.Fatal("notty painted background")
						}
						return
					}
					if tc.name != "empty" && tc.name != "blank" {
						if last-first != 2 || start < 2 {
							t.Fatalf("rectangle bounds %d..%d [%d,%d)", first, last, start, end)
						}
						hl := highlightLine(lines[first], []span{{start, start + 3}}, -1)
						if themeCells(hl)[start+3].bg != "48;5;24" {
							t.Fatal("search lost rectangle")
						}
					}
					if themeToken(t, after, "OUTSIDE").bg != "none" {
						t.Fatal("background leaked")
					}
				})
			}
		})
	}
}

func TestThemeCodeTabs(t *testing.T) {
	for _, prefix := range []string{"", "> ", "  "} {
		t.Run(fmt.Sprintf("prefix_%q", prefix), func(t *testing.T) {
			code := "\tvar x = 1\n界\t\tend\n \t mixed\n"
			src := "```text\n" + code + "```\n"
			if prefix != "" {
				src = prefix + strings.ReplaceAll(strings.TrimSuffix(src, "\n"), "\n", "\n"+prefix) + "\n"
				if prefix == "  " {
					src = "- item\n\n" + src
				}
			}
			plain := renderThemeTest(t, src, paletteStyleName, "", 80)
			clear := renderThemeTest(t, src, paletteStyleName, "code_block: {bg: none}", 80)
			painted := renderThemeTest(t, src, paletteStyleName, "code_block: {bg: '#010203'}", 80)
			if !strings.Contains(plain, "\t") || !strings.Contains(clear, "\t") {
				t.Fatal("no-rectangle changed TAB bytes")
			}
			if strings.Contains(painted, "\t") {
				t.Fatal("painted TAB not expanded")
			}
			if trimTrailing(ansi.Strip(expandTableTabs(plain))) != trimTrailing(ansi.Strip(painted)) {
				t.Fatal("TAB expansion differs from existing four-space policy")
			}
		})
	}
}

func TestThemeConcurrentRegistrationAndRender(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		t.Run(base, func(t *testing.T) {
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Go(func() {
					yaml := fmt.Sprintf("code_block: {bg: '#%06x'}\nstrong: {fg: %d}", i+1, i)
					out := renderThemeTest(t, "**MARK**\n\n```go\nvar a = 1\n```", base, yaml, 80)
					if base == "notty" {
						for _, c := range themeCells(out) {
							if c.fg != "default" || c.bg != "none" {
								t.Errorf("notty colors: %+v", c)
								break
							}
						}
					}
				})
			}
			wg.Wait()
		})
	}
}

func TestThemeMalformedMarkers(t *testing.T) {
	for _, kind := range []string{"close", "unclosed", "invalid", "truncated", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			o := glamansi.Options{Styles: paletteConfig}
			c, err := newThemeComposer(config.Theme{}, &o, false)
			if err != nil {
				t.Fatal(err)
			}
			var input string
			switch kind {
			case "close":
				input = c.mark(roleStrong, false)
			case "unclosed":
				input = c.mark(roleStrong, true) + "text"
			case "invalid":
				input = c.prefix + "999;open\a"
			case "truncated":
				input = c.prefix + "6;open"
			case "foreign":
				input = "\x1b]777;readmd-forged;6;open\a"
			}
			if _, err := c.compose(input); err == nil {
				t.Fatal("incomplete marker was accepted")
			}
		})
	}
}

func TestThemeCalloutIconDefaults(t *testing.T) {
	for _, kind := range []struct{ name, title, octicon, unicode, fg string }{
		{"note", "Note", "\uf449", "ⓘ", "94"},
		{"tip", "Tip", "\uf400", "✦", "92"},
		{"important", "Important", "\uf50a", "✱", "95"},
		{"warning", "Warning", "\uf421", "⚠", "93"},
		{"caution", "Caution", "\uf46e", "✖", "91"},
	} {
		for _, tc := range []struct {
			name, yaml, icon, rail string
			configured             bool
		}{
			{"unconfigured", "", kind.octicon, "│", false},
			{"empty theme", "{}", kind.octicon, "│", false},
			{"other role only", "strong: {fg: '#123456'}", kind.octicon, "│", false},
			{"empty callouts", "callouts: {}", kind.octicon, "│", false},
			{"empty title", "callouts: {title: {}}", kind.octicon, "│", false},
			{"omitted preset shared", "callouts: {title: {bold: true}}", kind.octicon, "│", true},
			{"omitted preset per type", "callouts: {" + kind.name + ": {title: {bold: true}}}", kind.octicon, "│", true},
			{"unicode", "callouts: {preset: unicode}", kind.unicode, "│", true},
			{"nerd", "callouts: {preset: nerd}", kind.octicon, "▋", true},
			{"default override", "callouts: {" + kind.name + ": {icon: ' X '}}", " X ", "│", true},
			{"unicode override", "callouts: {preset: unicode, " + kind.name + ": {icon: ' X '}}", " X ", "│", true},
			{"nerd override", "callouts: {preset: nerd, " + kind.name + ": {icon: ' X '}}", " X ", "▋", true},
		} {
			for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
				for _, width := range []int{1, 40} {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", kind.name, tc.name, base, width), func(t *testing.T) {
						src := "> [!" + strings.ToUpper(kind.name) + "]\n> BODY\n> NEXT\n\n> QUOTE\n"
						out := renderThemeTest(t, src, base, tc.yaml, width)
						theme, err := config.ParseTheme([]byte(tc.yaml))
						if err != nil {
							t.Fatal(err)
						}
						stock, _, _, err := renderThemedStyled(imgCtx{}, src, width, base, false, theme, 12)
						if err != nil {
							t.Fatal(err)
						}
						if base != paletteStyleName && !tc.configured {
							if out != stock {
								t.Fatalf("unconfigured non-auto callout changed:\n%q\n%q", out, stock)
							}
							return
						}
						plain := ansi.Strip(out)
						want := tc.rail + " " + tc.icon + " " + kind.title
						found := false
						for _, line := range strings.Split(plain, "\n") {
							if strings.TrimLeft(line, " ") == want {
								found = true
							}
						}
						if !found || strings.Contains(plain, "[!") || strings.Contains(plain, "readmd-") {
							t.Fatalf("want exact enhanced title %q, got %q", want, out)
						}
						fg := kind.fg
						if base == "notty" {
							fg = "default"
						}
						if cell := themeToken(t, out, kind.title); cell.fg != fg || cell.bg != "none" || !cell.bold {
							t.Fatalf("default title style changed: %+v", cell)
						}
						if cell := themeToken(t, out, tc.rail); cell.fg != fg || cell.bg != "none" || cell.bold {
							t.Fatalf("default rail style changed: %+v", cell)
						}
						for _, token := range []string{"BODY", "NEXT", "QUOTE"} {
							if got, want := themeToken(t, out, token), themeToken(t, stock, token); got != want {
								t.Fatalf("%s style changed: %+v, want %+v", token, got, want)
							}
						}
						if !strings.Contains(plain, tc.rail+" BODY\n") || !strings.Contains(plain, tc.rail+" NEXT\n") {
							t.Fatalf("body lines/rail spacing changed: %q", plain)
						}
					})
				}
			}
		}
	}
}

func TestThemeCallouts(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		t.Run(base, func(t *testing.T) {
			for _, tc := range []struct{ name, icon string }{{"NOTE", "\uf449"}, {"TIP", "\uf400"}, {"IMPORTANT", "\uf50a"}, {"WARNING", "\uf421"}, {"CAUTION", "\uf46e"}} {
				t.Run(tc.name, func(t *testing.T) {
					src := "> [!" + tc.name + "]\n> BODY [link](https://example.com/full)\n> next line\n"
					out := renderThemeTest(t, src, base, "callouts: {preset: nerd, rail: {glyph: ▋, fg: 2}, title: {bold: false}}", 40)
					if !strings.Contains(out, tc.icon) || !strings.Contains(out, "▋") {
						t.Fatalf("preset missing: %q", out)
					}
					if themeToken(t, out, "BODY").bold {
						t.Fatal("callout title/body attributes leaked")
					}
					for _, c := range themeCells(out) {
						if c.text == tc.icon && c.bold {
							t.Fatal("title false ignored")
						}
						if base == "notty" && (c.fg != "default" || c.bg != "none") {
							t.Fatal("notty color")
						}
					}
					if !strings.Contains(out, "https://example.com/full\a") {
						t.Fatal("callout target lost")
					}
					custom := renderThemeTest(t, src, base, "callouts: {preset: nerd, title: {bold: true}, "+strings.ToLower(tc.name)+": {icon: X, rail: {glyph: '||'}, title: {bold: false}}}", 40)
					if !strings.Contains(ansi.Strip(custom), "|| X ") {
						t.Fatalf("per-type override missing: %q", custom)
					}
					if themeToken(t, custom, "X").bold {
						t.Fatal("per-type false did not override shared true")
					}
				})
			}
		})
	}
}

func TestThemeFootnoteProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		refs      int
	}{{"lookalikes", "Real[^n] [ordinary][url] `[^n]` \\[^n] [^missing]\n\n```text\n[^n]\n```\n\n[^n]: note\n\n[url]: https://example.com/full?q=1\n", 1}, {"HTML", "<span title=\"[^n]\">x</span> real[^n]\n\n[^n]: note\n", 1}, {"table", "| A | B |\n|---|---|\n| first[^1] long words again[^1] | `[^1]` live[^1] |\n\n[^1]: note\n", 3}, {"wrapped", "long[^abcdefghijk]\n\n[^abcdefghijk]: note\n", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
				t.Run(base, func(t *testing.T) {
					width := 40
					if tc.name == "wrapped" {
						width = 12
					}
					out := renderThemeTest(t, tc.src, base, "footnotes: {reference: {fg: 5, underline: true}, definition: {fg: 3, bold: true, underline: false}}", width)
					lines := strings.Split(out, "\n")
					targets := parseFootnoteTargets(tc.src, lines)
					if len(targets) != tc.refs {
						t.Fatalf("refs=%d want=%d, %q", len(targets), tc.refs, out)
					}
					occurrences := mappedFootnoteMarkers(tc.src, lines)
					for row, line := range lines {
						for col, c := range themeCells(line) {
							expected := ""
							for _, o := range occurrences {
								for _, r := range o.regions {
									if r.line == row && col >= r.start && col < r.end {
										expected = "ref"
										if o.marker.definition {
											expected = "def"
										}
									}
								}
							}
							if base == "notty" {
								if c.fg != "default" || c.bg != "none" {
									t.Fatal("notty footnote color")
								}
							} else {
								if expected == "ref" && c.fg != "35" || expected == "def" && c.fg != "33" || expected == "" && (c.fg == "35" || c.fg == "33") {
									t.Fatalf("wrong semantic role row=%d col=%d role=%s cell=%+v", row, col, expected, c)
								}
							}
							if expected == "ref" && !c.underline || expected == "def" && (!c.bold || c.underline) {
								t.Fatal("footnote distinction missing")
							}
						}
					}
					if tc.name == "lookalikes" && !strings.Contains(out, "https://example.com/full?q=1\a") {
						t.Fatal("ordinary reference-style link target lost")
					}
				})
			}
		})
	}
}

func TestThemeTableBorderScope(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		colored   bool
	}{{"top", "| A | B |\n|---|---|\n| 界 | 👩‍💻 |\n", true}, {"nested", "> | A | B |\n> |---|---|\n> | 界 | 👩‍💻 |\n", false}} {
		t.Run(tc.name, func(t *testing.T) {
			before := renderThemeTest(t, tc.src, paletteStyleName, "", 40)
			after := renderThemeTest(t, tc.src, paletteStyleName, "table: {border: {fg: 1}}", 40)
			if ansi.Strip(before) != ansi.Strip(after) {
				t.Fatal("border changed geometry")
			}
			count := 0
			for _, c := range themeCells(after) {
				if c.fg == "31" {
					count++
					if !strings.Contains("╭╮╰╯─│┼┬┴├┤", c.text) {
						t.Fatal("border styled content")
					}
				}
			}
			if (count > 0) != tc.colored {
				t.Fatalf("border scope: count=%d", count)
			}
		})
	}
}

func TestThemeCodeKeepsExactInnerForeground(t *testing.T) {
	for _, bg := range []string{"24", "#000003", "none"} {
		t.Run(bg, func(t *testing.T) {
			// Same-buffer oracle avoids Chroma terminal256's nearest-color tie randomness.
			raw := "\x1b[38;2;0;2;3m界\x1b[0m\x1b[1;91mX\x1b[22;49m\n\nY\n"
			inner := chroma.FormatterFunc(func(w io.Writer, _ *chroma.Style, _ chroma.Iterator) error {
				_, err := io.WriteString(w, raw)
				return err
			})
			var b bytes.Buffer
			if err := codeBackgroundFormatter(inner, bg).Format(&b, nil, nil); err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(raw, "\n") {
				before := themeCells(line)
				after := themeCells(strings.Split(b.String(), "\n")[i])
				for col, c := range before {
					if c.fg != after[col].fg || c.bold != after[col].bold {
						t.Fatalf("syntax changed %+v %+v", c, after[col])
					}
				}
			}
		})
	}
}

func TestThemeSnapshotAndRerender(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			src := "# HEAD\n\n**MARK** reference[^n]\n\n[^n]: note\n"
			theme, err := config.ParseTheme([]byte("strong: {fg: 1}"))
			if err != nil {
				t.Fatal(err)
			}
			m := New(src, "doc.md")
			if err := m.SetStyle("auto"); err != nil {
				t.Fatal(err)
			}
			m.SetTheme(theme)
			m.width, m.height = width, 25
			command := m.requestRender()
			*theme.Strong.FG = "2"
			msg := command().(renderedMsg)
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if themeToken(t, msg.content, "MARK").fg != "31" {
				t.Fatal("render snapshot mutated")
			}
			m.rendering = false
			m.Update(msg)
			original := strings.Join(m.base, "\n")
			m.reader = true
			rerender := m.requestRender()().(renderedMsg)
			if rerender.err != nil {
				t.Fatal(rerender.err)
			}
			if themeToken(t, rerender.content, "MARK").fg != "31" || m.source != src || strings.Join(m.base, "\n") != original {
				t.Fatal("source/cache/theme mutation during rerender")
			}
		})
	}
}

func TestThemeSearchRestoresEndState(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		spans      []span
	}{
		{"full reset", "\x1b[31;44mAB\x1b[0mCDE", []span{{1, 3}}},
		{"defaults", "\x1b[31;44mAB\x1b[39;49mCDE", []span{{1, 3}}},
		{"RGB zero and selective", "\x1b[38;2;0;2;0;48;2;0;0;4mAB\x1b[49mCDE", []span{{1, 3}}},
		{"adjacent matches", "\x1b[31mAB\x1b[32mCD\x1b[39mEF", []span{{0, 2}, {2, 4}}},
		{"multiple matches", "\x1b[31mAB\x1b[32;44mCD\x1b[39;49mEFGH", []span{{0, 3}, {5, 7}}},
		{"nested attributes", "\x1b[1mAB\x1b[3mCD\x1b[23mEF\x1b[22mGH", []span{{1, 5}}},
		{"OSC8", "A\x1b]8;id=target;https://example.com/full?q=1\a\x1b[32mBCDE\x1b]8;;\aF", []span{{0, 3}}},
		{"ending EOL", "\x1b[31mAB\x1b[32mCD", []span{{1, 4}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := highlightLine(tc.line, tc.spans, -1)
			before, after := themeCells(tc.line), themeCells(out)
			if ansi.Strip(out) != ansi.Strip(tc.line) {
				t.Fatal("search changed visible bytes")
			}
			for col, want := range before {
				matched := false
				for _, s := range tc.spans {
					matched = matched || col >= s.start && col < s.end
				}
				if matched {
					continue
				}
				if after[col] != want {
					t.Fatalf("unmatched column %d: %+v want %+v; output=%q", col, after[col], want, out)
				}
			}
			for rest := tc.line; rest != ""; {
				seq, _, n, _ := ansi.DecodeSequence(rest, 0, nil)
				rest = rest[n:]
				if strings.HasPrefix(seq, "\x1b]8;") && !strings.Contains(out, seq) {
					t.Fatal("search swallowed OSC8")
				}
			}
		})
	}
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		t.Run("nested theme/"+base, func(t *testing.T) {
			out := renderThemeTest(t, "# HEAD [LINK](https://example.com) `CODE` TAIL", base, "headings: {h1: {bg: 4}}\nlinks: {bg: none}\ninline_code: {bg: none}", 120)
			for _, line := range strings.Split(out, "\n") {
				plain := ansi.Strip(line)
				start := strings.Index(plain, "HEAD")
				end := strings.Index(plain, "LINK")
				if start < 0 || end < 0 {
					continue
				}
				hl := highlightLine(line, []span{{start, end + 2}}, -1)
				before, after := themeCells(line), themeCells(hl)
				for col := end + 2; col < len(before); col++ {
					if before[col] != after[col] {
						t.Fatalf("nested tail state changed at %d: %+v vs %+v", col, before[col], after[col])
					}
				}
			}
		})
	}
}

func themeOSC8(s string) []string {
	var result []string
	state := byte(0)
	for s != "" {
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		s, state = s[n:], next
		if strings.HasPrefix(seq, "\x1b]8;") {
			result = append(result, seq)
		}
	}
	return result
}

func TestThemeLinkKinds(t *testing.T) {
	for _, tc := range []struct{ name, src, token, dest string }{{"inline", "[LABEL](https://example.com/full?q=1)", "LABEL", "https://example.com/full?q=1"}, {"reference", "[LABEL][ref]\n\n[ref]: https://example.com/ref\n", "LABEL", "https://example.com/ref"}, {"URL autolink", "<https://example.com/auto>", "https://example.com/auto", "https://example.com/auto"}, {"email autolink", "<a@example.com>", "a@example.com", "mailto:a@example.com"}, {"formatted label", "[**LABEL**](https://example.com/bold)", "LABEL", "https://example.com/bold"}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
				t.Run(base, func(t *testing.T) {
					out := renderThemeTest(t, tc.src, base, "links: {fg: 5, bg: none, underline: false, bold: false}", 80)
					cell := themeToken(t, out, tc.token)
					fg := "35"
					if base == "notty" {
						fg = "default"
					}
					// A semantic strong child retains its own base bold attribute.
					if cell.fg != fg || cell.bg != "none" || cell.underline || (tc.name != "formatted label" && cell.bold) {
						t.Fatalf("link role missing: %+v %q", cell, out)
					}
					if !strings.Contains(out, ";"+tc.dest+"\a") {
						t.Fatal("full OSC8 target lost")
					}
				})
			}
		})
	}
}

func TestThemeReaderPanResizeReload(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			wide := strings.Repeat("界👩‍💻", 40)
			src := "# HEAD\n\n**MARK** ref[^1]\n\n```text\n" + wide + "\n```\n\n[^1]: note\n"
			theme, err := config.ParseTheme([]byte("strong: {fg: 1}\ncode_block: {bg: 4}"))
			if err != nil {
				t.Fatal(err)
			}
			m := New(src, "doc.md")
			if err := m.SetStyle("auto"); err != nil {
				t.Fatal(err)
			}
			m.SetTheme(theme)
			_, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
			settle(t, m, cmd)
			baseline := strings.Join(m.base, "\n")
			press(m, "l")
			_ = m.View()
			if strings.Join(m.base, "\n") != baseline || m.source != src {
				t.Fatal("pan mutated cache/source")
			}
			settle(t, m, press(m, "r"))
			_, cmd = m.Update(tea.WindowSizeMsg{Width: width + 1, Height: 20})
			settle(t, m, cmd)
			if themeToken(t, strings.Join(m.base, "\n"), "MARK").fg != "31" || countFootnoteTargets(m.links) != 1 {
				t.Fatal("resize/reader lost theme/provenance")
			}
			replacement := strings.ReplaceAll(src, "[^1]", "[^2]")
			settle(t, m, m.applyReload(reloadDoneMsg{body: []byte(replacement)}))
			if m.source != replacement || themeToken(t, strings.Join(m.base, "\n"), "MARK").fg != "31" || countFootnoteTargets(m.links) != 1 {
				t.Fatal("reload lost source/theme/footnotes")
			}
			for _, target := range m.links {
				if target.kind == targetFootnote && target.footnote != "2" {
					t.Fatal("stale footnote metadata")
				}
			}
			if !strings.Contains(strings.Join(m.stripped, "\n"), wide) {
				t.Fatal("code graphemes reflowed or split")
			}
		})
	}
}

func TestThemeNestedTableLinkScope(t *testing.T) {
	for _, tc := range []struct{ name, prefix, fg string }{{"top", "", "31"}, {"stock nested", "> ", "32"}} {
		t.Run(tc.name, func(t *testing.T) {
			src := "| A | B |\n|---|---|\n| [**WORD**](https://example.com) | [WORD](https://example.com) |\n"
			if tc.prefix != "" {
				src = tc.prefix + strings.ReplaceAll(strings.TrimSuffix(src, "\n"), "\n", "\n"+tc.prefix) + "\n"
			}
			out := renderThemeTest(t, src, paletteStyleName, "strong: {fg: 1}\nlinks: {fg: 2, bold: false}", 80)
			if cell := themeToken(t, out, "WORD"); cell.fg != tc.fg {
				t.Fatalf("link-role scope: %+v", cell)
			}
			if tc.prefix != "" && strings.Contains(ansi.Strip(out), "[2]") {
				t.Fatal("role markers changed footer deduplication")
			}
		})
	}
}

func TestThemeCalloutBodySpacing(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		t.Run(base, func(t *testing.T) {
			src := "> [!NOTE]\n> body\n>\n> [!NOTE]\n>\n> after\n"
			out := renderThemeTest(t, src, base, "callouts: {title: {bold: false}}", 80)
			plain := ansi.Strip(out)
			if strings.Count(plain, "[!NOTE]") != 1 {
				t.Fatalf("body marker was rewritten: %q", plain)
			}
			lines := strings.Split(plain, "\n")
			for i, line := range lines {
				if strings.Contains(line, "[!NOTE]") && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "│" {
					t.Fatalf("body spacing was removed: %q", plain)
				}
			}
		})
	}
}

func TestThemeAllHeadingLevels(t *testing.T) {
	for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
		t.Run(base, func(t *testing.T) {
			for _, level := range []int{1, 2, 3, 4, 5, 6} {
				t.Run(strconv.Itoa(level), func(t *testing.T) {
					out := renderThemeTest(t, strings.Repeat("#", level)+" MARK", base, fmt.Sprintf("headings: {h%d: {fg: %d, bg: none, bold: false}}", level, level), 40)
					cell := themeToken(t, out, "MARK")
					fg := strconv.Itoa(30 + level)
					if base == "notty" {
						fg = "default"
					}
					if cell.fg != fg || cell.bg != "none" || cell.bold {
						t.Fatalf("h%d state: %+v", level, cell)
					}
				})
			}
		})
	}
}

func TestThemeCalloutIconPadding(t *testing.T) {
	for _, kind := range []struct{ name, title, unicode, nerd string }{
		{"note", "Note", "ⓘ", "\uf449"},
		{"tip", "Tip", "✦", "\uf400"},
		{"important", "Important", "✱", "\uf50a"},
		{"warning", "Warning", "⚠", "\uf421"},
		{"caution", "Caution", "✖", "\uf46e"},
	} {
		for _, preset := range []struct{ name, icon, rail string }{
			{"unicode", kind.unicode, "│"}, {"nerd", kind.nerd, "▋"},
		} {
			for _, icon := range []struct{ name, override, rendered string }{
				{"preset", "", preset.icon},
				{"custom", "i", "i"},
				{"padded", "i ", "i "},
				{"user glyph", "", ""},
				{"user padded", " ", " "},
				{"explicit spaces", " i  ", " i  "},
				{"wide", "界", "界"},
				{"grapheme", "👩‍💻", "👩‍💻"},
				{"max width", "abcd", "abcd"},
			} {
				for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
					for _, width := range []int{1, 40} {
						t.Run(fmt.Sprintf("%s/%s/%s/%s/%d", kind.name, preset.name, icon.name, base, width), func(t *testing.T) {
							override := ""
							if icon.override != "" {
								override = "icon: '" + icon.override + "', "
							}
							yaml := "callouts: {preset: " + preset.name + ", title: {bold: true}, " + kind.name + ": {" + override + "rail: {fg: 12}, title: {fg: 12, bold: false}}}"
							src := "> [!" + strings.ToUpper(kind.name) + "]\n> BODY\n\n> QUOTE\n\n# AFTER\n"
							out := renderThemeTest(t, src, base, yaml, width)
							wantTitle := icon.rendered + " " + kind.title
							want := preset.rail + " " + wantTitle
							found := false
							for _, line := range strings.Split(out, "\n") {
								plain := ansi.Strip(line)
								if !strings.Contains(plain, kind.title) {
									continue
								}
								found = true
								if strings.TrimLeft(plain, " ") != want || !strings.Contains(line, wantTitle+"\x1b[m") {
									t.Fatalf("styled title = %q, want prefix %q and intact styled title %q", line, want, wantTitle)
								}
								margin := len(plain) - len(strings.TrimLeft(plain, " "))
								wantWidth := margin + ansi.StringWidth(preset.rail) + 1 + ansi.StringWidth(icon.rendered) + 1 + len(kind.title)
								if got := ansi.StringWidth(line); got != wantWidth {
									t.Fatalf("width = %d, want %d", got, wantWidth)
								}
								if got := ansi.Strip(ansi.Cut(line, wantWidth-len(kind.title), wantWidth)); got != kind.title {
									t.Fatalf("display-column title slice = %q", got)
								}
								for _, cell := range themeCells(line) {
									if cell.bold || (base == "notty" && (cell.fg != "default" || cell.bg != "none")) {
										t.Fatalf("title override/notty violated: %+v", cell)
									}
								}
								if base != "notty" && themeToken(t, line, kind.title).fg != "94" {
									t.Fatalf("title color lost: %q", line)
								}
							}
							plain := ansi.Strip(out)
							if !found || strings.Contains(plain, "readmd-") || !strings.Contains(plain, preset.rail+" BODY") || !strings.Contains(plain, "AFTER") {
								t.Fatalf("callout/adjacent content lost: %q", out)
							}
						})
					}
				}
			}
		}
	}
}

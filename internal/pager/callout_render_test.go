package pager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestCalloutTitleBodySpacing(t *testing.T) {
	for _, tc := range []struct {
		name, source, title string
		gap                 int
	}{
		{"adjacent", "> [!NOTE]\n> BODYONE\n", "Note", 0},
		{"explicit blank", "> [!NOTE]\n>\n> BODYONE\n", "Note", 1},
		{"body paragraphs", "> [!NOTE]\n> BODYONE\n>\n> BODYTWO\n", "Note", 0},
		{"custom title", "> [!TIP] Custom title\n> BODYONE\n", "Custom title", 0},
		{"wrapped title", "> [!NOTE] " + strings.Repeat("title words ", 12) + "TITLEEND\n> BODYONE\n", "TITLEEND", 0},
		{"nested", "> [!NOTE]\n> Outer body\n>\n> > [!TIP]\n> > BODYONE\n", "Tip", 0},
		{"in list", "- item\n\n  > [!NOTE]\n  > BODYONE\n", "Note", 0},
	} {
		for _, style := range []string{paletteStyleName, "dark", "light", "notty"} {
			t.Run(tc.name+"/"+style, func(t *testing.T) {
				out, _, _, err := renderDoc(imgCtx{}, tc.source, 40, style)
				if err != nil {
					t.Fatal(err)
				}
				lines := splitStrip(out)
				title, first, second := -1, -1, -1
				for i, line := range lines {
					if strings.Contains(line, tc.title) {
						title = i
					}
					if strings.Contains(line, "BODYONE") {
						first = i
					}
					if strings.Contains(line, "BODYTWO") {
						second = i
					}
				}
				if title < 0 || first-title != tc.gap+1 {
					t.Fatalf("title=%d body=%d, want %d blank rows:\n%s", title, first, tc.gap, out)
				}
				if strings.Contains(tc.source, "BODYTWO") && (second < 0 || second-first != 2) {
					t.Fatalf("body paragraph missing or spacing changed: rows %d/%d:\n%s", first, second, out)
				}
			})
		}
	}
}

func TestCalloutRegistryAndMalformedMarkers(t *testing.T) {
	for _, tc := range []struct {
		marker, title string
		valid         bool
	}{
		{"[!summary]", "Abstract", true}, {"[!hint]+", "Tip", true}, {"[!attention]-", "Warning", true}, {"[!IMPORTANT]", "Important", true}, {"[!CAUTION]", "Caution", true}, {"[!experiment_test]", "Experiment test", true},
		{"[!123]", "", false}, {"[!bad type]", "", false}, {"[!note]oops", "", false}, {"[!note", "", false}, {"[!]", "", false},
	} {
		t.Run(tc.marker, func(t *testing.T) {
			for _, style := range []string{paletteStyleName, "dark", "light", "notty"} {
				out, _, _, err := renderDoc(imgCtx{}, "> "+tc.marker+"\n> BODY\n", 40, style)
				if err != nil {
					t.Fatal(err)
				}
				plain := ansi.Strip(out)
				if tc.valid && (!strings.Contains(plain, tc.title) || strings.Contains(plain, "[!")) {
					t.Fatalf("callout missing: %q", plain)
				}
				if !tc.valid && !strings.Contains(plain, tc.marker) {
					t.Fatalf("malformed marker consumed: %q", plain)
				}
				if !strings.Contains(plain, "BODY") {
					t.Fatal("body hidden")
				}
			}
		})
	}
}

func TestCalloutProseAndStructuralInvariance(t *testing.T) {
	for _, tc := range []struct{ name, prefix string }{
		{"root", "> "}, {"nested", "> outer\n> > "}, {"list", "- item\n  > "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rail := tc.prefix
			switch tc.name {
			case "nested":
				rail = "> > "
			case "list":
				rail = "  > "
			}
			source := tc.prefix + "[!note]- " + strings.Repeat("title words ", 20) + "\n" + rail + strings.Repeat("ordinary prose words ", 30) + "\n" + rail + "second source line\n" + rail + "\n" + rail + "```text\n" + rail + "CODE_" + strings.Repeat("x", 140) + "\n" + rail + "```\n" + rail + "\n" + rail + "| A | B |\n" + rail + "| - | - |\n" + rail + "| " + strings.Repeat("cell", 30) + " | token |\n" + rail + "\n" + rail + "> " + strings.Repeat("plain quote words ", 20) + "\n"
			for _, width := range []int{1, 20, 40, 80, 120} {
				t.Run(fmt.Sprint(width), func(t *testing.T) {
					out, _, _, err := renderDoc(imgCtx{}, source, width, paletteStyleName)
					if err != nil {
						t.Fatal(err)
					}
					plain := ansi.Strip(out)
					for _, line := range strings.Split(plain, "\n") {
						if width >= 20 && (strings.Contains(line, "ordinary") || strings.Contains(line, "title words")) && ansi.StringWidth(line) > width {
							t.Fatalf("prose overwide: %q", line)
						}
					}
					for _, token := range []string{"CODE_" + strings.Repeat("x", 140), strings.Repeat("cell", 30), strings.Repeat("plain quote words ", 19), "second source line"} {
						if width >= 20 && (token != "second source line" || width >= 40) && !strings.Contains(plain, token) {
							t.Fatalf("intrinsic/source line token lost %q: %q", token, plain)
						}
					}
					if strings.Contains(out, "\x1b]777;readmd-") || strings.Contains(ansi.Strip(out), "readmd-") || strings.Contains(plain, "[!note]") {
						t.Fatal("marker leakage")
					}
				})
			}
		})
	}
}

func TestCalloutTitleInlineLinksAndMetadata(t *testing.T) {
	for _, tc := range []struct{ name, title string }{
		{"explicit", "**Rich** [linked title words linked title words](https://example.com/title?x=1&y=2)"},
		{"reference", "**Rich** [linked title words linked title words][ref]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "> [!note] " + tc.title + "\n> body [linked body words linked body words](https://example.com/body) ![image label](image.png) after words\n\n[ref]: https://example.com/title?x=1&y=2\n"
			for _, width := range []int{20, 40, 80, 120} {
				out, _, _, err := renderDoc(imgCtx{}, source, width, paletteStyleName)
				if err != nil {
					t.Fatal(err)
				}
				links := parseRenderedLinkTargets(source, strings.Split(out, "\n"))
				seen := map[string]bool{}
				counts := map[string]int{}
				for _, link := range links {
					seen[link.dest] = true
					counts[link.dest]++
					for _, region := range link.regions {
						line := strings.Split(out, "\n")[region.line]
						if region.start < 0 || region.end > ansi.StringWidth(line) {
							t.Fatalf("invalid hit region: %+v", region)
						}
					}
				}
				for _, dest := range []string{"https://example.com/title?x=1&y=2", "https://example.com/body"} {
					if !seen[dest] || counts[dest] != 1 {
						t.Fatalf("link lost or fragmented %s: %+v %q", dest, links, out)
					}
				}
				if !strings.Contains(ansi.Strip(out), "image.png") || !strings.Contains(ansi.Strip(out), "Rich") {
					t.Fatal("inline title/image content lost")
				}
			}
		})
	}
}

func TestCustomCalloutPrecedenceAndOwnRails(t *testing.T) {
	for _, tc := range []struct{ name, marker, yaml, railFG, titleFG string }{
		{"canonical and alias", "summary", "callouts: {custom: {abstract: {color: 14}, summary: {title: {fg: 10}}}}", "96", "92"},
		{"explicit FG", "experiment", "callouts: {custom: {experiment: {color: 13, icon: X, rail: {fg: 12}, title: {fg: 10}}}}", "94", "92"},
		{"legacy last", "hint", "callouts: {custom: {tip: {color: 14}, hint: {color: 13}}, tip: {rail: {fg: 12}, title: {fg: 10}}}", "94", "92"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
				source := "> [!" + tc.marker + "] TITLE\n> BODY\n>\n> > PLAIN\n>\n> > [!warning] NESTED\n> > INSIDE\n>\n> ```text\n> │ CODE\n> ```\n"
				theme, err := config.ParseTheme([]byte(tc.yaml))
				if err != nil {
					t.Fatal(err)
				}
				out, _, _, err := renderThemedDoc(imgCtx{}, source, 40, base, theme)
				if err != nil {
					t.Fatal(err)
				}
				titleFG, railFG := tc.titleFG, tc.railFG
				if base == "notty" {
					titleFG = "default"
					railFG = "default"
				}
				if cell := themeToken(t, out, "TITLE"); cell.fg != titleFG {
					t.Fatalf("title precedence: %+v\n%q", cell, out)
				}
				if cell := themeToken(t, out, "│"); cell.fg != railFG {
					t.Fatalf("rail precedence: %+v", cell)
				}
				if cell := themeToken(t, out, "NESTED"); base != "notty" && cell.fg != "93" {
					t.Fatalf("nested callout recolored: %+v", cell)
				}
				if strings.Contains(out, "\x1b]777;readmd-") || strings.Contains(ansi.Strip(out), "readmd-") {
					t.Fatal("marker leakage")
				}
			}
		})
	}
}

func TestUnknownCalloutDoesNotInheritLegacyNote(t *testing.T) {
	for _, tc := range []struct{ marker, yaml, wantFG string }{
		{"experiment", "callouts: {note: {color: 9, icon: N}, custom: {experiment: {color: 13, icon: X}}}", "95"},
		{"unknown", "callouts: {note: {color: 9, icon: N}}", "94"},
	} {
		t.Run(tc.marker, func(t *testing.T) {
			out := renderThemeTest(t, "> [!"+tc.marker+"] TITLE\n> BODY\n", paletteStyleName, tc.yaml, 40)
			if cell := themeToken(t, out, "TITLE"); cell.fg != tc.wantFG {
				t.Fatalf("legacy note overrode own type: %+v", cell)
			}
			if strings.Contains(ansi.Strip(out), "N TITLE") {
				t.Fatal("legacy note icon inherited")
			}
		})
	}
}

func TestCustomCalloutSnapshotAndReaderRerender(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			source := "> [!experiment] TITLE\n> " + strings.Repeat("ordinary prose words ", 30) + "\n\n- " + strings.Repeat("list prose words ", 30) + "\n"
			theme, err := config.ParseTheme([]byte("callouts: {custom: {experiment: {color: 13, icon: X}}}"))
			if err != nil {
				t.Fatal(err)
			}
			m := New(source, "doc.md")
			if err := m.SetStyle("auto"); err != nil {
				t.Fatal(err)
			}
			m.SetTheme(theme)
			m.width, m.height = width, 25
			command := m.requestRender()
			entry := theme.Callouts.Custom["experiment"]
			*entry.Color = "9"
			*entry.Icon = "Y"
			delete(theme.Callouts.Custom, "experiment")
			msg := command().(renderedMsg)
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if themeToken(t, msg.content, "TITLE").fg != "95" || !strings.Contains(ansi.Strip(msg.content), "X TITLE") {
				t.Fatal("caller map mutation changed delayed snapshot")
			}
			m.rendering = false
			m.Update(msg)
			before := strings.Join(m.base, "\n")
			m.reader = true
			rerender := m.requestRender()().(renderedMsg)
			if rerender.err != nil {
				t.Fatal(rerender.err)
			}
			if m.source != source || strings.Join(m.base, "\n") != before {
				t.Fatal("rerender modified raw source or cache")
			}
			if themeToken(t, rerender.content, "TITLE").fg != "95" {
				t.Fatal("reader snapshot changed")
			}
			readerWidth, _ := m.readerGeom(true)
			for _, line := range strings.Split(rerender.content, "\n") {
				if ansi.StringWidth(line) > readerWidth {
					t.Fatalf("reader prose overwide: %q", line)
				}
			}
		})
	}
}

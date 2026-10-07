package pager

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/lmilojevicc/readmd/internal/config"
)

func reviewRenderedTarget(t *testing.T, m *Model, dest string) hintTarget {
	t.Helper()
	for _, target := range m.links {
		if target.dest == dest {
			return hintTarget{linkTarget: target}
		}
	}
	t.Fatalf("missing real rendered target %q: err=%q", dest, m.errMsg)
	return hintTarget{}
}

// The notty heading prefix distinguishes the actual heading from earlier labels/prose.
func reviewHeadingRows(t *testing.T, m *Model, prefix string) []int {
	t.Helper()
	var rows []int
	for i, line := range m.stripped {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			rows = append(rows, i)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no actual heading prefix %q in %q err=%q flash=%q", prefix, m.stripped, m.errMsg, m.flash)
	}
	return rows
}

func TestNavigationReviewRealHeadingActivation(t *testing.T) {
	for _, earlier := range []string{"[Target](#target)", "[go](#target)\n\nTarget", "[go](#target)\n\n- Target"} {
		for _, crossFile := range []bool{false, true} {
			t.Run(earlier+map[bool]string{false: " same", true: " cross"}[crossFile], func(t *testing.T) {
				root := t.TempDir()
				source := earlier + "\n\n" + strings.Repeat("filler\n\n", 15) + "# Target\n\n" + strings.Repeat("tail\n\n", 25)
				dest := "#target"
				origin := source
				if crossFile {
					dest = "next.md#target"
					navigationWrite(t, filepath.Join(root, "next.md"), source)
					origin = "[Target](" + dest + ")\n\n"
				}
				c := config.Defaults()
				c.Images = false
				c.Style = "notty"
				a, err := NewApplication(origin, "(stdin)", root, c, config.Theme{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(a.Close)
				_, render := a.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
				appFiniteCommands(t, a, render)
				navigationCommands(t, a, a.current.activateTarget(reviewRenderedTarget(t, a.current, dest)))
				rows := reviewHeadingRows(t, a.current, "# Target")
				if len(rows) != 1 || a.current.vp.YOffset() != rows[0] {
					t.Fatalf("jump y=%d actual heading rows=%v", a.current.vp.YOffset(), rows)
				}
			})
		}
	}
}

func TestNavigationReviewDelayedRequests(t *testing.T) {
	for _, change := range []string{"reload", "browser return"} {
		for _, kind := range []string{"footnote", "file", "anchor", "back"} {
			t.Run(change+" "+kind, func(t *testing.T) {
				a := testApplication(t, "(stdin)")
				m := a.current
				navigationWrite(t, filepath.Join(a.cwd, "next.md"), navigationSource("Next"))
				a.history = []navigationEntry{a.navigationSnapshot()}
				var delayed tea.Cmd
				switch kind {
				case "footnote":
					delayed = m.activateTarget(hintTarget{linkTarget: linkTarget{kind: targetFootnote, footnote: "1", definition: targetRegion{line: 20}}})
				case "file":
					delayed = m.activateTarget(hintTarget{linkTarget: linkTarget{dest: "next.md"}})
				case "anchor":
					delayed = m.activateTarget(hintTarget{linkTarget: linkTarget{dest: "#"}})
				case "back":
					delayed = m.backLocation()
				}
				if change == "reload" {
					_, render := a.Update(documentResult{owner: m, msg: reloadDoneMsg{body: []byte(navigationSource("Changed"))}})
					appFiniteCommands(t, a, render)
				} else {
					a.showBrowser()
					appFiniteCommands(t, a, a.returnReader())
				}
				m.vp.SetYOffset(7)
				location := m.currentLocation()
				history := len(a.history)
				pendingRead := a.navigate(navigationRequest{local: localLink{path: "next.md"}})
				pending := a.navigation
				_, next := a.Update(delayed())
				if next != nil || a.navigation != pending || a.current != m || m.currentLocation() != location || len(a.history) != history {
					t.Fatal("expired request changed state/canceled newer navigation")
				}
				// A newly created request must still execute normally.
				navigationCommands(t, a, m.activateTarget(hintTarget{linkTarget: linkTarget{dest: "#"}}))
				if m.vp.YOffset() != 0 || len(a.history) != history+1 || a.navigation != nil {
					t.Fatal("current request was rejected")
				}
				a.Update(pendingRead())
			})
		}
	}
}

func TestNavigationReviewStandaloneFootnoteIntent(t *testing.T) {
	for _, action := range []string{"resize", "reader"} {
		t.Run(action, func(t *testing.T) {
			source := "[Heading](#heading)\n\n# Heading\n\n" + strings.Repeat("filler\n\n", 25) + "# Footnote area\n\nreference[^one]\n\n[^one]: footnote body\n\n" + strings.Repeat("tail\n\n", 25)
			m := New(source, "review")
			if err := m.SetStyle("notty"); err != nil {
				t.Fatal(err)
			}
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
			m.Update(cmd())
			m.activateTarget(reviewRenderedTarget(t, m, "#heading"))
			var footnote linkTarget
			for _, target := range m.links {
				if target.kind == targetFootnote {
					footnote = target
					break
				}
			}
			if footnote.footnote == "" {
				t.Fatal("missing rendered footnote target")
			}
			m.activateTarget(hintTarget{linkTarget: footnote})
			if m.pendingHeading != nil {
				t.Error("footnote retained heading intent")
			}
			if action == "resize" {
				_, cmd = m.Update(tea.WindowSizeMsg{Width: 38, Height: 8})
			} else {
				cmd = m.handleNormalKey(keyMsg("r"))
			}
			m.Update(cmd())
			rows := reviewHeadingRows(t, m, "# Footnote area")
			if m.vp.YOffset() != rows[0] {
				t.Fatalf("after %s y=%d want footnote-area heading row=%d", action, m.vp.YOffset(), rows[0])
			}
		})
	}
}

func TestNavigationReviewHeadingProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, source, prefix string
		count                int
	}{
		{"duplicates", "[Dup](#dup)\n\nDup\n\n# Dup\n\nbody\n\n# Dup\n\n", "# Dup", 2},
		{"wrapped", "[Heading starts here and continues over several wrapped rows](#heading-starts-here-and-continues-over-several-wrapped-rows)\n\n# Heading starts here and continues over several wrapped rows\n\n", "# Heading", 1},
		{"quote", "[Nested](#nested)\n\n> ## Nested\n>\n> body\n\n", "| ## Nested", 1},
		{"callout", "[Nested](#nested)\n\n> [!NOTE]\n> ## Nested\n>\n> body\n\n", "│ ## Nested", 1},
		{"list", "[Nested](#nested)\n\n- prose\n\n  ## Nested\n\n  body\n\n", "## Nested", 1},
		{"list callout", "[Nested](#nested)\n\n- item\n\n  > [!TIP]\n  > ## Nested\n  >\n  > body\n\n", "│ ## Nested", 1},
		{"structural", "[After](#after)\n\n# Before\n\n```text\ncode\n```\n\n| Col | Data |\n| --- | --- |\n| text | text |\n\n```mermaid\ngraph LR\nA --> B\n```\n\n$$x^2$$\n\n# After\n\n", "# After", 1},
		{"setext", "[Setext](#setext)\n\nSetext\n======\n\n", "# Setext", 1},
	} {
		for _, width := range []int{20, 40, 80} {
			t.Run(tc.name+fmt.Sprint(width), func(t *testing.T) {
				m := New(tc.source+strings.Repeat("tail\n\n", 20), "positions")
				if err := m.SetStyle("notty"); err != nil {
					t.Fatal(err)
				}
				_, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 8})
				m.Update(cmd())
				if m.errMsg != "" {
					t.Fatal(m.errMsg)
				}
				var rows []int
				for row, line := range m.stripped {
					if strings.Contains(line, tc.prefix) {
						rows = append(rows, row)
					}
				}
				if len(rows) != tc.count {
					t.Fatalf("independent heading prefix %q rows=%v output=%q", tc.prefix, rows, m.stripped)
				}
				var got []int
				for _, h := range m.heads {
					if strings.Contains(tc.prefix, h.text) || tc.name == "wrapped" {
						got = append(got, h.line)
					}
					if h.srcLine < 0 || h.srcLine >= len(strings.Split(m.source, "\n")) {
						t.Fatal("invalid source line")
					}
				}
				if !reflect.DeepEqual(got, rows) {
					t.Fatalf("provenance rows=%v actual heading rows=%v", got, rows)
				}
				if m.source != tc.source+strings.Repeat("tail\n\n", 20) {
					t.Fatal("raw source mutated")
				}
				// Source line metadata is independently checked against actual Markdown lines.
				for _, h := range m.heads {
					line := strings.Split(m.source, "\n")[h.srcLine]
					if !strings.Contains(line, h.text) {
						t.Fatalf("heading %q source line=%q", h.text, line)
					}
				}
			})
		}
	}
}

func TestNavigationReviewHeadingMarkerInvariance(t *testing.T) {
	for _, source := range []string{
		"[Target](#target)\n\n# Target\n\n#\n\n## Dup\n\n## Dup\n\n",
		"# Long heading words with [label](#long-heading-words-with-label)\n\n> [!NOTE]\n> ## Nested\n>\n> - words with a [link](#nested)\n>\n> ```text\n> code\n> ```\n\n",
		"\ufeff# BOM\n\n# Math $x^2$\n\n![image](missing.png)\n\n# After\n\n",
	} {
		for _, style := range []string{paletteStyleName, "dark", "light", "notty"} {
			for _, width := range []int{1, 20, 40, 80} {
				for _, themed := range []bool{false, true} {
					t.Run(style+fmt.Sprint(width, themed)+source, func(t *testing.T) {
						theme := config.Theme{}
						if themed {
							var err error
							theme, err = config.ParseTheme([]byte("headings:\n  h1: {fg: 13, bg: 4}\n  h2: {bold: true}\nlinks: {fg: 11}\n"))
							if err != nil {
								t.Fatal(err)
							}
						}
						plain, _, _, err := renderThemedDoc(imgCtx{}, source, width, style, theme)
						if err != nil {
							t.Fatal(err)
						}
						marked, _, _, _, err := renderNavigationDoc(imgCtx{}, source, width, style, theme)
						if err != nil {
							t.Fatal(err)
						}
						if plain != marked {
							t.Fatalf("heading instrumentation changed fresh output\nplain=%q\nmarked=%q", plain, marked)
						}
						if strings.Contains(marked, "readmd-heading-") || strings.Contains(marked, "\x1b]778;") {
							t.Fatal("heading marker leaked")
						}
						if !reflect.DeepEqual(renderedTargets(source, strings.Split(plain, "\n"), splitStrip(plain)), renderedTargets(source, strings.Split(marked, "\n"), splitStrip(marked))) {
							t.Fatal("heading markers changed OSC8 hit regions")
						}
					})
				}
			}
		}
	}
}

func TestNavigationReviewNarrowHeadingStart(t *testing.T) {
	for _, width := range []int{1, 2, 3, 6} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			source := "[Target](#target)\n\n" + strings.Repeat("filler\n\n", 5) + "# Target\n\n" + strings.Repeat("tail\n\n", 15)
			m := New(source, "narrow")
			if err := m.SetStyle("notty"); err != nil {
				t.Fatal(err)
			}
			_, render := m.Update(tea.WindowSizeMsg{Width: width, Height: 8})
			m.Update(render())
			m.activateTarget(reviewRenderedTarget(t, m, "#target"))
			// At tiny widths the prefix can occupy a row before the title itself.
			rows := reviewHeadingRows(t, m, "#")
			if len(rows) != 1 || m.vp.YOffset() != rows[0] {
				t.Fatalf("narrow heading jump y=%d actual prefix rows=%v", m.vp.YOffset(), rows)
			}
		})
	}
}

func TestNavigationReviewFigureHeadingProvenance(t *testing.T) {
	for _, width := range []int{20, 40} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			t.Setenv("KITTY_WINDOW_ID", "heading-provenance")
			t.Setenv("TMUX", "")
			root := t.TempDir()
			f, err := os.Create(filepath.Join(root, "figure.png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 48, 64))); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			source := "[After](#after)\n\n# Before\n\n![figure](figure.png)\n\n# After\n\n" + strings.Repeat("tail\n\n", 20)
			m := New(source, "figure")
			m.SetPath(filepath.Join(root, "document.md"))
			if err := m.SetStyle("notty"); err != nil {
				t.Fatal(err)
			}
			m.SetImages(ImageConfig{})
			t.Cleanup(m.Close)
			_, render := m.Update(tea.WindowSizeMsg{Width: width, Height: 8})
			m.Update(render())
			if m.errMsg != "" {
				t.Fatal(m.errMsg)
			}
			if !strings.Contains(strings.Join(m.base, "\n"), string(kitty.Placeholder)) {
				t.Fatal("fixture did not render an actual figure")
			}
			m.activateTarget(reviewRenderedTarget(t, m, "#after"))
			rows := reviewHeadingRows(t, m, "# After")
			if len(rows) != 1 || m.vp.YOffset() != rows[0] || m.heads[1].srcLine != 6 {
				t.Fatalf("figure heading provenance y=%d actual rows=%v sourceLine=%d", m.vp.YOffset(), rows, m.heads[1].srcLine)
			}
		})
	}
}

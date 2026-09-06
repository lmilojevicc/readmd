package pager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

func stockNatural(t *testing.T, src, style string) string {
	t.Helper()
	opts := []glamour.TermRendererOption{glamour.WithWordWrap(0), glamour.WithTableWrap(false)}
	if style == paletteStyleName {
		registerPaletteChroma()
		opts = append(opts, glamour.WithStyles(paletteConfig), glamour.WithChromaFormatter("terminal16"))
	} else {
		opts = append(opts, glamour.WithStandardStyle(style))
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Render(src)
	if err != nil {
		t.Fatal(err)
	}
	return trimTrailing(out)
}

func TestAdapterIntrinsicContainersMatchStock(t *testing.T) {
	wide := strings.Repeat("wide cell content ", 12)
	for _, tc := range []struct{ name, src string }{
		{"table", "| First | Last |\n| - | - |\n| " + wide + " | end |\n"},
		{"code", "```go\n// " + wide + "\n\nfmt.Println(\"last\")\n```\n"},
		{"Mermaid", expandMermaid("```mermaid\nflowchart LR\nA[Alpha] --> B[Bravo] --> C[Charlie] --> D[Delta]\n```\n")},
		{"list", "3. " + wide + "\n   - nested\n     - deepest\n4. last\n"},
		{"quote", "> " + wide + "\n>\n> | A | B |\n> | - | - |\n> | first | last |\n"},
		{"definitions", "Term\n: " + wide + "\n\nAnother\n: definition\n"},
	} {
		for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
			t.Run(tc.name+"/"+style, func(t *testing.T) {
				want := stockNatural(t, tc.src, style)
				for _, w := range []int{40, 80, 120} {
					out, _, _, err := renderDoc(imgCtx{}, tc.src, w, style)
					if err != nil {
						t.Fatal(err)
					}
					// Chroma's terminal256 nearest-color ties vary in stock
					// dark/light output; compare geometry there. Palette ANSI
					// and every other container retain byte-exact baselines.
					if tc.name == "code" && (style == styles.DarkStyle || style == styles.LightStyle) {
						out, want = ansi.Strip(out), ansi.Strip(want)
					}
					if out != want {
						t.Fatalf("w%d differs from stock natural render\ngot=%q\nwant=%q", w, out, want)
					}
				}
			})
		}
	}
}

func TestAdapterProseWrapsAndReaderStaysNatural(t *testing.T) {
	for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
		t.Run(style, func(t *testing.T) {
			src := "# Heading with ordinary words that should wrap at the narrower viewport\n\n" + strings.Repeat("ordinary readable prose words ", 14) + "\n"
			prior := 0
			for _, w := range []int{40, 80, 120} {
				out, _, _, err := renderDoc(imgCtx{}, src, w, style)
				if err != nil {
					t.Fatal(err)
				}
				lines := splitStrip(out)
				if got := widestLine(lines); got > w {
					t.Fatalf("w%d prose width=%d", w, got)
				}
				if prior > 0 && len(lines) >= prior {
					t.Fatalf("w%d line count=%d prior=%d", w, len(lines), prior)
				}
				prior = len(lines)
				natural, _, _, err := renderDoc(imgCtx{Width: w}, src, 0, style)
				if err != nil {
					t.Fatal(err)
				}
				if natural != stockNatural(t, src, style) {
					t.Fatalf("reader differs from stock at w%d", w)
				}
				t.Logf("w%d prose: %d lines, widest %d; reader widest %d", w, len(lines), widestLine(lines), widestLine(splitStrip(natural)))
			}
		})
	}
}

func TestAdapterCorpusReaderMatchesBaseline(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/corpus/*.md")
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got, _, _, err := renderDoc(imgCtx{}, string(src), 0, styles.NoTTYStyle)
			if err != nil {
				t.Fatal(err)
			}
			want := stockNatural(t, string(src), styles.NoTTYStyle)
			if got != want {
				t.Fatalf("reader differs from baseline stock output\ngot=%q\nwant=%q", got, want)
			}
			t.Logf("baseline exact: %d lines, width %d", len(splitStrip(got)), widestLine(splitStrip(got)))
		})
	}
}

func TestAdapterGlobalReferencesAndBreaks(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			src := "[early]: https://early.example/path\n\n# [Linked heading][late]\n\nRead [before][early].\n\n> Read [inside][late].\n\nfirst hard  \nsecond hard\\\nthird line\n\nlast paragraph\n\n[late]: https://late.example/path\n"
			out, err := Render(src, w)
			if err != nil {
				t.Fatal(err)
			}
			for _, dest := range []string{"https://early.example/path", "https://late.example/path"} {
				found := false
				for _, reg := range rawLinkRegions(strings.Split(out, "\n")) {
					if reg.dest == dest {
						found = true
					}
				}
				if !found {
					t.Fatalf("lost reference destination %q: %q", dest, out)
				}
			}
			plain := ansi.Strip(out)
			if !strings.Contains(plain, "first hard\n  second hard\n  third line\n\n  last paragraph") {
				t.Fatalf("hard breaks or paragraph spacing changed: %q", plain)
			}
		})
	}
}

func TestAdapterExactTableCells(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		cells     []string
		absent    []string
	}{
		{"escaped pipes", "| Expression | Meaning | Code |\n| - | - | - |\n| a \\| b | either | `x \\| y` |\n| p\\|q | adjacent | `` `\\|\\|` `` |\n", []string{"Expression", "Meaning", "Code", "a | b", "either", "x | y", "p|q", "adjacent", "`||`"}, nil},
		{"ragged rows and empty headers", "| | Name | Third |\n| - | - | - |\n| x | y | z | discarded-extra |\n| only |\n| | middle | |\n", []string{"Name", "Third", "x", "y", "z", "only", "middle"}, []string{"discarded-extra"}},
		{"inline code pipe follows GFM delimiter semantics", "| A | B | C |\n| - | - | - |\n| `x|y` | last |\n", []string{"`x", "y`", "last"}, nil},
		{"clusters and long cell", "| Cluster | Narrative |\n| - | - |\n| 👍🏽 👨‍👩‍👧‍👦 e\u0301 東京 | " + strings.Repeat("uncapped prose ", 20) + "END |\n", []string{"👍🏽", "👨‍👩‍👧‍👦", "e\u0301", "東京", strings.Repeat("uncapped prose ", 20) + "END"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := stockNatural(t, tc.src, styles.NoTTYStyle)
			for _, w := range []int{40, 80, 120} {
				out, err := Render(tc.src, w)
				if err != nil {
					t.Fatal(err)
				}
				if out != baseline {
					t.Fatalf("w%d grid differs from stock", w)
				}
				plain := ansi.Strip(out)
				for _, cell := range tc.cells {
					if !strings.Contains(plain, cell) {
						t.Fatalf("w%d lost exact cell %q: %q", w, cell, plain)
					}
				}
				for _, cell := range tc.absent {
					if strings.Contains(plain, cell) {
						t.Fatalf("GFM excess cell %q was not discarded", cell)
					}
				}
			}
			t.Logf("exact cell tokens and baseline grid, width %d", widestLine(splitStrip(baseline)))
		})
	}
}

func TestAdapterMixedGeometry(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			table := "| First | Last |\n| - | - |\n| " + strings.Repeat("wide ", 30) + " | end |\n"
			code := "```\n" + strings.Repeat("code", 40) + "\n```\n"
			mermaid := "```mermaid\nflowchart LR\nA[Alpha] --> B[Bravo] --> C[Charlie] --> D[Delta]\n```\n"
			prose := strings.Repeat("prose wraps independently ", 20)
			src := prose + "\n\n" + table + "\n" + prose + "\n\n" + code + "\n" + mermaid + "\n" + prose
			out, err := Render(src, w)
			if err != nil {
				t.Fatal(err)
			}
			for _, block := range []string{table, code, expandMermaid(mermaid)} {
				want := strings.Trim(stockNatural(t, block, styles.NoTTYStyle), "\n")
				if !strings.Contains(out, want) {
					t.Fatalf("w%d composed output lost complete stock block %q", w, want)
				}
			}
		})
	}
}

func TestAdapterConcurrentRenders(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
			t.Run(fmt.Sprintf("%s/w%d", style, w), func(t *testing.T) {
				t.Parallel()
				src := "# [Heading][ref]\n\n" + strings.Repeat("text with soft\nbreaks and ", 10) + "[link][ref]\n\n> quote\n> continues\n\n| A | B |\n| - | - |\n| first | last |\n\n[ref]: https://example.org/guide\n"
				want, _, _, err := renderDoc(imgCtx{}, src, w, style)
				if err != nil {
					t.Fatal(err)
				}
				for range 10 {
					out, _, _, err := renderDoc(imgCtx{}, src, w, style)
					if err != nil {
						t.Fatal(err)
					}
					if out != want {
						t.Fatal("concurrent rendering changed private AST or shared style state")
					}
				}
			})
		}
	}
}

func TestAdapterFinalFixtureMeasures(t *testing.T) {
	for _, fixture := range []string{"../../example.md", "../../testdata/corpus/wide-table.md"} {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			data, err := os.ReadFile(fixture)
			if os.IsNotExist(err) && fixture == "../../example.md" {
				t.Skipf("optional local fixture unavailable: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, style := range []string{styles.NoTTYStyle, paletteStyleName} {
				for _, width := range []int{40, 80, 120} {
					for _, reader := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/w%d/reader=%v", style, width, reader), func(t *testing.T) {
							wrap := width
							if reader {
								wrap = 0
							}
							out, _, _, err := renderDoc(imgCtx{Width: width}, string(data), wrap, style)
							if err != nil {
								t.Fatal(err)
							}
							lines := splitStrip(out)
							heads := extractHeadings(string(data))
							mapHeadings(heads, lines)
							mapped := 0
							for _, h := range heads {
								if h.line < len(lines) {
									mapped++
								}
							}
							links := renderedTargets(string(data), strings.Split(out, "\n"), lines)
							t.Logf("rows=%d widest=%d headings=%d/%d targets=%d footnotes=%d", len(lines), widestLine(lines), mapped, len(heads), len(links), countFootnoteTargets(links))
							if mapped != len(heads) {
								t.Fatal("fixture headings lost")
							}
						})
					}
				}
			}
		})
	}
}

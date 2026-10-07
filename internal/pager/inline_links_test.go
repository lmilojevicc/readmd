package pager

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestRenderedProseOccurrenceIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		counts       map[string]int
	}{
		{"callout title body image", "> [!note] [long title words long title words](https://same.example)\n> [long body words long body words](https://same.example) ![image label](image.png) [last label](https://same.example)\n", map[string]int{"https://same.example": 3, "image.png": 1}},
		{"list", "- [long label words long label words](https://same.example) [second label](https://same.example)\n  - [third label](https://same.example)\n", map[string]int{"https://same.example": 3}},
		{"mixed stock and identified", "[top words](https://same.example)\n\n- [list words list words list words](https://same.example) ![label](image.png)\n\n| A | B |\n| - | - |\n| [table](https://same.example) | cell |\n\n> [quote words](https://same.example)\n\n- nested\n\n  | A | B |\n  | - | - |\n  | [nested](https://same.example) | cell |\n", map[string]int{"https://same.example": 5, "image.png": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, width := range []int{20, 40, 80, 120} {
				out, _, _, err := renderDoc(imgCtx{}, tc.source, width, paletteStyleName)
				if err != nil {
					t.Fatal(err)
				}
				counts := map[string]int{}
				lines := strings.Split(out, "\n")
				for _, target := range parseRenderedLinkTargets(tc.source, lines) {
					counts[target.dest]++
					for _, region := range target.regions {
						text := ansi.Strip(ansi.Cut(lines[region.line], region.start, region.end))
						if text == "" || strings.Contains(text, "│") {
							t.Fatalf("bad region %+v: %q", region, text)
						}
					}
				}
				for dest, want := range tc.counts {
					if counts[dest] != want {
						t.Fatalf("w%d target %s count=%d want=%d; all=%v", width, dest, counts[dest], want, counts)
					}
				}
			}
		})
	}
}

func TestRenderedHeadingAnchorLinks(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		counts       map[string]int
	}{
		{"paragraph", "[label](#heading) and [top](#)\n\n# Heading\n", map[string]int{"#heading": 1, "#": 1}},
		{"heading", "# **[heading label](#heading)**\n", map[string]int{"#heading": 1}},
		{"nested lists", "- **[界 👩‍💻 long label words long label words](#Heading%20Name)**\n  - [top](#) [repeated](#Heading%20Name)\n", map[string]int{"#Heading%20Name": 2, "#": 1}},
		{"callout", "> [!tip] **[long title words long title words](#heading)**\n> [long body words long body words](#heading)\n", map[string]int{"#heading": 2}},
		{"top table", "| A | B |\n| - | - |\n| **[label](#heading)** | [top](#) |\n", map[string]int{"#heading": 1, "#": 1}},
		{"nested table", "- container\n\n  | A | B | C |\n  | - | - | - |\n  | **[界 👩‍💻 label](#heading)** [repeat](#heading) | [external](https://example.com) | [top](#) |\n", map[string]int{"#heading": 2, "#": 1, "https://example.com": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme, err := config.ParseTheme([]byte("links: {fg: 10, bg: 4}\nstrong: {italic: true}"))
			if err != nil {
				t.Fatal(err)
			}
			for _, base := range []string{paletteStyleName, "dark", "light", "notty"} {
				for _, width := range []int{20, 40, 80, 120} {
					out, _, _, err := renderThemedDoc(imgCtx{}, tc.source, width, base, theme)
					if err != nil {
						t.Fatal(err)
					}
					lines := strings.Split(out, "\n")
					counts := map[string]int{}
					for _, target := range parseRenderedLinkTargets(tc.source, lines) {
						counts[target.dest]++
						for _, region := range target.regions {
							text := ansi.Strip(ansi.Cut(lines[region.line], region.start, region.end))
							if text == "" || strings.Contains(text, "│") {
								t.Fatalf("bad anchor region %+v: %q", region, text)
							}
						}
					}
					for dest, want := range tc.counts {
						if counts[dest] != want {
							t.Fatalf("%s w%d %s count=%d want=%d: %q", base, width, dest, counts[dest], want, out)
						}
					}
					if strings.Contains(ansi.Strip(out), "#heading") || strings.Contains(ansi.Strip(out), "#Heading%20Name") {
						t.Fatal("anchor printed visible href")
					}
					if tc.name == "nested table" {
						plain := ansi.Strip(out)
						if !strings.Contains(plain, "external[1]") || strings.Contains(plain, "[2]") {
							t.Fatalf("external footer renumbered/orphaned: %q", plain)
						}
						if strings.Count(plain, "👩‍💻") != 1 {
							t.Fatal("anchor grapheme lost")
						}
					}
					if strings.Contains(out, "\x1b]777;readmd-") {
						t.Fatal("role marker leaked")
					}
				}
			}
		})
	}
}

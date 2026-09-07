package pager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBlockquotePreservesSourceLinesAndIndentation(t *testing.T) {
	const first = "A blockquote nested inside a list item."
	const second = "Second line of wisdom."
	for _, tc := range []struct {
		name, src string
	}{
		{"direct", "> " + first + "\n> " + second + "\n"},
		{"list nested", "1. List item\n\n   > " + first + "\n   > " + second + "\n\n2. Next item\n"},
		{"deep list nested", "- Outer item\n  - Inner item\n\n    > " + first + "\n    > " + second + "\n"},
	} {
		for _, style := range []string{paletteStyleName, "notty", "dark", "light"} {
			for _, width := range []int{0, 40, 80, 120} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, style, width), func(t *testing.T) {
					out, _, _, err := renderDoc(imgCtx{}, tc.src, width, style)
					if err != nil {
						t.Fatal(err)
					}
					lines := splitStrip(out)
					a, b := findLine(lines, first), findLine(lines, second)
					if a < 0 || b != a+1 {
						t.Fatalf("quote source lines collapsed/moved:\n%s", ansi.Strip(out))
					}
					prefixA := lines[a][:strings.Index(lines[a], first)]
					prefixB := lines[b][:strings.Index(lines[b], second)]
					if prefixA != prefixB {
						t.Fatalf("rail/indentation differs: %q / %q", prefixA, prefixB)
					}
					if style == paletteStyleName && !strings.Contains(prefixA, "│") {
						t.Fatalf("quote rail missing: %q", prefixA)
					}
					baseline := splitStrip(stockNatural(t, tc.src, style))
					original := baseline[findLine(baseline, first)]
					originalPrefix := original[:strings.Index(original, first)]
					if prefixA != originalPrefix {
						t.Fatalf("stock list/rail indentation changed: %q want %q", prefixA, originalPrefix)
					}

				})
			}
		}
	}
}

func TestParagraphSoftBreaksStillFlow(t *testing.T) {
	for _, prefix := range []string{"", "- "} {
		for _, width := range []int{0, 80} {
			t.Run(fmt.Sprintf("%q/%d", prefix, width), func(t *testing.T) {
				src := prefix + "ordinary paragraph\n"
				if prefix != "" {
					src += "  "
				}
				src += "continues naturally\n"
				out, _, _, err := renderDoc(imgCtx{}, src, width, paletteStyleName)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(ansi.Strip(out), "ordinary paragraph continues naturally") {
					t.Fatalf("ordinary soft break preserved unexpectedly: %q", ansi.Strip(out))
				}
			})
		}
	}
}

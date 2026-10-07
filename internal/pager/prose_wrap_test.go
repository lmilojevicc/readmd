package pager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestListProseWrapStructuralInvariance(t *testing.T) {
	for _, tc := range []struct{ name, prefix string }{
		{"bullet", "- "}, {"number9", "9. "}, {"number10", "10. "}, {"number100", "100. "}, {"task", "- [x] "}, {"nested", "- parent\n  - "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.prefix + strings.Repeat("ordinary prose words ", 16) + "\n\n    ```text\n    STRUCTURAL_" + strings.Repeat("x", 140) + "\n    ```\n"
			for _, width := range []int{1, 20, 40, 80, 120} {
				t.Run(fmt.Sprint(width), func(t *testing.T) {
					out, _, _, err := renderDoc(imgCtx{}, source, width, paletteStyleName)
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range strings.Split(ansi.Strip(out), "\n") {
						if width >= 20 && strings.Contains(line, "ordinary") && ansi.StringWidth(line) > width {
							t.Fatalf("prose exceeds %d columns: %q", width, line)
						}
					}
					if !strings.Contains(ansi.Strip(out), "STRUCTURAL_"+strings.Repeat("x", 140)) {
						t.Fatal("structural line reflowed")
					}
				})
			}
		})
	}
}

func TestListProsePublicInlineInvariance(t *testing.T) {
	for _, tc := range []struct{ name, source, theme string }{
		{"links", "- [label words repeated label words repeated](https://example.com/a?b=1&c=2) after words after words\n", "links: {bg: 4}\nstrong: {bg: 5}"},
		{"image fallback", "- before words before words ![image label](image.png) after words after words\n", ""},
		{"task glyph", "- [x] " + strings.Repeat("words ", 30) + "\n", "tasks: {checked: {glyph: '[DONE]', bg: 4}}"},
		{"long token", "- " + strings.Repeat("x", 140) + "\n", ""},
		{"hard breaks", "- first line  \n  second line\n", ""},
		{"unicode", "- " + strings.Repeat("界 👩‍💻 é ", 20) + "\n", "strong: {bg: 4}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme, err := config.ParseTheme([]byte(tc.theme))
			if err != nil {
				t.Fatal(err)
			}
			for _, style := range []string{paletteStyleName, "notty"} {
				for _, width := range []int{20, 40, 80, 120} {
					out, _, _, err := renderThemedDoc(imgCtx{}, tc.source, width, style, theme)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(out, "\x1b]777;readmd-") || strings.Contains(ansi.Strip(out), "readmd-") {
						t.Fatalf("marker leakage: %q", out)
					}
					for _, line := range strings.Split(out, "\n") {
						if tc.name != "long token" && !strings.Contains(ansi.Strip(line), "https://example.com/a?b=1&c=2") && ansi.StringWidth(line) > width {
							t.Fatalf("%s w%d: overwide prose %q", style, width, line)
						}
					}
					switch tc.name {
					case "long token":
						if !strings.Contains(ansi.Strip(out), strings.Repeat("x", 140)) {
							t.Fatal("unbreakable token reflowed")
						}
					case "links":
						if !strings.Contains(out, "https://example.com/a?b=1&c=2") {
							t.Fatal("OSC8 target lost")
						}
					case "image fallback":
						if !strings.Contains(ansi.Strip(out), "image.png") {
							t.Fatal("image fallback lost")
						}
					case "hard breaks":
						if strings.Contains(ansi.Strip(out), "first line second") {
							t.Fatal("hard break lost")
						}
					case "unicode":
						if strings.Count(ansi.Strip(out), "👩‍💻") != 20 || strings.Count(ansi.Strip(out), "é") != 20 {
							t.Fatal("graphemes split/lost")
						}
					}
				}
			}
		})
	}
}

func TestListPlainQuoteRemainsIntrinsic(t *testing.T) {
	for _, source := range []string{"- > " + strings.Repeat("quote words ", 30) + "\n  > second line\n", "> - " + strings.Repeat("quote list words ", 30) + "\n"} {
		t.Run(source[:4], func(t *testing.T) {
			narrow, _, _, err := renderDoc(imgCtx{}, source, 20, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			wide, _, _, err := renderDoc(imgCtx{}, source, 120, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			if narrow != wide {
				t.Fatal("ordinary quote width policy changed")
			}
		})
	}
}

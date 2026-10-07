package pager

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCalloutTitleBodyInlineBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		linked       bool
	}{
		{"emphasis", "> [!note] **TITLE\n> BODY**\n", false},
		{"link", "> [!note] [TITLE\n> BODY](https://example.com)\n", true},
		{"anchor", "> [!note] [TITLE\n> BODY](#heading)\n", true},
		{"code span", "> [!note] `TITLE\n> BODY`\n", false},
		{"strike", "> [!note] ~~TITLE\n> BODY~~\n", false},
		{"strike link", "> [!note] ~~[TITLE\n> BODY](https://example.com)~~\n", false},
		{"unsupported image split", "> [!note] ![TITLE\n> BODY](image.png)\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, width := range []int{20, 40, 80, 120} {
				out := renderThemeTest(t, tc.source, paletteStyleName, "callouts: {title: {fg: 10}}", width)
				if !strings.Contains(ansi.Strip(out), "TITLE") || !strings.Contains(ansi.Strip(out), "BODY") {
					t.Fatalf("content lost: %q", out)
				}
				if tc.name == "unsupported image split" {
					if !strings.Contains(ansi.Strip(out), "[!note]") {
						t.Fatal("unsupported split must retain stock quote")
					}
					continue
				}
				if cell := themeToken(t, out, "TITLE"); !tc.linked && tc.name != "code span" && cell.fg != "92" {
					t.Fatalf("title style missing: %+v", cell)
				}
				if cell := themeToken(t, out, "BODY"); cell.fg == "92" {
					t.Fatalf("body absorbed into title: %+v %q", cell, out)
				}
				if tc.name == "strike link" && strings.Contains(out, "\x1b]8;") {
					t.Fatal("stock strike link flattening changed")
				}
				if tc.linked {
					links := parseRenderedLinkTargets(tc.source, strings.Split(out, "\n"))
					if len(links) != 1 || len(links[0].regions) < 2 {
						t.Fatalf("crossing link identity lost: %+v %q", links, out)
					}
					if strings.Count(ansi.Strip(out), "https://example.com") > 1 {
						t.Fatal("crossing link duplicated visible destination")
					}
				}
			}
		})
	}
}

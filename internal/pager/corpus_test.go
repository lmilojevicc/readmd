package pager

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func codeLines(src string) map[string]bool {
	set := map[string]bool{}
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		fcb, ok := n.(*ast.FencedCodeBlock)
		if !ok || fcb.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		for _, l := range strings.Split(string(fcb.Lines().Value(bsrc)), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				set[l] = true
			}
		}
		return ast.WalkContinue, nil
	})
	return set
}

var corpusTokens = map[string][]string{
	"alerts.md":        {"Caution: proceed carefully."},
	"cjk-paragraph.md": {"横方向のパン", "横向平移"},
	"cjk-table.md":     {"東京タワー", "서울"},
	"code-blocks.md":   {"plain fence no language"},
	"emoji-table.md":   {"thumbs up", "rocket"},
	"escaped-pipes.md": {"either", "adjacent"},
	"footnotes.md":     {"first note", "More prose"},
	"front-matter.md":  {"After Front Matter", "Body content follows"},
	"gfm-alerts.md":    {"critical content", "optional advice"},
	"long-tokens.md":   {"aVeryLongIdentifierName_With_Underscores_And_More_0123456789"},
	"nested-lists.md":  {"deepest content here", "mixed bullet"},
	"quote-list.md":    {"quoted inside list", "deepest"},
	"ragged-table.md":  {"four cells", "Empty header below"},
	"raw-html.md":      {"inside a div", "bold html"},
	"task-lists.md":    {"nested unchecked", "ordered done"},
	"wide-table.md":    {"Titanium Spork", "Camp Chair Deluxe"},
}

func TestGoldenCorpus(t *testing.T) {
	files, err := filepath.Glob("../../testdata/corpus/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob corpus: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		name := filepath.Base(f)
		wideRawWidth := 0
		doc := string(src)
		for _, width := range []int{40, 80, 120} {
			out, err := Render(doc, width)
			label := name + "/w" + strconv.Itoa(width)
			if err != nil {
				t.Errorf("%s: %v", label, err)
				continue
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("%s: empty render", label)
			}
			plain := ansi.Strip(out)
			for _, want := range corpusTokens[name] {
				tokenText := plain
				// Stock wrapping may split prose tokens, but tables must
				// retain exact cell strings (never normalize table cells).
				if name == "cjk-paragraph.md" || name == "long-tokens.md" {
					tokenText = strings.Map(func(r rune) rune {
						if unicode.IsSpace(r) {
							return -1
						}
						return r
					}, plain)
				}
				if !strings.Contains(tokenText, want) {
					t.Errorf("%s: lost content token %q", label, want)
				}
			}
			if name == "emoji-table.md" {
				for _, cluster := range []string{"👍🏽", "👨‍👩‍👧‍👦", "🏴󠁧󠁢󠁳󠁣󠁴󠁿"} {
					if !strings.Contains(plain, cluster) {
						t.Errorf("%s: split or lost grapheme cluster %q", label, cluster)
					}
				}
			}
			if name == "wide-table.md" {
				for _, want := range []string{"Titanium Spork", "dishwasher safe", "Camp Chair Deluxe", "folds flat"} {
					if !strings.Contains(plain, want) {
						t.Errorf("%s: wide table lost first/last row token %q", label, want)
					}
				}
				got := widestLine(strings.Split(plain, "\n"))
				if got <= 40 {
					t.Errorf("%s: wide table geometry collapsed to %d columns", label, got)
				}
				if wideRawWidth == 0 {
					wideRawWidth = got
				} else if got != wideRawWidth {
					t.Errorf("%s: wide table geometry changed from %d to %d columns", label, wideRawWidth, got)
				}
			}
		}
	}
}

func TestRenderPreservesCodeLines(t *testing.T) {
	files, err := filepath.Glob("../../testdata/corpus/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob corpus: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		code := codeLines(string(src))
		if len(code) == 0 {
			continue
		}
		out, err := Render(string(src), 40)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		got := map[string]bool{}
		for _, l := range strings.Split(out, "\n") {
			got[strings.TrimSpace(ansi.Strip(l))] = true
		}
		for l := range code {
			if !got[l] {
				t.Errorf("%s: rendered output lost code line %q", filepath.Base(f), l)
			}
		}
	}
}

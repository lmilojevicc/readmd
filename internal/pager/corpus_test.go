package pager

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
		collapsed := collapseTables(string(src))
		if collapsed == string(src) && strings.Contains(name, "table") && name != "code-blocks.md" {
			t.Errorf("%s: expected tables to collapse", name)
		}
		code := codeLines(string(src))
		for _, doc := range []struct{ label, text string }{
			{"raw", string(src)},
			{"collapsed", collapsed},
		} {
			for _, width := range []int{40, 80, 120} {
				for _, wrap := range []bool{true, false} {
					out, err := Render(doc.text, width, wrap)
					label := name + "/" + doc.label + "/w" + strconv.Itoa(width) + "/wrap=" + strconv.FormatBool(wrap)
					if err != nil {
						t.Errorf("%s: %v", label, err)
						continue
					}
					if strings.TrimSpace(out) == "" {
						t.Errorf("%s: empty render", label)
						continue
					}
					if !wrap {
						continue
					}
					for i, line := range strings.Split(out, "\n") {
						if w := ansi.StringWidth(line); w > width && !code[strings.TrimSpace(ansi.Strip(line))] {
							t.Errorf("%s: line %d width %d > %d: %q",
								label, i, w, width, ansi.Strip(line))
							break
						}
					}
				}
			}
		}
	}
}

func TestSplicePreservesCodeLines(t *testing.T) {
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
		out, err := Render(string(src), 40, true)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		got := map[string]bool{}
		for _, l := range strings.Split(out, "\n") {
			got[strings.TrimSpace(ansi.Strip(l))] = true
		}
		for l := range code {
			if !got[l] {
				t.Errorf("%s: spliced output lost code line %q", filepath.Base(f), l)
			}
		}
	}
}

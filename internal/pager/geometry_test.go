package pager

import (
	"strings"
	"testing"
)

var srcDoc = "# Alpha\n\nintro text here\n\n## Beta\n\n" +
	strings.Repeat("beta body line\n", 12) + "\n## Gamma\n\n" + strings.Repeat("gamma tail line\n", 12)

func bodyOf(m *Model) string {
	lines := strings.Split(m.View().Content, "\n")
	return strings.Join(lines[:max(0, len(lines)-1)], "\n")
}

func TestHeadingSourceLines(t *testing.T) {
	src := "# Top\n\ntitle\n======\n\n##\n\n## Deep\n\nbody\n"
	for _, tc := range []struct {
		text string
		want int
	}{
		{"Top", 0},
		{"title", 2},
		{"Deep", 7},
	} {
		var got = -1
		for _, h := range extractHeadings(src) {
			if h.text == tc.text {
				got = h.srcLine
			}
		}
		if got != tc.want {
			t.Errorf("%s: srcLine %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestGeometryRefreshClampsAfterReloadAndResize(t *testing.T) {
	for _, reader := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reader"}[reader], func(t *testing.T) {
			m := newRenderedModel(t, "```\n"+strings.Repeat("界", 100)+"\n```\n", 60, 10)
			if reader {
				settle(t, m, press(m, "r"))
			}
			for range 5 {
				press(m, "l")
			}
			if m.vp.XOffset() == 0 {
				t.Fatal("precondition: no horizontal pan")
			}
			raw := "# Short\n\nraw **markdown**\n"
			m.path = "doc.md"
			m.readFile = func(string) ([]byte, error) { return []byte(raw), nil }
			settle(t, m, press(m, "R"))
			if m.source != raw || m.vp.XOffset() != 0 || m.widest != widestLine(m.stripped) {
				t.Fatal("reload did not refresh raw source/widest/clamp")
			}
			m.width = 40
			m.syncVPWidth()
			if m.vp.XOffset() != 0 {
				t.Fatal("width sync used stale widest")
			}
		})
	}
}

package pager

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestUnclosedFence(t *testing.T) {
	for _, src := range []string{
		"```go\nx := 1",
		"```go\nx := 1\n",
		"prose\n\n```\n\n```",
	} {
		out, err := Render(src, 40, true)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if strings.Contains(out, "readmd-code-") {
			t.Errorf("%q: sentinel leaked into output", src)
		}
	}
}

// A fence immediately followed by prose used to merge the code end sentinel
// into the prose paragraph; below ~31 columns glamour word-wrapped the token
// and fragments leaked. The blank-line separator plus the splice-anomaly
// re-render must keep every width clean.
func TestFenceProseAdjacencyNoLeak(t *testing.T) {
	src := "```go\nx := 1\n```\nprose text here\n"
	for _, w := range []int{20, 25, 30} {
		t.Run(strconv.Itoa(w), func(t *testing.T) {
			out, err := Render(src, w, true)
			if err != nil {
				t.Fatal(err)
			}
			s := ansi.Strip(out)
			if strings.Contains(s, "readmd-code-") {
				t.Errorf("w%d: sentinel fragment leaked: %q", w, s)
			}
			if !strings.Contains(s, "x := 1") || !strings.Contains(s, "prose text here") {
				t.Errorf("w%d: content lost: %q", w, s)
			}
		})
	}
}

func TestSentinelNonceDiffersPerRender(t *testing.T) {
	src := "```go\nx\n```\n"
	a, _ := insertCodeSentinels(src)
	b, _ := insertCodeSentinels(src)
	if a == b {
		t.Fatal("sentinels must differ between renders")
	}
	if !strings.HasPrefix(a, "```go\nreadmd-code-") {
		t.Fatalf("unexpected sentinel placement: %q", a)
	}
}

func TestDocLiteralMarkerDoesNotBreakSplice(t *testing.T) {
	src := "readmd-code-0-start prose mentioning a marker\n\n```go\nx\n```\n"
	out, err := Render(src, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(ansi.Strip(out), "readmd-code-"); got != 1 {
		t.Fatalf("doc literal must survive once, splice must not leak: %d in %q", got, ansi.Strip(out))
	}
}

func TestSpliceFallbackStripsMarkers(t *testing.T) {
	blocks := []codeBlock{{
		content:  "hidden",
		startTok: "readmd-code-tok-0-start",
		endTok:   "readmd-code-tok-0-end",
	}}
	for _, tc := range []struct {
		name string
		out  string
		want string
	}{
		{
			name: "duplicate start token",
			out:  "a\nreadmd-code-tok-0-start\nb\nreadmd-code-tok-0-start\nc\nreadmd-code-tok-0-end\nd\n",
			want: "a\nb\nc\nd\n",
		},
		{
			name: "missing end token",
			out:  "a\nreadmd-code-tok-0-start\nb\n",
			want: "a\nb\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spliceCodeBlocks(tc.out, blocks, ""); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWrappedWidthBound(t *testing.T) {
	src := "# T\n\nlong prose line that must wrap well below the limit yes\n\n| A | B |\n| - | - |\n| x | y |\n"
	for _, width := range []int{20, 40, 79} {
		out, err := Render(src, width, true)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w > width {
				t.Fatalf("w%d line %d = %d cols: %q", width, i, w, ansi.Strip(l))
			}
		}
	}
}

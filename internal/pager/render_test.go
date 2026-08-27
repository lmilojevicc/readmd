package pager

import (
	"fmt"
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
		out, err := Render(src, 40)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if strings.Contains(out, "readmd-code-") {
			t.Errorf("%q: sentinel leaked into output", src)
		}
	}
}

// A fence immediately followed by prose must preserve both blocks.
func TestFenceProseAdjacencyNoLeak(t *testing.T) {
	src := "```go\nx := 1\n```\nprose text here\n"
	out, err := Render(src, 20)
	if err != nil {
		t.Fatal(err)
	}
	s := ansi.Strip(out)
	if strings.Contains(s, "readmd-code-") {
		t.Errorf("sentinel fragment leaked: %q", s)
	}
	if !strings.Contains(s, "x := 1") || !strings.Contains(s, "prose text here") {
		t.Errorf("content lost: %q", s)
	}
}

func TestDocLiteralMarkerDoesNotBreakSplice(t *testing.T) {
	src := "readmd-code-0-start prose mentioning a marker\n\n```go\nx\n```\n"
	out, err := Render(src, 40)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(ansi.Strip(out), "readmd-code-"); got != 1 {
		t.Fatalf("doc literal must survive once, splice must not leak: %d in %q", got, ansi.Strip(out))
	}
}

func TestWideTableNaturalGeometry(t *testing.T) {
	var header, delimiter, row strings.Builder
	for i := range 20 {
		fmt.Fprintf(&header, "| column-%02d ", i)
		delimiter.WriteString("| --- ")
		fmt.Fprintf(&row, "| endpoint-%02d.internal:84%02d ", i, i)
	}
	header.WriteString("|\n")
	delimiter.WriteString("|\n")
	row.WriteString("|\n")
	out, err := Render(header.String()+delimiter.String()+row.String(), 40)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"column-00", "column-19", "endpoint-00.internal:8400", "endpoint-19.internal:8419"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("wide table lost token %q", want)
		}
	}
	if widestLine(strings.Split(plain, "\n")) <= 40 {
		t.Fatal("wide table must retain geometry beyond the viewport")
	}
}

func TestNaturalWidthIndependentOfViewport(t *testing.T) {
	src := "# T\n\nlong prose line that must remain intact across every viewport width\n\n" +
		"| Service identifier | Internal endpoint |\n| - | - |\n| auth-api | auth.internal:8443 |\n"
	var first string
	for _, width := range []int{20, 40, 79} {
		out, err := Render(src, width)
		if err != nil {
			t.Fatal(err)
		}
		plain := ansi.Strip(out)
		for _, want := range []string{"long prose line that must remain intact", "Service identifier", "auth.internal:8443"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("w%d lost token %q: %q", width, want, plain)
			}
		}
		if first == "" {
			first = plain
		} else if plain != first {
			t.Fatalf("natural-width output changed at width %d", width)
		}
		if width == 20 && widestLine(strings.Split(plain, "\n")) <= width {
			t.Fatalf("w%d precondition: output did not overflow", width)
		}
	}
}

package pager

import (
	"os"
	"testing"
)

func TestRenderWidths(t *testing.T) {
	src, err := os.ReadFile("../../testdata/sample.md")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, width := range []int{40, 80, 120} {
		out, err := Render(string(src), width, true)
		if err != nil {
			t.Errorf("width %d: %v", width, err)
			continue
		}
		if out == "" {
			t.Errorf("width %d: empty output", width)
		}
	}
}

func TestSanitize(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"bom", "\ufeffhello", "hello"},
		{"control chars", "a\x00b\x07c\x7f", "a\ufffdb\ufffdc\ufffd"},
		{"kitty placeholder preserved", "a\U0010EEEE\u0305\u030D b", "a\U0010EEEE\u0305\u030D b"},
	} {
		if got := sanitize(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

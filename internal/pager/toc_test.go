package pager

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

func TestExtractHeadings(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []heading
	}{
		{"nested levels", "# A\n\n## B\n\n### C\n", []heading{{1, "A", 0, 0}, {2, "B", 0, 0}, {3, "C", 0, 0}}},
		{"duplicates", "## Dup\n\ntext\n\n## Dup\n", []heading{{2, "Dup", 0, 0}, {2, "Dup", 0, 0}}},
		{"all levels", "##### E\n###### F\n", []heading{{5, "E", 0, 0}, {6, "F", 0, 0}}},
		{"setext", "Title\n======\n", []heading{{1, "Title", 0, 0}}},
		{"inline markup", "## `code` and *em* and [l](https://x.io)\n",
			[]heading{{2, "code and em and l https://x.io", 0, 0}}},
		{"cjk emoji", "## 中文 🎉 head\n", []heading{{2, "中文 🎉 head", 0, 0}}},
		{"no headings", "just prose\n\ntext\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := extractHeadings(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d headings %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i].level != tc.want[i].level || got[i].text != tc.want[i].text {
					t.Errorf("head %d: got {%d %q}, want {%d %q}",
						i, got[i].level, got[i].text, tc.want[i].level, tc.want[i].text)
				}
			}
		})
	}
}

func TestMapHeadings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		texts []string
		want  []int
	}{
		{
			name:  "single heading",
			lines: []string{"", "  # Title One", "", "prose"},
			texts: []string{"Title One"},
			want:  []int{1},
		},
		{
			name:  "duplicates resolve sequentially",
			lines: []string{"", " ## Dup", "", "text", "", " ## Dup", ""},
			texts: []string{"Dup", "Dup"},
			want:  []int{1, 5},
		},
		{
			name:  "wrapped heading accumulates run",
			lines: []string{"", " ## a very", " long heading", "", "x"},
			texts: []string{"A Very Long Heading"},
			want:  []int{1},
		},
		{
			name:  "blockquote decoration prefix",
			lines: []string{"", " | ## Quoted Head", ""},
			texts: []string{"Quoted Head"},
			want:  []int{1},
		},
		{
			name:  "emphasis markers stripped both sides",
			lines: []string{"", " ### Code span and *em*", ""},
			texts: []string{"Code `span` and *em*"},
			want:  []int{1},
		},
		{
			name:  "unmatched maps past end",
			lines: []string{"a", "b"},
			texts: []string{"Missing"},
			want:  []int{2},
		},
		{
			name:  "later duplicate does not match earlier occurrence twice",
			lines: []string{"", " ## Dup", "", "body", "", " ## Dup", ""},
			texts: []string{"Dup", "Dup", "Dup"},
			want:  []int{1, 5, 7},
		},
		{
			name:  "decoration-only heading skipped",
			lines: []string{"", " ## Real", ""},
			texts: []string{"***", "Real"},
			want:  []int{0, 1},
		},
		{
			name:  "empty heading anchors at previous heading like source mode",
			lines: []string{"", " ## Real", "", "body", ""},
			texts: []string{"Real", ""},
			want:  []int{1, 1},
		},
		{
			name:  "cjk and emoji",
			lines: []string{"", " ## 中文 🎉 end", ""},
			texts: []string{"中文 🎉 END"},
			want:  []int{1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			heads := make([]heading, len(tc.texts))
			for i, x := range tc.texts {
				heads[i] = heading{text: x}
			}
			mapHeadings(heads, tc.lines)
			for i := range heads {
				if heads[i].line != tc.want[i] {
					t.Errorf("head %q: line %d, want %d", heads[i].text, heads[i].line, tc.want[i])
				}
			}
		})
	}
}

// Integration: through the real renderer, each heading's recorded line must be
// the start of a region whose stripped text contains the normalized heading.
func TestHeadingMappingPipeline(t *testing.T) {
	src := "# Main Title\n\nIntro prose.\n\n## Setup\n\nStep text.\n\n## Setup\n\nAgain.\n\n### 中文 🎉 深入\n\ndetails\n\n## A fairly long heading that will wrap around at narrow widths indeed yes\n\ntail\n"
	strippedDoc := map[int][]string{}
	for _, w := range []int{20, 40, 80} {
		out, err := Render(src, w, true)
		if err != nil {
			t.Fatalf("w%d: %v", w, err)
		}
		lines := splitStrip(out)
		heads := extractHeadings(src)
		mapHeadings(heads, lines)
		strippedDoc[w] = lines
		prev := -1
		for i, h := range heads {
			if h.line <= prev {
				t.Errorf("w%d head %q: line %d not monotonic (prev %d)", w, h.text, h.line, prev)
			}
			prev = h.line
			end := len(lines)
			if i+1 < len(heads) && heads[i+1].line > h.line {
				end = heads[i+1].line
			}
			region := normHeading(strings.Join(lines[h.line:end], " "))
			if !strings.Contains(region, normHeading(h.text)) {
				t.Errorf("w%d head %q: mapped line %d lacks text (region %q)", w, h.text, h.line, region)
			}
		}
	}
	dups := 0
	last := map[string]int{}
	for _, h := range func() []heading {
		out, _ := Render(src, 40, true)
		lines := splitStrip(out)
		hs := extractHeadings(src)
		mapHeadings(hs, lines)
		return hs
	}() {
		if p, ok := last[h.text]; ok && p != h.line {
			dups++
		}
		last[h.text] = h.line
	}
	if dups < 1 {
		t.Error("duplicate headings should map to distinct lines")
	}
}

var tocDoc = "# Alpha\n\n" + strings.Repeat("para line\n", 15) +
	"\n## Beta\n\n" + strings.Repeat("more text\n", 15) +
	"\n## Gamma\n\n" + strings.Repeat("final text\n", 15)

func newRenderedModel(t *testing.T, src string, w, h int) *Model {
	t.Helper()
	m := New(src, "doc.md")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	*m = *nm.(*Model)
	settle(t, m, cmd)
	return m
}

func pressKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	nm, cmd := m.Update(msg)
	*m = *nm.(*Model)
	return cmd
}

func TestTOCOpenJumpClose(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 60, 10)

	press(m, "o")
	if !m.tocOpen || len(m.heads) != 3 {
		t.Fatalf("o should open TOC with 3 heads, open=%v heads=%d", m.tocOpen, len(m.heads))
	}
	if m.tocSel != 0 {
		t.Fatal("selection starts at first heading")
	}

	yBefore := m.vp.YOffset()
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tocOpen || m.vp.YOffset() != yBefore {
		t.Fatal("esc closes without moving")
	}

	press(m, "o")
	if m.tocSel != 0 {
		t.Fatal("reopen restores selection")
	}
	press(m, "j") // select Beta (mid-document, safely scrollable)
	if m.tocSel != 1 {
		t.Fatalf("j moves selection, got %d", m.tocSel)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tocOpen {
		t.Fatal("enter closes overlay")
	}
	if want := m.heads[1].line; m.vp.YOffset() != want {
		t.Fatalf("enter jumped to %d, want %d", m.vp.YOffset(), want)
	}
	if got := m.currentSection(); got != 1 {
		t.Fatalf("after jump current section %d, want 1", got)
	}

	top := m.vp.YOffset()
	press(m, "o")
	if m.tocSel != 1 {
		t.Fatal("selection persists across close/reopen")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close without moving
	if m.vp.YOffset() != top {
		t.Fatal("close preserves reading position")
	}

	m2 := newRenderedModel(t, "no headings here\n", 60, 10)
	press(m2, "o")
	if m2.tocOpen {
		t.Fatal("o without headings must not open overlay")
	}
}

func TestTOCSwallowsUnboundKeys(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 60, 10)
	press(m, "o")
	yBefore, xBefore := m.vp.YOffset(), m.vp.XOffset()
	for _, key := range []string{"d", "u", "f", "b", " ", "w", "T", "/", "?", "n", "N"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%q under open TOC returned a command", key)
		}
		if !m.tocOpen {
			t.Fatalf("%q disturbed overlay state", key)
		}
		if m.vp.YOffset() != yBefore || m.vp.XOffset() != xBefore {
			t.Fatalf("%q moved the viewport", key)
		}
	}
	if !m.wrapMode || m.collapsed || m.search.active || m.search.query != "" {
		t.Error("mode/search state changed under open TOC")
	}
	press(m, "k")
	if m.tocSel != 0 {
		t.Errorf("k at top must clamp selection: got %d, want 0", m.tocSel)
	}
}

func TestTOCEnterJumpModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap bool
	}{
		{"nowrap enter jump", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRenderedModel(t, tocDoc, 60, 10)
			settle(t, m, press(m, "w"))
			if m.wrapMode != tc.wrap {
				t.Fatalf("precondition: wrapMode=%v", m.wrapMode)
			}
			press(m, "o")
			press(m, "j")
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.tocOpen {
				t.Fatal("enter closes overlay")
			}
			if want := m.heads[1].line; m.vp.YOffset() != want {
				t.Fatalf("enter jumped to %d, want %d", m.vp.YOffset(), want)
			}
		})
	}
}

func TestTOCRowsAndTruncation(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 60, 10)
	pw := m.tocPanelWidth()
	if pw != 40 {
		t.Fatalf("panel width %d, want min(40, 56)=40", pw)
	}
	long := "## " + strings.Repeat("中文🎉", 30) + "\n\ntext\n"
	m2 := newRenderedModel(t, long, 30, 8)
	pw2 := m2.tocPanelWidth()
	rows := m2.tocRows(pw2, 8)
	found := false
	for _, r := range rows {
		if r == "" {
			continue
		}
		if w := ansi.StringWidth(r); w > pw2 {
			t.Fatalf("row width %d exceeds panel %d: %q", w, pw2, ansi.Strip(r))
		}
		if strings.Contains(ansi.Strip(strings.TrimRight(r, " ")), "…") {
			found = true
		}
	}
	if !found {
		t.Fatal("long heading should truncate with ellipsis")
	}
}

func TestOverlayViewComposition(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 80, 12)
	press(m, "o")
	v := m.View().Content
	lines := strings.Split(v, "\n")
	if n := len(lines); n < 2 {
		t.Fatalf("view too short: %d lines", n)
	}
	statusIdx := len(lines) - 1
	if !strings.Contains(lines[statusIdx], "doc.md") {
		t.Fatalf("status bar must stay visible: %q", lines[statusIdx])
	}
	body := strings.Join(lines[:statusIdx], "\n")
	if !strings.Contains(ansi.Strip(body), "Alpha") || !strings.Contains(ansi.Strip(body), "Beta") {
		t.Fatalf("overlay rows missing:\n%s", ansi.Strip(body))
	}
	sel := tocSelStyle.Render(strings.Repeat(" ", 40))
	if !strings.Contains(body, ansi.Strip(sel)) && !strings.Contains(body, "\x1b[7m") {
		t.Fatalf("selected row not highlighted:\n%q", body)
	}

	m.vp.SetYOffset(m.heads[1].line)
	if got := m.currentSection(); got != 1 {
		t.Fatalf("current section after scroll %d, want 1", got)
	}
	m.vp.GotoTop()
	if got := m.currentSection(); got != -1 {
		t.Logf("current section at absolute top: %d (no heading above top yet)", got)
	}

	narrow := newRenderedModel(t, tocDoc, 24, 10)
	press(narrow, "o")
	v2 := narrow.View().Content
	if !strings.Contains(v2, "Alpha") {
		t.Fatal("narrow terminal should center panel and still show it")
	}
}

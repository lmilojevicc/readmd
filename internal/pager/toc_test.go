package pager

import (
	"fmt"
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
		{"nested levels", "# A\n\n## B\n\n### C\n", []heading{{level: 1, text: "A"}, {level: 2, text: "B"}, {level: 3, text: "C"}}},
		{"duplicates", "## Dup\n\ntext\n\n## Dup\n", []heading{{level: 2, text: "Dup"}, {level: 2, text: "Dup"}}},
		{"all levels", "##### E\n###### F\n", []heading{{level: 5, text: "E"}, {level: 6, text: "F"}}},
		{"setext", "Title\n======\n", []heading{{level: 1, text: "Title"}}},
		{"inline markup and linked title", "## `code` and *em* and [l](https://x.io)\n",
			[]heading{{level: 2, text: "code and em and l"}}},
		{"cjk emoji", "## 中文 🎉 head\n", []heading{{level: 2, text: "中文 🎉 head"}}},
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
			name:  "multiline heading accumulates run",
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
			name:  "empty heading anchors at previous heading using source heading anchors",
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
	src := "# Main Title\n\nIntro prose.\n\n## Setup\n\nStep text.\n\n## Setup\n\nAgain.\n\n### 中文 🎉 深入\n\ndetails\n\n## A fairly long heading that remains intact at narrow widths indeed yes\n\ntail\n"
	strippedDoc := map[int][]string{}
	for _, w := range []int{20, 40, 80} {
		out, err := Render(src, w)
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
		out, _ := Render(src, 40)
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

var tocDoc = "# Alpha\n\n" + strings.Repeat("para line\n\n", 15) +
	"## Beta\n\n" + strings.Repeat("more text\n\n", 15) +
	"## Gamma\n\n" + strings.Repeat("final text\n\n", 15)

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

func TestTOCExperimentalPreviewCommitAndCancel(t *testing.T) {
	for _, closer := range []string{"esc", "q", "o"} {
		t.Run("cancel "+closer, func(t *testing.T) {
			m := newRenderedModel(t, tocDoc, 80, 12)
			m.vp.SetYOffset(m.heads[1].line)
			m.vp.SetXOffset(7)
			x, y := m.vp.XOffset(), m.vp.YOffset()
			press(m, "o")
			if !m.tocOpen || m.tocSel != 1 || m.tocPreview {
				t.Fatalf("open state open=%v sel=%d preview=%v", m.tocOpen, m.tocSel, m.tocPreview)
			}
			press(m, "j")
			if m.tocSel != 2 || !m.tocPreview || !strings.Contains(ansi.Strip(m.tocPreviewBody()), "Gamma") {
				t.Fatalf("selection did not preview Gamma: sel=%d preview=%v body=%q", m.tocSel, m.tocPreview, ansi.Strip(m.tocPreviewBody()))
			}
			if m.vp.XOffset() != x || m.vp.YOffset() != y {
				t.Fatal("experimental preview mutated the real viewport")
			}
			pressKey(m, keyMsg(closer))
			if m.tocOpen || m.vp.XOffset() != x || m.vp.YOffset() != y {
				t.Fatalf("%s did not cancel at exact x/y", closer)
			}
		})
	}

	m := newRenderedModel(t, tocDoc, 80, 12)
	m.vp.SetXOffset(9)
	press(m, "o")
	press(m, "j")
	want := m.heads[1].line
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tocOpen || m.vp.YOffset() != want || m.vp.XOffset() != 0 {
		t.Fatalf("Enter did not commit preview: open=%v x/y=%d/%d want y=%d", m.tocOpen, m.vp.XOffset(), m.vp.YOffset(), want)
	}
}

func TestTOCFilterAndEscLadder(t *testing.T) {
	src := "# Alpha\n\n## [Install](https://example.com/install)\n\n### 中文配置\n\n## Usage\n"
	m := newRenderedModel(t, src, 80, 14)
	press(m, "o")
	press(m, "/")
	for _, key := range []string{"q", "o"} {
		press(m, key)
	}
	if !m.tocPrompt || m.tocFilter != "qo" || !m.tocOpen {
		t.Fatalf("q/o must be literal prompt input: prompt=%v filter=%q open=%v", m.tocPrompt, m.tocFilter, m.tocOpen)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	typeQuery(m, "配置")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.tocFilter != "配" {
		t.Fatalf("Backspace must remove one Unicode rune: %q", m.tocFilter)
	}
	typeQuery(m, "置")
	if m.tocSel != 2 || !m.tocPreview || len(m.filteredHeadings()) != 1 {
		t.Fatalf("Unicode filter fallback sel=%d preview=%v heads=%v", m.tocSel, m.tocPreview, m.filteredHeadings())
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tocPrompt || m.tocFilter != "配置" {
		t.Fatal("Enter must retain the filter and leave the prompt")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.tocOpen || m.tocFilter != "" {
		t.Fatal("first Esc clears retained filter")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tocOpen {
		t.Fatal("second Esc closes outline")
	}

	press(m, "o")
	press(m, "G")
	gamma := m.tocSel
	press(m, "/")
	typeQuery(m, "missing")
	if m.tocSel != gamma || !strings.Contains(ansi.Strip(strings.Join(m.tocRows(), "\n")), "No matching headings") {
		t.Fatal("zero-match filter must preserve the underlying selection and show an empty state")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // leave prompt, keep zero results
	press(m, "j")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.tocOpen || m.tocSel != gamma {
		t.Fatal("movement and Enter must be no-ops with zero matches")
	}
	press(m, "/")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tocFilter != "" || m.tocPrompt || !m.tocOpen || m.tocSel != gamma {
		t.Fatal("prompt Esc must restore the prior selection and retain outline")
	}
	wantGamma := min(m.heads[gamma].line, max(0, len(m.stripped)-m.vp.Height()))
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tocOpen || m.vp.YOffset() != wantGamma {
		t.Fatal("Enter after clearing a zero-result filter must commit the prior heading")
	}

	preserve := newRenderedModel(t, src, 80, 14)
	preserve.vp.SetYOffset(preserve.heads[1].line)
	press(preserve, "o")
	preserve.setTOCFilter("install")
	if preserve.tocSel != 1 {
		t.Fatalf("filter must preserve a still-matching selection: %d", preserve.tocSel)
	}
	preserve.setTOCFilter("example.com")
	if len(preserve.filteredHeadings()) != 0 {
		t.Fatal("title-only filter must not match a linked heading URL")
	}
}

func TestTOCFilterPromptTreatsGKeysAsText(t *testing.T) {
	m := newRenderedModel(t, "# Alpha\n\n## gG Selected\n\n## Omega\n", 80, 14)
	press(m, "o")
	m.tocSel = 1
	m.tocPreview = false
	press(m, "/")
	selected, preview := m.tocSel, m.tocPreview

	for _, tc := range []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: 'g', Text: "g"}, "g"},
		{tea.KeyPressMsg{Code: 'G', Text: "G"}, "gG"},
	} {
		pressKey(m, tc.key)
		if m.tocFilter != tc.want {
			t.Fatalf("filter=%q, want literal %q", m.tocFilter, tc.want)
		}
		if m.tocSel != selected || m.tocPreview != preview {
			t.Fatalf("g/G prompt input moved selection or preview: sel=%d preview=%v, want %d/%v",
				m.tocSel, m.tocPreview, selected, preview)
		}
	}
}

func TestTOCMovementSourcesUsePreviewHelper(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start int
		key   tea.KeyPressMsg
		want  int
	}{
		{"j", 0, keyMsg("j"), 1},
		{"down", 0, tea.KeyPressMsg{Code: tea.KeyDown}, 1},
		{"k", 2, keyMsg("k"), 1},
		{"up", 2, tea.KeyPressMsg{Code: tea.KeyUp}, 1},
		{"g", 2, keyMsg("g"), 0},
		{"G", 0, keyMsg("G"), 2},
		{"home", 2, tea.KeyPressMsg{Code: tea.KeyHome}, 0},
		{"end", 0, tea.KeyPressMsg{Code: tea.KeyEnd}, 2},
		{"page down", 0, tea.KeyPressMsg{Code: tea.KeyPgDown}, 2},
		{"page up", 2, tea.KeyPressMsg{Code: tea.KeyPgUp}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRenderedModel(t, tocDoc, 60, 10)
			m.vp.SetYOffset(m.heads[tc.start].line)
			x, y := m.vp.XOffset(), m.vp.YOffset()
			press(m, "o")
			pressKey(m, tc.key)
			if m.tocSel != tc.want || !m.tocPreview {
				t.Fatalf("sel=%d preview=%v want=%d", m.tocSel, m.tocPreview, tc.want)
			}
			if m.vp.XOffset() != x || m.vp.YOffset() != y {
				t.Fatal("movement mutated the real viewport")
			}
		})
	}
}

func TestTOCHelpStyleGeometryAndTitles(t *testing.T) {
	src := "## Root\n\n" + strings.Repeat("body\n\n", 8) +
		"#### [Linked title](https://example.com/private)\n\n" + strings.Repeat("more\n\n", 8) +
		"### " + strings.Repeat("中文🎉", 30) + "\n"
	m := newRenderedModel(t, src, 60, 14)
	m.vp.SetYOffset(m.heads[0].line)
	press(m, "o")
	before := m.tocRows()
	plain := ansi.Strip(strings.Join(before, "\n"))
	for _, want := range []string{"╭─ Outline", "Root", "Linked title", "of 3", "/ filter", "enter jump"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("outline modal lacks %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "example.com") {
		t.Fatal("linked-heading URL leaked into title-only outline label")
	}
	if !strings.Contains(strings.Join(before, "\n"), "\x1b[36m") || !strings.Contains(strings.Join(before, "\n"), "\x1b[7m") {
		t.Fatal("outline must use palette-cyan border and selected-row highlight")
	}
	m.moveTOC(1)
	styledRows := strings.Join(m.tocRows(), "\n")
	if !strings.Contains(styledRows, "\x1b[1m") {
		t.Fatalf("the original current heading must remain visually distinct during preview: %q", styledRows)
	}
	if !strings.Contains(plain, "    Linked title") {
		t.Fatalf("heading indentation must be relative to shallowest level:\n%s", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Fatal("long Unicode title must truncate by display width")
	}
	for _, row := range before {
		if ansi.StringWidth(row) != ansi.StringWidth(before[0]) {
			t.Fatalf("row width %d differs from panel %d: %q", ansi.StringWidth(row), ansi.StringWidth(before[0]), ansi.Strip(row))
		}
		if ansi.StringWidth(row) > m.width {
			t.Fatalf("outline row exceeds terminal: %q", ansi.Strip(row))
		}
	}
	press(m, "/")
	typeQuery(m, "Root")
	after := m.tocRows()
	if len(after) != len(before) || ansi.StringWidth(after[0]) != ansi.StringWidth(before[0]) {
		t.Fatal("filtering resized the stable outline modal")
	}
}

func TestTOCPreviewExposureAtRepresentativeHeights(t *testing.T) {
	var src strings.Builder
	for i := range 30 {
		fmt.Fprintf(&src, "## Section %02d\n\nbody %02d\n\n", i, i)
	}
	for _, tc := range []struct {
		height      int
		wantVisible int
	}{
		{16, 5},
		{24, 13},
		{40, 20},
	} {
		t.Run(fmt.Sprintf("height %d", tc.height), func(t *testing.T) {
			m := newRenderedModel(t, src.String(), 80, tc.height)
			press(m, "o")
			rows := m.tocRows()
			if got := len(rows) - 2; got != tc.wantVisible {
				t.Fatalf("visible rows=%d, want %d", got, tc.wantVisible)
			}
			bodyRows := m.vp.Height()
			top := (bodyRows - len(rows)) / 2
			bottom := bodyRows - top - len(rows)
			if top < 4 || bottom < 4 {
				t.Fatalf("preview exposure top/bottom=%d/%d, want at least 4 (body=%d panel=%d)", top, bottom, bodyRows, len(rows))
			}
		})
	}
}

func TestTOCPreviewCommitAndCancelAcrossReaderModes(t *testing.T) {
	doc := "# Alpha\n\n" + strings.Repeat("x", 200) + "\n\n" +
		strings.Repeat("alpha body\n\n", 6) + "## Beta\n\n" +
		strings.Repeat("beta body\n\n", 6) + "## Gamma\n\nfinal highlighted body\n"
	for _, mode := range []string{"regular", "reader"} {
		for _, action := range []string{"cancel", "commit"} {
			t.Run(mode+" "+action, func(t *testing.T) {
				m := newRenderedModel(t, doc, 140, 16)
				switch mode {
				case "reader":
					settle(t, m, press(m, "r"))
				}
				m.vp.SetYOffset(m.heads[1].line)
				m.vp.SetXOffset(7)
				m.search.query = "final"
				m.refreshSearch()
				x, y := m.vp.XOffset(), m.vp.YOffset()
				press(m, "o")
				press(m, "G")
				preview := m.tocPreviewBody()
				if !strings.Contains(ansi.Strip(preview), "Gamma") || !strings.Contains(preview, curHL) {
					t.Fatalf("%s preview must show Gamma with real search-highlight ANSI: %q", mode, preview)
				}
				if m.vp.XOffset() != x || m.vp.YOffset() != y || m.search.query != "final" {
					t.Fatalf("%s preview mutated real state", mode)
				}
				wantY := min(m.heads[m.tocSel].line, max(0, len(m.stripped)-m.vp.Height()))
				if action == "cancel" {
					pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
					if m.tocOpen || m.vp.XOffset() != x || m.vp.YOffset() != y {
						t.Fatalf("%s cancel x/y=%d/%d, want %d/%d", mode, m.vp.XOffset(), m.vp.YOffset(), x, y)
					}
					return
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.tocOpen || m.vp.XOffset() != 0 || m.vp.YOffset() != wantY {
					t.Fatalf("%s commit x/y=%d/%d, want 0/%d", mode, m.vp.XOffset(), m.vp.YOffset(), wantY)
				}
			})
		}
	}
}

func TestTOCCommitDuringResizeAnchorsToFreshHeadingLine(t *testing.T) {
	m := newRenderedModel(t, tocDoc, 80, 16)
	press(m, "o")
	press(m, "G")
	selected := m.tocSel

	nm, resize := m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	*m = *nm.(*Model)
	if resize == nil || !m.rendering || !m.tocOpen {
		t.Fatalf("fitting resize must render behind the open outline: cmd=%v rendering=%v open=%v", resize, m.rendering, m.tocOpen)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tocOpen || m.anchor == nil {
		t.Fatalf("commit during resize must close and install a heading anchor: open=%v anchor=%#v", m.tocOpen, m.anchor)
	}

	msg := resize().(renderedMsg)
	at := msg.heads[selected].line
	const shift = 4
	for i := selected; i < len(msg.heads); i++ {
		msg.heads[i].line += shift
	}
	content := strings.Split(msg.content, "\n")
	blanks := make([]string, shift)
	content = append(content[:at], append(blanks, content[at:]...)...)
	msg.content = strings.Join(content, "\n")
	msg.stripped = append(msg.stripped[:at], append(blanks, msg.stripped[at:]...)...)
	freshLine := msg.heads[selected].line

	nm, _ = m.Update(msg)
	*m = *nm.(*Model)
	if m.vp.YOffset() != freshLine || m.anchor != nil {
		t.Fatalf("completed resize landed at %d with anchor %#v, want fresh heading line %d", m.vp.YOffset(), m.anchor, freshLine)
	}
}

func TestTOCRefusesTinyResizeAndReloadInvalidates(t *testing.T) {
	fit := newRenderedModel(t, tocDoc, 60, 12)
	press(fit, "o")
	nm, resize := fit.Update(tea.WindowSizeMsg{Width: 50, Height: 10})
	*fit = *nm.(*Model)
	settle(t, fit, resize)
	if !fit.tocOpen {
		t.Fatal("outline must survive a resize while its modal still fits")
	}

	for _, size := range []tea.WindowSizeMsg{{Width: 4, Height: 10}, {Width: 40, Height: 5}} {
		m := newRenderedModel(t, tocDoc, 60, 12)
		press(m, "o")
		nm, _ := m.Update(size)
		*m = *nm.(*Model)
		if m.tocOpen {
			t.Fatalf("outline remained open after too-small resize: %+v", size)
		}
	}
	m := newRenderedModel(t, tocDoc, 60, 12)
	press(m, "o")
	nm, cmd := m.Update(reloadDoneMsg{body: []byte("# Fresh\n")})
	*m = *nm.(*Model)
	if cmd == nil || m.tocOpen {
		t.Fatalf("accepted reload must synchronously invalidate outline: cmd=%v open=%v", cmd, m.tocOpen)
	}
}

func TestTOCUnavailableDoesNotTrapInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		w, h int
	}{
		{"no headings", "plain text\n", 60, 10},
		{"too narrow", tocDoc, 4, 10},
		{"too short", tocDoc, 40, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRenderedModel(t, tc.doc, tc.w, tc.h)
			press(m, "o")
			if m.tocOpen {
				t.Fatal("outline opened without usable geometry/content")
			}
		})
	}
}

func TestHeadingReviewExactRows(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		width     int
		rows      []int
	}{
		{"space collision", "AB\n\n# A B\n", 120, []int{3}},
		{"word boundary collision", "# Root\n\nAB C\n\n## A BC\n", 120, []int{1, 5}},
		{"linked entity destination", "## [Title](https://example.org/?a=1&amp;b=2)\n\n## Next\n", 120, []int{1, 3}},
		{"visible entities", "## A &amp; B &lt; C\n\n## Next\n", 120, []int{1, 3}},
		{"wrapped words", "## A long heading with enough words to wrap over several rows\n\n## Next\n", 20, []int{1, 7}},
		{"wrapped unbroken token", "## " + strings.Repeat("abcdefgh", 8) + "\n\n## Next\n", 20, []int{1, 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Render(tc.src, tc.width)
			if err != nil {
				t.Fatal(err)
			}
			lines := splitStrip(out)
			heads := extractHeadings(tc.src)
			mapHeadings(heads, lines)
			for i, row := range tc.rows {
				if heads[i].line != row {
					t.Errorf("heading %d row=%d want=%d; render=%q", i, heads[i].line, row, lines)
				}
			}
		})
	}
}

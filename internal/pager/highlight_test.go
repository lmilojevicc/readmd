package pager

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

func spansOf(line, q string) []span {
	ms := findMatches([]string{ansi.Strip(line)}, q)
	sps := make([]span, len(ms))
	for i, m := range ms {
		sps[i] = span{m.start, m.end}
	}
	return sps
}

func hlOf(line, q string) string {
	return highlightLine(line, spansOf(line, q), -1)
}

func TestHighlightGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		q    string
		cur  bool
		want string
	}{
		{
			name: "match inside bold span restores active style",
			line: "plain \x1b[1mbold\x1b[0m tail",
			q:    "bold",
			want: "plain " + matchHL + "bold\x1b[m\x1b[1m\x1b[0m tail",
		},
		{
			name: "glamour styling continues after match",
			line: "x \x1b[32mgreen tail\x1b[0m y",
			q:    "tail",
			want: "x \x1b[32mgreen " + matchHL + "tail\x1b[m\x1b[32m\x1b[0m y",
		},
		{
			name: "sgr at match start folds into restore, hidden under highlight",
			line: "one \x1b[31mtwo\x1b[0m three",
			q:    "two th",
			want: "one " + matchHL + "two th\x1b[m\x1b[31mree",
		},
		{
			name: "interior sgr suppressed, pre-match style replayed at end",
			line: "one \x1b[31mtwo\x1b[0m three",
			q:    "two three",
			want: "one " + matchHL + "two three\x1b[m\x1b[31m",
		},
		{
			name: "multiple matches one line",
			line: "aa bb aa",
			q:    "aa",
			want: matchHL + "aa\x1b[m bb " + matchHL + "aa\x1b[m",
		},
		{
			name: "current match gets distinct palette bg",
			line: "aa bb aa",
			q:    "aa",
			cur:  true,
			want: matchHL + "aa\x1b[m bb " + curHL + "aa\x1b[m",
		},
		{
			name: "match at line start",
			line: "abc def",
			q:    "abc",
			want: matchHL + "abc\x1b[m def",
		},
		{
			name: "match at line end",
			line: "def abc",
			q:    "abc",
			want: "def " + matchHL + "abc\x1b[m",
		},
		{
			name: "whole line match",
			line: "word",
			q:    "word",
			want: matchHL + "word\x1b[m",
		},
		{
			name: "cjk around match uses columns not bytes",
			line: "中文test中文",
			q:    "test",
			want: "中文" + matchHL + "test\x1b[m中文",
		},
		{
			name: "cjk inside match stays cluster aligned",
			line: "中文tail中文",
			q:    "文t",
			want: "中" + matchHL + "文t\x1b[m" + "ail中文",
		},
		{
			name: "wide query chars",
			line: "xa中文",
			q:    "中文",
			want: "xa" + matchHL + "中文\x1b[m",
		},
		{
			name: "case insensitive",
			line: "Alpha ALPHA",
			q:    "aLpHa",
			want: matchHL + "Alpha\x1b[m " + matchHL + "ALPHA\x1b[m",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cur := -1
			if tc.cur {
				cur = 1
			}
			if got := highlightLine(tc.line, spansOf(tc.line, tc.q), cur); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestHighlightEmptyQueryYieldsNothing(t *testing.T) {
	for _, line := range []string{"plain", "\x1b[1mstyled\x1b[0m", "中文 emoji 🎉"} {
		if got := hlOf(line, ""); got != line {
			t.Errorf("empty query must not touch %q, got %q", line, got)
		}
	}
}

var hlSGRRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// countSGRWith counts SGR sequences carrying a palette code (43 = match bg,
// 45 = current-match bg). The cell buffer re-encodes SGRs, so View output is
// asserted by params, not raw bytes.
func countSGRWith(s, code string) int {
	n := 0
	for _, m := range hlSGRRe.FindAllStringSubmatch(s, -1) {
		for _, p := range strings.Split(m[1], ";") {
			if p == code {
				n++
				break
			}
		}
	}
	return n
}

var hlDoc = "# Title\n\nalpha one\n\n**bold** alpha two\n" + strings.Repeat("\nfiller line\n", 15)

func TestSearchHighlightsRenderedView(t *testing.T) {
	m := newRenderedModel(t, hlDoc, 60, 12)

	press(m, "/")
	typeQuery(m, "alpha")
	if v := bodyOf(m); countSGRWith(v, "43") == 0 || countSGRWith(v, "45") == 0 {
		t.Fatal("highlight applies incrementally while typing")
	}

	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	body := bodyOf(m)
	if n := strings.Count(ansi.Strip(body), "alpha"); n != 2 {
		t.Fatalf("precondition: visible alpha occurrences=%d", n)
	}
	if c := countSGRWith(body, "43") + countSGRWith(body, "45"); c != 2 {
		t.Fatalf("all matches highlighted: %d SGRs, want 2", c)
	}
	if c := countSGRWith(body, "45"); c != 1 {
		t.Fatalf("exactly one current match: %d SGRs, want 1", c)
	}

	curText := func() string {
		for _, l := range strings.Split(bodyOf(m), "\n") {
			if countSGRWith(l, "45") > 0 {
				return ansi.Strip(l)
			}
		}
		return ""
	}
	first := curText()
	if first == "" {
		t.Fatal("current match must be highlighted after commit")
	}
	press(m, "n")
	if cur := curText(); cur == "" || cur == first {
		t.Fatalf("n must move the current highlight to the other match, got %q", cur)
	}
	press(m, "N")
	if back := curText(); back != first {
		t.Fatalf("N must restore previous current highlight: %q, want %q", back, first)
	}
}

func TestSearchPromptTrueMatchCount(t *testing.T) {
	m := newRenderedModel(t, "ab ab ab\n\ntail\n", 60, 10)
	press(m, "/")
	typeQuery(m, "ab")
	if v := m.View().Content; !strings.Contains(v, "[3 matches]") {
		t.Fatalf("prompt must show true occurrence count on one line:\n%s", v)
	}
	if m.search.count != 3 {
		t.Fatalf("count=%d, want 3", m.search.count)
	}
}

// hlPresent scopes highlight SGR counting to the document body: the status
// bar's brand chip legitimately carries a 45 background.
func hlPresent(m *Model) bool {
	body := bodyOf(m)
	return countSGRWith(body, "43") > 0 && countSGRWith(body, "45") > 0
}

func TestSearchHighlightLifecycle(t *testing.T) {
	m := newRenderedModel(t, hlDoc, 60, 12)

	press(m, "/")
	typeQuery(m, "alpha")
	if !hlPresent(m) {
		t.Fatal("highlight applies incrementally while typing")
	}
	for range len("alpha") {
		pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if v := bodyOf(m); countSGRWith(v, "43") > 0 || countSGRWith(v, "45") > 0 {
		t.Fatal("empty query removes all highlights")
	}

	typeQuery(m, "alpha")
	if !hlPresent(m) {
		t.Fatal("re-typing re-applies highlights")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if hlPresent(m) || m.search.active || m.search.query != "" {
		t.Fatal("cancel clears the query and its highlights")
	}
	settle(t, m, nil)
	press(m, "n")
	if hlPresent(m) {
		t.Fatal("n after clearing must not resurrect highlights")
	}
	gen := m.gen
	cmd := press(m, "r")
	if cmd == nil || m.gen <= gen {
		t.Fatal("rerender must schedule a render and advance generation")
	}
	settle(t, m, cmd)
	if hlPresent(m) {
		t.Fatal("re-render on reader toggle must not resurrect highlights")
	}

	press(m, "/")
	typeQuery(m, "alpha")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !hlPresent(m) {
		t.Fatal("precondition: highlights active for resize checks")
	}

	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	*m = *nm.(*Model)
	settle(t, m, cmd)
	if !hlPresent(m) {
		t.Fatal("resize re-render keeps highlights fresh")
	}

	press(m, "/")
	typeQuery(m, "alphax")
	if v := bodyOf(m); countSGRWith(v, "43") > 0 || countSGRWith(v, "45") > 0 {
		t.Fatal("query change drops stale ranges immediately")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !hlPresent(m) {
		t.Fatal("backspace re-derives highlights")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
}

func TestSearchHighlightsSourceView(t *testing.T) {
	m := newRenderedModel(t, srcDoc, 60, 12)
	press(m, "s")
	press(m, "/")
	typeQuery(m, "beta")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	body := bodyOf(m)
	if countSGRWith(body, "43") == 0 {
		t.Fatal("source view highlights matches")
	}
	for _, m := range hlSGRRe.FindAllStringSubmatch(body, -1) {
		for _, p := range strings.Split(m[1], ";") {
			switch p {
			case "", "0", "30", "43", "45":
			default:
				t.Fatalf("source view must stay plain outside highlights, found SGR param %q:\n%q", p, body)
			}
		}
	}
	if m.search.count != 13 {
		t.Fatalf("search must count raw occurrences: %d, want 13", m.search.count)
	}

	m.path = "doc.md"
	m.readFile = func(string) ([]byte, error) { return []byte("# Replaced\n\nbeta fresh\n"), nil }
	settle(t, m, press(m, "R"))
	body = bodyOf(m)
	if countSGRWith(body, "43")+countSGRWith(body, "45") == 0 ||
		!strings.Contains(ansi.Strip(body), "beta fresh") {
		t.Fatal("reload in source view re-derives highlights on new content")
	}
	if strings.Contains(ansi.Strip(body), "gamma tail") {
		t.Fatal("reload must replace old content")
	}
}

func TestHighlightSurvivesHorizontalSlice(t *testing.T) {
	doc := strings.Repeat("x", 70) + " needle " + strings.Repeat("y", 70) + "\n"
	m := newRenderedModel(t, doc, 40, 10)
	press(m, "s")
	press(m, "/")
	typeQuery(m, "needle")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	const start, end = 71, 77
	for _, off := range []int{0, 34, 40, 66} {
		m.vp.SetXOffset(off)
		line := strings.Split(m.vp.View(), "\n")[0]
		full := off <= start && end <= off+40
		if full && countSGRWith(line, "43")+countSGRWith(line, "45") == 0 {
			t.Fatalf("offset %d: highlight lost in %q", off, ansi.Strip(line))
		}
		if got := ansi.StringWidth(line); got > 40 {
			t.Fatalf("offset %d: sliced width %d exceeds viewport (%q)", off, got, line)
		}
		if plain := ansi.Strip(line); !strings.Contains(doc, strings.TrimRight(plain, " ")) &&
			!strings.HasPrefix(doc[off:], plain) {
			t.Fatalf("offset %d: corrupted slice text %q", off, plain)
		}
		for _, sm := range hlSGRRe.FindAllStringSubmatch(line, -1) {
			for _, p := range strings.Split(sm[1], ";") {
				switch p {
				case "", "0", "30", "43", "45":
				default:
					t.Fatalf("offset %d: stray sequence SGR(%s): %q", off, sm[1], line)
				}
			}
		}
	}
}

func TestCutPreservesHighlightSGRs(t *testing.T) {
	styled := "aaaaaaaaaa\x1b[1mbbbbbbbbbb" + matchHL + "NNNNNN\x1b[1mcccccc"
	cut := ansi.Cut(styled, 15, 28)
	if !strings.Contains(cut, matchHL) || !strings.Contains(cut, "\x1b[1m") {
		t.Fatalf("Cut dropped SGRs: %q", cut)
	}
	if s := ansi.Strip(cut); !strings.HasPrefix(s, "bbbbbNNNNNN") {
		t.Fatalf("Cut text %q, want prefix bbbbbNNNNNN", s)
	}
}

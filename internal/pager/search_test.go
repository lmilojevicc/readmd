package pager

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFindMatches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		q     string
		want  []int
	}{
		{"empty query", []string{"abc"}, "", nil},
		{"case insensitive", []string{"Hello World", "world peace"}, "WORLD", []int{0, 1}},
		{"multiple per line", []string{"ab ab ab"}, "ab", []int{0, 0, 0}},
		{"cjk", []string{"中文测试 middle 中文"}, "中文", []int{0, 0}},
		{"emoji untouched", []string{"a 🎉 b 🎉"}, "🎉", []int{0, 0}},
		{"no match", []string{"abc", "def"}, "zzz", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := findMatches(tc.lines, tc.q)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

var searchDoc = "# Top\n\nalpha beta\n\ngamma ALPHA delta\n\n" +
	strings.Repeat("filler\n", 20) + "\nfinal alpha tail\n"

func typeQuery(m *Model, q string) {
	for _, r := range q {
		pressKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestSearchFlow(t *testing.T) {
	m := newRenderedModel(t, searchDoc, 60, 10)

	press(m, "/")
	if !m.search.active || !m.search.fwd {
		t.Fatal("/ opens forward search")
	}
	typeQuery(m, "alpha")
	if m.search.query != "alpha" || m.search.count != 3 {
		t.Fatalf("live state: query=%q count=%d want alpha/3", m.search.query, m.search.count)
	}

	v := m.View().Content
	if !strings.Contains(v, "/alpha") || !strings.Contains(v, "[3 matches]") {
		t.Fatalf("prompt with live count missing:\n%s", v)
	}
	lines := strings.Split(v, "\n")
	if strings.Index(lines[len(lines)-2], "/alpha") == -1 ||
		!strings.Contains(lines[len(lines)-1], "doc.md") {
		t.Fatal("prompt must sit directly above status bar")
	}

	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.search.query != "alph" || m.search.count != 3 {
		t.Fatalf("backspace: query=%q count=%d", m.search.query, m.search.count)
	}
	typeQuery(m, "a")

	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.search.active {
		t.Fatal("enter commits search")
	}
	wantFirst := findMatches(m.stripped, "alpha")[0]
	if m.vp.YOffset() != wantFirst {
		t.Fatalf("enter jumps to first match: at %d, want %d", m.vp.YOffset(), wantFirst)
	}

	press(m, "n")
	if m.vp.YOffset() <= wantFirst {
		t.Fatal("n moves to next match below")
	}
	press(m, "N")
	if m.vp.YOffset() != wantFirst {
		t.Fatalf("N returns to previous match: at %d, want %d", m.vp.YOffset(), wantFirst)
	}

	m.vp.SetYOffset(findMatches(m.stripped, "alpha")[2])
	press(m, "n")
	if m.vp.YOffset() <= wantFirst {
		t.Fatal("wrap case covered in TestSearchWrapAround")
	}

	gamma := findMatches(m.stripped, "gamma")
	m.vp.GotoTop()
	press(m, "?")
	if !m.search.active || m.search.fwd {
		t.Fatal("? opens backward search")
	}
	typeQuery(m, "gamma")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.vp.YOffset() != gamma[len(gamma)-1] {
		t.Fatalf("backward search from top wraps to last gamma: at %d, want %d",
			m.vp.YOffset(), gamma[len(gamma)-1])
	}

	m3 := newRenderedModel(t, searchDoc, 60, 10)
	press(m3, "/")
	typeQuery(m3, "alpha")
	pressKey(m3, tea.KeyPressMsg{Code: tea.KeyEnter})
	y1 := m3.vp.YOffset()
	press(m3, "/")
	typeQuery(m3, "nomatch-at-all")
	pressKey(m3, tea.KeyPressMsg{Code: tea.KeyEscape}) // cancel, keep old matches
	press(m3, "n")
	if m3.vp.YOffset() <= y1 {
		t.Fatal("after cancel, n uses last committed query")
	}
}

var wrapDoc = func() string {
	var b strings.Builder
	b.WriteString("# Head\n\nalpha here\n\n")
	for i := range 40 {
		fmt.Fprintf(&b, "pad %d\n\n", i)
	}
	b.WriteString("alpha middle\n\n")
	for i := range 30 {
		fmt.Fprintf(&b, "tail %d\n\n", i)
	}
	return b.String()
}()

func TestSearchWrapAround(t *testing.T) {
	m := newRenderedModel(t, wrapDoc, 60, 10)
	press(m, "/")
	typeQuery(m, "alpha")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	ms := m.search.matches
	if len(ms) != 2 {
		t.Fatalf("precondition matches %v", ms)
	}
	wantFirst := ms[0]
	last := ms[len(ms)-1]

	m.vp.SetYOffset(last)
	if m.vp.YOffset() != last {
		t.Fatalf("precondition: cannot scroll to last match (%d < %d)", m.vp.YOffset(), last)
	}
	press(m, "n")
	if m.vp.YOffset() != wantFirst {
		t.Fatalf("n past last wraps to first: %d, want %d", m.vp.YOffset(), wantFirst)
	}

	m.vp.GotoTop()
	press(m, "N")
	if m.vp.YOffset() != last {
		t.Fatalf("N before first wraps to last: %d, want %d", m.vp.YOffset(), last)
	}
}

func TestSearchSwallowsKeysWhileTyping(t *testing.T) {
	m := newRenderedModel(t, searchDoc, 60, 10)
	press(m, "/")
	wrapBefore := m.wrapMode
	collapsedBefore := m.collapsed
	press(m, "w")
	press(m, "T")
	press(m, "j")
	if m.wrapMode != wrapBefore || m.collapsed != collapsedBefore {
		t.Fatal("search input must capture mode-toggling keys")
	}
	if m.search.query != "wTj" {
		t.Fatalf("printables append to query, got %q", m.search.query)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search.active {
		t.Fatal("esc cancels input")
	}
	if !m.wrapMode || m.collapsed {
		t.Fatal("modes unchanged after cancel")
	}
}

func TestSearchNoMatchesAndEmptyQuery(t *testing.T) {
	m := newRenderedModel(t, searchDoc, 60, 10)
	press(m, "/")
	typeQuery(m, "zzz")
	if m.search.count != 0 {
		t.Fatal("precondition: no matches")
	}
	if v := m.View().Content; !strings.Contains(v, "[no matches]") {
		t.Fatalf("zero-match prompt:\n%s", v)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	top := m.vp.YOffset()
	press(m, "n")
	press(m, "N")
	if m.vp.YOffset() != top {
		t.Fatal("n/N without matches must not move viewport")
	}
	press(m, "/")
	typeQuery(m, "ab")
	for range 2 {
		pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.search.query != "" || m.search.count != 0 {
		t.Fatal("backspace empties query and count")
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, "n")
	if m.vp.YOffset() != top {
		t.Fatal("empty committed query must not move viewport")
	}
}

func TestSearchRecomputesAfterRerender(t *testing.T) {
	m := newRenderedModel(t, searchDoc, 60, 10)
	press(m, "/")
	typeQuery(m, "alpha")
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	n := len(m.search.matches)
	if n != 3 {
		t.Fatalf("committed matches %d, want 3", n)
	}
	settle(t, m, press(m, "T")) // re-render from source
	if len(m.search.matches) != n || m.search.query != "alpha" {
		t.Fatalf("re-render must refresh matches: %d vs %d", len(m.search.matches), n)
	}
}

func TestCtrlCQuitsEverywhere(t *testing.T) {
	docs := []*Model{
		newRenderedModel(t, tocDoc, 60, 10),
		newRenderedModel(t, tocDoc, 60, 10),
	}
	press(docs[0], "t")
	press(docs[1], "/")
	typeQuery(docs[1], "x")
	for i, m := range docs {
		nm, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		_ = nm
		if cmd == nil {
			t.Errorf("context %d: ctrl+c must quit", i)
		}
	}
}

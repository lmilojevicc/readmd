package pager

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

type searchState struct {
	active  bool
	query   string
	fwd     bool
	matches []int
	pos     int
	count   int
}

func findMatches(lines []string, q string) []int {
	if q == "" {
		return nil
	}
	lq := strings.ToLower(q)
	var out []int
	for i, l := range lines {
		ls := strings.ToLower(l)
		for off := 0; ; {
			k := strings.Index(ls[off:], lq)
			if k < 0 {
				break
			}
			out = append(out, i)
			off += k + len(lq)
		}
	}
	return out
}

func (m *Model) openSearch(fwd bool) {
	m.search.active = true
	m.search.fwd = fwd
	m.search.query = ""
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.search.active = false
		m.commitSearch()
	case "esc":
		m.search.active = false
	case "backspace", "ctrl+h":
		if r := []rune(m.search.query); len(r) > 0 {
			m.search.query = string(r[:len(r)-1])
			m.refreshSearch()
		}
	default:
		if kp, ok := msg.(tea.KeyPressMsg); ok {
			if rs := []rune(kp.Text); len(rs) > 0 && printable(rs) {
				m.search.query += string(rs)
				m.refreshSearch()
			}
		}
	}
	return nil
}

func printable(rs []rune) bool {
	for _, r := range rs {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func (m *Model) commitSearch() {
	m.refreshSearch()
	if len(m.search.matches) > 0 {
		m.jumpMatch(m.search.dir(), true)
	}
}

func (m *Model) refreshSearch() {
	if m.search.query == "" {
		m.search.matches = nil
		m.search.count = 0
		return
	}
	ms := findMatches(m.stripped, m.search.query)
	m.search.count = len(ms)
	if !m.search.active {
		m.search.matches = ms
		if m.search.pos >= len(ms) {
			m.search.pos = 0
		}
	}
}

func (s *searchState) dir() int {
	if s.fwd {
		return 1
	}
	return -1
}

func (m *Model) jumpMatch(dir int, inclusive bool) {
	ms := m.search.matches
	if len(ms) == 0 {
		return
	}
	top := m.vp.YOffset()
	eqPass := (dir > 0) != inclusive
	lo, hi := 0, len(ms)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if ms[mid] < top || (ms[mid] == top && eqPass) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := 0
	if dir < 0 {
		i = lo - 1
		if i < 0 {
			i = len(ms) - 1
		}
	} else {
		i = lo
		if i >= len(ms) {
			i = 0
		}
	}
	m.search.pos = i
	m.vp.SetYOffset(ms[i])
}

func (m *Model) searchPrompt() string {
	mark := "/"
	if !m.search.fwd {
		mark = "?"
	}
	s := mark + m.search.query
	if m.search.query != "" {
		n := m.search.count
		if n == 0 {
			s += "  [no matches]"
		} else {
			s += fmt.Sprintf("  [%d match%s]", n, plural(n))
		}
	}
	return ansi.Truncate(s, m.width, "…")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

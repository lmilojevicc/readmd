package pager

import (
	"fmt"
	"math"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

type match struct {
	line       int
	start, end int
}

type searchState struct {
	active  bool
	query   string
	matches []match
	pos     int
	count   int
}

// findMatches locates every case-insensitive occurrence of q across the
// visible text of lines, reporting display-column ranges aligned to grapheme
// cluster boundaries (so wide runes never split).
func findMatches(lines []string, q string) []match {
	qr := lowerRunes([]rune(q))
	if len(qr) == 0 {
		return nil
	}
	var out []match
	for i, l := range lines {
		low, cs, ce := visibleRunes(l)
		for off := 0; off+len(qr) <= len(low); off++ {
			if !runesAt(low, off, qr) {
				continue
			}
			out = append(out, match{i, cs[off], ce[off+len(qr)-1]})
			off += len(qr) - 1
		}
	}
	return out
}

func lowerRunes(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = unicode.ToLower(r)
	}
	return out

}

// visibleRunes flattens a line into lowered runes paired with each rune's
// cluster start/end display column.
func visibleRunes(line string) (low []rune, cs, ce []int) {
	col, st := 0, byte(0)
	for rest := line; rest != ""; {
		seq, w, n, ns := ansi.DecodeSequence(rest, st, nil)
		st = ns
		rest = rest[n:]
		if w == 0 {
			if !zeroWidthText(seq) {
				continue
			}
			for _, r := range seq {
				low = append(low, unicode.ToLower(r))
				cs = append(cs, col)
				ce = append(ce, col)
			}
			continue
		}
		for _, r := range seq {
			low = append(low, unicode.ToLower(r))
			cs = append(cs, col)
			ce = append(ce, col+w)
		}
		col += w
	}
	return low, cs, ce
}

// zeroWidthText reports whether a width-0 decoded sequence is printable text
// (combining marks) rather than an escape or control sequence.
func zeroWidthText(seq string) bool {
	if seq == "" || seq[0] == '\x1b' || seq[0] == '\x9b' {
		return false
	}
	for _, r := range seq {
		if r < ' ' || r == 0x7f || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func runesAt(low []rune, off int, qr []rune) bool {
	for i, r := range qr {
		if low[off+i] != r {
			return false
		}
	}
	return true
}

func (m *Model) openSearch() {
	m.search.active = true
	m.search.query = ""
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.search.active = false
		m.commitSearch()
	case "esc":
		m.search.active = false
		m.search.query = ""
		m.refreshSearch()
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
		m.jumpMatch(1, true)
	}
}

func (m *Model) refreshSearch() {
	m.search.matches = findMatches(m.stripped, m.search.query)
	m.search.count = len(m.search.matches)
	if m.search.pos >= len(m.search.matches) {
		m.search.pos = 0
	}
	m.applySearchView()
}

func (m *Model) jumpMatch(dir int, inclusive bool) {
	ms := m.search.matches
	if len(ms) == 0 {
		return
	}
	top := m.vp.YOffset()
	col := math.MaxInt
	if p := m.search.pos; p >= 0 && p < len(ms) && ms[p].line == top {
		col = ms[p].start
	}
	before := func(mt match) bool {
		if mt.line != top {
			return mt.line < top
		}
		if dir > 0 {
			return !inclusive && mt.start <= col
		}
		return mt.start < col
	}
	lo, hi := 0, len(ms)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if before(ms[mid]) {
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
	m.vp.SetYOffset(ms[i].line)
	m.applySearchView()
}

func (m *Model) searchPrompt() string {
	s := "/" + m.search.query
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

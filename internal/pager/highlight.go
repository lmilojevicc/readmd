package pager

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	matchHL = "\x1b[43m\x1b[30m" // palette yellow bg, black fg
	curHL   = "\x1b[45m\x1b[30m" // palette magenta bg, black fg
)

type span struct{ start, end int }

// applySearchView loads the viewport with the cached styled lines overlayed
// with search highlights. Base lines are never modified; the highlight is a
// post-render display transform.
func (m *Model) applySearchView() {
	if m.base == nil {
		return
	}
	type lineSpans struct {
		sps []span
		cur int
	}
	groups := map[int]*lineSpans{}
	for i, mt := range m.search.matches {
		g := groups[mt.line]
		if g == nil {
			g = &lineSpans{cur: -1}
			groups[mt.line] = g
		}
		g.sps = append(g.sps, span{mt.start, mt.end})
		if i == m.search.pos {
			g.cur = len(g.sps) - 1
		}
	}
	targets := m.targetSpans()
	var b strings.Builder
	for i, l := range m.base {
		if g := groups[i]; g != nil {
			l = highlightLine(l, g.sps, g.cur)
		}
		if sps := targets[i]; len(sps) > 0 {
			l = highlightTargetLine(l, sps)
		}
		b.WriteString(l)
		if i < len(m.base)-1 {
			b.WriteByte('\n')
		}
	}
	m.vp.SetContent(b.String())
}

// highlightLine inserts highlight SGRs for each span into a styled line. At a
// match end the style active before the match is replayed so glamour styling
// continues correctly; sequences surfacing inside a match are hidden so the
// highlight paints uniformly.
func highlightLine(line string, sps []span, cur int) string {
	return highlightLineStyle(line, sps, cur, matchHL, curHL)
}

func highlightLineStyle(line string, sps []span, cur int, normal, current string) string {
	var b strings.Builder
	var active []string
	st := byte(0)
	col, si, snap, in := 0, 0, "", false
	open := func() {
		snap = strings.Join(active, "")
		if si == cur {
			b.WriteString(current)
		} else {
			b.WriteString(normal)
		}
		in = true
	}
	closeSpan := func() {
		// Explicit reset before replaying the snapshot: without it the match
		// background bleeds into following cells until the next SGR or EOL.
		b.WriteString("\x1b[m")
		b.WriteString(snap)
		in, si = false, si+1
	}
	for rest := line; rest != ""; {
		seq, w, n, ns := ansi.DecodeSequence(rest, st, nil)
		st = ns
		rest = rest[n:]
		sgr := w == 0 && isSGR(seq)
		if in && col >= sps[si].end {
			closeSpan()
		}
		atStart := !in && si < len(sps) && col >= sps[si].start
		switch {
		case atStart && sgr:
			active = pushSGR(active, seq)
			open()
			continue
		case atStart:
			open()
		case in && sgr:
			continue
		}
		if sgr {
			active = pushSGR(active, seq)
		}
		b.WriteString(seq)
		col += w
	}
	if in {
		closeSpan()
	}
	return b.String()
}

// Focus attributes compose with stock link styling and existing search colors.
func highlightTargetLine(line string, spans []span) string {
	var b strings.Builder
	var active []string
	col, index, state, inside := 0, 0, byte(0), false
	for rest := line; rest != ""; {
		seq, width, n, next := ansi.DecodeSequence(rest, state, nil)
		if n <= 0 {
			break
		}
		state, rest = next, rest[n:]
		if inside && col >= spans[index].end {
			b.WriteString("\x1b[m" + strings.Join(active, ""))
			inside = false
			index++
		}
		if isSGR(seq) {
			active = pushSGR(active, seq)
			b.WriteString(seq)
			if inside {
				b.WriteString(targetHL)
			}
			continue
		}
		if !inside && index < len(spans) && col >= spans[index].start && width > 0 {
			b.WriteString(targetHL)
			inside = true
		}
		b.WriteString(seq)
		col += width
	}
	if inside {
		b.WriteString("\x1b[m" + strings.Join(active, ""))
	}
	return b.String()
}

func isSGR(seq string) bool {
	return strings.HasSuffix(seq, "m") &&
		(strings.HasPrefix(seq, "\x1b[") || strings.HasPrefix(seq, "\x9b"))
}

// pushSGR folds an SGR sequence into the active-style accumulator; a reset
// clears it. The accumulator is what gets replayed after a highlight.
func pushSGR(active []string, seq string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(seq, "\x1b["), "\x9b"), "m")
	reset, set := false, false
	for _, part := range strings.Split(body, ";") {
		v, _, _ := strings.Cut(part, ":")
		if n, err := strconv.Atoi(v); err != nil || n == 0 {
			reset = true
			continue
		}
		set = true
	}
	switch {
	case set:
		if reset {
			active = active[:0]
		}
		return append(active, seq)
	default:
		return active[:0]
	}
}

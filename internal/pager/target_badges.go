package pager

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const badgeStyle = "\x1b[1;35m"

type targetBadge struct {
	targetRegion
	target hintTarget
}

type targetPlan struct {
	badges     []targetBadge
	hits       []targetBadge
	rows       []string
	surfaceTop int
}

type badgeCell struct {
	start, end int
	text       string
}

// Placement only inspects visible display cells. It never changes document
// geometry: ambiguous boundaries and short/clipped tokens go to the shelf.
func (m *Model) placeTargetBadges() {
	st := &m.targets
	cellsByLine := map[int][]badgeCell{}
	place := func(height int) ([]targetBadge, []hintTarget) {
		var badges []targetBadge
		var fallback []hintTarget
		for _, target := range st.targets {
			if badge, ok := m.placeTargetBadge(target, height, badges, cellsByLine); ok {
				badges = append(badges, badge)
			} else {
				fallback = append(fallback, target)
			}
		}
		return badges, fallback
	}
	st.vimium.badges, st.vimium.fallback = place(st.savedHeight)
	if len(st.vimium.fallback) > 0 {
		st.vimium.fallbackRows = min(4, st.savedHeight-1)
		st.vimium.badges, st.vimium.fallback = place(st.savedHeight - st.vimium.fallbackRows)
	}
}

func (m *Model) placeTargetBadge(target hintTarget, height int, placed []targetBadge, cellsByLine map[int][]badgeCell) (targetBadge, bool) {
	width := len(target.label) + 2
	top := m.targets.savedY
	// Prefer adjacent whitespace in any fragment before covering own text.
	for _, adjacent := range []bool{true, false} {
		for _, reg := range target.regions {
			if reg.line < top || reg.line >= top+height || reg.line >= len(m.base) {
				continue
			}
			left := m.targets.rows[reg.line-m.targets.savedY].left
			right := left + m.vp.Width()
			cells, ok := cellsByLine[reg.line]
			if !ok {
				cells = badgeCells(m.base[reg.line], right)
				cellsByLine[reg.line] = cells
			}
			starts := []int{reg.end, reg.start - width}
			if !adjacent {
				starts = nil
				if reg.end-reg.start <= width {
					continue
				}
				for _, cell := range cells {
					if cell.start >= max(left, reg.start) && cell.end <= min(right, reg.end) {
						starts = append(starts, cell.start)
					}
				}
			}
			for _, start := range starts {
				end := start + width
				if start < left || end > right || (!adjacent && end > reg.end) || !safeBadgeCells(cells, start, end, adjacent) {
					continue
				}
				collision := false
				for _, badge := range placed {
					if badge.line == reg.line && start < badge.end && end > badge.start {
						collision = true
					}
				}
				if adjacent {
					for _, other := range m.targets.targets {
						for _, r := range other.regions {
							if r.line == reg.line && start < r.end && end > r.start {
								collision = true
							}
						}
					}
				}
				if !collision {
					return targetBadge{targetRegion{reg.line, start, end}, target}, true
				}
			}
		}
	}
	return targetBadge{}, false
}

func badgeCells(line string, right int) []badgeCell {
	var cells []badgeCell
	col := 0
	for rest := ansi.Strip(line); rest != "" && col < right; {
		cluster, width := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		rest = rest[len(cluster):]
		cells = append(cells, badgeCell{col, col + width, cluster})
		col += width
	}
	for col < right {
		cells = append(cells, badgeCell{col, col + 1, " "})
		col++
	}
	return cells
}

func safeBadgeCells(cells []badgeCell, start, end int, whitespace bool) bool {
	col := start
	for _, cell := range cells {
		if cell.end <= start || cell.start >= end {
			continue
		}
		if cell.start != col || cell.end > end || (whitespace && cell.text != " ") {
			return false
		}
		for _, r := range cell.text {
			if r >= '\u2500' && r <= '\u257f' {
				return false
			}
		}
		col = cell.end
	}
	return col == end
}

func (m *Model) filteredFallback() []hintTarget {
	var out []hintTarget
	for _, target := range m.targets.vimium.fallback {
		if strings.HasPrefix(target.label, strings.ToUpper(m.targets.prefix)) {
			out = append(out, target)
		}
	}
	return out
}

func (m *Model) moveTargetFallback(delta int) {
	if n := len(m.filteredFallback()); n > 0 {
		m.targets.vimium.focus = (m.targets.vimium.focus + delta + n) % n
	}
}

// View and mouse consume this same final screen-coordinate plan. The shelf
// owns its entire surface, including blank rows and its non-interactive header.
func (m *Model) targetPlan() targetPlan {
	st := m.targets
	plan := targetPlan{surfaceTop: st.savedHeight}
	margin := 0
	if on, _ := m.readerFrame(); on {
		_, margin = m.readerGeom(true)
	}
	for _, badge := range st.vimium.badges {
		if !strings.HasPrefix(badge.target.label, strings.ToUpper(st.prefix)) {
			continue
		}
		badge.line -= st.savedY
		left := st.rows[badge.line].left
		badge.start += margin - left
		badge.end += margin - left
		plan.badges = append(plan.badges, badge)
		plan.hits = append(plan.hits, badge)
	}
	if st.vimium.fallbackRows == 0 {
		return plan
	}
	plan.surfaceTop = st.savedHeight - st.vimium.fallbackRows
	fallback := m.filteredFallback()
	capacity := st.vimium.fallbackRows - 1
	focus := min(st.vimium.focus, max(0, len(fallback)-1))
	page, pages := focus/capacity, max(1, (len(fallback)+capacity-1)/capacity)
	plan.rows = append(plan.rows, fmt.Sprintf("more %d · page %d/%d", len(fallback), page+1, pages))
	for row := range capacity {
		index := page*capacity + row
		text := " "
		if index < len(fallback) {
			target := fallback[index]
			marker := "  "
			if index == focus {
				marker = "> "
			}
			origin := target.regions[0]
			label := fmt.Sprintf("%s[%s] L%d:C%d ", marker, target.label, origin.line+1, origin.start+1)
			description := hintDescription(target)
			if target.kind != targetFootnote {
				var context []string
				for _, reg := range target.regions {
					context = append(context, ansi.Strip(ansi.Cut(m.base[reg.line], reg.start, reg.end)))
				}
				description = strings.Join(strings.Fields(strings.Join(context, " ")), " ") + " · " + description
			}
			text = label + ansi.Truncate(description, max(0, m.width-len(label)), "…")
			if index == focus {
				text = badgeStyle + text + "\x1b[m"
			}
			plan.hits = append(plan.hits, targetBadge{targetRegion{plan.surfaceTop + row + 1, 0, m.width}, target})
		}
		plan.rows = append(plan.rows, text)
	}
	for i, row := range plan.rows {
		row = ansi.Truncate(row, m.width, "…")
		plan.rows[i] = "\x1b[m\x1b]8;;\x1b\\" + row + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(row)))
	}
	return plan
}

func (m *Model) targetFooter() string {
	prefix := strings.ToUpper(m.targets.prefix)
	if prefix == "" {
		prefix = "_"
	}
	text := fmt.Sprintf("%s · %d targets · Esc cancel", prefix, len(m.targetCandidates()))
	if len(m.targets.vimium.fallback) > 0 {
		text += " · Tab/Shift+Tab focus · Enter open"
	}
	if ansi.StringWidth(text) > m.width {
		text = fmt.Sprintf("%s %d Esc", prefix, len(m.targetCandidates()))
		if len(m.targets.vimium.fallback) > 0 {
			text += " Tab Enter"
		}
	}
	return ansi.Truncate(text, m.width, "…")
}

func (m *Model) applyTargetPlan(body string, plan targetPlan) string {
	lines := strings.Split(body, "\n")
	for _, badge := range plan.badges {
		if badge.line >= 0 && badge.line < len(lines) {
			lines[badge.line] = paintTargetBadge(lines[badge.line], badge.start, badge.end, "["+badge.target.label+"]")
		}
	}
	body = strings.Join(lines, "\n")
	if len(plan.rows) > 0 {
		body = m.overlay(body, 0, plan.surfaceTop, m.width, plan.rows)
	}
	return body
}

// Keep original control sequences in order, replacing only whole display
// graphemes. Replaying the active SGRs contains the transient badge accent.
func paintTargetBadge(line string, start, end int, label string) string {
	line += strings.Repeat(" ", max(0, end-ansi.StringWidth(line)))
	var out strings.Builder
	var active []string
	col, state, covered := 0, byte(0), false
	for rest := line; rest != ""; {
		seq, width, n, next := ansi.DecodeSequence(rest, state, nil)
		if n <= 0 {
			break
		}
		rest, state = rest[n:], next
		if width == 0 {
			if covered && ansi.Strip(seq) != "" {
				continue
			}
			if isSGR(seq) {
				active = pushSGR(active, seq)
			}
			out.WriteString(seq)
		} else {
			covered = col >= start && col < end
			if col == start {
				out.WriteString("\x1b[m" + badgeStyle + label + "\x1b[m" + strings.Join(active, ""))
			}
			if col < start || col >= end {
				out.WriteString(seq)
			}
		}
		col += width
	}
	return out.String()
}

func underlineTargets(line string, spans []span) string {
	var out strings.Builder
	var active []string
	col, state, inside := 0, byte(0), false
	for rest := line; rest != ""; {
		seq, width, n, next := ansi.DecodeSequence(rest, state, nil)
		if n <= 0 {
			break
		}
		rest, state = rest[n:], next
		wanted := false
		for _, span := range spans {
			if col >= span.start && col < span.end {
				wanted = true
				break
			}
		}
		if inside && !wanted {
			out.WriteString("\x1b[m" + strings.Join(active, ""))
		}
		if wanted && !inside {
			out.WriteString("\x1b[4m")
		}
		inside = wanted
		out.WriteString(seq)
		if width == 0 && isSGR(seq) {
			active = pushSGR(active, seq)
			if inside {
				out.WriteString("\x1b[4m")
			}
		}
		col += width
	}
	if inside {
		out.WriteString("\x1b[m" + strings.Join(active, ""))
	}
	return out.String()
}

func hintDescription(target hintTarget) string {
	if target.kind == targetFootnote {
		return "footnote:" + target.footnote
	}
	return targetDescription(target.dest)
}

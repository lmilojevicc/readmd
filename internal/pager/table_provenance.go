package pager

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const tableCellPrefix = "readmd-cell-"
const tableCellEnd = tableCellPrefix + "end"

func tableCellID(table, row, col int) string {
	return fmt.Sprintf("%s%d-%d-%d", tableCellPrefix, table, row, col)
}

// Cell identity is structural, so preprocessing that shifts source offsets does
// not shift it. Mapping still requires exact occurrence counts within each cell.
func sourceTableCells(src string) map[string][]sourceRange {
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	cells := map[string][]sourceRange{}
	id := 0
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		if _, ok := node.(*extast.Table); !ok {
			continue
		}
		rowID := 0
		for row := node.FirstChild(); row != nil; row = row.NextSibling() {
			col := 0
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells[tableCellID(id, rowID, col)] = appendSegments(nil, cell.Lines())
				col++
			}
			rowID++
		}
		id++
	}
	return cells
}

// These OSC 8 sequences have no destination and are never actionable. Real
// inline links do not change cell identity. Lipgloss may replay an end reset
// before the next line's start; an end without an active cell is ignored.
func renderedTableCells(lines []string) map[string][]targetRegion {
	cells := map[string][]targetRegion{}
	for line, content := range lines {
		id, start, col, state := "", 0, 0, byte(0)
		for rest := content; rest != ""; {
			seq, w, n, next := ansi.DecodeSequence(rest, state, nil)
			rest, state = rest[n:], next
			if params, dest, ok := parseOSC8(seq); ok && dest == "" {
				marker := osc8ID(params)
				if marker == tableCellEnd {
					if id != "" && col > start {
						cells[id] = append(cells[id], targetRegion{line, start, col})
					}
					id = ""
				} else if strings.HasPrefix(marker, tableCellPrefix) {
					id, start = marker, col
				}
			}
			col += w
		}
	}
	return cells
}

func footnoteRegionCell(regions []targetRegion, cells map[string][]targetRegion) string {
	for id, bounds := range cells {
		contained := 0
		for _, reg := range regions {
			for _, bound := range bounds {
				if reg.line == bound.line && reg.start >= bound.start && reg.end <= bound.end {
					contained++
					break
				}
			}
		}
		if contained == len(regions) {
			return id
		}
	}
	return ""
}

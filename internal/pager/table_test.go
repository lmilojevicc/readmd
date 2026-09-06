package pager

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func TestTablePublicAPIProof(t *testing.T) {
	for _, cols := range []int{7} {
		t.Run(fmt.Sprint(cols), func(t *testing.T) {
			prose := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu"
			src := "| A | B | C | D | E | F | G |\n| - | - | - | - | - | - | - |\n| " + prose + " | x | **" + prose + "** | y | `atomic-endpoint.internal:8443` | z | [" + prose + "](https://example.org/full-target) |\n"
			node := md.Parser().Parse(text.NewReader([]byte(src))).FirstChild().(*extast.Table)
			got, err := renderTable(node, []byte(src), glamansi.Options{Styles: paletteConfig}, 0)
			if err != nil {
				t.Fatal(err)
			}
			grid := strings.Split(strings.TrimSpace(ansi.Strip(got)), "\n")
			for _, col := range []int{0, 2, 6} {
				var words []string
				for _, line := range grid[2:] {
					cells := strings.Split(line, "│")
					if len(cells) != 7 {
						t.Fatalf("bad grid: %q", line)
					}
					words = append(words, strings.Fields(cells[col])...)
				}
				if strings.Join(words, " ") != prose {
					t.Fatalf("column %d lost words: %q", col, words)
				}
			}
			widths, _, reconstructed := tableGrid(t, got, 1)
			for _, col := range []int{1, 3, 5} {
				if widths[col] != 3 || reconstructed[0][col] != []string{"x", "y", "z"}[(col-1)/2] {
					t.Fatalf("tiny column %d: width=%d value=%q", col, widths[col], reconstructed[0][col])
				}
			}
			if reconstructed[0][4] != "atomic-endpoint.internal:8443" {
				t.Fatalf("atomic=%q", reconstructed[0][4])
			}
			if strings.Contains(ansi.Strip(got), "https://example.org") {
				t.Fatal("visible repeated destination")
			}
			regions := rawLinkRegions(strings.Split(got, "\n"))
			if len(regions) < 3 {
				t.Fatalf("wrapped link regions=%#v", regions)
			}
			for _, reg := range regions {
				if reg.dest != "https://example.org/full-target" {
					t.Fatalf("dest=%s", reg.dest)
				}
			}
			baseline := stockNatural(t, src, paletteStyleName)
			t.Logf("stock %dx%d; adapter %dx%d; reconstructed columns 0/2/6 exactly (%d words each)", widestLine(splitStrip(baseline)), len(splitStrip(baseline)), widestLine(splitStrip(got)), len(splitStrip(got)), len(strings.Fields(prose)))
			t.Log("\n" + ansi.Strip(got))
		})
	}
}

// Decode by separator display columns, not literal pipes inside cells. Each
// logical row/cell is reconstructed independently, including empty cells.
func tableGrid(t *testing.T, out string, rowCount int) ([]int, []string, [][]string) {
	t.Helper()
	lines := strings.Split(strings.Trim(ansi.Strip(out), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("missing grid: %q", out)
	}
	rule := lines[1]
	sep := "┼"
	if !strings.Contains(rule, "─") {
		sep = "|"
	}
	widths := []int{}
	for i, part := range strings.Split(rule, sep) {
		width := ansi.StringWidth(part)
		if i == 0 {
			width -= 2
		}
		widths = append(widths, width)
	}
	cells := func(line string) []string {
		result := make([]string, len(widths))
		x := 2
		for i, width := range widths {
			result[i] = strings.TrimSpace(ansi.Cut(line, x, x+width))
			x += width + 1
		}
		return result
	}
	headers := cells(lines[0])
	multiline := false
	for _, line := range lines[2:] {
		multiline = multiline || line == rule
	}
	var rows [][]string
	current := make([]string, len(widths))
	for _, line := range lines[2:] {
		if line == rule {
			rows = append(rows, current)
			current = make([]string, len(widths))
			continue
		}
		if !multiline && rowCount > 1 {
			rows = append(rows, cells(line))
			continue
		}
		for col, value := range cells(line) {
			if value != "" {
				current[col] += " " + value
			}
		}
	}
	if multiline || rowCount == 1 {
		rows = append(rows, current)
	}
	for _, row := range rows {
		for i, cell := range row {
			row[i] = strings.Join(strings.Fields(cell), " ")
		}
	}
	if len(rows) != rowCount {
		t.Fatalf("rows=%d want %d: %q", len(rows), rowCount, out)
	}
	return widths, headers, rows
}

func markdownTable(headers []string, rows [][]string) string {
	delimiter := make([]string, len(headers))
	for i := range delimiter {
		delimiter[i] = "---"
	}
	src := "| " + strings.Join(headers, " | ") + " |\n| " + strings.Join(delimiter, " | ") + " |\n"
	for _, row := range rows {
		src += "| " + strings.Join(row, " | ") + " |\n"
	}
	return src
}

func TestTableCellWidthsRotatingColumns(t *testing.T) {
	for _, cols := range []int{2, 7, 14} {
		positions := []int{0, cols - 1}
		if cols > 2 {
			positions = []int{0, cols / 2, cols - 1}
		}
		for _, longCol := range positions {
			t.Run(fmt.Sprintf("%d/long%d", cols, longCol), func(t *testing.T) {
				headers := make([]string, cols)
				rows := make([][]string, 3)
				for col := range headers {
					headers[col] = "H"
				}
				for row := range rows {
					rows[row] = make([]string, cols)
					for col := range rows[row] {
						rows[row][col] = "x"
					}
					var words []string
					for word := range 33 {
						words = append(words, fmt.Sprintf("row%dword%02d", row, word))
					}
					rows[row][longCol] = strings.Join(words, " ")
				}
				src := markdownTable(headers, rows)
				baseline := stockNatural(t, src, styles.NoTTYStyle)
				first := ""
				for _, width := range []int{40, 80, 120, 0} {
					out, err := Render(src, width)
					if err != nil {
						t.Fatal(err)
					}
					widths, gotHeaders, gotRows := tableGrid(t, out, len(rows))
					for col := range headers {
						if gotHeaders[col] != headers[col] {
							t.Fatalf("header %d=%q", col, gotHeaders[col])
						}
						wantWidth := 3
						if col == longCol {
							wantWidth = 42
						}
						if widths[col] != wantWidth {
							t.Fatalf("col %d width=%d want %d", col, widths[col], wantWidth)
						}
						for row := range rows {
							if gotRows[row][col] != rows[row][col] {
								t.Fatalf("row%d col%d reconstruction=%q want %q", row, col, gotRows[row][col], rows[row][col])
							}
						}
					}
					if first != "" && out != first {
						t.Fatalf("w%d changed table geometry", width)
					}
					first = out
				}
				if widestLine(splitStrip(first)) >= widestLine(splitStrip(baseline)) || len(splitStrip(first)) <= len(splitStrip(baseline)) {
					t.Fatal("long cells did not wrap")
				}
				t.Logf("stock %dx%d -> adapter %dx%d; reconstructed all %d cells, 33 whole words per long cell, tiny columns=3 incl. padding", widestLine(splitStrip(baseline)), len(splitStrip(baseline)), widestLine(splitStrip(first)), len(splitStrip(first)), cols*len(rows))
			})
		}
	}
}

func TestTableAtomicFloorsAndFormatting(t *testing.T) {
	prose := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu"
	for _, tc := range []struct {
		name, header, markdown, want string
		floor                        int
	}{
		{"SHA", "H", strings.Repeat("abcdef012345", 8), strings.Repeat("abcdef012345", 8), 96},
		{"endpoint", "H", "events-processor-7f84c9d6b9.namespace.svc.cluster.local:15672", "events-processor-7f84c9d6b9.namespace.svc.cluster.local:15672", 0},
		{"URL", "H", "https://example.com/" + strings.Repeat("long-path-", 9), "https://example.com/" + strings.Repeat("long-path-", 9), 0},
		{"CJK", "H", strings.Repeat("東京都", 15), strings.Repeat("東京都", 15), 90},
		{"clusters", "H", strings.Repeat("👨‍👩‍👧‍👦👍🏽e\u0301", 12), strings.Repeat("👨‍👩‍👧‍👦👍🏽e\u0301", 12), 60},
		{"header floor", prose, "x", "x", 0},
		{"strong", "H", "**" + prose + "**", prose, 40},
		{"emphasis", "H", "*" + prose + "*", prose, 40},
		{"strike", "H", "~~" + prose + "~~", prose, 40},
		{"code", "H", "`" + prose + "`", prose, 40},
		{"global ref", "H", "[" + prose + "][global]", prose, 40},
		{"image", "H", "![" + prose + "][global]", "Image: " + prose, 40},
		{"escaped pipes", "H", "a \\| b and `x \\| y`", "a | b and x | y", 0},
	} {
		for _, style := range []string{styles.NoTTYStyle, paletteStyleName, styles.DarkStyle, styles.LightStyle} {
			t.Run(tc.name+"/"+style, func(t *testing.T) {
				src := markdownTable([]string{tc.header, "N"}, [][]string{{tc.markdown, "z"}}) + "\n[global]: https://global.example/full?target=preserved\n"
				out, _, _, err := renderDoc(imgCtx{}, src, 40, style)
				if err != nil {
					t.Fatal(err)
				}
				widths, headers, rows := tableGrid(t, out, 1)
				want := tc.want
				if style == styles.NoTTYStyle && (tc.name == "strong" || tc.name == "emphasis" || tc.name == "strike") {
					want = tc.markdown
				}
				if headers[0] != tc.header || rows[0][0] != want || rows[0][1] != "z" {
					t.Fatalf("reconstructed %q %q", headers, rows)
				}
				if widths[1] != 3 {
					t.Fatalf("tiny neighbor width=%d", widths[1])
				}
				if tc.floor > 0 && widths[0] != tc.floor+2 {
					t.Fatalf("width=%d want %d", widths[0], tc.floor+2)
				}
				if (tc.name == "header floor" || tc.name == "URL" || tc.name == "endpoint") && widths[0] < ansi.StringWidth(maxWidthText(tc.header, tc.want))+2 {
					t.Fatal("header/token truncated")
				}
				if tc.name == "global ref" || tc.name == "image" {
					links := parseRenderedLinkTargets(src, strings.Split(out, "\n"))
					if len(links) != 1 || len(links[0].regions) < 3 || links[0].dest != "https://global.example/full?target=preserved" {
						t.Fatalf("links=%#v", links)
					}
					if strings.Contains(ansi.Strip(out), "https://") {
						t.Fatal("table destination printed")
					}
				}
			})
		}
	}
}

func maxWidthText(a, b string) string {
	if ansi.StringWidth(a) > ansi.StringWidth(b) {
		return a
	}
	return b
}

func TestWrapTableCellStyledCuts(t *testing.T) {
	for _, tc := range []struct{ name, prefix, suffix string }{
		{"bold", "\x1b[1m", "\x1b[m"},
		{"nested truecolor", "\x1b[38;2;123;45;67m\x1b[3m", "\x1b[23m\x1b[m"},
		{"OSC8", ansi.SetHyperlink("https://example.org/full", "id=proof"), ansi.ResetHyperlink()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa"
			out := wrapTableCell(tc.prefix+original+tc.suffix, 40)
			var words []string
			for _, line := range strings.Split(out, "\n") {
				if !strings.HasPrefix(line, tc.prefix) || !strings.HasSuffix(line, tc.suffix) {
					t.Fatalf("style/link not replayed and closed: %q", line)
				}
				if ansi.StringWidth(line) > 40 {
					t.Fatalf("wide line: %q", line)
				}
				words = append(words, strings.Fields(ansi.Strip(line))...)
			}
			if strings.Join(words, " ") != original {
				t.Fatalf("lost words: %q", out)
			}
		})
	}
}

func TestTableFixtureDimensions(t *testing.T) {
	for _, fixture := range []string{"../../example.md", "../../testdata/corpus/wide-table.md"} {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			source, err := os.ReadFile(fixture)
			if os.IsNotExist(err) && fixture == "../../example.md" {
				t.Skip("optional ignored fixture absent")
			}
			if err != nil {
				t.Fatal(err)
			}
			doc := md.Parser().Parse(text.NewReader(source))
			id := 0
			for node := doc.FirstChild(); node != nil; {
				next := node.NextSibling()
				if node, ok := node.(*extast.Table); ok {
					options := glamansi.Options{Styles: *styles.DefaultStyles[styles.NoTTYStyle], TableWrap: boolPtr(false), PreserveNewLines: true}
					out, err := renderTable(node, source, options, id)
					if err != nil {
						t.Fatal(err)
					}
					rows := node.ChildCount() - 1
					tableGrid(t, out, rows)
					root := ast.NewDocument()
					root.AppendChild(root, node)
					var baseline bytes.Buffer
					stock := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(glamansi.NewRenderer(options), 1000)))
					if err := stock.Render(&baseline, source, root); err != nil {
						t.Fatal(err)
					}
					// The old adapter passed each table to this same stock public renderer
					// with WordWrap=0/TableWrap=false. Count fragment margins/URL footers too.
					t.Logf("table%d columns=%d rows=%d stock=%dx%d adapter=%dx%d (fragment incl. margins/footers)", id, len(node.Alignments), rows, widestLine(splitStrip(baseline.String())), len(splitStrip(baseline.String())), widestLine(splitStrip(out)), len(splitStrip(out)))
					id++
				}
				node = next
			}
			if id == 0 {
				t.Fatal("fixture has no top-level tables")
			}
		})
	}
}

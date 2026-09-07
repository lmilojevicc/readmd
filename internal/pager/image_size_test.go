package pager

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

func TestImageCellGeometry(t *testing.T) {
	for _, source := range []struct {
		name string
		w, h int
	}{
		{"wide", 800, 200}, {"tall", 200, 800}, {"square", 600, 600},
		{"cover", 1000, 605}, {"tiny", 3, 5}, {"extreme tall", 1, 1000},
		{"extreme wide", 1000, 1}, {"odd", 25, 45},
	} {
		for _, cells := range [][2]int{{10, 20}, {10, 24}, {14, 30}, {20, 40}, {0, 0}, {-1, 30}, {10, math.MaxInt}, {1, 1}} {
			for _, width := range []int{1, 2, 40, 80, 120, 10000} {
				for _, reader := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%dx%d/width%d/reader%v", source.name, cells[0], cells[1], width, reader), func(t *testing.T) {
						m := New("", "")
						m.width, m.reader = width, reader
						m.readerWidth = 60
						w, _ := m.readerGeom(reader)
						avail := max(1, w-2)
						cw, ch := cells[0], cells[1]
						if !validCellSize(cw, ch) {
							cw, ch = cellW, cellH
						}
						cols, rows := fitCells(source.w, source.h, avail, cells[0], cells[1])
						wantCols := min(avail, max(1, source.w/cw))
						height := float64(min(source.w, wantCols*cw)) * float64(source.h) / float64(source.w)
						if cols != wantCols || rows != int(math.Ceil(height/float64(ch))) {
							t.Fatalf("grid %dx%d: want %d columns and enough rows for %.3f pixels", cols, rows, wantCols, height)
						}
						if float64(rows*ch) < height || float64((rows-1)*ch) >= height {
							t.Fatalf("row rounding shrinks image or wastes a full row: %dx%d", cols, rows)
						}
						o := imgCtx{Enabled: true, Dir: "/local", Width: w, CellWidth: cells[0], CellHeight: cells[1], store: newImageStore()}
						ref := o.store.ensureRef(filepath.Join(o.Dir, "img.png"), &imgData{w: source.w, h: source.h})
						o.store.applyGfx([]int{ref.id}, nil)
						_, figs, _ := insertFigures("![picture](img.png)\n", o)
						if cols > maxDiacritic || rows > maxDiacritic {
							if len(figs) != 0 {
								t.Fatal("unaddressable grid must retain alt text")
							}
							return
						}
						if len(figs) != 1 || figs[0].cols != cols || figs[0].rows != rows {
							t.Fatalf("eligibility geometry differs: %+v", figs)
						}
						grid := strings.Split(renderFigure(figs[0]), "\n")
						if len(grid) != rows {
							t.Fatalf("placeholder rows = %d, want %d", len(grid), rows)
						}
						for _, line := range grid {
							if ansi.StringWidth(line) != cols || strings.Count(line, string(kitty.Placeholder)) != cols {
								t.Fatal("placeholder columns differ from reserved columns")
							}
						}
						g := gfxControls(o.store, figs)
						if len(g.tx) != 0 || g.esc != kittyPlace(ref.id, cols, rows) || len(g.places) != 1 || g.places[0] != (place{ref.id, cols, rows}) {
							t.Fatalf("placement disagrees with placeholders: %+v", g)
						}
					})
				}
			}
		}
	}
}

func TestFitCellsRejectsInvalidPixels(t *testing.T) {
	for _, dims := range [][2]int{{0, 1}, {1, -1}, {math.MaxInt, math.MaxInt}, {maxDim + 1, 1}} {
		t.Run(fmt.Sprint(dims), func(t *testing.T) {
			cols, rows := fitCells(dims[0], dims[1], math.MaxInt, math.MaxInt, math.MaxInt)
			if cols <= maxDiacritic || rows <= maxDiacritic {
				t.Fatal("invalid decoded dimensions must be ineligible")
			}
		})
	}
}

func TestDecodeImageSizeAndPayload(t *testing.T) {
	cover, err := os.ReadFile("../../assets/readme-cover.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		w, h int
	}{
		{"cover exact cap", cover, 1000, 605},
		{"wide unchanged", redPNG(t, 800, 200), 800, 200},
		{"tall cap", redPNG(t, 1718, 2838), 605, 1000},
		{"tiny axis cap", redPNG(t, 1, 4000), 1, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decodeImage(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if d.w != tc.w || d.h != tc.h || len(d.pix) != tc.w*tc.h*4 {
				t.Fatalf("decoded %dx%d/%d bytes; want %dx%d RGBA", d.w, d.h, len(d.pix), tc.w, tc.h)
			}
			tx := kittyTx(1, d.w, d.h, d.pix)
			if !strings.HasPrefix(tx, fmt.Sprintf("\x1b_Ga=t,f=32,s=%d,v=%d,", d.w, d.h)) {
				t.Fatal("transmission must use decoded pixel dimensions, not cell geometry")
			}
			var payload strings.Builder
			for _, chunk := range strings.Split(strings.TrimSuffix(tx, "\x1b\\"), "\x1b\\") {
				_, data, ok := strings.Cut(chunk, ";")
				if !ok {
					t.Fatal("missing transmission payload")
				}
				payload.WriteString(data)
			}
			decoded, err := base64.StdEncoding.DecodeString(payload.String())
			if err != nil || !bytes.Equal(decoded, d.pix) {
				t.Fatal("transmitted pixels differ from decoded pixels")
			}
		})
	}
}

func TestREADMEImageLayout(t *testing.T) {
	source, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		width, cols, rows int
	}{{40, 38, 12}, {80, 78, 24}, {120, 100, 31}} {
		t.Run(fmt.Sprint(tc.width), func(t *testing.T) {
			o := imgCtx{Enabled: true, NoRemote: true, Dir: "../..", Width: tc.width, store: newImageStore()}
			out, pending, g, err := renderDoc(o, string(source), tc.width, "notty")
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 0 || len(g.places) != 1 || g.places[0] != (place{1, tc.cols, tc.rows}) {
				t.Fatalf("README must render only the local cover with the expected geometry: %+v", g.places)
			}
			rows := 0
			for _, line := range strings.Split(out, "\n") {
				if n := strings.Count(line, string(kitty.Placeholder)); n > 0 {
					rows++
					if n != tc.cols {
						t.Fatalf("cover row has %d columns, want %d", n, tc.cols)
					}
				}
			}
			if rows != tc.rows {
				t.Fatalf("cover reserves %d rows, want %d", rows, tc.rows)
			}
		})
	}
}

func TestCellSizeQueryAndReports(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			m := New("# Heading\n", "")
			m.gfx = enabled
			query := m.Init()
			if !enabled {
				if query != nil {
					t.Fatal("disabled/unsupported graphics must not query")
				}
			} else if query == nil || query().(tea.RawMsg).Msg != "\x1b[16t" {
				t.Fatal("startup must request terminal character-cell pixels")
			}
			for _, report := range []uv.CellSizeEvent{{Width: 0, Height: 20}, {Width: -1, Height: 20}, {Width: 10, Height: 0}, {Width: 10, Height: -1}, {Width: 1001, Height: 20}, {Width: 10, Height: math.MaxInt}} {
				if _, cmd := m.Update(report); cmd != nil || m.cellWidth != 0 || m.cellHeight != 0 {
					t.Fatalf("bad report must be ignored: %+v", report)
				}
			}
			_, cmd := m.Update(uv.CellSizeEvent{Width: 14, Height: 30})
			if cmd != nil || m.gen != 0 {
				t.Fatal("report before viewport must not start render")
			}
			if enabled && (m.cellWidth != 14 || m.cellHeight != 30) {
				t.Fatal("valid metrics not retained before viewport")
			}
			_, cmd = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			settle(t, m, cmd)
			gen := m.gen
			_, cmd = m.Update(uv.CellSizeEvent{Width: 14, Height: 30})
			if cmd != nil || m.gen != gen {
				t.Fatal("unchanged/disabled metrics must not render")
			}
			_, cmd = m.Update(uv.CellSizeEvent{Width: math.MaxInt, Height: 30})
			if cmd != nil || m.gen != gen || (enabled && (m.cellWidth != 14 || m.cellHeight != 30)) {
				t.Fatal("bad later reports must retain the last valid metrics")
			}
			_, cmd = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			if enabled {
				if cmd == nil || cmd().(tea.RawMsg).Msg != "\x1b[16t" || m.gen != gen {
					t.Fatal("resize notification must re-query even when columns stay unchanged")
				}
			} else if cmd != nil || m.cellWidth != 0 || m.cellHeight != 0 {
				t.Fatal("disabled graphics must ignore reports and resize queries")
			}
		})
	}
}

func TestCellSizeFallbackReport(t *testing.T) {
	for _, report := range []uv.CellSizeEvent{{Width: 10, Height: 20}, {Width: 0, Height: 0}} {
		t.Run(fmt.Sprint(report), func(t *testing.T) {
			m := New("# Heading\n", "")
			m.gfx, m.width, m.height = true, 80, 24
			first := m.requestRender()
			gen := m.gen
			_, cmd := m.Update(report)
			if cmd != nil || m.gen != gen {
				t.Fatal("report matching fallback or invalid report must not invalidate an inflight render")
			}
			settle(t, m, first)
			if m.rendering || len(m.heads) != 1 {
				t.Fatal("fallback render not applied")
			}
		})
	}
}

func TestCellSizeRenderSnapshots(t *testing.T) {
	for _, reader := range []bool{false, true} {
		t.Run(fmt.Sprintf("reader%v", reader), func(t *testing.T) {
			e := newImgEnv(t)
			e.writeImg(t, "wide.png", redPNG(t, 800, 200))
			m := New("# Heading\n\n[link](https://example.com)\n\n![wide](wide.png)\n\n"+strings.Repeat("after\n\n", 30), "doc.md")
			m.path = filepath.Join(e.dir, "doc.md")
			m.store, m.gfx, m.reader = e.store, true, reader
			m.width, m.height, m.readerWidth = 80, 24, 40
			m.syncVPWidth()
			settle(t, m, m.requestRender())
			data := e.store.data(1)
			pixels := append([]byte(nil), data.pix...)
			m.vp.SetYOffset(1)
			m.openTargets()
			if !m.targets.active {
				t.Fatal("precondition: picker did not open")
			}
			_, first := m.Update(uv.CellSizeEvent{Width: 10, Height: 24})
			if first == nil || m.targets.active || !m.rendering || m.anchor == nil {
				t.Fatal("metrics-only change must invalidate picker and preserve anchor during render")
			}
			_, coalesced := m.Update(uv.CellSizeEvent{Width: 20, Height: 40})
			if coalesced != nil {
				t.Fatal("inflight metric changes must coalesce")
			}
			old := first().(renderedMsg)
			w, _ := m.readerGeom(reader)
			cols, rows := fitCells(800, 200, max(1, w-2), 10, 24)
			if len(old.gfx.tx) != 0 || len(old.gfx.places) != 1 || old.gfx.places[0] != (place{1, cols, rows}) {
				t.Fatalf("render command did not snapshot first metrics: %+v", old.gfx)
			}
			before := e.store.placedGeom(1)
			_, latest := m.Update(old)
			if latest == nil || e.store.placedGeom(1) != before {
				t.Fatal("stale result must not apply placements")
			}
			fresh := latest().(renderedMsg)
			cols, rows = fitCells(800, 200, max(1, w-2), 20, 40)
			wantEscape := ""
			if before != [2]int{cols, rows} {
				wantEscape = kittyPlace(1, cols, rows)
			}
			if len(fresh.gfx.tx) != 0 || fresh.gfx.esc != wantEscape {
				t.Fatalf("latest metrics not used: %+v", fresh.gfx)
			}
			m.Update(fresh)
			if m.anchor != nil || m.vp.YOffset() != 1 {
				t.Fatal("metrics render lost reading position")
			}
			for _, action := range []struct {
				name string
				cmd  func() tea.Cmd
			}{
				{"resize", func() tea.Cmd { _, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24}); return cmd }},
				{"reader toggle", func() tea.Cmd { return press(m, "r") }},
			} {
				t.Run(action.name, func(t *testing.T) {
					settle(t, m, action.cmd())
					w, _ := m.readerGeom(m.reader)
					cols, rows := fitCells(800, 200, max(1, w-2), 20, 40)
					if e.store.placedGeom(1) != [2]int{cols, rows} || e.store.data(1) != data || !bytes.Equal(data.pix, pixels) {
						t.Fatal("resize/toggle changed cached pixels or ignored cell metrics")
					}
					msg := m.requestRender()().(renderedMsg)
					if msg.gfx.esc != "" {
						t.Fatal("unchanged geometry must not retransmit or replace")
					}
					m.Update(msg)
				})
			}
		})
	}
}

package pager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestDisplayTabsFooterFrame(t *testing.T) {
	for _, text := range []struct{ name, token string }{
		{"ASCII", "x"}, {"CJK", "界"}, {"emoji", "👨‍👩‍👧‍👦"},
	} {
		for _, style := range []string{"auto", "dark", "light", "notty"} {
			for _, bg := range []string{"", "none", "24"} {
				for _, reader := range []bool{false, true} {
					for _, width := range []int{40, 80, 120, 160, 200} {
						t.Run(fmt.Sprintf("%s/%s/bg%s/reader%v/w%d", text.name, style, bg, reader, width), func(t *testing.T) {
							code := "\tendpoint := \"" + strings.Repeat(text.token, 260) + "TAIL!\""
							src := "# Before\n\n" + strings.Repeat("before\n\n", 8) + "```go\n" + code + "\n\tshort\n```\n\n# After\n\n" + strings.Repeat("after\n\n", 8)
							m := renderDisplayTabsModel(t, src, style, bg, reader, width)
							cached := strings.Join(m.base, "\n")
							codeRow := -1
							for i, line := range m.stripped {
								if strings.Contains(line, "endpoint := \"") {
									if codeRow >= 0 {
										t.Fatal("code duplicated")
									}
									codeRow = i
								}
							}
							if codeRow < 0 || m.widest <= m.vp.Width() {
								t.Fatal("intrinsic code row lost")
							}
							h := m.vp.Height()
							// Entrance places the long row at the bottom; also check its
							// next step, top alignment, exit, and document boundaries.
							ys := []int{0, codeRow - h, codeRow - h + 1, codeRow - h + 2, codeRow, codeRow + 1, len(m.base) - h}
							for _, x := range []int{0, 1, 7, m.widest - m.vp.Width()} {
								m.vp.SetXOffset(x)
								seen := map[int]bool{}
								for _, y := range ys {
									m.vp.SetYOffset(y)
									actual := m.vp.YOffset()
									if !seen[actual] {
										seen[actual] = true
										assertDisplayTabsFrame(t, m)
									}
								}
							}
							control := renderDisplayTabsModel(t, strings.ReplaceAll(src, "\t", "    "), style, bg, reader, width)
							if !reflect.DeepEqual(m.stripped, control.stripped) || m.widest != control.widest || !reflect.DeepEqual(m.heads, control.heads) {
								t.Fatal("TABs and four-space control disagree on display geometry")
							}
							if !strings.Contains(m.stripped[codeRow], strings.ReplaceAll(code, "\t", "    ")) {
								t.Fatal("intrinsic code row reflowed")
							}
							m.vp.SetYOffset(codeRow)
							m.vp.SetXOffset(0)
							for x := 0; x < m.widest; x += m.hStep() {
								press(m, "l")
							}
							if m.vp.XOffset() != m.widest-m.vp.Width() || !strings.Contains(ansi.Strip(m.vp.View()), "TAIL") {
								t.Fatal("rightmost code tail unreachable")
							}
							assertDisplayTabsFrame(t, m)
							if m.source != src || strings.Join(m.base, "\n") != cached {
								t.Fatal("scrolling changed raw source or cached render")
							}
							m.search.query = "TAIL"
							m.refreshSearch()
							control.search.query = "TAIL"
							control.refreshSearch()
							if !reflect.DeepEqual(m.search.matches, control.search.matches) || len(m.search.matches) != 1 {
								t.Fatal("search geometry differs after TAB")
							}
							match := m.search.matches[0]
							prefix, _, _ := strings.Cut(m.stripped[codeRow], "TAIL")
							if match.line != codeRow || match.start != ansi.StringWidth(prefix) || !strings.Contains(m.vp.View(), curHL+"TAIL") {
								t.Fatal("search highlight missed code text after TAB")
							}
							assertDisplayTabsFrame(t, m)
						})
					}
				}
			}
		}
	}
}

func renderDisplayTabsModel(t *testing.T, src, style, bg string, reader bool, width int) *Model {
	t.Helper()
	m := New(src, "tabs.md")
	if err := m.SetStyle(style); err != nil {
		t.Fatal(err)
	}
	if bg != "" {
		color := config.Color(bg)
		theme := config.Theme{}
		theme.CodeBlock.BG = &color
		m.SetTheme(theme)
	}
	m.width, m.height, m.reader = width, 12, reader
	m.syncVPWidth()
	settle(t, m, m.requestRender())
	if m.errMsg != "" || len(m.base) == 0 {
		t.Fatalf("render failed: %s", m.errMsg)
	}
	return m
}

func assertDisplayTabsFrame(t *testing.T, m *Model) {
	t.Helper()
	rows := strings.Split(m.View().Content, "\n")
	if len(rows) != m.height || rows[len(rows)-1] != m.statusBar() {
		t.Fatalf("y%d/x%d: frame rows=%d want=%d with footer last", m.vp.YOffset(), m.vp.XOffset(), len(rows), m.height)
	}
	for _, row := range rows {
		if ansi.StringWidth(row) > m.width {
			t.Fatal("frame row exceeds terminal width")
		}
	}
}

func TestDisplayTabsMetadata(t *testing.T) {
	for _, style := range []string{"auto", "dark", "light", "notty"} {
		for _, bg := range []string{"", "none", "24"} {
			for _, reader := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/bg%s/reader%v", style, bg, reader), func(t *testing.T) {
					const dest = "https://example.org/full?target=preserved"
					src := "# Before\n\n```text\n\t" + strings.Repeat("wide", 60) + "\n```\n\n> BEFORE\t[AFTER](" + dest + ")\t[^n]\n\n![grid](img.png)\n\n# After\n\n[^n]: note body\n"
					m := renderDisplayTabsModel(t, src, style, bg, reader, 80)
					m.path, m.gfx, m.store = "/local/doc.md", true, newImageStore()
					defer m.store.cancel()
					m.store.ensureRef("/local/img.png", &imgData{w: 100, h: 100})
					settle(t, m, m.requestRender())
					geometry := m.store.placedGeom(1)
					control := renderDisplayTabsModel(t, strings.ReplaceAll(src, "\t", "    "), style, bg, reader, 80)
					control.path, control.gfx, control.store = m.path, true, m.store
					settle(t, control, control.requestRender())
					if !reflect.DeepEqual(m.stripped, control.stripped) || !reflect.DeepEqual(m.heads, control.heads) || !reflect.DeepEqual(m.links, control.links) || m.store.placedGeom(1) != geometry {
						t.Fatal("TAB control differs on image/heading/target/footnote geometry")
					}
					if geometry[0] == 0 || geometry[1] == 0 || !strings.Contains(strings.Join(m.stripped, "\n"), string(kitty.Placeholder)) || len(m.heads) != 2 || len(m.links) != 2 || countFootnoteTargets(m.links) != 1 {
						t.Fatal("image, heading, link or footnote missing")
					}
					for _, h := range m.heads {
						if !strings.Contains(m.stripped[h.line], h.text) {
							t.Fatal("heading position shifted")
						}
					}
					for _, target := range m.links {
						reg := target.regions[0]
						m.vp.SetYOffset(reg.line)
						m.vp.SetXOffset(0)
						margin := 0
						if reader {
							_, margin = m.readerGeom(true)
						}
						opened := ""
						m.openURL = func(s string) error { opened = s; return nil }
						if target.kind == targetExternal && ansi.Cut(m.stripped[reg.line], reg.start, reg.end) != "AFTER" {
							t.Fatal("link region missed label after TAB")
						}
						settle(t, m, sendMouseClick(m, margin+reg.start, reg.line-m.vp.YOffset()))
						if target.kind == targetExternal && opened != dest {
							t.Fatal("link click lost full URL after TAB")
						}
						if target.kind == targetFootnote && m.vp.YOffset() != min(target.definition.line, max(0, len(m.base)-m.vp.Height())) {
							t.Fatal("footnote click missed definition after TAB")
						}
						assertDisplayTabsFrame(t, m)
					}
					if m.source != src {
						t.Fatal("metadata consumers changed raw source")
					}
				})
			}
		}
	}
}

package pager

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestAdapterWrappedLinkTargets(t *testing.T) {
	dest := "https://example.com/very/long/path?query=complete-destination"
	for _, label := range []string{"guide", "a link label with enough ordinary words to wrap across several lines", dest, "bold **label** and `code`", "日本語 👨‍👩‍👧‍👦 label"} {
		for _, w := range []int{40, 80, 120} {
			t.Run(fmt.Sprintf("%s/w%d", label, w), func(t *testing.T) {
				src := "Prefix text before [" + label + "](" + dest + ") and [second](" + dest + ").\n"
				m := newRenderedModel(t, src, w, 30)
				if len(m.links) != 2 {
					t.Fatalf("logical links=%d want 2: %#v\nrender=%q", len(m.links), m.links, m.stripped)
				}
				opened := ""
				m.openURL = func(s string) error { opened = s; return nil }
				for _, link := range m.links {
					if link.dest != dest {
						t.Fatalf("destination=%q", link.dest)
					}
					for _, reg := range link.regions {
						m.vp.SetYOffset(reg.line)
						cmd := sendMouseClick(m, reg.start, reg.line-m.vp.YOffset())
						if cmd == nil {
							t.Fatalf("unclickable region %#v", reg)
						}
						settle(t, m, cmd)
						if opened != dest {
							t.Fatalf("opened %q", opened)
						}
					}
				}
				m.vp.GotoTop()
				press(m, "p")
				if len(m.targets.targets) != 2 {
					t.Fatalf("hints=%d", len(m.targets.targets))
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			})
		}
	}
}

func TestAdapterNavigationAcrossBlocks(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			src := "# First\n\n" + strings.Repeat("ordinary prose wraps here ", 20) + "\n\n| A | B |\n| - | - |\n| wide | last |\n\n## Before [Linked title](https://example.com/guide) after heading\n\nneedle ref[^1]\n\n" + strings.Repeat("tail filler\n\n", 20) + "[^1]: note body\n"
			m := newRenderedModel(t, src, w, 16)
			if len(m.heads) != 2 || m.heads[1].line <= m.heads[0].line || m.heads[1].line >= len(m.stripped) {
				t.Fatalf("headings=%#v", m.heads)
			}
			press(m, "o")
			press(m, "j")
			if !m.tocPreview || !strings.Contains(m.tocPreviewBody(), "Linked") {
				t.Fatal("cross-block outline preview lost linked heading")
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			m.openSearch()
			typeQuery(m, "needle")
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.search.count != 1 {
				t.Fatalf("search count=%d", m.search.count)
			}
			var fn *linkTarget
			for i := range m.links {
				if m.links[i].kind == targetFootnote {
					fn = &m.links[i]
					break
				}
			}
			if fn == nil {
				t.Fatal("footnote missing")
			}
			before := m.currentLocation()
			m.jumpFootnote(*fn)
			settle(t, m, pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace}))
			if m.currentLocation() != before {
				t.Fatal("footnote back changed position")
			}
			for _, key := range []string{"r", "s", "s", "r"} {
				settle(t, m, press(m, key))
			}
			if m.reader || countFootnoteTargets(m.links) != 1 {
				t.Fatal("representation round trip lost metadata")
			}
			nm, cmd := m.Update(tea.WindowSizeMsg{Width: w + 10, Height: 16})
			*m = *nm.(*Model)
			settle(t, m, cmd)
			nm, cmd = m.Update(reloadDoneMsg{body: []byte(src + "\n## Reloaded\n\nend\n")})
			*m = *nm.(*Model)
			settle(t, m, cmd)
			if len(m.heads) != 3 || countFootnoteTargets(m.links) != 1 {
				t.Fatalf("reload metadata heads=%#v links=%#v", m.heads, m.links)
			}
		})
	}
}

func TestAdapterUnboundTAndPrompts(t *testing.T) {
	for _, mode := range []string{"render", "reader", "search", "outline filter", "help filter"} {
		t.Run(mode, func(t *testing.T) {
			m := newRenderedModel(t, "# Title\n\n| A |\n| - |\n| value |\n", 60, 20)
			switch mode {
			case "reader":
				settle(t, m, press(m, "r"))
			case "search":
				press(m, "/")
			case "outline filter":
				press(m, "o")
				press(m, "/")
			case "help filter":
				press(m, "?")
				press(m, "/")
			}
			gen, content := m.gen, m.vp.GetContent()
			if cmd := press(m, "T"); cmd != nil {
				t.Fatal("T produced a command")
			}
			switch mode {
			case "search":
				if m.search.query != "T" {
					t.Fatal("search did not accept literal T")
				}
			case "outline filter":
				if m.tocFilter != "T" {
					t.Fatal("outline did not accept literal T")
				}
			case "help filter":
				if m.helpFilter != "T" {
					t.Fatal("help did not accept literal T")
				}
			default:
				if m.gen != gen || m.vp.GetContent() != content {
					t.Fatal("unbound T changed render state")
				}
			}
			for _, entry := range helpEntries {
				if entry.key == "T" {
					t.Fatal("T remains in help")
				}
			}
		})
	}
}

func TestAdapterWrappedLinksBesideTableLinks(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			proseURL := "https://prose.example/long/path/with/a/complete/destination"
			src := "| Link | Other |\n| - | - |\n| [first](https://table.example) | a |\n| [second](https://table.example) | b |\n\nRead [a guide with a long wrapped label](" + proseURL + ").\n"
			m := newRenderedModel(t, src, w, 30)
			proseTargets := 0
			for _, target := range m.links {
				if target.dest == proseURL {
					proseTargets++
					continue
				}
				for _, reg := range target.regions {
					if reg.line != target.regions[0].line {
						t.Fatalf("separate table rows coalesced: %#v", target)
					}
				}
			}
			if proseTargets != 1 {
				t.Fatalf("wrapped prose targets=%d: %#v", proseTargets, m.links)
			}
		})
	}
}

func TestAdapterLinkedHeadingMapping(t *testing.T) {
	for _, dest := range []string{"https://example.org/very/long/path/for/a/wrapped/destination", "#section", "#", "#with%20space", "bad%zz", "relative.md"} {
		for _, w := range []int{40, 80, 120} {
			t.Run(fmt.Sprintf("%s/w%d", dest, w), func(t *testing.T) {
				src := "# Root\n\nbody\n\n## Before [linked title](" + dest + ") After " + strings.Repeat("longtoken", 10) + "\n\nend\n"
				m := newRenderedModel(t, src, w, 12)
				if len(m.heads) != 2 {
					t.Fatalf("heads=%#v", m.heads)
				}
				line := m.heads[1].line
				if line >= len(m.stripped) || !strings.Contains(m.stripped[line], "Before") {
					t.Fatalf("linked heading not mapped to first wrapped row: %#v render=%q", m.heads, m.stripped)
				}
			})
		}
	}
}

func TestAdapterMultilineTableTargets(t *testing.T) {
	dest := "https://example.org/full/path?query=complete&other=preserved"
	label := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu"
	for _, width := range []int{40, 80, 120} {
		for _, reader := range []bool{false, true} {
			t.Run(fmt.Sprintf("w%d/reader=%v", width, reader), func(t *testing.T) {
				linked := "[" + label + "][global]"
				src := "[global]: " + dest + "\n\n" + markdownTable([]string{"Early", "N", "Middle", "Atomic", "Last"}, [][]string{
					{linked, "x", "**" + linked + "**", strings.Repeat("endpoint", 9), linked},
					{linked + " then " + linked, "y", "z", "id", "end"},
				}) + "\nOutside [external][global].\n\n" + strings.Repeat("tail\n\n", 15)
				m := newRenderedModel(t, src, width, 12)
				if reader {
					settle(t, m, press(m, "r"))
				}
				if len(m.links) != 6 {
					t.Fatalf("logical links=%d want6: %#v", len(m.links), m.links)
				}
				original := strings.Join(m.base, "\n")
				seen := map[string]bool{}
				for _, link := range m.links {
					if link.dest != dest {
						t.Fatalf("lost destination: %#v", link)
					}
					if !strings.HasPrefix(link.id, tableLinkPrefix) {
						continue
					}
					if seen[link.id] {
						t.Fatal("repeated destinations merged")
					}
					seen[link.id] = true
					if len(link.regions) < 4 {
						t.Fatalf("wrapped regions=%#v", link.regions)
					}
					if strings.Join(strings.Fields(strings.Join(link.texts, " ")), " ") != label {
						t.Fatalf("label reconstruction=%q", link.texts)
					}
					for _, reg := range link.regions {
						text := ansi.Strip(ansi.Cut(m.base[reg.line], reg.start, reg.end))
						if strings.ContainsAny(text, "│|") {
							t.Fatalf("OSC hit includes table rail: %q", text)
						}
						m.vp.SetYOffset(reg.line)
						m.vp.SetXOffset(reg.start + 1)
						margin := 0
						if reader {
							_, margin = readerGeom(width, true)
						}
						opened := ""
						m.openURL = func(s string) error { opened = s; return nil }
						cmd := sendMouseClick(m, margin+reg.start+1-m.vp.XOffset(), reg.line-m.vp.YOffset())
						if cmd == nil {
							t.Fatalf("clipped/panned region not clickable: %#v", reg)
						}
						settle(t, m, cmd)
						if opened != dest {
							t.Fatalf("opened=%q", opened)
						}
						opened = ""
						press(m, "p")
						hint := ""
						for _, target := range m.targets.targets {
							if target.id == link.id {
								hint = target.label
							}
						}
						if hint == "" {
							t.Fatalf("wrapped clipped link absent from hints: %#v", reg)
						}
						settle(t, m, press(m, strings.ToLower(hint)))
						if opened != dest {
							t.Fatalf("hint opened=%q", opened)
						}
					}
				}
				if len(seen) != 5 {
					t.Fatalf("table occurrences=%d", len(seen))
				}
				if strings.Join(m.base, "\n") != original {
					t.Fatal("mouse/hints mutated cache")
				}
				settle(t, m, press(m, "s"))
				if m.source != src {
					t.Fatal("unbound s changed raw document")
				}
				settle(t, m, press(m, "s"))
				if strings.Join(m.base, "\n") != original || len(m.links) != 6 {
					t.Fatal("unbound s changed render/targets")
				}
			})
		}
	}
}

func TestAdapterTableFootnoteNavigation(t *testing.T) {
	for _, reader := range []bool{false, true} {
		t.Run(fmt.Sprintf("reader=%v", reader), func(t *testing.T) {
			marker := "[^note]"
			src := markdownTable([]string{"Narrative", "Other"}, [][]string{{strings.Repeat("ordinary readable explanation ", 7) + marker, "x"}, {"again" + marker, "y"}}) + "\n" + strings.Repeat("filler\n\n", 15) + marker + ": note body\n\n" + strings.Repeat("tail\n\n", 8)
			m := newRenderedModel(t, src, 40, 10)
			if reader {
				settle(t, m, press(m, "r"))
			}
			if countFootnoteTargets(m.links) != 2 {
				t.Fatalf("footnotes=%#v", m.links)
			}
			original := strings.Join(m.base, "\n")
			for _, target := range m.links {
				if target.kind != targetFootnote {
					continue
				}
				reg := target.regions[0]
				if ansi.Cut(m.stripped[reg.line], reg.start, reg.end) != marker {
					t.Fatalf("marker hit=%#v", reg)
				}
				m.vp.SetYOffset(reg.line)
				m.vp.SetXOffset(reg.start)
				before := m.currentLocation()
				margin := 0
				if reader {
					_, margin = readerGeom(m.width, true)
				}
				if cmd := sendMouseClick(m, margin+reg.start-m.vp.XOffset(), reg.line-m.vp.YOffset()); cmd != nil {
					t.Fatal("footnote opened externally")
				}
				if m.vp.YOffset() != target.definition.line || len(m.locations) != 1 {
					t.Fatal("footnote missed definition")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
				if m.currentLocation() != before {
					t.Fatal("footnote return changed position")
				}
			}
			if strings.Join(m.base, "\n") != original {
				t.Fatal("footnote navigation changed cache")
			}
		})
	}
}

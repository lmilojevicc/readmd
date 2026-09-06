package pager

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
				press(m, "t")
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
			if m.srcView || m.reader || countFootnoteTargets(m.links) != 1 {
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
	for _, mode := range []string{"render", "reader", "source", "search", "outline filter", "help filter"} {
		t.Run(mode, func(t *testing.T) {
			m := newRenderedModel(t, "# Title\n\n| A |\n| - |\n| value |\n", 60, 20)
			switch mode {
			case "reader":
				settle(t, m, press(m, "r"))
			case "source":
				press(m, "s")
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

package pager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTargetBadgePlacement(t *testing.T) {
	for _, tc := range []struct {
		name, before, text, after string
		x, wantStart              int
		fallback                  bool
	}{
		{"spacious prose", "intro ", "link", "     tail", 0, 10, false},
		{"left whitespace", "intro    ", "link", ",tail", 0, 6, false},
		{"crowded own text", "(", "linked", "),tail", 0, 1, false},
		{"one cell", "(", "x", "),tail", 0, 0, true},
		{"narrow reference", "(", "^n", "),tail", 0, 0, true},
		{"rail at boundary", "│ ", "x", " │ tail", 0, 0, true},
		{"rail in link text", "(", "│──│", ")", 0, 0, true},
		{"CJK indivisible", "(", "界界", "),tail", 0, 0, true},
		{"CJK plus ASCII", "(", "界a界", "),tail", 0, 1, false},
		{"emoji combining", "(", "👨‍👩‍👧‍👦éxx", "),tail", 0, 1, false},
		{"partly clipped long link", "", "abcdefghi", strings.Repeat("!", 50), 2, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := tc.before + linkLine("one", "https://one.example", tc.text) + tc.after
			m := newBadgeModel(t, 40, 10, []string{line})
			m.openURL = func(string) error { t.Fatal("placement opened link"); return nil }
			m.vp.SetXOffset(tc.x)
			before, cache, location := m.View().Content, strings.Join(m.base, "\n"), m.currentLocation()
			press(m, "p")
			if !m.targets.active || len(m.targets.targets) != 1 {
				t.Fatalf("missing target: %#v", m.targets)
			}
			if (len(m.targets.vimium.fallback) == 1) != tc.fallback {
				t.Fatalf("fallback=%d badges=%#v", len(m.targets.vimium.fallback), m.targets.vimium.badges)
			}
			if !tc.fallback && (len(m.targets.vimium.badges) != 1 || m.targets.vimium.badges[0].start != tc.wantStart) {
				t.Fatalf("badges=%#v want start=%d", m.targets.vimium.badges, tc.wantStart)
			}
			assertTargetPlan(t, m)
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.View().Content != before || strings.Join(m.base, "\n") != cache || m.currentLocation() != location {
				t.Fatal("cancel changed original view/cache/location")
			}
		})
	}
}

func assertTargetPlan(t *testing.T, m *Model) {
	t.Helper()
	plan := m.targetPlan()
	seen := map[string]bool{}
	for i, badge := range m.targets.vimium.badges {
		if seen[badge.target.label] {
			t.Fatal("duplicate occurrence badge")
		}
		seen[badge.target.label] = true
		if !safeBadgeCells(badgeCells(m.base[badge.line], badge.end), badge.start, badge.end, false) {
			t.Fatalf("badge cuts grapheme or rail: %#v", badge)
		}
		for _, other := range m.targets.vimium.badges[:i] {
			if badge.line == other.line && badge.start < other.end && badge.end > other.start {
				t.Fatal("badge collision")
			}
		}
	}
	for _, target := range m.targets.vimium.fallback {
		if seen[target.label] {
			t.Fatal("badge duplicated in fallback")
		}
		seen[target.label] = true
	}
	if len(seen) != len(m.targets.targets) {
		t.Fatalf("stranded candidates: %d of %d", len(seen), len(m.targets.targets))
	}
	view := m.View().Content
	if !utf8.ValidString(ansi.Strip(view)) || len(strings.Split(view, "\n")) != m.height {
		t.Fatal("view geometry/UTF-8 changed")
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("view overflows: %q", line)
		}
	}
	for _, badge := range plan.badges {
		line := strings.Split(view, "\n")[badge.line]
		if ansi.Cut(ansi.Strip(line), badge.start, badge.end) != "["+badge.target.label+"]" {
			t.Fatalf("final badge missing/cut: %#v line=%q", badge, ansi.Strip(line))
		}
	}
	for _, hit := range plan.hits {
		if hit.line >= plan.surfaceTop {
			if !strings.Contains(ansi.Strip(plan.rows[hit.line-plan.surfaceTop]), "["+hit.target.label+"]") {
				t.Fatal("fallback hit lacks complete displayed label")
			}
		}
	}
}

func crowdedBadgeLines(n int) []string {
	var lines []string
	for i := range n {
		lines = append(lines, "("+linkLine(fmt.Sprint(i), fmt.Sprintf("https://same.example/%d", i), "x")+")")
	}
	return lines
}

func TestTargetBadgeFallbackPagesAndMouse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		height int
		key    tea.KeyPressMsg
	}{
		{"tab", 40, 12, tea.KeyPressMsg{Code: tea.KeyTab}},
		{"arrow", 80, 12, tea.KeyPressMsg{Code: tea.KeyDown}},
		{"tiny capacity", 24, 4, tea.KeyPressMsg{Code: tea.KeyRight}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newBadgeModel(t, tc.width, tc.height, crowdedBadgeLines(10))
			opened := ""
			m.openURL = func(dest string) error { opened = dest; return nil }
			press(m, "p")
			location := m.currentLocation()
			seen := map[string]bool{}
			for range len(m.targets.vimium.fallback) {
				assertTargetPlan(t, m)
				plan := m.targetPlan()
				if !strings.Contains(plan.rows[0], "page") || !strings.Contains(m.targetFooter(), "Tab") {
					t.Fatal("missing fallback count/page/controls")
				}
				for _, hit := range plan.hits {
					seen[hit.target.label] = true
				}
				if cmd := sendMouseClick(m, 1, plan.surfaceTop); cmd != nil || !m.targets.active {
					t.Fatal("fallback header clicked through")
				}
				pressKey(m, tc.key)
			}
			if len(seen) != len(m.targets.vimium.fallback) || m.currentLocation() != location {
				t.Fatal("pagination strands targets or scrolls document")
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			if m.targets.vimium.focus != len(m.targets.vimium.fallback)-1 {
				t.Fatal("Shift+Tab must cycle to final fallback")
			}
			plan := m.targetPlan()
			hit := plan.hits[len(plan.hits)-1]
			settle(t, m, sendMouseClick(m, hit.start, hit.line))
			if opened != hit.target.dest || m.targets.active {
				t.Fatal("fallback mouse missed final placement")
			}
			press(m, "t") // Clear the opener notice before reopening a tiny viewport.
			press(m, "p")
			want := m.filteredFallback()[0].dest
			settle(t, m, pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}))
			if opened != want || m.targets.active {
				t.Fatal("Enter missed focused fallback")
			}
		})
	}
}

func TestTargetBadgeFallbackOccurrenceDescriptions(t *testing.T) {
	for _, tc := range []struct {
		name, cells, definition string
		kind                    targetKind
	}{
		{"query and fragment links", "[x](https://e.test/path?q=1#one) | [x](https://e.test/path?q=2#two)", "", targetExternal},
		{"repeated footnote", "[^n] | [^n]", "\n[^n]: shared note\n", targetFootnote},
	} {
		for _, width := range []int{24, 80} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, width), func(t *testing.T) {
				src := "| A | B |\n| - | - |\n" + strings.Repeat("| "+tc.cells+" |\n", 14) + tc.definition
				m := newBadgeRenderedModel(t, src, width, 80)
				press(m, "p")
				if !m.targets.active || len(m.targets.vimium.fallback) != 28 || len(m.targets.vimium.badges) != 0 {
					t.Fatalf("rail-bounded targets must fall back: %#v", m.targets)
				}
				seen := map[string]string{}
				for range len(m.targets.vimium.fallback) {
					assertTargetPlan(t, m)
					plan := m.targetPlan()
					view := strings.Split(ansi.Strip(m.View().Content), "\n")
					for _, hit := range plan.hits {
						target := hit.target
						if target.kind != tc.kind || len(target.label) != 2 {
							t.Fatalf("unexpected target or incomplete key: %#v", target)
						}
						origin := target.regions[0]
						position := fmt.Sprintf("L%d:C%d", origin.line+1, origin.start+1)
						row := strings.TrimLeft(view[hit.line], "> ")
						if !strings.HasPrefix(row, "["+target.label+"] "+position+" ") {
							t.Fatalf("key/occurrence prefix lost: %q", row)
						}
						if label, ok := seen[position]; ok && label != target.label {
							t.Fatalf("occurrences share position %s: %s and %s", position, label, target.label)
						}
						seen[position] = target.label
						if width == 80 {
							want := "footnote:n"
							if tc.kind == targetExternal {
								want = strings.TrimPrefix(target.dest, "https://")
							}
							if !strings.Contains(row, want) {
								t.Fatalf("destination summary lost: %q, want %q", row, want)
							}
						}
					}
					pressKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
				}
				if len(seen) != 28 {
					t.Fatalf("only %d distinct occurrences displayed", len(seen))
				}
			})
		}
	}
}

func TestTargetBadgePrefixAndUnlimitedLabels(t *testing.T) {
	for _, n := range []int{27, 677} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			labels := hintLabels(n)
			seen := map[string]bool{}
			for _, label := range labels {
				if seen[label] || len(label) != len(labels[0]) {
					t.Fatal("labels not unique/prefix-free")
				}
				seen[label] = true
			}
			if len(seen) != n {
				t.Fatal("candidate cap silently drops targets")
			}
			m := newBadgeModel(t, 40, n+2, crowdedBadgeLines(n))
			m.openURL = func(string) error { t.Fatal("prefix unexpectedly activated"); return nil }
			press(m, "p")
			press(m, "a")
			if !m.targets.active {
				t.Fatal("nonunique prefix activated")
			}
			for _, hit := range m.targetPlan().hits {
				if !strings.HasPrefix(hit.target.label, "A") {
					t.Fatal("filtered fallback still displayed")
				}
			}
			pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
			if len(m.targetCandidates()) != n {
				t.Fatal("Backspace lost candidates")
			}
		})
	}
}

func TestTargetBadgeOriginalViewportAndInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(*Model)
	}{
		{"escape", func(m *Model) { pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) }},
		{"resize", func(m *Model) { m.Update(tea.WindowSizeMsg{Width: 80, Height: 10}) }},
		{"reload", func(m *Model) { m.Update(reloadDoneMsg{body: []byte("new\n")}) }},
		{"render replacement", func(m *Model) { m.Update(renderedMsg{gen: m.gen, content: "new", stripped: []string{"new"}}) }},
		{"render error", func(m *Model) { m.Update(renderedMsg{gen: m.gen, err: errors.New("render")}) }},
		{"opener completion", func(m *Model) { m.Update(openedURLMsg{dest: "https://old.example"}) }},
		{"new notice", func(m *Model) { m.flash = "new notice"; m.View() }},
		{"reload failure", func(m *Model) { m.Update(reloadDoneMsg{err: errors.New("reload")}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := append([]string{"before"}, crowdedBadgeLines(20)...)
			m := newBadgeModel(t, 40, 10, lines)
			m.flash = "existing notice"
			m.syncVPHeight()
			m.vp.SetYOffset(1)
			m.openURL = func(string) error { t.Fatal("stale target opened"); return nil }
			before, location, height := m.View().Content, m.currentLocation(), m.vp.Height()
			geometry, _ := m.targetViewportRows()
			original := visibleLinkTargets(m.links, m.vp.YOffset(), geometry)
			press(m, "p")
			if m.vp.Height() != height || len(m.targets.targets) != len(original) || m.currentLocation() != location {
				t.Fatal("entry changed original viewport/candidates")
			}
			tc.act(m)
			if m.targets.active {
				t.Fatal("stale picker survived invalidation")
			}
			if tc.name == "escape" && (m.View().Content != before || m.currentLocation() != location) {
				t.Fatal("escape failed exact restoration with notice")
			}
		})
	}
}

func TestTargetBadgeTinyDecline(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {8, 8}, {23, 10}, {80, 3}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := newBadgeModel(t, size[0], size[1], crowdedBadgeLines(2))
			press(m, "p")
			if m.targets.active || m.flash != "not enough room for targets" {
				t.Fatal("tiny picker stranded targets")
			}
		})
	}
}

func TestTargetBadgeRenderedTables(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, reader := range []bool{false, true} {
			t.Run(fmt.Sprintf("w%d/reader%v", width, reader), func(t *testing.T) {
				src := "# Links\n\n" + markdownTable([]string{"Long link", "Refs", "Repeat"}, [][]string{
					{"[" + strings.Repeat("界é emoji 👨‍👩‍👧‍👦 words ", 6) + "](https://same.example)", "x[^n]y", "[same](https://same.example)"},
					{"[a](https://a.example)[b](https://b.example)", "[^n]", "[same](https://same.example)"},
				}) + "\n[^n]: note text\n"
				m := newBadgeRenderedModel(t, src, width, 24)
				m.style = paletteStyleName
				settle(t, m, m.requestRender())
				m.openURL = func(string) error { return nil }
				if reader {
					settle(t, m, press(m, "r"))
				}
				cache, links := strings.Join(m.base, "\n"), fmt.Sprintf("%#v", m.links)
				for _, x := range []int{0, 7, 30} {
					m.vp.SetXOffset(x)
					m.vp.SetYOffset(2)
					before, location := m.View().Content, m.currentLocation()
					press(m, "p")
					if !m.targets.active {
						if m.flash != targetPanNotice || m.currentLocation() != location {
							t.Fatal("unsafe table slice must decline without moving")
						}
						press(m, "t")
						continue
					}
					assertTargetPlan(t, m)
					if dir := os.Getenv("READMD_BADGE_SAMPLES"); dir != "" && x == 0 {
						if err := os.MkdirAll(dir, 0755); err != nil {
							t.Fatal(err)
						}
						for _, ext := range []string{"ansi", "txt"} {
							view := m.View().Content
							if ext == "txt" {
								view = ansi.Strip(view)
							}
							path := filepath.Join(dir, fmt.Sprintf("crowded-table-%d-reader%v.%s", width, reader, ext))
							if err := os.WriteFile(path, []byte(view+"\n"), 0644); err != nil {
								t.Fatal(err)
							}
						}
					}
					plan := m.targetPlan()
					originalRows, badgeRows := strings.Split(before, "\n"), strings.Split(m.View().Content, "\n")
					for y := 0; y < plan.surfaceTop; y++ {
						for _, cell := range badgeCells(originalRows[y], width) {
							if strings.ContainsAny(cell.text, "╭╮╰╯├┤┬┴┼─│") && ansi.Cut(ansi.Strip(badgeRows[y]), cell.start, cell.end) != cell.text {
								t.Fatalf("rail changed at (%d,%d)", cell.start, y)
							}
						}
					}
					pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
					if m.View().Content != before || m.currentLocation() != location || strings.Join(m.base, "\n") != cache || fmt.Sprintf("%#v", m.links) != links {
						t.Fatal("table cache or cancel invariance failed")
					}
				}
			})
		}
	}
}

func TestTargetBadgeControlsAndCache(t *testing.T) {
	for _, line := range []string{
		"\x1b[31m(" + linkLine("one", "https://one.example/full?q=1", "ab\x1b[32mcd\x1b[0mef") + ")",
		linkLine("one", "https://one.example", "界é👨‍👩‍👧‍👦") + "     ",
	} {
		t.Run(ansi.Strip(line), func(t *testing.T) {
			start := 1
			if strings.HasPrefix(ansi.Strip(line), "界") {
				start = 0
			}
			painted := paintTargetBadge(line, start, start+3, "[A]")
			controls := func(s string) []string {
				var out []string
				state := byte(0)
				for s != "" {
					seq, _, n, next := ansi.DecodeSequence(s, state, nil)
					s, state = s[n:], next
					if _, _, ok := parseOSC8(seq); ok {
						out = append(out, seq)
					}
				}
				return out
			}
			if !reflect.DeepEqual(controls(line), controls(painted)) {
				t.Fatal("badge changed stock OSC opcode order or destination")
			}
			underlined := underlineTargets(line, []span{{0, ansi.StringWidth(line)}})
			if ansi.Strip(underlined) != ansi.Strip(line) || !reflect.DeepEqual(controls(line), controls(underlined)) || strings.Contains(underlined, "\x1b[7m") {
				t.Fatal("underline changed content/links or reverse-painted link")
			}
			for _, sgr := range []string{"\x1b[31m", "\x1b[32m"} {
				if strings.Contains(line, sgr) && !strings.Contains(underlined, sgr) {
					t.Fatal("normal link color lost")
				}
			}
		})
	}
}

func TestTargetBadgeExampleSamples(t *testing.T) {
	dir := os.Getenv("READMD_BADGE_SAMPLES")
	if dir == "" {
		t.Skip("set READMD_BADGE_SAMPLES to export optional local fixture views")
	}
	src, err := os.ReadFile("../../example.md")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("optional local example.md absent")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newBadgeRenderedModel(t, string(src), width, 24)
			m.style = paletteStyleName
			settle(t, m, m.requestRender())
			m.openURL = func(string) error { t.Fatal("sample opened browser"); return nil }
			for _, section := range []struct{ name string }{{"prose"}, {"table"}, {"footnotes"}} {
				x, y := 0, 0
				for _, target := range m.links {
					if (section.name == "table") == strings.HasPrefix(target.id, tableLinkPrefix) && (section.name == "footnotes") == (target.kind == targetFootnote) {
						y = max(0, target.regions[0].line-3)
						if section.name == "table" {
							x = max(0, target.regions[0].start-4)
						}
						break
					}
				}
				m.vp.SetYOffset(y)
				m.vp.SetXOffset(x)
				press(m, "p")
				if !m.targets.active {
					t.Fatal("fixture has no visible targets")
				}
				assertTargetPlan(t, m)
				for _, ext := range []string{"ansi", "txt"} {
					view := m.View().Content
					if ext == "txt" {
						view = ansi.Strip(view)
					}
					path := filepath.Join(dir, fmt.Sprintf("example-%s-%d.%s", section.name, width, ext))
					if err := os.WriteFile(path, []byte(view+"\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			}
		})
	}
}

func TestTargetBadgeFragmentsAndCrowding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lines    []string
		links    []linkTarget
		wantLine int
	}{
		{"prefer another fragment whitespace", []string{"(linked),", "fragment     tail"}, []linkTarget{{id: "wrapped", dest: "https://same.example", regions: []targetRegion{{0, 1, 7}, {1, 0, 8}}}}, 1},
		{"shared whitespace collisions", []string{linkLine("a", "https://same.example", "x") + "   " + linkLine("b", "https://same.example", "y") + "   " + linkLine("c", "https://same.example", "z")}, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newBadgeModel(t, 40, 10, tc.lines)
			m.openURL = func(string) error { t.Fatal("placement opened"); return nil }
			if tc.links != nil {
				m.links = tc.links
			}
			press(m, "p")
			assertTargetPlan(t, m)
			if len(m.targets.vimium.badges) != len(m.links) || m.targets.vimium.badges[0].line != tc.wantLine {
				t.Fatalf("badges=%#v", m.targets.vimium.badges)
			}
		})
	}
}

func TestTargetBadgeMouseReaderAndSearch(t *testing.T) {
	for _, tc := range []struct {
		name          string
		reader, badge bool
		x             int
	}{
		{"badge", false, true, 0},
		{"original", false, false, 0},
		{"panned badge", false, true, 7},
		{"reader badge", true, true, 7},
		{"reader original", true, false, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := strings.Repeat("x", 12) + linkLine("one", "https://one.example", "linked text") + strings.Repeat(" ", 10) + strings.Repeat("x", 140)
			m := newBadgeModel(t, 140, 12, []string{"before", line})
			m.reader = tc.reader
			m.syncVPWidth()
			m.vp.SetXOffset(tc.x)
			m.vp.SetYOffset(1)
			m.search.query = "linked"
			m.refreshSearch()
			m.vp.SetXOffset(tc.x)
			m.vp.SetYOffset(1)
			cache, before := strings.Join(m.base, "\n"), m.currentLocation()
			opened := 0
			m.openURL = func(string) error { opened++; return nil }
			press(m, "p")
			plan := m.targetPlan()
			if len(plan.badges) != 1 {
				t.Fatal("missing badge")
			}
			hit := plan.badges[0]
			x, y := hit.start, hit.line
			margin := 0
			if tc.reader {
				_, margin = readerGeom(m.width, true)
			}
			if !tc.badge {
				x, y = margin+12-before.x, 1-before.y
			}
			if tc.reader {
				if cmd := sendMouseClick(m, margin-1, 0); cmd != nil {
					t.Fatal("reader margin click-through")
				}
			}
			if cmd := sendMouseClick(m, m.width-1, m.height-1); cmd != nil {
				t.Fatal("footer click-through")
			}
			settle(t, m, sendMouseClick(m, x, y))
			if opened != 1 || m.targets.active || m.currentLocation() != before || strings.Join(m.base, "\n") != cache {
				t.Fatal("final plan hit/cache/location mismatch")
			}
		})
	}
}

func TestTargetBadgeTypingControls(t *testing.T) {
	for _, key := range []string{"j", "k", "ctrl+c", "filter"} {
		t.Run(key, func(t *testing.T) {
			var lines []string
			n := 26
			if key == "filter" {
				n = 27
			}
			for i := range n {
				lines = append(lines, linkLine(fmt.Sprint(i), fmt.Sprintf("https://example.org/%d", i), "linked")+"     ")
			}
			m := newBadgeModel(t, 40, 40, lines)
			opened := ""
			m.openURL = func(dest string) error { opened = dest; return nil }
			press(m, "p")
			before := m.currentLocation()
			switch key {
			case "ctrl+c":
				cmd := pressKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				if cmd == nil {
					t.Fatal("ctrl+c blocked")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("ctrl+c did not quit")
				}
			case "filter":
				press(m, "a")
				plan := m.targetPlan()
				if len(plan.badges) != 26 || m.targets.prefix != "a" {
					t.Fatal("badges not filtered")
				}
				for _, badge := range plan.badges {
					if !strings.HasPrefix(badge.target.label, "A") {
						t.Fatal("unmatched badge remains")
					}
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
				if len(m.targetPlan().badges) != 27 {
					t.Fatal("Backspace lost original badges")
				}
			default:
				settle(t, m, press(m, key))
				want := fmt.Sprintf("https://example.org/%d", strings.Index(hintAlphabet, key))
				if opened != want || m.targets.active {
					t.Fatal("j/k did not activate their hint letters")
				}
			}
			if m.currentLocation() != before {
				t.Fatal("hint controls scrolled document")
			}
		})
	}
}

func TestTargetBadgeStockSliceReloadError(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		safe       bool
	}{
		{"unsafe slice replaces old error", strings.Repeat("x", 50), false},
		{"safe entry preserves old error", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := "界" + linkLine("one", "https://one.example", "linked") + tc.tail
			lines := append([]string{"before", line}, strings.Split(strings.Repeat("filler\n", 20), "\n")...)
			m := newBadgeModel(t, 40, 10, lines)
			m.errMsg = "reload: old failure"
			m.flash = "existing notice"
			m.syncVPHeight()
			m.vp.SetXOffset(1)
			m.vp.SetYOffset(1)
			location := m.currentLocation()
			before := m.View().Content
			if _, safe := m.targetViewportRows(); safe != tc.safe {
				t.Fatalf("safe=%v, want %v", safe, tc.safe)
			}
			press(m, "p")
			view := ansi.Strip(m.View().Content)
			if m.targets.active != tc.safe || m.currentLocation() != location {
				t.Fatal("entry changed activation or original X/Y")
			}
			if tc.safe {
				if m.errMsg != "reload: old failure" || m.flash != "existing notice" {
					t.Fatal("successful entry discarded existing notices")
				}
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.View().Content != before {
					t.Fatal("cancel did not preserve existing error view")
				}
			} else {
				if m.errMsg != "" || !strings.Contains(view, targetPanNotice) || strings.Contains(view, "old failure") {
					t.Fatalf("recovery instruction masked: %q", view)
				}
				if cmd := sendMouseClick(m, 2, 0); cmd != nil || m.targets.active || m.currentLocation() != location {
					t.Fatal("unsafe document click activated or moved")
				}
				if !strings.Contains(ansi.Strip(m.View().Content), targetPanNotice) {
					t.Fatal("unsafe document click hid recovery instruction")
				}
			}
		})
	}
}

func TestTargetBadgeStockSliceGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, lead, tail string
		safe             bool
	}{
		{"CJK single screen row", "界", "", true},
		{"emoji single screen row", "👨‍👩‍👧‍👦", "", true},
		{"CJK extra screen row", "界", strings.Repeat("x", 50), false},
		{"emoji extra screen row", "👨‍👩‍👧‍👦", strings.Repeat("x", 50), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := tc.lead + linkLine("one", "https://one.example", "linked") + tc.tail
			m := newBadgeModel(t, 40, 10, []string{line, linkLine("next", "https://next.example", "NEXT"), strings.Repeat("x", 60)})
			m.flash = "existing notice"
			m.syncVPHeight()
			m.vp.SetXOffset(1)
			before, location := m.View().Content, m.currentLocation()
			opened := ""
			m.openURL = func(dest string) error { opened = dest; return nil }
			geometry, safe := m.targetViewportRows()
			if safe != tc.safe {
				t.Fatalf("safe=%v", safe)
			}
			press(m, "p")
			if m.targets.active != tc.safe || m.currentLocation() != location {
				t.Fatal("unsafe entry or moved location")
			}
			if tc.safe {
				if geometry[0].left != 0 {
					t.Fatal("left-clipped grapheme origin not retained")
				}
				assertTargetPlan(t, m)
				pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.View().Content != before {
					t.Fatal("CJK cancel/notice did not restore exactly")
				}
				settle(t, m, sendMouseClick(m, 2, 0))
				if opened != "https://one.example" {
					t.Fatal("normal CJK mouse mapping missed link")
				}
			} else {
				if m.flash != targetPanNotice {
					t.Fatal("unsafe pan recovery not explained")
				}
				for _, y := range []int{0, 1, 2} {
					if cmd := sendMouseClick(m, 2, y); cmd != nil {
						t.Fatal("wrong-link mouse activation in wrapped geometry")
					}
				}
				press(m, "0")
				press(m, "p")
				if !m.targets.active {
					t.Fatal("reset pan did not restore picker")
				}
			}
		})
	}
}

func newBadgeModel(t *testing.T, width, height int, lines []string) *Model {
	m := newHintModel(t, width, height, lines)
	m.picker = pickerVimium
	return m
}
func newBadgeRenderedModel(t *testing.T, source string, width, height int) *Model {
	m := newRenderedModel(t, source, width, height)
	m.picker = pickerVimium
	return m
}

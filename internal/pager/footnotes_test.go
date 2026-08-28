package pager

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

func TestSourceFootnoteMarkers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		wantRefs []string
		wantDefs []string
		ok       bool
	}{
		{
			"numeric named multiple and unicode context",
			"界[^1] 👨‍👩‍👧‍👦[^name] again[^1]\n\n[^1]: one\n[^name]: named\n    continuation\n",
			[]string{"1", "name", "1"}, []string{"1", "name"}, true,
		},
		{
			"code and escaped markers excluded",
			"`[^1]` \\[^1] real[^1]\n\n```md\n[^1]\n```\n\n[^1]: note\n",
			[]string{"1"}, []string{"1"}, true,
		},
		{
			"inline raw HTML marker excluded",
			"<span title=\"[^n]\">x</span>\nreal[^n]\n\n[^n]: note\n",
			[]string{"n"}, []string{"n"}, true,
		},
		{
			"HTML block marker excluded",
			"<div data-note=\"[^n]\">\ncontent\n</div>\n\nreal[^n]\n\n[^n]: note\n",
			[]string{"n"}, []string{"n"}, true,
		},
		{
			"missing definition ignored",
			"missing[^none]\n",
			nil, nil, true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markers, ok := sourceFootnoteMarkers(tc.source)
			if ok != tc.ok {
				t.Fatalf("ok=%v want=%v markers=%#v", ok, tc.ok, markers)
			}
			var refs, defs []string
			for _, marker := range markers {
				if marker.reference {
					refs = append(refs, marker.label)
				}
				if marker.definition {
					defs = append(defs, marker.label)
				}
			}
			if strings.Join(refs, ",") != strings.Join(tc.wantRefs, ",") || strings.Join(defs, ",") != strings.Join(tc.wantDefs, ",") {
				t.Fatalf("refs=%v defs=%v, want refs=%v defs=%v; markers=%#v", refs, defs, tc.wantRefs, tc.wantDefs, markers)
			}
		})
	}
}

func TestDefinitionLikeLiteralsDoNotDisableFootnoteTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			"fenced code",
			"```md\n[^n]: fake fenced definition\n```\n\nreal[^n]\n\n[^n]: real note\n",
		},
		{
			"indented code",
			"    [^n]: fake indented definition\n\nreal[^n]\n\n[^n]: real note\n",
		},
		{
			"inline code",
			"`[^n]: fake inline definition`\n\nreal[^n]\n\n[^n]: real note\n",
		},
		{
			"escaped marker",
			"\\[^n]: fake escaped definition\n\nreal[^n]\n\n[^n]: real note\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markers, ok := sourceFootnoteMarkers(tc.source)
			if !ok {
				t.Fatal("definition-like literal disabled semantic footnote mapping")
			}
			refs, defs := 0, 0
			for _, marker := range markers {
				if marker.reference {
					refs++
				}
				if marker.definition {
					defs++
				}
			}
			if refs != 1 || defs != 1 {
				t.Fatalf("semantic refs/defs=%d/%d, want 1/1; markers=%#v", refs, defs, markers)
			}

			out, err := Render(tc.source, 60)
			if err != nil {
				t.Fatal(err)
			}
			targets := renderedTargets(tc.source, strings.Split(out, "\n"), splitStrip(out))
			if countFootnoteTargets(targets) != 1 {
				t.Fatalf("valid footnote is not navigable: targets=%#v render=%q", targets, ansi.Strip(out))
			}
			for _, target := range targets {
				if target.kind == targetFootnote && !strings.Contains(splitStrip(out)[target.definition.line], "real note") {
					t.Fatalf("target mapped to fake definition: %#v render=%q", target, ansi.Strip(out))
				}
			}
		})
	}
}

func TestRawHTMLDoesNotDisableFootnoteTargets(t *testing.T) {
	for _, src := range []string{
		"<span title=\"[^n]\">x</span>\nreal[^n]\n\n[^n]: note body\n",
		"<div data-note=\"[^n]\">\ncontent\n</div>\n\nreal[^n]\n\n[^n]: note body\n",
	} {
		out, err := Render(src, 60)
		if err != nil {
			t.Fatal(err)
		}
		base := strings.Split(out, "\n")
		if targets := renderedTargets(src, base, splitStrip(out)); countFootnoteTargets(targets) != 1 {
			t.Fatalf("real footnote lost beside raw HTML: targets=%#v render=%q", targets, ansi.Strip(out))
		}
	}
}

func TestParseFootnoteTargetsMapsRenderedMarkersMonotonically(t *testing.T) {
	src := "# Notes\n\n`[^1]` and \\[^1] then 界界[^1] plus 👨‍👩‍👧‍👦[^named] and again[^1].\n\n" +
		"[^1]: first definition\n    second line\n[^named]: named definition\n"
	out, err := Render(src, 40)
	if err != nil {
		t.Fatal(err)
	}
	stripped := splitStrip(out)
	targets := parseFootnoteTargets(src, stripped)
	if len(targets) != 3 {
		t.Fatalf("targets=%#v\nrender=%q", targets, ansi.Strip(out))
	}
	var numeric []linkTarget
	for _, target := range targets {
		if target.kind != targetFootnote || len(target.regions) != 1 {
			t.Fatalf("invalid footnote target %#v", target)
		}
		if target.footnote == "1" {
			numeric = append(numeric, target)
		}
	}
	if len(numeric) != 2 {
		t.Fatalf("numeric refs=%#v", numeric)
	}
	for _, target := range numeric[1:] {
		if target.definition != numeric[0].definition {
			t.Fatalf("references must share one definition: %#v", numeric)
		}
	}
	first := targets[0].regions[0]
	line := stripped[first.line]
	if got := ansi.StringWidth(line[:strings.Index(line, "[^1]")]); first.start <= got {
		t.Fatalf("first semantic ref mapped to code/escaped marker: start=%d early=%d line=%q", first.start, got, line)
	}
}

func TestFootnoteHintJumpAndBack(t *testing.T) {
	src := "# Top\n\n" + strings.Repeat("intro paragraph\n\n", 8) +
		"reference[^note] tail\n\n" + strings.Repeat("middle paragraph\n\n", 12) +
		"[^note]: definition body\n"
	m := newRenderedModel(t, src, 60, 10)
	for i, line := range m.stripped {
		if strings.Contains(line, "reference[^note]") {
			m.vp.SetYOffset(i)
			break
		}
	}
	before := m.currentLocation()
	base := append([]string(nil), m.base...)
	press(m, "t")
	if !m.targets.active || len(m.targets.targets) != 1 || !strings.Contains(m.hintStrip(), "footnote:note") {
		t.Fatalf("footnote hint missing: active=%v targets=%#v strip=%q", m.targets.active, m.targets.targets, m.hintStrip())
	}
	cmd := pressKey(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd != nil || m.targets.active || len(m.locations) != 1 {
		t.Fatalf("internal activation state: cmd=%v active=%v history=%d", cmd, m.targets.active, len(m.locations))
	}
	visible := strings.Join(m.stripped[m.vp.YOffset():min(len(m.stripped), m.vp.YOffset()+m.vp.Height())], "\n")
	if !strings.Contains(visible, "[^note]:") {
		t.Fatalf("jump did not reveal definition: y=%d view=%q", m.vp.YOffset(), visible)
	}
	for i := range base {
		if base[i] != m.base[i] {
			t.Fatal("footnote hints mutated cached ANSI")
		}
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.vp.YOffset() != before.y || m.vp.XOffset() != before.x || len(m.locations) != 0 {
		t.Fatalf("back did not restore location: got x/y=%d/%d want %d/%d history=%d", m.vp.XOffset(), m.vp.YOffset(), before.x, before.y, len(m.locations))
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.vp.YOffset() != before.y || m.vp.XOffset() != before.x {
		t.Fatal("empty history backspace must be harmless")
	}
}

func TestFootnoteReaderJumpRestoresMarginAndOffset(t *testing.T) {
	src := strings.Repeat("界", 70) + "[^1]\n\n" + strings.Repeat("filler\n\n", 10) + "[^1]: note body\n"
	m := newRenderedModel(t, src, 140, 16)
	settle(t, m, press(m, "r"))
	m.vp.SetXOffset(30)
	before := m.currentLocation()
	press(m, "t")
	if len(m.targets.targets) != 1 {
		t.Fatalf("panned reader must expose footnote: %#v", m.targets.targets)
	}
	pressKey(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !m.reader || m.vp.Width() != 120 || m.vp.XOffset() >= before.x {
		t.Fatalf("definition jump must retain reader and reveal marker: reader=%v width=%d x=%d", m.reader, m.vp.Width(), m.vp.XOffset())
	}
	for _, line := range strings.Split(bodyOf(m), "\n") {
		if strings.Contains(line, "[^1]:") && !strings.HasPrefix(ansi.Strip(line), strings.Repeat(" ", 10)) {
			t.Fatalf("reader definition lost display margin: %q", ansi.Strip(line))
		}
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !m.reader || m.vp.XOffset() != before.x || m.vp.YOffset() != before.y {
		t.Fatalf("reader return mismatch: reader=%v x/y=%d/%d want %d/%d", m.reader, m.vp.XOffset(), m.vp.YOffset(), before.x, before.y)
	}
}

func TestFootnoteLocationStackOrderAndStateRestore(t *testing.T) {
	m := newHintModel(t, 140, 16, make([]string, 80))
	m.reader = true
	m.syncVPWidth()
	m.vp.SetYOffset(3)
	m.vp.SetXOffset(9)
	first := m.currentLocation()
	m.jumpFootnote(linkTarget{kind: targetFootnote, footnote: "one", definition: targetRegion{line: 25, start: 2, end: 6}})
	second := m.currentLocation()
	m.jumpFootnote(linkTarget{kind: targetFootnote, footnote: "two", definition: targetRegion{line: 60, start: 4, end: 8}})
	if len(m.locations) != 2 {
		t.Fatalf("history=%#v", m.locations)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.vp.YOffset() != second.y || m.vp.XOffset() != second.x {
		t.Fatalf("first pop got x/y=%d/%d want %d/%d", m.vp.XOffset(), m.vp.YOffset(), second.x, second.y)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !m.reader || m.vp.YOffset() != first.y || m.vp.XOffset() != first.x {
		t.Fatalf("second pop reader=%v x/y=%d/%d want %d/%d", m.reader, m.vp.XOffset(), m.vp.YOffset(), first.x, first.y)
	}
}

func TestFootnoteBackRestoresRepresentation(t *testing.T) {
	src := "ref[^1]\n\n| A | B |\n| - | - |\n| x | y |\n\n[^1]: note body\n"
	m := newRenderedModel(t, src, 140, 16)
	settle(t, m, press(m, "r"))
	original := m.currentLocation()
	m.jumpFootnote(linkTarget{kind: targetFootnote, footnote: "1", definition: targetRegion{line: len(m.stripped) - 2, start: 2, end: 6}})
	settle(t, m, press(m, "T"))
	press(m, "s")
	cmd := pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if cmd == nil {
		t.Fatal("restoring rendered representation must re-render")
	}
	settle(t, m, cmd)
	if m.srcView || m.collapsed || !m.reader || m.vp.YOffset() != original.y || m.vp.XOffset() != original.x {
		t.Fatalf("restored state source=%v table=%v reader=%v x/y=%d/%d want %d/%d", m.srcView, m.collapsed, m.reader, m.vp.XOffset(), m.vp.YOffset(), original.x, original.y)
	}
}

func TestFootnoteTargetsRecomputeAcrossTransformAndSource(t *testing.T) {
	src := "ref[^1]\n\n| A | B |\n| - | - |\n| x | y |\n\n[^1]: note body\n"
	m := newRenderedModel(t, src, 60, 12)
	if got := countFootnoteTargets(m.links); got != 1 {
		markers, ok := sourceFootnoteMarkers(src)
		t.Fatalf("initial footnotes=%d markers=%#v ok=%v rendered=%q", got, markers, ok, m.stripped)
	}
	settle(t, m, press(m, "T"))
	if got := countFootnoteTargets(m.links); got != 1 {
		t.Fatalf("collapsed footnotes=%d", got)
	}
	press(m, "s")
	if m.srcView && len(m.links) != 0 {
		t.Fatalf("source view must invalidate targets: %#v", m.links)
	}
	press(m, "t")
	if m.targets.active || m.flash != "targets unavailable" {
		t.Fatalf("source target mode active=%v flash=%q", m.targets.active, m.flash)
	}
	settle(t, m, press(m, "s"))
	if got := countFootnoteTargets(m.links); got != 1 {
		t.Fatalf("restored footnotes=%d", got)
	}
	if err := m.SetStyle("notty"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, m.requestRender())
	if got := countFootnoteTargets(m.links); got != 1 {
		t.Fatalf("restyled footnotes=%d", got)
	}
}

func TestFootnoteReloadInvalidatesHistoryAndMetadata(t *testing.T) {
	m := newRenderedModel(t, "old[^1]\n\n[^1]: old note\n", 60, 12)
	m.locations = append(m.locations, m.currentLocation())
	cmd := m.applyReload(reloadDoneMsg{body: []byte("new[^2]\n\n[^2]: new note\n")})
	if len(m.locations) != 0 || m.pendingLocation != nil {
		t.Fatalf("reload retained location history: %#v pending=%#v", m.locations, m.pendingLocation)
	}
	settle(t, m, cmd)
	if len(m.links) != 1 || m.links[0].kind != targetFootnote || m.links[0].footnote != "2" {
		t.Fatalf("reload did not replace footnote metadata: %#v", m.links)
	}
}

func TestFootnoteAmbiguousRenderedMappingDeclines(t *testing.T) {
	src := "ref[^1]\n\n[^1]: note\n"
	out, err := Render(src, 40)
	if err != nil {
		t.Fatal(err)
	}
	base, stripped := strings.Split(out, "\n"), splitStrip(out)
	if targets := renderedTargets(src, base, stripped); len(targets) != 0 {
		t.Fatalf("one-token link-definition ambiguity must decline: %#v", targets)
	}
	linkOut, err := Render("[guide](guide.md)\n", 40)
	if err != nil {
		t.Fatal(err)
	}
	if targets := renderedTargets("[guide](guide.md)\n", strings.Split(linkOut, "\n"), splitStrip(linkOut)); len(targets) != 1 || targets[0].dest != "guide.md" {
		t.Fatalf("ordinary relative links must remain visible deferred targets: %#v", targets)
	}
}

func countFootnoteTargets(targets []linkTarget) int {
	n := 0
	for _, target := range targets {
		if target.kind == targetFootnote {
			n++
		}
	}
	return n
}

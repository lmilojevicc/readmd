package pager

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

func osc8(params, dest, text, term string) string {
	return "\x1b]8;" + params + ";" + dest + term + text + "\x1b]8;;" + term
}

func TestParseLinkTargets(t *testing.T) {
	st, bel := "\x1b\\", "\a"
	family := "👨‍👩‍👧‍👦"
	for _, tc := range []struct {
		name  string
		lines []string
		want  []linkTarget
	}{
		{
			"ST terminator",
			[]string{"x" + osc8("id=one", "https://one.example", "link", st)},
			[]linkTarget{{id: "one", dest: "https://one.example", regions: []targetRegion{{0, 1, 5}}}},
		},
		{
			"BEL and colon params",
			[]string{osc8("foo=bar:id=mail", "mailto:a@example.com", "mail", bel)},
			[]linkTarget{{id: "mail", dest: "mailto:a@example.com", regions: []targetRegion{{0, 0, 4}}}},
		},
		{
			"C1 OSC and ST",
			[]string{"\x9d8;id=c1;https://c1.example\x9cc1\x9d8;;\x9c"},
			[]linkTarget{{id: "c1", dest: "https://c1.example", regions: []targetRegion{{0, 0, 2}}}},
		},
		{
			"label and printed destination coalesce",
			[]string{osc8("id=same", "https://same.example", "label", st) + " " +
				osc8("id=same", "https://same.example", "https://same.example", st)},
			[]linkTarget{{id: "same", dest: "https://same.example", regions: []targetRegion{{0, 0, 5}, {0, 6, 26}}}},
		},
		{
			"duplicate URL distinct IDs",
			[]string{osc8("id=first", "https://same.example", "one", st) + " / " +
				osc8("id=second", "https://same.example", "two", st)},
			[]linkTarget{
				{id: "first", dest: "https://same.example", regions: []targetRegion{{0, 0, 3}}},
				{id: "second", dest: "https://same.example", regions: []targetRegion{{0, 6, 9}}},
			},
		},
		{
			"SGR nesting",
			[]string{osc8("id=color", "https://color.example", "\x1b[31mred\x1b[0m", st)},
			[]linkTarget{{id: "color", dest: "https://color.example", regions: []targetRegion{{0, 0, 3}}}},
		},
		{
			"grapheme display columns",
			[]string{"a" + osc8("id=wide", "https://wide.example", "界"+family+"e\u0301", st)},
			[]linkTarget{{id: "wide", dest: "https://wide.example", regions: []targetRegion{{0, 1, 1 + ansi.StringWidth("界"+family+"e\u0301")}}}},
		},
		{
			"malformed and unterminated ignored",
			[]string{"\x1b]8;id=x;https://bad.examplebroken", "plain"},
			nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLinkTargets(tc.lines)
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if got[i].id != tc.want[i].id || got[i].dest != tc.want[i].dest || len(got[i].regions) != len(tc.want[i].regions) {
					t.Fatalf("got %#v, want %#v", got, tc.want)
				}
				for j := range got[i].regions {
					if got[i].regions[j] != tc.want[i].regions[j] {
						t.Fatalf("got %#v, want %#v", got, tc.want)
					}
				}
			}
		})
	}
}

func TestRenderedLinksParseAsLogicalTargets(t *testing.T) {
	src := "[one](https://example.com) and [two](https://example.com)\n"
	out, err := Render(src, 80)
	if err != nil {
		t.Fatal(err)
	}
	got := parseRenderedLinkTargets(src, strings.Split(out, "\n"))
	if len(got) != 2 {
		t.Fatalf("two source links to the same URL must remain distinct: %#v\nrender=%q", got, out)
	}
	for i, target := range got {
		if target.dest != "https://example.com" || len(target.regions) != 2 {
			t.Fatalf("target %d did not coalesce label and destination: %#v", i, target)
		}
	}
}

func TestRenderedLinkWhoseLabelEqualsDestinationStillCoalesces(t *testing.T) {
	src := "[https://example.com](https://example.com)\n"
	out, err := Render(src, 80)
	if err != nil {
		t.Fatal(err)
	}
	got := parseRenderedLinkTargets(src, strings.Split(out, "\n"))
	if len(got) != 1 || len(got[0].regions) != 2 {
		t.Fatalf("one source link must remain one target: %#v\nrender=%q", got, out)
	}
}

func TestRenderedAdjacentAutolinksRemainDistinct(t *testing.T) {
	for _, tc := range []struct {
		name, source, dest string
	}{
		{"HTTP", "<https://example.com> <https://example.com>\n", "https://example.com"},
		{"email", "<a@example.com> <a@example.com>\n", "mailto:a@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Render(tc.source, 80)
			if err != nil {
				t.Fatal(err)
			}
			got := parseRenderedLinkTargets(tc.source, strings.Split(out, "\n"))
			if len(got) != 2 {
				t.Fatalf("adjacent autolinks must remain distinct: %#v\nrender=%q", got, out)
			}
			for i, target := range got {
				if target.dest != tc.dest || len(target.regions) != 1 {
					t.Fatalf("target %d = %#v, want one %q region", i, target, tc.dest)
				}
			}
		})
	}
}

func TestVisibleLinkTargetsClipsViewport(t *testing.T) {
	links := []linkTarget{
		{id: "a", dest: "https://a", regions: []targetRegion{{0, 0, 5}}},
		{id: "b", dest: "https://b", regions: []targetRegion{{2, 8, 14}, {8, 0, 3}}},
		{id: "c", dest: "https://c", regions: []targetRegion{{3, 20, 25}}},
	}
	got := visibleLinkTargets(links, 10, 1, 10, 3)
	if len(got) != 1 || got[0].id != "b" || len(got[0].regions) != 1 {
		t.Fatalf("got %#v, want only clipped target b", got)
	}
	if links[1].regions[0] != (targetRegion{2, 8, 14}) {
		t.Fatal("visibility filtering must not mutate cached metadata")
	}
}

func TestHintLabelsUseOneFixedWidth(t *testing.T) {
	for _, tc := range []struct {
		name      string
		n         int
		wantFirst string
		wantLast  string
		width     int
	}{
		{"one character", len(hintAlphabet), "A", "M", 1},
		{"two characters", len(hintAlphabet) + 1, "AA", "SA", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := hintLabels(tc.n)
			if len(got) != tc.n || got[0] != tc.wantFirst || got[len(got)-1] != tc.wantLast {
				t.Fatalf("got %v", got)
			}
			for _, label := range got {
				if len(label) != tc.width {
					t.Fatalf("mixed label widths in %v", got)
				}
			}
		})
	}
}

func newHintModel(t *testing.T, width, height int, lines []string) *Model {
	t.Helper()
	m := New("", "links.md")
	m.width, m.height = width, height
	m.vp.SetWidth(width)
	m.vp.SetHeight(height - 1)
	m.syncView(lines, splitStrip(strings.Join(lines, "\n")), nil, parseLinkTargets(lines))
	return m
}

func linkLine(id, dest, text string) string {
	return osc8("id="+id, dest, text, "\x1b\\")
}

func TestTargetModeEntryKey(t *testing.T) {
	for _, tc := range []struct {
		key  string
		open bool
	}{
		{"p", true},
		{"t", false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m := newHintModel(t, 60, 12, []string{linkLine("one", "https://one.example", "link")})
			before, height, location := m.View().Content, m.vp.Height(), m.currentLocation()
			if cmd := press(m, tc.key); cmd != nil {
				t.Fatal("entry key must not return a command")
			}
			if m.targets.active != tc.open || m.currentLocation() != location || m.flash != "" || m.errMsg != "" {
				t.Fatalf("key %q: active=%v location=%#v flash=%q error=%q", tc.key, m.targets.active, m.currentLocation(), m.flash, m.errMsg)
			}
			if tc.open {
				if len(m.targets.targets) != 1 || m.vp.Height() != height-1 {
					t.Fatal("p must open the visible target and reserve the hint strip")
				}
			} else if m.View().Content != before || m.vp.Height() != height || len(m.targets.targets) != 0 || m.helpOpen || m.tocOpen || m.search.active {
				t.Fatal("t must be a normal-mode no-op, not an alias")
			}
		})
	}
}

func TestTargetModePromptAndOverlaySuppression(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
	}{
		{"search", []string{"/"}},
		{"help filter", []string{"?", "/"}},
		{"outline filter", []string{"o", "/"}},
		{"help", []string{"?"}},
		{"outline", []string{"o"}},
	} {
		for _, key := range []string{"p", "t"} {
			t.Run(tc.name+"/"+key, func(t *testing.T) {
				m := newRenderedModel(t, "# Topic\n\n[link](https://one.example)\n", 60, 12)
				for _, entry := range tc.keys {
					press(m, entry)
				}
				location := m.currentLocation()
				if cmd := press(m, key); cmd != nil || m.targets.active || m.currentLocation() != location {
					t.Fatal("prompt/overlay must consume the key without opening hints or moving the document")
				}
				switch tc.name {
				case "search":
					if !m.search.active || m.search.query != key {
						t.Fatal("search must retain literal input")
					}
				case "help filter":
					if !m.helpOpen || !m.helpPrompt || m.helpFilter != key {
						t.Fatal("help filter must retain literal input")
					}
				case "outline filter":
					if !m.tocOpen || !m.tocPrompt || m.tocFilter != key {
						t.Fatal("outline filter must retain literal input")
					}
				case "help":
					if m.helpOpen {
						t.Fatal("help must close without forwarding the key")
					}
				case "outline":
					if !m.tocOpen {
						t.Fatal("outline must remain open")
					}
				}
			})
		}
	}
}

func TestTargetModeEntryKeysRemainHintLabels(t *testing.T) {
	for _, key := range []string{"p", "t"} {
		t.Run(key, func(t *testing.T) {
			var lines []string
			for i := range len(hintAlphabet) {
				lines = append(lines, linkLine(fmt.Sprint(i), fmt.Sprintf("https://example.com/%d", i), "link"))
			}
			m := newHintModel(t, 60, 40, lines)
			var opened string
			m.openURL = func(dest string) error { opened = dest; return nil }
			press(m, "p")
			index := strings.Index(hintAlphabet, key)
			if !m.targets.active || m.targets.targets[index].label != strings.ToUpper(key) {
				t.Fatal("precondition: original hint alphabet must supply the label")
			}
			cmd := press(m, key)
			if cmd == nil || m.targets.active {
				t.Fatal("p/t must activate their labels, not reopen or ignore hints")
			}
			settle(t, m, cmd)
			if want := fmt.Sprintf("https://example.com/%d", index); opened != want {
				t.Fatalf("opened=%q want=%q", opened, want)
			}
		})
	}
}

func TestTargetModeFreezesViewportAndRestoresHeight(t *testing.T) {
	lines := []string{linkLine("one", "https://one.example", "one")}
	for range 20 {
		lines = append(lines, "filler")
	}
	m := newHintModel(t, 40, 10, lines)
	m.vp.SetYOffset(0)
	savedHeight, savedX, savedY := m.vp.Height(), m.vp.XOffset(), m.vp.YOffset()
	press(m, "p")
	if !m.targets.active || m.vp.Height() != savedHeight-1 {
		t.Fatalf("target mode state=%v height=%d", m.targets.active, m.vp.Height())
	}
	before := append([]string(nil), m.base...)
	if !strings.Contains(m.vp.GetContent(), targetHL) {
		t.Fatalf("target token lacks display-only highlight: %q", m.vp.GetContent())
	}
	viewLines := strings.Split(m.View().Content, "\n")
	if len(viewLines) != m.height || !strings.Contains(viewLines[len(viewLines)-2], "A one.example") || !strings.Contains(viewLines[len(viewLines)-1], "links.md") {
		t.Fatalf("hint strip must sit directly above status without growing the view: %#v", viewLines)
	}
	press(m, "j")
	press(m, "l")
	if m.vp.XOffset() != savedX || m.vp.YOffset() != savedY || m.vp.Height() != savedHeight-1 {
		t.Fatal("navigation keys moved the frozen target viewport")
	}
	for i := range before {
		if before[i] != m.base[i] {
			t.Fatal("target highlighting mutated cached ANSI")
		}
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.targets.active || m.vp.Height() != savedHeight || m.vp.XOffset() != savedX || m.vp.YOffset() != savedY {
		t.Fatal("escape did not restore the exact viewport")
	}
	if strings.Contains(m.vp.GetContent(), targetHL) {
		t.Fatal("target highlight survived cancellation")
	}
}

func TestTargetPrefixBackspaceAndActivation(t *testing.T) {
	var lines []string
	for i := 0; i < len(hintAlphabet)+1; i++ {
		lines = append(lines, linkLine(string(rune('a'+i%20))+string(rune('A'+i)), "https://example.com/"+string(rune('a'+i)), "link"))
	}
	m := newHintModel(t, 200, 40, lines)
	press(m, "p")
	if len(m.targets.targets) != len(hintAlphabet)+1 || len(m.targets.targets[0].label) != 2 {
		t.Fatalf("precondition: hints=%d first=%q", len(m.targets.targets), m.targets.targets[0].label)
	}
	pressKey(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !m.targets.active || m.targets.prefix != "a" || len(m.targetCandidates()) <= 1 {
		t.Fatalf("first prefix must filter without activation: prefix=%q candidates=%d", m.targets.prefix, len(m.targetCandidates()))
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.targets.prefix != "" || len(m.targetCandidates()) != len(hintAlphabet)+1 {
		t.Fatal("backspace did not restore the frozen candidate set")
	}

	var opened string
	m.openURL = func(dest string) error { opened = dest; return nil }
	cmd := pressKey(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd == nil || m.targets.active {
		t.Fatal("a unique prefix must auto-activate and close target mode")
	}
	msg := cmd()
	nm, _ := m.Update(msg)
	*m = *nm.(*Model)
	if opened == "" || !strings.Contains(m.flash, "opened") {
		t.Fatalf("stub opener not called: opened=%q flash=%q", opened, m.flash)
	}
}

func TestTargetModeVisibilityReaderAndOffsets(t *testing.T) {
	lines := []string{
		linkLine("left", "https://left.example", "left"),
		strings.Repeat("x", 130) + linkLine("right", "https://right.example", "right"),
		linkLine("below", "https://below.example", "below"),
	}
	m := newHintModel(t, 140, 10, lines)
	m.reader = true
	m.syncVPWidth()
	m.vp.SetXOffset(20)
	m.vp.SetYOffset(1)
	press(m, "p")
	if len(m.targets.targets) != 1 {
		t.Fatalf("reader viewport should expose only the panned right target, got %#v", m.targets.targets)
	}
	for _, target := range m.targets.targets {
		if target.id == "left" {
			t.Fatal("off-screen left target entered frozen reader hints")
		}
	}
	found := false
	for _, line := range strings.Split(m.View().Content, "\n") {
		if !strings.Contains(line, targetHL) {
			continue
		}
		found = true
		if plain := ansi.Strip(line); !strings.HasPrefix(plain, strings.Repeat(" ", 10)) {
			t.Fatalf("reader target highlight lost display-only margin: %q", plain)
		}
	}
	if !found {
		t.Fatal("reader target highlight is not visible")
	}
}

func TestTargetModeSuppressionAndSearchComposition(t *testing.T) {
	line := "needle " + linkLine("one", "https://one.example", "link")
	m := newHintModel(t, 60, 12, []string{line})
	m.search.query = "needle"
	m.refreshSearch()
	press(m, "p")
	content := m.vp.GetContent()
	if !strings.Contains(content, curHL) || !strings.Contains(content, targetHL) {
		t.Fatalf("search and target highlights must compose: %q", content)
	}
	pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m.search.active = true
	press(m, "p")
	if m.targets.active || m.search.query != "needlep" {
		t.Fatal("active search must capture p instead of opening targets")
	}
	m.search.active = false
	m.tocOpen = true
	press(m, "p")
	if m.targets.active {
		t.Fatal("TOC must suppress target mode")
	}
	m.tocOpen = false
	m.helpOpen = true
	press(m, "p")
	if m.targets.active {
		t.Fatal("help must suppress target mode")
	}
}

func TestTargetModeUnavailableStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{"no links", func(*Model) {}, "no visible targets"},
		{"source view", func(m *Model) { m.srcView = true }, "targets unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHintModel(t, 60, 12, []string{"plain text"})
			tc.setup(m)
			press(m, "p")
			if m.targets.active || m.flash != tc.want {
				t.Fatalf("active=%v flash=%q want=%q", m.targets.active, m.flash, tc.want)
			}
		})
	}
}

func TestTargetReloadInvalidatesFrozenMetadata(t *testing.T) {
	m := newHintModel(t, 60, 12, []string{linkLine("old", "https://old.example", "old")})
	m.source = "[old](https://old.example)\n"
	opened := false
	m.openURL = func(string) error { opened = true; return nil }
	press(m, "p")
	if !m.targets.active || len(m.links) != 1 {
		t.Fatal("precondition: old target mode must be active")
	}

	nm, render := m.Update(reloadDoneMsg{body: []byte("[new](https://new.example)\n")})
	*m = *nm.(*Model)
	if render == nil || m.targets.active || len(m.links) != 0 {
		t.Fatalf("accepted reload must synchronously invalidate targets: render=%v active=%v links=%#v", render, m.targets.active, m.links)
	}
	if cmd := pressKey(m, tea.KeyPressMsg{Code: 'a', Text: "a"}); cmd != nil || opened {
		t.Fatalf("old hint key activated during render: cmd=%v opened=%v", cmd, opened)
	}
	if cmd := sendMouseClick(m, 0, 0); cmd != nil || opened {
		t.Fatalf("old click activated during render: cmd=%v opened=%v", cmd, opened)
	}

	settle(t, m, render)
	if len(m.links) != 1 || m.links[0].dest != "https://new.example" {
		t.Fatalf("link metadata was not recomputed: %#v", m.links)
	}
}

func TestExternalURLValidation(t *testing.T) {
	for _, tc := range []struct {
		dest string
		ok   bool
	}{
		{"https://example.com/path?q=x", true},
		{"http://example.com", true},
		{"mailto:a@example.com", true},
		{"https:///missing-host", false},
		{"mailto:", false},
		{"javascript:alert(1)", false},
		{"file:///tmp/a", false},
		{"relative.md", false},
		{"#footnote", false},
		{"https://example.com/\nboom", false},
	} {
		t.Run(tc.dest, func(t *testing.T) {
			err := validateExternalURL(tc.dest)
			if (err == nil) != tc.ok {
				t.Fatalf("validateExternalURL(%q) err=%v, want ok=%v", tc.dest, err, tc.ok)
			}
		})
	}
}

func TestExternalURLCommandUsesOneArgument(t *testing.T) {
	dest := "https://example.com/a;echo-owned?x=$(bad)"
	for _, tc := range []struct {
		goos string
		want []string
	}{
		{"darwin", []string{"open", dest}},
		{"linux", []string{"xdg-open", dest}},
		{"windows", []string{"rundll32", "url.dll,FileProtocolHandler", dest}},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			got := externalURLCmd(tc.goos, dest).Args
			if len(got) != len(tc.want) {
				t.Fatalf("args=%q want=%q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("args=%q want=%q", got, tc.want)
				}
			}
		})
	}
}

func TestTargetActivationReportsOpenerFailure(t *testing.T) {
	m := newHintModel(t, 60, 12, []string{linkLine("x", "https://example.com", "x")})
	m.openURL = func(string) error { return errors.New("boom") }
	press(m, "p")
	cmd := pressKey(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("valid external URL must invoke the opener command")
	}
	nm, _ := m.Update(cmd())
	*m = *nm.(*Model)
	if m.flash != "could not open link" {
		t.Fatalf("flash=%q", m.flash)
	}
}

func TestTargetActivationRefusesDeferredDestinations(t *testing.T) {
	for _, tc := range []struct {
		dest string
		want string
	}{
		{"relative.md", "file navigation not implemented"},
		{"file:///tmp/a", "file navigation not implemented"},
		{"#footnote", "internal link navigation not implemented"},
		{"javascript:alert(1)", "unsupported link"},
	} {
		t.Run(tc.dest, func(t *testing.T) {
			m := newHintModel(t, 60, 12, []string{linkLine("x", tc.dest, "x")})
			called := false
			m.openURL = func(string) error { called = true; return errors.New("must not run") }
			press(m, "p")
			cmd := pressKey(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
			if cmd != nil || called || m.flash != tc.want {
				t.Fatalf("cmd=%v called=%v flash=%q want=%q", cmd, called, m.flash, tc.want)
			}
		})
	}
}

func TestRenderedLinkGroupingAfterTable(t *testing.T) {
	for _, tc := range []struct {
		name, prose string
		want        int
	}{
		{"URL suffixed label", "[visit https://example.org](https://example.org)", 1},
		{"URL label", "[https://example.org](https://example.org)", 1},
		{"normal label", "[visit](https://example.org)", 1},
		{"adjacent repeated URL labels", "[https://example.org](https://example.org) [https://example.org](https://example.org)", 2},
		{"adjacent autolinks", "<https://example.org> <https://example.org>", 2},
		{"wrapped destination", "[visit https://example.org](https://example.org/long/path/with/many/segments/and/a/query?one=two)", 1},
	} {
		for _, width := range []int{40, 120} {
			t.Run(fmt.Sprintf("%s/w%d", tc.name, width), func(t *testing.T) {
				src := "| A |\n| - |\n| [x](https://table.example) |\n\n" + tc.prose + "\n"
				out, err := Render(src, width)
				if err != nil {
					t.Fatal(err)
				}
				targets := parseRenderedLinkTargets(src, strings.Split(out, "\n"))
				prose, table := 0, 0
				for _, target := range targets {
					if target.dest == "https://table.example" {
						table++
					} else {
						prose++
					}
				}
				if prose != tc.want || table != 1 {
					t.Fatalf("prose=%d want=%d table=%d targets=%#v render=%q", prose, tc.want, table, targets, splitStrip(out))
				}
			})
		}
	}
}

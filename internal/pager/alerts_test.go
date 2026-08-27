package pager

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAlertIconsSingleCell(t *testing.T) {
	for _, k := range alertKinds {
		if w := ansi.StringWidth(k.icon); w != 1 {
			t.Errorf("icon %q for %s: width %d, want 1", k.icon, k.name, w)
		}
	}
}

func TestPlainBarTokenMatchesRailSeq(t *testing.T) {
	if got := barToken(quoteBarSGR); got != quoteBarToken {
		t.Fatalf("barToken(%s) = %q, palette IndentToken = %q", quoteBarSGR, got, quoteBarToken)
	}
	if got := railSeq(quoteBarSGR); got != "  "+quoteBarToken {
		t.Fatalf("railSeq(%s) = %q, want margin + token", quoteBarSGR, got)
	}
}

func TestAlertGoldens(t *testing.T) {
	for _, k := range alertKinds {
		t.Run(k.name, func(t *testing.T) {
			src := "> [!" + strings.ToUpper(k.name) + "]\n> body one\n> body two\n"
			out, _, _, err := renderDoc(imgCtx{}, src, 40, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			title := railSeq(k.sgr) + "\x1b[" + k.sgr + ";1m" + k.icon + " " + k.title + "\x1b[m"
			lines := strings.Split(out, "\n")
			titles := 0
			rails := 0
			for _, l := range lines {
				switch {
				case l == title:
					titles++
				case strings.Contains(l, railSeq(k.sgr)):
					rails++
				}
			}
			if titles != 1 || rails < 1 {
				t.Fatalf("titles=%d rails=%d, want 1/>=1\n%q", titles, rails, out)
			}
			if k.sgr != quoteBarSGR && strings.Contains(out, quoteBarToken) {
				t.Error("plain magenta bar survived inside alert region")
			}
			if strings.Contains(out, "\x1b[36m") {
				t.Error("alert body carries old cyan text color")
			}
			s := ansi.Strip(out)
			if !strings.Contains(s, "body one") || !strings.Contains(s, "body two") {
				t.Errorf("alert body lost: %q", s)
			}
			if strings.Contains(strings.ToLower(s), "[!"+k.name+"]") {
				t.Errorf("raw marker survived: %q", s)
			}
			if strings.Contains(s, "readmd-alert-") {
				t.Errorf("sentinel leaked: %q", s)
			}
		})
	}
}

func TestAlertAcceptedVariants(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"lowercase", "> [!note]\n> b\n"},
		{"trailing space", "> [!NOTE] \n> b\n"},
		{"mixed case", "> [!WaRnInG]\n> b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, _, err := renderDoc(imgCtx{}, tc.src, 40, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			s := ansi.Strip(out)
			if strings.Contains(s, "readmd-alert-") {
				t.Fatalf("sentinel leaked: %q", s)
			}
			if !styledTitlePresent(out) {
				t.Errorf("expected styled alert title in %q", s)
			}
		})
	}
}

func styledTitlePresent(out string) bool {
	for _, k := range alertKinds {
		title := railSeq(k.sgr) + "\x1b[" + k.sgr + ";1m" + k.icon + " " + k.title + "\x1b[m"
		if strings.Contains(out, title) {
			return true
		}
	}
	return false
}

func TestAlertMultiBlockBody(t *testing.T) {
	src := "> [!TIP]\n> intro para\n>\n> - x\n> - y\n"
	out, _, _, err := renderDoc(imgCtx{}, src, 40, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	s := ansi.Strip(out)
	for _, want := range []string{"✦ Tip", "intro para", "• x", "• y"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	if n := strings.Count(out, railSeq("92")); n < 3 {
		t.Errorf("type-colored rails = %d, want >= 3\n%q", n, out)
	}
}

func TestAlertTitleOnlyFirstLineStyled(t *testing.T) {
	src := "> [!CAUTION]\n> a\n>\n> b\n"
	out, _, _, err := renderDoc(imgCtx{}, src, 40, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "\x1b[91;1m"); n != 1 {
		t.Errorf("bold SGR count = %d, want 1 (title only)\n%q", n, out)
	}
}

func TestPlainQuoteMagentaBar(t *testing.T) {
	out, _, _, err := renderDoc(imgCtx{}, "> hello\n> world\n", 40, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	first := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(ansi.Strip(l)) != "" {
			first = l
			break
		}
	}
	if first != railSeq(quoteBarSGR)+"hello world" {
		t.Errorf("plain quote line = %q, want %q", first, railSeq(quoteBarSGR)+"hello world")
	}
	if strings.Contains(out, "\x1b[36m") {
		t.Error("old cyan blockquote color still present")
	}
}

func TestAlertDeclines(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"unknown type", "> [!FOO]\n> body\n"},
		{"prose same line", "> [!NOTE] prose\n> body\n"},
		{"in list", "- > [!NOTE]\n  > body\n"},
		{"nested quote", "> outer\n> > [!NOTE]\n> > inner\n"},
		{"fence in quote", "> [!NOTE]\n> ```go\n> x := 1\n> ```\n"},
		{"table in quote", "> [!NOTE]\n> | A |\n> | - |\n> | b |\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _, err := renderStyled(imgCtx{}, tc.src, paletteStyleName, true)
			if err != nil {
				t.Fatal(err)
			}
			want, _, _, err := renderStyled(imgCtx{}, tc.src, paletteStyleName, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("declined input must render pristine:\ngot  %q\nwant %q", got, want)
			}
			if strings.Contains(got, "readmd-alert-") {
				t.Errorf("sentinel leaked: %q", got)
			}
			if styledTitlePresent(got) {
				t.Errorf("declined input styled as alert: %q", got)
			}
		})
	}
}

func TestSpliceAlertsAnomaliesBail(t *testing.T) {
	a := alert{startTok: "T0s", endTok: "T0e", name: "note", icon: "ⓘ", title: "Note", sgr: "94"}
	for _, tc := range []struct {
		name string
		out  string
	}{
		{"duplicate start", "a\nT0s\nb\nT0s\nc\nT0e\nd\n"},
		{"duplicate end", "a\nT0s\nb\nT0e\nc\nT0e\nd\n"},
		{"missing end", "a\nT0s\nb\n"},
		{"reversed", "a\nT0e\nb\nT0s\nc\n"},
		{"leak fragment", "a\nT0s\nb\nreadmd-alert-zz-9x\nc\nT0e\nd\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := spliceAlerts(tc.out, []alert{a})
			if ok {
				t.Errorf("expected bail for %q", tc.out)
			}
			if got != tc.out {
				t.Errorf("bail must pass output through unchanged:\ngot  %q\nwant %q", got, tc.out)
			}
		})
	}
}

func TestAlertNarrowWidthKeepsStyling(t *testing.T) {
	src := "> [!NOTE]\n> body\n"
	out, _, _, err := renderDoc(imgCtx{}, src, 12, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	if !styledTitlePresent(out) || !strings.Contains(ansi.Strip(out), "body") {
		t.Fatalf("narrow alert lost styling or content: %q", ansi.Strip(out))
	}
}

func TestAlertDocLiteralMarkerSurvives(t *testing.T) {
	src := "readmd-alert-x-0s fake literal line\n\n> [!NOTE]\n> body\n"
	got, _, _, err := renderDoc(imgCtx{}, src, 60, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	want, _, _, err := renderStyled(imgCtx{}, src, paletteStyleName, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("literal collision must bail to pristine:\ngot  %q\nwant %q", got, want)
	}
	s := ansi.Strip(got)
	if n := strings.Count(s, "readmd-alert-"); n != 1 {
		t.Errorf("literal must survive exactly once, got %d in %q", n, s)
	}
}

func TestAlertNaturalWidth(t *testing.T) {
	src := "> [!WARNING]\n> careful text\n"
	out, _, _, err := renderDoc(imgCtx{}, src, 80, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	if !styledTitlePresent(out) {
		t.Errorf("natural-width render lost alert styling: %q", ansi.Strip(out))
	}
	if strings.Contains(ansi.Strip(out), "readmd-alert-") {
		t.Errorf("sentinel leaked: %q", ansi.Strip(out))
	}
}

func TestAlertBetweenProse(t *testing.T) {
	src := "intro line\n\n> [!NOTE]\n> body\n\noutro line\n"
	for _, w := range []int{40, 80} {
		out, _, _, err := renderDoc(imgCtx{}, src, w, paletteStyleName)
		if err != nil {
			t.Fatal(err)
		}
		s := ansi.Strip(out)
		if !styledTitlePresent(out) {
			t.Errorf("w%d: alert not styled: %q", w, s)
		}
		if strings.Contains(s, "readmd-alert-") {
			t.Errorf("w%d: sentinel leaked/merged: %q", w, s)
		}
		if !strings.Contains(s, "intro line") || !strings.Contains(s, "outro line") {
			t.Errorf("w%d: prose lost: %q", w, s)
		}
	}
}

func TestAlertMultipleInDoc(t *testing.T) {
	src := "> [!NOTE]\n> one\n\n> [!TIP]\n> two\n\nplain tail\n"
	out, _, _, err := renderDoc(imgCtx{}, src, 60, paletteStyleName)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		railSeq("94") + "\x1b[94;1mⓘ Note\x1b[m",
		railSeq("92") + "\x1b[92;1m✦ Tip\x1b[m",
	} {
		if n := strings.Count(out, want); n != 1 {
			t.Errorf("title %q count=%d, want 1\n%q", ansi.Strip(want), n, ansi.Strip(out))
		}
	}
	if strings.Contains(ansi.Strip(out), "readmd-alert-") {
		t.Errorf("sentinel leaked: %q", ansi.Strip(out))
	}
}

// Alert sentinels must remain intact at narrow viewport widths.
func TestAlertMinWidth(t *testing.T) {
	for _, k := range alertKinds {
		t.Run(k.name, func(t *testing.T) {
			src := "> [!" + strings.ToUpper(k.name) + "]\n> body\n"
			out, _, _, err := renderDoc(imgCtx{}, src, 20, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			if !styledTitlePresent(out) {
				t.Errorf("w20: alert not styled: %q", ansi.Strip(out))
			}
			if strings.Contains(ansi.Strip(out), "readmd-alert-") {
				t.Errorf("w20: sentinel leaked: %q", ansi.Strip(out))
			}
		})
	}
}

// A fence immediately followed by an alert must preserve both blocks at
// narrow viewport widths.
func TestAlertAdjacentFenceNarrowWidths(t *testing.T) {
	src := "```go\nx := 1\n```\n> [!NOTE]\n> body text\n"
	for _, w := range []int{20, 25, 30} {
		t.Run(strconv.Itoa(w), func(t *testing.T) {
			out, _, _, err := renderDoc(imgCtx{}, src, w, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			s := ansi.Strip(out)
			if strings.Contains(s, "readmd-") {
				t.Errorf("w%d: sentinel fragment leaked: %q", w, s)
			}
			if !strings.Contains(s, "x := 1") || !strings.Contains(s, "body") || !strings.Contains(s, "text") {
				t.Errorf("w%d: content lost: %q", w, s)
			}
			if !styledTitlePresent(out) {
				t.Errorf("w%d: alert not styled: %q", w, s)
			}
		})
	}
}

// GFM lazy continuation: bare non-blank lines directly after the quote are
// absorbed into it (prefixed with "> " in markdown space) so they render on
// the rail. A blank line or a new block ends absorption.
func TestAlertLazyContinuation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		src      string
		absorbed string
		outside  string
	}{
		{"tail absorbed", "> [!NOTE]\n> body\nlazy tail\n", "lazy tail", ""},
		{"lazy-only body", "> [!NOTE]\nlazy tail\n", "lazy tail", ""},
		{"blank ends absorption", "> [!NOTE]\n> body\n\nloose para\n", "", "loose para"},
		{"heading ends absorption", "> [!NOTE]\n> body\n# Heading\n", "", "# Heading"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, _, err := renderDoc(imgCtx{}, tc.src, 60, paletteStyleName)
			if err != nil {
				t.Fatal(err)
			}
			s := ansi.Strip(out)
			if !styledTitlePresent(out) {
				t.Errorf("alert not styled: %q", s)
			}
			if strings.Contains(s, "readmd-alert-") {
				t.Errorf("sentinel leaked: %q", s)
			}
			if tc.absorbed != "" {
				railed := false
				for _, l := range strings.Split(s, "\n") {
					if strings.Contains(l, tc.absorbed) && strings.Contains(l, "│") {
						railed = true
					}
				}
				if !railed {
					t.Errorf("%q not rendered on the rail: %q", tc.absorbed, s)
				}
				return
			}
			if strings.Contains(s, "│ "+tc.outside) {
				t.Errorf("non-contiguous block absorbed into quote: %q", s)
			}
			if !strings.Contains(s, tc.outside) {
				t.Errorf("%q lost: %q", tc.outside, s)
			}
		})
	}
}

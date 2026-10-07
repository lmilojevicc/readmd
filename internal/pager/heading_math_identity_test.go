package pager

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHeadingMathIdentityUnsafeLive(t *testing.T) {
	for _, tc := range []struct{ source, title, prefix string }{
		{"$\\quad$\n---\n\n$## \\alpha$\n", "$\\quad$", "## $\\quad$"},
		{"$\\!$\n===\n\n$# \\alpha$\n", "$!$", "# $!$"},
		{"$# \\alpha$\n===\n", "$# \\alpha$", "# $# \\alpha$"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			dest := "#" + url.PathEscape(tc.title)
			source := "[original](" + dest + ")\n\n[Later](#later)\n\n" + tc.source + "\n# Later\n\n$x^2$\n\n" + strings.Repeat("tail\n\n", 20)
			original := extractHeadings(source)
			m := New(source, "identity")
			if err := m.SetStyle("notty"); err != nil {
				t.Fatal(err)
			}
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
			result := cmd().(renderedMsg)
			if result.err != nil {
				t.Fatal(result.err)
			}
			m.Update(result)
			plain, err := Render(source, 80)
			if err != nil || plain != result.content {
				t.Fatalf("ordinary/live parity err=%v", err)
			}
			if m.source != source || len(m.heads) != 2 || m.heads[0].text != tc.title || m.heads[1].text != "Later" {
				t.Fatalf("source heading metadata changed: %+v", m.heads)
			}
			for i, h := range m.heads {
				h.line = original[i].line
				if h != original[i] {
					t.Fatal("source-heading metadata changed")
				}
			}
			if !strings.Contains(result.content, "$x^2$") {
				t.Fatal("unsafe math did not trigger whole-phase literal fallback")
			}
			if strings.Contains(result.content, "readmd-heading-") || strings.Contains(result.content, "\x1b]778;") {
				t.Fatal("provenance leaked")
			}
			rows := reviewHeadingRows(t, m, tc.prefix)
			if len(rows) != 1 {
				t.Fatalf("original heading prefix rows=%v", rows)
			}
			m.activateTarget(reviewRenderedTarget(t, m, dest))
			if m.vp.YOffset() != rows[0] {
				t.Fatalf("exact-title navigation y=%d original syntax heading row=%d", m.vp.YOffset(), rows[0])
			}
			later := reviewHeadingRows(t, m, "# Later")
			m.activateTarget(reviewRenderedTarget(t, m, "#later"))
			if m.vp.YOffset() != later[0] {
				t.Fatal("later source heading lost")
			}
		})
	}
}

func TestHeadingMathIdentitySafeLive(t *testing.T) {
	for _, tc := range []struct{ source, prefix, token string }{
		{"# $\\alpha$\n", "# α", "α"},
		{"$\\alpha$\n===\n", "# α", "α"},
		{"$\\quad$ Title\n---\n", "##", "Title"},
		{"> # $\\alpha$\n", "| # α", "α"},
		{"> $\\alpha$\n> ===\n", "| # α", "α"},
		{"- # $\\alpha$\n", "# α", "α"},
		{"- $\\alpha$\n  ===\n", "# α", "α"},
		{"> $\\quad$ Title\n> ---\n", "| ##", "Title"},
		{"- $\\quad$ Title\n    ---\n", "##", "Title"},
		{"> $\\alpha$\n> second\n> ===\n", "| # α", "second"},
		{"#\r\n\r\n$x^2$\r\n", "#", "x²"},
		{"> #\r\n>\r\n> $x^2$\r\n", "| #", "x²"},
		{"猫 $x^2$\n\n$$\n\\alpha\n$$\n\n# Later\n", "# Later", "α"},
		{"```mermaid\ngraph LR\nA-->B\n```\n\n猫 $x^2$\n\n# Later\n", "# Later", "x²"},
		{"reference[^one]\n\n[^one]: definition\n\n猫 $x^2$\n\n# Later\n", "# Later", "x²"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			source := tc.source + "\n" + strings.Repeat("tail\n\n", 10)
			m := New(source, "safe identity")
			if err := m.SetStyle("notty"); err != nil {
				t.Fatal(err)
			}
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
			result := cmd().(renderedMsg)
			if result.err != nil {
				t.Fatal(result.err)
			}
			m.Update(result)
			if m.source != source || strings.Contains(result.content, "readmd-heading-") {
				t.Fatal("raw source/marker invariance failed")
			}
			if !strings.Contains(strings.Join(m.stripped, "\n"), tc.token) || strings.Contains(result.content, "$x^2$") || strings.Contains(result.content, "\\alpha") || strings.Contains(result.content, "\\quad") {
				t.Fatalf("safe math stayed literal: %q", m.stripped)
			}
			var rows []int
			for row, line := range m.stripped {
				if strings.Contains(line, tc.prefix) {
					rows = append(rows, row)
				}
			}
			if len(rows) != 1 || len(m.heads) != 1 || m.heads[0].line != rows[0] {
				t.Fatalf("source heading row metadata=%+v independently rendered rows=%v", m.heads, rows)
			}
			plain, err := Render(source, 80)
			if err != nil || result.content != plain {
				t.Fatal("safe ordinary/live output mismatch")
			}
			if !reflect.DeepEqual(renderedTargets(source, strings.Split(plain, "\n"), splitStrip(plain)), m.links) {
				t.Fatal("safe OSC8 regions changed")
			}
		})
	}
}

func TestHeadingMathSyntaxLocator(t *testing.T) {
	for _, tc := range []struct {
		source            string
		kind              headingSyntaxKind
		level, start, end int
	}{
		{"# $\\alpha$\n", headingATX, 1, 0, 1},
		{"$\\alpha$\n===\n", headingSetext, 1, 9, 12},
		{"$\\quad$ Title\n---\n", headingSetext, 2, 14, 17},
		{"> # $\\alpha$\r\n", headingATX, 1, 2, 3},
		{"- # $\\alpha$\n", headingATX, 1, 2, 3},
		{"> $\\alpha$\n> ===\n", headingSetext, 1, 13, 16},
		{"- $\\alpha$\n  ===\n", headingSetext, 1, 13, 16},
		{"> first\r\n> second\r\n> ===\r\n", headingSetext, 1, 21, 24},
		{"$\\alpha$\nsecond\n===\n", headingSetext, 1, 16, 19},
		{"#", headingATX, 1, 0, 1},
		{"##\r\n", headingATX, 2, 0, 2},
		{"> ### ###\r\n", headingATX, 3, 2, 5},
		{"   # Title\r\n", headingATX, 1, 3, 4},
		{"  $\\alpha$\r\n  ===\r\n", headingSetext, 1, 14, 17},
		{"> - $\\alpha$\n>   ===\n", headingSetext, 1, 17, 20},
	} {
		t.Run(tc.source, func(t *testing.T) {
			spans, known := headingSyntaxSpans(tc.source)
			want := []headingSyntax{{tc.kind, tc.level, tc.start, tc.end}}
			if !known || !reflect.DeepEqual(spans, want) {
				t.Fatalf("syntax spans=%+v known=%v want=%+v", spans, known, want)
			}
		})
	}
}

func TestHeadingMathSyntaxByteMapping(t *testing.T) {
	for _, tc := range []struct {
		name       string
		edits      []edit
		start, end int
		ok         bool
	}{
		{"ending at syntax start", []edit{{1, 5, "α"}}, 3, 5, true},
		{"starting at syntax start", []edit{{5, 6, "α"}}, 0, 0, false},
		{"starting at syntax end", []edit{{7, 8, "α"}}, 5, 7, true},
		{"crossing syntax start", []edit{{4, 6, "α"}}, 0, 0, false},
		{"adjacent preceding edits", []edit{{0, 2, "猫"}, {2, 5, "α"}}, 5, 7, true},
		{"following edits", []edit{{8, 10, "猫"}}, 5, 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapped, ok := mapMathHeadingSyntax(headingSyntax{headingATX, 2, 5, 7}, tc.edits)
			if ok != tc.ok || (ok && mapped != (headingSyntax{headingATX, 2, tc.start, tc.end})) {
				t.Fatalf("mapped=%+v ok=%v", mapped, ok)
			}
		})
	}
}

func TestMathAppliedEditContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edits []edit
		want  string
		valid bool
	}{
		{"none", nil, "abcdef", true},
		{"sorted adjacent", []edit{{2, 4, "猫"}, {0, 2, "α"}}, "α猫ef", true},
		{"last byte", []edit{{5, 6, ""}}, "abcde", true},
		{"overlap", []edit{{0, 3, "α"}, {2, 4, "猫"}}, "abcdef", false},
		{"duplicate", []edit{{0, 2, "α"}, {0, 2, "α"}}, "abcdef", false},
		{"negative", []edit{{-1, 2, "α"}}, "abcdef", false},
		{"insertion", []edit{{2, 2, "α"}}, "abcdef", false},
		{"reversed", []edit{{3, 2, "α"}}, "abcdef", false},
		{"past end", []edit{{0, 2, "α"}, {5, 7, "猫"}}, "abcdef", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, applied := applyMathEdits("abcdef", tc.edits)
			if candidate != tc.want || (!tc.valid && len(applied) != 0) {
				t.Fatalf("candidate=%q applied=%+v", candidate, applied)
			}
			if reconstructed := applyEdits("abcdef", append([]edit(nil), applied...)); reconstructed != candidate {
				t.Fatalf("applied edits produce %q instead of %q", reconstructed, candidate)
			}
			for i, e := range applied {
				if e.start < 0 || e.start >= e.end || e.end > 6 || (i > 0 && e.start < applied[i-1].end) {
					t.Fatal("returned invalid applied edit")
				}
			}
		})
	}
}

func TestSubstituteMathWithEditsContract(t *testing.T) {
	for _, tc := range []struct{ source, candidate string }{
		{"猫 $x^2$\n\n$$\n\\alpha\n$$\n\n# Later\n", "猫 x²\n\nα\n\n# Later\n"},
		{"# $\\alpha$\n", "# α\n"},
		{"`$\\alpha$`\n", "`$\\alpha$`\n"},
		{"plain\n", "plain\n"},
		{"> $\\alpha$\n> ===\n", "> α\n> ===\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			candidate, applied := substituteMathWithEdits(tc.source)
			if candidate != tc.candidate || substituteMath(tc.source) != candidate {
				t.Fatalf("candidate=%q edits=%+v", candidate, applied)
			}
			if applyEdits(tc.source, append([]edit(nil), applied...)) != candidate {
				t.Fatal("returned scanner intentions instead of exact applied edits")
			}
			for i, e := range applied {
				if e.start < 0 || e.start >= e.end || e.end > len(tc.source) || (i > 0 && e.start < applied[i-1].end) {
					t.Fatal("invalid math edit contract")
				}
			}
		})
	}
}

func TestHeadingMathIdentityNewlineAndUTF8Mapping(t *testing.T) {
	for _, source := range []string{"猫 $x^2$\n\n$$\n\\alpha\n$$\n\n# Later\n", "猫 $x^2$\r\n\r\n$$\r\n\\alpha\r\n$$\r\n\r\n# Later\r\n"} {
		t.Run(source, func(t *testing.T) {
			candidate, applied := substituteMathWithEdits(source)
			before, known := headingSyntaxSpans(source)
			if !known || len(before) != 1 {
				t.Fatal("missing source syntax")
			}
			mapped, ok := mapMathHeadingSyntax(before[0], applied)
			start := strings.Index(candidate, "# Later")
			want := headingSyntax{headingATX, 1, start, start + 1}
			if !ok || mapped != want || !mathPreservesHeadingSyntax(source, candidate, applied) {
				t.Fatalf("byte span mapping=%+v want=%+v edits=%+v", mapped, want, applied)
			}
			if strings.Count(source[:before[0].syntaxStart], "\n") == strings.Count(candidate[:start], "\n") {
				t.Fatal("fixture failed to remove preceding newlines")
			}
			if !strings.Contains(candidate, "猫 x²") || !strings.Contains(candidate, "α") {
				t.Fatal("UTF8 math did not substitute")
			}
		})
	}
}

func TestHeadingMathIdentityKeepsMarkerErrors(t *testing.T) {
	for _, input := range []string{"", "marker marker"} {
		t.Run(input, func(t *testing.T) {
			p := &headingPositions{heads: []heading{{level: 1}}, marker: "marker"}
			if _, err := p.collect(input); err == nil {
				t.Fatal("missing/extra heading markers must remain errors")
			}
		})
	}
}

func TestHeadingMathSyntaxBijection(t *testing.T) {
	for _, source := range []string{"$\\quad$\n---\n\n$## \\alpha$\n", "$\\!$\n===\n\n$# \\alpha$\n", "$# \\alpha$\n===\n"} {
		t.Run(source, func(t *testing.T) {
			candidate, applied := substituteMathWithEdits(source)
			original, known := headingSyntaxSpans(source)
			actual, candidateKnown := headingSyntaxSpans(candidate)
			underline := strings.IndexAny(source, "=-")
			wantOriginal := headingSyntax{headingSetext, 1, underline, underline + 3}
			if source[underline] == '-' {
				wantOriginal.level = 2
			}
			if !known || !candidateKnown || !reflect.DeepEqual(original, []headingSyntax{wantOriginal}) {
				t.Fatalf("unexpected original syntax=%+v", original)
			}
			start := strings.Index(candidate, "#")
			wantCandidate := headingSyntax{headingATX, wantOriginal.level, start, start + wantOriginal.level}
			if !reflect.DeepEqual(actual, []headingSyntax{wantCandidate}) {
				t.Fatalf("unexpected candidate syntax=%+v want=%+v", actual, wantCandidate)
			}
			mapped, ok := mapMathHeadingSyntax(wantOriginal, applied)
			if !ok || mapped.kind != headingSetext || mapped == wantCandidate || mathPreservesHeadingSyntax(source, candidate, applied) {
				t.Fatalf("different surviving syntax incorrectly identified: mapped=%+v candidate=%+v", mapped, wantCandidate)
			}
		})
	}
}

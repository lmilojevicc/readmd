package pager

import (
	"fmt"
	"slices"
	"strings"

	glamansi "charm.land/glamour/v2/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type headingPositions struct {
	heads  []heading
	marker string
}

func renderNavigationDoc(o imgCtx, source string, width int, style string, theme config.Theme, cellWidths ...int) (string, []string, docGfx, []heading, error) {
	positions := &headingPositions{heads: extractHeadings(sanitize(source))}
	if o.Width <= 0 {
		o.Width = width
	}
	out, pending, graphics, err := renderThemedStyledPositions(o, source, width, style, true, theme, positions, cellWidths...)
	return out, pending, graphics, positions.heads, err
}

// Public heading prefixes mark actual AST heading starts, including prefixes
// that wrap onto their own row. This count/order assertion is secondary to
// the math phase's source-syntax identity proof.
func (p *headingPositions) annotate(doc ast.Node, options *glamansi.Options, nonce string) error {
	count := 0
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		if count >= len(p.heads) || p.heads[count].level != h.Level {
			return ast.WalkStop, fmt.Errorf("heading provenance changed during preprocessing")
		}
		count++
		return ast.WalkContinue, nil
	})
	if err != nil {
		return err
	}
	if count != len(p.heads) {
		return fmt.Errorf("heading provenance changed during preprocessing")
	}
	p.marker = strings.Replace(nonce, "\x1b]777;readmd-", "\x1b]778;readmd-heading-", 1) + "start\a"
	for _, style := range []*glamansi.StyleBlock{&options.Styles.H1, &options.Styles.H2, &options.Styles.H3, &options.Styles.H4, &options.Styles.H5, &options.Styles.H6} {
		prefix := style.Prefix
		if prefix == "" {
			prefix = options.Styles.Heading.Prefix
		}
		style.Prefix = p.marker + prefix
	}
	return nil
}

// Consume only render-local provenance before output is cached or OSC8 regions
// are collected. Images and complete containers have already been composed.
func (p *headingPositions) collect(input string) (string, error) {
	var out strings.Builder
	row, count := 0, 0
	for {
		at := strings.Index(input, p.marker)
		if at < 0 {
			out.WriteString(input)
			break
		}
		if count >= len(p.heads) {
			return "", fmt.Errorf("extra rendered heading marker")
		}
		before := input[:at]
		row += strings.Count(before, "\n")
		out.WriteString(before)
		p.heads[count].line = row
		count++
		input = input[at+len(p.marker):]
	}
	if count != len(p.heads) {
		return "", fmt.Errorf("missing rendered heading marker")
	}
	return out.String(), nil
}

type headingSyntaxKind uint8

const (
	headingATX headingSyntaxKind = iota
	headingSetext
)

type headingSyntax struct {
	kind                          headingSyntaxKind
	level, syntaxStart, syntaxEnd int
}

// Locate only syntax belonging to Goldmark-recognized headings. ATX Pos is
// its opening hash; setext Lines end immediately before the underline line.
func headingSyntaxSpans(src string) ([]headingSyntax, bool) {
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	var spans []headingSyntax
	known := true
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		span, ok := locateHeadingSyntax(src, h)
		if !ok {
			known = false
			return ast.WalkStop, nil
		}
		spans = append(spans, span)
		return ast.WalkContinue, nil
	})
	return spans, known
}

func locateHeadingSyntax(src string, h *ast.Heading) (headingSyntax, bool) {
	start := h.Pos()
	if start >= 0 && start < len(src) && src[start] == '#' && (h.Lines().Len() == 0 || start < h.Lines().At(0).Start) {
		end := start
		for end < len(src) && src[end] == '#' {
			end++
		}
		if end-start == h.Level && (end == len(src) || strings.ContainsRune(" \t\r\n", rune(src[end]))) {
			return headingSyntax{headingATX, h.Level, start, end}, true
		}
	}
	if h.Level > 2 || h.Lines().Len() == 0 {
		return headingSyntax{}, false
	}
	last := h.Lines().At(h.Lines().Len() - 1)
	if last.Start < 0 || last.Start >= len(src) {
		return headingSyntax{}, false
	}
	start = lineEnd(src, last.Start)
	if start >= len(src) {
		return headingSyntax{}, false
	}
	start++
	limit := lineEnd(src, start)
	for start < limit && strings.ContainsRune(" \t>", rune(src[start])) {
		start++
	}
	character := byte('=')
	if h.Level == 2 {
		character = '-'
	}
	end := start
	for end < limit && src[end] == character {
		end++
	}
	if end == start || strings.Trim(src[end:limit], " \t\r") != "" {
		return headingSyntax{}, false
	}
	return headingSyntax{headingSetext, h.Level, start, end}, true
}

func mapMathHeadingSyntax(span headingSyntax, edits []edit) (headingSyntax, bool) {
	delta := 0
	for _, e := range edits {
		if e.start < span.syntaxEnd && e.end > span.syntaxStart {
			return headingSyntax{}, false
		}
		if e.end <= span.syntaxStart {
			delta += len(e.replacement) - (e.end - e.start)
		}
	}
	span.syntaxStart += delta
	span.syntaxEnd += delta
	return span, true
}

// A heading survives only if its untouched syntax token survives at the mapped
// byte span. Count/level matches cannot substitute for this ordered bijection.
func mathPreservesHeadingSyntax(before, after string, edits []edit) bool {
	original, known := headingSyntaxSpans(before)
	if !known {
		return false
	}
	candidate, known := headingSyntaxSpans(after)
	if !known {
		return false
	}
	mapped := make([]headingSyntax, 0, len(original))
	for _, span := range original {
		span, ok := mapMathHeadingSyntax(span, edits)
		if !ok {
			return false
		}
		mapped = append(mapped, span)
	}
	return slices.Equal(mapped, candidate)
}

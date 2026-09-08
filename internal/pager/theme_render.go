package pager

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"slices"
	"strconv"
	"strings"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type themeRole int

const (
	roleH1 themeRole = iota
	roleH2
	roleH3
	roleH4
	roleH5
	roleH6
	roleStrong
	roleEmphasis
	roleStrike
	roleLinkText
	roleLinkURL
	roleCode
	roleChecked
	roleUnchecked
	roleLayout
	roleCount
)

type themeComposer struct {
	prefix    string
	roles     [roleCount]config.TextStyle
	active    bool
	notty     bool
	headingBG [6]*config.Color
}

func primitiveStyle(s glamansi.StylePrimitive) config.TextStyle {
	color := func(p *string) *config.Color {
		if p == nil {
			return nil
		}
		c := config.Color(*p)
		return &c
	}
	return config.TextStyle{FG: color(s.Color), BG: color(s.BackgroundColor), Bold: s.Bold, Italic: s.Italic, Underline: s.Underline, Strikethrough: s.CrossedOut}
}

func mergeText(parent, child config.TextStyle) config.TextStyle {
	if child.FG != nil {
		parent.FG = child.FG
	}
	if child.BG != nil {
		parent.BG = child.BG
	}
	if child.Bold != nil {
		parent.Bold = child.Bold
	}
	if child.Italic != nil {
		parent.Italic = child.Italic
	}
	if child.Underline != nil {
		parent.Underline = child.Underline
	}
	if child.Strikethrough != nil {
		parent.Strikethrough = child.Strikethrough
	}
	return parent
}

func textSGR(s config.TextStyle, notty bool) string {
	var style ansi.Style
	if !notty {
		if s.FG != nil {
			if *s.FG == "default" {
				style = style.ForegroundColor(nil)
			} else {
				style = style.ForegroundColor(lipgloss.Color(string(*s.FG)))
			}
		}
		if s.BG != nil {
			if *s.BG == "none" {
				style = style.BackgroundColor(nil)
			} else {
				style = style.BackgroundColor(lipgloss.Color(string(*s.BG)))
			}
		}
	}
	if s.Bold != nil {
		if *s.Bold {
			style = style.Bold()
		} else {
			style = style.Normal()
		}
	}
	if s.Italic != nil {
		style = style.Italic(*s.Italic)
	}
	if s.Underline != nil {
		style = style.Underline(*s.Underline)
	}
	if s.Strikethrough != nil {
		style = style.Strikethrough(*s.Strikethrough)
	}
	if len(style) == 0 {
		return ""
	}
	return style.String()
}

func newThemeComposer(t config.Theme, o *glamansi.Options, notty bool) (*themeComposer, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("theme markers: %w", err)
	}
	c := &themeComposer{prefix: "\x1b]777;readmd-" + hex.EncodeToString(nonce[:]) + ";", notty: notty}
	s := &o.Styles
	headings := []*glamansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6}
	overrides := []config.TextStyle{t.Headings.H1, t.Headings.H2, t.Headings.H3, t.Headings.H4, t.Headings.H5, t.Headings.H6, t.Strong, t.Emphasis, t.Strikethrough, t.Links, t.Links, t.InlineCode, t.Tasks.Checked.TextStyle, t.Tasks.Unchecked.TextStyle}
	bases := []glamansi.StylePrimitive{s.H1.StylePrimitive, s.H2.StylePrimitive, s.H3.StylePrimitive, s.H4.StylePrimitive, s.H5.StylePrimitive, s.H6.StylePrimitive, s.Strong, s.Emph, s.Strikethrough, s.LinkText, s.Link, s.Code.StylePrimitive, s.Task.StylePrimitive, s.Task.StylePrimitive}
	for i := range c.headingBG {
		c.headingBG[i] = primitiveStyle(bases[i]).BG
	}
	for i, override := range overrides {
		c.roles[i] = mergeText(primitiveStyle(bases[i]), override)
		c.active = c.active || override != (config.TextStyle{})
	}
	c.active = c.active || t.Tasks.Checked.Glyph != nil || t.Tasks.Unchecked.Glyph != nil
	if !c.active {
		return c, nil
	}
	// IndentToken is a public layout hook: mark stock-generated margins, not
	// source whitespace. These zero-width bounds survive wrapping unchanged.
	for _, block := range []*glamansi.StyleBlock{&s.Document, &s.BlockQuote, &s.List.StyleBlock, &s.DefinitionList} {
		token := " "
		if block.IndentToken != nil {
			token = *block.IndentToken
		}
		token = c.mark(roleLayout, true) + token + c.mark(roleLayout, false)
		block.IndentToken = &token
	}
	for i, h := range headings {
		h.Prefix = c.mark(themeRole(i), true) + h.Prefix
		h.Suffix += c.mark(themeRole(i), false)
	}
	s.Code.Prefix = c.mark(roleCode, true) + s.Code.Prefix
	s.Code.Suffix += c.mark(roleCode, false)
	s.Link.Format = c.mark(roleLinkURL, true) + "{{.text}}" + c.mark(roleLinkURL, false)
	// Autolink labels (notably email) are synthesized by Glamour, not AST
	// Text children; the public label hook covers those as well.
	s.LinkText.Format = c.mark(roleLinkText, true) + "{{.text}}" + c.mark(roleLinkText, false)
	for _, task := range []struct {
		role  themeRole
		glyph *string
		value *string
	}{{roleChecked, t.Tasks.Checked.Glyph, &s.Task.Ticked}, {roleUnchecked, t.Tasks.Unchecked.Glyph, &s.Task.Unticked}} {
		glyph := strings.TrimRight(*task.value, " ")
		gap := strings.TrimPrefix(*task.value, glyph)
		if task.glyph != nil {
			glyph = *task.glyph
		}
		*task.value = c.mark(task.role, true) + glyph + c.mark(task.role, false) + gap
	}
	return c, nil
}

func (c *themeComposer) mark(role themeRole, open bool) string {
	direction := "close"
	if open {
		direction = "open"
	}
	return c.prefix + strconv.Itoa(int(role)) + ";" + direction + "\a"
}

// Mark only the already-parsed render-local AST. Source lines and the raw
// document remain unchanged; added segments live at the end of this buffer.
func (c *themeComposer) annotate(doc ast.Node, source *[]byte) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		leaf, ok := n.(*ast.Text)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		if stockLabelText(n) {
			return ast.WalkContinue, nil
		}
		var roles []themeRole
		for parent := n.Parent(); parent != nil; parent = parent.Parent() {
			switch parent := parent.(type) {
			case *ast.Emphasis:
				role := roleEmphasis
				if parent.Level > 1 {
					role = roleStrong
				}
				roles = append(roles, role)
			case *extast.Strikethrough:
				// Stock strike containers flatten child links to plain label text.
				roles = slices.DeleteFunc(roles, func(role themeRole) bool { return role == roleLinkText })
				roles = append(roles, roleStrike)
			case *ast.Link:
				roles = append(roles, roleLinkText)
			case *ast.CodeSpan:
				roles = append(roles, roleCode)
			}
		}
		if len(roles) == 0 {
			return ast.WalkContinue, nil
		}
		var value strings.Builder
		for i := len(roles) - 1; i >= 0; i-- {
			value.WriteString(c.mark(roles[i], true))
		}
		value.Write(leaf.Segment.Value(*source))
		for _, role := range roles {
			value.WriteString(c.mark(role, false))
		}
		start := len(*source)
		*source = append(*source, value.String()...)
		leaf.Segment = text.NewSegment(start, len(*source))
		return ast.WalkContinue, nil
	})
}

// Glamour flattens image alt text and stock nested-table link labels. Marking
// those source leaves changes the private footer deduplication keys. Links still
// get their theme through the public label/URL hooks; images stay stock.
func stockLabelText(n ast.Node) bool {
	link := false
	for parent := n.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case ast.KindImage:
			return true
		case ast.KindLink, ast.KindAutoLink:
			link = true
		case extast.KindTable:
			return link && parent.Parent() != nil && parent.Parent().Kind() != ast.KindDocument
		}
	}
	return false
}

// Consume trusted structural markers on fresh output, before measurement or
// cache ownership. Replay stock SGR plus the nested role cascade at boundaries;
// real OSC 8 and all visible bytes pass through unchanged. Defer SGR/space
// runs until content, a role boundary, or EOL: stock resets protect trailing
// layout padding; source spaces preceding a reset retain their content state.
func (c *themeComposer) compose(input string) (string, error) {
	var out strings.Builder
	var pending, padding bytes.Buffer
	flush := func(layout bool) {
		if layout {
			out.WriteString(padding.String())
		} else {
			out.WriteString(pending.String())
		}
		pending.Reset()
		padding.Reset()
	}
	var stock uv.Style
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	var stack []themeRole
	var active []string
	state := byte(0)
	overlay := func() string {
		var style config.TextStyle
		for _, role := range stack {
			if role == roleLayout {
				return ""
			}
			style = mergeText(style, c.roles[role])
		}
		return textSGR(style, c.notty)
	}
	for rest := input; rest != ""; {
		seq, width, n, next := ansi.DecodeSequence(rest, state, parser)
		if n <= 0 {
			return "", fmt.Errorf("theme: invalid ANSI sequence")
		}
		rest, state = rest[n:], next
		if strings.HasPrefix(seq, c.prefix) {
			body, ok := strings.CutSuffix(strings.TrimPrefix(seq, c.prefix), "\a")
			id, direction, found := strings.Cut(body, ";")
			i, err := strconv.Atoi(id)
			if !ok || !found || err != nil || i < 0 || i >= int(roleCount) {
				return "", fmt.Errorf("theme: malformed role marker")
			}
			role := themeRole(i)
			switch direction {
			case "open":
				stack = append(stack, role)
			case "close":
				if len(stack) == 0 || stack[len(stack)-1] != role {
					return "", fmt.Errorf("theme: unbalanced role marker")
				}
				stack = stack[:len(stack)-1]
			default:
				return "", fmt.Errorf("theme: invalid role marker direction")
			}
			if c.active && (role != roleLayout || slices.ContainsFunc(stack, func(role themeRole) bool { return role != roleLayout })) {
				flush(false)
				out.WriteString("\x1b[m" + strings.Join(active, "") + overlay())
			}
			continue
		}
		if strings.Contains(seq, "\x1b]777;readmd-") && !strings.HasPrefix(seq, c.prefix) {
			return "", fmt.Errorf("theme: untrusted role marker")
		}
		if strings.Contains(seq, c.prefix) {
			return "", fmt.Errorf("theme: incomplete role marker")
		}
		if isSGR(seq) {
			active = pushSGR(active, seq)
			uv.ReadStyle(parser.Params(), &stock)
			pending.WriteString(seq)
			padding.WriteString(seq)
			if c.active {
				pending.WriteString(overlay())
				// Heading-owned padding retains the heading override, not an
				// open inline child. Document padding has no heading background.
				for _, role := range stack {
					if role <= roleH6 && c.headingBG[role] != nil && stock.Bg != nil {
						r, g, b, a := stock.Bg.RGBA()
						x, y, z, w := lipgloss.Color(string(*c.headingBG[role])).RGBA()
						if r == x && g == y && b == z && a == w {
							padding.WriteString(textSGR(c.roles[role], c.notty))
						}
					}
				}
			}
		} else if seq == " " {
			pending.WriteString(seq)
			padding.WriteString(seq)
		} else if seq == "\n" {
			flush(true)
			out.WriteString(seq)
		} else if width == 0 {
			pending.WriteString(seq)
			padding.WriteString(seq)
		} else {
			flush(false)
			out.WriteString(seq)
		}
	}
	flush(true)
	if len(stack) != 0 || strings.Contains(out.String(), c.prefix) {
		return "", fmt.Errorf("theme: unclosed role marker")
	}
	return out.String(), nil
}

// Only link-bearing emphasis subtrees need precomposition: stock emphasis does
// not pass its style through LinkElement. Keep stock parsing and public element
// rendering, then reinsert one escaped render-local segment at the outer boundary.
func (c *themeComposer) prepareLinks(doc ast.Node, source *[]byte, options glamansi.Options) error {
	var nodes []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Kind() == ast.KindImage || n.Kind() == extast.KindStrikethrough || n.Kind() == extast.KindTable {
			return ast.WalkSkipChildren, nil
		}
		if n.Kind() != ast.KindEmphasis {
			return ast.WalkContinue, nil
		}
		link := false
		_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				if child.Kind() == extast.KindStrikethrough || child.Kind() == ast.KindImage {
					return ast.WalkSkipChildren, nil
				}
				link = link || child.Kind() == ast.KindLink || child.Kind() == ast.KindAutoLink
			}
			return ast.WalkContinue, nil
		})
		if link {
			nodes = append(nodes, n)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, node := range nodes {
		ctx := glamansi.NewRenderContext(options)
		base := &glamansi.BlockElement{Block: &bytes.Buffer{}, Style: options.Styles.Document}
		if err := base.Render(io.Discard, ctx); err != nil {
			return err
		}
		for parent := node.Parent(); parent != nil; parent = parent.Parent() {
			if h, ok := parent.(*ast.Heading); ok {
				if err := (&glamansi.HeadingElement{Level: h.Level, First: true}).Render(io.Discard, ctx); err != nil {
					return err
				}
				break
			}
		}
		r := glamansi.NewRenderer(options)
		var out bytes.Buffer
		if err := c.inline(r.NewElement(node, *source).Renderer).Render(&out, ctx); err != nil {
			return err
		}
		// The replacement goes through stock Text's HTML and backslash decoding
		// exactly once more; quote those bytes, including bytes inside OSC targets.
		value := html.EscapeString(strings.ReplaceAll(out.String(), "\\", "\\\\"))
		start := len(*source)
		*source = append(*source, value...)
		node.Parent().ReplaceChild(node.Parent(), node, ast.NewTextSegment(text.NewSegment(start, len(*source))))
	}
	return nil
}

func (c *themeComposer) inline(e glamansi.ElementRenderer) glamansi.ElementRenderer {
	if c == nil || !c.active {
		return e
	}
	if emphasis, ok := e.(*glamansi.EmphasisElement); ok {
		for i, child := range emphasis.Children {
			emphasis.Children[i] = c.inline(child)
		}
		role := roleEmphasis
		if emphasis.Level > 1 {
			role = roleStrong
		}
		return themeInlineElement{emphasis, c.mark(role, true), c.mark(role, false)}
	}
	return e
}

type themeInlineElement struct {
	*glamansi.EmphasisElement
	open, close string
}

func (e themeInlineElement) Render(w io.Writer, ctx glamansi.RenderContext) error {
	_, _ = io.WriteString(w, e.open)
	if err := e.EmphasisElement.Render(w, ctx); err != nil {
		return err
	}
	_, err := io.WriteString(w, e.close)
	return err
}

func (e themeInlineElement) StyleOverrideRender(w io.Writer, ctx glamansi.RenderContext, style glamansi.StylePrimitive) error {
	_, _ = io.WriteString(w, e.open)
	if err := e.EmphasisElement.StyleOverrideRender(w, ctx, style); err != nil {
		return err
	}
	_, err := io.WriteString(w, e.close)
	return err
}

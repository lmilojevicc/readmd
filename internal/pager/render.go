package pager

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"unicode"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const paletteStyleName = "palette"

const paletteChromaTheme = "readmd-palette"

// Plain blockquote rail: magenta(13), styled inside the indent token so
// the bar carries color while quote text stays default-fg.
const quoteBarSGR = "95"

const quoteBarToken = "\x1b[" + quoteBarSGR + "m│\x1b[m "

func resolveStyle(name string) (string, error) {
	switch name {
	case "", "auto":
		return paletteStyleName, nil
	case styles.DarkStyle, styles.LightStyle, styles.NoTTYStyle:
		return name, nil
	}
	return "", fmt.Errorf("unknown style %q (want auto, dark, light or notty)", name)
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }

// Palette-adaptive default: every color is a 16-color ANSI index so output
// follows the terminal's own theme (starship-like) — no truecolor, no painted
// backgrounds. Hue ladder: h1 magenta(13), h2 blue(12), h3 cyan(14),
// h4 green(10), h5 yellow(11), h6 red(9); blockquote rails magenta(13)
// (GFM alerts recolor rail+title per type); inline code white(7);
// rules/fences bright-black(8).
var paletteConfig = glamansi.StyleConfig{
	Document: glamansi.StyleBlock{
		StylePrimitive: glamansi.StylePrimitive{BlockPrefix: "\n", BlockSuffix: "\n"},
		Margin:         uintPtr(2),
	},
	BlockQuote: glamansi.StyleBlock{
		Indent:      uintPtr(1),
		IndentToken: strPtr(quoteBarToken),
	},
	List: glamansi.StyleList{LevelIndent: 2},
	Heading: glamansi.StyleBlock{
		StylePrimitive: glamansi.StylePrimitive{BlockSuffix: "\n"},
	},
	H1:            glamansi.StyleBlock{StylePrimitive: glamansi.StylePrimitive{Prefix: "# ", Color: strPtr("13"), Bold: boolPtr(true)}},
	H2:            glamansi.StyleBlock{StylePrimitive: glamansi.StylePrimitive{Prefix: "## ", Color: strPtr("12"), Bold: boolPtr(true)}},
	H3:            glamansi.StyleBlock{StylePrimitive: glamansi.StylePrimitive{Prefix: "### ", Color: strPtr("14")}},
	H4:            glamansi.StyleBlock{StylePrimitive: glamansi.StylePrimitive{Prefix: "#### ", Color: strPtr("10")}},
	H5:            glamansi.StyleBlock{StylePrimitive: glamansi.StylePrimitive{Prefix: "##### ", Color: strPtr("11")}},
	H6:            glamansi.StyleBlock{StylePrimitive: glamansi.StylePrimitive{Prefix: "###### ", Color: strPtr("9")}},
	Strikethrough: glamansi.StylePrimitive{CrossedOut: boolPtr(true)},
	Emph:          glamansi.StylePrimitive{Italic: boolPtr(true)},
	Strong:        glamansi.StylePrimitive{Bold: boolPtr(true)},
	HorizontalRule: glamansi.StylePrimitive{
		Color:  strPtr("8"),
		Format: "\n--------\n",
	},
	Item:        glamansi.StylePrimitive{BlockPrefix: "• "},
	Enumeration: glamansi.StylePrimitive{BlockPrefix: ". "},
	Task: glamansi.StyleTask{
		Ticked:   "[✓] ",
		Unticked: "[ ] ",
	},
	Link:     glamansi.StylePrimitive{Color: strPtr("6"), Underline: boolPtr(true)},
	LinkText: glamansi.StylePrimitive{Color: strPtr("14"), Bold: boolPtr(true)},
	Image:    glamansi.StylePrimitive{Color: strPtr("13"), Underline: boolPtr(true)},
	ImageText: glamansi.StylePrimitive{
		Faint:  boolPtr(true),
		Format: "Image: {{.text}} →",
	},
	Code: glamansi.StyleBlock{
		StylePrimitive: glamansi.StylePrimitive{
			Prefix: "\u00a0",
			Suffix: "\u00a0",
			Color:  strPtr("7"),
		},
	},
	CodeBlock: glamansi.StyleCodeBlock{
		StyleBlock: glamansi.StyleBlock{Margin: uintPtr(2)},
		Theme:      paletteChromaTheme,
	},
}

// Chroma token → ANSI palette mapping (terminal16 SGRs): keywords 91,
// strings 92, types/constants/classes 93, functions 94, builtins/decorators 95,
// numbers/tags/namespaces/escapes/preproc 96, comments/rules 90, errors 91+bold.
var paletteChromaOnce sync.Once

func registerPaletteChroma() {
	paletteChromaOnce.Do(func() {
		chromastyles.Register(chroma.MustNewStyle(paletteChromaTheme, chroma.StyleEntries{
			chroma.Text:                "",
			chroma.Error:               "#ansired bold",
			chroma.Comment:             "#ansidarkgray",
			chroma.CommentPreproc:      "#ansiturquoise",
			chroma.Keyword:             "#ansired",
			chroma.KeywordReserved:     "#ansifuchsia",
			chroma.KeywordNamespace:    "#ansiturquoise",
			chroma.KeywordType:         "#ansiyellow",
			chroma.Operator:            "",
			chroma.Punctuation:         "",
			chroma.Name:                "",
			chroma.NameBuiltin:         "#ansifuchsia",
			chroma.NameTag:             "#ansiturquoise",
			chroma.NameAttribute:       "#ansiyellow",
			chroma.NameClass:           "#ansiyellow bold",
			chroma.NameConstant:        "#ansiyellow",
			chroma.NameDecorator:       "#ansifuchsia",
			chroma.NameException:       "#ansired bold",
			chroma.NameFunction:        "#ansiblue",
			chroma.LiteralNumber:       "#ansiturquoise",
			chroma.LiteralString:       "#ansigreen",
			chroma.LiteralStringEscape: "#ansiturquoise",
			chroma.GenericDeleted:      "#ansired",
			chroma.GenericInserted:     "#ansigreen",
			chroma.GenericEmph:         "italic",
			chroma.GenericStrong:       "bold",
			chroma.GenericSubheading:   "#ansidarkgray bold",
			chroma.Background:          "",
		}))
	})
}

// Reader mode pins a centered viewport capped at readerWidth columns. Rendered
// prose wraps to that width while structural content can pan within it.
const readerWidth = 120

// readerGeom returns the viewport width and display-only left margin.
// Off = full width, no margin.
func readerGeom(vw int, on bool) (w, margin int) {
	return readerGeometry(vw, on, readerWidth)
}

func (m *Model) readerGeom(on bool) (w, margin int) {
	return readerGeometry(m.width, on, m.readerWidth)
}

func readerGeometry(vw int, on bool, preference int) (w, margin int) {
	if !on {
		return vw, 0
	}
	w = max(1, min(preference, vw-2))
	return w, max(0, (vw-w)/2)
}

func padMargin(out string, margin int) string {
	if margin <= 0 {
		return out
	}
	pad := strings.Repeat(" ", margin)
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func Render(source string, width int) (string, error) {
	out, _, _, err := renderDoc(imgCtx{}, source, width, styles.NoTTYStyle)
	return out, err
}

func renderDoc(o imgCtx, source string, width int, style string, cellWidths ...int) (string, []string, docGfx, error) {
	return renderThemedDoc(o, source, width, style, config.Theme{}, cellWidths...)
}

func renderThemedDoc(o imgCtx, source string, width int, style string, theme config.Theme, cellWidths ...int) (string, []string, docGfx, error) {
	if o.Width <= 0 {
		o.Width = width
	}
	return renderThemedStyled(o, source, width, style, true, theme, cellWidths...)
}

func renderStyled(o imgCtx, source string, width int, style string, alertsOn bool, cellWidths ...int) (string, []string, docGfx, error) {
	return renderThemedStyled(o, source, width, style, alertsOn, config.Theme{}, cellWidths...)
}

func renderThemedStyled(o imgCtx, source string, width int, style string, alertsOn bool, theme config.Theme, cellWidths ...int) (string, []string, docGfx, error) {
	return renderThemedStyledPositions(o, source, width, style, alertsOn, theme, nil, cellWidths...)
}

func renderThemedStyledPositions(o imgCtx, source string, width int, style string, alertsOn bool, theme config.Theme, positions *headingPositions, cellWidths ...int) (string, []string, docGfx, error) {
	src := sanitize(source)
	semanticSource := src
	src = protectFootnoteMarkers(src)
	src = expandMermaid(src)
	mathSource, mathEdits := substituteMathWithEdits(src)
	if mathSource == src || mathPreservesHeadingSyntax(src, mathSource, mathEdits) {
		src = mathSource
	}
	options, err := rendererOptions(style)
	if err != nil {
		return "", nil, docGfx{}, err
	}
	// Use the same document layout as prose, before display-only reader framing.
	document := options.Styles.Document
	o.Padding = blockColumns(document)
	o.LeftPadding = o.Padding
	if document.Margin != nil {
		o.LeftPadding -= int(*document.Margin)
	}
	var figs []figure
	var pending []string
	if o.Enabled && o.store != nil {
		src, figs, pending = insertFigures(src, o)
	}
	post := func(rendered string) (string, docGfx, error) {
		g := docGfx{}
		rendered, ok := spliceFigures(rendered, figs, renderFigure)
		if ok && len(figs) > 0 {
			g = gfxControls(o.store, figs)
		}
		return rendered, g, nil
	}
	intrinsic := make(map[string]bool)
	for _, f := range figs {
		intrinsic[f.token] = true
	}
	out, err := renderGlamour(src, width, style, options, intrinsic, theme, alertsOn, positions, cellWidths...)
	if err != nil {
		return "", nil, docGfx{}, err
	}
	out, g, err := post(out)
	if err == nil {
		// Normalize fresh display text before any geometry consumers: viewport
		// clipping measures literal TABs as zero, but Lipgloss expands them later.
		out = expandDisplayTabs(out)
		out = styleFootnoteMarkers(semanticSource, out, style, theme)
	}
	if err == nil && positions != nil {
		out, err = positions.collect(out)
	}
	return trimTrailing(out), pending, g, err
}

func rendererOptions(style string) (glamansi.Options, error) {
	options := glamansi.Options{TableWrap: boolPtr(false), PreserveNewLines: true}
	if style == paletteStyleName {
		options.Styles = paletteConfig
		options.ChromaFormatter = "terminal16"
	} else {
		if style == "" {
			style = styles.NoTTYStyle
		}
		config, ok := styles.DefaultStyles[style]
		if !ok {
			return glamansi.Options{}, fmt.Errorf("%s: style not found", style)
		}
		options.Styles = *config
	}
	return options, nil
}

// Each render owns its AST and renderers. References resolve before nodes move
// into temporary roots; complete containers retain their parsing context.
func renderGlamour(src string, width int, style string, options glamansi.Options, intrinsic map[string]bool, theme config.Theme, alertsOn bool, positions *headingPositions, cellWidths ...int) (string, error) {
	chromaRenderMu.Lock()
	defer chromaRenderMu.Unlock()
	if style == paletteStyleName {
		registerPaletteChroma()
	}
	if err := configureChroma(&options, style, theme); err != nil {
		return "", err
	}
	composer, err := newThemeComposer(theme, &options, style == styles.NoTTYStyle || style == "")
	if err != nil {
		return "", err
	}
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	if positions != nil {
		if err := positions.annotate(doc, &options, composer.prefix); err != nil {
			return "", err
		}
	}
	markInlineLinkIndices(doc)
	callouts := map[*ast.Blockquote]alert{}
	if alertsOn {
		callouts = detectCallouts(doc, &source)
	}
	configureCallouts(callouts, &source, theme.Callouts, style == styles.NoTTYStyle || style == "")
	space := text.NewSegment(len(source), len(source)+1)
	source = append(source, ' ')
	// Glamour's preserved-newline option includes soft breaks. Resolve those
	// to spaces outside blockquotes, leaving quote lines and Markdown hard breaks.
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if n.Kind() == ast.KindCodeSpan {
			return ast.WalkSkipChildren, nil
		}
		if n, ok := n.(*ast.Text); entering && ok && n.SoftLineBreak() && !n.HardLineBreak() {
			for parent := n.Parent(); parent != nil; parent = parent.Parent() {
				if parent.Kind() == ast.KindBlockquote {
					return ast.WalkContinue, nil
				}
			}
			n.SetSoftLineBreak(false)
			n.Parent().InsertAfter(n.Parent(), n, ast.NewTextSegment(space))
		}
		return ast.WalkContinue, nil
	})
	if composer.active {
		composer.annotate(doc, &source)
	}
	if err := prepareInlineLinks(doc, &source, options, composer, callouts); err != nil {
		return "", err
	}
	if composer.active {
		if err := composer.prepareLinks(doc, &source, options); err != nil {
			return "", err
		}
	}
	if err := prepareProse(doc, &source, options, width, composer, callouts); err != nil {
		return "", err
	}
	if err := prepareCallouts(doc, &source, options, composer, callouts); err != nil {
		return "", err
	}
	var out bytes.Buffer
	first := true
	tableID := 0
	for node := doc.FirstChild(); node != nil; {
		next := node.NextSibling()
		fragment := options
		prose := node.Kind() == ast.KindParagraph || node.Kind() == ast.KindHeading
		if prose && !intrinsic[strings.TrimSpace(string(node.Lines().Value(source)))] {
			fragment.WordWrap = max(0, width)
		}
		// Apply document margins once. Stock headings/paragraphs normally insert
		// a leading newline when preceded by a sibling; temporary roots have none.
		if !first {
			fragment.Styles.Document.BlockPrefix = ""
			if prose {
				fragment.Styles.Document.BlockPrefix = "\n"
			}
		}
		if next != nil {
			fragment.Styles.Document.BlockSuffix = ""
		}
		if table, ok := node.(*extast.Table); ok {
			rendered, err := renderThemedTable(table, source, fragment, tableID, composer, theme.Table.Border, cellWidths...)
			if err != nil {
				return "", err
			}
			out.WriteString(rendered)
			tableID++
			first = false
			node = next
			continue
		}
		root := ast.NewDocument()
		root.AppendChild(root, node)
		r := renderer.NewRenderer(renderer.WithNodeRenderers(
			util.Prioritized(glamansi.NewRenderer(fragment), 1000),
		))
		if err := r.Render(&out, source, root); err != nil {
			return "", err
		}
		first = false
		node = next
	}
	return composer.compose(out.String())
}

func sanitize(src string) string {
	src = strings.TrimPrefix(src, "\ufeff")
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', kitty.Placeholder:
			return r
		}
		// Preserve format code points that participate in visible grapheme clusters.
		if r == '\u200c' || r == '\u200d' ||
			(r >= '\ufe00' && r <= '\ufe0f') ||
			(r >= '\U000e0020' && r <= '\U000e007f') ||
			(r >= '\U000e0100' && r <= '\U000e01ef') {
			return r
		}
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return '\ufffd'
		}
		return r
	}, src)
}

func trimTrailing(out string) string {
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

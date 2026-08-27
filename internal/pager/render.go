package pager

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"charm.land/glamour/v2"
	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi/kitty"
)

const paletteStyleName = "palette"

const paletteChromaTheme = "readmd-palette"

// Plain blockquote rail: magenta(13), styled directly inside the indent token
// so the bar carries the color while quote text stays default-fg. Alert
// splicing rewrites this exact sequence per type, so it must stay in sync
// with barToken (pinned by test).
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
// lines retain their natural width and pan within that viewport.
const readerWidth = 120

// readerGeom returns the viewport width and display-only left margin.
// Off = full width, no margin.
func readerGeom(vw int, on bool) (w, margin int) {
	if !on {
		return vw, 0
	}
	w = min(readerWidth, max(1, vw-2))
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

func renderDoc(o imgCtx, source string, width int, style string) (string, []string, docGfx, error) {
	if o.Width <= 0 {
		o.Width = width
	}
	out, pending, g, err := renderStyled(o, source, style, true)
	if errors.Is(err, errAlertSplice) {
		return renderStyled(o, source, style, false)
	}
	return out, pending, g, err
}

func renderStyled(o imgCtx, source string, style string, alertsOn bool) (string, []string, docGfx, error) {
	src := sanitize(source)
	src = expandMermaid(src)
	src = substituteMath(src)
	var figs []figure
	var pending []string
	if o.Enabled && o.store != nil {
		src, figs, pending = insertFigures(src, o)
	}
	var alerts []alert
	if alertsOn && style == paletteStyleName {
		src, alerts = insertAlertSentinels(src)
	}
	draw := func(f figure) string { return renderFigure(f, max(1, o.Width-2)) }
	post := func(rendered string) (string, docGfx, error) {
		rendered, ok := spliceAlerts(rendered, alerts)
		if !ok {
			return "", docGfx{}, errAlertSplice
		}
		g := docGfx{}
		rendered, ok = spliceFigures(rendered, figs, draw)
		if ok && len(figs) > 0 {
			g = gfxControls(o.store, figs, max(1, o.Width-2))
		}
		return rendered, g, nil
	}
	out, err := renderGlamour(src, style)
	if err != nil {
		return "", nil, docGfx{}, err
	}
	out, g, err := post(out)
	return trimTrailing(out), pending, g, err
}

func renderGlamour(src string, style string) (string, error) {
	opts := []glamour.TermRendererOption{
		glamour.WithWordWrap(0),
		glamour.WithTableWrap(false),
	}
	if style == paletteStyleName {
		registerPaletteChroma()
		opts = append(opts,
			glamour.WithStyles(paletteConfig),
			glamour.WithChromaFormatter("terminal16"),
		)
	} else {
		if style == "" {
			style = styles.NoTTYStyle
		}
		opts = append(opts, glamour.WithStandardStyle(style))
	}
	tr, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return "", err
	}
	return tr.Render(src)
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

// sepBefore returns "\n" when the line above pos is not blank, so prepending
// it guarantees an inserted sentinel line never merges with preceding prose.
func sepBefore(src string, pos int) string {
	line := strings.TrimRight(src[:pos], " \t\r")
	if !strings.HasSuffix(line, "\n") {
		return ""
	}
	prev := strings.TrimRight(line[:len(line)-1], " \t\r")
	if prev == "" || strings.HasSuffix(prev, "\n") {
		return ""
	}
	return "\n"
}

// sepAfter returns "\n" when the text at pos does not already begin with a
// blank line, so appending it guarantees one after an inserted sentinel.
func sepAfter(src string, pos int) string {
	rest := strings.TrimLeft(src[pos:], " \t")
	if rest != "" && rest[0] != '\n' && rest[0] != '\r' {
		return "\n"
	}
	return ""
}

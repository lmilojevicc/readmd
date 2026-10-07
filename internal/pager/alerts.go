package pager

import (
	"fmt"
	"strings"

	glamansi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/ast"
)

var alertKinds = []struct{ name, icon, title, sgr string }{
	{"note", "\uf449", "Note", "94"}, {"tip", "\uf400", "Tip", "92"}, {"important", "\uf50a", "Important", "95"}, {"warning", "\uf421", "Warning", "93"}, {"caution", "\uf46e", "Caution", "91"},
	{"abstract", "\uf50a", "Abstract", "96"}, {"info", "\uf449", "Info", "94"}, {"todo", "\uf400", "Todo", "94"}, {"success", "\uf400", "Success", "92"}, {"question", "\uf449", "Question", "93"}, {"failure", "\uf46e", "Failure", "91"}, {"danger", "\uf46e", "Danger", "91"}, {"bug", "\uf46e", "Bug", "91"}, {"example", "\uf50a", "Example", "95"}, {"quote", "\uf449", "Quote", "97"},
}

var alertAliases = map[string]string{"summary": "abstract", "tldr": "abstract", "check": "success", "done": "success", "help": "question", "faq": "question", "fail": "failure", "missing": "failure", "error": "danger", "cite": "quote", "hint": "tip", "attention": "warning"}

type alert struct {
	name, canonical, icon, title, sgr string
	consumed                          int
	heading                           *ast.Paragraph
	rail                              string
	titleStyle                        config.TextStyle
}

func barToken(sgr string) string { return "\x1b[" + sgr + "m│\x1b[m " }
func railSeq(sgr string) string  { return "  " + barToken(sgr) }

func matchAlert(line string) (alert, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "[!") {
		return alert{}, false
	}
	end := strings.IndexByte(line, ']')
	if end < 0 || !config.ValidCalloutID(line[2:end]) {
		return alert{}, false
	}
	name := strings.ToLower(line[2:end])
	consumed := end + 1
	if consumed < len(line) && (line[consumed] == '+' || line[consumed] == '-') {
		consumed++
	}
	if consumed < len(line) && line[consumed] != ' ' && line[consumed] != '\t' {
		return alert{}, false
	}
	canonical := name
	if alias, ok := alertAliases[name]; ok {
		canonical = alias
	}
	a := alert{name: name, canonical: canonical, consumed: consumed}
	for _, kind := range alertKinds {
		if kind.name == canonical {
			a.icon, a.title, a.sgr = kind.icon, kind.title, kind.sgr
			return a, true
		}
	}
	a.icon, a.sgr = alertKinds[0].icon, alertKinds[0].sgr
	title := strings.ReplaceAll(strings.ReplaceAll(name, "_", " "), "-", " ")
	a.title = strings.ToUpper(title[:1]) + title[1:]
	return a, true
}

// Recognition uses Goldmark's globally resolved quote/inline tree. Only the
// marker's first source line is detached; its remaining prose keeps line breaks.
func detectCallouts(doc ast.Node, source *[]byte) map[*ast.Blockquote]alert {
	callouts := map[*ast.Blockquote]alert{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		quote, ok := n.(*ast.Blockquote)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		p, ok := quote.FirstChild().(*ast.Paragraph)
		if !ok || p.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		first := p.Lines().At(0)
		a, ok := matchAlert(string(first.Value(*source)))
		if !ok {
			return ast.WalkContinue, nil
		}
		heading, body, ok := splitCalloutTitle(p, first, a.consumed, *source)
		if !ok {
			return ast.WalkContinue, nil
		}
		if strings.TrimSpace(string(first.Value(*source))[a.consumed:]) == "" {
			heading.RemoveChildren(heading)
			heading.AppendChild(heading, escapedRenderText(source, a.title))
		}
		heading.InsertBefore(heading, heading.FirstChild(), escapedRenderText(source, a.icon+" "))
		quote.InsertBefore(quote, p, heading)
		if body.ChildCount() == 0 {
			quote.RemoveChild(quote, p)
		} else {
			quote.ReplaceChild(quote, p, body)
		}
		a.heading = heading
		callouts[quote] = a
		return ast.WalkContinue, nil
	})
	return callouts
}

func calloutAppearance(a alert, theme config.Callouts, notty bool) (string, config.TextStyle) {
	color := config.Color(fmt.Sprint(atoiSGR(a.sgr)))
	railStyle := mergeText(config.TextStyle{FG: &color}, theme.Rail.TextStyle)
	titleStyle := mergeText(config.TextStyle{FG: &color, Bold: boolPtr(true)}, theme.Title)
	rail := "│"
	if theme.Preset != nil && *theme.Preset == "nerd" {
		rail = "▋"
	}
	if theme.Rail.Glyph != nil {
		rail = *theme.Rail.Glyph
	}
	legacy := map[string]config.Callout{"note": theme.Note, "tip": theme.Tip, "important": theme.Important, "warning": theme.Warning, "caution": theme.Caution}[a.canonical]
	layers := []config.Callout{theme.Custom[a.canonical]}
	if a.name != a.canonical {
		layers = append(layers, theme.Custom[a.name])
	}
	layers = append(layers, legacy)
	for _, layer := range layers {
		if layer.Color != nil {
			railStyle.FG = layer.Color
			titleStyle.FG = layer.Color
		}
		railStyle = mergeText(railStyle, layer.Rail.TextStyle)
		titleStyle = mergeText(titleStyle, layer.Title)
		if layer.Rail.Glyph != nil {
			rail = *layer.Rail.Glyph
		}
	}
	return textSGR(railStyle, notty) + rail + "\x1b[m ", titleStyle
}

func atoiSGR(s string) int { // bright ANSI SGR codes map to palette indices
	switch s {
	case "91":
		return 9
	case "92":
		return 10
	case "93":
		return 11
	case "94":
		return 12
	case "95":
		return 13
	case "96":
		return 14
	default:
		return 15
	}
}

func configureCallouts(callouts map[*ast.Blockquote]alert, source *[]byte, theme config.Callouts, notty bool) {
	for quote, a := range callouts {
		a.rail, a.titleStyle = calloutAppearance(a, theme, notty)
		icon := a.icon
		if theme.Preset != nil && *theme.Preset == "unicode" {
			switch a.canonical {
			case "tip", "success":
				icon = "✦"
			case "warning", "question":
				icon = "⚠"
			case "caution", "failure", "danger", "bug":
				icon = "✖"
			case "important", "abstract", "example":
				icon = "✱"
			default:
				icon = "ⓘ"
			}
		}
		layers := []config.Callout{theme.Custom[a.canonical]}
		if a.name != a.canonical {
			layers = append(layers, theme.Custom[a.name])
		}
		layers = append(layers, map[string]config.Callout{"note": theme.Note, "tip": theme.Tip, "important": theme.Important, "warning": theme.Warning, "caution": theme.Caution}[a.canonical])
		for _, layer := range layers {
			if layer.Icon != nil {
				icon = *layer.Icon
			}
		}
		a.heading.ReplaceChild(a.heading, a.heading.FirstChild(), escapedRenderText(source, icon+" "))
		callouts[quote] = a
	}
}

func calloutColumns(a alert, _ glamansi.Options) int { return ansi.StringWidth(a.rail) }

func titlePrimitive(style config.TextStyle, notty bool) glamansi.StylePrimitive {
	p := glamansi.StylePrimitive{Bold: style.Bold, Italic: style.Italic, Underline: style.Underline, CrossedOut: style.Strikethrough}
	if !notty {
		if style.FG != nil {
			p.Color = strPtr(string(*style.FG))
		}
		if style.BG != nil && *style.BG != "none" {
			p.BackgroundColor = strPtr(string(*style.BG))
		}
	}
	return p
}

func prepareCallouts(doc ast.Node, source *[]byte, options glamansi.Options, composer *themeComposer, callouts map[*ast.Blockquote]alert) error {
	if len(callouts) == 0 {
		return nil
	}
	var quotes []*ast.Blockquote
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if quote, ok := n.(*ast.Blockquote); ok && !entering {
			for parent := ast.Node(quote); parent != nil; parent = parent.Parent() {
				if q, ok := parent.(*ast.Blockquote); ok {
					if _, ok := callouts[q]; ok {
						quotes = append(quotes, quote)
						break
					}
				}
			}
		}
		return ast.WalkContinue, nil
	})
	for _, quote := range quotes {
		parent := quote.Parent()
		fresh := neutralDocument(options)
		if a, ok := callouts[quote]; ok {
			rail := a.rail
			if composer.active {
				rail = composer.mark(roleLayout, true) + rail + composer.mark(roleLayout, false)
			}
			fresh.Styles.BlockQuote = glamansi.StyleBlock{Indent: uintPtr(1), IndentToken: strPtr(rail)}
			if body, ok := a.heading.NextSibling().(*ast.Paragraph); ok && !body.HasBlankPreviousLines() {
				// Title and adjacent prose share one paragraph boundary, not a blank row.
				a.heading.AppendChild(a.heading, escapedRenderText(source, "\n"))
				for body.FirstChild() != nil {
					a.heading.AppendChild(a.heading, body.FirstChild())
				}
				quote.RemoveChild(quote, body)
			}
		}
		replacement := ast.NewTextBlock()
		parent.ReplaceChild(parent, quote, replacement)
		out, err := stockRender(quote, *source, fresh)
		if err != nil {
			return err
		}
		replacement.AppendChild(replacement, escapedRenderText(source, out))
	}
	return nil
}

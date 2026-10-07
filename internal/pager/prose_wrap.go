package pager

import (
	"bytes"
	"html"
	"io"
	"strings"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func escapedRenderText(source *[]byte, value string) *ast.Text {
	value = html.EscapeString(strings.ReplaceAll(value, "\\", "\\\\"))
	start := len(*source)
	*source = append(*source, value...)
	return ast.NewTextSegment(text.NewSegment(start, len(*source)))
}

func stockRender(node ast.Node, source []byte, options glamansi.Options) (string, error) {
	root := ast.NewDocument()
	root.AppendChild(root, node)
	var out bytes.Buffer
	r := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(glamansi.NewRenderer(options), 1000)))
	err := r.Render(&out, source, root)
	return out.String(), err
}

func neutralDocument(options glamansi.Options) glamansi.Options {
	options.WordWrap = 0
	options.Styles.Document.Margin = uintPtr(0)
	options.Styles.Document.Indent = uintPtr(0)
	options.Styles.Document.BlockPrefix = ""
	options.Styles.Document.BlockSuffix = ""
	options.Styles.Document.Prefix = ""
	options.Styles.Document.Suffix = ""
	return options
}

func blockColumns(style glamansi.StyleBlock) int {
	columns := 0
	if style.Margin != nil {
		columns += 2 * int(*style.Margin)
	}
	if style.Indent != nil {
		token := " "
		if style.IndentToken != nil {
			token = *style.IndentToken
		}
		columns += int(*style.Indent) * ansi.StringWidth(token)
	}
	return columns
}

func itemColumns(item *ast.ListItem, source []byte, options glamansi.Options) (int, error) {
	ctx := glamansi.NewRenderContext(options)
	base := &glamansi.BlockElement{Block: &bytes.Buffer{}, Style: options.Styles.Document}
	if err := base.Render(io.Discard, ctx); err != nil {
		return 0, err
	}
	var out bytes.Buffer
	if err := glamansi.NewRenderer(options).NewElement(item, source).Renderer.Render(&out, ctx); err != nil {
		return 0, err
	}
	return ansi.StringWidth(out.String()), nil
}

// Only fresh inline prose is wrapped. The complete stock structural roots keep
// WordWrap=0, so a sibling fence, table or ordinary quote is never reflowed.
func prepareProse(doc ast.Node, source *[]byte, options glamansi.Options, width int, composer *themeComposer, callouts map[*ast.Blockquote]alert) error {
	var nodes []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == ast.KindParagraph || n.Kind() == ast.KindTextBlock) {
			nodes = append(nodes, n)
		}
		return ast.WalkContinue, nil
	})
	for _, node := range nodes {
		eligible := false
		columns := blockColumns(options.Styles.Document)
		continuation := 0
		calloutOwner := false
		for parent := node.Parent(); parent != nil; parent = parent.Parent() {
			switch p := parent.(type) {
			case *ast.Blockquote:
				a, ok := callouts[p]
				if !ok {
					if !calloutOwner {
						eligible = false
						goto classified
					}
					columns += blockColumns(options.Styles.BlockQuote)
				} else {
					calloutOwner = true
					eligible = true
					columns += calloutColumns(a, options)
				}
			case *ast.List:
				eligible = true
				s := options.Styles.List.StyleBlock
				for ancestor := p.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
					if ancestor.Kind() == ast.KindList {
						s.Indent = uintPtr(options.Styles.List.LevelIndent)
						break
					}
				}
				columns += blockColumns(s)
			case *ast.ListItem:
				if parent == node.Parent() {
					var err error
					continuation, err = itemColumns(p, *source, options)
					if err != nil {
						return err
					}
					columns += continuation
				}
			default:
				if parent.Kind() == extast.KindDefinitionList {
					eligible = false
					goto classified
				}
			}
		}
	classified:
		if !eligible {
			continue
		}
		paragraph := ast.NewParagraph()
		var checkbox ast.Node
		for child := node.FirstChild(); child != nil; {
			next := child.NextSibling()
			if child.Kind() == extast.KindTaskCheckBox {
				checkbox = child
			} else {
				paragraph.AppendChild(paragraph, child)
			}
			child = next
		}
		fresh := neutralDocument(options)
		fresh.Styles.Paragraph = glamansi.StyleBlock{}
		for _, a := range callouts {
			if a.heading == node {
				fresh.Styles.Document.StylePrimitive = titlePrimitive(a.titleStyle, composer.notty)
				break
			}
		}
		out, err := stockRender(paragraph, *source, fresh)
		if err != nil {
			return err
		}
		out = strings.TrimSuffix(out, "\n")
		if width > 0 {
			var wrapped bytes.Buffer
			pen := lipgloss.NewWrapWriter(&wrapped)
			if _, err := io.WriteString(pen, ansi.Wordwrap(out, max(1, width-columns), "")); err != nil {
				return err
			}
			if err := pen.Close(); err != nil {
				return err
			}
			out = wrapped.String()
		}
		if continuation > 0 {
			pad := strings.Repeat(" ", continuation)
			if composer.active {
				pad = composer.mark(roleLayout, true) + pad + composer.mark(roleLayout, false)
			}
			out = strings.ReplaceAll(out, "\n", "\n"+pad)
			if node.PreviousSibling() != nil {
				out = pad + out
			}
		}
		node.RemoveChildren(node)
		if checkbox != nil {
			node.AppendChild(node, checkbox)
		}
		node.AppendChild(node, escapedRenderText(source, out))
	}
	return nil
}

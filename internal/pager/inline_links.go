package pager

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	glamansi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

const proseLinkPrefix = "readmd-prose-"

type proseLinkElement struct {
	inner    glamansi.ElementRenderer
	dest, id string
	anchor   bool
}

func (e proseLinkElement) Render(w io.Writer, ctx glamansi.RenderContext) error {
	var out bytes.Buffer
	if err := e.inner.Render(&out, ctx); err != nil {
		return err
	}
	if e.anchor {
		_, err := io.WriteString(w, ansi.SetHyperlink(e.dest, "id="+e.id)+out.String()+ansi.ResetHyperlink())
		return err
	}
	var tagged strings.Builder
	state := byte(0)
	for rest := out.String(); rest != ""; {
		seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
		if n <= 0 {
			return fmt.Errorf("prose link: invalid ANSI")
		}
		rest, state = rest[n:], next
		if _, dest, ok := parseOSC8(seq); ok && dest == e.dest {
			seq = ansi.SetHyperlink(dest, "id="+e.id)
		}
		tagged.WriteString(seq)
	}
	_, err := io.WriteString(w, tagged.String())
	return err
}

func proseOwned(node ast.Node, callouts map[*ast.Blockquote]alert) bool {
	eligible, calloutOwner := false, false
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case ast.KindList:
			eligible = true
		case ast.KindBlockquote:
			if _, ok := callouts[parent.(*ast.Blockquote)]; ok {
				eligible = true
				calloutOwner = true
			} else if !calloutOwner {
				return false
			}
		case extast.KindDefinitionList:
			return false
		}
	}
	return eligible
}

// IDs name actual AST occurrences, not destinations. Link indices also tell
// source-assisted metadata matching which already-identified occurrences to omit.
func prepareInlineLinks(doc ast.Node, source *[]byte, options glamansi.Options, composer *themeComposer, callouts map[*ast.Blockquote]alert) error {
	ids := map[ast.Node]string{}
	selected := map[ast.Node]bool{}
	imageIndex := 0
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Kind() == extast.KindTable && n.Parent().Kind() == ast.KindDocument {
			return ast.WalkSkipChildren, nil
		}
		id, anchor := "", false
		switch leaf := n.(type) {
		case *ast.Link:
			index, _ := n.AttributeString(proseLinkIndexAttribute)
			id = fmt.Sprintf("%slink-%d", proseLinkPrefix, index)
			anchor = strings.HasPrefix(string(leaf.Destination), "#")
		case *ast.AutoLink:
			index, _ := n.AttributeString(proseLinkIndexAttribute)
			id = fmt.Sprintf("%slink-%d", proseLinkPrefix, index)
		case *ast.Image:
			id = fmt.Sprintf("%simage-%d", proseLinkPrefix, imageIndex)
			imageIndex++
		default:
			return ast.WalkContinue, nil
		}
		ids[n] = id
		eligible := proseOwned(n, callouts)
		for parent := n.Parent(); parent != nil; parent = parent.Parent() {
			if parent.Kind() == extast.KindStrikethrough || parent.Kind() == ast.KindImage {
				return ast.WalkContinue, nil
			}
			if parent.Kind() == extast.KindTable {
				eligible = false
			}
		}
		selected[n] = anchor || eligible
		return ast.WalkContinue, nil
	})
	var nodes []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Kind() == ast.KindEmphasis {
			table := false
			for p := n.Parent(); p != nil; p = p.Parent() {
				table = table || p.Kind() == extast.KindTable
			}
			if !table {
				found := false
				_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
					if entering {
						found = found || selected[child]
					}
					return ast.WalkContinue, nil
				})
				if found {
					nodes = append(nodes, n)
					return ast.WalkSkipChildren, nil
				}
			}
		}
		if selected[n] {
			nodes = append(nodes, n)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, node := range nodes {
		fresh := neutralDocument(options)
		for _, a := range callouts {
			for p := node.Parent(); p != nil; p = p.Parent() {
				if p == a.heading {
					fresh.Styles.Document.StylePrimitive = titlePrimitive(a.titleStyle, composer.notty)
					break
				}
			}
		}
		ctx := glamansi.NewRenderContext(fresh)
		base := &glamansi.BlockElement{Block: &bytes.Buffer{}, Style: fresh.Styles.Document}
		if err := base.Render(io.Discard, ctx); err != nil {
			return err
		}
		r := glamansi.NewRenderer(fresh)
		var build func(ast.Node) glamansi.ElementRenderer
		build = func(n ast.Node) glamansi.ElementRenderer {
			element := r.NewElement(n, *source).Renderer
			if emphasis, ok := element.(*glamansi.EmphasisElement); ok {
				emphasis.Children = nil
				for child := n.FirstChild(); child != nil; child = child.NextSibling() {
					emphasis.Children = append(emphasis.Children, build(child))
				}
			}
			dest, anchor := "", false
			switch n := n.(type) {
			case *ast.Link:
				dest = string(n.Destination)
				anchor = strings.HasPrefix(dest, "#")
				if _, ok := n.AttributeString(calloutTitleLinkAttribute); ok {
					element.(*glamansi.LinkElement).SkipHref = true
				}
				if anchor {
					children := []glamansi.ElementRenderer{}
					for child := n.FirstChild(); child != nil; child = child.NextSibling() {
						children = append(children, build(child))
					}
					element = &glamansi.LinkElement{URL: "#", Children: children, SkipHref: true}
				}
			case *ast.AutoLink:
				if link, ok := element.(*glamansi.LinkElement); ok {
					dest = link.URL
				}
			case *ast.Image:
				dest = string(n.Destination)
			}
			if id, ok := ids[n]; ok {
				element = proseLinkElement{element, dest, id, anchor}
			}
			return composer.inline(element)
		}
		var out bytes.Buffer
		if err := build(node).Render(&out, ctx); err != nil {
			return err
		}
		node.Parent().ReplaceChild(node.Parent(), node, escapedRenderText(source, out.String()))
	}
	return nil
}

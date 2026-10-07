package pager

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const proseLinkIndexAttribute = "readmd-prose-link-index"
const calloutTitleLinkAttribute = "readmd-callout-title-link"

func markInlineLinkIndices(doc ast.Node) {
	index := 0
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Kind() == extast.KindTable && n.Parent().Kind() == ast.KindDocument {
			return ast.WalkSkipChildren, nil
		}
		if n.Kind() == ast.KindLink || n.Kind() == ast.KindAutoLink {
			n.SetAttributeString(proseLinkIndexAttribute, index)
			index++
		}
		return ast.WalkContinue, nil
	})
}

type calloutInlinePlan struct {
	node     ast.Node
	children []*calloutInlinePlan
}

func (p *calloutInlinePlan) assemble() ast.Node {
	for _, child := range p.children {
		p.node.AppendChild(p.node, child.assemble())
	}
	return p.node
}

// Plan before moving any nodes: an unsupported crossing inline leaves the
// exceptional quote completely intact. Public containers keep both styled halves.
func splitCalloutTitle(p *ast.Paragraph, first text.Segment, consumed int, source []byte) (*ast.Paragraph, *ast.Paragraph, bool) {
	line := string(first.Value(source))
	titleStart := first.Start + consumed
	titleStart += len(line[consumed:]) - len(strings.TrimLeft(line[consumed:], " \t"))
	titleEnd := first.Start + len(strings.TrimRight(line, "\r\n"))
	bodyStart := first.Stop
	cursor := first.Start
	var split func(ast.Node) (*calloutInlinePlan, *calloutInlinePlan, bool)
	split = func(n ast.Node) (*calloutInlinePlan, *calloutInlinePlan, bool) {
		if leaf, ok := n.(*ast.Text); ok {
			var title, body *calloutInlinePlan
			if start, stop := max(leaf.Segment.Start, titleStart), min(leaf.Segment.Stop, titleEnd); start < stop {
				segment := leaf.Segment
				segment.Start, segment.Stop = start, stop
				clone := ast.NewTextSegment(segment)
				clone.SetRaw(leaf.IsRaw())
				title = &calloutInlinePlan{node: clone}
			}
			if start := max(leaf.Segment.Start, bodyStart); start < leaf.Segment.Stop || start == leaf.Segment.Stop && leaf.Segment.Start >= bodyStart && (leaf.SoftLineBreak() || leaf.HardLineBreak()) {
				segment := leaf.Segment
				segment.Start = start
				clone := ast.NewTextSegment(segment)
				clone.SetRaw(leaf.IsRaw())
				clone.SetSoftLineBreak(leaf.SoftLineBreak())
				clone.SetHardLineBreak(leaf.HardLineBreak())
				body = &calloutInlinePlan{node: clone}
			}
			cursor = leaf.Segment.Stop
			if leaf.SoftLineBreak() || leaf.HardLineBreak() {
				cursor = max(cursor, bodyStart)
			}
			return title, body, true
		}
		var clone func() ast.Node
		switch node := n.(type) {
		case *ast.Emphasis:
			clone = func() ast.Node { return ast.NewEmphasis(node.Level) }
		case *ast.CodeSpan:
			clone = func() ast.Node { return ast.NewCodeSpan() }
		case *extast.Strikethrough:
			clone = func() ast.Node { return extast.NewStrikethrough() }
		case *ast.Link:
			clone = func() ast.Node {
				link := ast.NewLink()
				link.Destination = node.Destination
				link.Title = node.Title
				link.Reference = node.Reference
				if index, ok := node.AttributeString(proseLinkIndexAttribute); ok {
					link.SetAttributeString(proseLinkIndexAttribute, index)
				}
				return link
			}
		}
		if clone != nil {
			title, body := &calloutInlinePlan{node: clone()}, &calloutInlinePlan{node: clone()}
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				left, right, ok := split(child)
				if !ok {
					return nil, nil, false
				}
				if left != nil {
					title.children = append(title.children, left)
				}
				if right != nil {
					body.children = append(body.children, right)
				}
			}
			if len(title.children) == 0 {
				title = nil
			}
			if len(body.children) == 0 {
				body = nil
			}
			if n.Kind() == ast.KindLink && title != nil && body != nil {
				title.node.SetAttributeString(calloutTitleLinkAttribute, true)
			}
			return title, body, true
		}
		start, stop := n.Pos(), -1
		_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
			if leaf, ok := child.(*ast.Text); entering && ok {
				if start < 0 || leaf.Segment.Start < start {
					start = leaf.Segment.Start
				}
				stop = max(stop, leaf.Segment.Stop)
			}
			if raw, ok := child.(*ast.RawHTML); entering && ok {
				for i := 0; i < raw.Segments.Len(); i++ {
					s := raw.Segments.At(i)
					if start < 0 || s.Start < start {
						start = s.Start
					}
					stop = max(stop, s.Stop)
				}
			}
			return ast.WalkContinue, nil
		})
		if start < 0 {
			start = cursor
		}
		if start < titleEnd && stop > bodyStart {
			return nil, nil, false
		}
		if stop >= 0 {
			cursor = stop
		}
		part := &calloutInlinePlan{node: n}
		if start < bodyStart {
			return part, nil, true
		}
		return nil, part, true
	}
	title, body := ast.NewParagraph(), ast.NewParagraph()
	var left, right []*calloutInlinePlan
	for child := p.FirstChild(); child != nil; child = child.NextSibling() {
		a, b, ok := split(child)
		if !ok {
			return nil, nil, false
		}
		if a != nil {
			left = append(left, a)
		}
		if b != nil {
			right = append(right, b)
		}
	}
	for _, part := range left {
		title.AppendChild(title, part.assemble())
	}
	for _, part := range right {
		body.AppendChild(body, part.assemble())
	}
	return title, body, true
}

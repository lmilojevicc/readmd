package pager

import (
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	ext "github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var md = goldmark.New(
	goldmark.WithExtensions(
		ext.GFM,
		ext.DefinitionList,
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
)

type edit struct {
	start, end  int
	replacement string
}

func applyEdits(src string, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	prev := 0
	for _, e := range edits {
		b.WriteString(src[prev:e.start])
		b.WriteString(e.replacement)
		prev = e.end
	}
	b.WriteString(src[prev:])
	return b.String()
}

func lineStart(src string, i int) int {
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	return i
}

func lineEnd(src string, i int) int {
	for i < len(src) && src[i] != '\n' {
		i++
	}
	return i
}

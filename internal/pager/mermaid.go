package pager

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	mmdMaxLabel = 16
	mmdHGap     = 4
	mmdVGap     = 3
	mmdSeqGap   = 6
	mmdLRGap    = 5
	mmdLRVGap   = 2
	mmdNoteCap  = 20

	mmdWidePad = '​'
)

func expandMermaid(src string) string {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	var edits []edit
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		fcb, ok := n.(*ast.FencedCodeBlock)
		if !ok || fcb.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		if !strings.EqualFold(strings.TrimSpace(string(fcb.Language(bsrc))), "mermaid") {
			return ast.WalkContinue, nil
		}
		art, ok := layoutMermaid(string(fcb.Lines().Value(bsrc)))
		if !ok {
			return ast.WalkContinue, nil
		}
		ls := fcb.Lines()
		first := ls.At(0).Start
		contentLine := lineStart(src, first)
		opener := contentLine
		if contentLine > 0 {
			opener = lineStart(src, contentLine-1)
		}
		marker := strings.Index(src[opener:contentLine], "`")
		if marker < 0 {
			return ast.WalkContinue, nil
		}
		prefix := src[opener : opener+marker]
		start := opener
		end := lineEnd(src, ls.At(ls.Len()-1).Stop)
		trail := ""
		if end < len(src) && src[end] == '\n' {
			end++
		} else {
			trail = "\n"
		}
		var b strings.Builder
		for _, l := range strings.Split("```mermaid\n"+art+"\n```", "\n") {
			b.WriteString(prefix)
			b.WriteString(l)
			b.WriteByte('\n')
		}
		edits = append(edits, edit{start, end, b.String() + trail})
		return ast.WalkContinue, nil
	})
	if len(edits) == 0 {
		return src
	}
	return applyEdits(src, edits)
}

func layoutMermaid(content string) (string, bool) {
	var sig []string
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		sig = append(sig, t)
	}
	if len(sig) == 0 {
		return "", false
	}
	if sig[0] == "sequenceDiagram" {
		return layoutSequence(sig[1:])
	}
	fields := strings.Fields(sig[0])
	if len(fields) != 2 || fields[0] != "flowchart" {
		return "", false
	}
	switch fields[1] {
	case "TD", "TB":
		return layoutFlowTD(sig[1:])
	case "LR":
		return layoutFlowLR(sig[1:])
	}
	return "", false
}

type mmdShape int

const (
	shapeNone mmdShape = iota
	shapeSquare
	shapeRound
	shapeDiamond
	shapeCylinder
)

type mmdNode struct {
	id    string
	label string
	shape mmdShape
	order int
}

type mmdEdge struct {
	from, to string
	label    string
	arrow    bool
}

type mmdGraph struct {
	nodes []*mmdNode
	pos   map[string]int
	edges []mmdEdge
}

func newMmdGraph() *mmdGraph {
	return &mmdGraph{pos: map[string]int{}}
}

func (g *mmdGraph) node(id string) *mmdNode {
	if i, ok := g.pos[id]; ok {
		return g.nodes[i]
	}
	n := &mmdNode{id: id, label: id, order: len(g.nodes)}
	g.pos[id] = len(g.nodes)
	g.nodes = append(g.nodes, n)
	return n
}

var flowKeywords = map[string]bool{
	"subgraph": true, "end": true, "direction": true,
	"classDef": true, "class": true, "style": true,
	"linkStyle": true, "click": true,
}

type mmdTok struct {
	node  bool
	text  string
	label string
}

func hasRunesPrefix(rs []rune, i int, p string) bool {
	if i+len(p) > len(rs) {
		return false
	}
	for k := 0; k < len(p); k++ {
		if rs[i+k] != rune(p[k]) {
			return false
		}
	}
	return true
}

func lexFlowLine(s string) ([]mmdTok, bool) {
	rs := []rune(s)
	var out []mmdTok
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, mmdTok{node: true, text: cur.String()})
			cur.Reset()
		}
	}
	i := 0
	for i < len(rs) {
		r := rs[i]
		if depth == 0 && r == '-' && (hasRunesPrefix(rs, i, "-->") || hasRunesPrefix(rs, i, "---")) {
			flush()
			head := string(rs[i : i+3])
			i += 3
			label := ""
			if i < len(rs) && rs[i] == '|' {
				j := i + 1
				for j < len(rs) && rs[j] != '|' {
					j++
				}
				if j >= len(rs) || j == i+1 {
					return nil, false
				}
				label = strings.TrimSpace(string(rs[i+1 : j]))
				if label == "" {
					return nil, false
				}
				i = j + 1
			}
			out = append(out, mmdTok{text: head, label: label})
			continue
		}
		switch r {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ' ', '\t':
			if depth == 0 {
				flush()
				i++
				continue
			}
		}
		cur.WriteRune(r)
		i++
	}
	if depth != 0 {
		return nil, false
	}
	flush()
	return out, true
}

var shapePairs = map[rune]rune{'[': ']', '(': ')', '{': '}'}

func parseNodeTok(t string) (id, label string, shape mmdShape, ok bool) {
	rs := []rune(t)
	i := 0
	for i < len(rs) && isIDRune(rs[i]) {
		i++
	}
	if i == 0 {
		return "", "", shapeNone, false
	}
	id = string(rs[:i])
	if i == len(rs) {
		return id, id, shapeNone, true
	}
	body, shape, ok := shapeInner([]rune(rs[i:]))
	if !ok {
		return "", "", shapeNone, false
	}
	label = strings.TrimSpace(string(body))
	if len(label) >= 2 && label[0] == '"' && label[len(label)-1] == '"' {
		label = label[1 : len(label)-1]
	}
	if label == "" {
		return "", "", shapeNone, false
	}
	return id, label, shape, true
}

// shapeInner validates the runes between a node's outermost shape delimiters.
// The cylinder [(label)] nests a second pair; its body must not contain shape
// characters, matching the rule for every other shape.
func shapeInner(rs []rune) ([]rune, mmdShape, bool) {
	if len(rs) < 2 {
		return nil, shapeNone, false
	}
	closer, known := shapePairs[rs[0]]
	if !known || rs[len(rs)-1] != closer {
		return nil, shapeNone, false
	}
	body := rs[1 : len(rs)-1]
	shape := shapeSquare
	switch rs[0] {
	case '(':
		shape = shapeRound
	case '{':
		shape = shapeDiamond
	case '[':
		if len(body) >= 2 && body[0] == '(' && body[len(body)-1] == ')' {
			body, shape = body[1:len(body)-1], shapeCylinder
		}
	}
	for _, r := range body {
		if _, bad := shapePairs[r]; bad || r == ']' || r == ')' || r == '}' {
			return nil, shapeNone, false
		}
	}
	return body, shape, true
}

func isIDRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' || r == '_'
}

func (g *mmdGraph) chain(toks []mmdTok) bool {
	if len(toks) == 0 || flowKeywords[toks[0].text] {
		return false
	}
	id, label, shape, ok := parseNodeTok(toks[0].text)
	if !ok {
		return false
	}
	n := g.node(id)
	if shape != shapeNone {
		n.label, n.shape = label, shape
	}
	prev := id
	for i := 1; i < len(toks); i += 2 {
		a := toks[i]
		if a.node || (a.text != "-->" && a.text != "---") {
			return false
		}
		if i+1 >= len(toks) {
			return false
		}
		id2, label2, shape2, ok := parseNodeTok(toks[i+1].text)
		if !ok {
			return false
		}
		if id2 == prev {
			return false
		}
		n2 := g.node(id2)
		if shape2 != shapeNone {
			n2.label, n2.shape = label2, shape2
		}
		g.edges = append(g.edges, mmdEdge{from: prev, to: id2, label: a.label, arrow: a.text == "-->"})
		prev = id2
	}
	return true
}

func parseFlow(lines []string) (*mmdGraph, bool) {
	g := newMmdGraph()
	for _, l := range lines {
		toks, ok := lexFlowLine(l)
		if !ok || !g.chain(toks) {
			return nil, false
		}
	}
	if len(g.nodes) == 0 {
		return nil, false
	}
	return g, true
}

// assignRanks layers nodes for flow layout. Cycles are tolerated the way the
// pager draws them: a DFS from each node in declaration order marks cycle-
// closing edges as back edges; they are excluded from ranking and from
// drawing (an ASCII layered canvas has no honest way to route an upward
// arrow), so the rendered diagram shows every acyclic path and silently omits
// the closing edge. Self loops are declined earlier, in chain().
func assignRanks(g *mmdGraph) ([]int, map[int]bool, bool) {
	out := make([][]int, len(g.nodes))
	for ei := range g.edges {
		f := g.pos[g.edges[ei].from]
		out[f] = append(out[f], ei)
	}
	state := make([]int8, len(g.nodes))
	back := map[int]bool{}
	var dfs func(u int)
	dfs = func(u int) {
		state[u] = 1
		for _, ei := range out[u] {
			v := g.pos[g.edges[ei].to]
			switch state[v] {
			case 0:
				dfs(v)
			case 1:
				back[ei] = true
			}
		}
		state[u] = 2
	}
	for i := range g.nodes {
		if state[i] == 0 {
			dfs(i)
		}
	}
	indeg := make([]int, len(g.nodes))
	adj := make([][]int, len(g.nodes))
	for ei, e := range g.edges {
		if back[ei] {
			continue
		}
		f, t := g.pos[e.from], g.pos[e.to]
		adj[f] = append(adj[f], t)
		indeg[t]++
	}
	ranks := make([]int, len(g.nodes))
	queue := make([]int, 0, len(g.nodes))
	for i, d := range indeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	done := 0
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		done++
		for _, v := range adj[u] {
			if ranks[u]+1 > ranks[v] {
				ranks[v] = ranks[u] + 1
			}
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	if done < len(g.nodes) {
		return nil, nil, false
	}
	return ranks, back, true
}

type mmdCell struct {
	arms  byte
	glyph rune
	lock  bool
	box   bool
}

const (
	armN = 1
	armE = 2
	armS = 4
	armW = 8
)

func armChar(a byte) rune {
	switch a {
	case armN | armS:
		return '│'
	case armE | armW:
		return '─'
	case armE | armS:
		return '┌'
	case armS | armW:
		return '┐'
	case armN | armE:
		return '└'
	case armN | armW:
		return '┘'
	case armN | armS | armE:
		return '├'
	case armN | armS | armW:
		return '┤'
	case armE | armW | armS:
		return '┬'
	case armN | armE | armW:
		return '┴'
	case armN | armE | armS | armW:
		return '┼'
	case armN, armS:
		return '│'
	case armE, armW:
		return '─'
	}
	return ' '
}

type mmdCanvas struct {
	c [][]mmdCell
	w int
}

func newMmdCanvas(w, h int) *mmdCanvas {
	cv := &mmdCanvas{w: max(w, 1)}
	cv.c = make([][]mmdCell, max(h, 1))
	for i := range cv.c {
		cv.c[i] = make([]mmdCell, cv.w)
	}
	return cv
}

func (cv *mmdCanvas) addArms(x, y int, a byte) {
	if x < 0 || y < 0 || x >= cv.w || y >= len(cv.c) {
		return
	}
	c := &cv.c[y][x]
	if c.lock {
		return
	}
	c.arms |= a
	c.glyph = 0
}

func (cv *mmdCanvas) setGlyph(x, y int, g rune) {
	if x < 0 || y < 0 || x >= cv.w || y >= len(cv.c) {
		return
	}
	c := &cv.c[y][x]
	if c.lock {
		return
	}
	c.glyph = g
	c.arms = 0
}

func (cv *mmdCanvas) put(x, y int, s string) {
	xx := x
	for _, r := range s {
		w := max(ansi.StringWidth(string(r)), 1)
		if xx >= 0 && xx < cv.w && y >= 0 && y < len(cv.c) {
			c := &cv.c[y][xx]
			if !c.lock {
				c.glyph = r
				c.arms = 0
			}
			for k := 1; k < w && xx+k < cv.w; k++ {
				p := &cv.c[y][xx+k]
				if !p.lock {
					p.glyph = mmdWidePad
					p.arms = 0
				}
			}
		}
		xx += w
	}
}

func (cv *mmdCanvas) clearRect(b mmdBox) {
	for y := b.y; y < b.y+b.h && y < len(cv.c); y++ {
		for x := b.x; x < b.x+b.w && x < cv.w; x++ {
			cv.c[y][x] = mmdCell{}
		}
	}
}

func (cv *mmdCanvas) trimLeading() {
	used := false
	for _, row := range cv.c {
		if cv.w > 0 && (row[0].arms != 0 || row[0].glyph != 0 || row[0].lock) {
			used = true
			break
		}
	}
	if used {
		return
	}
	for y, row := range cv.c {
		cv.c[y] = row[1:]
	}
	cv.w--
}

func (cv *mmdCanvas) String() string {
	var b strings.Builder
	for _, row := range cv.c {
		var lb strings.Builder
		lb.Grow(cv.w)
		for _, c := range row {
			switch {
			case c.glyph != 0:
				if c.glyph != mmdWidePad {
					lb.WriteRune(c.glyph)
				}
			case c.arms != 0:
				lb.WriteRune(armChar(c.arms))
			default:
				lb.WriteRune(' ')
			}
		}
		b.WriteString(strings.TrimRight(lb.String(), " "))
		b.WriteByte('\n')
	}
	s := b.String()
	return s[:len(s)-1]
}

type mmdBox struct {
	x, y, w, h int
}

func (b mmdBox) cx() int    { return b.x + b.w/2 }
func (b mmdBox) cy() int    { return b.y + b.h/2 }
func (b mmdBox) bot() int   { return b.y + b.h - 1 }
func (b mmdBox) right() int { return b.x + b.w - 1 }

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

func wrapLabel(s string, cap int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, w := range words {
		switch {
		case cur == "":
			cur = w
		case ansi.StringWidth(cur)+1+ansi.StringWidth(w) <= cap:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	return append(lines, cur)
}

func mmdBoxFor(label []string) mmdBox {
	w := 1
	for _, l := range label {
		w = max(w, ansi.StringWidth(l))
	}
	return mmdBox{w: w + 2, h: len(label) + 2}
}

// drawMmdBox renders a node. Cylinders [(label)] keep the exact box geometry
// but swap the four corners for rounded glyphs (╭╮╰╯), approximating the
// curved caps of mermaid's database shape with zero width math changes.
func drawMmdBox(cv *mmdCanvas, b mmdBox, label []string, lockInterior, cyl bool) {
	l, r, t, bo := b.x, b.right(), b.y, b.bot()
	set := func(x, y int, a byte) {
		cv.addArms(x, y, a)
		cv.c[y][x].box = true
	}
	corner := func(x, y int, g rune) {
		cv.setGlyph(x, y, g)
		cv.c[y][x].box = true
	}
	if cyl {
		corner(l, t, '╭')
		corner(r, t, '╮')
		corner(l, bo, '╰')
		corner(r, bo, '╯')
	} else {
		set(l, t, armE|armS)
		set(r, t, armS|armW)
		set(l, bo, armN|armE)
		set(r, bo, armN|armW)
	}
	for x := l + 1; x < r; x++ {
		set(x, t, armE|armW)
	}
	for y := t + 1; y < bo; y++ {
		set(l, y, armN|armS)
		for x := l + 1; x < r; x++ {
			cv.c[y][x] = mmdCell{lock: lockInterior, box: true}
		}
		set(r, y, armN|armS)
	}
	for x := l + 1; x < r; x++ {
		set(x, bo, armE|armW)
	}
	inner := b.w - 2
	for i, ln := range label {
		yy := t + 1 + i
		if yy >= bo {
			break
		}
		off := (inner - ansi.StringWidth(ln)) / 2
		xx := l + 1 + off
		for _, rr := range ln {
			w := max(ansi.StringWidth(string(rr)), 1)
			cv.c[yy][xx] = mmdCell{glyph: rr}
			for k := 1; k < w; k++ {
				cv.c[yy][xx+k] = mmdCell{glyph: mmdWidePad}
			}
			xx += w
		}
	}
}

func layoutFlowTD(lines []string) (string, bool) {
	g, ok := parseFlow(lines)
	if !ok {
		return "", false
	}
	return renderFlowTD(g)
}

func flowRows(g *mmdGraph) ([][]*mmdNode, []int, map[int]bool, bool) {
	ranks, back, ok := assignRanks(g)
	if !ok {
		return nil, nil, nil, false
	}
	maxRank := 0
	for _, r := range ranks {
		maxRank = max(maxRank, r)
	}
	rows := make([][]*mmdNode, maxRank+1)
	for i, n := range g.nodes {
		rows[ranks[i]] = append(rows[ranks[i]], n)
	}
	return rows, ranks, back, true
}

func renderFlowTD(g *mmdGraph) (string, bool) {
	rows, ranks, back, ok := flowRows(g)
	if !ok {
		return "", false
	}
	labels := map[string][]string{}
	boxOf := map[string]mmdBox{}
	for _, n := range g.nodes {
		ls := wrapLabel(n.label, mmdMaxLabel)
		labels[n.id] = ls
		boxOf[n.id] = mmdBoxFor(ls)
	}
	rowW := make([]int, len(rows))
	rowH := make([]int, len(rows))
	for r, row := range rows {
		for k, n := range row {
			if k > 0 {
				rowW[r] += mmdHGap
			}
			rowW[r] += boxOf[n.id].w
			rowH[r] = max(rowH[r], boxOf[n.id].h)
		}
	}
	W := 0
	for _, w := range rowW {
		W = max(W, w)
	}
	W += 2
	gapHasLabel := make([]bool, len(rows))
	for _, e := range g.edges {
		if e.label != "" {
			gapHasLabel[ranks[g.pos[e.from]]] = true
		}
	}
	y := 0
	rowY := make([]int, len(rows))
	laneY := make([]int, len(rows))
	headY := make([]int, len(rows))
	labelRowY := make([]int, len(rows))
	for r := range rows {
		rowY[r] = y
		bot := y + rowH[r] - 1
		laneY[r] = bot + 1
		gh := mmdVGap - 1
		if gapHasLabel[r] {
			gh++
			labelRowY[r] = laneY[r] + 1
		}
		headY[r] = bot + gh
		y = headY[r] + 1
	}
	H := rowY[len(rows)-1] + rowH[len(rows)-1]
	cv := newMmdCanvas(W, H)
	for r, row := range rows {
		x := (W-2-rowW[r])/2 + 1
		for _, n := range row {
			b := boxOf[n.id]
			b.x, b.y = x, rowY[r]
			boxOf[n.id] = b
			drawMmdBox(cv, b, labels[n.id], true, n.shape == shapeCylinder)
			x += b.w + mmdHGap
		}
	}
	type labelJob struct {
		text string
		x, y int
	}
	var jobs []labelJob
	for ei, e := range g.edges {
		if back[ei] {
			continue
		}
		u, v := boxOf[e.from], boxOf[e.to]
		r := ranks[g.pos[e.from]]
		sx := u.cx()
		tx, ok := corridorCol(cv, laneY[r]+1, v.y-2, v, v.cx())
		detour := !ok
		if detour {
			tx, ok = freeColumn(cv, laneY[r]+1, v.y-2, v.cx())
			if !ok {
				return "", false
			}
		}
		drawEdgeTD(cv, u, v, sx, tx, laneY[r], detour)
		if e.label != "" {
			jobs = append(jobs, labelJob{text: e.label, x: (sx + tx) / 2, y: labelRowY[r]})
		}
	}
	for _, j := range jobs {
		cv.put(labelSpot(cv, j.x, j.y, ansi.StringWidth(j.text)), j.y, j.text)
	}
	cv.trimLeading()
	return cv.String(), true
}

func labelSpot(cv *mmdCanvas, pref, y, lw int) int {
	pref = clampInt(pref-lw/2, 0, max(cv.w-lw, 0))
	for d := 0; d < cv.w; d++ {
		for _, x := range []int{pref + d, pref - d} {
			if x < 0 || x+lw > cv.w {
				continue
			}
			clear := true
			for k := x; k < x+lw; k++ {
				c := cv.c[y][k]
				if c.lock || c.glyph != 0 || c.arms != 0 {
					clear = false
					break
				}
			}
			if clear {
				return x
			}
		}
	}
	return pref
}

func corridorCol(cv *mmdCanvas, laneY, headY int, v mmdBox, pref int) (int, bool) {
	lo, hi := v.x+1, v.right()-1
	if lo > hi {
		return 0, false
	}
	pref = clampInt(pref, lo, hi)
	for d := 0; pref-d >= lo || pref+d <= hi; d++ {
		if pref-d >= lo && corridorClear(cv, laneY+1, headY-1, pref-d) {
			return pref - d, true
		}
		if pref+d <= hi && corridorClear(cv, laneY+1, headY-1, pref+d) {
			return pref + d, true
		}
	}
	return 0, false
}

func corridorClear(cv *mmdCanvas, y0, y1, x int) bool {
	if y0 > y1 {
		return true
	}
	for y := y0; y <= y1; y++ {
		if x < 0 || x >= cv.w || y >= len(cv.c) {
			return false
		}
		c := cv.c[y][x]
		if c.lock || c.box || c.glyph != 0 || c.arms != 0 {
			return false
		}
	}
	return true
}

func drawEdgeTD(cv *mmdCanvas, u, v mmdBox, sx, tx, laneY int, detour bool) {
	hjog := func(from, to, y int) {
		if from == to {
			return
		}
		d, o, step := byte(armE), byte(armW), 1
		if to < from {
			d, o, step = armW, armE, -1
		}
		cv.addArms(from, y, d)
		for x := from + step; x != to; x += step {
			cv.addArms(x, y, armE|armW)
		}
		cv.addArms(to, y, armS|o)
	}
	cv.addArms(sx, u.bot(), armS)
	for y := u.bot() + 1; y < laneY; y++ {
		cv.addArms(sx, y, armN|armS)
	}
	cv.addArms(sx, laneY, armN)
	hjog(sx, tx, laneY)
	if !detour {
		for y := laneY + 1; y <= v.y-2; y++ {
			cv.addArms(tx, y, armN|armS)
		}
		cv.setGlyph(tx, v.y-1, '▼')
		cv.addArms(tx, v.y, armN)
		return
	}
	for y := laneY + 1; y <= v.y-3; y++ {
		cv.addArms(tx, y, armN|armS)
	}
	cv.addArms(tx, v.y-2, armN)
	hjog(tx, v.cx(), v.y-2)
	cv.setGlyph(v.cx(), v.y-1, '▼')
	cv.addArms(v.cx(), v.y, armN)
}

func freeColumn(cv *mmdCanvas, y0, y1, pref int) (int, bool) {
	for d := 0; d <= cv.w; d++ {
		for _, c := range []int{pref - d, pref + d} {
			if c >= 0 && c < cv.w && corridorClear(cv, y0, y1, c) {
				return c, true
			}
		}
	}
	return 0, false
}

func renderFlowLR(g *mmdGraph) (string, bool) {
	rows, ranks, back, ok := flowRows(g)
	if !ok {
		return "", false
	}
	labels := map[string][]string{}
	boxOf := map[string]mmdBox{}
	for _, n := range g.nodes {
		ls := wrapLabel(n.label, mmdMaxLabel)
		labels[n.id] = ls
		boxOf[n.id] = mmdBoxFor(ls)
	}
	colW := make([]int, len(rows))
	colH := make([]int, len(rows))
	for c, col := range rows {
		for k, n := range col {
			if k > 0 {
				colH[c] += mmdLRVGap
			}
			colH[c] += boxOf[n.id].h
			colW[c] = max(colW[c], boxOf[n.id].w)
		}
	}
	gapHasLabel := make([]bool, len(rows))
	for _, e := range g.edges {
		if e.label != "" {
			gapHasLabel[ranks[g.pos[e.from]]] = true
		}
	}
	gaps := make([]int, len(rows))
	for c := range rows {
		gaps[c] = mmdLRGap
	}
	for _, e := range g.edges {
		c := ranks[g.pos[e.from]]
		if lw := ansi.StringWidth(e.label); c < len(rows)-1 {
			gaps[c] = max(gaps[c], lw+4)
		}
	}
	x := 0
	colX := make([]int, len(rows))
	for c := range rows {
		colX[c] = x
		x += colW[c]
		if c < len(rows)-1 {
			x += gaps[c]
		}
	}
	W := x
	laneX := make([]int, len(rows))
	headX := make([]int, len(rows))
	for c := range rows {
		r := colX[c] + colW[c] - 1
		laneX[c] = r + 1
		headX[c] = r + gaps[c]
	}
	H := 0
	for _, h := range colH {
		H = max(H, h)
	}
	cv := newMmdCanvas(W, H)
	for c, col := range rows {
		yy := (H - colH[c]) / 2
		for _, n := range col {
			b := boxOf[n.id]
			b.x, b.y = colX[c], yy
			boxOf[n.id] = b
			drawMmdBox(cv, b, labels[n.id], true, n.shape == shapeCylinder)
			yy += b.h + mmdLRVGap
		}
	}
	var jobs []labelJobLR
	for ei, e := range g.edges {
		if back[ei] {
			continue
		}
		u, v := boxOf[e.from], boxOf[e.to]
		c := ranks[g.pos[e.from]]
		sy := clampInt(u.cy(), u.y+1, u.bot()-1)
		ty := clampInt(v.cy(), v.y+1, v.bot()-1)
		drawEdgeLR(cv, u, v, sy, ty, laneX[c], headX[c])
		if e.label != "" && c < len(rows)-1 {
			jobs = append(jobs, labelJobLR{
				text: e.label,
				x:    clampInt((u.right()+1+v.x)/2-ansi.StringWidth(e.label)/2, u.right()+1, max(v.x-ansi.StringWidth(e.label), u.right()+1)),
				y:    sy,
				rows: [2]int{sy, ty},
			})
		}
	}
	for _, j := range jobs {
		cv.put(j.x, lrLabelRow(cv, j), j.text)
	}
	return cv.String(), true
}

type labelJobLR struct {
	text string
	x    int
	y    int
	rows [2]int
}

func lrLabelRow(cv *mmdCanvas, j labelJobLR) int {
	lw := ansi.StringWidth(j.text)
	for _, ry := range [6]int{j.rows[0], j.rows[1], j.rows[0] - 1, j.rows[1] - 1, j.rows[0] + 1, j.rows[1] + 1} {
		if ry < 0 || ry >= len(cv.c) {
			continue
		}
		clear := true
		for k := j.x; k < j.x+lw; k++ {
			c := cv.c[ry][k]
			if c.lock || c.glyph != 0 || c.arms != 0 {
				clear = false
				break
			}
		}
		if clear {
			return ry
		}
	}
	return j.rows[0]
}

func drawEdgeLR(cv *mmdCanvas, u, v mmdBox, sy, ty, laneX, headX int) {
	cv.addArms(u.right(), sy, armE)
	down := ty > sy
	for x := u.right() + 1; x < laneX; x++ {
		cv.addArms(x, sy, armE|armW)
	}
	turn := byte(armS | armW)
	if !down {
		turn = armN | armW
	}
	if sy == ty {
		cv.addArms(laneX, sy, armE|armW)
	} else {
		cv.addArms(laneX, sy, turn)
		step := 1
		arrive := byte(armN | armE)
		if !down {
			step = -1
			arrive = armS | armE
		}
		for y := sy + step; y != ty; y += step {
			cv.addArms(laneX, y, armN|armS)
		}
		cv.addArms(laneX, ty, arrive)
	}
	for x := laneX + 1; x < headX; x++ {
		cv.addArms(x, ty, armE|armW)
	}
	cv.setGlyph(headX, ty, '▶')
	cv.addArms(v.x, ty, armW)
}

func layoutFlowLR(lines []string) (string, bool) {
	g, ok := parseFlow(lines)
	if !ok {
		return "", false
	}
	return renderFlowLR(g)
}

type seqActor struct {
	label string
	box   mmdBox
	cx    int
}

type seqItem struct {
	a, b   *seqActor
	text   string
	dashed bool
	note   bool
}

func parseSequence(lines []string) ([]*seqActor, []seqItem, bool) {
	actors := []*seqActor{}
	pos := map[string]int{}
	actor := func(name string) *seqActor {
		if i, ok := pos[name]; ok {
			return actors[i]
		}
		a := &seqActor{label: name}
		pos[name] = len(actors)
		actors = append(actors, a)
		return a
	}
	var items []seqItem
	for _, l := range lines {
		switch {
		case l == "autonumber":
			continue
		case strings.HasPrefix(l, "participant "):
			f := strings.Fields(l)
			if len(f) < 2 || !isID(f[1]) {
				return nil, nil, false
			}
			a := actor(f[1])
			switch {
			case len(f) == 2:
			case len(f) >= 4 && f[2] == "as":
				a.label = strings.Join(f[3:], " ")
			default:
				return nil, nil, false
			}
		case strings.HasPrefix(l, "Note over "):
			rest := strings.TrimSpace(l[len("Note over "):])
			ci := strings.Index(rest, ":")
			if ci < 0 {
				return nil, nil, false
			}
			text := strings.TrimSpace(rest[ci+1:])
			if text == "" {
				return nil, nil, false
			}
			parts := strings.Split(strings.TrimSpace(rest[:ci]), ",")
			if len(parts) > 2 {
				return nil, nil, false
			}
			for _, p := range parts {
				if !isID(strings.TrimSpace(p)) {
					return nil, nil, false
				}
			}
			a := actor(strings.TrimSpace(parts[0]))
			b := a
			if len(parts) == 2 {
				b = actor(strings.TrimSpace(parts[1]))
			}
			items = append(items, seqItem{a: a, b: b, text: text, note: true})
		default:
			sep, dashed := -1, false
			tok := ""
			if i := strings.Index(l, "-->>"); i >= 0 {
				sep, dashed, tok = i, true, "-->>"
			} else if i := strings.Index(l, "->>"); i >= 0 {
				sep, tok = i, "->>"
			}
			if sep <= 0 {
				return nil, nil, false
			}
			ci := strings.Index(l, ":")
			if ci < sep {
				return nil, nil, false
			}
			from := strings.TrimSpace(l[:sep])
			to := strings.TrimSpace(l[sep+len(tok) : ci])
			text := strings.TrimSpace(l[ci+1:])
			if !isID(from) || !isID(to) || from == to {
				return nil, nil, false
			}
			items = append(items, seqItem{a: actor(from), b: actor(to), text: text, dashed: dashed})
		}
	}
	if len(actors) == 0 {
		return nil, nil, false
	}
	return actors, items, true
}

func isID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isIDRune(r) {
			return false
		}
	}
	return true
}

func layoutSequence(lines []string) (string, bool) {
	actors, items, ok := parseSequence(lines)
	if !ok {
		return "", false
	}
	H0 := 0
	for _, a := range actors {
		a.box = mmdBoxFor(wrapLabel(a.label, mmdMaxLabel))
		H0 = max(H0, a.box.h)
	}
	x := 0
	for _, a := range actors {
		a.box.x = x
		a.box.y = 0
		a.cx = x + a.box.w/2
		x += a.box.w + mmdSeqGap
	}
	W := x - mmdSeqGap
	y := H0
	type place struct {
		item  seqItem
		y     int
		noteH int
	}
	var places []place
	for _, it := range items {
		if it.note {
			hh := len(wrapLabel(it.text, mmdNoteCap)) + 2
			places = append(places, place{item: it, y: y, noteH: hh})
			y += hh + 1
		} else {
			places = append(places, place{item: it, y: y})
			y += 3
		}
	}
	cv := newMmdCanvas(max(W, 1), max(y, 1))
	for _, a := range actors {
		drawMmdBox(cv, a.box, wrapLabel(a.label, mmdMaxLabel), true, false)
	}
	for yy := H0; yy < y; yy++ {
		for _, a := range actors {
			cv.addArms(a.cx, yy, armN|armS)
		}
	}
	for _, p := range places {
		it := p.item
		if it.note {
			lines := wrapLabel(it.text, mmdNoteCap)
			bw := 1
			for _, ln := range lines {
				bw = max(bw, ansi.StringWidth(ln))
			}
			bw += 2
			mid := (it.a.cx + it.b.cx) / 2
			bx := clampInt(mid-bw/2, 0, max(W-bw, 0))
			b := mmdBox{x: bx, y: p.y, w: bw, h: p.noteH}
			cv.clearRect(b)
			drawMmdBox(cv, b, lines, false, false)
			continue
		}
		lw := ansi.StringWidth(it.text)
		mid := (it.a.cx + it.b.cx) / 2
		cv.put(clampInt(mid-lw/2, 0, max(W-lw, 0)), p.y, it.text)
		ar := p.y + 1
		lo, hi, headX := it.a.cx, it.b.cx, it.b.cx
		head := '▶'
		if it.dashed {
			head = '▷'
		}
		if it.b.cx < it.a.cx {
			lo, hi, headX = it.b.cx, it.a.cx, it.a.cx
			head = '◀'
			if it.dashed {
				head = '◁'
			}
		}
		cv.addArms(it.a.cx, ar, armE)
		cv.addArms(it.b.cx, ar, armW)
		if lo == hi {
			continue
		}
		for cx := lo; cx <= hi; cx++ {
			cv.addArms(cx, ar, armE|armW)
		}
		cv.setGlyph(headX, ar, head)
	}
	return cv.String(), true
}

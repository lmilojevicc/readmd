package pager

import (
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	astext "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

func substituteMath(src string) string {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	var prot [][2]int
	type chunk struct{ start, stop int }
	var chunks []chunk
	addProt := func(s, e int) {
		if e > s {
			prot = append(prot, [2]int{s, e})
		}
	}
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
			ls := n.Lines()
			if ls.Len() > 0 {
				addProt(ls.At(0).Start, ls.At(ls.Len()-1).Stop)
			}
		case *ast.RawHTML:
			for i := 0; i < n.Segments.Len(); i++ {
				s := n.Segments.At(i)
				addProt(s.Start, s.Stop)
			}
		case *ast.AutoLink:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					addProt(t.Segment.Start, t.Segment.Stop)
				}
			}
		case *ast.CodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					addProt(t.Segment.Start, t.Segment.Stop)
				}
			}
		case *ast.Link, *ast.Image:
			lo, hi := -1, -1
			var visit func(ast.Node)
			visit = func(n ast.Node) {
				for c := n.FirstChild(); c != nil; c = c.NextSibling() {
					if t, ok := c.(*ast.Text); ok {
						if lo < 0 || t.Segment.Start < lo {
							lo = t.Segment.Start
						}
						if t.Segment.Stop > hi {
							hi = t.Segment.Stop
						}
					}
					visit(c)
				}
			}
			visit(n)
			i := hi
			if i < 0 {
				break
			}
			for i < len(bsrc) && (bsrc[i] == ' ' || bsrc[i] == '\t' || bsrc[i] == '\n') {
				i++
			}
			if i >= len(bsrc) || bsrc[i] != ']' {
				break
			}
			i++
			for i < len(bsrc) && (bsrc[i] == ' ' || bsrc[i] == '\t') {
				i++
			}
			if i >= len(bsrc) || bsrc[i] != '(' {
				break
			}
			i++
			depth := 1
			j := i
			for ; j < len(bsrc) && depth > 0; j++ {
				switch bsrc[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			if depth == 0 {
				addProt(i, j-1)
			}
		case *ast.Paragraph, *ast.Heading, *astext.TableCell, *astext.DefinitionTerm, *ast.TextBlock:
			ls := n.Lines()
			if ls.Len() == 0 {
				break
			}
			chunks = append(chunks, chunk{ls.At(0).Start, ls.At(ls.Len() - 1).Stop})
		}
		return ast.WalkContinue, nil
	})
	if len(chunks) == 0 {
		return src
	}
	sort.Slice(prot, func(i, j int) bool { return prot[i][0] < prot[j][0] })
	merged := prot[:0]
	for _, p := range prot {
		if n := len(merged); n > 0 && p[0] <= merged[n-1][1] {
			if p[1] > merged[n-1][1] {
				merged[n-1][1] = p[1]
			}
			continue
		}
		merged = append(merged, p)
	}
	prot = merged
	var edits []edit
	for _, c := range chunks {
		edits = append(edits, mathChunkEdits(src, prot, c.start, c.stop)...)
	}
	if len(edits) == 0 {
		return src
	}
	return applyEdits(src, edits)
}

func mathChunkEdits(src string, prot [][2]int, start, stop int) []edit {
	mask := []byte(src[start:stop])
	pi := sort.Search(len(prot), func(i int) bool { return prot[i][1] > start })
	for ; pi < len(prot) && prot[pi][0] < stop; pi++ {
		lo, hi := max(start, prot[pi][0])-start, min(stop, prot[pi][1])-start
		for i := lo; i < hi; i++ {
			mask[i] = 0 // protected bytes blank to \x00: the $-scan below relies on \x00 to skip protected regions
		}
	}
	var edits []edit
	i := 0
	for i < len(mask) {
		if mask[i] != '$' {
			i++
			continue
		}
		if i > 0 && mask[i-1] == '\\' {
			i += 2
			continue
		}
		if i+1 < len(mask) && mask[i+1] == '$' {
			j := indexDouble(mask, i+2)
			if j < 0 {
				i++
				continue
			}
			content := strings.TrimSpace(string(mask[i+2 : j]))
			if content == "" ||
				strings.Contains(content, "$") ||
				strings.Contains(content, "\x00") ||
				strings.Contains(content, "\n\n") {
				i++
				continue
			}
			if out := texSub(content); out != content {
				edits = append(edits, edit{start + i, start + j + 2, out})
			}
			i = j + 2
			continue
		}
		j := i + 1
		for j < len(mask) && mask[j] != '$' {
			j++
		}
		if j >= len(mask) || j == i+1 {
			i++
			continue
		}
		content := mask[i+1 : j]
		if content[0] == ' ' || content[0] == '\t' ||
			content[len(content)-1] == ' ' || content[len(content)-1] == '\t' {
			i++
			continue
		}
		if strings.Contains(string(content), "\x00") || strings.Contains(string(content), "\n") {
			i++
			continue
		}
		if j+1 < len(mask) && mask[j+1] >= '0' && mask[j+1] <= '9' {
			i++
			continue
		}
		out := texSub(string(content))
		if out == string(content) {
			i++
			continue
		}
		edits = append(edits, edit{start + i, start + j + 1, out})
		i = j + 1
	}
	return edits
}

func indexDouble(b []byte, from int) int {
	for i := from; i+1 < len(b); i++ {
		if b[i] == '$' && b[i+1] == '$' {
			return i
		}
		if b[i] == '$' {
			i++
		}
	}
	return -1
}

var mathSymbols = map[string]string{
	"sum": "∑", "prod": "∏", "int": "∫", "infty": "∞",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ",
	"epsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ",
	"iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ",
	"nu": "ν", "xi": "ξ", "omicron": "ο", "pi": "π",
	"rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ",
	"Xi": "Ξ", "Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ",
	"Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"pm": "±", "times": "×", "cdot": "·", "div": "÷",
	"leq": "≤", "geq": "≥", "neq": "≠", "approx": "≈", "equiv": "≡",
	"rightarrow": "→", "leftarrow": "←", "Rightarrow": "⇒",
	"Leftrightarrow": "⇔", "mapsto": "↦",
	"in": "∈", "notin": "∉", "subset": "⊂", "subseteq": "⊆",
	"cup": "∪", "cap": "∩",
	"forall": "∀", "exists": "∃", "partial": "∂", "nabla": "∇",
	"ldots": "…", "cdots": "⋯",
}

var supMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
	'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'a': 'ᵃ', 'b': 'ᵇ', 'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ', 'f': 'ᶠ',
	'g': 'ᵍ', 'h': 'ʰ', 'i': 'ⁱ', 'j': 'ʲ', 'k': 'ᵏ', 'l': 'ˡ',
	'm': 'ᵐ', 'n': 'ⁿ', 'o': 'ᵒ', 'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ',
	't': 'ᵗ', 'u': 'ᵘ', 'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ',
	'z': 'ᶻ',
	'A': 'ᴬ', 'B': 'ᴮ', 'D': 'ᴰ', 'E': 'ᴱ', 'G': 'ᴳ', 'H': 'ᴴ',
	'I': 'ᴵ', 'J': 'ᴶ', 'K': 'ᴷ', 'L': 'ᴸ', 'M': 'ᴹ', 'N': 'ᴺ',
	'O': 'ᴼ', 'P': 'ᴾ', 'R': 'ᴿ', 'T': 'ᵀ', 'U': 'ᵁ', 'V': 'ⱽ',
	'W': 'ᵂ',
}

var subMap = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
	'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ',
	'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ', 'p': 'ₚ', 'r': 'ᵣ',
	's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ', 'v': 'ᵥ', 'x': 'ₓ',
}

func texSub(s string) string {
	return texRun([]rune(s))
}

func texRun(rs []rune) string {
	var b strings.Builder
	i := 0
	for i < len(rs) {
		switch r := rs[i]; {
		case r == '\\':
			val, next, ok := texCmd(rs, i)
			if !ok {
				b.WriteByte('\\')
				i++
				continue
			}
			b.WriteString(val)
			i = next
		case r == '^' || r == '_':
			arg, next, ok := texArg(rs, i+1)
			if !ok {
				b.WriteRune(r)
				i++
				continue
			}
			b.WriteString(texScript(arg, r == '^'))
			i = next
		case r == '{':
			inner, next, ok := texGroup(rs, i)
			if !ok {
				b.WriteRune(r)
				i++
				continue
			}
			b.WriteString(texRun(inner))
			i = next
		default:
			b.WriteRune(r)
			i++
		}
	}
	return b.String()
}

func texCmd(rs []rune, i int) (string, int, bool) {
	j := i + 1
	if j >= len(rs) {
		return "", 0, false
	}
	r := rs[j]
	if isTexLetter(r) {
		for j < len(rs) && isTexLetter(rs[j]) {
			j++
		}
		name := string(rs[i+1 : j])
		switch name {
		case "left", "right":
			return "", j, true
		case "sqrt":
			inner, next, ok := texGroupArg(rs, j)
			if !ok {
				return "\\" + name, j, true
			}
			return "√(" + texRun(inner) + ")", next, true
		case "frac":
			a, next, ok := texGroupArg(rs, j)
			if !ok {
				return "\\" + name, j, true
			}
			b, next2, ok := texGroupArg(rs, next)
			if !ok {
				return "\\frac" + string(a), next, true
			}
			return "(" + texRun(a) + "/" + texRun(b) + ")", next2, true
		}
		if v, ok := mathSymbols[name]; ok {
			return v, j, true
		}
		switch name {
		case "quad", "qquad":
			return " ", j, true
		}
		return "\\" + name, j, true
	}
	switch r {
	case ',', ';':
		return " ", j + 1, true
	case '!':
		return "", j + 1, true
	}
	return "\\" + string(r), j + 1, true
}

func isTexLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func texGroup(rs []rune, i int) ([]rune, int, bool) {
	if i >= len(rs) || rs[i] != '{' {
		return nil, 0, false
	}
	depth := 0
	for j := i; j < len(rs); j++ {
		switch rs[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rs[i+1 : j], j + 1, true
			}
		}
	}
	return nil, 0, false
}

func texGroupArg(rs []rune, i int) ([]rune, int, bool) {
	if i < len(rs) && rs[i] == '{' {
		return texGroup(rs, i)
	}
	return nil, 0, false
}

func texArg(rs []rune, i int) ([]rune, int, bool) {
	if i >= len(rs) {
		return nil, 0, false
	}
	if rs[i] == '{' {
		return texGroup(rs, i)
	}
	if rs[i] == '\\' {
		_, next, ok := texCmd(rs, i)
		if !ok {
			return nil, 0, false
		}
		return rs[i:next], next, true
	}
	return rs[i : i+1], i + 1, true
}

func texScript(rs []rune, super bool) string {
	sub := texRun(rs)
	m := subMap
	if super {
		m = supMap
	}
	out, ok := texMapAll([]rune(sub), m)
	if ok {
		return out
	}
	if super {
		return "^(" + sub + ")"
	}
	return "_(" + sub + ")"
}

func texMapAll(rs []rune, m map[rune]rune) (string, bool) {
	var b strings.Builder
	for _, r := range rs {
		mr, ok := m[r]
		if !ok {
			return "", false
		}
		b.WriteRune(mr)
	}
	return b.String(), true
}

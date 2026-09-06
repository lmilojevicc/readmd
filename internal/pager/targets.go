package pager

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	tea "charm.land/bubbletea/v2"
)

const hintAlphabet = "asdfghjklqwertyuiopzxcvbnm"

const targetHL = "\x1b[7m\x1b[4m"

type targetRegion struct {
	line       int
	start, end int
}

type targetKind uint8

const (
	targetExternal targetKind = iota
	targetFootnote
)

type linkTarget struct {
	kind       targetKind
	id         string
	dest       string
	footnote   string
	regions    []targetRegion
	texts      []string
	definition targetRegion
}

type hintTarget struct {
	linkTarget
	label string
}

type targetMode struct {
	active  bool
	prefix  string
	targets []hintTarget
	savedX  int
	savedY  int
}

type openedURLMsg struct {
	dest string
	err  error
}

type rawLinkRegion struct {
	id, dest, text string
	wrapsPrevious  bool
	targetRegion
}

// parseLinkTargets extracts logical links from cached rendered ANSI. OSC 8
// control sequences carry identity and destination; all positions are display
// columns produced by the same grapheme-aware decoder used by the viewport.
func parseLinkTargets(lines []string) []linkTarget {
	return coalesceLinkRegions(rawLinkRegions(lines))
}

func rawLinkRegions(lines []string) []rawLinkRegion {
	var raw []rawLinkRegion
	for line, content := range lines {
		col, st := 0, byte(0)
		var active *rawLinkRegion
		for rest := content; rest != ""; {
			seq, w, n, ns := ansi.DecodeSequence(rest, st, nil)
			if n <= 0 {
				break
			}
			st = ns
			rest = rest[n:]
			if params, dest, ok := parseOSC8(seq); ok {
				if dest == "" {
					if active != nil && col > active.start {
						active.end = col
						raw = append(raw, *active)
					}
					active = nil
					continue
				}
				active = &rawLinkRegion{
					id:           osc8ID(params),
					dest:         dest,
					targetRegion: targetRegion{line: line, start: col},
				}
				continue
			}
			if active != nil && w > 0 {
				active.text += seq
			}
			col += w
		}
		// An unterminated OSC 8 span is malformed and is deliberately ignored.
	}
	for i := 1; i < len(raw); i++ {
		prev, next := raw[i-1], &raw[i]
		// Only whitespace may surround a continued line. Table rails and
		// other container text must not merge separate links across rows.
		next.wrapsPrevious = next.line == prev.line+1 &&
			strings.TrimSpace(ansi.Strip(ansi.TruncateLeft(lines[prev.line], prev.end, ""))) == "" &&
			strings.TrimSpace(ansi.Strip(ansi.Cut(lines[next.line], 0, next.start))) == ""
	}
	return raw
}

func parseRenderedLinkTargets(src string, lines []string) []linkTarget {
	raw := rawLinkRegions(lines)
	layout := sourceLinkLayout(src)
	if len(raw) == 0 || len(layout) == 0 {
		return coalesceLinkRegions(raw)
	}
	var out []linkTarget
	ri := 0
	for _, source := range layout {
		if ri >= len(raw) || raw[ri].dest != source.dest {
			return coalesceLinkRegions(raw)
		}
		first := raw[ri]
		target := linkTarget{id: first.id, dest: first.dest}
		remaining := compactLinkText(source.text)
		if source.table {
			// Intrinsic table cells emit one OSC span, with a stock footnote
			// suffix rather than the prose label-plus-destination layout.
			remaining = compactLinkText(first.text)
		}
		for remaining != "" && ri < len(raw) {
			next := raw[ri]
			part := compactLinkText(next.text)
			if next.id != first.id || next.dest != first.dest || !strings.HasPrefix(remaining, part) {
				return coalesceLinkRegions(raw)
			}
			target.regions = append(target.regions, next.targetRegion)
			target.texts = append(target.texts, ansi.Strip(next.text))
			remaining = strings.TrimPrefix(remaining, part)
			ri++
		}
		if remaining != "" {
			return coalesceLinkRegions(raw)
		}
		out = append(out, target)
	}
	if ri != len(raw) {
		return coalesceLinkRegions(raw)
	}
	return out
}

type sourceLink struct {
	dest, text string
	table      bool
}

func sourceLinkLayout(src string) []sourceLink {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	var out []sourceLink
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		table := false
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Kind() == extast.KindTable {
				table = true
				break
			}
		}
		switch n := n.(type) {
		case *ast.Link:
			out = append(out, sourceLink{dest: string(n.Destination), text: plainText(n, bsrc) + printedLinkDestination(string(n.Destination)), table: table})
		case *ast.AutoLink:
			dest := string(n.URL(bsrc))
			if n.AutoLinkType == ast.AutoLinkEmail {
				dest = "mailto:" + dest
			}
			out = append(out, sourceLink{dest: dest, text: string(n.URL(bsrc)), table: table})
		}
		return ast.WalkContinue, nil
	})
	return out
}

// Glamour prints resolved relative paths, but keeps the original OSC target.
func printedLinkDestination(dest string) string {
	u, err := url.Parse(dest)
	if err != nil || dest == "#"+u.Fragment {
		return ""
	}
	if !u.IsAbs() {
		return new(url.URL).ResolveReference(u).String()
	}
	return dest
}

// Wrapping adds or removes whitespace between OSC spans, not label characters.
func compactLinkText(s string) string {
	return strings.Join(strings.Fields(ansi.Strip(s)), "")
}

func parseOSC8(seq string) (params, dest string, ok bool) {
	var body string
	switch {
	case strings.HasPrefix(seq, "\x1b]8;"):
		body = seq[len("\x1b]8;"):]
	case strings.HasPrefix(seq, "\x9d8;"):
		body = seq[len("\x9d8;"):]
	default:
		return "", "", false
	}
	switch {
	case strings.HasSuffix(body, "\x1b\\"):
		body = strings.TrimSuffix(body, "\x1b\\")
	case strings.HasSuffix(body, "\x9c"):
		body = strings.TrimSuffix(body, "\x9c")
	case strings.HasSuffix(body, "\a"):
		body = strings.TrimSuffix(body, "\a")
	default:
		return "", "", false
	}
	params, dest, ok = strings.Cut(body, ";")
	return params, dest, ok
}

func osc8ID(params string) string {
	for _, field := range strings.Split(params, ":") {
		if id, ok := strings.CutPrefix(field, "id="); ok {
			return id
		}
	}
	return ""
}

// coalesceLinkRegions joins Glamour's separately wrapped label and printed
// destination spans. Identity includes both OSC id and destination; completion
// after the printed destination keeps repeated links to one URL distinct.
func coalesceLinkRegions(raw []rawLinkRegion) []linkTarget {
	var out []linkTarget
	complete := true
	remaining := ""
	for _, reg := range raw {
		text := ansi.Strip(reg.text)
		join := false
		if len(out) > 0 && !complete {
			last := &out[len(out)-1]
			prev := last.regions[len(last.regions)-1]
			join = last.id == reg.id && last.dest == reg.dest &&
				((prev.line == reg.line && reg.start-prev.end <= 1) || reg.wrapsPrevious)
		}
		if !join {
			out = append(out, linkTarget{id: reg.id, dest: reg.dest})
			complete = renderedAutolink(reg.dest, text)
			remaining = compactLinkText(printedLinkDestination(reg.dest))
		}
		last := &out[len(out)-1]
		last.regions = append(last.regions, reg.targetRegion)
		last.texts = append(last.texts, text)
		if join {
			part := compactLinkText(text)
			if strings.HasPrefix(remaining, part) {
				remaining = strings.TrimPrefix(remaining, part)
				complete = remaining == ""
			} else {
				remaining = compactLinkText(printedLinkDestination(reg.dest))
			}
		}
	}
	return out
}

func renderedAutolink(dest, text string) bool {
	if text == dest {
		return true
	}
	return strings.HasPrefix(strings.ToLower(dest), "mailto:") &&
		strings.EqualFold(strings.TrimPrefix(dest, "mailto:"), text)
}

func hintLabels(n int) []string {
	if n <= 0 {
		return nil
	}
	base := len(hintAlphabet)
	width := 1
	if n > base {
		width = 2
	}
	maxLabels := base
	if width == 2 {
		maxLabels *= base
	}
	if n > maxLabels {
		n = maxLabels
	}
	out := make([]string, n)
	for i := range n {
		if width == 1 {
			out[i] = strings.ToUpper(hintAlphabet[i : i+1])
			continue
		}
		out[i] = strings.ToUpper(string([]byte{
			hintAlphabet[(i/base)%base],
			hintAlphabet[i%base],
		}))
	}
	return out
}

func visibleLinkTargets(links []linkTarget, x, y, width, height int) []linkTarget {
	if width <= 0 || height <= 0 {
		return nil
	}
	var out []linkTarget
	for _, link := range links {
		var visible []targetRegion
		for _, reg := range link.regions {
			if reg.line < y || reg.line >= y+height || reg.end <= x || reg.start >= x+width {
				continue
			}
			visible = append(visible, reg)
		}
		if len(visible) > 0 {
			link.regions = visible
			out = append(out, link)
		}
	}
	return out
}

func (m *Model) openTargets() {
	if m.srcView || m.base == nil {
		m.flash = "targets unavailable"
		return
	}
	if m.vp.Height() < 2 {
		m.flash = "not enough room for targets"
		return
	}
	savedX, savedY := m.vp.XOffset(), m.vp.YOffset()
	m.targets = targetMode{active: true, savedX: savedX, savedY: savedY}
	m.syncVPHeight()
	m.vp.SetXOffset(savedX)
	m.vp.SetYOffset(savedY)
	links := visibleLinkTargets(m.links, savedX, savedY, m.vp.Width(), m.vp.Height())
	sort.SliceStable(links, func(i, j int) bool {
		a, b := links[i].regions[0], links[j].regions[0]
		if a.line != b.line {
			return a.line < b.line
		}
		return a.start < b.start
	})
	labels := hintLabels(len(links))
	if len(labels) == 0 {
		m.targets = targetMode{}
		m.syncVPHeight()
		m.vp.SetXOffset(savedX)
		m.vp.SetYOffset(savedY)
		m.flash = "no visible targets"
		return
	}
	links = links[:len(labels)]
	hints := make([]hintTarget, len(links))
	for i := range links {
		hints[i] = hintTarget{linkTarget: links[i], label: labels[i]}
	}
	m.targets.targets = hints
	m.applySearchView()
}

func (m *Model) stopTargets(refresh bool) {
	if !m.targets.active {
		return
	}
	st := m.targets
	m.targets = targetMode{}
	m.syncVPHeight()
	m.vp.SetXOffset(st.savedX)
	m.vp.SetYOffset(st.savedY)
	if refresh {
		m.applySearchView()
	}
}

func (m *Model) targetCandidates() []hintTarget {
	prefix := strings.ToUpper(m.targets.prefix)
	out := make([]hintTarget, 0, len(m.targets.targets))
	for _, target := range m.targets.targets {
		if strings.HasPrefix(target.label, prefix) {
			out = append(out, target)
		}
	}
	return out
}

func (m *Model) handleTargetKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.stopTargets(true)
		return nil
	case "backspace", "ctrl+h":
		if r := []rune(m.targets.prefix); len(r) > 0 {
			m.targets.prefix = string(r[:len(r)-1])
			m.applySearchView()
		}
		return nil
	}
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok || len(kp.Text) != 1 {
		return nil
	}
	r := unicode.ToLower([]rune(kp.Text)[0])
	if !strings.ContainsRune(hintAlphabet, r) {
		return nil
	}
	m.targets.prefix += string(r)
	candidates := m.targetCandidates()
	m.applySearchView()
	if len(candidates) == 1 {
		return m.activateTarget(candidates[0])
	}
	return nil
}

func (m *Model) activateTarget(target hintTarget) tea.Cmd {
	m.stopTargets(true)
	if target.kind == targetFootnote {
		m.jumpFootnote(target.linkTarget)
		return nil
	}
	dest := target.dest
	if err := validateExternalURL(dest); err != nil {
		u, _ := url.Parse(dest)
		switch {
		case strings.HasPrefix(dest, "#"):
			m.flash = "internal link navigation not implemented"
		case u != nil && (u.Scheme == "" || u.Scheme == "file"):
			m.flash = "file navigation not implemented"
		default:
			m.flash = "unsupported link"
		}
		return nil
	}
	opener := m.openURL
	return func() tea.Msg {
		return openedURLMsg{dest: dest, err: opener(dest)}
	}
}

func validateExternalURL(dest string) error {
	if dest == "" || strings.IndexFunc(dest, unicode.IsControl) >= 0 {
		return errors.New("invalid link")
	}
	u, err := url.Parse(dest)
	if err != nil {
		return errors.New("invalid link")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return errors.New("invalid web link")
		}
	case "mailto":
		if u.Opaque == "" && u.Path == "" {
			return errors.New("invalid mail link")
		}
	default:
		return errors.New("unsupported link scheme")
	}
	return nil
}

func openExternalURL(dest string) error {
	return externalURLCmd(runtime.GOOS, dest).Run()
}

func externalURLCmd(goos, dest string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", dest)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", dest)
	default:
		return exec.Command("xdg-open", dest)
	}
}

func (m *Model) hintStrip() string {
	strip, _ := m.hintStripLayout()
	return strip
}

func (m *Model) hintStripHits() []hintStripHit {
	_, hits := m.hintStripLayout()
	return hits
}

func (m *Model) hintStripLayout() (string, []hintStripHit) {
	candidates := m.targetCandidates()
	if len(candidates) == 0 {
		return ansi.Truncate("targets: no match", m.width, "…"), nil
	}
	const separator = "    "
	var b strings.Builder
	var pending []hintStripHit
	col := 0
	for i, target := range candidates {
		if i > 0 {
			b.WriteString(separator)
			col += ansi.StringWidth(separator)
		}
		part := target.label + " " + hintDescription(target)
		start := col
		b.WriteString(part)
		col += ansi.StringWidth(part)
		pending = append(pending, hintStripHit{start: start, end: col, target: target})
	}
	full := b.String()
	strip := ansi.Truncate(full, m.width, "…")
	limit := m.width
	if ansi.StringWidth(full) > m.width {
		limit = max(0, m.width-ansi.StringWidth("…"))
	}
	hits := pending[:0]
	for _, hit := range pending {
		if hit.end <= limit {
			hits = append(hits, hit)
		}
	}
	return strip, hits
}

func hintDescription(target hintTarget) string {
	if target.kind == targetFootnote {
		return "footnote:" + target.footnote
	}
	return targetDescription(target.dest)
}

func targetDescription(dest string) string {
	u, err := url.Parse(dest)
	if err != nil {
		return dest
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host + u.EscapedPath()
	case "mailto":
		return strings.TrimPrefix(dest, "mailto:")
	default:
		return dest
	}
}

func (m *Model) targetSpans() map[int][]span {
	if !m.targets.active {
		return nil
	}
	groups := map[int][]span{}
	for _, target := range m.targetCandidates() {
		for _, reg := range target.regions {
			groups[reg.line] = append(groups[reg.line], span{reg.start, reg.end})
		}
	}
	return groups
}

func (m *Model) handleOpenedURL(msg openedURLMsg) {
	m.stopTargets(true)
	if msg.err != nil {
		m.flash = "could not open link"
		return
	}
	m.flash = fmt.Sprintf("opened %s", targetDescription(msg.dest))
}

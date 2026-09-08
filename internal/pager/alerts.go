package pager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// GFM alert kinds: type-colored rail, bold colored `<icon> <Title>` heading,
// default-fg body. Default icons use the Nerd Fonts Octicons family.
var alertKinds = []struct {
	name  string
	icon  string
	title string
	sgr   string
}{
	{"note", "\uf449", "Note", "94"},
	{"tip", "\uf400", "Tip", "92"},
	{"important", "\uf50a", "Important", "95"},
	{"warning", "\uf421", "Warning", "93"},
	{"caution", "\uf46e", "Caution", "91"},
}

const alertTokenFmt = "readmd-alert-%s%d%s"

var errAlertSplice = errors.New("alert sentinel anomaly")

func barToken(sgr string) string { return "\x1b[" + sgr + "m│\x1b[m " }

// railSeq prefixes the document margin glamour applies around block quotes.
func railSeq(sgr string) string { return "  " + barToken(sgr) }

type alert struct {
	startTok, endTok string
	name, icon       string
	title            string
	sgr              string
}

func matchAlert(marker string) (alert, bool) {
	marker = strings.TrimSpace(marker)
	if !strings.HasPrefix(marker, "[!") || !strings.HasSuffix(marker, "]") {
		return alert{}, false
	}
	name := marker[2 : len(marker)-1]
	for _, k := range alertKinds {
		if strings.EqualFold(name, k.name) {
			return alert{name: k.name, icon: k.icon, title: k.title, sgr: k.sgr}, true
		}
	}
	return alert{}, false
}

// insertAlertSentinels isolates each detected GFM alert marker onto its own
// paragraph (splitting the quote at the marker line) and wraps the quote with
// nonce sentinel lines, so glamour renders a locatable region that later gets
// its title and rails restyled.
func insertAlertSentinels(src string) (string, []alert) {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	// 2 hex chars keep the 17-col sentinel inside narrow viewports; the
	// width-20 styling contract is pinned by TestAlertMinWidth.
	var nonce [1]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return src, nil
	}
	nonceStr := hex.EncodeToString(nonce[:])
	var edits []edit
	var alerts []alert
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		bq, ok := n.(*ast.Blockquote)
		if !ok || bq.Parent() == nil || bq.Parent().Kind() != ast.KindDocument {
			return ast.WalkContinue, nil
		}
		p, ok := bq.FirstChild().(*ast.Paragraph)
		if !ok || p.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		seg := p.Lines().At(0)
		a, ok := matchAlert(string(seg.Value(bsrc)))
		if !ok {
			return ast.WalkContinue, nil
		}
		first, last, ok := quoteExtent(bq, src)
		if !ok {
			return ast.WalkContinue, nil
		}
		i := len(alerts)
		a.startTok = fmt.Sprintf(alertTokenFmt, nonceStr, i, "s")
		a.endTok = fmt.Sprintf(alertTokenFmt, nonceStr, i, "e")
		for ls := first; ls <= last; {
			le := lineEnd(src, ls)
			line := strings.TrimLeft(src[ls:le], " \t")
			if !strings.HasPrefix(line, ">") {
				if strings.TrimSpace(line) == "" {
					edits = append(edits, edit{ls, ls, ">"})
				} else {
					edits = append(edits, edit{ls, ls, "> "})
				}
			}
			if le >= len(src) || src[le] != '\n' {
				break
			}
			ls = le + 1
		}
		if nl := strings.IndexByte(src[first:last], '\n'); nl >= 0 {
			pos := first + nl
			edits = append(edits, edit{pos, pos + 1, "\n>\n"})
		}
		edits = append(edits, edit{first, first, sepBefore(src, first) + a.startTok + "\n\n"})
		end := last
		if end < len(src) && src[end] == '\n' {
			end++
		}
		edits = append(edits, edit{end, end, "\n\n" + a.endTok + "\n" + sepAfter(src, end)})
		alerts = append(alerts, a)
		return ast.WalkContinue, nil
	})
	if len(edits) == 0 {
		return src, nil
	}
	return applyEdits(src, edits), alerts
}

// quoteExtent returns the full-line source range of the blockquote, declining
// quotes containing non-paragraph blocks (fences, tables, HTML, headings…).
func quoteExtent(bq *ast.Blockquote, src string) (int, int, bool) {
	minS, maxE := -1, -1
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				ls := c.Lines()
				for i := range ls.Len() {
					seg := ls.At(i)
					if minS < 0 || seg.Start < minS {
						minS = seg.Start
					}
					if seg.Stop > maxE {
						maxE = seg.Stop
					}
				}
			case *ast.List, *ast.ListItem:
				if !walk(c) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !walk(bq) || minS < 0 || maxE < minS {
		return 0, 0, false
	}
	return lineStart(src, minS), lineEnd(src, maxE), true
}

// spliceAlerts locates each alert's sentinel pair, drops the sentinels, and
// restyles the enclosed region: the marker line becomes the icon+title header
// and every rail glyph takes the type color. Any sentinel anomaly bails so the
// caller can re-render plainly.
func spliceAlerts(out string, alerts []alert) (string, bool) {
	return spliceThemedAlerts(out, alerts, paletteStyleName, config.Callouts{})
}

func spliceThemedAlerts(out string, alerts []alert, base string, theme config.Callouts) (string, bool) {
	if len(alerts) == 0 {
		return out, true
	}
	lines := strings.Split(out, "\n")
	stripped := make([]string, len(lines))
	for i, l := range lines {
		stripped[i] = strings.TrimSpace(ansi.Strip(l))
	}
	type span struct{ from, to int }
	spans := make([]span, len(alerts))
	prev := -1
	for i, a := range alerts {
		from, to := -1, -1
		for j, s := range stripped {
			switch s {
			case a.startTok:
				if from >= 0 {
					return out, false
				}
				from = j
			case a.endTok:
				if to >= 0 {
					return out, false
				}
				to = j
			}
		}
		if from < 0 || to <= from || from <= prev {
			return out, false
		}
		spans[i] = span{from, to}
		prev = to
	}
	for _, s := range stripped {
		if strings.Contains(s, "readmd-alert-") {
			known := false
			for _, a := range alerts {
				if s == a.startTok || s == a.endTok {
					known = true
					break
				}
			}
			if !known {
				return out, false
			}
		}
	}
	var outLines []string
	done := 0
	for i, sp := range spans {
		outLines = append(outLines, lines[done:sp.from]...)
		region, ok := styleAlert(lines[sp.from+1:sp.to], alerts[i])
		if theme != (config.Callouts{}) {
			region, ok = styleCustomAlert(lines[sp.from+1:sp.to], alerts[i], base, theme)
		}
		if !ok {
			return out, false
		}
		outLines = append(outLines, region...)
		done = sp.to + 1
	}
	outLines = append(outLines, lines[done:]...)
	return strings.Join(outLines, "\n"), true
}

func styleAlert(rows []string, a alert) ([]string, bool) {
	rows = trimEdgeRows(rows)
	styled := make([]string, 0, len(rows))
	titleDone := false
	for i := 0; i < len(rows); i++ {
		l := rows[i]
		s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ansi.Strip(l)), "│"))
		if !titleDone && strings.EqualFold(s, "[!"+a.name+"]") {
			styled = append(styled, railSeq(a.sgr)+"\x1b["+a.sgr+";1m"+alertTitle(a.icon, a.title)+"\x1b[m")
			titleDone = true
			if i+1 < len(rows) {
				if s := strings.TrimSpace(ansi.Strip(rows[i+1])); s == "" || s == "│" {
					i++
				}
			}
			continue
		}
		styled = append(styled, strings.ReplaceAll(l, railSeq(quoteBarSGR), railSeq(a.sgr)))
	}
	return styled, titleDone
}

func alertTitle(icon, title string) string {
	return icon + " " + title
}

func trimEdgeRows(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(ansi.Strip(rows[0])) == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func styleCustomAlert(rows []string, a alert, base string, theme config.Callouts) ([]string, bool) {
	specific := map[string]config.Callout{"note": theme.Note, "tip": theme.Tip, "important": theme.Important, "warning": theme.Warning, "caution": theme.Caution}[a.name]
	rail := "│"
	if theme.Preset != nil {
		switch *theme.Preset {
		case "unicode":
			a.icon = map[string]string{"note": "ⓘ", "tip": "✦", "important": "✱", "warning": "⚠", "caution": "✖"}[a.name]
		case "nerd":
			rail = "▋"
		}
	}
	if theme.Rail.Glyph != nil {
		rail = *theme.Rail.Glyph
	}
	if specific.Rail.Glyph != nil {
		rail = *specific.Rail.Glyph
	}
	if specific.Icon != nil {
		a.icon = *specific.Icon
	}
	color := config.Color(map[string]string{"note": "12", "tip": "10", "important": "13", "warning": "11", "caution": "9"}[a.name])
	railStyle := mergeText(mergeText(config.TextStyle{FG: &color}, theme.Rail.TextStyle), specific.Rail.TextStyle)
	titleStyle := mergeText(mergeText(config.TextStyle{FG: &color, Bold: boolPtr(true)}, theme.Title), specific.Title)
	notty := base == "notty" || base == ""
	rail = textSGR(railStyle, notty) + rail + "\x1b[m"
	title := textSGR(titleStyle, notty) + alertTitle(a.icon, a.title) + "\x1b[m"
	rows = trimEdgeRows(rows)
	var result []string
	done, afterTitle := false, false
	for _, line := range rows {
		plain := ansi.Strip(line)
		trimmed := strings.TrimLeft(plain, " ")
		if !strings.HasPrefix(trimmed, "│") && !strings.HasPrefix(trimmed, "|") {
			return nil, false
		}
		col := len(plain) - len(trimmed)
		railBytes := len("│")
		if strings.HasPrefix(trimmed, "|") {
			railBytes = 1
		}
		content := strings.TrimSpace(trimmed[railBytes:])
		prefix := ansi.Cut(line, 0, col) + rail + " "
		if !done && strings.EqualFold(content, "[!"+a.name+"]") {
			result = append(result, prefix+title)
			done, afterTitle = true, true
			continue
		}
		if afterTitle {
			afterTitle = false
			if content == "" {
				continue
			}
		}
		result = append(result, prefix+ansi.Cut(line, col+2, ansi.StringWidth(line)))
	}
	return result, done
}

package pager

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	glamansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
)

// Chroma's public registries are unsynchronized, including stock reads. Hold
// this lock over registration AND the complete Glamour/table render, not merely
// Register. Render commands remain off the UI thread; snapshots are immutable.
var chromaRenderMu sync.Mutex

func configureChroma(o *glamansi.Options, base string, t config.Theme) error {
	rules := &o.Styles.CodeBlock
	if rules.Chroma != nil {
		name := "readmd-" + base
		if _, ok := chromastyles.Registry[name]; !ok {
			entries := chroma.StyleEntries{}
			values := reflect.ValueOf(*rules.Chroma)
			for i := 0; i < values.NumField(); i++ {
				token, err := chroma.TokenTypeString(values.Type().Field(i).Name)
				if err != nil {
					return err
				}
				st := values.Field(i).Interface().(glamansi.StylePrimitive)
				var parts []string
				if st.Color != nil {
					parts = append(parts, *st.Color)
				}
				if st.BackgroundColor != nil {
					parts = append(parts, "bg:"+*st.BackgroundColor)
				}
				if st.Bold != nil && *st.Bold {
					parts = append(parts, "bold")
				}
				if st.Italic != nil && *st.Italic {
					parts = append(parts, "italic")
				}
				if st.Underline != nil && *st.Underline {
					parts = append(parts, "underline")
				}
				entries[token] = strings.Join(parts, " ")
			}
			style, err := chroma.NewStyle(name, entries)
			if err != nil {
				return err
			}
			chromastyles.Register(style)
		}
		rules.Theme, rules.Chroma = name, nil
	}
	if t.CodeBlock.BG == nil || base == "notty" || base == "" {
		return nil
	}
	innerName := o.ChromaFormatter
	if innerName == "" {
		innerName = "terminal256"
	}
	bg := string(*t.CodeBlock.BG)
	name := "readmd-background-" + innerName + "-" + bg
	if _, ok := formatters.Registry[name]; !ok {
		formatters.Register(name, codeBackgroundFormatter(formatters.Get(innerName), bg))
	}
	o.ChromaFormatter = name
	return nil
}

// The formatter sees only AST-identified code, before Glamour's indent/margin
// writers. Inner lexer/formatter foregrounds are retained byte-for-byte.
func codeBackgroundFormatter(inner chroma.Formatter, bg string) chroma.Formatter {
	return chroma.FormatterFunc(func(w io.Writer, style *chroma.Style, it chroma.Iterator) error {
		var raw bytes.Buffer
		if err := inner.Format(&raw, style, it); err != nil {
			return err
		}
		content := raw.String()
		paint := bg != "none"
		background := ansi.Style{}.BackgroundColor(nil).String()
		if paint {
			background = ansi.Style{}.BackgroundColor(lipgloss.Color(bg)).String()
			content = expandTableTabs(content)
		}
		trailing := strings.HasSuffix(content, "\n")
		lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		width := 0
		if paint {
			for _, line := range lines {
				width = max(width, ansi.StringWidth(line))
			}
		}
		for i, line := range lines {
			var out strings.Builder
			out.WriteString(background)
			state := byte(0)
			for rest := line; rest != ""; {
				seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
				if n <= 0 {
					return fmt.Errorf("code background: invalid ANSI")
				}
				rest, state = rest[n:], next
				out.WriteString(seq)
				if isSGR(seq) {
					out.WriteString(background)
				}
			}
			if paint {
				out.WriteString(strings.Repeat(" ", width-ansi.StringWidth(line)))
			}
			out.WriteString("\x1b[49m")
			if i < len(lines)-1 || trailing {
				out.WriteByte('\n')
			}
			if _, err := io.WriteString(w, out.String()); err != nil {
				return err
			}
		}
		return nil
	})
}

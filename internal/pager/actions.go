package pager

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const maxCopyBytes = 10 << 20

// copyRaw hands the raw markdown to the system clipboard via OSC 52
// (bubbletea's native clipboard command), which works over SSH without any
// external tool. Oversized documents decline with a flash instead.
func (m *Model) copyRaw() tea.Cmd {
	if len(m.source) > maxCopyBytes {
		m.flash = "too large to copy"
		return nil
	}
	m.flash = "copied"
	return tea.SetClipboard(m.source)
}

type editedMsg struct{ err error }

// editDoc suspends the TUI and opens the document in $VISUAL/$EDITOR (vi as
// fallback), positioned at the nearest heading above the viewport top via the
// vi-family +N argv convention. File documents only.
func (m *Model) editDoc() tea.Cmd {
	if m.path == "" {
		m.flash = "cannot edit stdin"
		return nil
	}
	m.edited = true
	c := editorCmd(m.cursorSrcLine(), m.path)
	return tea.ExecProcess(c, func(err error) tea.Msg { return m.result(editedMsg{err}) })
}

// editorArgv resolves $VISUAL/$EDITOR (vi as fallback) into program plus
// leading arguments — the less/vim convention that lets VISUAL carry flags.
func editorArgv() (string, []string) {
	for _, e := range []string{"VISUAL", "EDITOR"} {
		if f := strings.Fields(os.Getenv(e)); len(f) > 0 {
			return f[0], f[1:]
		}
	}
	return "vi", nil
}

func editorCmd(line int, path string) *exec.Cmd {
	prog, pre := editorArgv()
	return exec.Command(prog, append(append([]string{}, pre...), "+"+strconv.Itoa(line), path)...)
}

// cursorSrcLine reports the 1-based source line of the nearest heading at or
// above the viewport top; 1 when no heading precedes the top line.
func (m *Model) cursorSrcLine() int {
	y, line := m.vp.YOffset(), 0
	for _, h := range m.heads {
		if h.line > y {
			break
		}
		line = h.srcLine
	}
	return line + 1
}

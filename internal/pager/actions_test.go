package pager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyRawMarkdown(t *testing.T) {
	m := New("# tiny\n", "doc.md")
	cmd := m.copyRaw()
	if cmd == nil || m.flash != "copied" {
		t.Fatalf("copy must stage OSC 52 and flash copied (cmd=%v flash=%q)", cmd, m.flash)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("clipboard command must yield a message for the program to emit")
	}

	big := strings.Repeat("x\n", maxCopyBytes/2+1)
	m = New(big, "doc.md")
	if cmd := m.copyRaw(); cmd != nil || m.flash != "too large to copy" {
		t.Fatalf("oversized copy must decline (cmd=%v flash=%q)", cmd, m.flash)
	}
	if m.errMsg != "" {
		t.Fatalf("declined copy is not an error: %q", m.errMsg)
	}
}

func TestEditGuardsStdin(t *testing.T) {
	m := New("a\n", "(stdin)")
	if cmd := m.editDoc(); cmd != nil || m.flash != "cannot edit stdin" {
		t.Fatalf("stdin documents cannot be edited (cmd=%v flash=%q)", cmd, m.flash)
	}
	if m.edited {
		t.Fatal("guard must not arm the edited flag")
	}
}

func TestEditorSelection(t *testing.T) {
	for _, tc := range []struct {
		name           string
		visual, editor string
		wantProg       string
		wantArgs       string
	}{
		{"visual wins", "myed -w", "other", "myed", "-w"},
		{"editor fallback", "", "emacs", "emacs", ""},
		{"vi default", "", "", "vi", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VISUAL", tc.visual)
			t.Setenv("EDITOR", tc.editor)
			prog, args := editorArgv()
			if prog != tc.wantProg || strings.Join(args, " ") != tc.wantArgs {
				t.Fatalf("editorArgv = %q + [%s], want %q + [%s]", prog, strings.Join(args, " "), tc.wantProg, tc.wantArgs)
			}
		})
	}
}

// Regression: a VISUAL carrying arguments (less/vim convention) must split
// into program + argv; before, exec received one bogus executable name.
func TestEditVisualWithArguments(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fakeed")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "doc.md")
	t.Setenv("VISUAL", script+" --flag")
	c := editorCmd(3, doc)
	if c.Path != script {
		t.Fatalf("program = %q, want first VISUAL field %q", c.Path, script)
	}
	if got := strings.Join(c.Args[1:], " "); got != "--flag +3 "+doc {
		t.Fatalf("argv = %v, want [--flag +3 %s]", c.Args[1:], doc)
	}
	if err := c.Run(); err != nil {
		t.Fatalf("assembled editor command must execute: %v", err)
	}
}

func TestCursorSrcLineNearestHeadingAboveTop(t *testing.T) {
	src := "# Alpha\n\nintro\n" + strings.Repeat("filler line\n\n", 15) +
		"\n## Beta\n\nmore text here\n" + strings.Repeat("tail line\n\n", 15)
	m := newRenderedModel(t, src, 60, 12)
	for i := range m.heads {
		if m.heads[i].text == "Beta" && m.heads[i].srcLine != 34 {
			t.Fatalf("Beta srcLine = %d, want 34", m.heads[i].srcLine)
		}
	}
	if got := m.cursorSrcLine(); got != 1 {
		t.Fatalf("at top: cursor line %d, want 1 (Alpha)", got)
	}
	beta := findLine(m.stripped, "Beta")
	if beta < 0 {
		t.Fatal("Beta missing from render")
	}
	m.vp.SetYOffset(beta + 1)
	if got := m.cursorSrcLine(); got != 35 {
		t.Fatalf("below Beta: cursor line %d, want 35 (Beta+1)", got)
	}

	settle(t, m, press(m, "s"))
	betaSrc := findLine(m.stripped, "## Beta")
	m.vp.SetYOffset(betaSrc)
	if got := m.cursorSrcLine(); got != 35 {
		t.Fatalf("source view below Beta: cursor line %d, want 35", got)
	}
}

func TestEditedReloadFlashAndNoDoubleReload(t *testing.T) {
	m := bootModel(t, "# A\n\none\n")
	m.path = "doc.md"

	next := "# A\n\ntwo\n"
	m.readFile = func(string) ([]byte, error) { return []byte(next), nil }

	m.edited = true
	settle(t, m, m.reload())
	if m.flash != "edited" || m.source != next {
		t.Fatalf("editor-exit reload: flash=%q source-ok=%v", m.flash, m.source == next)
	}
	if m.edited {
		t.Fatal("edited flag must be consumed by the reload")
	}

	m.readFile = func(string) ([]byte, error) { return []byte(next), nil }
	settle(t, m, m.reload())
	if m.flash != "edited" || m.source != next {
		t.Fatalf("same-content watcher reload must no-op (flash=%q)", m.flash)
	}

	m.readFile = func(string) ([]byte, error) { return []byte("# A\n\nthree\n"), nil }
	settle(t, m, m.reload())
	if m.flash != "reloaded" {
		t.Fatalf("external change after editing flashes reloaded, got %q", m.flash)
	}
}

func TestEditedMsgWiring(t *testing.T) {
	m := bootModel(t, "# A\n\none\n")
	nm, cmd := m.Update(editedMsg{err: errors.New("editor crashed")})
	*m = *nm.(*Model)
	if cmd != nil || !strings.Contains(m.errMsg, "editor crashed") {
		t.Fatalf("edit failure must surface as errMsg (errMsg=%q cmd=%v)", m.errMsg, cmd)
	}
	if m.edited {
		t.Fatal("failed edit must disarm the flag")
	}

	m.edited = true
	m.readFile = func(string) ([]byte, error) { return []byte("# A\n\nedited body\n"), nil }
	nm, cmd = m.Update(editedMsg{})
	*m = *nm.(*Model)
	if cmd == nil {
		t.Fatal("successful edit must trigger the reload path")
	}
	settle(t, m, cmd)
	if m.flash != "edited" || !strings.Contains(m.source, "edited body") {
		t.Fatalf("after edit reload: flash=%q source=%q", m.flash, m.source)
	}
}

func TestEditExecCommandShape(t *testing.T) {
	m := newRenderedModel(t, "# Alpha\n\nbody\n", 60, 12)
	m.SetPath("doc.md")
	m.vp.SetYOffset(0)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	cmd := m.editDoc()
	if cmd == nil {
		t.Fatal("file documents must launch the editor")
	}
	if msg := cmd(); msg == nil || !strings.Contains(fmt.Sprintf("%T", msg), "execMsg") {
		t.Fatalf("got %T, want an exec message bubbletea suspends on", msg)
	}
}

package pager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
)

func testApplication(t *testing.T, name string) *Application {
	t.Helper()
	c := config.Defaults()
	c.Images = false
	a, err := NewApplication("# Original\n\n"+strings.Repeat("text\n\n", 50), name, t.TempDir(), c, config.Theme{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	_, cmd := a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	appFiniteCommands(t, a, cmd)
	return a
}

// Runtime sequences (watch waits, Exec, Raw) are exercised by PTYs, not executed
// synchronously here. These tests drain only finite public command trees.
func appFiniteCommands(t *testing.T, a *Application, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			appFiniteCommands(t, a, child)
		}
		return
	}
	if _, ok := msg.(documentGraphics); ok {
		FilterApplicationMessage(a, msg)
		return
	}
	if _, ok := msg.(documentResult); !ok {
		if _, ok := msg.(browserFilterMsg); !ok {
			if _, ok := msg.(scanFilesMsg); !ok {
				return
			}
		}
	}
	_, next := a.Update(msg)
	appFiniteCommands(t, a, next)
}
func appKey(a *Application, k string) tea.Cmd {
	msg := keyMsg(k)
	switch k {
	case "ctrl+f":
		msg = tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
	case "ctrl+c":
		msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	_, cmd := a.Update(msg)
	return cmd
}

func TestApplicationCtrlFPriority(t *testing.T) {
	for _, mode := range []string{"normal", "search", "outline", "help", "targets"} {
		t.Run(mode, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			switch mode {
			case "search":
				a.current.search.active = true
			case "outline":
				a.current.tocOpen = true
			case "help":
				a.current.helpOpen = true
			case "targets":
				a.current.targets.active = true
			}
			appKey(a, "ctrl+f")
			if a.browsing != (mode == "normal") {
				t.Fatalf("browsing=%v", a.browsing)
			}
		})
	}
}

func TestApplicationReaderReturn(t *testing.T) {
	for _, origin := range []string{"direct", "browser"} {
		t.Run(origin, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			a.fromBrowser = origin == "browser"
			a.current.vp.SetYOffset(8)
			before := a.current
			y := before.vp.YOffset()
			appKey(a, "ctrl+f")
			if !a.browsing || a.browser.root != a.cwd {
				t.Fatal("wrong browser root")
			}
			appFiniteCommands(t, a, appKey(a, "esc"))
			if a.browsing || a.current != before || a.current.vp.YOffset() != y {
				t.Fatal("cancel changed reader")
			}
			a.current.search.query = "text"
			appKey(a, "esc")
			if a.browsing || a.current.search.query != "" {
				t.Fatal("Esc failed search ladder")
			}
			cmd := appKey(a, "esc")
			if origin == "browser" {
				if !a.browsing {
					t.Fatal("Esc did not return browser")
				}
			} else if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("direct Esc no longer quits")
			}
		})
	}
}

func TestApplicationBrowserFilterAndReturn(t *testing.T) {
	for _, kind := range []string{"accept", "empty", "escape", "q literal", "stale", "refresh selection"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "")
			b := a.browser
			b.list.SetItems(browserItems(b.root, 30))
			b.list.Select(17)
			if err := os.WriteFile(filepath.Join(b.root, "doc17.md"), []byte("# Selected\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "refresh selection" {
				b.list.SetFilterText("doc")
				b.list.Select(17)
				path := b.selectedPath()
				b.scanGen = 1
				_, cmd := a.Update(scanFilesMsg{gen: 1, items: browserItems(b.root, 35)})
				appFiniteCommands(t, a, cmd)
				if b.selectedPath() != path || b.list.FilterValue() != "doc" {
					t.Fatal("refresh lost filtered selection")
				}
				return
			}
			appKey(a, "/")
			term := "doc17"
			if kind == "empty" {
				term = "nomatch"
			}
			if kind == "q literal" {
				term = "q"
			}
			cmd := appKey(a, term)
			if kind == "stale" {
				appKey(a, "esc")
				appFiniteCommands(t, a, cmd)
				if b.list.FilterState() != list.Unfiltered || len(b.list.VisibleItems()) != 30 {
					t.Fatal("stale filter applied")
				}
				return
			}
			if kind == "accept" {
				// Enter before the filter command finishes must wait for that exact query.
				if appKey(a, "enter") != nil || !b.enterAfterFilter {
					t.Fatal("did not wait for filter")
				}
			}
			appFiniteCommands(t, a, cmd)
			switch kind {
			case "q literal":
				if !a.browsing || b.list.FilterValue() != "q" {
					t.Fatal("q quit filter")
				}
			case "empty":
				if appKey(a, "enter") != nil || !a.browsing || b.list.FilterValue() != term {
					t.Fatal("empty filter opened unrelated file")
				}
			case "escape":
				appKey(a, "esc")
				if b.list.FilterState() != list.Unfiltered || !a.browsing {
					t.Fatal("Esc did not clear filter first")
				}
			case "accept":
				if !b.opening || b.list.FilterState() != list.FilterApplied {
					t.Fatal("filter not applied/opening")
				}
				a.Update(openFileMsg{gen: b.openGen, path: filepath.Join(b.root, "doc17.md"), body: []byte("# Selected\n")})
				if a.browsing || !a.fromBrowser || a.current.source != "# Selected\n" {
					t.Fatal("open failed")
				}
				appKey(a, "esc")
				if !a.browsing || b.selectedPath() != filepath.Join(b.root, "doc17.md") || b.list.FilterValue() != term {
					t.Fatal("return lost browser state")
				}
			}
		})
	}
}

func TestApplicationFailedAndStaleOpen(t *testing.T) {
	for _, kind := range []string{"error", "stale open", "stale scan", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			previous := a.current
			appKey(a, "ctrl+f")
			b := a.browser
			b.list.SetItems(browserItems(b.root, 2))
			cmd := b.openSelected()
			gen := b.openGen
			switch kind {
			case "error":
				a.Update(openFileMsg{gen: gen, err: errors.New("missing\n\x1b[31m")})
				if !strings.Contains(b.notice, "\\u000a") || strings.Contains(b.notice, "\x1b") {
					t.Fatal("unsafe error")
				}
			case "stale open":
				b.cancelOpen()
				a.Update(openFileMsg{gen: gen, path: "stale", body: []byte("stale")})
			case "stale scan":
				a.Update(scanFilesMsg{gen: b.scanGen - 1, items: nil})
				if len(b.list.Items()) != 2 {
					t.Fatal("stale scan accepted")
				}
			case "cancel":
				appKey(a, "esc")
				msg := cmd()
				if msg != nil {
					a.Update(msg)
				}
			}
			if a.current != previous || a.current.source != previous.source {
				t.Fatal("failed/stale open replaced reader")
			}
		})
	}
}

func TestApplicationDocumentOwnershipAndCleanup(t *testing.T) {
	for _, kind := range []string{"render", "reload", "watch", "editor", "image", "url"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "one.md")
			if err := os.WriteFile(path, []byte("# Old"), 0600); err != nil {
				t.Fatal(err)
			}
			a := testApplication(t, path)
			a.current.Init()
			old := a.current
			fw := old.fw
			appKey(a, "ctrl+f")
			if a.browser.root != dir {
				t.Fatal("direct file browser root")
			}
			b := a.browser
			b.openGen = 1
			nextPath := filepath.Join(dir, "two.md")
			if err := os.WriteFile(nextPath, []byte("# New"), 0600); err != nil {
				t.Fatal(err)
			}
			a.Update(openFileMsg{gen: 1, path: nextPath, body: []byte("# New")})
			select {
			case <-fw.done:
			default:
				t.Fatal("old watcher not closed")
			}
			if old.store.ctx.Err() != context.Canceled {
				t.Fatal("old image store not canceled")
			}
			var msg tea.Msg
			switch kind {
			case "render":
				msg = renderedMsg{gen: a.current.gen, content: "OLD"}
			case "reload":
				msg = reloadDoneMsg{body: []byte("OLD")}
			case "watch":
				msg = docChangedMsg{}
			case "editor":
				msg = editedMsg{}
			case "image":
				msg = imagesDoneMsg{}
			case "url":
				msg = openedURLMsg{err: errors.New("OLD")}
			}
			if _, cmd := a.Update(documentResult{old, msg}); cmd != nil || a.current.source != "# New" || a.current.errMsg != "" {
				t.Fatal("retired completion affected new document")
			}
			latest := a.current.fw
			store := a.current.store
			var model tea.Model = a
			app := model.(*Application)
			app.Close()
			app.Close()
			select {
			case <-latest.done:
			default:
				t.Fatal("latest watcher leaked")
			}
			if store.ctx.Err() != context.Canceled {
				t.Fatal("latest store leaked")
			}
		})
	}
}

func TestApplicationGraphicsRuntimeGuard(t *testing.T) {
	for _, kind := range []string{"current", "browser", "resume stale", "replaced", "native raw", "native exec", "native batch"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			m := a.current
			packet := m.graphics("GRAPHICS")()
			want := kind == "current"
			switch kind {
			case "browser":
				appKey(a, "ctrl+f")
			case "resume stale":
				appKey(a, "ctrl+f")
				appKey(a, "esc")
			case "replaced":
				a.current = New("new", "new")
			case "native raw":
				packet = tea.Raw("raw")()
				want = true
			case "native exec":
				packet = tea.ExecProcess(nil, nil)()
				want = true
			case "native batch":
				packet = tea.Batch(tea.Quit, tea.Quit)()
				want = true
			}
			got := FilterApplicationMessage(a, packet)
			if (got != nil) != want {
				t.Fatalf("result %T", got)
			}
			if strings.HasPrefix(kind, "native") && reflect.TypeOf(got) != reflect.TypeOf(packet) {
				t.Fatal("runtime message wrapped")
			}
		})
	}
}

func TestApplicationImageCancellation(t *testing.T) {
	for _, kind := range []string{"request", "semaphore"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			previous := stdoutIsTTY
			stdoutIsTTY = func() bool { return true }
			t.Cleanup(func() { stdoutIsTTY = previous })
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
			defer server.Close()
			st := a.current.store
			defer st.cancel()
			if kind == "semaphore" {
				for range maxFetchConcurrent {
					st.sem <- struct{}{}
				}
			}
			cmd := a.current.fetchPending([]string{server.URL})
			done := make(chan tea.Msg, 1)
			go func() { done <- cmd() }()
			if kind == "request" {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("request did not start")
				}
			}
			a.Close()
			select {
			case msg := <-done:
				result, ok := msg.(documentResult)
				if !ok || result.owner != a.current {
					t.Fatalf("untagged image completion %T", msg)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("retired image work did not cancel")
			}
			if st.pending != 0 {
				t.Fatal("pending image work leaked")
			}
			if _, cmd := a.Update(documentResult{a.current, imagesDoneMsg{}}); cmd != nil {
				t.Fatal("closed application restarted work")
			}
		})
	}
}

func TestApplicationAcceptPreservesSelection(t *testing.T) {
	for _, query := range []string{"", "doc"} {
		t.Run(query, func(t *testing.T) {
			a := testApplication(t, "")
			b := a.browser
			b.list.SetItems(browserItems(b.root, 30))
			b.list.SetFilterText(query)
			b.list.Select(17)
			path := b.selectedPath()
			a.acceptAndOpen()
			if b.selectedPath() != path {
				t.Fatal("applying filter reset selection")
			}
			if query == "" && b.list.FilterState() != list.Unfiltered {
				t.Fatal("empty filter retained a redundant Esc layer")
			}
		})
	}
}

func TestApplicationReloadReader(t *testing.T) {
	for _, kind := range []string{"empty", "fifo", "closed"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "doc.md")
			if kind == "fifo" {
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			a := testApplication(t, path)
			if kind == "closed" {
				a.Close()
			}
			result := a.current.reload()().(documentResult)
			msg := result.msg.(reloadDoneMsg)
			if kind == "empty" {
				if msg.err != nil || len(msg.body) != 0 {
					t.Fatal("empty reload semantics changed")
				}
				a.Update(result)
				if a.current.errMsg != "file truncated to empty" || !strings.Contains(a.current.source, "Original") {
					t.Fatal("empty reload destroyed document")
				}
			} else if msg.err == nil {
				t.Fatal("nonregular/canceled reload accepted")
			}
			if kind == "closed" && !errors.Is(msg.err, context.Canceled) {
				t.Fatalf("closed reload: %v", msg.err)
			}
		})
	}
}

func TestApplicationRefreshFilterOrdering(t *testing.T) {
	for _, kind := range []string{"complete then enter", "enter then complete", "applied enter", "clear", "change query", "replace items"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "")
			b := a.browser
			b.list.SetItems(browserItems(b.root, 30))
			b.list.SetFilterText("doc1")
			b.list.Select(7)
			path := b.selectedPath()
			b.refresh() // Deliver deterministic scan contents without filesystem timing.
			_, held := a.Update(scanFilesMsg{gen: b.scanGen, items: browserItems(b.root, 35)})
			gen := b.filterGen
			if !b.filtering || held == nil {
				t.Fatal("refresh did not schedule refilter")
			}
			if kind != "applied enter" {
				appKey(a, "/")
			}
			switch kind {
			case "clear":
				appKey(a, "esc")
				appFiniteCommands(t, a, held)
				if b.list.FilterState() != list.Unfiltered || len(b.list.VisibleItems()) != 35 || b.filterGen == gen {
					t.Fatal("clear accepted stale refresh matches")
				}
				return
			case "change query":
				next := appKey(a, "7")
				appFiniteCommands(t, a, held)
				if !b.filtering || b.filterGen == gen {
					t.Fatal("old completion consumed new query")
				}
				appFiniteCommands(t, a, next)
				if len(b.list.VisibleItems()) != 1 || b.selectedPath() != path {
					t.Fatal("new query lost matches")
				}
				return
			case "replace items":
				b.refresh()
				_, next := a.Update(scanFilesMsg{gen: b.scanGen, items: browserItems(b.root, 12)})
				appFiniteCommands(t, a, held)
				if !b.filtering || b.filterGen == gen {
					t.Fatal("old completion consumed new items")
				}
				appFiniteCommands(t, a, next)
				if len(b.list.VisibleItems()) != 3 {
					t.Fatal("new items lost matches")
				}
				return
			}
			if kind != "complete then enter" {
				if appKey(a, "enter") != nil || !b.enterAfterFilter {
					t.Fatal("Enter did not wait for pending refresh")
				}
			}
			appFiniteCommands(t, a, held)
			if len(b.list.VisibleItems()) != 13 || b.selectedPath() != path || b.filterGen != gen {
				t.Fatalf("refresh lost matches/selection: %d %q", len(b.list.VisibleItems()), b.selectedPath())
			}
			if kind == "complete then enter" {
				appKey(a, "enter")
			}
			if !b.opening || b.list.FilterState() != list.FilterApplied || b.filtering {
				t.Fatal("Enter did not apply and open refreshed selection")
			}
		})
	}
}

func TestApplicationCellSizeRouting(t *testing.T) {
	for _, kind := range []string{"visible", "hidden", "no reader", "retired"} {
		t.Run(kind, func(t *testing.T) {
			name := "(stdin)"
			if kind == "no reader" {
				name = ""
			}
			a := testApplication(t, name)
			if a.current == nil {
				if _, cmd := a.Update(uv.CellSizeEvent{Width: 14, Height: 60}); cmd != nil || !a.browsing {
					t.Fatal("cell report without reader started work")
				}
				return
			}
			m := a.current
			m.gfx = true
			m.cellWidth, m.cellHeight = 14, 30
			if kind != "visible" {
				appKey(a, "ctrl+f")
			}
			_, cmd := a.Update(uv.CellSizeEvent{Width: 14, Height: 60})
			if m.cellWidth != 14 || m.cellHeight != 60 || cmd == nil {
				t.Fatal("cell report did not reach retained reader/render")
			}
			if kind == "retired" {
				a.Update(openFileMsg{gen: a.browser.openGen, path: "(stdin)", body: []byte("# Replacement")})
				gen := a.current.gen
				result := cmd()
				if _, next := a.Update(result); next != nil || a.current.gen != gen || a.current.cellHeight == 60 {
					t.Fatal("retired metric render affected replacement")
				}
				return
			}
			appFiniteCommands(t, a, cmd)
			if kind == "hidden" {
				if m.graphics("HIDDEN") != nil || FilterApplicationMessage(a, documentGraphics{owner: m, epoch: m.graphicsEpoch, payload: "QUEUED"}) != nil {
					t.Fatal("hidden metric render bypassed graphics guards")
				}
				appFiniteCommands(t, a, appKey(a, "esc"))
				if a.browsing || m.hidden || m.cellHeight != 60 {
					t.Fatal("resume lost hidden metrics")
				}
			}
		})
	}
}

func TestApplicationUnsafeFilenameReloadNotice(t *testing.T) {
	for _, name := range []string{"line\nbreak.md", "escape\x1b[2J.md", "bidi\u202e.md", "invalid\xff.md"} {
		for _, trigger := range []string{"manual", "watcher"} {
			t.Run(fmt.Sprintf("%q/%s", name, trigger), func(t *testing.T) {
				a := testApplication(t, "")
				path := filepath.Join(a.browser.root, name)
				if err := os.WriteFile(path, []byte("# Safe body\n"), 0600); err != nil {
					if !utf8.ValidString(name) {
						t.Skipf("filesystem rejects invalid filename bytes: %v", err)
					}
					t.Fatal(err)
				}
				appFiniteCommands(t, a, a.Init())
				open := appKey(a, "enter")
				if open == nil {
					t.Fatal("no browser open")
				}
				a.Update(open())
				m := a.current
				if m == nil || m.path != path || m.source != "# Safe body\n" {
					t.Fatal("browser changed IO identity/source")
				}
				// Drain the initial render separately from Init's perpetual watcher wait.
				m.rendering = false
				_, render := a.Update(tea.WindowSizeMsg{Width: 1000, Height: 20})
				appFiniteCommands(t, a, render)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var reload tea.Cmd
				if trigger == "manual" {
					reload = appKey(a, "R")
				} else {
					_, batch := a.Update(documentResult{m, docChangedMsg{}})
					reload = batch().(tea.BatchMsg)[0]
				}
				result := reload().(documentResult)
				err := result.msg.(reloadDoneMsg).err
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) || pathErr.Path != path {
					t.Fatalf("reload did not use raw path: %v", err)
				}
				a.Update(result)
				view := a.View().Content
				plain := ansi.Strip(view)
				if !strings.Contains(plain, "reload: "+safeFilename(err.Error())) || strings.Contains(plain, name) || strings.Contains(view, "\x1b[2J") || !utf8.ValidString(view) {
					t.Fatalf("unsafe or missing escaped notice: %q", strings.TrimSpace(strings.Join(strings.Fields(plain), " ")))
				}
				if len(strings.Split(plain, "\n")) != 20 || m.path != path || m.source != "# Safe body\n" {
					t.Fatal("reload broke chrome geometry or IO identity")
				}
			})
		}
	}
}

func TestApplicationRootAlias(t *testing.T) {
	for _, mode := range []string{"startup", "ctrl+f"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			real := filepath.Join(dir, "real")
			if err := os.Mkdir(real, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"a.md", "b.md"} {
				if err := os.WriteFile(filepath.Join(real, name), []byte("# Alias"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(dir, "alias")
			if err := os.Symlink(real, alias); err != nil {
				t.Fatal(err)
			}
			name := ""
			if mode == "ctrl+f" {
				name = filepath.Join(alias, "a.md")
			}
			c := config.Defaults()
			c.Images = false
			a, err := NewApplication("# Alias", name, alias, c, config.Theme{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(a.Close)
			a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			if mode == "startup" {
				appFiniteCommands(t, a, a.Init())
			} else {
				appFiniteCommands(t, a, appKey(a, "ctrl+f"))
			}
			b := a.browser
			if b.root != alias || len(b.list.Items()) != 2 || b.selectedPath() != filepath.Join(alias, "a.md") {
				t.Fatalf("alias root/initial selection lost: %s %v", b.root, b.list.Items())
			}
			b.list.Select(1)
			appFiniteCommands(t, a, appKey(a, "r"))
			if b.root != alias || b.selectedPath() != filepath.Join(alias, "b.md") {
				t.Fatal("alias refresh lost logical identity/selection")
			}
			open := appKey(a, "enter")
			a.Update(open())
			if a.current.path != filepath.Join(alias, "b.md") || a.current.source != "# Alias" {
				t.Fatal("alias open lost raw identity/source")
			}
			appKey(a, "esc")
			if !a.browsing || b.root != alias || b.selectedPath() != a.current.path {
				t.Fatal("alias return lost selection")
			}
		})
	}
}

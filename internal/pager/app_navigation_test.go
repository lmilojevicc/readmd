package pager

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lmilojevicc/readmd/internal/config"
)

// Drain read/render steps only: a commit's Init includes a live watcher wait.
func navigationCommands(t *testing.T, a *Application, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			navigationCommands(t, a, child)
		}
		return
	}
	before := a.current
	_, next := a.Update(msg)
	if a.current == before {
		navigationCommands(t, a, next)
	}
}
func navigationTarget(t *testing.T, a *Application, dest string) {
	t.Helper()
	a.current.openURL = func(string) error { t.Fatal("local target reached external opener"); return nil }
	navigationCommands(t, a, a.current.activateTarget(hintTarget{linkTarget: linkTarget{dest: dest}}))
}
func navigationWrite(t *testing.T, path, source string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}
func navigationSource(title string) string {
	return "# " + title + "\n\n" + strings.Repeat("body\n\n", 20) + "# Target 世界\n\n" + strings.Repeat("tail\n\n", 20)
}

func TestNavigationApplicationTransactions(t *testing.T) {
	for _, tc := range []struct {
		name, dest                  string
		success, symlink, directory bool
	}{
		{"anchor", "next.md#target-世界", true, false, false},
		{"exact title", "next.md#Target%20世界", true, false, false},
		{"empty fragment", "next.md#", true, false, false},
		{"empty file", "empty.md#", true, false, false},
		{"encoded name", "space%20%23%3F%25.MARKDOWN#target-世界", true, false, false},
		{"exactly once", "%2523.md", true, false, false},
		{"parent", "../next.md", true, false, false},
		{"symlink", "link.md#target-世界", true, true, false},
		{"missing anchor", "next.md#missing", false, false, false},
		{"missing file", "missing.md", false, false, false},
		{"nonregular", "directory.md", false, false, true},
		{"unsupported", "next.txt", false, false, false},
		{"scheme", "file:next.md", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			sub := filepath.Join(root, "sub")
			if err := os.Mkdir(sub, 0700); err != nil {
				t.Fatal(err)
			}
			navigationWrite(t, filepath.Join(root, "next.md"), navigationSource("Parent"))
			for _, name := range []string{"next.md", "space #?%.MARKDOWN", "%23.md"} {
				navigationWrite(t, filepath.Join(sub, name), navigationSource("Next"))
			}
			navigationWrite(t, filepath.Join(sub, "empty.md"), "")
			if tc.symlink {
				if err := os.Symlink(filepath.Join(sub, "next.md"), filepath.Join(sub, "link.md")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.directory {
				if err := os.Mkdir(filepath.Join(sub, "directory.md"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			c := config.Defaults()
			c.Images = false
			a, err := NewApplication(navigationSource("Original"), filepath.Join(sub, "original.md"), root, c, config.Theme{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(a.Close)
			_, render := a.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
			appFiniteCommands(t, a, render)
			old := a.current
			old.vp.SetYOffset(7)
			old.reader = true
			old.syncVPWidth()
			old.vp.SetXOffset(0)
			location := old.currentLocation()
			a.fromBrowser = true
			navigationTarget(t, a, tc.dest)
			if tc.success {
				if a.current == old || len(a.history) != 1 || a.current.hidden || !a.fromBrowser {
					t.Fatal("commit lifecycle/history failed")
				}
				if old.store.ctx.Err() == nil || old.fw != nil {
					t.Fatal("retired document not closed")
				}
				fragment := ""
				if strings.Contains(tc.dest, "#") {
					local, _, _ := classifyLocalLink(tc.dest)
					fragment = local.fragment
				}
				line, _ := headingLine(a.current.heads, fragment)
				if a.current.vp.YOffset() != line || a.current.vp.XOffset() != 0 || a.current.reader != c.Reader {
					t.Fatal("fresh document settings/anchor wrong")
				}
			} else if a.current != old || a.current.currentLocation() != location || len(a.history) != 0 || a.current.flash == "" || old.store.ctx.Err() != nil {
				t.Fatal("failed open changed current document/state or lacked notice")
			}
			if a.navigation != nil {
				t.Fatal("transaction not cleared")
			}
		})
	}
}

func TestNavigationHistoryChronology(t *testing.T) {
	for _, origin := range []string{"stdin", "file"} {
		t.Run(origin, func(t *testing.T) {
			root := t.TempDir()
			source := navigationSource("Original")
			path := "(stdin)"
			if origin == "file" {
				path = filepath.Join(root, "original.md")
				navigationWrite(t, path, source)
			}
			navigationWrite(t, filepath.Join(root, "next.md"), navigationSource("Next"))
			c := config.Defaults()
			c.Images = false
			a, err := NewApplication(source, path, root, c, config.Theme{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(a.Close)
			_, cmd := a.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
			appFiniteCommands(t, a, cmd)
			a.current.vp.SetYOffset(3)
			initial := a.current.currentLocation()
			navigationCommands(t, a, a.current.activateTarget(hintTarget{linkTarget: linkTarget{kind: targetFootnote, footnote: "1", definition: targetRegion{line: 9}}}))
			footnote := a.current.currentLocation()
			navigationTarget(t, a, "#target-世界")
			anchor := a.current.currentLocation()
			a.current.reader = true
			a.current.syncVPWidth()
			saved := a.current.currentLocation()
			navigationTarget(t, a, "next.md#target-世界")
			if len(a.history) != 3 {
				t.Fatalf("history=%d", len(a.history))
			}
			if origin == "file" {
				navigationWrite(t, path, source+"\nchanged on disk\n")
			}
			navigationCommands(t, a, a.current.backLocation())
			if a.current.currentLocation() != saved || len(a.history) != 2 {
				t.Fatalf("cross-file back location=%+v want %+v history=%d", a.current.currentLocation(), saved, len(a.history))
			}
			if origin == "stdin" && (a.current.path != "" || a.current.source != source) {
				t.Fatal("stdin raw source lost")
			}
			if origin == "file" && !strings.Contains(a.current.source, "changed on disk") {
				t.Fatal("file back did not reread disk")
			}
			navigationCommands(t, a, a.current.backLocation())
			if a.current.currentLocation() != footnote || len(a.history) != 1 {
				t.Fatalf("heading back lost chronology: %+v, prior anchor %+v", a.current.currentLocation(), anchor)
			}
			navigationCommands(t, a, a.current.backLocation())
			if a.current.currentLocation() != initial || len(a.history) != 0 {
				t.Fatal("footnote back lost chronology")
			}
		})
	}
}

func TestNavigationFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"failed back", "render error", "resize read", "resize render", "browser read", "browser render", "replaced origin", "new navigation", "close"} {
		t.Run(mode, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			navigationWrite(t, filepath.Join(a.cwd, "next.md"), navigationSource("Next"))
			old := a.current
			location := old.currentLocation()
			if mode == "failed back" {
				a.history = append(a.history, navigationEntry{path: filepath.Join(a.cwd, "missing.md"), reopen: true})
				navigationCommands(t, a, old.backLocation())
				if a.current != old || len(a.history) != 1 || a.current.currentLocation() != location {
					t.Fatal("failed back consumed history/state")
				}
				return
			}
			read := a.navigate(navigationRequest{local: localLink{path: "next.md"}})
			readMsg := read()
			if mode == "resize read" {
				a.Update(tea.WindowSizeMsg{Width: 41, Height: 20})
			}
			if mode == "browser read" {
				a.showBrowser()
			}
			if mode == "close" {
				a.Close()
			}
			if strings.HasSuffix(mode, "read") || mode == "close" {
				_, cmd := a.Update(readMsg)
				if cmd != nil || a.navigation != nil || a.current != old {
					t.Fatal("stale read revived transaction")
				}
				return
			}
			_, render := a.Update(readMsg)
			candidate := a.navigation.candidate
			if candidate == nil || candidate.fw != nil || !candidate.hidden {
				t.Fatal("candidate published before commit")
			}
			if candidate.graphics("packet") != nil {
				t.Fatal("hidden candidate emitted graphics")
			}
			renderMsg := render()
			switch mode {
			case "render error":
				result := renderMsg.(documentResult)
				result.msg = renderedMsg{gen: candidate.gen, err: errors.New("forced render failure")}
				renderMsg = result
			case "resize render":
				a.Update(tea.WindowSizeMsg{Width: 41, Height: 20})
			case "browser render":
				a.showBrowser()
			case "replaced origin":
				a.current, _ = a.document("# Replacement", "(stdin)")
				t.Cleanup(old.Close)
			case "new navigation":
				a.navigate(navigationRequest{local: localLink{path: "missing.md"}})
			}
			a.Update(renderMsg)
			if a.current == candidate || len(a.history) != 0 {
				t.Fatal("stale/failed render committed")
			}
			if mode != "replaced origin" && a.current != old {
				t.Fatal("failure replaced live document")
			}
			if mode != "replaced origin" && candidate.store.ctx.Err() == nil {
				t.Fatal("discarded candidate resources remain live")
			}
			if FilterApplicationMessage(a, documentGraphics{owner: candidate, epoch: candidate.graphicsEpoch, payload: "packet"}) != nil {
				t.Fatal("candidate graphics passed runtime filter")
			}
		})
	}
}

func TestNavigationHistoryBoundAndBrowserReturn(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "application"}[managed], func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			m := a.current
			m.managed = managed
			a.fromBrowser = true
			for i := 0; i < navigationHistoryLimit+7; i++ {
				m.vp.SetYOffset(i % 20)
				if managed {
					a.pushNavigation(false)
				} else {
					m.pushLocation()
				}
			}
			if managed {
				if len(a.history) != navigationHistoryLimit || a.history[0].location.y != 7 {
					t.Fatal("application history bound")
				}
				a.showBrowser()
				appFiniteCommands(t, a, a.returnReader())
				if !a.fromBrowser || len(a.history) != navigationHistoryLimit {
					t.Fatal("browser return lost provenance/history")
				}
			} else if len(m.locations) != navigationHistoryLimit || m.locations[0].y != 7 {
				t.Fatal("model history bound")
			}
		})
	}
}

func TestNavigationBackRenderRollback(t *testing.T) {
	for _, kind := range []string{"file", "stdin reader change"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			old := a.current
			entry := a.navigationSnapshot()
			if kind == "file" {
				entry.path = filepath.Join(a.cwd, "previous.md")
				entry.reopen = true
				navigationWrite(t, entry.path, navigationSource("Previous"))
			} else {
				entry.location.reader = !old.reader
			}
			a.history = append(a.history, entry)
			location := old.currentLocation()
			read := a.navigate(navigationRequest{back: true})
			_, render := a.Update(read())
			candidate := a.navigation.candidate
			result := render().(documentResult)
			result.msg = renderedMsg{gen: candidate.gen, err: errors.New("failed back render")}
			a.Update(result)
			if a.current != old || old.currentLocation() != location || len(a.history) != 1 || a.navigation != nil {
				t.Fatal("render failure consumed back history/state")
			}
			if candidate.store.ctx.Err() == nil {
				t.Fatal("failed candidate leaked")
			}
		})
	}
}

func TestNavigationStaleReadsAndWatchers(t *testing.T) {
	for _, kind := range []string{"new request", "watcher commit"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			navigationWrite(t, filepath.Join(a.cwd, "first.md"), navigationSource("First"))
			navigationWrite(t, filepath.Join(a.cwd, "second.md"), navigationSource("Second"))
			old := a.current
			if kind == "watcher commit" {
				old.SetPath(filepath.Join(a.cwd, "original.md"))
				navigationWrite(t, old.path, old.source)
				old.Init()
				watcher := old.fw
				navigationCommands(t, a, a.navigate(navigationRequest{local: localLink{path: "first.md"}}))
				select {
				case <-watcher.stopped:
				default:
					t.Fatal("retired watcher remained alive")
				}
				if old.fw != nil || a.current.fw == nil || old.store.ctx.Err() == nil {
					t.Fatal("watcher/store ownership not transferred")
				}
				return
			}
			staleRead := a.navigate(navigationRequest{local: localLink{path: "first.md"}})()
			newRead := a.navigate(navigationRequest{local: localLink{path: "second.md"}})
			pending := a.navigation
			a.Update(staleRead)
			if a.navigation != pending || pending.candidate != nil || a.current != old {
				t.Fatal("stale read disturbed new navigation")
			}
			navigationCommands(t, a, newRead)
			if a.current.path != filepath.Join(a.cwd, "second.md") || len(a.history) != 1 {
				t.Fatal("new request did not commit")
			}
		})
	}
}

func TestNavigationReloadHistory(t *testing.T) {
	for _, kind := range []string{"changed", "unchanged", "error", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			old := a.current
			source := old.source
			fragment := "original"
			old.pendingHeading = &fragment
			local := a.navigationSnapshot()
			previous := filepath.Join(a.cwd, "previous.md")
			navigationWrite(t, previous, navigationSource("Previous"))
			a.history = []navigationEntry{local, {path: previous}, {path: previous, reopen: true}}
			read := a.navigate(navigationRequest{local: localLink{path: "unread.md"}})
			pending := a.navigation
			reload := reloadDoneMsg{body: []byte(source)}
			switch kind {
			case "changed":
				reload.body = []byte(navigationSource("Changed"))
			case "error":
				reload.err = errors.New("read failed")
			case "truncated":
				reload.body = nil
			}
			_, render := a.Update(documentResult{owner: old, msg: reload})
			if kind == "changed" {
				if a.navigation != nil || len(a.history) != 2 || old.pendingHeading != nil {
					t.Fatal("changed reload retained stale jumps/navigation/heading intent")
				}
				a.Update(read())
				if a.navigation != nil || a.current != old {
					t.Fatal("stale read revived after reload")
				}
				navigationCommands(t, a, render)
				navigationCommands(t, a, old.backLocation())
				if a.current.path != previous || len(a.history) != 1 {
					t.Fatal("reload lost cross-file back history")
				}
			} else if old.source != source || a.navigation != pending || len(a.history) != 3 || old.pendingHeading == nil {
				t.Fatal("unchanged/rejected reload altered navigation history/intent")
			}
		})
	}
}

func TestNavigationReloadInvalidatesCandidate(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "managed candidate"}[managed], func(t *testing.T) {
			a := testApplication(t, "(stdin)")
			old := a.current
			fragment := "original"
			old.pendingHeading = &fragment
			old.managed = managed
			if !managed {
				old.applyReload(reloadDoneMsg{body: []byte(navigationSource("Changed"))})
				if old.pendingHeading != nil {
					t.Fatal("standalone reload kept pending heading")
				}
				return
			}
			navigationWrite(t, filepath.Join(a.cwd, "next.md"), navigationSource("Next"))
			read := a.navigate(navigationRequest{local: localLink{path: "next.md"}})
			_, render := a.Update(read())
			candidate := a.navigation.candidate
			stale := render()
			a.Update(documentResult{owner: old, msg: reloadDoneMsg{body: []byte(navigationSource("Changed"))}})
			a.Update(stale)
			if a.current != old || a.navigation != nil || candidate.store.ctx.Err() == nil || len(a.history) != 0 {
				t.Fatal("changed origin accepted stale candidate")
			}
		})
	}
}

func TestNavigationHiddenImageLifecycle(t *testing.T) {
	for _, fragment := range []string{"", "missing"} {
		t.Run(fragment, func(t *testing.T) {
			t.Setenv("KITTY_WINDOW_ID", "navigation-test")
			t.Setenv("TMUX", "")
			a := testApplication(t, "(stdin)")
			a.settings.Images = true
			old := a.current
			old.store.startFetch("https://example.org/retired.png")
			navigationWrite(t, filepath.Join(a.cwd, "image.md"), "# Graphics\n\n![remote](https://example.org/pending.png)\n")
			read := a.navigate(navigationRequest{local: localLink{path: "image.md", fragment: fragment}})
			_, render := a.Update(read())
			candidate := a.navigation.candidate
			result := render().(documentResult)
			if len(result.msg.(renderedMsg).pending) != 1 || candidate.store.pending != 0 || candidate.fw != nil || len(candidate.store.tx) != 0 {
				t.Fatal("candidate fetched/published image state before validation")
			}
			a.Update(result)
			if fragment == "missing" {
				if a.current != old || candidate.store.ctx.Err() == nil || candidate.store.pending != 0 || old.store.ctx.Err() != nil {
					t.Fatal("failed fragment changed image ownership")
				}
			} else if a.current != candidate || candidate.store.pending != 1 || old.store.ctx.Err() == nil || candidate.fw == nil {
				t.Fatal("successful commit did not transfer watcher/image ownership")
			}
		})
	}
}

func TestNavigationUnreadableFile(t *testing.T) {
	for _, kind := range []string{"open", "back"} {
		t.Run(kind, func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("root can read files with mode 000")
			}
			a := testApplication(t, "(stdin)")
			path := filepath.Join(a.cwd, "unreadable.md")
			navigationWrite(t, path, navigationSource("Unreadable"))
			if err := os.Chmod(path, 0000); err != nil {
				t.Fatal(err)
			}
			old := a.current
			location := old.currentLocation()
			if kind == "open" {
				navigationTarget(t, a, "unreadable.md")
			} else {
				a.history = []navigationEntry{{path: path, reopen: true}}
				navigationCommands(t, a, old.backLocation())
				if len(a.history) != 1 {
					t.Fatal("unreadable back consumed history")
				}
			}
			if a.current != old || old.currentLocation() != location || old.flash == "" || a.navigation != nil {
				t.Fatal("unreadable file altered current state")
			}
		})
	}
}

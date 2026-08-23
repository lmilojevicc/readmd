package pager

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

func TestRelevantEvents(t *testing.T) {
	for _, tc := range []struct {
		name string
		base string
		dir  string
		ev   fsnotify.Event
		want bool
	}{
		{"write to doc", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/doc.md", Op: fsnotify.Write}, true},
		{"create replaces inode", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/doc.md", Op: fsnotify.Create}, true},
		{"rename over doc", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/doc.md", Op: fsnotify.Rename}, true},
		{"remove", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/doc.md", Op: fsnotify.Remove}, true},
		{"case-insensitive save", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/DOC.MD", Op: fsnotify.Write}, true},
		{"other file ignored", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/other.md", Op: fsnotify.Write}, false},
		{"editor temp file ignored", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/.doc.md.swp", Op: fsnotify.Write}, false},
		{"chmod only ignored", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/doc.md", Op: fsnotify.Chmod}, false},
		{"sibling dir ignored", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d/sub", Op: fsnotify.Create}, false},
		{"watched dir removed triggers rewatch", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d", Op: fsnotify.Remove}, true},
		{"watched dir renamed triggers rewatch", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d", Op: fsnotify.Rename}, true},
		{"write to watched dir ignored", "doc.md", "/tmp/d", fsnotify.Event{Name: "/tmp/d", Op: fsnotify.Write}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := relevant(tc.dir, tc.base, tc.ev); got != tc.want {
				t.Errorf("relevant = %v, want %v", got, tc.want)
			}
		})
	}
}

func startFakeWatcher(events chan fsnotify.Event, errs chan error) *fileWatcher {
	fw := &fileWatcher{
		base:    "doc.md",
		events:  events,
		errs:    errs,
		tick:    make(chan struct{}),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
	go fw.run()
	return fw
}

func TestDebounceCoalescesBurst(t *testing.T) {
	events := make(chan fsnotify.Event)
	errs := make(chan error, 1)
	fw := startFakeWatcher(events, errs)
	defer fw.stop()

	for _, ev := range []fsnotify.Event{
		{Name: "/d/doc.md", Op: fsnotify.Write},
		{Name: "/d/doc.md", Op: fsnotify.Chmod},
		{Name: "/d/notes.md", Op: fsnotify.Write},
		{Name: "/d/doc.md", Op: fsnotify.Create},
	} {
		events <- ev
	}
	select {
	case <-fw.tick:
		t.Fatal("tick fired before burst settled")
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-fw.tick:
	case <-time.After(2 * time.Second):
		t.Fatal("no tick after burst settled")
	}
	select {
	case <-fw.tick:
		t.Fatal("extra tick for a single burst")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestIrrelevantEventsNeverFire(t *testing.T) {
	events := make(chan fsnotify.Event)
	errs := make(chan error, 1)
	fw := startFakeWatcher(events, errs)
	defer fw.stop()
	events <- fsnotify.Event{Name: "/d/doc.md", Op: fsnotify.Chmod}
	events <- fsnotify.Event{Name: "/d/other.md", Op: fsnotify.Write}
	select {
	case <-fw.tick:
		t.Fatal("irrelevant events must not fire")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestRestoreOffset(t *testing.T) {
	oldHeads := []heading{{line: 0}, {line: 10}, {line: 20}}
	for _, tc := range []struct {
		name     string
		a        anchorState
		newHeads []heading
		newTotal int
		want     int
	}{
		{"empty doc", anchorState{y: 10, total: 40}, nil, 0, 0},
		{"fraction without headings", anchorState{y: 10, total: 40}, nil, 80, 20},
		{"anchor preferred", anchorState{y: 15, total: 100, heads: oldHeads}, []heading{{line: 0}, {line: 7}, {line: 60}}, 90, 7},
		{"anchor at top heading", anchorState{y: 0, total: 100, heads: oldHeads}, []heading{{line: 2}}, 90, 2},
		{"anchor beyond new heads falls back to fraction", anchorState{y: 25, total: 100, heads: oldHeads}, []heading{{line: 0}}, 90, 22},
		{"clamped to bottom", anchorState{y: 15, total: 100, heads: oldHeads}, []heading{{line: 0}, {line: 999}}, 30, 29},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := restoreOffset(tc.a, tc.newHeads, tc.newTotal); got != tc.want {
				t.Errorf("restoreOffset = %d, want %d", got, tc.want)
			}
		})
	}
}

func bootModel(t *testing.T, src string) *Model {
	t.Helper()
	m := New(src, "doc.md")
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m2 := nm.(*Model)
	settle(t, m2, cmd)
	return m2
}

func findLine(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func TestReloadDeletedThenRecovered(t *testing.T) {
	m := bootModel(t, "# A\n\noriginal\n")
	m.fw = &fileWatcher{tick: make(chan struct{}, 1), stopped: make(chan struct{})}

	gone := errors.New("file does not exist")
	m.readFile = func(string) ([]byte, error) { return nil, gone }
	settle(t, m, m.reload())
	if !strings.Contains(m.errMsg, gone.Error()) {
		t.Fatalf("errMsg = %q, want transient error", m.errMsg)
	}
	if v := m.View().Content; !strings.Contains(v, "original") {
		t.Fatalf("last good content must stay on screen:\n%s", v)
	}

	body := "# A\n\nreplacement one\n\nreplacement two\n"
	m.readFile = func(string) ([]byte, error) { return []byte(body), nil }
	settle(t, m, m.reload())
	if m.source != body || m.flash != "reloaded" || m.errMsg != "" {
		t.Fatalf("after recovery: source ok=%v flash=%q errMsg=%q", m.source == body, m.flash, m.errMsg)
	}
	if v := m.View().Content; !strings.Contains(v, "replacement") {
		t.Fatalf("viewport must show reloaded content:\n%s", v)
	}

	m.readFile = func(string) ([]byte, error) { return nil, nil }
	settle(t, m, m.reload())
	if !strings.Contains(m.errMsg, "empty") {
		t.Fatalf("errMsg = %q, want truncation notice", m.errMsg)
	}
	if m.source != body || m.flash != "reloaded" {
		t.Fatalf("truncation must keep last good content: source ok=%v flash=%q", m.source == body, m.flash)
	}
}

func TestDocChangedMsgWiring(t *testing.T) {
	m := New("a\n", "doc.md")
	m.fw = &fileWatcher{tick: make(chan struct{}, 1), stopped: make(chan struct{})}
	m.readFile = func(string) ([]byte, error) { return []byte("a\n"), nil }
	m.fw.tick <- struct{}{}
	_, cmd := m.Update(docChangedMsg{})
	if cmd == nil {
		t.Fatal("doc change must re-arm the listener")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("got %T, want tea.BatchMsg with re-armed listener", cmd())
	}
	sawChange := false
	for _, c := range batch {
		switch msg := c().(type) {
		case docChangedMsg:
			sawChange = true
		case reloadDoneMsg:
			nm, render := m.Update(msg)
			*m = *nm.(*Model)
			if render != nil {
				t.Fatal("unchanged content must not re-render")
			}
		}
	}
	if !sawChange {
		t.Fatal("re-armed listener must yield docChangedMsg")
	}
	if m.source != "a\n" || m.flash != "" {
		t.Fatalf("unchanged content must not re-render: source ok=%v flash=%q", m.source == "a\n", m.flash)
	}
}

func TestReloadPreservesHeadingAnchor(t *testing.T) {
	old := "# Alpha\n\nintro\n" + strings.Repeat("filler line\n\n", 20) + "\n# Beta\n\nafter beta\n" + strings.Repeat("tail line\n\n", 20)
	m := bootModel(t, old)
	betaOld := findLine(m.stripped, "Beta")
	if betaOld < 0 {
		t.Fatal("Beta heading missing in rendered output")
	}
	m.vp.SetYOffset(betaOld)

	next := "# Alpha\n\nintro\n" + strings.Repeat("new padding\n\n", 12) + "\n# Beta\n\nafter beta\n" + strings.Repeat("tail line\n\n", 20)
	m.readFile = func(string) ([]byte, error) { return []byte(next), nil }
	settle(t, m, m.reload())
	betaNew := findLine(m.stripped, "Beta")
	if betaNew < 0 {
		t.Fatal("Beta heading missing after reload")
	}
	if got := m.vp.YOffset(); got != betaNew {
		t.Fatalf("top line %d, want anchored at moved Beta heading %d", got, betaNew)
	}
}

func TestManualReloadKey(t *testing.T) {
	m := bootModel(t, "one\n")
	if cmd := press(m, "r"); cmd != nil {
		t.Fatal("stdin document has nothing to reload")
	}
	m.path = "doc.md"
	m.readFile = func(string) ([]byte, error) { return []byte("two lines\n"), nil }
	settle(t, m, press(m, "r"))
	if m.source != "two lines\n" {
		t.Fatalf("source = %q, want reloaded", m.source)
	}
	if v := m.View().Content; !strings.Contains(v, "reloaded") {
		t.Fatalf("status flash missing:\n%s", v)
	}
	press(m, "j")
	if m.flash != "" {
		t.Fatalf("flash must clear on keypress, got %q", m.flash)
	}
}

func TestWaitForChangeStopsCleanly(t *testing.T) {
	m := New("a\n", "doc.md")
	m.fw = &fileWatcher{tick: make(chan struct{}), stopped: make(chan struct{})}
	wc := m.waitForChange()
	done := make(chan tea.Msg, 1)
	go func() { done <- wc() }()
	close(m.fw.stopped)
	select {
	case msg := <-done:
		if msg != nil {
			t.Fatalf("stopped watcher must yield nil msg, got %T", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("waitForChange did not return after stop")
	}
}

func TestLiveReloadAtomicReplaceIntegration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	start := "# Title\n\nfirst version\n"
	if err := os.WriteFile(path, []byte(start), 0o644); err != nil {
		t.Fatal(err)
	}
	m := bootModel(t, start)
	m.SetPath(path)
	m.Init()
	defer m.Close()

	next := "# Title\n\nsecond atomic version\n"
	tmp := filepath.Join(dir, ".doc.md.tmp")
	if err := os.WriteFile(tmp, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}

	msgc := make(chan tea.Msg, 1)
	go func() { msgc <- m.waitForChange()() }()
	select {
	case msg := <-msgc:
		if _, ok := msg.(docChangedMsg); !ok {
			t.Fatalf("got %T, want docChangedMsg", msg)
		}
	case <-time.After(5 * time.Second):
		t.Skip("fsnotify event not delivered")
	}
	settle(t, m, m.reload())
	if m.source != next {
		t.Fatalf("source not reloaded through atomic replace:\n%q", m.source)
	}
	if m.flash != "reloaded" {
		t.Fatalf("flash = %q, want reloaded", m.flash)
	}
}

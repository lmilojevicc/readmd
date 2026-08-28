package pager

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 250 * time.Millisecond

type docChangedMsg struct{}

type reloadDoneMsg struct {
	body []byte
	err  error
}

// fileWatcher watches the parent directory of the document (editors save
// atomically by rename, replacing the file's inode, so a watch on the file
// itself would go stale), filters events to the file's base name and emits
// at most one tick per quiet period. The event channels are plain fields so
// tests can drive run without a real fsnotify watcher. The run goroutine
// solely owns the real watcher; stop only signals and waits.
type fileWatcher struct {
	dir, base string

	real   *fsnotify.Watcher
	events <-chan fsnotify.Event
	errs   <-chan error

	tick    chan struct{}
	stopped chan struct{}
	done    chan struct{}

	stopOnce sync.Once
}

func newFileWatcher(path string) *fileWatcher {
	fw := &fileWatcher{
		dir:     filepath.Dir(path),
		base:    filepath.Base(path),
		tick:    make(chan struct{}),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
	w, err := fsnotify.NewWatcher()
	if err == nil {
		err = w.Add(fw.dir)
	}
	if err == nil {
		fw.real = w
		fw.events, fw.errs = w.Events, w.Errors
	} else if w != nil {
		w.Close()
	}
	go fw.run()
	return fw
}

func (fw *fileWatcher) stop() {
	if fw == nil {
		return
	}
	fw.stopOnce.Do(func() { close(fw.stopped) })
	<-fw.done
}

func (fw *fileWatcher) run() {
	defer close(fw.done)
	defer func() {
		if fw.real != nil {
			fw.real.Close()
		}
	}()
	events, errs := fw.events, fw.errs
	if events == nil {
		events, errs = fw.rewatch()
	}
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-fw.stopped:
			return
		case ev, ok := <-events:
			if !ok {
				events, errs = fw.rewatch()
				continue
			}
			if !relevant(fw.dir, fw.base, ev) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(debounceDelay)
			} else if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounceDelay)
			fire = timer.C
		case err, ok := <-errs:
			if !ok || err != nil {
				events, errs = fw.rewatch()
			}
		case <-fire:
			fire = nil
			select {
			case fw.tick <- struct{}{}:
			case <-fw.stopped:
				return
			}
		}
	}
}

func (fw *fileWatcher) rewatch() (<-chan fsnotify.Event, <-chan error) {
	if fw.real != nil {
		fw.real.Close()
		fw.real = nil
	}
	for {
		w, err := fsnotify.NewWatcher()
		if err == nil {
			err = w.Add(fw.dir)
		}
		if err == nil {
			fw.real = w
			select {
			case fw.tick <- struct{}{}:
			case <-fw.stopped:
				return nil, nil
			}
			return w.Events, w.Errors
		}
		if w != nil {
			w.Close()
		}
		select {
		case <-fw.stopped:
			return nil, nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func relevant(dir, base string, ev fsnotify.Event) bool {
	if ev.Name == dir {
		return ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0
	}
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	return strings.EqualFold(filepath.Base(ev.Name), base)
}

// anchorState captures the reading position before a reload: top line,
// rendered line count and heading map of the old render.
type anchorState struct {
	y     int
	total int
	heads []heading
}

// restoreOffset keeps the reading position across a content change: anchored
// to whichever heading was at or above the old top line (at its new line)
// when one existed, else by scroll fraction over rendered lines.
func restoreOffset(a anchorState, newHeads []heading, newTotal int) int {
	if newTotal <= 0 {
		return 0
	}
	idx := -1
	for i, h := range a.heads {
		if h.line > a.y {
			break
		}
		idx = i
	}
	if idx >= 0 && idx < len(newHeads) {
		return min(newHeads[idx].line, newTotal-1)
	}
	frac := float64(a.y) / float64(max(1, a.total))
	return min(int(frac*float64(newTotal)), newTotal-1)
}

func (m *Model) SetPath(path string) { m.path = path }

func (m *Model) Close() {
	m.fw.stop()
	m.fw = nil
}

func (m *Model) waitForChange() tea.Cmd {
	fw := m.fw
	return func() tea.Msg {
		select {
		case <-fw.tick:
			return docChangedMsg{}
		case <-fw.stopped:
			return nil
		}
	}
}

func (m *Model) reload() tea.Cmd {
	path, read := m.path, m.readFile
	return func() tea.Msg {
		b, err := read(path)
		return reloadDoneMsg{body: b, err: err}
	}
}

func (m *Model) applyReload(msg reloadDoneMsg) tea.Cmd {
	// The edited flag is consumed by whichever reload lands first — the
	// editor-exit reload or a watcher tick that fired while the editor ran;
	// debouncing coalesces them. A read error also burns the flag so a later
	// unrelated reload cannot flash edited.
	edited := m.edited
	m.edited = false
	if msg.err != nil {
		m.errMsg = "reload: " + msg.err.Error()
		return nil
	}
	src := string(msg.body)
	if src == "" {
		if m.source != "" {
			m.errMsg = "file truncated to empty"
		} else if edited {
			m.flash = "edited"
		}
		return nil
	}
	if src == m.source {
		if edited {
			m.flash = "edited"
		}
		return nil
	}
	m.stopTargets(true)
	m.links = nil
	m.anchor = &anchorState{y: m.vp.YOffset(), total: len(m.stripped), heads: m.heads}
	m.source = src
	m.locations = nil
	m.pendingLocation = nil
	m.flash = "reloaded"
	if edited {
		m.flash = "edited"
	}
	if m.srcView {
		m.applySource()
		return nil
	}
	return m.requestRender()
}

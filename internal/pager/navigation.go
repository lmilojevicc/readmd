package pager

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

const navigationHistoryLimit = 128

type localLink struct{ path, fragment string }
type navigationRequest struct {
	epoch    uint64
	local    localLink
	footnote *linkTarget
	back     bool
}
type navigationEntry struct {
	path, source       string
	location           documentLocation
	mouse, fromBrowser bool
	reopen             bool
}
type pendingNavigation struct {
	gen               uint64
	origin, candidate *Model
	cancel            context.CancelFunc
	link              localLink
	restore           *navigationEntry
}
type navigationRead struct {
	gen          uint64
	origin       *Model
	source, path string
	err          error
}

func classifyLocalLink(dest string) (localLink, bool, error) {
	invalid := func() (localLink, bool, error) { return localLink{}, true, errors.New("unsupported local link") }
	u, err := url.Parse(dest)
	if err != nil {
		return invalid()
	}
	if u.Scheme != "" {
		return localLink{}, false, nil
	}
	if dest == "" || u.Host != "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery {
		return invalid()
	}
	for _, value := range []string{dest, u.Path, u.Fragment} {
		if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return invalid()
		}
	}
	if strings.HasPrefix(u.Fragment, "^") {
		return invalid()
	}
	if u.Path == "" {
		if !strings.HasPrefix(dest, "#") {
			return invalid()
		}
		return localLink{fragment: u.Fragment}, true, nil
	}
	if filepath.IsAbs(u.Path) || strings.HasPrefix(u.Path, "\\") || strings.Contains(u.Path, "\\") {
		return invalid()
	}
	switch strings.ToLower(filepath.Ext(u.Path)) {
	case ".md", ".markdown":
		return localLink{u.Path, u.Fragment}, true, nil
	default:
		return invalid()
	}
}

func (m *Model) jumpHeading(fragment string, record bool) bool {
	line, ok := headingLine(m.heads, fragment)
	if !ok {
		m.flash = "heading not found: " + safeFilename(fragment)
		return false
	}
	if record {
		m.pushLocation()
	}
	m.vp.SetYOffset(line)
	m.vp.SetXOffset(0)
	m.pendingHeading = &fragment
	m.flash = "heading"
	return true
}

func (a *Application) navigationSnapshot() navigationEntry {
	m := a.current
	entry := navigationEntry{path: m.path, location: m.currentLocation(), mouse: m.mouse, fromBrowser: a.fromBrowser}
	if m.path == "" {
		entry.source = m.source
	}
	return entry
}
func (a *Application) pushNavigation(reopen bool) {
	entry := a.navigationSnapshot()
	entry.reopen = reopen
	a.history = append(a.history, entry)
	if len(a.history) > navigationHistoryLimit {
		a.history = a.history[len(a.history)-navigationHistoryLimit:]
	}
}
func (a *Application) cancelNavigation() {
	a.navigationGen++
	if p := a.navigation; p != nil {
		p.cancel()
		if p.candidate != nil {
			p.candidate.Close()
		}
		a.navigation = nil
	}
}
func (a *Application) navigate(request navigationRequest) tea.Cmd {
	a.cancelNavigation()
	m := a.current
	if a.browsing || m == nil || m.rendering {
		return nil
	}
	if request.footnote != nil {
		a.pushNavigation(false)
		// Managed history lives in Application; standalone Model keeps its own stack.
		m.vp.SetYOffset(request.footnote.definition.line)
		m.revealTargetRegion(request.footnote.definition)
		m.pendingHeading = nil
		m.flash = "footnote " + request.footnote.footnote
		return nil
	}
	if request.back {
		if len(a.history) == 0 {
			return nil
		}
		entry := a.history[len(a.history)-1]
		if !entry.reopen && entry.location.reader == m.reader && entry.path == m.path && (entry.path != "" || entry.source == m.source) {
			a.history = a.history[:len(a.history)-1]
			m.reader, m.mouse = entry.location.reader, entry.mouse
			m.pendingHeading = nil
			m.pendingLocation = &entry.location
			m.syncVPWidth()
			a.fromBrowser = entry.fromBrowser
			m.applyPendingLocation()
			return nil
		}
		return a.startNavigation(localLink{path: entry.path}, &entry)
	}
	if request.local.path == "" {
		if _, ok := headingLine(m.heads, request.local.fragment); !ok {
			m.flash = "heading not found: " + safeFilename(request.local.fragment)
			return nil
		}
		a.pushNavigation(false)
		m.jumpHeading(request.local.fragment, false)
		return nil
	}
	return a.startNavigation(request.local, nil)
}
func (a *Application) startNavigation(link localLink, restore *navigationEntry) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	p := &pendingNavigation{gen: a.navigationGen, origin: a.current, cancel: cancel, link: link, restore: restore}
	a.navigation = p
	path := link.path
	if restore == nil {
		dir := a.cwd
		if a.current.path != "" {
			dir = filepath.Dir(a.current.path)
		}
		path = filepath.Join(dir, path)
	}
	return func() tea.Msg {
		var source string
		var err error
		if restore != nil && restore.path == "" {
			source = restore.source
		} else {
			var body []byte
			body, err = readRegularFile(ctx, path)
			source = string(body)
		}
		return navigationRead{gen: p.gen, origin: p.origin, source: source, path: path, err: err}
	}
}
func (a *Application) navigationFailure(err error) tea.Cmd {
	a.current.flash = "Open: " + safeFilename(err.Error())
	a.cancelNavigation()
	return nil
}
func (a *Application) navigationReadDone(msg navigationRead) tea.Cmd {
	p := a.navigation
	if p == nil || msg.gen != p.gen {
		return nil
	}
	if msg.origin != a.current || a.browsing {
		a.cancelNavigation()
		return nil
	}
	if msg.err != nil {
		return a.navigationFailure(msg.err)
	}
	name := msg.path
	if name == "" {
		name = "(stdin)"
	}
	m, err := a.document(msg.source, name)
	if err != nil {
		return a.navigationFailure(err)
	}
	p.candidate = m
	m.hidden = true
	if p.restore != nil {
		m.reader, m.mouse = p.restore.location.reader, p.restore.mouse
	}
	m.cellWidth, m.cellHeight = a.current.cellWidth, a.current.cellHeight
	_, cmd := m.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	return cmd
}
func (a *Application) navigationRendered(msg renderedMsg) tea.Cmd {
	p := a.navigation
	m := p.candidate
	if msg.gen != m.gen {
		return a.navigationFailure(errors.New("navigation render expired"))
	}
	if msg.err != nil {
		return a.navigationFailure(msg.err)
	}
	line, ok := headingLine(msg.heads, p.link.fragment)
	if !ok {
		return a.navigationFailure(errors.New("heading not found: " + p.link.fragment))
	}
	m.rendering = false
	m.syncView(strings.Split(msg.content, "\n"), msg.stripped, msg.heads, msg.links)
	if p.restore != nil {
		m.pendingLocation = &p.restore.location
		m.applyPendingLocation()
		a.history = a.history[:len(a.history)-1]
		a.fromBrowser = p.restore.fromBrowser
	} else {
		a.pushNavigation(true)
		m.vp.SetYOffset(line)
		m.vp.SetXOffset(0)
		m.pendingHeading = &p.link.fragment
	}
	retired := a.current
	var cleanup tea.Cmd
	if retired.store != nil {
		cleanup = gfxCmd(kittyDeleteAll(retired.store.transmittedIDs()))
	}
	retired.graphicsEpoch++
	retired.Close()
	p.cancel()
	a.navigation = nil
	a.current = m
	m.hidden = false
	if m.store != nil {
		m.store.applyGfx(msg.gfx.tx, msg.gfx.places)
	}
	m.errMsg = msg.warn
	return tea.Sequence(cleanup, tea.Batch(m.Init(), m.graphics(msg.gfx.esc), m.fetchPending(msg.pending)))
}

func (m *Model) navigationCommand(request navigationRequest) tea.Cmd {
	m.navigationEpoch++
	request.epoch = m.navigationEpoch
	return func() tea.Msg { return m.result(request) }
}

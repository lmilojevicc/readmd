package pager

import (
	"fmt"
	"path/filepath"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lmilojevicc/readmd/internal/config"
)

// Application is pointer-owned for the entire Tea program, including error
// teardown: Close must always reach the latest document, not its initial copy.
type Application struct {
	current               *Model
	browser               *fileBrowser
	browsing, fromBrowser bool
	closed                bool
	cwd                   string
	width, height         int
	settings              config.Config
	theme                 config.Theme
}

func NewApplication(source, name, cwd string, c config.Config, theme config.Theme) (*Application, error) {
	a := &Application{cwd: cwd, settings: c, theme: theme.Clone()}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if name != "" {
		m, err := a.document(source, name)
		if err != nil {
			return nil, err
		}
		a.current = m
	} else {
		a.browser = newFileBrowser(cwd)
		a.browsing = true
	}
	return a, nil
}
func (a *Application) document(source, path string) (*Model, error) {
	m := New(source, safeFilename(path))
	m.managed = true
	if path != "(stdin)" {
		m.SetPath(path)
	}
	m.SetTheme(a.theme)
	if err := m.Configure(a.settings); err != nil {
		return nil, err
	}
	m.store.uniqueIDs = true
	ctx := m.store.ctx
	m.readFile = func(path string) ([]byte, error) { return readRegularFile(ctx, path) }
	return m, nil
}
func (a *Application) Init() tea.Cmd {
	if a.browsing {
		return a.browser.refresh()
	}
	return a.current.Init()
}
func (a *Application) Close() {
	if a.closed {
		return
	}
	a.closed = true
	if a.browser != nil {
		a.browser.cancelOpen()
		if a.browser.scanCancel != nil {
			a.browser.scanCancel()
		}
	}
	if a.current != nil {
		a.current.Close()
	}
}

type documentResult struct {
	owner *Model
	msg   tea.Msg
}
type documentGraphics struct {
	owner   *Model
	epoch   uint64
	payload string
	clear   bool
}

func (m *Model) result(msg tea.Msg) tea.Msg {
	if m.managed {
		return documentResult{m, msg}
	}
	return msg
}
func (m *Model) graphics(esc string) tea.Cmd {
	if !m.managed {
		return gfxCmd(esc)
	}
	if esc == "" || m.hidden {
		return nil
	}
	epoch := m.graphicsEpoch
	return func() tea.Msg { return documentGraphics{owner: m, epoch: epoch, payload: esc} }
}

// FilterApplicationMessage runs at Tea's runtime boundary, before RawMsg is
// emitted. Guarding only the command would leave a queued graphics packet able
// to paint over a browser/new document. Exec, Batch, clipboard and other runtime
// messages deliberately remain native.
func FilterApplicationMessage(model tea.Model, msg tea.Msg) tea.Msg {
	g, ok := msg.(documentGraphics)
	if !ok {
		return msg
	}
	a, ok := model.(*Application)
	if !ok || a.closed {
		return nil
	}
	if g.clear && a.current != g.owner {
		return tea.Raw(g.payload)()
	}
	if a.current != g.owner || g.epoch != g.owner.graphicsEpoch || a.browsing != g.clear {
		return nil
	}
	return tea.Raw(g.payload)()
}

func (a *Application) showBrowser() tea.Cmd {
	if a.browser == nil {
		root := a.cwd
		if a.current.path != "" {
			root = filepath.Dir(a.current.path)
		}
		a.browser = newFileBrowser(root)
		a.browser.size(a.width, a.height)
	}
	a.browsing = true
	var cleanup tea.Cmd
	if m := a.current; m != nil {
		m.hidden = true
		m.graphicsEpoch++
		if m.store != nil {
			payload := kittyDeleteAll(m.store.transmittedIDs())
			m.store.mu.Lock()
			clear(m.store.tx)
			clear(m.store.placed)
			m.store.mu.Unlock()
			epoch := m.graphicsEpoch
			if payload != "" {
				cleanup = func() tea.Msg { return documentGraphics{owner: m, epoch: epoch, payload: payload, clear: true} }
			}
		}
	}
	if a.browser.scanGen == 0 {
		return tea.Batch(cleanup, a.browser.refresh())
	}
	return cleanup
}
func (a *Application) returnReader() tea.Cmd {
	a.browser.cancelOpen()
	a.browsing = false
	a.current.hidden = false
	a.current.graphicsEpoch++
	return a.current.requestRender()
}
func (a *Application) quit() tea.Cmd {
	var cmd tea.Cmd = tea.Quit
	if a.current != nil {
		cmd = a.current.quitCmd()
	}
	a.Close()
	return cmd
}

func (a *Application) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.closed {
		return a, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		if a.browser != nil {
			a.browser.size(msg.Width, msg.Height)
		}
		if a.current != nil {
			_, cmd := a.current.Update(msg)
			return a, cmd
		}
		return a, nil
	case uv.CellSizeEvent:
		if a.current != nil {
			_, cmd := a.current.Update(msg)
			return a, cmd
		}
		return a, nil
	case documentResult:
		if msg.owner != a.current {
			return a, nil
		}
		_, cmd := a.current.Update(msg.msg)
		return a, cmd
	case scanFilesMsg:
		b := a.browser
		if b == nil || msg.gen != b.scanGen {
			return a, nil
		}
		b.loading = false
		if msg.err != nil {
			b.notice = "Scan: " + safeFilename(msg.err.Error())
		}
		// Preserve usable cached results when a refresh cannot reach its root.
		if msg.items == nil && msg.err != nil {
			return a, nil
		}
		path := b.selectedPath()
		cmd := b.list.SetItems(msg.items)
		b.filterGen++
		b.filtering = b.list.FilterValue() != ""
		b.filterSelection = path
		b.retainSelection(path)
		return a, guardFilter(cmd, b.filterGen)
	case browserFilterMsg:
		b := a.browser
		if b == nil || msg.gen != b.filterGen {
			return a, nil
		}
		path := b.selectedPath()
		if b.filterSelection != "" {
			path = b.filterSelection
			b.filterSelection = ""
		}
		b.list, _ = b.list.Update(msg.matches)
		b.size(a.width, a.height)
		b.retainSelection(path)
		b.filtering = false
		if b.enterAfterFilter {
			return a, a.acceptAndOpen()
		}
		return a, nil
	case openFileMsg:
		b := a.browser
		if b == nil || !a.browsing || msg.gen != b.openGen {
			return a, nil
		}
		b.opening = false
		if msg.err != nil {
			b.notice = "Open: " + safeFilename(msg.err.Error())
			return a, nil
		}
		m, err := a.document(string(msg.body), msg.path)
		if err != nil {
			b.notice = fmt.Sprintf("Open: %s", safeFilename(err.Error()))
			return a, nil
		}
		var cleanup tea.Cmd
		if a.current != nil {
			if a.current.store != nil {
				cleanup = gfxCmd(kittyDeleteAll(a.current.store.transmittedIDs()))
			}
			a.current.Close()
		}
		a.current = m
		a.browsing = false
		a.fromBrowser = true
		init := m.Init()
		_, render := m.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
		return a, tea.Sequence(cleanup, tea.Batch(init, render))
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, a.quit()
		}
		if a.browsing {
			return a, a.browserKey(msg)
		}
		m := a.current
		normal := !m.targets.active && !m.search.active && !m.tocOpen && !m.helpOpen
		if normal {
			switch msg.String() {
			case "ctrl+f":
				return a, a.showBrowser()
			case "esc":
				if a.fromBrowser && m.search.query == "" {
					return a, a.showBrowser()
				}
			case "q":
				return a, a.quit()
			}
		}
	}
	if a.browsing {
		return a, a.browser.updateList(msg)
	}
	_, cmd := a.current.Update(msg)
	return a, cmd
}
func (a *Application) acceptAndOpen() tea.Cmd {
	b := a.browser
	b.enterAfterFilter = false
	// Do not let the stock zero-match Enter behavior clear the query and open an
	// unrelated file. The latest filter completion owns the candidate set.
	if len(b.list.VisibleItems()) == 0 {
		return nil
	}
	path := b.selectedPath()
	state := list.FilterApplied
	if b.list.FilterValue() == "" {
		state = list.Unfiltered
	}
	b.list.SetFilterState(state)
	b.retainSelection(path)
	b.list.FilterInput.Blur()
	return b.openSelected()
}
func (a *Application) browserKey(msg tea.KeyMsg) tea.Cmd {
	b := a.browser
	k := msg.String()
	if b.list.FilterState() != list.Filtering && k == "q" {
		return a.quit()
	}
	if k == "esc" {
		b.cancelOpen()
		if b.list.Help.ShowAll {
			b.list.Help.ShowAll = false
			b.size(a.width, a.height)
			return nil
		}
		if b.list.FilterState() != list.Unfiltered {
			return b.updateList(msg)
		}
		if a.current != nil {
			return a.returnReader()
		}
		return a.quit()
	}
	if b.list.FilterState() == list.Filtering {
		if k == "enter" {
			if b.filtering {
				b.enterAfterFilter = true
				return nil
			}
			return a.acceptAndOpen()
		}
		return b.updateList(msg)
	}
	switch k {
	case "enter":
		if b.filtering {
			b.enterAfterFilter = true
			return nil
		}
		return b.openSelected()
	case "r":
		return b.refresh()
	}
	b.cancelOpen()
	return b.updateList(msg)
}
func (a *Application) View() tea.View {
	if a.browsing {
		return a.browser.view(a.width, a.height)
	}
	return a.current.View()
}

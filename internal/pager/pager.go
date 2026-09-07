package pager

import (
	"fmt"
	"os"

	"readmd/internal/config"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Model struct {
	vp     viewport.Model
	source string
	title  string
	width  int
	height int

	style          string
	picker         pickerDesign
	readerWidth    int
	tableCellWidth int

	path     string
	fw       *fileWatcher
	anchor   *anchorState
	flash    string
	readFile func(string) ([]byte, error)
	openURL  func(string) error
	mouse    bool

	gfx    bool
	imgCfg ImageConfig
	store  *imageStore

	heads           []heading
	stripped        []string
	base            []string
	links           []linkTarget
	widest          int
	tocOpen         bool
	tocSel          int
	tocFilter       string
	tocPrompt       bool
	tocPreview      bool
	search          searchState
	targets         targetMode
	locations       []documentLocation
	pendingLocation *documentLocation

	helpOpen   bool
	helpTop    int
	helpFilter string
	helpPrompt bool
	edited     bool
	reader     bool

	gen       int
	rendering bool
	errMsg    string
}

func New(source, title string) *Model {
	return &Model{
		vp:             viewport.New(),
		source:         source,
		title:          title,
		readFile:       os.ReadFile,
		openURL:        openExternalURL,
		mouse:          true,
		readerWidth:    readerWidth,
		tableCellWidth: tableCellWidth,
	}
}

// SetStyle sets the glamour style ("auto" = palette-adaptive, "dark",
// "light", "notty").
func (m *Model) SetStyle(name string) error {
	st, err := resolveStyle(name)
	if err != nil {
		return err
	}
	m.style = st
	return nil
}

// Configure applies validated startup settings before Init or any render command.
func (m *Model) Configure(c config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := m.SetStyle(c.Style); err != nil {
		return err
	}
	m.picker = pickerList
	if c.Picker == "vimium" {
		m.picker = pickerVimium
	}
	m.mouse, m.reader = c.Mouse, c.Reader
	m.readerWidth, m.tableCellWidth = c.ReaderWidth, c.TableCellWidth
	m.SetImages(ImageConfig{NoImages: !c.Images, NoRemote: !c.RemoteImages, DocDir: m.docDir()})
	return nil
}

func (m *Model) Init() tea.Cmd {
	if m.path == "" {
		return nil
	}
	m.fw = newFileWatcher(m.path)
	return m.waitForChange()
}

type renderedMsg struct {
	content  string
	err      error
	gen      int
	heads    []heading
	stripped []string
	links    []linkTarget
	pending  []string
	gfx      docGfx
	warn     string
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncVPHeight()
	defer m.syncVPHeight()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.stopTargets(true)
		resized := msg.Width != m.width
		m.width = msg.Width
		m.height = msg.Height
		if m.tocOpen && !m.tocFits() {
			m.closeTOC()
		}
		m.syncVPWidth()
		m.syncVPHeight()
		if resized {
			return m, m.requestRender()
		}
		return m, nil

	case renderedMsg:
		m.stopTargets(true)
		m.rendering = false
		if msg.gen != m.gen {
			return m, m.requestRender()
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.syncView(strings.Split(msg.content, "\n"), msg.stripped, msg.heads, msg.links)
		if m.store != nil {
			m.store.applyGfx(msg.gfx.tx, msg.gfx.places)
		}
		if msg.warn != "" {
			m.errMsg = msg.warn
		}
		return m, tea.Batch(gfxCmd(msg.gfx.esc), m.fetchPending(msg.pending))

	case docChangedMsg:
		m.stopTargets(true)
		cmd := m.reload()
		return m, tea.Batch(cmd, m.waitForChange())

	case reloadDoneMsg:
		m.stopTargets(true)
		return m, m.applyReload(msg)

	case editedMsg:
		m.stopTargets(true)
		if msg.err != nil {
			m.edited = false
			m.errMsg = "edit: " + msg.err.Error()
			return m, nil
		}
		return m, m.reload()

	case imagesDoneMsg:
		if msg.left > 0 {
			return m, nil
		}
		return m, m.requestRender()

	case openedURLMsg:
		m.handleOpenedURL(msg)
		return m, nil

	case tea.MouseWheelMsg:
		return m, m.handleMouseWheel(msg)

	case tea.MouseClickMsg:
		return m, m.handleMouseClick(msg)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quitCmd()
		}
		if m.targets.active {
			return m, m.handleTargetKey(msg)
		}
		// Snapshot before clearing an existing notice and exposing another row.
		if msg.String() == "p" && !m.search.active && !m.tocOpen && !m.helpOpen {
			m.openTargets()
			return m, nil
		}
		m.errMsg = ""
		m.flash = ""
		m.syncVPHeight()
		switch {
		case m.search.active:
			return m, m.handleSearchKey(msg)
		case m.tocOpen:
			return m, m.handleTOCKey(msg)
		case m.helpOpen:
			m.handleHelpKey(msg)
			return m, nil
		default:
			return m, m.handleNormalKey(msg)
		}
	}
	return m, nil
}

// syncView installs a new line set (rendered) into the model.
// Order matters: refreshSearch runs before the pending anchor is consumed,
// and x-clamping happens after widest is refreshed.
func (m *Model) syncView(base, stripped []string, heads []heading, links []linkTarget) {
	m.stopTargets(false)
	m.base = base
	m.stripped = stripped
	m.links = links
	m.heads = heads
	m.widest = widestLine(stripped)
	if m.tocSel >= len(heads) {
		m.tocSel = max(0, len(heads)-1)
	}
	if m.tocOpen {
		if len(heads) == 0 || !m.tocFits() {
			m.closeTOC()
		} else {
			m.setTOCFilter(m.tocFilter)
		}
	}
	m.refreshSearch()
	if m.anchor != nil {
		a := *m.anchor
		m.anchor = nil
		m.vp.SetYOffset(restoreOffset(a, heads, len(stripped)))
	}
	m.clampXWidest()
	m.applyPendingLocation()
}

func (m *Model) handleNormalKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q":
		return m.quitCmd()
	case "esc":
		if m.search.query != "" {
			m.search.query = ""
			m.refreshSearch()
			return nil
		}
		return m.quitCmd()
	case "j", "down":
		m.vp.ScrollDown(1)
	case "k", "up":
		m.vp.ScrollUp(1)
	case "d", "ctrl+d":
		m.vp.HalfPageDown()
	case "u", "ctrl+u":
		m.vp.HalfPageUp()
	case "f", " ", "space", "pgdown":
		m.vp.PageDown()
	case "b", "pgup":
		m.vp.PageUp()
	case "g", "home":
		m.vp.GotoTop()
	case "G", "end":
		m.vp.GotoBottom()
	case "h", "left":
		m.vp.ScrollLeft(m.hStep())
	case "l", "right":
		m.vp.ScrollRight(m.hStep())
	case "0":
		m.vp.SetXOffset(0)
	case "backspace", "ctrl+h":
		return m.backLocation()
	case "o":
		m.openTOC()
	case "/":
		m.openSearch()
	case "?":
		m.toggleHelp()
	case "p":
		m.openTargets()
	case "m":
		m.mouse = !m.mouse
		if m.mouse {
			m.flash = "mouse on"
		} else {
			m.flash = "mouse off"
		}
	case "n":
		m.jumpMatch(1, false)
	case "N":
		m.jumpMatch(-1, false)
	case "r":
		m.reader = !m.reader
		m.anchor = &anchorState{y: m.vp.YOffset(), total: len(m.stripped), heads: m.heads}
		m.syncVPWidth()
		return m.requestRender()
	case "R":
		if m.path == "" {
			return nil
		}
		return m.reload()
	case "c":
		return m.copyRaw()
	case "e":
		return m.editDoc()
	}
	return nil
}

func (m *Model) hStep() int { return max(8, m.width/10) }

type viewChrome struct {
	notice, search bool
}

func (m *Model) chrome() viewChrome {
	remaining := max(0, m.height-1) // status always owns the last row
	var c viewChrome
	if m.search.active && remaining > 0 {
		c.search = true
		remaining--
	}
	if (m.errMsg != "" || m.flash != "") && remaining > 0 {
		c.notice = true
	}
	return c
}

func (m *Model) syncVPHeight() {
	if m.targets.active && (m.flash != m.targets.savedFlash || m.errMsg != m.targets.savedError) {
		m.stopTargets(true)
	}
	if m.height <= 0 {
		return
	}
	if m.targets.active {
		m.vp.SetHeight(m.targets.savedHeight)
		return
	}
	c := m.chrome()
	rows := 1
	if c.notice {
		rows++
	}
	if c.search {
		rows++
	}
	m.vp.SetHeight(max(0, m.height-rows))
}

// readerFrame reports whether the viewport is pinned to the centered reader
// column. Lines retain their unwrapped content width; the margin is a
// display-only prefix (see View).
func (m *Model) readerFrame() (on bool, effW int) {
	if m.reader {
		w, _ := m.readerGeom(true)
		return true, w
	}
	return false, 0
}

// gfxCmd delivers terminal-global kitty graphics escapes (image
// transmissions, virtual placements) through the program's own output buffer:
// the viewport only paints the scrolled window, so escapes embedded in content
// would never reach the terminal when their image is off-screen, and the
// renderer's cell buffer would otherwise re-emit them on every repaint.
func gfxCmd(esc string) tea.Cmd {
	if esc == "" {
		return nil
	}
	return tea.Raw(esc)
}

// quitCmd tears down kitty image state before quitting: delete placements and
// free the data of every image this process transmitted, so nothing ghosts
// into the shell.
func (m *Model) quitCmd() tea.Cmd {
	if m.store == nil || !stdoutIsTTY() {
		return tea.Quit
	}
	payload := kittyDeleteAll(m.store.transmittedIDs())
	if payload == "" {
		return tea.Quit
	}
	return tea.Sequence(tea.Raw(payload), tea.Quit)
}

// requestRender re-renders from raw source. Every call invalidates any
// in-flight render; if one is running, the stale result is discarded on
// arrival and this is retried then.
func (m *Model) requestRender() tea.Cmd {
	m.stopTargets(true)
	m.gen++
	if m.rendering {
		return nil
	}
	src := m.source
	m.rendering = true
	w, _ := m.readerGeom(m.reader)
	wrapWidth := w
	if m.reader {
		wrapWidth = 0
	}
	gen, st, cellWidth := m.gen, m.style, m.tableCellWidth
	o := imgCtx{
		Enabled:  m.gfx,
		NoRemote: m.imgCfg.NoRemote,
		Dir:      m.docDir(),
		Width:    w,
		store:    m.store,
	}
	return func() tea.Msg {
		out, pending, g, err := renderDoc(o, src, wrapWidth, st, cellWidth)
		if err != nil {
			return renderedMsg{err: err, gen: gen}
		}
		base := strings.Split(out, "\n")
		stripped := splitStrip(out)
		heads := extractHeadings(src)
		mapHeadings(heads, stripped)
		links := renderedTargets(src, base, stripped)
		return renderedMsg{
			content: out, gen: gen,
			heads: heads, stripped: stripped, links: links, pending: pending,
			gfx: g, warn: warnFrom(o.store),
		}
	}
}

func warnFrom(st *imageStore) string {
	if st == nil {
		return ""
	}
	return st.takeCacheWarn()
}

func splitStrip(content string) []string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = ansi.Strip(l)
	}
	return lines
}

var (
	errStyle = lipgloss.NewStyle().Faint(true)
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// brandChip is the glow-style brand chip: palette-index magenta bg with black
// fg. helpChip mirrors it on the right edge for the `? help` hint.
const brandChip = "\x1b[45m\x1b[30m\x1b[1m readmd \x1b[m"

const helpChip = "\x1b[45m\x1b[30m ? help \x1b[m"

func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("")
		v.AltScreen = true
		if m.mouse {
			v.MouseMode = tea.MouseModeCellMotion
		}
		return v
	}
	m.syncVPHeight()
	chrome := m.chrome()
	var rows []string
	if m.vp.Height() > 0 {
		body := m.vp.View()
		if m.tocOpen {
			body = m.tocPreviewBody()
		}
		if on, _ := m.readerFrame(); on {
			_, margin := m.readerGeom(true)
			body = padMargin(body, margin)
		}
		switch {
		case m.tocOpen:
			body = m.applyOverlay(body)
		case m.helpOpen:
			body = m.applyHelp(body)
		}
		rows = append(rows, body)
	}
	if chrome.notice {
		if m.errMsg != "" {
			rows = append(rows, ansi.Truncate(errStyle.Render(m.errMsg), m.width, "…"))
		} else {
			rows = append(rows, ansi.Truncate(errStyle.Render(m.flash), m.width, "…"))
		}
	}
	if chrome.search {
		rows = append(rows, m.searchPrompt())
	}
	if m.targets.active {
		body := strings.Join(rows, "\n")
		switch m.picker {
		case pickerList:
			body = m.applyTargetPanel(body)
		case pickerVimium:
			body = m.applyTargetPlan(body, m.targetPlan())
		}
		rows = []string{body}
	}
	rows = append(rows, m.statusBar())
	v := tea.NewView(strings.Join(rows, "\n"))
	v.AltScreen = true
	if m.mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// statusBar lays out glow-style: brand chip left, dimmed filename and view
// info, right side with scroll percent and a help chip mirroring the brand
// chip. The bar itself is transparent (terminal background). Narrowing drops
// the help chip first, then the percent; the brand chip is never truncated.
func (m *Model) statusBar() string {
	if m.targets.active {
		switch m.picker {
		case pickerVimium:
			return m.targetFooter()
		default:
			return m.targetStatus()
		}
	}
	var info []string
	if m.reader {
		info = append(info, "reader")
	}
	if m.vp.XOffset() > 0 {
		info = append(info, fmt.Sprintf("→%d", m.vp.XOffset()))
	}
	view := strings.Join(info, " ")
	pct := fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100)
	metadata := pct
	if view != "" {
		metadata = view + " " + pct
	}
	rights := []string{
		dimStyle.Render(metadata) + "  " + helpChip,
		dimStyle.Render(metadata),
		dimStyle.Render(view),
		"",
	}
	for _, right := range rights {
		avail := m.width - lipgloss.Width(brandChip) - lipgloss.Width(right)
		if avail < 1 {
			continue
		}
		name := ""
		if avail > 3 {
			name = ansi.Truncate(m.title, avail-3, "…")
			// Name-display floor: below 2 cells a truncated name is a bare
			// ellipsis; drop the segment unless the whole name fits.
			if lipgloss.Width(name) < 2 && name != m.title {
				name = ""
			}
		}
		pad := avail - lipgloss.Width(name)
		left := strings.Repeat(" ", pad)
		if name != "" {
			left = " " + dimStyle.Render(name) + strings.Repeat(" ", pad-1)
		}
		return brandChip + left + right
	}
	return brandChip
}

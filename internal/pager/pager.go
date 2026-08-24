package pager

import (
	"fmt"
	"os"
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

	wrapMode  bool
	collapsed bool
	style     string

	path     string
	fw       *fileWatcher
	anchor   *anchorState
	flash    string
	readFile func(string) ([]byte, error)

	gfx    bool
	imgCfg ImageConfig
	store  *imageStore

	heads    []heading
	stripped []string
	base     []string
	widest   int
	tocOpen  bool
	tocSel   int
	search   searchState

	srcView       bool
	wrapBeforeSrc bool
	helpOpen      bool
	helpTop       int
	edited        bool
	reader        bool

	gen       int
	rendering bool
	renderW   int
	errMsg    string
}

func New(source, title string) *Model {
	return &Model{
		vp:       viewport.New(),
		source:   source,
		title:    title,
		wrapMode: true,
		readFile: os.ReadFile,
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

func (m *Model) SetWrap(wrap bool) { m.wrapMode = wrap }

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
	width    int
	gen      int
	heads    []heading
	stripped []string
	pending  []string
	gfx      docGfx
	warn     string
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		resized := msg.Width != m.renderW
		m.renderW = msg.Width
		m.vp.SetWidth(msg.Width)
		m.vp.SetHeight(max(1, msg.Height-1))
		if resized {
			if m.srcView {
				m.clampXWidest()
				return m, nil
			}
			return m, m.requestRender()
		}
		return m, nil

	case renderedMsg:
		m.rendering = false
		if m.srcView {
			return m, nil
		}
		if msg.gen != m.gen {
			return m, m.requestRender()
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.syncView(strings.Split(msg.content, "\n"), msg.stripped, msg.heads)
		if m.store != nil {
			m.store.applyGfx(msg.gfx.tx, msg.gfx.places)
		}
		if msg.warn != "" {
			m.errMsg = msg.warn
		}
		return m, tea.Batch(gfxCmd(msg.gfx.esc), m.fetchPending(msg.pending))

	case docChangedMsg:
		cmd := m.reload()
		return m, tea.Batch(cmd, m.waitForChange())

	case reloadDoneMsg:
		return m, m.applyReload(msg)

	case editedMsg:
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

	case tea.KeyMsg:
		m.errMsg = ""
		m.flash = ""
		if msg.String() == "ctrl+c" {
			return m, m.quitCmd()
		}
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

// syncView installs a new line set (rendered or source) into the model.
// Order matters: refreshSearch runs before the pending anchor is consumed,
// and x-clamping happens after widest is refreshed.
func (m *Model) syncView(base, stripped []string, heads []heading) {
	m.base = base
	m.stripped = stripped
	m.heads = heads
	m.widest = widestLine(stripped)
	if m.tocSel >= len(heads) {
		m.tocSel = max(0, len(heads)-1)
	}
	m.refreshSearch()
	if m.anchor != nil {
		a := *m.anchor
		m.anchor = nil
		m.vp.SetYOffset(restoreOffset(a, heads, len(stripped)))
	}
	m.clampXWidest()
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
		if !m.wrapMode {
			m.vp.ScrollLeft(m.hStep())
		}
	case "l", "right":
		if !m.wrapMode {
			m.vp.ScrollRight(m.hStep())
		}
	case "0":
		if !m.wrapMode {
			m.vp.SetXOffset(0)
		}
	case "w":
		if m.srcView {
			return nil
		}
		m.wrapMode = !m.wrapMode
		m.vp.SetXOffset(0)
		return m.requestRender()
	case "T":
		if m.srcView {
			return nil
		}
		m.collapsed = !m.collapsed
		return m.requestRender()
	case "o":
		m.openTOC()
	case "s":
		return m.toggleSource()
	case "/":
		m.openSearch()
	case "?":
		m.toggleHelp()
	case "n":
		m.jumpMatch(1, false)
	case "N":
		m.jumpMatch(-1, false)
	case "r":
		if m.srcView {
			return nil
		}
		m.reader = !m.reader
		m.anchor = &anchorState{y: m.vp.YOffset(), total: len(m.stripped), heads: m.heads}
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
	if m.srcView {
		return nil
	}
	m.gen++
	if m.rendering {
		return nil
	}
	src := m.source
	if m.collapsed {
		src = collapseTables(src)
	}
	m.rendering = true
	w, margin := readerGeom(m.renderW, m.reader)
	gen, wrap, st := m.gen, m.wrapMode, m.style
	o := imgCtx{
		Enabled:  m.gfx,
		NoRemote: m.imgCfg.NoRemote,
		Dir:      m.docDir(),
		Width:    w,
		store:    m.store,
	}
	return func() tea.Msg {
		out, pending, g, err := renderDoc(o, src, w, wrap, st)
		if err != nil {
			return renderedMsg{err: err, width: w, gen: gen}
		}
		out = padMargin(out, margin)
		stripped := splitStrip(out)
		heads := extractHeadings(src)
		mapHeadings(heads, stripped)
		return renderedMsg{
			content: out, width: w, gen: gen,
			heads: heads, stripped: stripped, pending: pending,
			gfx: g, warn: warnFrom(m.store),
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
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle       = lipgloss.NewStyle().Faint(true)
	hintKeyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
)

// brandChip is the one sanctioned painted background in the app: a glow-style
// brand chip with palette-index magenta bg and black fg.
const brandChip = "\x1b[45m\x1b[30m\x1b[1m readmd \x1b[m"

func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	var b strings.Builder
	body := m.vp.View()
	switch {
	case m.tocOpen:
		body = m.applyOverlay(body)
	case m.helpOpen:
		body = m.applyHelp(body)
	}
	b.WriteString(body)
	b.WriteByte('\n')
	if m.errMsg != "" {
		b.WriteString(ansi.Truncate(errStyle.Render(m.errMsg), m.width, "…"))
		b.WriteByte('\n')
	} else if m.flash != "" {
		b.WriteString(ansi.Truncate(errStyle.Render(m.flash), m.width, "…"))
		b.WriteByte('\n')
	}
	if m.search.active {
		b.WriteString(m.searchPrompt())
		b.WriteByte('\n')
	}
	b.WriteString(m.statusBar())
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// statusBar lays out glow-style: brand chip, then filename, then the right
// side (view mode, scroll percent, help hint). Narrowing drops the hint
// first, then the percent; the chip is never truncated.
func (m *Model) statusBar() string {
	mode := "wrap"
	if !m.wrapMode {
		mode = "nowrap"
	}
	view := "render"
	if m.srcView {
		view = "source"
	}
	info := view + " " + mode
	if m.reader && !m.srcView {
		info += " reader"
	}
	if !m.wrapMode && m.vp.XOffset() > 0 {
		info += fmt.Sprintf(" →%d", m.vp.XOffset())
	}
	pct := fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100)
	hint := hintKeyStyle.Render("?") + statusBarStyle.Render(" help")
	rights := []string{
		statusBarStyle.Render(info+" "+pct+"  ") + hint,
		statusBarStyle.Render(info + " " + pct),
		statusBarStyle.Render(info),
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
			left = " " + name + strings.Repeat(" ", pad-1)
		}
		return brandChip + statusBarStyle.Render(left) + right
	}
	return brandChip
}

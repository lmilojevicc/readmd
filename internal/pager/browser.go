package pager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

type markdownFile struct {
	path, display string
	modified      time.Time
}

func (f markdownFile) Title() string       { return f.display }
func (f markdownFile) Description() string { return f.modified.Format("02 Jan 2006 15:04 MST") }
func (f markdownFile) FilterValue() string { return f.display }

// Filesystem identities never pass through this display-only escaping.
func safeFilename(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && n == 1 {
			fmt.Fprintf(&b, "\\x%02x", s[0])
		} else if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d' && (r < 0xe0020 || r > 0xe007f)) || r == '\u2028' || r == '\u2029' {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
		s = s[n:]
	}
	return b.String()
}

func discoverMarkdown(ctx context.Context, root string) ([]list.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Walk the root alias, but never descend through symlinks found inside it.
	walkRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var files []markdownFile
	var firstErr error
	err = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if d.IsDir() {
			if path != walkRoot {
				switch d.Name() {
				case ".git", ".hg", ".svn", "node_modules", "vendor", ".venv":
					return filepath.SkipDir
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return err
		}
		files = append(files, markdownFile{path: filepath.Join(root, rel), display: safeFilename(rel), modified: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	items := make([]list.Item, len(files))
	for i := range files {
		items[i] = files[i]
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return items, firstErr
}

func readMarkdown(ctx context.Context, path string) ([]byte, error) {
	body, err := readRegularFile(ctx, path)
	if err == nil && len(body) == 0 {
		err = errors.New("empty document")
	}
	return body, err
}

func readRegularFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Nonblocking open prevents a file replaced by a FIFO from hanging the UI worker.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	var body []byte
	buf := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := f.Read(buf)
		body = append(body, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

// The stock delegate styles rune ranges. Expand every match to whole graphemes
// before it inserts ANSI, using the same sanitized string as Title/FilterValue.
func filenameFilter(term string, targets []string) []list.Rank {
	ranks := list.DefaultFilter(term, targets)
	for i := range ranks {
		rank := &ranks[i]
		matched := make(map[int]bool, len(rank.MatchedIndexes))
		for _, n := range rank.MatchedIndexes {
			matched[n] = true
		}
		rank.MatchedIndexes = nil
		g := uniseg.NewGraphemes(targets[rank.Index])
		offset := 0
		for g.Next() {
			n := utf8.RuneCountInString(g.Str())
			hit := false
			for j := offset; j < offset+n; j++ {
				hit = hit || matched[j]
			}
			if hit {
				for j := offset; j < offset+n; j++ {
					rank.MatchedIndexes = append(rank.MatchedIndexes, j)
				}
			}
			offset += n
		}
	}
	return ranks
}

type fileBrowser struct {
	list                                          list.Model
	root, notice, filterSelection                 string
	loading, opening, filtering, enterAfterFilter bool
	scanGen, openGen, filterGen                   uint64
	scanCancel, openCancel                        context.CancelFunc
	scanGate, openGate                            chan struct{}
}

type scanFilesMsg struct {
	gen   uint64
	items []list.Item
	err   error
}
type openFileMsg struct {
	gen  uint64
	path string
	body []byte
	err  error
}
type browserFilterMsg struct {
	gen     uint64
	matches list.FilterMatchesMsg
}

func newFileBrowser(root string) *fileBrowser {
	plain := lipgloss.NewStyle()
	dim := plain.Faint(true)
	accent := plain.Foreground(lipgloss.Color("5"))
	d := list.NewDefaultDelegate()
	normal := plain.PaddingLeft(2)
	selected := accent.Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("5")).PaddingLeft(1)
	d.Styles = list.DefaultItemStyles{NormalTitle: normal, NormalDesc: normal.Faint(true), SelectedTitle: selected, SelectedDesc: selected, DimmedTitle: normal.Faint(true), DimmedDesc: normal.Faint(true), FilterMatch: plain.Bold(true)}
	open := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	refresh := key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	quit := key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	back := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	d.ShortHelpFunc = func() []key.Binding {
		return []key.Binding{open, key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "page")), refresh, quit}
	}
	d.FullHelpFunc = func() [][]key.Binding { return [][]key.Binding{{open, refresh, back, quit}} }
	l := list.New(nil, d, 0, 0)
	l.Title = "readmd"
	l.DisableQuitKeybindings()
	l.Filter = filenameFilter
	l.SetStatusBarItemName("document", "documents")
	l.KeyMap.PrevPage = key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "prev page"))
	l.KeyMap.NextPage = key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "next page"))
	l.KeyMap.AcceptWhileFiltering = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	l.Styles = list.Styles{
		TitleBar: plain.Padding(0, 0, 1, 2), Title: accent.Bold(true), Spinner: dim,
		Filter: textinput.DefaultStyles(false), DefaultFilterCharacterMatch: plain.Bold(true),
		StatusBar: dim.Padding(0, 0, 1, 2), StatusEmpty: dim, StatusBarActiveFilter: plain,
		StatusBarFilterCount: dim, NoItems: dim, PaginationStyle: plain.PaddingLeft(2),
		HelpStyle: plain.Padding(1, 0, 0, 2), ActivePaginationDot: accent.SetString("•"),
		InactivePaginationDot: dim.SetString("•"), ArabicPagination: dim, DividerDot: dim.SetString(" • "),
	}
	l.FilterInput.SetStyles(l.Styles.Filter)
	l.Help.Styles = help.Styles{Ellipsis: dim, ShortKey: plain, ShortDesc: dim, ShortSeparator: dim, FullKey: plain, FullDesc: dim, FullSeparator: dim}
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
	return &fileBrowser{list: l, root: root, scanGate: make(chan struct{}, 1), openGate: make(chan struct{}, 1)}
}

func (b *fileBrowser) selectedPath() string {
	if f, ok := b.list.SelectedItem().(markdownFile); ok {
		return f.path
	}
	return ""
}
func (b *fileBrowser) retainSelection(path string) {
	for i, item := range b.list.VisibleItems() {
		if item.(markdownFile).path == path {
			b.list.Select(i)
			return
		}
	}
	b.list.Select(min(b.list.Index(), max(0, len(b.list.VisibleItems())-1)))
}
func (b *fileBrowser) cancelOpen() {
	b.openGen++
	if b.openCancel != nil {
		b.openCancel()
		b.openCancel = nil
	}
	b.opening = false
	b.enterAfterFilter = false
}
func (b *fileBrowser) refresh() tea.Cmd {
	b.cancelOpen()
	b.scanGen++
	b.filterGen++
	b.filtering = false
	if b.scanCancel != nil {
		b.scanCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.scanCancel = cancel
	b.loading = true
	b.notice = ""
	gen, root, gate := b.scanGen, b.root, b.scanGate
	return func() tea.Msg {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		defer func() { <-gate }()
		items, err := discoverMarkdown(ctx, root)
		return scanFilesMsg{gen, items, err}
	}
}
func (b *fileBrowser) openSelected() tea.Cmd {
	path := b.selectedPath()
	if path == "" {
		return nil
	}
	b.cancelOpen()
	ctx, cancel := context.WithCancel(context.Background())
	b.openCancel = cancel
	b.opening = true
	b.notice = ""
	gen, gate := b.openGen, b.openGate
	return func() tea.Msg {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		defer func() { <-gate }()
		body, err := readMarkdown(ctx, path)
		return openFileMsg{gen, path, body, err}
	}
}

// Only public list filter completions are enveloped; Tea runtime commands and
// text-input cursor messages remain native. Batch children need individual guards.
func guardFilter(cmd tea.Cmd, gen uint64) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		switch msg := msg.(type) {
		case list.FilterMatchesMsg:
			return browserFilterMsg{gen, msg}
		case tea.BatchMsg:
			for i := range msg {
				msg[i] = guardFilter(msg[i], gen)
			}
			return msg
		default:
			return msg
		}
	}
}
func (b *fileBrowser) updateList(msg tea.Msg) tea.Cmd {
	before, state := b.list.FilterValue(), b.list.FilterState()
	var cmd tea.Cmd
	b.list, cmd = b.list.Update(msg)
	// Re-entering an applied query does not schedule a replacement filter. Keep
	// any pending SetItems refilter and its saved selection alive across it.
	reediting := state == list.FilterApplied && b.list.FilterState() == list.Filtering
	if before != b.list.FilterValue() || (state != b.list.FilterState() && !reediting) {
		b.filterGen++
		b.filterSelection = ""
		b.cancelOpen()
		b.filtering = before != b.list.FilterValue() && b.list.FilterState() == list.Filtering
	}
	return guardFilter(cmd, b.filterGen)
}
func (b *fileBrowser) size(w, h int) { b.list.SetSize(max(1, w), max(1, h-1)) }
func (b *fileBrowser) view(w, h int) tea.View {
	notice := b.notice
	if b.loading {
		notice = "Scanning…"
	} else if b.opening {
		notice = "Opening…"
	}
	content := b.list.View()
	if notice != "" {
		content = notice + "\n" + content
	}
	lines := strings.Split(content, "\n")
	lines = lines[:min(len(lines), max(0, h))]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, w), "")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

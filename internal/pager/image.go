package pager

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	// Fallback until the terminal reports valid cell dimensions in pixels.
	cellW         = 10
	cellH         = 20
	maxCellPixels = 1000
	maxDim        = 1000
	maxSrcDim     = 4000
	chunkSize     = 4096
	maxFetchSize  = 32 << 20
	fetchTimeout  = 10 * time.Second

	maxFetchConcurrent = 4
	// rowcolumn-diacritics.txt defines at least 255 entries in every kitty
	// release; beyond that a cell cannot be addressed.
	maxDiacritic = 255
)

const imgTokenFmt = "readmd-img-%s%d"

func graphicsDetected(getenv func(string) string) bool {
	if getenv("TMUX") != "" && getenv("READMD_TMUX_PASSTHROUGH") != "1" {
		return false
	}
	term := getenv("TERM")
	return getenv("KITTY_WINDOW_ID") != "" ||
		strings.Contains(term, "kitty") ||
		getenv("WEZTERM_PANE") != "" ||
		getenv("GHOSTTY_RESOURCES_DIR") != ""
}

type ImageConfig struct {
	DocDir   string
	NoImages bool
	NoRemote bool
}

type imgCtx struct {
	Enabled               bool
	NoRemote              bool
	Dir                   string
	Width                 int
	CellWidth, CellHeight int
	store                 *imageStore
}

type imgData struct {
	w, h int
	pix  []byte
}

// figure is one displayable image occurrence in the document. id is the kitty
// image id the pixel data was transmitted under; several figures may share an
// id when the same image appears more than once.
type figure struct {
	token      string
	orig       string
	id         int
	cols, rows int
}

type imgRef struct {
	id   int
	data *imgData
}

type place struct {
	id         int
	cols, rows int
}

// docGfx carries the terminal-global graphics escapes a rendered document
// needs: data transmissions not yet sent plus virtual placements whose
// geometry changed. Escapes are emitted by View (outside the scrolled
// content); the store is marked only when the frame is actually applied.
type docGfx struct {
	esc    string
	tx     []int
	places []place
}

type imageStore struct {
	mu        sync.Mutex
	refs      map[string]*imgRef
	byID      map[int]*imgRef
	bad       map[string]bool
	inflight  map[string]bool
	tx        map[int]bool
	placed    map[int][2]int
	pending   int
	sem       chan struct{}
	cacheWarn string
}

func newImageStore() *imageStore {
	return &imageStore{
		refs:     map[string]*imgRef{},
		byID:     map[int]*imgRef{},
		bad:      map[string]bool{},
		inflight: map[string]bool{},
		tx:       map[int]bool{},
		placed:   map[int][2]int{},
		sem:      make(chan struct{}, maxFetchConcurrent),
	}
}

func (st *imageStore) get(key string) *imgRef {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.refs[key]
}

func (st *imageStore) ensureRef(key string, d *imgData) *imgRef {
	st.mu.Lock()
	defer st.mu.Unlock()
	if r := st.refs[key]; r != nil {
		return r
	}
	r := &imgRef{id: len(st.byID) + 1, data: d}
	st.refs[key] = r
	st.byID[r.id] = r
	return r
}

func (st *imageStore) data(id int) *imgData {
	st.mu.Lock()
	defer st.mu.Unlock()
	if r := st.byID[id]; r != nil {
		return r.data
	}
	return nil
}

func (st *imageStore) fail(key string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.bad[key] = true
}

func (st *imageStore) isBad(key string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.bad[key]
}

func (st *imageStore) startFetch(key string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.inflight[key] || st.bad[key] || st.refs[key] != nil {
		return false
	}
	st.inflight[key] = true
	st.pending++
	return true
}

// finishFetch clears one in-flight key and reports how many fetches are still
// pending. Keys never started leave the counter untouched.
func (st *imageStore) finishFetch(key string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.inflight[key] {
		return st.pending
	}
	delete(st.inflight, key)
	st.pending--
	return st.pending
}

func (st *imageStore) txNeeded(id int) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return !st.tx[id]
}

func (st *imageStore) placedGeom(id int) [2]int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.placed[id]
}

func (st *imageStore) applyGfx(tx []int, places []place) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, id := range tx {
		st.tx[id] = true
	}
	for _, p := range places {
		st.placed[p.id] = [2]int{p.cols, p.rows}
	}
}

func (st *imageStore) recordCacheWarn(msg string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.cacheWarn = msg
}

func (st *imageStore) takeCacheWarn() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	msg := st.cacheWarn
	st.cacheWarn = ""
	return msg
}

func (st *imageStore) transmittedIDs() []int {
	st.mu.Lock()
	defer st.mu.Unlock()
	var ids []int
	for id := range st.tx {
		if st.tx[id] {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

var userCacheDir = os.UserCacheDir

func cachedImagePath(root, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(root, hex.EncodeToString(sum[:]))
}

func (st *imageStore) cachePath(key string) string {
	base, err := userCacheDir()
	if err != nil {
		return ""
	}
	return cachedImagePath(filepath.Join(base, "readmd", "images"), key)
}

// saveRemote writes cache content atomically: temp file in the destination
// directory, then rename.
func saveRemote(path string, b []byte) error {
	if path == "" {
		return errors.New("no image cache directory available")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".readmd-img-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // Best-effort cleanup, including after rename.
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close() // Preserve the write error.
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func insertFigures(src string, o imgCtx) (string, []figure, []string) {
	bsrc := []byte(src)
	doc := md.Parser().Parse(text.NewReader(bsrc))
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return src, nil, nil
	}
	nonceStr := hex.EncodeToString(nonce[:])
	var edits []edit
	var figs []figure
	var pending []string
	seen := map[string]bool{}
	// This visitor never returns an error.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		p, ok := n.(*ast.Paragraph)
		if !ok || p.Parent() == nil || p.Parent().Kind() != ast.KindDocument || p.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		img := soleImage(p, bsrc)
		if img == nil {
			return ast.WalkContinue, nil
		}
		dest := string(img.Destination)
		u, err := url.Parse(dest)
		if err != nil || dest == "" {
			return ast.WalkContinue, nil
		}
		var key string
		var remote bool
		switch {
		case u.Scheme == "http" || u.Scheme == "https":
			remote = true
			key = dest
		case u.Scheme == "" && o.Dir != "":
			key = filepath.Join(o.Dir, dest)
		default:
			return ast.WalkContinue, nil
		}
		ref := loadImg(key, remote, o)
		if ref == nil {
			if remote && !o.NoRemote && !seen[key] && !o.store.isBad(key) {
				pending = append(pending, key)
				seen[key] = true
			}
			return ast.WalkContinue, nil
		}
		cols, rows := fitCells(ref.data.w, ref.data.h, max(1, o.Width-2), o.CellWidth, o.CellHeight)
		if cols > maxDiacritic || rows > maxDiacritic {
			return ast.WalkContinue, nil
		}
		ls := p.Lines()
		start := lineStart(src, ls.At(0).Start)
		end := lineEnd(src, ls.At(ls.Len()-1).Stop)
		if end < len(src) && src[end] == '\n' {
			end++
		}
		token := fmt.Sprintf(imgTokenFmt, nonceStr, len(figs))
		edits = append(edits, edit{start, end, token + "\n"})
		figs = append(figs, figure{
			token: token,
			orig:  strings.TrimSuffix(src[start:end], "\n"),
			id:    ref.id,
			cols:  cols,
			rows:  rows,
		})
		return ast.WalkContinue, nil
	})
	if len(edits) == 0 {
		return src, nil, pending
	}
	return applyEdits(src, edits), figs, pending
}

func soleImage(p *ast.Paragraph, src []byte) *ast.Image {
	var img *ast.Image
	for c := p.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Image:
			if img != nil {
				return nil
			}
			img = t
		case *ast.Text:
			if strings.TrimSpace(string(t.Segment.Value(src))) != "" {
				return nil
			}
		default:
			return nil
		}
	}
	return img
}

func loadImg(key string, remote bool, o imgCtx) *imgRef {
	if r := o.store.get(key); r != nil {
		return r
	}
	path := key
	if remote {
		path = o.store.cachePath(key)
		if path == "" {
			return nil
		}
	}
	b, err := readCapped(path)
	if err == nil && len(b) > maxFetchSize {
		err = fmt.Errorf("image exceeds %d bytes", maxFetchSize)
	}
	if err != nil {
		if !remote {
			o.store.fail(key)
		}
		return nil
	}
	d, err := decodeImage(b)
	if err != nil {
		o.store.fail(key)
		return nil
	}
	return o.store.ensureRef(key, d)
}

// readCapped reads path only if it is a regular file, capped at
// maxFetchSize+1 bytes so devices/FIFOs/huge files cannot wedge or OOM us.
func readCapped(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // Read-only file; no pending writes.
	return io.ReadAll(io.LimitReader(f, maxFetchSize+1))
}

func validCellSize(w, h int) bool {
	return w > 0 && h > 0 && w <= maxCellPixels && h <= maxCellPixels
}

// fitCells uses decoded pixels, choosing columns first and rounding the
// proportional height outward. U=1 fits the image without changing its aspect;
// spare space in the final row is preferable to shrinking it a second time.
func fitCells(sw, sh, avail, cw, ch int) (cols, rows int) {
	if sw <= 0 || sh <= 0 || sw > maxDim || sh > maxDim {
		return maxDiacritic + 1, maxDiacritic + 1
	}
	if !validCellSize(cw, ch) {
		cw, ch = cellW, cellH
	}
	cols = min(max(1, avail), max(1, sw/cw))
	// A sub-cell image still reserves one column, but not an upscaled height.
	// Bounded decoded dimensions and cell metrics keep these products safe.
	numerator := min(sw, cols*cw) * sh
	denominator := sw * ch
	rows = max(1, (numerator+denominator-1)/denominator)
	return cols, rows
}

func decodeImage(b []byte) (*imgData, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSrcDim || cfg.Height > maxSrcDim {
		return nil, fmt.Errorf("image dimensions %dx%d out of range", cfg.Width, cfg.Height)
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	w, h := cfg.Width, cfg.Height
	if longest := max(w, h); longest > maxDim {
		w = max(1, w*maxDim/longest)
		h = max(1, h*maxDim/longest)
	}
	return &imgData{w: w, h: h, pix: resample(im, w, h)}, nil
}

func resample(im image.Image, dw, dh int) []byte {
	b := im.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := make([]byte, dw*dh*4)
	for y := range dh {
		sy := b.Min.Y + min(sh-1, y*sh/dh)
		for x := range dw {
			sx := b.Min.X + min(sw-1, x*sw/dw)
			r, g, bl, a := im.At(sx, sy).RGBA()
			o := (y*dw + x) * 4
			dst[o], dst[o+1], dst[o+2], dst[o+3] = byte(r>>8), byte(g>>8), byte(bl>>8), byte(a>>8)
		}
	}
	return dst
}

// renderFigure renders the full placeholder grid: rows lines of cols
// placeholder cells each. The cells are ordinary text, so bubbletea repaints
// move and clip them, and following text never overlaps the image area.
func renderFigure(f figure) string {
	lines := make([]string, f.rows)
	for r := range f.rows {
		lines[r] = placeholderLine(f.id, f.cols, r)
	}
	return strings.Join(lines, "\n")
}

// placeholderLine emits one row of cols Unicode-placeholder cells. The image
// id rides in the foreground color (24-bit true color), the virtual placement
// id in the underline color (one placement per image, p=id), and the exact
// grid position in row/column diacritics — spelled out per cell so clipped or
// rescrolled runs never depend on inheritance from a hidden neighbor.
func placeholderLine(imgID, cols, row int) string {
	r, g, b := idRGB(imgID)
	var sb strings.Builder
	fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[58;2;%d;%d;%dm", r, g, b, r, g, b)
	msb := imgID >> 24 & 0xff
	for c := range cols {
		sb.WriteRune(kitty.Placeholder)
		sb.WriteRune(kitty.Diacritic(row))
		sb.WriteRune(kitty.Diacritic(c))
		if msb != 0 {
			sb.WriteRune(kitty.Diacritic(msb))
		}
	}
	sb.WriteString("\x1b[39m\x1b[59m")
	return sb.String()
}

func idRGB(id int) (r, g, b int) {
	return id >> 16 & 0xff, id >> 8 & 0xff, id & 0xff
}

// kittyTx builds the chunked RGBA transmission escape. Sent once per image id
// per process; s/v are the natural pixel dimensions of the payload.
func kittyTx(id, w, h int, pix []byte) string {
	b64 := base64.StdEncoding.EncodeToString(pix)
	var chunks []string
	for len(b64) > chunkSize {
		chunks = append(chunks, b64[:chunkSize])
		b64 = b64[chunkSize:]
	}
	chunks = append(chunks, b64)
	var b strings.Builder
	for i, c := range chunks {
		switch i {
		case 0:
			m := 1
			if len(chunks) == 1 {
				m = 0
			}
			fmt.Fprintf(&b, "\x1b_Ga=t,f=32,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\", w, h, id, m, c)
		default:
			m := 0
			if i < len(chunks)-1 {
				m = 1
			}
			fmt.Fprintf(&b, "\x1b_Gq=2,m=%d;%s\x1b\\", m, c)
		}
	}
	return b.String()
}

// kittyPlace creates the invisible virtual placement that prototypes the
// placeholder grid: U=1, sized purely in cells (c/r). Never mixed with s/v.
func kittyPlace(id, cols, rows int) string {
	o := kitty.Options{
		Action:           kitty.Put,
		ID:               id,
		PlacementID:      id,
		Columns:          cols,
		Rows:             rows,
		VirtualPlacement: true,
		Quiet:            2,
	}
	return "\x1b_G" + o.String() + "\x1b\\"
}

// kittyDeleteAll removes every transmitted image and its placements (and
// frees their data) so nothing ghosts into the shell after quit.
func kittyDeleteAll(ids []int) string {
	var b strings.Builder
	for _, id := range ids {
		o := kitty.Options{
			Action:          kitty.Delete,
			ID:              id,
			Delete:          kitty.DeleteID,
			DeleteResources: true,
			Quiet:           2,
		}
		b.WriteString("\x1b_G" + o.String() + "\x1b\\")
	}
	return b.String()
}

// gfxControls computes the escapes needed to show figs right now: transmit
// data for ids never sent, and (re)create virtual placements whose geometry
// differs from what the terminal already has. Nothing is marked applied here;
// that happens when the frame is accepted.
func gfxControls(st *imageStore, figs []figure) docGfx {
	var g docGfx
	var sb strings.Builder
	txSeen := map[int]bool{}
	for _, f := range figs {
		cols, rows := f.cols, f.rows
		if !txSeen[f.id] && st.txNeeded(f.id) {
			if d := st.data(f.id); d != nil {
				sb.WriteString(kittyTx(f.id, d.w, d.h, d.pix))
				g.tx = append(g.tx, f.id)
				txSeen[f.id] = true
			}
		}
		if geom := [2]int{cols, rows}; st.placedGeom(f.id) != geom {
			sb.WriteString(kittyPlace(f.id, cols, rows))
			g.places = append(g.places, place{id: f.id, cols: cols, rows: rows})
		}
	}
	g.esc = sb.String()
	return g
}

// spliceFigures replaces each unique token line with the drawn block. It
// reports ok=false when detection fails (duplicate, missing or out-of-order
// tokens); the caller must then skip graphics controls.
func spliceFigures(out string, figs []figure, draw func(figure) string) (string, bool) {
	if len(figs) == 0 {
		return out, true
	}
	lines := strings.Split(out, "\n")
	stripped := make([]string, len(lines))
	for i, l := range lines {
		stripped[i] = ansi.Strip(l)
	}
	idx := make([]int, len(figs))
	for i := range figs {
		found, n := -1, 0
		for j, s := range stripped {
			if strings.Contains(s, figs[i].token) {
				n++
				if found < 0 {
					found = j
				}
			}
		}
		if n != 1 {
			return restoreFigLines(out, figs), false
		}
		idx[i] = found
		if i > 0 && idx[i] <= idx[i-1] {
			return restoreFigLines(out, figs), false
		}
	}
	var outLines []string
	prev := 0
	for i := range figs {
		outLines = append(outLines, lines[prev:idx[i]]...)
		outLines = append(outLines, strings.Split(draw(figs[i]), "\n")...)
		prev = idx[i] + 1
	}
	outLines = append(outLines, lines[prev:]...)
	return strings.Join(outLines, "\n"), true
}

// restoreFigLines swaps every line carrying a figure token back to the
// original markdown lines so alt text survives a bail-out.
func restoreFigLines(out string, figs []figure) string {
	byTok := make(map[string]*figure, len(figs))
	for i := range figs {
		byTok[figs[i].token] = &figs[i]
	}
	done := map[*figure]bool{}
	var kept []string
	for _, l := range strings.Split(out, "\n") {
		s := ansi.Strip(l)
		f := (*figure)(nil)
		for tok, cand := range byTok {
			if strings.Contains(s, tok) {
				f = cand
				break
			}
		}
		if f == nil {
			kept = append(kept, l)
			continue
		}
		if done[f] {
			continue
		}
		done[f] = true
		kept = append(kept, strings.Split(f.orig, "\n")...)
	}
	return strings.Join(kept, "\n")
}

type imagesDoneMsg struct{ left int }

var (
	httpClient  = &http.Client{Timeout: fetchTimeout}
	stdoutIsTTY = func() bool {
		fi, err := os.Stdout.Stat()
		return err == nil && fi.Mode()&os.ModeCharDevice != 0
	}
)

// fetchOne downloads, decodes and caches one remote image. The caller holds a
// store semaphore slot bounding concurrency.
func fetchOne(key string, st *imageStore) tea.Msg {
	if !stdoutIsTTY() {
		st.fail(key)
		return imagesDoneMsg{left: st.finishFetch(key)}
	}
	resp, err := httpClient.Get(key)
	if err != nil {
		st.fail(key)
		return imagesDoneMsg{left: st.finishFetch(key)}
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close() // Read result/status determines success, not cleanup.
		st.fail(key)
		return imagesDoneMsg{left: st.finishFetch(key)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchSize+1))
	_ = resp.Body.Close() // Read result/status determines success, not cleanup.
	switch {
	case err != nil || len(b) > maxFetchSize:
	default:
		var d *imgData
		if d, err = decodeImage(b); err == nil {
			st.ensureRef(key, d)
			if werr := saveRemote(st.cachePath(key), b); werr != nil {
				st.recordCacheWarn("image cache: " + werr.Error())
			}
			return imagesDoneMsg{left: st.finishFetch(key)}
		}
	}
	st.fail(key)
	return imagesDoneMsg{left: st.finishFetch(key)}
}

func (m *Model) fetchPending(urls []string) tea.Cmd {
	if m.store == nil || len(urls) == 0 {
		return nil
	}
	st := m.store
	var cmds []tea.Cmd
	for _, u := range urls {
		if !st.startFetch(u) {
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			st.sem <- struct{}{}
			defer func() { <-st.sem }()
			return fetchOne(u, st)
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *Model) SetImages(cfg ImageConfig) {
	m.imgCfg = cfg
	m.gfx = graphicsDetected(os.Getenv) && !cfg.NoImages
	m.store = newImageStore()
}

func (m *Model) docDir() string {
	if m.path == "" {
		return ""
	}
	return filepath.Dir(m.path)
}

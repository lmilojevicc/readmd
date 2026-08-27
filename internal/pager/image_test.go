package pager

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

func envmap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestGraphicsDetection(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"plain", map[string]string{"TERM": "xterm-256color"}, false},
		{"kitty term", map[string]string{"TERM": "xterm-kitty"}, true},
		{"term contains kitty", map[string]string{"TERM": "kitty"}, true},
		{"kitty window id", map[string]string{"TERM": "screen", "KITTY_WINDOW_ID": "3"}, true},
		{"wezterm", map[string]string{"TERM": "xterm-256color", "WEZTERM_PANE": "0"}, true},
		{"ghostty", map[string]string{"GHOSTTY_RESOURCES_DIR": "/opt/ghostty"}, true},
		{"tmux blocks", map[string]string{"TERM": "xterm-kitty", "TMUX": "/tmp/tmux-0/default,1,0"}, false},
		{"tmux passthrough on", map[string]string{"TMUX": "/tmp/tmux", "READMD_TMUX_PASSTHROUGH": "1", "KITTY_WINDOW_ID": "1"}, true},
		{"tmux passthrough off value", map[string]string{"TMUX": "/tmp/tmux", "READMD_TMUX_PASSTHROUGH": "yes", "TERM": "xterm-kitty"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := graphicsDetected(envmap(tc.env)); got != tc.want {
				t.Errorf("graphicsDetected = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFitCells(t *testing.T) {
	for _, tc := range []struct {
		name            string
		sw, sh, avail   int
		wantCols, wantR int
	}{
		{"natural small", 100, 40, 1000, 10, 2},
		{"wide capped by avail floors", 1920, 1080, 80, 80, 22},
		{"huge source capped", 3000, 3000, 200, 100, 50},
		{"scaled to width", 2000, 1000, 60, 60, 15},
		{"tiny rounds up to one cell", 5, 5, 10, 1, 1},
		{"tall image", 10, 2000, 50, 1, 50},
		{"no avail clamps to one cell", 100, 40, 0, 1, 1},
		{"never exceeds natural pixels", 25, 45, 100, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cols, rows := fitCells(tc.sw, tc.sh, tc.avail)
			if cols != tc.wantCols || rows != tc.wantR {
				t.Errorf("fitCells(%d,%d,%d) = %d,%d, want %d,%d",
					tc.sw, tc.sh, tc.avail, cols, rows, tc.wantCols, tc.wantR)
			}
		})
	}
}

func TestKittyTxGolden(t *testing.T) {
	apc := kittyTx(7, 1, 1, []byte{255, 0, 0, 255})
	want := "\x1b_Ga=t,f=32,s=1,v=1,i=7,q=2,m=0;/wAA/w==\x1b\\"
	if apc != want {
		t.Fatalf("got %q, want %q", apc, want)
	}
	if w := ansi.StringWidth(apc); w != 0 {
		t.Errorf("APC must be zero-width after strip, got %d", w)
	}
}

func TestKittyTxChunks(t *testing.T) {
	pix := make([]byte, 64*64*4)
	for i := range pix {
		pix[i] = byte(i % 251)
	}
	apc := kittyTx(3, 64, 64, pix)
	if n := strings.Count(apc, "\x1b_G"); n != 6 {
		t.Fatalf("chunk count = %d, want 6 (16384 bytes -> 21848 b64 chars)", n)
	}
	var payload bytes.Buffer
	for i, part := range strings.Split(strings.TrimSuffix(apc, "\x1b\\"), "\x1b\\") {
		body := strings.TrimPrefix(part, "\x1b_G")
		ctrl, data, ok := strings.Cut(body, ";")
		if !ok {
			t.Fatalf("chunk %d malformed: %q", i, part)
		}
		payload.WriteString(data)
		switch i {
		case 0:
			if !strings.HasPrefix(ctrl, "a=t,f=32,s=64,v=64,i=3,q=2,m=1") {
				t.Errorf("first chunk ctrl = %q", ctrl)
			}
		case 5:
			if ctrl != "q=2,m=0" {
				t.Errorf("last chunk ctrl = %q, want q=2,m=0", ctrl)
			}
		default:
			if ctrl != "q=2,m=1" {
				t.Errorf("middle chunk %d ctrl = %q", i, ctrl)
			}
		}
		if len(data) > chunkSize {
			t.Errorf("chunk %d payload %d > %d", i, len(data), chunkSize)
		}
	}
	got, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pix) {
		t.Fatal("concatenated chunk payloads must decode back to the raw RGBA pixels")
	}
}

func TestKittyPlaceGolden(t *testing.T) {
	got := kittyPlace(5, 12, 3)
	want := "\x1b_Gq=2,i=5,p=5,U=1,c=12,r=3,a=p\x1b\\"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestKittyDeleteAllGolden(t *testing.T) {
	got := kittyDeleteAll([]int{1, 2})
	want := "\x1b_Gq=2,i=1,d=I,a=d\x1b\\\x1b_Gq=2,i=2,d=I,a=d\x1b\\"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if kittyDeleteAll(nil) != "" {
		t.Fatal("no ids must produce no escape")
	}
}

func TestPlaceholderLineGolden(t *testing.T) {
	line := placeholderLine(42, 3, 0)
	want := "\x1b[38;2;0;0;42m\x1b[58;2;0;0;42m" +
		"\U0010EEEE\u0305\u0305" +
		"\U0010EEEE\u0305\u030D" +
		"\U0010EEEE\u0305\u030E" +
		"\x1b[39m\x1b[59m"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
	row1 := placeholderLine(42, 1, 1)
	want1 := "\x1b[38;2;0;0;42m\x1b[58;2;0;0;42m\U0010EEEE\u030D\u0305\x1b[39m\x1b[59m"
	if row1 != want1 {
		t.Fatalf("row 1: got %q, want %q", row1, want1)
	}
}

func TestPlaceholderIDEncoding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		id       int
		wantFg   string
		wantCell string
	}{
		{
			name:     "small id no msb diacritic",
			id:       1,
			wantFg:   "\x1b[38;2;0;0;1m",
			wantCell: "\U0010EEEE\u0305\u0305",
		},
		{
			name:     "spec example 42 plus msb 2",
			id:       42 + (2 << 24),
			wantFg:   "\x1b[38;2;0;0;42m",
			wantCell: "\U0010EEEE\u0305\u0305\u030E",
		},
		{
			name:     "id spread across rgb bytes",
			id:       0x0A0B0C,
			wantFg:   "\x1b[38;2;10;11;12m",
			wantCell: "\U0010EEEE\u0305\u0305",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := placeholderLine(tc.id, 1, 0)
			if !strings.Contains(line, tc.wantFg) {
				t.Errorf("foreground must encode image id %d: %q", tc.id, line)
			}
			r, g, b := idRGB(tc.id)
			ul := fmt.Sprintf("\x1b[58;2;%d;%d;%dm", r, g, b)
			if !strings.Contains(line, ul) {
				t.Errorf("underline color must carry the placement id: %q", line)
			}
			if !strings.Contains(line, tc.wantCell) {
				t.Errorf("cell encoding wrong: %q", line)
			}
			if w := ansi.StringWidth(line); w != 1 {
				t.Errorf("one placeholder cell must be width 1, got %d", w)
			}
		})
	}
}

func TestPlaceholderLineWidthInvariant(t *testing.T) {
	for _, id := range []int{1, 42, 0x0A0B0C, 42 + (2 << 24)} {
		for _, cols := range []int{1, 5, 39} {
			line := placeholderLine(id, cols, 3)
			if w := ansi.StringWidth(line); w != cols {
				t.Errorf("id %d cols %d: StringWidth = %d", id, cols, w)
			}
			for cut := range cols + 3 {
				tr := ansi.Truncate(line, cut, "")
				if w := ansi.StringWidth(tr); w > cut {
					t.Fatalf("id %d cols %d truncate(%d): width %d > %d", id, cols, cut, w, cut)
				}
			}
		}
	}
	stripped := ansi.Strip(placeholderLine(1, 7, 0))
	if got := strings.Count(stripped, string(kitty.Placeholder)); got != 7 {
		t.Errorf("Strip must preserve placeholders, got %d", got)
	}
}

func pngFixture(t *testing.T, w, h int, px func(x, y int) color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			im.SetRGBA(x, y, px(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func redPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return pngFixture(t, w, h, func(int, int) color.RGBA { return color.RGBA{255, 0, 0, 255} })
}

func TestDecodeImage(t *testing.T) {
	b := pngFixture(t, 40, 30, func(x, y int) color.RGBA {
		return color.RGBA{uint8(x * 6), uint8(y * 8), 0, 255}
	})
	d, err := decodeImage(b)
	if err != nil {
		t.Fatal(err)
	}
	if d.w != 40 || d.h != 30 || len(d.pix) != 40*30*4 {
		t.Fatalf("dims = %dx%d pixlen %d, want 40x30 %d", d.w, d.h, len(d.pix), 40*30*4)
	}
	if d.pix[0] != uint8(0) || d.pix[3] != 255 {
		t.Errorf("corner pixel alpha/premult wrong: %v", d.pix[:4])
	}

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"corrupt", []byte("not an image")},
		{"oversized", pngFixture(t, 5000, 4, func(int, int) color.RGBA { return color.RGBA{1, 2, 3, 4} })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeImage(tc.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCachedImagePath(t *testing.T) {
	got := cachedImagePath("/cache/root", "a")
	want := "/cache/root/ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"
	if got != filepath.FromSlash(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCacheWritePerms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested")
	p := cachedImagePath(root, "https://example.com/x.png")
	if err := saveRemote(p, []byte("img")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", dir.Mode().Perm())
	}
}

func TestSaveRemoteAtomic(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "img")
	for _, want := range [][]byte{[]byte("first"), []byte("second-longer")} {
		if err := saveRemote(p, want); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("overwrite: got %q, want %q", got, want)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "img" {
		t.Errorf("rename must leave no temp files behind: %v", entries)
	}
	if err := saveRemote("", []byte("x")); err == nil {
		t.Error("empty cache path must be reported as an error")
	}
}

type imgEnv struct {
	dir   string
	store *imageStore
}

func newImgEnv(t *testing.T) imgEnv {
	t.Helper()
	return imgEnv{dir: t.TempDir(), store: newImageStore()}
}

func (e imgEnv) ctx() imgCtx {
	return imgCtx{Enabled: true, Dir: e.dir, Width: 80, store: e.store}
}

func (e imgEnv) writeImg(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(e.dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInsertFiguresDetection(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 8, 8))

	for _, tc := range []struct {
		name        string
		src         string
		ctx         imgCtx
		wantTokens  int
		wantPending []string
	}{
		{"solo figure replaced", "# T\n\n![cat](cat.png)\n\nafter\n", e.ctx(), 1, nil},
		{"inline keeps alt", "see ![cat](cat.png) here\n", e.ctx(), 0, nil},
		{"two images one para keep alt", "![a](cat.png)![b](cat.png)\n", e.ctx(), 0, nil},
		{"caption text keeps alt", "![a](cat.png) figure 1\n", e.ctx(), 0, nil},
		{"list nested keeps alt", "- ![a](cat.png)\n", e.ctx(), 0, nil},
		{"blockquote keeps alt", "> ![a](cat.png)\n", e.ctx(), 0, nil},
		{"missing file keeps alt", "![a](gone.png)\n", e.ctx(), 0, nil},
		{"unsupported scheme keeps alt", "![a](ftp://h/x.png)\n", e.ctx(), 0, nil},
		{"disabled no-op", "![a](cat.png)\n", imgCtx{}, 0, nil},
		{"remote pending", "![r](https://x.test/y.png)\n", e.ctx(), 0,
			[]string{"https://x.test/y.png"}},
		{"remote skipped with NoRemote", "![r](https://x.test/y.png)\n",
			imgCtx{Enabled: true, NoRemote: true, Dir: e.dir, Width: 80, store: e.store}, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marked, figs, pending := insertFigures(tc.src, tc.ctx)
			if got := strings.Count(marked, "readmd-img-"); got != tc.wantTokens {
				t.Fatalf("tokens = %d, want %d; marked=%q", got, tc.wantTokens, marked)
			}
			if len(figs) != tc.wantTokens {
				t.Fatalf("figs = %d, want %d", len(figs), tc.wantTokens)
			}
			if strings.Join(pending, "|") != strings.Join(tc.wantPending, "|") {
				t.Fatalf("pending = %v, want %v", pending, tc.wantPending)
			}
			if tc.wantTokens == 0 && len(pending) == 0 && marked != tc.src {
				t.Fatalf("source must be untouched, got %q", marked)
			}
			for _, f := range figs {
				if f.orig == "" || !strings.Contains(tc.src, f.orig) {
					t.Errorf("figure must keep its original markdown, got %q", f.orig)
				}
			}
		})
	}
}

func TestLoadImgRejectsNonRegularFiles(t *testing.T) {
	e := newImgEnv(t)
	dir := filepath.Join(e.dir, "subdir")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	o := e.ctx()
	if r := loadImg(e.dir, false, o); r != nil {
		t.Error("directory must not load")
	}
	if r := loadImg(filepath.Join(dir, "..", "missing.png"), false, o); r != nil {
		t.Error("missing file must not load")
	}
	if !e.store.isBad(e.dir) || !e.store.isBad(filepath.Join(dir, "..", "missing.png")) {
		t.Error("local failures must mark keys bad so alt text sticks")
	}
	fifo := filepath.Join(e.dir, "fifo.png")
	if err := os.WriteFile(fifo, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.writeImg(t, "dev.png", bytes.Repeat([]byte{0}, maxFetchSize+1))
	for _, key := range []string{fifo, filepath.Join(e.dir, "dev.png")} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if r := loadImg(key, false, e.ctx()); r != nil {
				t.Errorf("%s must not load", key)
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("loadImg wedged on %s (must stat-guard regular files and cap reads)", key)
		}
	}
}

func TestInsertFiguresNonceDiffersPerRender(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 8, 8))
	src := "![a](cat.png)\n"
	a, _, _ := insertFigures(src, e.ctx())
	b, _, _ := insertFigures(src, e.ctx())
	if a == b {
		t.Fatal("figure tokens must differ between renders")
	}
}

func TestSpliceFiguresMechanics(t *testing.T) {
	draw := func(f figure) string {
		_, rows := fitCells(f.w, f.h, 80)
		return strings.Repeat("R\n", rows-1) + "R"
	}
	mk := func(tok, orig string, w, h int) figure {
		return figure{token: tok, orig: orig, w: w, h: h}
	}
	for _, tc := range []struct {
		name   string
		out    string
		figs   []figure
		want   string
		wantOK bool
	}{
		{
			name: "single replacement reserves rows",
			out:  "before\ntok1\nafter",
			figs: []figure{mk("tok1", "![](a.png)", 10, 40)},
			want: "before\nR\nR\nafter", wantOK: true,
		},
		{
			name: "two figures in order",
			out:  "a\ntok1\nb\ntok2\nc",
			figs: []figure{mk("tok1", "![](a)", 10, 20), mk("tok2", "![](b)", 10, 60)},
			want: "a\nR\nb\nR\nR\nR\nc", wantOK: true,
		},
		{
			name: "duplicate token restores original markdown",
			out:  "a\ntok1\nb\ntok1\nc",
			figs: []figure{mk("tok1", "![alt text](a.png)", 10, 20)},
			want: "a\n![alt text](a.png)\nb\nc", wantOK: false,
		},
		{
			name: "missing token leaves output intact",
			out:  "a\ntok2\nc",
			figs: []figure{mk("tok1", "![alt text](a.png)", 10, 20)},
			want: "a\ntok2\nc", wantOK: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := spliceFigures(tc.out, tc.figs, draw)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("got (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestRenderDocEndToEnd(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 40, 60))
	src := "# T\n\n![cat](cat.png)\n\nafter the picture\n"

	o := e.ctx()
	o.Width = 80
	out, pending, g, err := renderDoc(o, src, 80, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("local figure must not be pending: %v", pending)
	}
	cols, rows := fitCells(40, 60, 78)
	txWant := "\x1b_Ga=t,f=32,s=40,v=60,i=1,q=2,m="
	if !strings.Contains(g.esc, txWant) {
		t.Errorf("gfx controls must transmit natural pixels once: %q", g.esc)
	}
	plcWant := "\x1b_Gq=2,i=1,p=1,U=1,c=4,r=3,a=p\x1b\\"
	if !strings.Contains(g.esc, plcWant) {
		t.Errorf("gfx controls must create the virtual placement: %q", g.esc)
	}
	lines := strings.Split(out, "\n")
	var grid []int
	for i, l := range lines {
		if n := strings.Count(l, string(kitty.Placeholder)); n > 0 {
			grid = append(grid, i)
		}
	}
	if len(grid) != rows {
		t.Fatalf("expected %d placeholder rows, got %d in %q", rows, len(grid), out)
	}
	for k, i := range grid {
		n := strings.Count(lines[i], string(kitty.Placeholder))
		if n != cols {
			t.Errorf("line %d: %d placeholder cells, want %d", i, n, cols)
		}
		if w := ansi.StringWidth(lines[i]); w != cols {
			t.Errorf("line %d width = %d, want %d", i, w, cols)
		}
		if !strings.Contains(lines[i], string(kitty.Diacritic(k))) {
			t.Errorf("grid row %d (line %d) must encode its row via diacritic: %q", k, i, lines[i])
		}
	}
	start := grid[0]
	next := ""
	for i := start + rows; i < len(lines); i++ {
		if s := strings.TrimSpace(ansi.Strip(lines[i])); s != "" {
			next = s
			break
		}
	}
	if next != "after the picture" {
		t.Errorf("content below image displaced, found %q", next)
	}
	if strings.Contains(ansi.Strip(out), "readmd-img-") {
		t.Error("placeholder leaked into output")
	}
	if strings.Contains(ansi.Strip(out), "cat") {
		t.Error("alt text should be replaced by the image")
	}
	if strings.Count(out, "\x1b_G") != 0 {
		t.Error("graphics control escapes must not ride the scrolled content")
	}
}

func TestRenderDocAltFallbacks(t *testing.T) {
	e := newImgEnv(t)
	for _, tc := range []struct {
		name string
		src  string
		ctx  imgCtx
	}{
		{"missing local file shows alt", "![kitty pic](gone.png)\n", e.ctx()},
		{"remote pending shows alt until loaded", "![kitty pic](https://x.test/y.png)\n", e.ctx()},
		{"NoRemote shows alt", "![kitty pic](https://x.test/y.png)\n",
			imgCtx{Enabled: true, NoRemote: true, Dir: e.dir, Width: 80, store: e.store}},
		{"NoImages shows alt", "![kitty pic](https://x.test/y.png)\n",
			imgCtx{Dir: e.dir, Width: 80, store: e.store}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, _, err := renderDoc(tc.ctx, tc.src, 80, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(ansi.Strip(out), "kitty pic") {
				t.Errorf("alt text missing: %q", ansi.Strip(out))
			}
			if strings.Contains(out, "\x1b_G") {
				t.Errorf("unexpected kitty escape: %q", out)
			}
		})
	}
}

func TestGfxControlsAppliedOnce(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "big.png", redPNG(t, 600, 200))
	src := "![a](big.png)\n"
	o := e.ctx()

	_, _, g1, err := renderDoc(o, src, 80, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g1.esc, "a=t") || !strings.Contains(g1.esc, "a=p") {
		t.Fatalf("first render must transmit and place: %q", g1.esc)
	}
	o.store.applyGfx(g1.tx, g1.places)

	_, _, g2, err := renderDoc(o, src, 80, "")
	if err != nil {
		t.Fatal(err)
	}
	if g2.esc != "" {
		t.Fatalf("unchanged geometry must not re-transmit or re-place: %q", g2.esc)
	}

	o.Width = 40
	_, _, g3, err := renderDoc(o, src, 40, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g3.esc, "a=p") {
		t.Fatalf("geometry change must recreate the placement: %q", g3.esc)
	}
	if strings.Contains(g3.esc, "a=t") {
		t.Fatalf("geometry change must never re-transmit data: %q", g3.esc)
	}
}

func TestRenderImagesOffIsUnchanged(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 8, 8))
	src := "# T\n\n![cat](cat.png)\n\n| A | B |\n| - | - |\n| x | y |\n"
	viaRender, err := Render(src, 60)
	if err != nil {
		t.Fatal(err)
	}
	viaDoc, _, _, err := renderDoc(imgCtx{}, src, 60, "")
	if err != nil {
		t.Fatal(err)
	}
	if viaRender != viaDoc {
		t.Fatal("images-off path must be identical to legacy Render")
	}
	if strings.Contains(viaRender, "\x1b_G") || strings.Contains(ansi.Strip(viaRender), "readmd-img-") {
		t.Fatal("images-off must never emit graphics or placeholders")
	}
}

func TestDocLiteralImageMarkerDoesNotBreakSplice(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 8, 8))
	src := "readmd-img-000000 prose mentioning a marker\n\n![a](cat.png)\n"
	out, _, _, err := renderDoc(e.ctx(), src, 60, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(ansi.Strip(out), "readmd-img-"); got != 1 {
		t.Fatalf("doc literal must survive once, splice must not leak: %d in %q", got, ansi.Strip(out))
	}
}

func TestModelImageFlow(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 40, 30))
	doc := filepath.Join(e.dir, "doc.md")
	if err := os.WriteFile(doc, []byte("# T\n\n![cat](cat.png)\n\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	m := New(string(b), "doc.md")
	m.SetPath(doc)
	m.SetImages(ImageConfig{DocDir: e.dir})

	oldTTY := stdoutIsTTY
	stdoutIsTTY = func() bool { return false }
	defer func() { stdoutIsTTY = oldTTY }()

	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	*m = *nm.(*Model)
	settle(t, m, cmd)

	view := m.View().Content
	if !strings.Contains(view, string(kitty.Placeholder)) {
		t.Fatalf("rendered view should contain kitty placeholder cells:\n%q", view)
	}
	if strings.Contains(view, "a=t,f=32") {
		t.Fatal("graphics control escapes must not ride the frame content")
	}

	rm := New("# T\n\n![r](https://x.test/y.png)\n\nafter\n", "doc.md")
	rm.SetImages(ImageConfig{})
	nm, cmd = rm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	*rm = *nm.(*Model)
	settle(t, rm, cmd)
	if strings.Contains(rm.View().Content, string(kitty.Placeholder)) {
		t.Fatal("failed remote fetch must fall back to alt text permanently")
	}
	if rm.store == nil || !rm.store.isBad("https://x.test/y.png") {
		t.Fatal("failed fetch must mark the key bad in the store")
	}
}

func TestFetchTTYGaurdMarksBadWithoutNetwork(t *testing.T) {
	st := newImageStore()
	oldTTY := stdoutIsTTY
	stdoutIsTTY = func() bool { return false }
	defer func() { stdoutIsTTY = oldTTY }()
	msg := fetchOne("https://x.test/nope.png", st)
	done, ok := msg.(imagesDoneMsg)
	if !ok {
		t.Fatalf("expected imagesDoneMsg, got %T", msg)
	}
	if done.left != 0 {
		t.Errorf("left = %d, want 0", done.left)
	}
	if !st.isBad("https://x.test/nope.png") {
		t.Fatal("non-TTY stdout must fail the fetch without network access")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// probeBody records whether the response body was ever read.
type probeBody struct{ read *atomic.Bool }

func (b probeBody) Read([]byte) (int, error) {
	b.read.Store(true)
	return 0, io.EOF
}

func respWithBody(status int, body io.Reader) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d status", status),
		Body:       io.NopCloser(body),
		Header:     http.Header{},
	}
}

func TestFetchStatusBeforeBody(t *testing.T) {
	oldClient := httpClient
	read := &atomic.Bool{}
	httpClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return respWithBody(http.StatusForbidden, probeBody{read: read}), nil
	})}
	defer func() { httpClient = oldClient }()
	oldTTY := stdoutIsTTY
	stdoutIsTTY = func() bool { return true }
	defer func() { stdoutIsTTY = oldTTY }()

	st := newImageStore()
	st.startFetch("https://x.test/deny.png")
	msg := fetchOne("https://x.test/deny.png", st)
	if done := msg.(imagesDoneMsg); done.left != 0 {
		t.Errorf("left = %d, want 0", done.left)
	}
	if read.Load() {
		t.Error("non-200 response body must not be read before the status check")
	}
	if !st.isBad("https://x.test/deny.png") {
		t.Error("non-200 must mark the key bad")
	}
}

func TestFetchSemaphoreBound(t *testing.T) {
	oldClient := httpClient
	var cur, peak atomic.Int64
	slow := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		cur.Add(-1)
		return respWithBody(http.StatusOK, bytes.NewReader(redPNG(t, 6, 6))), nil
	})
	httpClient = &http.Client{Transport: slow, Timeout: fetchTimeout}
	defer func() { httpClient = oldClient }()
	oldTTY := stdoutIsTTY
	stdoutIsTTY = func() bool { return true }
	defer func() { stdoutIsTTY = oldTTY }()

	st := newImageStore()
	urls := make([]string, 8)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://x.test/%d.png", i)
	}
	m := &Model{store: st}
	batch, ok := m.fetchPending(urls)().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batch)
	}
	if len(batch) != len(urls) {
		t.Fatalf("expected %d fetch commands, got %d", len(urls), len(batch))
	}
	var wg sync.WaitGroup
	for _, c := range batch {
		wg.Add(1)
		go func(c func() tea.Msg) {
			defer wg.Done()
			c()
		}(c)
	}
	wg.Wait()
	if p := peak.Load(); p > maxFetchConcurrent {
		t.Errorf("peak concurrency = %d, want <= %d", p, maxFetchConcurrent)
	}
	for _, u := range urls {
		if st.get(u) == nil {
			t.Errorf("%s must have been fetched and stored", u)
		}
	}
}

func TestBatchedRenderOnlyAtZero(t *testing.T) {
	m := New("# T\n\nx\n", "doc.md")
	m.SetImages(ImageConfig{})
	st := m.store
	st.startFetch("u1")
	st.startFetch("u2")

	left := st.finishFetch("u1")
	nm, cmd := m.Update(imagesDoneMsg{left: left})
	*m = *nm.(*Model)
	if cmd != nil {
		t.Fatal("a render must not fire while other fetches are pending")
	}

	left = st.finishFetch("u2")
	nm, cmd = m.Update(imagesDoneMsg{left: left})
	*m = *nm.(*Model)
	if cmd == nil {
		t.Fatal("the last completion must trigger exactly one re-render")
	}
	if _, ok := cmd().(renderedMsg); !ok {
		t.Fatalf("expected renderedMsg, got %T", cmd())
	}
}

func TestCacheWarnSurfacesOnce(t *testing.T) {
	e := newImgEnv(t)
	e.writeImg(t, "cat.png", redPNG(t, 8, 8))
	m := New("# T\n\n![cat](cat.png)\n\nx\n", "doc.md")
	m.SetImages(ImageConfig{})
	m.width = 80
	m.imgCfg.DocDir = e.dir
	m.store.recordCacheWarn("image cache: disk full")

	cmd := m.requestRender()
	if msg := cmd().(renderedMsg); msg.warn != "image cache: disk full" {
		t.Fatalf("warn = %q, want cache warning surfaced once", msg.warn)
	}
	nm, _ := m.Update(cmd().(renderedMsg))
	*m = *nm.(*Model)
	if msg := m.requestRender()().(renderedMsg); msg.warn != "" {
		t.Fatalf("warning must be consumed, got %q", msg.warn)
	}

	oldDir := userCacheDir
	userCacheDir = func() (string, error) { return "", errors.New("no cache dir") }
	defer func() { userCacheDir = oldDir }()
	oldClient := httpClient
	httpClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return respWithBody(http.StatusOK, bytes.NewReader(redPNG(t, 6, 6))), nil
	})}
	defer func() { httpClient = oldClient }()
	oldTTY := stdoutIsTTY
	stdoutIsTTY = func() bool { return true }
	defer func() { stdoutIsTTY = oldTTY }()

	st := newImageStore()
	st.startFetch("https://x.test/warn.png")
	fetchOne("https://x.test/warn.png", st)
	if got := st.takeCacheWarn(); got == "" {
		t.Error("failed cache write must be recorded as a warning")
	}
}

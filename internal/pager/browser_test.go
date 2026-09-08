package pager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func TestDiscoverMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name              string
		missing, canceled bool
	}{
		{name: "recursive"}, {name: "missing", missing: true}, {name: "canceled", canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			names := []string{"z.MARKDOWN", "a.md", ".github/hidden.MD", "ignored.md", "nested/a.md", "note.txt", ".git/no.md", ".hg/no.md", ".svn/no.md", "node_modules/no.md", "vendor/no.md", ".venv/no.md"}
			for _, name := range names {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.md\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "a.md"), filepath.Join(root, "link.md")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "directory.md"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(filepath.Join(root, "fifo.md"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			if tc.missing {
				root = filepath.Join(root, "missing")
			}
			items, err := discoverMarkdown(ctx, root)
			if tc.canceled || tc.missing {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range items {
				got = append(got, item.FilterValue())
			}
			want := []string{".github/hidden.MD", "a.md", "ignored.md", "link.md", "nested/a.md", "z.MARKDOWN"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v want %v", got, want)
			}
		})
	}
}

func TestDiscoveryPartialErrors(t *testing.T) {
	for _, kind := range []string{"broken link", "permission"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			good := filepath.Join(root, "good.md")
			if err := os.WriteFile(good, []byte("good"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "broken link" {
				if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "bad.md")); err != nil {
					t.Fatal(err)
				}
			} else {
				dir := filepath.Join(root, "private")
				if err := os.Mkdir(dir, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
				if _, err := os.ReadDir(dir); err == nil {
					t.Skip("process can read mode-000 directory")
				}
			}
			items, err := discoverMarkdown(context.Background(), root)
			if err == nil || len(items) != 1 || items[0].(markdownFile).path != good {
				t.Fatalf("items=%v err=%v", items, err)
			}
		})
	}
}

func TestReadMarkdown(t *testing.T) {
	for _, kind := range []string{"regular", "empty", "missing", "directory", "fifo", "unreadable", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "doc.md")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
			default:
				body := "raw\r\n# Markdown\n"
				if kind == "empty" {
					body = ""
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "unreadable" {
					if err := os.Chmod(path, 0000); err != nil {
						t.Fatal(err)
					}
					if _, err := os.ReadFile(path); err == nil {
						t.Skip("process can read mode-000 file")
					}
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "canceled" {
				cancel()
			}
			body, err := readMarkdown(ctx, path)
			if kind == "regular" {
				if err != nil || string(body) != "raw\r\n# Markdown\n" {
					t.Fatalf("%q %v", body, err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid file")
			}
		})
	}
}

func TestSafeFilename(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{".github/中文-e\u0301-👩🏽‍💻.md", ".github/中文-e\u0301-👩🏽‍💻.md"},
		{"-a\n\t\r\x1b[31m\u202e.md", "-a\\u000a\\u0009\\u000d\\u001b[31m\\u202e.md"},
		{"🏴\U000e0067\U000e0062\U000e007f.md", "🏴\U000e0067\U000e0062\U000e007f.md"},
		{"bad\xff.md", "bad\\xff.md"}, {"a\u2028b\u2066c", "a\\u2028b\\u2066c"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := safeFilename(tc.raw); got != tc.want || !utf8.ValidString(got) {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFilenameFilterGraphemes(t *testing.T) {
	for _, tc := range []struct {
		term, title string
		indexes     []int
	}{
		{"e", "e\u0301.md", []int{0, 1}}, {"👩", "👩🏽‍💻.md", []int{0, 1, 2, 3}}, {"中", "中文.md", []int{0}}, {"000a", safeFilename("\n.md"), []int{2, 3, 4, 5}},
	} {
		t.Run(tc.title, func(t *testing.T) {
			ranks := filenameFilter(tc.term, []string{tc.title})
			if len(ranks) != 1 || !reflect.DeepEqual(ranks[0].MatchedIndexes, tc.indexes) {
				t.Fatalf("ranks=%+v", ranks)
			}
			b := newFileBrowser("")
			b.list.SetItems([]list.Item{markdownFile{display: tc.title}})
			b.list.SetFilterText(tc.term)
			for _, w := range []int{1, 2, 4, 12, 80} {
				b.size(w, 12)
				v := b.view(w, 12).Content
				if w == 80 {
					if !strings.Contains(ansi.Strip(v), tc.title) {
						t.Fatalf("safe title changed: %q", v)
					}
					g := uniseg.NewGraphemes(tc.title)
					for g.Next() {
						if utf8.RuneCountInString(g.Str()) > 1 && !strings.Contains(v, g.Str()) {
							t.Fatalf("ANSI split a grapheme: %q", v)
						}
					}
				}
				if !utf8.ValidString(v) {
					t.Fatalf("invalid UTF8: %q", v)
				}
				for _, line := range strings.Split(v, "\n") {
					if ansi.StringWidth(line) > w {
						t.Fatalf("width %d: %q", w, line)
					}
				}
			}
		})
	}
}

func browserItems(root string, n int) []list.Item {
	items := make([]list.Item, n)
	for i := range items {
		name := fmt.Sprintf("doc%02d.md", i)
		items[i] = markdownFile{path: filepath.Join(root, name), display: name}
	}
	return items
}
func TestBrowserPaginationAndState(t *testing.T) {
	for _, n := range []int{0, 1, 4, 5, 40} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			b := newFileBrowser(t.TempDir())
			b.list.SetItems(browserItems(b.root, n))
			b.size(80, 20)
			if n > 0 {
				b.list.Select(n - 1)
			}
			selected := b.selectedPath()
			for _, size := range [][2]int{{80, 40}, {15, 10}, {1, 1}, {0, 0}, {80, 20}} {
				b.size(size[0], size[1])
				if b.selectedPath() != selected {
					t.Fatal("resize lost selection")
				}
				v := b.view(size[0], size[1]).Content
				if size[1] > 0 && len(strings.Split(v, "\n")) > size[1] {
					t.Fatal("height overflow")
				}
				for _, line := range strings.Split(v, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("width overflow")
					}
				}
			}
			if n > 5 {
				b.list.Select(0)
				b.updateList(tea.KeyPressMsg{Code: 'l', Text: "l"})
				if b.list.Paginator.Page != 1 {
					t.Fatal("l did not page")
				}
				b.updateList(tea.KeyPressMsg{Code: 'h', Text: "h"})
				if b.list.Paginator.Page != 0 {
					t.Fatal("h did not page")
				}
			}
		})
	}
}

func TestBrowserNotices(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		setup      func(*fileBrowser)
	}{
		{"loading", "Scanning", func(b *fileBrowser) { b.loading = true }},
		{"opening", "Opening", func(b *fileBrowser) { b.opening = true }},
		{"empty", "No documents", func(*fileBrowser) {}},
		{"error", "Scan: denied", func(b *fileBrowser) { b.notice = "Scan: denied" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newFileBrowser("")
			b.size(80, 20)
			tc.setup(b)
			if v := ansi.Strip(b.view(80, 20).Content); !strings.Contains(v, tc.want) {
				t.Fatalf("%q", v)
			}
		})
	}
}

func TestScanCancellationAndGeneration(t *testing.T) {
	for _, kind := range []string{"refresh", "close"} {
		t.Run(kind, func(t *testing.T) {
			b := newFileBrowser(t.TempDir())
			cmd := b.refresh()
			old := b.scanGen
			if kind == "refresh" {
				b.refresh()
			} else {
				b.scanCancel()
			}
			msg := cmd()
			if result, ok := msg.(scanFilesMsg); ok && !errors.Is(result.err, context.Canceled) {
				t.Fatalf("%+v", result)
			}
			if kind == "refresh" && b.scanGen == old {
				t.Fatal("generation unchanged")
			}
		})
	}
}

func TestBrowserFilesystemIdentity(t *testing.T) {
	for _, name := range []string{"-leading.md", "中文-e\u0301-👩🏽‍💻.md", "line\n\t\x1b[31m\u202e.md"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, name)
			const raw = "# Raw\r\n\nbody\n"
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			files, err := discoverMarkdown(context.Background(), root)
			if err != nil || len(files) != 1 {
				t.Fatalf("files=%v err=%v", files, err)
			}
			file := files[0].(markdownFile)
			if file.path != path || file.Title() != safeFilename(name) || file.FilterValue() != file.Title() {
				t.Fatal("filesystem/display identities mixed")
			}
			body, err := readMarkdown(context.Background(), file.path)
			if err != nil || string(body) != raw {
				t.Fatalf("raw=%q err=%v", body, err)
			}
		})
	}
}

func TestBrowserCompactPagination(t *testing.T) {
	for _, width := range []int{12, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			b := newFileBrowser("")
			b.list.SetItems(browserItems("", 90))
			b.size(width, 14)
			view := ansi.Strip(b.view(width, 14).Content)
			if width == 12 && !strings.Contains(view, "1/") {
				t.Fatalf("no compact pagination: %q", view)
			}
			if width == 80 && !strings.Contains(view, "••") {
				t.Fatalf("no dot pagination: %q", view)
			}
		})
	}
}

func TestDiscoverMarkdownRootAlias(t *testing.T) {
	for _, kind := range []string{"alias", "broken alias"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			real := filepath.Join(dir, "real")
			if err := os.Mkdir(real, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(real, "a.md"), []byte("# Alias"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, filepath.Join(real, "loop.md")); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(dir, "alias")
			target := real
			if kind == "broken alias" {
				target = filepath.Join(dir, "missing")
			}
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			items, err := discoverMarkdown(context.Background(), alias)
			if kind == "broken alias" {
				if err == nil || !strings.Contains(err.Error(), target) || len(items) != 0 {
					t.Fatalf("broken root not actionable: %v %v", items, err)
				}
				return
			}
			if err != nil || len(items) != 1 {
				t.Fatalf("alias scan: %v %v", items, err)
			}
			file := items[0].(markdownFile)
			if file.path != filepath.Join(alias, "a.md") || file.display != "a.md" {
				t.Fatalf("logical identity changed: %+v", file)
			}
			body, err := readMarkdown(context.Background(), file.path)
			if err != nil || string(body) != "# Alias" {
				t.Fatalf("alias read: %q %v", body, err)
			}
		})
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/lmilojevicc/readmd/internal/pager"
)

func TestBrowserStartupConfiguration(t *testing.T) {
	for _, kind := range []string{"bootstrap", "existing", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			original := "style: notty\npicker: vimium\nimages: false\n"
			if kind == "invalid" {
				original = "picker: unknown\n"
			}
			if kind != "bootstrap" {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var warnings bytes.Buffer
			c, theme, err := startupSettings(cliOpts{}, &warnings)
			if kind == "invalid" {
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			a, err := pager.NewApplication("", "", t.TempDir(), c, theme)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			if !strings.Contains(a.View().Content, "readmd") {
				t.Fatal("no browser")
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "existing" && string(content) != original {
				t.Fatal("config replaced")
			}
			if kind == "bootstrap" {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm()&0077 != 0 {
					t.Fatal("bootstrap not private")
				}
			}
		})
	}
}

func TestCLIFileErrorsPrecedeBootstrap(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "directory"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			path := filepath.Join(t.TempDir(), "doc.md")
			switch kind {
			case "empty":
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			old := os.Args
			t.Cleanup(func() { os.Args = old })
			os.Args = []string{"readmd", path}
			if err := run(); err == nil {
				t.Fatal("invalid input accepted")
			}
			entries, err := os.ReadDir(xdg)
			if err != nil || len(entries) != 0 {
				t.Fatal("input error bootstrapped config")
			}
		})
	}
}

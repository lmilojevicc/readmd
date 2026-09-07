package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"readmd/internal/config"
)

func TestConfigCLIOverridePrecedence(t *testing.T) {
	for _, body := range []string{"{}", "style: light\nmouse: false\npicker: vimium\nreader: true\nreader_width: 73\ntable_cell_width: 21\nimages: false\nremote_images: false"} {
		for _, args := range [][]string{nil, {"--style=dark"}, {"--no-images"}, {"--no-remote-images"}, {"--style", "auto", "--no-images", "--no-remote-images"}} {
			t.Run(body+"/"+strings.Join(args, " "), func(t *testing.T) {
				file, err := config.Parse([]byte(body))
				if err != nil {
					t.Fatal(err)
				}
				opts, err := parseArgs(args)
				if err != nil {
					t.Fatal(err)
				}
				got := applyCLI(file, opts)
				want := file
				if opts.style != "" {
					want.Style = opts.style
				}
				if opts.imgs.NoImages {
					want.Images = false
				}
				if opts.imgs.NoRemote {
					want.RemoteImages = false
				}
				if got != want {
					t.Fatalf("got=%+v want=%+v", got, want)
				}
			})
		}
	}
}

func TestStartupModelUsesConfig(t *testing.T) {
	for _, mouse := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[mouse], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, _ := config.Path()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			body := "style: light\npicker: vimium\nreader: true\nreader_width: 70\ntable_cell_width: 17\nimages: false\nremote_images: false\nmouse: " + map[bool]string{false: "false", true: "true"}[mouse] + "\n"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			m, err := startupModel("# Test\n", "(stdin)", cliOpts{}, &warnings)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			want := tea.MouseModeNone
			if mouse {
				want = tea.MouseModeCellMotion
			}
			if m.View().MouseMode != want || warnings.Len() != 0 {
				t.Fatalf("empty view ignored configured mouse; warnings=%s", &warnings)
			}
			data, _ := os.ReadFile(path)
			if string(data) != body {
				t.Fatal("startup rewrote file")
			}
		})
	}
}

func TestStartupValidatesFileBeforeOverrides(t *testing.T) {
	for _, bad := range []string{"style: invalid", "images: yes", "picker: unknown"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, _ := config.Path()
			os.MkdirAll(filepath.Dir(path), 0700)
			os.WriteFile(path, []byte(bad), 0600)
			opts, err := parseArgs([]string{"--style=auto", "--no-images"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = startupModel("# Test\n", "(stdin)", opts, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("bad config masked by CLI: %v", err)
			}
		})
	}
}

func TestInvalidCLIHasNoConfigSideEffects(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"--style"}, {"--style=invalid"}, {"--style="}, {"one", "two"}, {"--no-images=true"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home, xdg := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", xdg)
			previous := os.Args
			t.Cleanup(func() { os.Args = previous })
			os.Args = append([]string{"readmd"}, args...)
			if err := run(); err == nil {
				t.Fatal("invalid CLI accepted")
			}
			for _, root := range []string{home, xdg} {
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("invalid CLI touched %s: %v %v", root, entries, err)
				}
			}
		})
	}
}

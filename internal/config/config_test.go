package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestParse(t *testing.T) {
	all := Config{Style: "notty", Mouse: false, Picker: "vimium", Reader: true, ReaderWidth: 72, TableCellWidth: 24, Images: false, RemoteImages: false}
	partial := Defaults()
	partial.Mouse = false
	partial.Picker = "vimium"
	for _, tc := range []struct {
		name, body string
		want       Config
	}{
		{"empty", "", Defaults()}, {"comments", "# comment\n", Defaults()}, {"mapping", "{}", Defaults()},
		{"example", Example, Defaults()}, {"partial", "mouse: false\npicker: vimium\n", partial},
		{"all keys", "style: notty\nmouse: false\npicker: vimium\nreader: true\nreader_width: 72\ntable_cell_width: 24\nimages: false\nremote_images: false\n", all},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.body))
			if err != nil || got != tc.want {
				t.Fatalf("got=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct{ body, part string }{
		{"style: neon", "style"}, {"style: true", "style"}, {"picker: 1", "picker"}, {"style: ''", "style"}, {"style:", "style"}, {"mouse:", "mouse"}, {"picker: badge", "picker"}, {"picker: LIST", "picker"},
		{"source: true", "source"}, {"unknown: 1", "unknown"}, {"mouse: true\nmouse: false", "duplicate"},
		{"style: auto\nstyle: auto", "duplicate"}, {"[]", "mapping"}, {"null", "mapping"}, {"---\n", "mapping"},
		{"mouse: [", "YAML"}, {"{}\n---\n{}", "single"}, {"{}\n---", "single"},
		{"mouse: &a true\nreader: *a", "reader"}, {"<<: {mouse: true}", "<<"},
		{"reader_width: 99999999999999999999999999", "reader_width"},
	}
	for _, key := range []string{"style", "picker", "mouse", "reader", "reader_width", "table_cell_width", "images", "remote_images"} {
		for _, value := range []string{"null", "[]", "{}"} {
			cases = append(cases, struct{ body, part string }{key + ": " + value, key})
		}
	}
	for _, key := range []string{"mouse", "reader", "images", "remote_images"} {
		for _, value := range []string{"'false'", "yes", "1", "0.5"} {
			cases = append(cases, struct{ body, part string }{key + ": " + value, key})
		}
	}
	for _, key := range []string{"reader_width", "table_cell_width"} {
		for _, value := range []string{"0", "-1", "10001", "1.5", "'40'", "true"} {
			cases = append(cases, struct{ body, part string }{key + ": " + value, key})
		}
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			_, err := Parse([]byte(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.part) {
				t.Fatalf("error=%v want %q", err, tc.part)
			}
		})
	}
}

func TestParseEnumsAndWidthBounds(t *testing.T) {
	for _, style := range []string{"auto", "dark", "light", "notty"} {
		for _, picker := range []string{"list", "vimium"} {
			for _, width := range []int{1, MaxWidth} {
				t.Run(fmt.Sprintf("%s/%s/%d", style, picker, width), func(t *testing.T) {
					c, err := Parse([]byte(fmt.Sprintf("style: %s\npicker: %s\nreader_width: %d\ntable_cell_width: %d", style, picker, width, width)))
					if err != nil || c.Style != style || c.Picker != picker || c.ReaderWidth != width || c.TableCellWidth != width {
						t.Fatalf("%+v %v", c, err)
					}
				})
			}
		}
	}
}

func TestPath(t *testing.T) {
	for _, mode := range []string{"unset", "empty", "set", "relative", "missing home"} {
		t.Run(mode, func(t *testing.T) {
			home, xdg := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			want := filepath.Join(home, ".config", "readmd", "config.yaml")
			switch mode {
			case "unset":
				if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
					t.Fatal(err)
				}
			case "set":
				t.Setenv("XDG_CONFIG_HOME", xdg)
				want = filepath.Join(xdg, "readmd", "config.yaml")
			case "relative":
				t.Setenv("XDG_CONFIG_HOME", "relative")
			case "missing home":
				t.Setenv("HOME", "")
			}
			got, err := Path()
			if mode == "relative" || mode == "missing home" {
				if err == nil {
					t.Fatal("expected actionable path error")
				}
				return
			}
			if err != nil || got != want {
				t.Fatalf("path=%q err=%v want=%q", got, err, want)
			}
		})
	}
}

func TestLoadBootstrapAndPreserve(t *testing.T) {
	for _, body := range []string{"", "# do not normalize\nmouse: false\npicker: vimium\n", "# existing empty\n"} {
		t.Run(fmt.Sprintf("existing=%t/%d", body != "", len(body)), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", root)
			path, err := Path()
			if err != nil {
				t.Fatal(err)
			}
			if body != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var warnings bytes.Buffer
			c, err := Load(&warnings)
			if err != nil || warnings.Len() != 0 {
				t.Fatalf("%v %s", err, &warnings)
			}
			want := Example
			if body != "" {
				want = body
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatalf("content=%q err=%v", data, err)
			}
			expected, err := Parse([]byte(want))
			if err != nil {
				t.Fatal(err)
			}
			if c != expected {
				t.Fatalf("config=%+v", c)
			}
			for _, file := range []string{path, filepath.Dir(path)} {
				info, err := os.Stat(file)
				if err != nil || info.Mode().Perm()&0077 != 0 {
					t.Fatalf("permissions %s: %v %v", file, info, err)
				}
			}
			_, err = Load(&warnings)
			if err != nil {
				t.Fatal(err)
			}
			data, err = os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatal("second load replaced existing file")
			}
		})
	}
}

func TestLoadErrorsAndWarnings(t *testing.T) {
	for _, mode := range []string{"malformed", "unreadable", "directory", "dangling symlink", "parent is file", "creation denied", "missing home", "relative xdg"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", root)
			path, err := Path()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			warn := false
			switch mode {
			case "malformed":
				if err := os.WriteFile(path, []byte("picker: nope"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses read permissions")
				}
				if err := os.WriteFile(path, []byte("{}"), 0000); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "dangling symlink":
				if err := os.Symlink(filepath.Join(root, "missing"), path); err != nil {
					t.Fatal(err)
				}
			case "parent is file":
				parent := filepath.Dir(path)
				if err := os.Remove(parent); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
				warn = true
			case "creation denied":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses directory permissions")
				}
				dir := filepath.Dir(path)
				if err := os.Chmod(dir, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(dir, 0700); err != nil {
						t.Error(err)
					}
				})
				warn = true
			case "missing home":
				t.Setenv("HOME", "")
				t.Setenv("XDG_CONFIG_HOME", "")
				warn = true
			case "relative xdg":
				t.Setenv("XDG_CONFIG_HOME", "relative")
			}
			var warnings bytes.Buffer
			c, err := Load(&warnings)
			if warn {
				if err != nil || c != Defaults() || !strings.Contains(warnings.String(), "warning") {
					t.Fatalf("config=%+v err=%v warnings=%s", c, err, &warnings)
				}
				return
			}
			if err == nil {
				t.Fatal("existing bad config was ignored")
			}
			if mode != "relative xdg" && !strings.Contains(err.Error(), path) {
				t.Fatalf("error lacks path: %v", err)
			}
		})
	}
}

func TestLoadCreationRace(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "readmd", "config.yaml")
			expected := Defaults()
			if existing {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("mouse: false\n"), 0600); err != nil {
					t.Fatal(err)
				}
				expected.Mouse = false
			}
			const count = 16
			errs := make(chan error, count)
			var wg sync.WaitGroup
			for range count {
				wg.Go(func() {
					var warnings bytes.Buffer
					c, err := loadPath(path, &warnings)
					if err == nil && (!reflect.DeepEqual(c, expected) || warnings.Len() != 0) {
						err = fmt.Errorf("partial/racing read: %+v %s", c, &warnings)
					}
					errs <- err
				})
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 || entries[0].Name() != "config.yaml" {
				t.Fatalf("temporary files leaked: %v %v", entries, err)
			}
			if existing {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "mouse: false\n" {
					t.Fatal("race replaced existing file")
				}
			}
		})
	}
}

func TestExampleFileMatchesBootstrap(t *testing.T) {
	for _, path := range []string{"../../config.example.yaml"} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != Example {
				t.Fatalf("documented example differs from bootstrap: %v", err)
			}
		})
	}
}

func TestExistingEmptyConfigRemainsEmpty(t *testing.T) {
	for _, body := range []string{"", "{}"} {
		t.Run(fmt.Sprintf("length=%d", len(body)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			c, err := loadPath(path, &warnings)
			if err != nil || c != Defaults() || warnings.Len() != 0 {
				t.Fatalf("%+v %v %s", c, err, &warnings)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != body {
				t.Fatal("existing empty config was replaced")
			}
		})
	}
}

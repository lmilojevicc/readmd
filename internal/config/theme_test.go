package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTheme(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"empty", "", true}, {"mapping", "{}", true},
		{"roles", "headings: {h1: {fg: 0, bg: none, bold: false}, h6: {fg: default}}\nstrong: {fg: 255, italic: true}\ninline_code: {bg: '#abcdef'}\ncode_block: {bg: none}\ntable: {border: {fg: 12}}\ntasks: {checked: {glyph: '[x]', fg: 10}}\nfootnotes: {reference: {underline: false}}\ncallouts: {preset: nerd, rail: {glyph: ▋}, title: {bold: false}, note: {icon: i, rail: {fg: 1}}}\n", true},
		{"scalar", "hello", false}, {"null", "null", false}, {"nested null", "strong: null", false},
		{"color null", "strong: {fg: null}", false}, {"bool null", "strong: {bold: null}", false},
		{"unknown root", "palette: {}", false}, {"unknown nested", "headings: {h7: {}}", false},
		{"duplicate root", "strong: {}\nstrong: {}", false}, {"duplicate nested", "strong: {fg: 1, fg: 2}", false},
		{"wrong role", "strong: true", false}, {"wrong bool", "strong: {bold: 'false'}", false},
		{"low color", "strong: {fg: -1}", false}, {"high color", "strong: {fg: 256}", false},
		{"short hex", "strong: {fg: '#fff'}", false}, {"bad hex", "strong: {fg: '#ffx000'}", false},
		{"unquoted hex", "strong: {fg: #abcdef\n}", false}, {"wrong clear fg", "strong: {fg: none}", false},
		{"wrong clear bg", "strong: {bg: default}", false}, {"named color", "strong: {fg: red}", false},
		{"syntax foreground unsupported", "code_block: {fg: 1}", false},
		{"task scope", "tasks: {checked: {body: {fg: 1}}}", false},
		{"escape glyph", "tasks: {checked: {glyph: \"\\e[31mX\"}}", false},
		{"newline glyph", "callouts: {note: {icon: \"x\\ny\"}}", false},
		{"invisible glyph", "callouts: {note: {icon: ' '}}", false},
		{"wide glyph", "tasks: {checked: {glyph: '123456789'}}", false},
		{"safe grapheme", "tasks: {checked: {glyph: '👩‍💻'}}", true},
		{"preset", "callouts: {preset: automatic}", false},
		{"multiple documents", "{}\n---\n{}", false},
		{"alias", "strong: &x {fg: 1}\nemphasis: *x", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTheme([]byte(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestLoadThemeSelection(t *testing.T) {
	for _, tc := range []struct {
		name, configPath, cliPath string
		yaml, yml, explicit, cli  string
		want, errorText           string
	}{
		{name: "missing"},
		{name: "yaml first", yaml: "strong: {fg: 1}", yml: "broken: true", want: "1"},
		{name: "yml fallback", yml: "strong: {fg: 2}", want: "2"},
		{name: "config relative", configPath: "local.yaml", yaml: "broken: true", explicit: "strong: {fg: 3}", want: "3"},
		{name: "CLI selected only", configPath: "missing.yaml", cliPath: "cli.yaml", yaml: "broken: true", cli: "strong: {fg: 4}", want: "4"},
		{name: "selected malformed", yaml: "unknown: true", errorText: "theme.yaml"},
		{name: "missing explicit", configPath: "missing.yaml", errorText: "missing.yaml"},
		{name: "missing CLI", cliPath: "missing.yaml", errorText: "missing.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
			cfg, _ := Path()
			dir := filepath.Dir(cfg)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"theme.yaml": tc.yaml, "theme.yml": tc.yml, "local.yaml": tc.explicit, "cli.yaml": tc.cli} {
				if data != "" {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			cwd := t.TempDir()
			if tc.cli != "" {
				if err := os.WriteFile(filepath.Join(cwd, "cli.yaml"), []byte(tc.cli), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(cwd)
			theme, err := LoadTheme(tc.configPath, tc.cliPath)
			if tc.errorText != "" {
				errorDir := dir
				if tc.cliPath != "" {
					errorDir = cwd
				}
				if err == nil || !strings.Contains(err.Error(), filepath.Join(errorDir, tc.errorText)) {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if theme.Strong.FG != nil {
				got = string(*theme.Strong.FG)
			}
			if got != tc.want {
				t.Fatalf("fg=%q want=%q", got, tc.want)
			}
			if _, err := os.Stat(cfg); !os.IsNotExist(err) {
				t.Fatal("theme loading created config")
			}
		})
	}
}

func TestThemeClone(t *testing.T) {
	for _, data := range []string{"strong: {fg: 1, bold: false}", "callouts: {note: {icon: i, rail: {fg: 2}}}"} {
		t.Run(data, func(t *testing.T) {
			theme, err := ParseTheme([]byte(data))
			if err != nil {
				t.Fatal(err)
			}
			copy := theme.Clone()
			if theme.Strong.FG != nil {
				*theme.Strong.FG = "9"
				if *copy.Strong.FG != "1" {
					t.Fatal("shared pointer")
				}
			}
			if theme.Callouts.Note.Icon != nil {
				*theme.Callouts.Note.Icon = "x"
				if *copy.Callouts.Note.Icon != "i" {
					t.Fatal("shared icon")
				}
			}
		})
	}
}

func TestThemeSelectedFileErrors(t *testing.T) {
	for _, kind := range []string{"directory", "dangling symlink", "absolute"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, _ := Path()
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(dir, "theme.yaml")
			if err := os.WriteFile(filepath.Join(dir, "theme.yml"), []byte("strong: {fg: 1}"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "directory":
				if err := os.Mkdir(selected, 0700); err != nil {
					t.Fatal(err)
				}
			case "dangling symlink":
				if err := os.Symlink("missing", selected); err != nil {
					t.Fatal(err)
				}
			case "absolute":
				selected = filepath.Join(t.TempDir(), "selected.yaml")
				if err := os.WriteFile(selected, []byte("strong: {fg: 7}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "absolute" {
				theme, err := LoadTheme(selected, "")
				if err != nil || theme.Strong.FG == nil || *theme.Strong.FG != "7" {
					t.Fatalf("absolute path: %+v %v", theme, err)
				}
				return
			}
			if _, err := LoadTheme("", ""); err == nil || !strings.Contains(err.Error(), selected) {
				t.Fatalf("selected file did not error: %v", err)
			}
		})
	}
}

func TestThemeExample(t *testing.T) {
	for _, path := range []string{"../../theme.example.yaml"} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseTheme(data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestThemeDiscoveryPreservesBootstrapWarnings(t *testing.T) {
	for _, obstruction := range []string{"root", "readmd"} {
		t.Run(obstruction, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root := filepath.Join(t.TempDir(), "config")
			blocked := root
			if obstruction == "readmd" {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				blocked = filepath.Join(root, "readmd")
			}
			if err := os.WriteFile(blocked, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_CONFIG_HOME", root)
			var warnings strings.Builder
			c, err := Load(&warnings)
			if err != nil || warnings.Len() == 0 {
				t.Fatalf("bootstrap should warn: %v %s", err, warnings.String())
			}
			if _, err := LoadTheme(c.Theme, ""); err != nil {
				t.Fatalf("absent discovery changed warning to error: %v", err)
			}
			data, err := os.ReadFile(blocked)
			if err != nil || string(data) != "preserve" {
				t.Fatal("obstruction overwritten")
			}
		})
	}
}

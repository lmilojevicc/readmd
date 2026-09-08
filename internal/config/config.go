// Package config owns startup-only settings. Runtime toggles are never persisted.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"
)

// MaxWidth bounds per-line padding/wrapping allocations and leaves arithmetic
// headroom even on 32-bit platforms. It is a preference, not a content clamp.
const MaxWidth = 10000

const Example = `# readmd startup settings (runtime m/r changes are not saved).
# Path: $XDG_CONFIG_HOME/readmd/config.yaml, otherwise
# $HOME/.config/readmd/config.yaml (including macOS).
# Defaults < this file < explicit CLI flags; theme overrides the selected style.
style: auto # auto follows the terminal palette; dark, light, notty are fixed styles
# theme: theme.yaml # optional; relative to this directory; --theme PATH wins
mouse: true # m toggles capture for this session only
picker: list # link picker only: p opens list (focus/details) or vimium (badges)
reader: false # r toggles the centered viewport for this session only
reader_width: 120 # 1..10000 columns; viewport max(1, min(preference, terminal-2))
table_cell_width: 40 # 1..10000 preferred body-cell wrap; full headers/atoms may exceed it
images: true # only uses graphics when supported by the terminal
remote_images: true # existing remote-image security/resource limits still apply
`

type Config struct {
	Style          string `yaml:"style"`
	Theme          string `yaml:"theme"`
	Mouse          bool   `yaml:"mouse"`
	Picker         string `yaml:"picker"`
	Reader         bool   `yaml:"reader"`
	ReaderWidth    int    `yaml:"reader_width"`
	TableCellWidth int    `yaml:"table_cell_width"`
	Images         bool   `yaml:"images"`
	RemoteImages   bool   `yaml:"remote_images"`
}

func Defaults() Config {
	return Config{Style: "auto", Mouse: true, Picker: "list", ReaderWidth: 120, TableCellWidth: 40, Images: true, RemoteImages: true}
}

func (c Config) Validate() error {
	switch c.Style {
	case "auto", "dark", "light", "notty":
	default:
		return fmt.Errorf("style: want auto, dark, light or notty; got %q", c.Style)
	}
	switch c.Picker {
	case "list", "vimium":
	default:
		return fmt.Errorf("picker: want list or vimium; got %q", c.Picker)
	}
	for _, field := range []struct {
		name  string
		value int
	}{{"reader_width", c.ReaderWidth}, {"table_cell_width", c.TableCellWidth}} {
		if field.value < 1 || field.value > MaxWidth {
			return fmt.Errorf("%s: want an integer in 1..%d; got %d", field.name, MaxWidth, field.value)
		}
	}
	return nil
}

func Parse(data []byte) (Config, error) {
	c := Defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if err == io.EOF {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return c, fmt.Errorf("YAML: %w", err)
		}
		return c, errors.New("YAML: expected a single mapping document")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return c, errors.New("YAML: expected a mapping of settings")
	}
	mapping := doc.Content[0]
	types := map[string]string{"style": "!!str", "theme": "!!str", "mouse": "!!bool", "picker": "!!str", "reader": "!!bool", "reader_width": "!!int", "table_cell_width": "!!int", "images": "!!bool", "remote_images": "!!bool"}
	seen := map[string]bool{}
	for i := 0; i < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		tag, ok := types[key.Value]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || !ok {
			return c, fmt.Errorf("line %d: unknown setting %q", key.Line, key.Value)
		}
		if seen[key.Value] {
			return c, fmt.Errorf("line %d: duplicate setting %q", key.Line, key.Value)
		}
		seen[key.Value] = true
		if key.Value == "theme" && strings.TrimSpace(value.Value) == "" {
			return c, fmt.Errorf("line %d: theme path must not be empty", value.Line)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != tag {
			return c, fmt.Errorf("line %d: %s must have type %s", value.Line, key.Value, tag)
		}
	}
	if err := mapping.Decode(&c); err != nil {
		return c, fmt.Errorf("settings: %w", err)
	}
	return c, c.Validate()
}

func Path() (string, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root != "" {
		if !filepath.IsAbs(root) {
			return "", fmt.Errorf("XDG_CONFIG_HOME must be absolute: %q", root)
		}
	} else {
		home := os.Getenv("HOME")
		if home == "" {
			return "", errors.New("HOME is unset; cannot locate readmd/config.yaml")
		}
		if !filepath.IsAbs(home) {
			return "", fmt.Errorf("HOME must be absolute: %q", home)
		}
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "readmd", "config.yaml"), nil
}

// Load publishes a fully written private temporary file with a create-only
// hard link. A racing creator wins without ever being truncated or replaced.
func Load(warnings io.Writer) (Config, error) {
	path, err := Path()
	if err != nil {
		if os.Getenv("XDG_CONFIG_HOME") != "" {
			return Config{}, err
		}
		_, _ = fmt.Fprintln(warnings, "readmd: warning:", err) // Best-effort diagnostic; defaults remain usable.
		return Defaults(), nil
	}
	return loadPath(path, warnings)
}

func readExisting(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	c, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

func loadPath(path string, warnings io.Writer) (Config, error) {
	if _, err := os.Lstat(path); err == nil || !missingPath(err) {
		return readExisting(path)
	}
	err := createDefault(path)
	// Re-read even after a creation failure: a concurrent creator may have won.
	if _, statErr := os.Lstat(path); statErr == nil || !missingPath(statErr) {
		return readExisting(path)
	}
	if err != nil {
		_, _ = fmt.Fprintf(warnings, "readmd: warning: cannot create config %s: %v; using defaults\n", path, err) // Best-effort diagnostic.
	}
	return Defaults(), nil
}

func createDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // Best-effort temporary-file cleanup.
	if _, err = f.WriteString(Example); err != nil {
		_ = f.Close() // Preserve the write/sync error.
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close() // Preserve the write/sync error.
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}

func missingPath(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

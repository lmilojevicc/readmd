package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"go.yaml.in/yaml/v3"
)

// Theme is a startup-only partial override. Nil fields inherit the selected base.
type Theme struct {
	Headings      struct{ H1, H2, H3, H4, H5, H6 TextStyle } `yaml:"headings"`
	Strong        TextStyle                                  `yaml:"strong"`
	Emphasis      TextStyle                                  `yaml:"emphasis"`
	Strikethrough TextStyle                                  `yaml:"strikethrough"`
	Links         TextStyle                                  `yaml:"links"`
	InlineCode    TextStyle                                  `yaml:"inline_code"`
	CodeBlock     struct {
		BG *Color `yaml:"bg"`
	} `yaml:"code_block"`
	Table struct {
		Border TextStyle `yaml:"border"`
	} `yaml:"table"`
	Tasks     struct{ Checked, Unchecked GlyphStyle }   `yaml:"tasks"`
	Footnotes struct{ Reference, Definition TextStyle } `yaml:"footnotes"`
	Callouts  Callouts                                  `yaml:"callouts"`
}

type Color string

type TextStyle struct {
	FG            *Color `yaml:"fg"`
	BG            *Color `yaml:"bg"`
	Bold          *bool  `yaml:"bold"`
	Italic        *bool  `yaml:"italic"`
	Underline     *bool  `yaml:"underline"`
	Strikethrough *bool  `yaml:"strikethrough"`
}

type GlyphStyle struct {
	TextStyle `yaml:",inline"`
	Glyph     *string `yaml:"glyph"`
}

type Callout struct {
	Icon  *string    `yaml:"icon"`
	Rail  GlyphStyle `yaml:"rail"`
	Title TextStyle  `yaml:"title"`
}

type Callouts struct {
	Preset                                 *string    `yaml:"preset"`
	Rail                                   GlyphStyle `yaml:"rail"`
	Title                                  TextStyle  `yaml:"title"`
	Note, Tip, Important, Warning, Caution Callout
}

func ParseTheme(data []byte) (Theme, error) {
	var theme Theme
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc, extra yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			return theme, nil
		}
		return theme, err
	}
	if err := dec.Decode(&extra); err != io.EOF {
		return theme, errors.New("theme must contain a single mapping document")
	}
	if len(doc.Content) != 1 {
		return theme, errors.New("theme must be a mapping")
	}
	if err := validateThemeNode(doc.Content[0], reflect.TypeFor[Theme](), "theme"); err != nil {
		return theme, err
	}
	if err := doc.Content[0].Decode(&theme); err != nil {
		return theme, err
	}
	return theme, nil
}

// Validate the YAML tree before decoding: KnownFields alone accepts nulls and
// coercions. The schema comes directly from the concrete structs above.
func validateThemeNode(n *yaml.Node, typ reflect.Type, path string) error {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	fail := func(want string) error { return fmt.Errorf("line %d: %s: %s", n.Line, path, want) }
	if typ == reflect.TypeFor[Color]() {
		if n.Kind != yaml.ScalarNode || (n.Tag != "!!str" && n.Tag != "!!int") {
			return fail("want ANSI 0..255, quoted #RRGGBB, fg default or bg none")
		}
		if n.Tag == "!!int" {
			i, e := strconv.Atoi(n.Value)
			if e == nil && i >= 0 && i <= 255 && n.Value == strconv.Itoa(i) {
				return nil
			}
		}
		if n.Tag == "!!str" {
			if strings.HasSuffix(path, ".fg") && n.Value == "default" || strings.HasSuffix(path, ".bg") && n.Value == "none" {
				return nil
			}
			if len(n.Value) == 7 && n.Value[0] == '#' {
				if _, e := strconv.ParseUint(n.Value[1:], 16, 24); e == nil {
					return nil
				}
			}
		}
		return fail("want ANSI integer 0..255, quoted #RRGGBB, fg default or bg none")
	}
	switch typ.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return fail("want a mapping (null is not an override)")
		}
		fields := themeFields(typ)
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			field, ok := fields[key.Value]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || !ok {
				return fmt.Errorf("line %d: %s: unknown key %q", key.Line, path, key.Value)
			}
			if seen[key.Value] {
				return fmt.Errorf("line %d: %s: duplicate key %q", key.Line, path, key.Value)
			}
			seen[key.Value] = true
			if err := validateThemeNode(value, field, path+"."+key.Value); err != nil {
				return err
			}
		}
	case reflect.Bool:
		if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" {
			return fail("want true or false")
		}
	case reflect.String:
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
			return fail("want a string")
		}
		if strings.HasSuffix(path, ".preset") {
			if n.Value != "unicode" && n.Value != "nerd" {
				return fail("want unicode or nerd")
			}
			return nil
		}
		maxWidth := 4
		if strings.Contains(path, ".tasks.") {
			maxWidth = 8
		}
		if len(n.Value) > 128 || strings.TrimSpace(n.Value) == "" || ansi.StringWidth(n.Value) < 1 || ansi.StringWidth(n.Value) > maxWidth {
			return fail(fmt.Sprintf("glyph must occupy 1..%d columns and at most 128 bytes", maxWidth))
		}
		for _, r := range n.Value {
			if unicode.IsControl(r) || (!unicode.IsPrint(r) && !unicode.In(r, unicode.Co) && r != '\u200c' && r != '\u200d') {
				return fail("glyph must not contain control characters")
			}
		}
	default:
		return fail("unsupported theme field")
	}
	return nil
}

func themeFields(typ reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == ",inline" {
			for name, t := range themeFields(f.Type) {
				fields[name] = t
			}
			continue
		}
		if tag == "" {
			tag = strings.ToLower(f.Name)
		}
		fields[tag] = f.Type
	}
	return fields
}

func (c *Color) UnmarshalYAML(n *yaml.Node) error {
	// ParseTheme has already rejected invalid scalar types and channel keywords.
	*c = Color(n.Value)
	return nil
}

// LoadTheme reads only the selected file. Discovery never creates a theme.
func LoadTheme(configTheme, cliTheme string) (Theme, error) {
	path := cliTheme
	if path == "" {
		configPath, err := Path()
		if err != nil {
			if configTheme == "" {
				return Theme{}, nil
			}
			return Theme{}, err
		}
		dir := filepath.Dir(configPath)
		if configTheme != "" {
			path = configTheme
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
		} else {
			for _, name := range []string{"theme.yaml", "theme.yml"} {
				candidate := filepath.Join(dir, name)
				if _, err := os.Lstat(candidate); err == nil || !missingPath(err) {
					path = candidate
					break
				}
			}
		}
	}
	if path == "" {
		return Theme{}, nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Theme{}, fmt.Errorf("theme %s: %w", path, err)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Theme{}, fmt.Errorf("theme %s: %w", absolute, err)
	}
	theme, err := ParseTheme(data)
	if err != nil {
		return Theme{}, fmt.Errorf("theme %s: %w", absolute, err)
	}
	return theme, nil
}

// Clone makes the render snapshot independent of caller-owned pointer fields.
func (t Theme) Clone() Theme {
	var clone func(reflect.Value) reflect.Value
	clone = func(v reflect.Value) reflect.Value {
		result := reflect.New(v.Type()).Elem()
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				result.Set(reflect.New(v.Type().Elem()))
				result.Elem().Set(clone(v.Elem()))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				result.Field(i).Set(clone(v.Field(i)))
			}
		default:
			result.Set(v)
		}
		return result
	}
	return clone(reflect.ValueOf(t)).Interface().(Theme)
}

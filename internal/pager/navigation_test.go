package pager

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestNavigationLocalClassification(t *testing.T) {
	for _, tc := range []struct {
		dest, path, fragment string
		local, valid         bool
	}{
		{"#", "", "", true, true}, {"#Hello%20世界", "", "Hello 世界", true, true},
		{"../space%20%23%3F.MARKDOWN#x%2520", "../space #?.MARKDOWN", "x%20", true, true},
		{"100%25.md", "100%.md", "", true, true}, {"%2523.md", "%23.md", "", true, true},
		{"http://example.org/a#b", "", "", false, true}, {"https://example.org", "", "", false, true},
		{"mailto:a@example.org", "", "", false, true},
		{"file:doc.md", "", "", false, true}, {"javascript:x", "", "", false, true},
		{"//example.org/doc.md", "", "", true, false}, {"/doc.md", "", "", true, false},
		{"%2Fdoc.md", "", "", true, false}, {"doc.md?x", "", "", true, false},
		{"doc.md?", "", "", true, false}, {"doc.txt", "", "", true, false},
		{"doc%00.md", "", "", true, false}, {"doc.md#%0A", "", "", true, false},
		{"doc.md#%zz", "", "", true, false}, {"doc%.md", "", "", true, false},
		{"#^block", "", "", true, false}, {"", "", "", true, false},
		{"\\doc.md", "", "", true, false}, {"#%FF", "", "", true, false},
	} {
		t.Run(tc.dest, func(t *testing.T) {
			got, local, err := classifyLocalLink(tc.dest)
			if local != tc.local || (err == nil) != tc.valid || got.path != tc.path || got.fragment != tc.fragment {
				t.Fatalf("got %+v local=%v err=%v", got, local, err)
			}
		})
	}
}

func TestNavigationHeadingSlugs(t *testing.T) {
	for _, tc := range []struct {
		source        string
		titles, slugs []string
	}{
		{"# Hello, World!\n# Hello, World!\n# Hello World-1\n# Hello, World!\n", []string{"Hello, World!", "Hello, World!", "Hello World-1", "Hello, World!"}, []string{"hello-world", "hello-world-1", "hello-world-1-1", "hello-world-2"}},
		{"# [Label](https://example.org) `Code` &amp; More\n# 日本語 Café\n# C++ _test_\\!\n", []string{"Label Code & More", "日本語 Café", "C++ test!"}, []string{"label-code--more", "日本語-café", "c-test"}},
		{"# <https://example.org>\n# hello_world\n", []string{"https://example.org", "hello_world"}, []string{"httpsexampleorg", "hello_world"}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			heads := extractHeadings(tc.source)
			var titles, slugs []string
			for _, h := range heads {
				titles = append(titles, h.text)
				slugs = append(slugs, h.slug)
			}
			if !reflect.DeepEqual(titles, tc.titles) || !reflect.DeepEqual(slugs, tc.slugs) {
				t.Fatalf("titles=%q slugs=%q", titles, slugs)
			}
		})
	}
}

func TestNavigationStandaloneAnchors(t *testing.T) {
	for _, tc := range []struct {
		dest    string
		index   int
		missing bool
	}{
		{"#", -1, false}, {"#second-heading", 1, false}, {"#Second%20Heading", 1, false},
		{"#世界", 2, false}, {"#second-heading-1", 3, false}, {"#missing", 0, true},
	} {
		t.Run(tc.dest, func(t *testing.T) {
			source := "# First\n\n" + strings.Repeat("paragraph\n\n", 20) + "# Second Heading\n\n" + strings.Repeat("paragraph\n\n", 20) + "# 世界\n\n# Second Heading\n\n" + strings.Repeat("tail\n\n", 20)
			m := New(source, "test")
			if err := m.SetStyle("notty"); err != nil {
				t.Fatal(err)
			}
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
			m.Update(cmd())
			m.vp.SetYOffset(5)
			before := m.currentLocation()
			m.openURL = func(string) error { t.Fatal("local target reached external opener"); return nil }
			m.activateTarget(hintTarget{linkTarget: linkTarget{dest: tc.dest}})
			if tc.missing {
				if m.currentLocation() != before || len(m.locations) != 0 || !strings.Contains(m.flash, "not found") {
					t.Fatal("missing anchor moved or recorded history")
				}
				return
			}
			line := 0
			if tc.index >= 0 {
				line = m.heads[tc.index].line
			}
			if m.vp.YOffset() != line || m.vp.XOffset() != 0 || len(m.locations) != 1 {
				t.Fatalf("location=%+v expected y=%d", m.currentLocation(), line)
			}
			if tc.index >= 0 {
				_, cmd = m.Update(tea.WindowSizeMsg{Width: 23, Height: 8})
				m.Update(cmd())
				if m.vp.YOffset() != m.heads[tc.index].line {
					t.Fatal("resize lost heading")
				}
			}
			if cmd := m.backLocation(); cmd != nil {
				m.Update(cmd())
			}
			if m.currentLocation() != before {
				t.Fatalf("back=%+v want %+v", m.currentLocation(), before)
			}
			if m.source != source {
				t.Fatal("source changed")
			}
		})
	}
}

func TestNavigationRenderThemeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		custom     bool
	}{
		{"pointer", "callouts:\n  note:\n    title: {fg: 13}\n", false},
		{"custom map", "callouts:\n  custom:\n    note: {title: {fg: 13}}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme, err := config.ParseTheme([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			m := New("> [!NOTE]\n> Body\n", "snapshot")
			if err := m.SetStyle("dark"); err != nil {
				t.Fatal(err)
			}
			m.SetTheme(theme)
			m.width = 40
			reference := m.requestRender()().(renderedMsg)
			m.rendering = false
			cmd := m.requestRender()
			if tc.custom {
				*m.theme.Callouts.Custom["note"].Title.FG = config.Color("10")
				delete(m.theme.Callouts.Custom, "note")
			} else {
				*m.theme.Callouts.Note.Title.FG = config.Color("10")
			}
			actual := cmd().(renderedMsg)
			if actual.err != nil || reference.err != nil || actual.content != reference.content {
				t.Fatal("delayed command observed later theme mutation")
			}
		})
	}
}

func TestNavigationExternalTargets(t *testing.T) {
	for _, dest := range []string{"https://example.org/a%20b?q=1#part", "http://example.org", "mailto:a@example.org", "file:doc.md", "javascript:alert(1)", "#missing", "relative.md"} {
		t.Run(dest, func(t *testing.T) {
			m := New("", "external")
			called := ""
			m.openURL = func(target string) error { called = target; return nil }
			cmd := m.activateTarget(hintTarget{linkTarget: linkTarget{dest: dest}})
			if err := validateExternalURL(dest); err == nil {
				if cmd == nil {
					t.Fatal("external opener missing")
				}
				msg := cmd().(openedURLMsg)
				if called != dest || msg.dest != dest {
					t.Fatal("external target changed")
				}
			} else if cmd != nil || called != "" {
				t.Fatal("unsupported/local link reached opener")
			}
		})
	}
}

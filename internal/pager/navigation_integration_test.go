package pager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestNavigationRenderedTargetActivation(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, container := range []struct{ name, format string }{
		{"paragraph", "%s\n"},
		{"nested task list", "- parent prose\n  - [ ] %s\n"},
		{"numbered list", "10. %s\n"},
		{"callout body", "> [!warning]- Always visible\n> %s\n"},
		{"callout title", "> [!tip] %s\n> Body remains visible.\n"},
		{"list callout", "- parent prose\n\n  > [!note]\n  > %s\n"},
		{"nested callout", "> [!note]\n> Outer body.\n>\n> > [!success]\n> > %s\n"},
		{"table", "| Link |\n| - |\n| %s |\n"},
		{"nested table", "- parent prose\n\n  | Link |\n  | - |\n  | %s |\n"},
	} {
		for _, destination := range []struct {
			name, dest, fragment string
			file, missing        bool
		}{
			{"slug", "#target-世界", "target-世界", false, false},
			{"title", "#Target%20世界", "Target 世界", false, false},
			{"top", "#", "", false, false},
			{"relative heading", "sample.md#table", "table", true, false},
			{"relative top", "sample.md#", "", true, false},
			{"missing fragment", "sample.md#absent", "absent", true, true},
		} {
			for _, interaction := range []string{"list hint", "vimium hint", "mouse first", "mouse last"} {
				for _, reader := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/reader=%v", container.name, destination.name, interaction, reader), func(t *testing.T) {
						root := t.TempDir()
						navigationWrite(t, filepath.Join(root, "sample.md"), string(fixture))
						dest := destination.dest
						label := "linked 日本語 👨‍👩‍👧‍👦 recovery checklist with many ordinary words and final destination"
						link := "[" + label + "](" + dest + ")"
						source := "# Original\n\n" + strings.Repeat("before\n\n", 8) + fmt.Sprintf(container.format, link) + "\n" + strings.Repeat("between\n\n", 20) + "# Target 世界\n\n" + strings.Repeat("tail\n\n", 20)
						path := filepath.Join(root, "original.md")
						navigationWrite(t, path, source)
						c := config.Defaults()
						c.Style, c.Images, c.Reader, c.ReaderWidth = "auto", false, reader, 40
						if interaction == "vimium hint" {
							c.Picker = "vimium"
						}
						a, err := NewApplication(source, path, root, c, config.Theme{})
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(a.Close)
						width := 40
						if reader {
							width = 60
						}
						_, cmd := a.Update(tea.WindowSizeMsg{Width: width, Height: 12})
						appFiniteCommands(t, a, cmd)
						old := a.current
						old.openURL = func(string) error { t.Fatal("local target reached external opener"); return nil }
						if len(old.links) != 1 || old.links[0].dest != dest {
							t.Fatalf("rendered targets=%+v want one %q", old.links, dest)
						}
						target := old.links[0]
						if container.name != "table" && container.name != "nested table" && len(target.regions) < 2 {
							t.Fatal("fixture did not exercise a multiline wrapped target")
						}
						reg := target.regions[0]
						if interaction == "mouse last" {
							reg = target.regions[len(target.regions)-1]
						}
						old.vp.SetYOffset(reg.line)
						if interaction == "mouse first" || interaction == "mouse last" {
							old.vp.SetXOffset(max(0, reg.start-old.vp.Width()+2))
						}
						before := old.currentLocation()
						if strings.HasSuffix(interaction, "hint") {
							appKey(a, "p")
							if !old.targets.active || len(old.targets.targets) != 1 || old.targets.targets[0].dest != dest {
								t.Fatalf("real rendered hint unavailable: %+v notice=%q", old.targets, old.flash)
							}
							for _, key := range strings.ToLower(old.targets.targets[0].label) {
								cmd = appKey(a, string(key))
							}
						} else {
							rows, safe := old.targetViewportRows()
							y := reg.line - old.vp.YOffset()
							if !safe || y < 0 || y >= len(rows) {
								t.Fatal("rendered region has unsafe screen coordinates")
							}
							margin := 0
							if reader {
								_, margin = old.readerGeom(true)
							}
							x := max(reg.start, rows[y].left) - rows[y].left + margin
							view := strings.Split(old.View().Content, "\n")
							if ansi.Strip(ansi.Cut(view[y], x, x+1)) == " " {
								t.Fatal("click coordinate points at padding rather than rendered label")
							}
							_, cmd = a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						}
						if cmd == nil {
							t.Fatal("real target interaction did not request navigation")
						}
						navigationCommands(t, a, cmd)
						if old.source != source {
							t.Fatal("render/navigation changed raw source")
						}
						if destination.missing {
							if a.current != old || old.currentLocation() != before || len(a.history) != 0 || !strings.Contains(old.flash, "heading not found") {
								t.Fatal("rendered missing-fragment target failed rollback")
							}
							return
						}
						line, ok := headingLine(a.current.heads, destination.fragment)
						if !ok || a.current.vp.YOffset() != line || a.current.vp.XOffset() != 0 || len(a.history) != 1 || (a.current != old) != destination.file {
							t.Fatalf("activation location=%+v want y=%d history=%d", a.current.currentLocation(), line, len(a.history))
						}
						navigationCommands(t, a, appKey(a, "backspace"))
						if a.current.path != path || a.current.source != source || a.current.currentLocation() != before || len(a.history) != 0 {
							t.Fatalf("Backspace did not restore rendered-link origin: %+v want %+v", a.current.currentLocation(), before)
						}
					})
				}
			}
		}
	}
}

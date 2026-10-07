package pager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lmilojevicc/readmd/internal/config"
)

func TestHeadingMathFallbackLiveRender(t *testing.T) {
	for _, math := range []string{"$# \\alpha$", "$\\quad$ # text"} {
		for _, genuine := range []bool{false, true} {
			for _, width := range []int{20, 40, 80} {
				t.Run(fmt.Sprintf("%s/genuine=%t/width=%d", math, genuine, width), func(t *testing.T) {
					source := math + "\n"
					if genuine {
						source = "[Before](#before)\n\n[After](#after)\n\nBefore\n\nAfter\n\n# Before\n\n" + math + "\n\n# After\n\n" + strings.Repeat("tail\n\n", 20)
					}
					m := New(source, "math fallback")
					if err := m.SetStyle("notty"); err != nil {
						t.Fatal(err)
					}
					_, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 8})
					result := cmd().(renderedMsg)
					if result.err != nil {
						t.Fatalf("live rendering failed: %v", result.err)
					}
					m.Update(result)
					if m.source != source {
						t.Fatal("raw source changed")
					}
					if !strings.Contains(strings.Join(m.stripped, "\n"), math) {
						t.Fatalf("literal math content lost: %q", m.stripped)
					}
					if strings.Contains(result.content, "readmd-heading-") || strings.Contains(result.content, "\x1b]778;") {
						t.Fatal("provenance marker leaked")
					}
					plain, err := Render(source, width)
					if err != nil || plain != result.content {
						t.Fatalf("ordinary/live output differs: err=%v\nplain=%q\nlive=%q", err, plain, result.content)
					}
					if !genuine {
						if len(m.heads) != 0 {
							t.Fatalf("fabricated source headings: %+v", m.heads)
						}
						return
					}
					if len(m.heads) != 2 || m.heads[0].text != "Before" || m.heads[1].text != "After" {
						t.Fatalf("lost/fabricated source headings: %+v", m.heads)
					}
					for _, title := range []string{"Before", "After"} {
						rows := reviewHeadingRows(t, m, "# "+title)
						if len(rows) != 1 {
							t.Fatalf("expected one actual heading %q, rows=%v", title, rows)
						}
						m.activateTarget(reviewRenderedTarget(t, m, "#"+strings.ToLower(title)))
						if m.vp.YOffset() != rows[0] {
							t.Fatalf("jump to %q y=%d actual heading row=%d", title, m.vp.YOffset(), rows[0])
						}
					}
				})
			}
		}
	}
}

func TestHeadingMathNormalRenderUnchanged(t *testing.T) {
	for _, source := range []string{
		"[Before](#before)\n\n# Before\n\n$\\alpha \\leq \\beta$\n\n# After\n\n",
		"# Math $x^2$\n\n$$\\alpha + \\beta$$\n\n> [!NOTE]\n> ## Nested\n> body $x^2$\n\n",
	} {
		for _, style := range []string{paletteStyleName, "dark", "light", "notty"} {
			t.Run(style+source, func(t *testing.T) {
				// With unchanged heading structure, the existing substitution is still used.
				normal, _, _, err := renderThemedDoc(imgCtx{}, source, 40, style, config.Theme{})
				if err != nil {
					t.Fatal(err)
				}
				substituted, _, _, err := renderThemedDoc(imgCtx{}, substituteMath(source), 40, style, config.Theme{})
				if err != nil {
					t.Fatal(err)
				}
				if normal != substituted {
					t.Fatal("normal math substitution output changed")
				}
				live, _, _, heads, err := renderNavigationDoc(imgCtx{}, source, 40, style, config.Theme{})
				if err != nil {
					t.Fatal(err)
				}
				if live != normal {
					t.Fatal("normal metadata-enabled output changed")
				}
				normalTargets := renderedTargets(source, strings.Split(normal, "\n"), splitStrip(normal))
				liveTargets := renderedTargets(source, strings.Split(live, "\n"), splitStrip(live))
				if !reflect.DeepEqual(normalTargets, liveTargets) {
					t.Fatal("normal OSC8 regions changed")
				}
				if len(heads) != 2 {
					t.Fatalf("normal source headings lost: %+v", heads)
				}
			})
		}
	}
}

# Third-party license inventory

Scope: the **31 direct and indirect modules explicitly pinned in go.mod** for
readmd, including platform-specific dependencies. Classifications below come
from the license files in those exact downloaded module versions, inspected
read-only. Paths in the last column are relative to each module root; obtain
those roots with `GOWORK=off go list -m -json all` after
`GOWORK=off go mod download`.

This is a bounded inventory, not a legal audit or a full file-level provenance
review. It excludes dependency-only test/tool modules in the wider module graph,
the Go toolchain/standard library, and CI/development tools. No incompatible
terms were identified in the listed license files; this does not certify all
embedded upstream data/code or remove attribution obligations.

| Module | Pinned version | License identified | Evidence file(s) |
| --- | --- | --- | --- |
| `charm.land/bubbles/v2` | `v2.2.1` | MIT | `LICENSE` |
| `charm.land/bubbletea/v2` | `v2.0.9` | MIT | `LICENSE` |
| `charm.land/glamour/v2` | `v2.0.1` | MIT | `LICENSE` |
| `charm.land/lipgloss/v2` | `v2.0.6` | MIT | `LICENSE` |
| `github.com/alecthomas/chroma/v2` | `v2.27.0` | MIT AND OFL-1.1 (font-specific) | `COPYING`, `formatters/svg/font_liberation_mono.go` |
| `github.com/aymerick/douceur` | `v0.2.0` | MIT | `LICENSE` |
| `github.com/charmbracelet/colorprofile` | `v0.4.3` | MIT | `LICENSE` |
| `github.com/charmbracelet/ultraviolet` | `v0.0.0-20260811164956-006e29f97886` | MIT | `LICENSE` |
| `github.com/charmbracelet/x/ansi` | `v0.11.8` | MIT | `LICENSE` |
| `github.com/charmbracelet/x/exp/slice` | `v0.0.0-20250327172914-2fdc97757edf` | MIT | `LICENSE` |
| `github.com/charmbracelet/x/term` | `v0.2.2` | MIT | `LICENSE` |
| `github.com/charmbracelet/x/termios` | `v0.1.1` | MIT | `LICENSE` |
| `github.com/charmbracelet/x/windows` | `v0.2.2` | MIT | `LICENSE` |
| `github.com/clipperhouse/displaywidth` | `v0.11.0` | MIT | `LICENSE` |
| `github.com/clipperhouse/uax29/v2` | `v2.7.0` | MIT | `LICENSE` |
| `github.com/dlclark/regexp2/v2` | `v2.2.1` | MIT AND BSD-3-Clause (file-specific) | `LICENSE`, `ATTRIB` |
| `github.com/fsnotify/fsnotify` | `v1.10.1` | BSD-3-Clause | `LICENSE` |
| `github.com/gorilla/css` | `v1.0.1` | BSD-3-Clause | `LICENSE` |
| `github.com/lucasb-eyer/go-colorful` | `v1.4.1` | MIT | `LICENSE` |
| `github.com/mattn/go-runewidth` | `v0.0.27` | MIT | `LICENSE` |
| `github.com/microcosm-cc/bluemonday` | `v1.0.27` | BSD-3-Clause | `LICENSE.md` |
| `github.com/muesli/cancelreader` | `v0.2.2` | MIT | `LICENSE` |
| `github.com/rivo/uniseg` | `v0.4.7` | MIT | `LICENSE.txt` |
| `github.com/xo/terminfo` | `v0.0.0-20220910002029-abceb7e1c41e` | MIT | `LICENSE` |
| `github.com/yuin/goldmark` | `v1.8.6` | MIT | [`LICENSE`](https://github.com/yuin/goldmark/blob/v1.8.6/LICENSE) |
| `github.com/yuin/goldmark-emoji` | `v1.0.5` | MIT | `LICENSE` |
| `go.yaml.in/yaml/v3` | `v3.0.5` | MIT AND Apache-2.0 (file-specific) | `LICENSE`, `NOTICE` |
| `golang.org/x/net` | `v0.56.0` | BSD-3-Clause | [`LICENSE`](https://go.googlesource.com/net/+/refs/tags/v0.56.0/LICENSE), [`PATENTS`](https://go.googlesource.com/net/+/refs/tags/v0.56.0/PATENTS) |
| `golang.org/x/sync` | `v0.22.0` | BSD-3-Clause | `LICENSE` |
| `golang.org/x/sys` | `v0.47.0` | BSD-3-Clause | `LICENSE` |
| `golang.org/x/text` | `v0.39.0` | BSD-3-Clause | [`LICENSE`](https://go.googlesource.com/text/+/refs/tags/v0.39.0/LICENSE), [`PATENTS`](https://go.googlesource.com/text/+/refs/tags/v0.39.0/PATENTS) |

## Attribution and distribution

Dependencies retain their own copyright and license notices. This repository
uses modules rather than vendoring their sources. When distributing binaries
or vendored sources, include the applicable dependency license texts and
notices, including the Go runtime's license; this inventory is not a substitute
for those texts. Recheck this inventory when updating dependencies.

Additional provenance to preserve when packaging:

- `golang.org/x/net v0.56.0` and `golang.org/x/text v0.39.0` retain the
  BSD-3-Clause license and `PATENTS` texts of the previously pinned v0.39.0 and
  v0.24.0 versions, respectively. Their upstream `PATENTS` files provide an
  additional patent grant. `github.com/yuin/goldmark v1.8.6` retains the MIT
  license text of v1.7.17 (and v1.7.8); `charm.land/bubbles/v2 v2.2.1` retains
  the MIT license text of v2.2.0. Preserve their existing copyright, license
  and patent notices when packaging.

- `go.yaml.in/yaml/v3` is **not simply MIT**: its `LICENSE` assigns MIT to the
  listed libyaml-ported files and Apache-2.0 to the remaining files. Its `NOTICE`
  is reproduced below.
- `github.com/rivo/uniseg` generated property tables explicitly reference the
  [Unicode license](https://www.unicode.org/license.html), for example
  `graphemeproperties.go`. `github.com/clipperhouse/uax29/v2/graphemes/trie.go`
  also identifies generated Unicode data. Their root MIT licenses alone do not
  describe all data provenance; retain the upstream Unicode notices.
- Chroma's `README.md` and `doc.go` identify Pygments-derived lexers/styles.
  In v2.27.0, `COPYING` additionally reproduces OFL-1.1 for the Liberation Mono
  font in `formatters/svg/font_liberation_mono.go`, whose font and license were
  already present in v2.14.0. The SVG formatter is imported and registered by
  Chroma's formatter package even though readmd renders terminal output.
  Preserve the font's Google/Red Hat copyright, reserved font names and OFL
  text when distributing it; the font is not relicensed under readmd's GPL.
- `github.com/dlclark/regexp2/v2 v2.2.1` retains v1.11.0's `LICENSE` and
  `ATTRIB` texts. `ATTRIB` supplies Microsoft's MIT notice for the .NET port,
  BSD-3-Clause terms for Go-derived code, and the Mono test-data permission
  notice. Preserve these attributions, not only the root MIT license.
  Its new `syntax/unicode_alias_tables.go` and `README.md` identify generated
  Unicode 17.0.0 property data and source files; preserve the applicable
  [Unicode notices](https://www.unicode.org/license.html) when packaging.
  These classifications are not independent clearance of every upstream
  contribution.

No copied third-party source or accompanying attribution headers were identified
in readmd's own Go files during this bounded inspection. Dependencies and their
upstream notices have not been rewritten or relicensed by readmd.

### go.yaml.in/yaml/v3 v3.0.5 — NOTICE (verbatim)

```text
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## readmd license provenance

[LICENSE](LICENSE) is the unmodified GNU GPL version 3 text from
<https://www.gnu.org/licenses/gpl-3.0.txt>.
SHA-256: `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986`.
The readmd grant in README.md is **GPL-3.0-only**; the generic application
instructions in the canonical license text do not grant a later-version option
for readmd.

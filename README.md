# readmd

A pager-style terminal markdown reader. Its one goal: make **any** markdown
file readable in a terminal — especially the wide GFM tables that other
renderers mangle — without reimplementing markdown. Rendering is glamour,
parsing is goldmark; this project is the frontend.

```
go build -o readmd . && ./readmd example.md
```

## Why

- **Wide tables** start in nowrap mode with column panning; `w` toggles wrap,
  and `T` collapses any table into key-value records.
  Transforms happen in markdown space, never by editing ANSI.
- **GitHub-style callouts** (`> [!NOTE]`) get a per-type colored rail and icon
  title, nvim render-markdown style.
- **Terminal-adaptive color**: the default theme is built purely from your
  terminal's own 16-color palette (starship-like — no hex, no truecolor, no
  painted backgrounds). `--style dark|light|notty` opts into fixed glamour
  themes.
- **Source view**: `s` flips to the raw markdown, one logical line per row, so
  terminal selection copies the exact text.

## Usage

```sh
readmd README.md          # pager
cat notes.md | readmd     # stdin when piped
readmd --wrap doc.md      # opt into wrapped rendering
readmd --style dark a.md  # fixed theme instead of palette-adaptive
```

| Flag | Meaning |
|------|---------|
| `--style auto\|dark\|light\|notty` | theme (default `auto`: follows terminal palette) |
| `--wrap` / `--no-wrap` | initial layout mode (default nowrap) |
| `--no-images` | never render figures as graphics |
| `--no-remote-images` | local images only, no network |

## Keybindings

| Key | Action |
|-----|--------|
| `j` `k` / `d` `u` / `ctrl+d` `ctrl+u` / `f` `b` `space` | line / half-page / page |
| `g` `G` | top / bottom |
| `h` `l` `0` | horizontal pan when content is wide, reset |
| `w` | toggle wrap / nowrap |
| `r` | reader column: centered 120-col prose |
| `s` | toggle rendered / source view |
| `o` | outline overlay (`j/k` select, `Enter` jump, `Esc` close) |
| `T` | collapse tables to key-value records |
| `/` | search; `n` / `N` next / previous match (highlighted) |
| `?` | help overlay |
| `R` | reload file |
| `c` | copy raw markdown to clipboard (OSC 52) |
| `e` | edit in `$VISUAL`/`$EDITOR` at the nearest heading |
| `q` `Esc` | quit (`Esc` first clears an active search) |

## Features

- Async rendering off the UI thread; resize always re-renders **from source**
  (cached raw markdown, never re-wrapped ANSI).
- TOC with jump-to-heading, incremental search with neovim-style match
  highlighting, live reload (survives atomic saves, keeps your reading
  position anchored to the nearest heading).
- GFM: tables, task lists, footnotes, strikethrough, alerts. Mermaid
  `flowchart`/`sequenceDiagram` render as box-drawing ASCII (unsupported
  diagram types decline to source instead of garbling). LaTeX math becomes
  Unicode (`$\alpha \leq \beta$` → α ≤ β).
- Images render through the kitty graphics protocol (kitty, ghostty, WezTerm)
  anchored to the text grid; everywhere else you get alt text. Remote images
  are fetched async and cached under `~/.cache/readmd/`.
- Links are OSC 8 hyperlinks with the full URL preserved as target.
- Grapheme-correct widths throughout: CJK, emoji and combining marks never
  split or misalign columns.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

The golden corpus in `testdata/corpus/` renders at widths 40/80/120 and
asserts no panics, no overlong lines, and no split graphemes. See
[AGENTS.md](AGENTS.md) for architecture rules and the development workflow.

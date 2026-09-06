# readmd

A pager-style terminal markdown reader. Its one goal: make **any** markdown
file readable in a terminal — especially the wide GFM tables that other
renderers mangle — without reimplementing Markdown parsing. Goldmark parses; stock Glamour
renders blocks and inline formatting, with a public-API Lipgloss adapter for
top-level tables.

```
go build -o readmd . && ./readmd example.md
```

## Why

- **Prose wraps; structural blocks pan.** Top-level paragraphs and headings
  use stock Glamour wrapping at the viewport width. Top-level table body cells
  soft-wrap at whitespace around **40 display columns per column**. Complete
  headers and unbreakable tokens set the minimum width; short columns stay
  compact. Tables can still exceed the viewport: use `h`/`l`/`0` to pan.
  Code/Mermaid, whole lists, blockquotes, and definition lists retain natural
  width. `r` centers a pannable reader viewport (up to 120 columns), with
  natural-width prose and the same table-cell wrapping.
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
readmd --style dark a.md  # fixed theme instead of palette-adaptive
```

| Flag | Meaning |
|------|---------|
| `--style auto\|dark\|light\|notty` | theme (default `auto`: follows terminal palette) |
| `--no-images` | never render figures as graphics |
| `--no-remote-images` | local images only, no network |

## Keybindings

| Key | Action |
|-----|--------|
| `j` `k` / `d` `u` / `ctrl+d` `ctrl+u` / `f` `b` `space` | line / half-page / page |
| `g` `G` | top / bottom |
| `h` `l` `0` | horizontal pan when content is wide, reset |
| `t` | hint visible links and footnotes; type the fixed-width label, `Backspace` edits, `Esc` cancels |
| `m` | toggle mouse capture (enabled by default) |
| `r` | centered 120-column reader viewport with horizontal panning |
| `s` | toggle rendered / source view |
| `o` | help-style outline (`j/k` or wheel preview, `/` filters, `Enter` commits, `Esc`/`q`/`o` cancel; prompt `Esc` clears first) |
| `/` | search; `n` / `N` next / previous match (highlighted) |
| `?` | help overlay |
| `R` | reload file |
| `c` | copy raw markdown to clipboard (OSC 52) |
| `e` | edit in `$VISUAL`/`$EDITOR` at the nearest heading |
| `q` `Esc` | quit (`Esc` first clears an active search) |

## Features

- Async rendering off the UI thread; resize always re-renders **from source**
  (cached raw markdown, never transformed cached ANSI). The no-fork adapter
  parses the preprocessed document globally with Goldmark, renders independent
  top-level AST units through public Glamour ANSI APIs, and composes fresh
  output before postprocessing and navigation metadata collection.
- Help-style TOC with title filtering and reversible heading previews (`Esc`,
  `q`, or `o` cancels; filter-prompt `Esc` clears before closing), incremental
  search with neovim-style match highlighting, live reload
  (survives atomic saves, keeps your reading position anchored to the nearest
  heading).
- GFM: tables, task lists, footnotes, strikethrough, alerts. Mermaid
  `flowchart`/`sequenceDiagram` render as box-drawing ASCII (unsupported
  diagram types decline to source instead of garbling). LaTeX math becomes
  Unicode (`$\alpha \leq \beta$` → α ≤ β).
- Images render through the kitty graphics protocol (kitty, ghostty, WezTerm)
  anchored to the text grid; everywhere else you get alt text. Remote images
  are fetched async and cached under `~/.cache/readmd/`.
- Links are OSC 8 hyperlinks with the full URL preserved as target; `t` gives visible links and footnote references fixed-width keyboard hints. HTTP, HTTPS, and mailto targets open externally; relative and file navigation is deferred. In target mode, type the label, edit with `Backspace`, or cancel with `Esc`; in normal mode, `Backspace` returns from a footnote jump. Mouse capture is enabled by default: click a visible link or footnote reference, and use the wheel to scroll. `m` releases capture for terminal-native selection and scrolling. While capture is active, terminal-native selection commonly uses Shift and varies by emulator.
- Grapheme-correct widths throughout: CJK, emoji and combining marks never
  split or misalign columns.

### Rendering tradeoffs

Stock Glamour can hard-split long prose tokens, including inline code and
printed URLs; OSC 8 destinations still retain the full URL. Explicit Markdown
hard breaks survive; soft source line breaks flow as spaces. Search remains
line-local, so phrases spanning a wrap boundary do not match. Reader or source
view can be used to inspect an unbroken token. GFM table parsing rules still
apply: escape pipes even inside inline code, and cells beyond the header count
are discarded by Goldmark. Table IDs, endpoints, URLs, hyphenated tokens, CJK
runs and grapheme clusters never split to meet the preferred width. Multiline
tables add row rules so neighboring records remain distinguishable. Table links
show their formatted label with the full OSC 8 target, without repeated visible
URLs or numbered link footers; bare/autolink URLs remain visible.

Only **top-level tables** use this policy. Tables nested in lists or blockquotes
stay on stock Glamour with their complete container and do not soft-wrap cells.
Source view remains raw. There is no wrap-mode toggle or table-records mode.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

The golden corpus in `testdata/corpus/` renders at widths 40/80/120 and
asserts no panics, preserved content, and no split graphemes. See
[AGENTS.md](AGENTS.md) for architecture rules and the development workflow.

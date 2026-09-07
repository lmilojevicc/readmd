# readmd

A pager-style terminal markdown reader. Its one goal: make **any** markdown
file readable in a terminal — especially the wide GFM tables that other
renderers mangle — without reimplementing Markdown parsing. Goldmark parses; stock Glamour
renders blocks and inline formatting, with a public-API Lipgloss adapter for
top-level tables.

```
GOWORK=off go build -o readmd . && ./readmd example.md
```

## Why

- **Prose wraps; structural blocks pan.** Top-level paragraphs and headings
  use stock Glamour wrapping at the viewport width. Top-level table body cells
  soft-wrap at whitespace around **40 display columns per column** by default. Complete
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

## Configuration

On normal startup, readmd reads `$XDG_CONFIG_HOME/readmd/config.yaml` when
`XDG_CONFIG_HOME` is nonempty; otherwise it uses
`$HOME/.config/readmd/config.yaml`, including on macOS. Relative
`XDG_CONFIG_HOME` is an error. Missing directories and a commented default file
are created privately, create-only; existing files are never rewritten.
Creation failures warn and use defaults where possible. Existing unreadable or
invalid files fail with their path and the offending setting.

Precedence: **built-in defaults < validated YAML file < explicit CLI flags**.
Omitted flags preserve file booleans. Only the existing `--style`, `--no-images`,
and `--no-remote-images` flags override settings; even overridden file values
must be valid. Empty/comment-only files and omitted keys use defaults; present
null/empty values are errors (omit a key to use its default). No config live reload; session `m`/`r` changes are never saved.

```yaml
style: auto
mouse: true
picker: list
reader: false
reader_width: 120
table_cell_width: 40
images: true
remote_images: true
```

See [config.example.yaml](config.example.yaml) for comments and allowed values.
Widths must be integers in **1..10000**, bounding per-line allocation and leaving
safe arithmetic headroom. Reader geometry is `max(1, min(reader_width, vw-2))`,
centered without reflowing prose. Table width is a preferred **body-cell** wrap
width, not a document/table clamp: complete headers and unbreakable tokens may
exceed it. `images: true` still requires supported terminal graphics;
`remote_images: true` does not bypass image security or resource limits.

`picker: list` (default) uses the focus/details panel described below.
`picker: vimium` uses modest candidate underlines and per-occurrence inline
badges; cramped/colliding targets go to a pageable fallback shelf. `Tab`/arrows
cycle shelf focus and `Enter` activates it; typing labels activates either kind.
Both designs enter with `p`, freeze the same visible targets and coordinates,
retain full URL queries/fragments, and restore the original view on `Esc`.
`j`/`k` remain hint letters. The vimium wheel is frozen; list wheel moves focus.
Vimium needs at least 24 columns and three document rows; list needs 30 columns
and one row. Neither design clicks through its overlay/status/margins.

## Keybindings

| Key | Action |
|-----|--------|
| `j` `k` / `d` `u` / `ctrl+d` `ctrl+u` / `f` `b` `space` | line / half-page / page |
| `g` `G` | top / bottom |
| `h` `l` `0` | horizontal pan when content is wide, reset |
| `p` | pick visible links/references: type hint, `Tab`/arrows focus, `Enter` activates, `Backspace` edits, `Esc` cancels |
| `m` | toggle mouse capture (enabled by default) |
| `r` | centered reader viewport (120 columns by default) with horizontal panning |
| `o` | help-style outline (`j/k` or wheel preview, `/` filters, `Enter` commits, `Esc`/`q`/`o` cancel; prompt `Esc` clears first) |
| `/` | search; `n` / `N` next / previous match (highlighted) |
| `?` | help overlay |
| `R` | reload file |
| `c` | copy raw markdown to clipboard (OSC 52) |
| `e` | edit in `$VISUAL`/`$EDITOR` at the nearest heading |
| `q` `Esc` | quit (`Esc` first clears an active search) |

Lowercase `s`, `t`, and `w` are unbound in normal mode (`s` is literal input in prompts).

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
- Links are OSC 8 hyperlinks with the full URL preserved as target; `p` gives visible links and footnote references fixed-width keyboard hints. HTTP, HTTPS, and mailto targets open externally; relative and file navigation is deferred. The bottom-attached picker overlays the document without moving or reflowing it; even targets covered by the list remain selectable. Type the hint (`j`/`k` are letters), use `Tab`/`Shift+Tab` or arrows to focus, `PgUp`/`PgDown` to page, and `Enter` to activate. `Ctrl+Left`/`Ctrl+Right` pages long focus details, including complete destinations. `Backspace` edits the prefix and `Esc` restores the original view. Click a list row or uncovered document target to activate; the wheel moves list focus while the document stays frozen. Short terminals use one focus row; fewer than 30 columns or no document rows declines the picker. If stock wide-grapheme slicing makes screen coordinates unsafe, picking and document target clicks ask you to adjust pan or press `0`. In normal mode, `Backspace` returns from a footnote jump. Mouse capture is enabled by default: click a visible link or footnote reference, and use the wheel to scroll. `m` releases capture for terminal-native selection and scrolling. While capture is active, terminal-native selection commonly uses Shift and varies by emulator.
- Grapheme-correct widths throughout: CJK, emoji and combining marks never
  split or misalign columns.

### Rendering tradeoffs

Stock Glamour can hard-split long prose tokens, including inline code and
printed URLs; OSC 8 destinations still retain the full URL. Explicit Markdown
hard breaks survive; quoted source lines keep their line breaks and stock rail/list
indentation. Ordinary paragraph soft breaks flow as spaces. Search remains
line-local, so phrases spanning a wrap boundary do not match. Reader view can be used to inspect an unbroken token. GFM table parsing rules still
apply: escape pipes even inside inline code, and cells beyond the header count
are discarded by Goldmark. Table IDs, endpoints, URLs, hyphenated tokens, CJK
runs and grapheme clusters never split to meet the preferred width. Multiline
tables add row rules so neighboring records remain distinguishable. Table links
show their formatted label with the full OSC 8 target, without repeated visible
URLs or numbered link footers; bare/autolink URLs remain visible.

Only **top-level tables** use this policy. Tables nested in lists or blockquotes
stay on stock Glamour with their complete container and do not soft-wrap cells.
There is no wrap-mode toggle or table-records mode.

## Development

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...
```

Run `GOWORK=off go test -race ./... -count=1` for the race gate. On Unix,
`python3 testdata/startup_pty.py /path/to/readmd` runs bounded startup/picker checks
against a built binary with temporary HOME/XDG directories only.

The golden corpus in `testdata/corpus/` renders at widths 40/80/120 and
asserts no panics, preserved content, and no split graphemes. See
[AGENTS.md](AGENTS.md) for architecture rules and the development workflow.

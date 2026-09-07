# readmd

Terminal markdown reader (pager-style TUI) in Go. Goal: make ANY markdown file readable in a terminal — especially wide GFM tables — WITHOUT reimplementing Markdown parsing (Goldmark parses; stock Glamour renders all blocks except top-level tables, which use its public inline elements and the Lipgloss table API).

## Commands

- Build: `GOWORK=off go build ./...`
- Vet: `GOWORK=off go vet ./...`
- All tests: `GOWORK=off go test -count=1 ./...`
- Race gate: `GOWORK=off go test -race -count=1 ./...`
- Lint (v2.13.2): `GOWORK=off golangci-lint run`
- Formatting: `GOWORK=off golangci-lint fmt --diff`
- Single package/test: `GOWORK=off go test ./internal/<pkg> -run <TestName>`
- Run: `GOWORK=off go run . <file.md>` (reads stdin when piped and no file arg)

CI tests Linux and macOS; Windows support is not promised. See
[CONTRIBUTING.md](CONTRIBUTING.md) for setup and isolated PTY checks.

## Locked stack decisions

- Charm stack **v2** module paths (`charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, `glamour/v2`). Fall back to v1 paths ONLY if a v2 API is verifiably broken; record the deviation in the commit/report.
- Theme: default `--style auto` = palette-adaptive ANSI-index style that follows the terminal's own 16-color theme (starship-like: zero hex/truecolor, zero painted backgrounds); `--style dark|light|notty` opts into glamour's fixed hex/attribute-only styles. (changed by user request after phase 1, refined again after first color attempt)
- NO hardcoded max-width clamp on structural content (glow's #942 mistake). Normal view wraps top-level paragraphs/headings with stock Glamour at viewport width; code/Mermaid, whole lists, blockquotes and definition lists render at intrinsic width and pan. Top-level table body cells soft-wrap at whitespace around `table_cell_width` display columns (default 40), independently per column, with complete headers and longest unbreakable tokens as width floors. Short columns stay compact; tables never shrink to the viewport. Reader keeps prose natural-width and uses the same table-cell policy. Nested tables remain on stock Glamour with their complete containers, without cell wrapping.
- goldmark (already a glamour dependency) is the AST source for TOC/positions. glamour does NOT map AST nodes → rendered lines; use the markdown-space preprocessing technique instead (see below).

## Architecture rules

- Cache the RAW markdown source. On resize / reader-toggle / reload, re-render FROM SOURCE. Never transform cached ANSI strings.
- Render off the UI thread in a `tea.Cmd`; the viewport model owns scrolling.
- Horizontal scroll offsets are stored in display COLUMNS, sliced grapheme-aware — never byte or rune counts (CJK/emoji correctness).
- Wide tables retain their intrinsic column widths with whole-document horizontal pan. There is no wrap-mode toggle or table-records mode.
- Preprocessing transforms happen in MARKDOWN SPACE (parse with goldmark → rewrite source ranges → re-render). The no-fork adapter may compose freshly rendered top-level AST units after global parsing, then run postprocessing and collect global metadata; never mutate cached ANSI. The bounded top-level-table adapter owns table layout via public Lipgloss APIs and cell formatting via public Glamour inline elements; no custom inline Markdown parser. Table links use label-only OSC 8 (autolinks retain their visible URL), without stock table link footers; unique occurrence IDs preserve multiline hit regions.
- Code blocks never silently reflow.
- Links always OSC 8 with full URL preserved as target.

## Conventions

- Fewest files/packages that stay readable. No speculative abstractions. No comments unless genuinely necessary.
- Table-driven tests only, no test frameworks.
- Golden corpus lives in `testdata/` and is rendered at widths {40, 80, 120}; assertions: no panic, content tokens and table geometry survive, graphemes never split.
- Every phase leaves at least one runnable check that fails if its logic breaks.
- Keybindings (pager conventions): j/k d/u ctrl+d/ctrl+u f/b/space g/G h/l/0 p m r o / ? n N R c e q Esc. Lowercase `s`, `t` and `w` are unbound (literal in prompts). `Esc` clears an active search, then quits. `o` = help-style outline (j/k and wheel preview headings, `/` filters titles, Enter commits, Esc/q/o cancel without moving; prompt Esc clears prompt/filter before a later Esc closes). `p` = visible-target hints for links and footnote references (type the fixed-width label, Backspace edits, Esc cancels); normal-mode Backspace returns from an internal jump. Mouse capture defaults on: clicks activate visible targets, the wheel scrolls, and `m` toggles capture for terminal-native selection/scrolling; Shift-selection support varies by terminal. `r` = reader viewport toggle: viewport width becomes max(1, min(reader_width, vw-2)) with a uniform display-only left margin (block centering) and horizontal panning; position preserved via the heading anchor; status bar gains a `reader` tag. `R` = reload file (stdin documents refuse). `/` = forward search prompt, `n`/`N` = next/previous match; there is NO backward search. `?` = help overlay (j/k scroll, any other key closes at the exact position). `c` = copy raw markdown via OSC 52 (oversized documents decline with a flash). `e` = edit in $VISUAL/$EDITOR (vi fallback) positioned at the nearest heading above the viewport top via `+N`; stdin documents refuse; editor exit reloads through the live-reload path (flash `edited`, watcher ticks during the edit coalesce with it).

## Workflow

Development runs as an agent loop: implementer → three parallel reviewers (spec compliance, edge cases, conventions) → fix worker for any findings → verify build/vet/test before a phase counts as done.

## Roadmap status

1. [x] Walking skeleton: file/stdin → async glamour render → viewport, less keys, resize re-render
2. [x] Natural-width table panning + golden corpus
3. [x] TOC overlay/jump-to-heading + incremental search
4. [x] Live reload (fsnotify)
5. [x] Images (kitty placeholder grid; alt-text fallback)
6. [x] Mermaid ASCII + math → Unicode substitution
7. [x] GFM alert callouts (per-type rail + icon title), palette-adaptive theme
8. [x] `?` help overlay, layout flag cleanup (source view subsequently removed)
9. [x] Search match highlighting; README
10. [x] Reader mode (`r` centered reading column) with reload moved to `R`; `c` OSC-52 copy + `e` $VISUAL/$EDITOR edit; glow-style status bar; help modal redesign; Esc-clears-search ladder; mermaid cylinder shape + cycle tolerance

## Startup configuration and pickers

- YAML: `$XDG_CONFIG_HOME/readmd/config.yaml`, otherwise `$HOME/.config/readmd/config.yaml` on all platforms. Reject relative XDG paths; never use additional search paths. Defaults and comments: `config.example.yaml`.
- Validate the whole strict single-mapping file before explicit existing CLI overrides (`--style`, `--no-images`, `--no-remote-images`). Unknown/duplicate keys, wrong types, and widths outside 1..10000 are errors. No live reload or persistence of runtime toggles.
- Missing config bootstraps via a fully written private temporary file and create-only hard link. Existing config is never replaced. Creation failure warns; an unreadable/malformed existing file errors with its path. CLI usage errors must precede filesystem effects. CLI/PTY tests must isolate HOME/XDG.
- `picker: list|vimium` selects the presentation at startup; default list. One targetMode owns frozen candidates/labels, safe coordinate rows, and saved geometry/notices. Concrete list/vimium substates and direct enum boundaries only; no plugin framework. Preserve unsafe physical-wrap refusal for CJK/grapheme slices, final View/hit-plan agreement, overlay click-through guards, footnote history, and in-flight render refusal.
- `mouse: true` defaults capture on; `reader: false`, `reader_width: 120`, `table_cell_width: 40`, `images: true`, `remote_images: true`. Configuration does not bypass graphics detection or image security. Async render commands snapshot settings before execution.
- Source view is removed. Preserve raw `Model.source`, heading source lines, semantic link/footnote source parsing, sanitize, copy/edit/reload, and shared geometry helpers. Blockquotes (including list-nested quotes) preserve source line breaks and stock rail/list indentation; ordinary paragraph soft breaks still flow.

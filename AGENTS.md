# readmd

Terminal markdown reader (pager-style TUI) in Go. Goal: make ANY markdown file readable in a terminal — especially wide GFM tables — WITHOUT reimplementing markdown parsing/rendering (glamour renders, goldmark parses; our code is the frontend).

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- All tests: `go test ./...`
- Single package/test: `go test ./internal/<pkg> -run <TestName>`
- Run: `go run . <file.md>` (reads stdin when piped and no file arg)

## Locked stack decisions

- Charm stack **v2** module paths (`charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, `glamour/v2`). Fall back to v1 paths ONLY if a v2 API is verifiably broken; record the deviation in the commit/report.
- Theme: default `--style auto` = palette-adaptive ANSI-index style that follows the terminal's own 16-color theme (starship-like: zero hex/truecolor, zero painted backgrounds); `--style dark|light|notty` opts into glamour's fixed hex/attribute-only styles. (changed by user request after phase 1, refined again after first color attempt)
- NO hardcoded max-width clamp on rendered content (glow's #942 mistake). Render at natural width and pan the viewport.
- goldmark (already a glamour dependency) is the AST source for TOC/positions. glamour does NOT map AST nodes → rendered lines; use the markdown-space preprocessing technique instead (see below).

## Architecture rules

- Cache the RAW markdown source. On resize / reader-toggle / reload, re-render FROM SOURCE. Never transform cached ANSI strings.
- Render off the UI thread in a `tea.Cmd`; the viewport model owns scrolling.
- Horizontal scroll offsets are stored in display COLUMNS, sliced grapheme-aware — never byte or rune counts (CJK/emoji correctness).
- Wide-table ladder: 1) natural-width tables with whole-document horizontal pan, 2) `T` collapses tables to key-value records. There is no wrap mode.
- Table collapse and other transforms happen in MARKDOWN SPACE (parse with goldmark → rewrite source ranges → re-render), never by editing rendered ANSI.
- Code blocks never silently reflow.
- Links always OSC 8 with full URL preserved as target.

## Conventions

- Fewest files/packages that stay readable. No speculative abstractions. No comments unless genuinely necessary.
- Table-driven tests only, no test frameworks.
- Golden corpus lives in `testdata/` and is rendered at widths {40, 80, 120}; assertions: no panic, content tokens and table geometry survive, graphemes never split.
- Every phase leaves at least one runnable check that fails if its logic breaks.
- Keybindings (pager conventions): j/k d/u ctrl+d/ctrl+u f/b/space g/G h/l/0 t m s T r o / ? n N R c e q Esc. Lowercase `w` is unbound. `Esc` clears an active search, then quits. `o` = outline (table of contents; j/k move selection, Enter jump, Esc/q/o close without moving). `t` = visible-target hints for links and footnote references (type the fixed-width label, Backspace edits, Esc cancels); normal-mode Backspace returns from an internal jump. Mouse capture defaults on: clicks activate visible targets, the wheel scrolls, and `m` toggles capture for terminal-native selection/scrolling; Shift-selection support varies by terminal. `s` = rendered/source toggle (representation only). `r` = reader viewport toggle: viewport width becomes min(120, vw-2) with a uniform display-only left margin (block centering) and horizontal panning; frozen in source view like T; position preserved via the heading anchor; status bar gains a `reader` tag. `R` = reload file (stdin documents refuse). `/` = forward search prompt, `n`/`N` = next/previous match; there is NO backward search. `?` = help overlay (j/k scroll, any other key closes at the exact position). `c` = copy raw markdown via OSC 52 (oversized documents decline with a flash). `e` = edit in $VISUAL/$EDITOR (vi fallback) positioned at the nearest heading above the viewport top via `+N`; stdin documents refuse; editor exit reloads through the live-reload path (flash `edited`, watcher ticks during the edit coalesce with it).

## Workflow

Development runs as an agent loop: implementer → three parallel reviewers (spec compliance, edge cases, conventions) → fix worker for any findings → verify build/vet/test before a phase counts as done.

## Roadmap status

1. [x] Walking skeleton: file/stdin → async glamour render → viewport, less keys, resize re-render
2. [x] Natural-width table panning + table collapse + golden corpus
3. [x] TOC overlay/jump-to-heading + incremental search
4. [x] Live reload (fsnotify)
5. [x] Images (kitty placeholder grid; alt-text fallback)
6. [x] Mermaid ASCII + math → Unicode substitution
7. [x] GFM alert callouts (per-type rail + icon title), palette-adaptive theme
8. [x] `s` rendered/source view toggle, `?` help overlay, layout flag cleanup
9. [x] Search match highlighting; README
10. [x] Reader mode (`r` centered reading column) with reload moved to `R`; `c` OSC-52 copy + `e` $VISUAL/$EDITOR edit; glow-style status bar; help modal redesign; Esc-clears-search ladder; mermaid cylinder shape + cycle tolerance

# Usage

[readmd](../README.md) · [Configuration](configuration.md)

## Open a document

```sh
readmd README.md          # pager
cat notes.md | readmd     # stdin when piped, with no file argument
readmd --style dark a.md  # fixed theme instead of palette-adaptive
```

| Flag | Meaning |
|------|---------|
| `--style auto\|dark\|light\|notty` | Theme (default `auto`: follows terminal palette). |
| `--no-images` | Never render figures as graphics. |
| `--no-remote-images` | Local images only, no network. |

See [Configuration](configuration.md) for file defaults and CLI precedence.
Linux and macOS are supported; Windows support is not promised.

## Keybindings

| Key | Action |
|-----|--------|
| `j` `k` / `d` `u` / `ctrl+d` `ctrl+u` / `f` `b` `space` | Line / half-page / page. |
| `g` `G` | Top / bottom. |
| `h` `l` `0` | Horizontal pan when content is wide, reset. |
| `p` | Pick visible links/references: type hint, `Tab`/arrows focus, `Enter` activates, `Backspace` edits, `Esc` cancels. |
| `Backspace` | Return from a footnote jump in normal mode. |
| `m` | Toggle mouse capture (enabled by default). |
| `r` | Toggle centered reader viewport (120 columns by default) with horizontal panning. |
| `o` | Outline: `j`/`k` or wheel preview, `/` filters titles, `Enter` commits, `Esc`/`q`/`o` cancel; prompt `Esc` clears first. |
| `/` | Forward search; `n` / `N` next / previous match (highlighted). |
| `?` | Help overlay: `j`/`k` scroll; any other key closes at the original position. |
| `R` | Reload file (unavailable for stdin). |
| `c` | Copy raw Markdown to clipboard via OSC 52; oversized documents decline with a notice. |
| `e` | Edit in `$VISUAL`/`$EDITOR` (`vi` fallback) at the nearest heading above the viewport; reload on exit (unavailable for stdin). |
| `q` `Esc` | Quit (`Esc` first clears an active search). |

Lowercase `s`, `t`, and `w` are unbound in normal mode and literal input in
prompts. There is no `T` table mode, wrap-mode toggle, table-records mode, or
source view. There is no backward-search prompt; `N` visits previous matches
of the forward search.

## Reader, outline, and search

`r` centers a reader viewport up to 120 display columns by default, bounded by
`max(1, min(reader_width, vw-2))`. The left margin is display-only; prose keeps
its natural width and can pan horizontally. Table cells use the same wrapping
policy as normal view. The heading anchor preserves your place when toggling,
and the status bar shows `reader`.

The outline offers title filtering and reversible heading previews. `Esc`,
`q`, or `o` cancels without moving the document; in the filter prompt, `Esc`
clears the prompt/filter before a later `Esc` closes the outline.

Search is incremental, with highlighted matches. It is line-local: phrases
spanning a wrap boundary do not match. Files reload live, including across
atomic saves, keeping your reading position anchored to the nearest heading.
`R` reloads manually; editor exit also reloads the file.

## Links and footnotes

Links use OSC 8 hyperlinks with the full URL preserved, including queries and
fragments. `p` gives visible links and footnote references fixed-width keyboard
hints. HTTP, HTTPS, and mailto targets open externally; relative and file
navigation and ordinary `#heading` links are not supported. Footnote jumps
stay in the reader; normal-mode `Backspace` returns from a jump.

The default list picker overlays the bottom of the document without moving
or reflowing it; even targets covered by the list remain selectable:

- Type a hint (`j`/`k` are letters, not scroll keys).
- Use `Tab`/`Shift+Tab` or arrows to focus and `PgUp`/`PgDown` to page.
- `Enter` activates; `Ctrl+Left`/`Ctrl+Right` pages long focus details,
  including complete destinations.
- `Backspace` edits the prefix; `Esc` restores the original view.
- Click a list row or uncovered document target to activate. The wheel moves
  list focus while the document stays frozen.

Short terminals use one focus row; fewer than 30 columns or no document rows
refuses the list picker. The alternative vimium picker uses inline badges and
a fallback shelf; see [picker preferences](configuration.md#picker-and-mouse-preferences).
Picking is unavailable while a render is in flight. If wide-grapheme slicing
makes screen coordinates unsafe, picking and document target clicks ask you
to adjust pan or press `0`. Overlays, status bars, and margins do not allow
click-through to covered document targets.

Mouse capture is enabled by default: click visible links or footnote references
and use the wheel to scroll. `m` releases capture for terminal-native selection
and scrolling. While captured, terminal-native selection commonly requires
Shift and varies by emulator. OSC 8 link handling and OSC 52 clipboard copying
also depend on terminal support.

## Rendering

GFM tables, task lists, footnotes, strikethrough, and alerts are supported.
GitHub-style callouts (`> [!NOTE]`) get per-type colored rails and icon titles.
Mermaid `flowchart` and `sequenceDiagram` render as box-drawing ASCII;
unsupported diagram types fall back to source. LaTeX math becomes Unicode
(for example, `$\alpha \leq \beta$` becomes α ≤ β).

The default `auto` theme uses your terminal's own 16-color palette, with no
hex/truecolor colors or painted backgrounds. `--style dark|light|notty` selects
fixed Glamour styles instead (`notty` is attribute-only).

### Wrapping and tables

In normal view, top-level paragraphs and headings wrap at the viewport width.
Code/Mermaid, whole lists, blockquotes, and definition lists retain natural
width and pan; code blocks never silently reflow. Explicit Markdown hard
breaks survive. Quoted source lines preserve line breaks and rail/list
indentation, including list-nested quotes; ordinary paragraph soft breaks flow
as spaces.

Top-level table body cells soft-wrap at whitespace around **40 display columns
per column** by default. Complete headers and longest unbreakable tokens set
minimum widths; short columns stay compact. Tables never shrink to the viewport:
use `h`/`l`/`0` to pan wide content. IDs, endpoints, URLs, hyphenated tokens, CJK
runs, and grapheme clusters never split to meet the preferred width. Multiline
tables add row rules to distinguish neighboring records.

Table links show their formatted label with the full OSC 8 target, without
repeated visible URLs or numbered link footers; bare/autolink URLs remain
visible. GFM parsing rules still apply: escape pipes even inside inline code,
and cells beyond the header count are discarded by Goldmark.

Only **top-level tables** use this policy. Tables nested in lists or blockquotes
stay on stock Glamour with their complete container and do not soft-wrap cells.

Stock Glamour can hard-split long prose tokens, including inline code and
printed URLs; OSC 8 destinations still retain the full URL. Reader view can
help inspect an unbroken token. Widths and horizontal offsets account for CJK,
emoji, and combining marks; unsafe physical-wrap coordinates still have the
picking limitations described above.

### Images

Images use the kitty graphics protocol (kitty, ghostty, WezTerm), anchored to
the text grid. Other terminals get alt text. Remote images are fetched
asynchronously and cached under `~/.cache/readmd/`. Enabling images in the
configuration does not bypass terminal detection or image security/resource
limits. Use `--no-images` to disable graphics or `--no-remote-images` to allow
only local images.

Image layout uses terminal-reported cell pixels, with a 10×20-pixel fallback
until a valid report arrives (or if unsupported). Kitty virtual placements
preserve image aspect ratio; reserved rows may include spare space. Decoded
images remain capped at 1000 pixels on the longest side, so detailed screenshots
can lose sharpness even when the cell geometry is correct.

For rendering architecture and raw-source handling, see [AGENTS.md](../AGENTS.md).

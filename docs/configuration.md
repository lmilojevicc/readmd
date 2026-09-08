# Configuration

[readmd](../README.md) · [Usage](usage.md)

## File location and startup

On normal startup, readmd reads `$XDG_CONFIG_HOME/readmd/config.yaml` when
`XDG_CONFIG_HOME` is nonempty; otherwise it uses
`$HOME/.config/readmd/config.yaml`, including on macOS. Relative
`XDG_CONFIG_HOME` is an error. No other locations are searched.

Missing directories and a commented default file are created privately. The
file is fully written to a private temporary file and installed with a
create-only hard link; existing files are never replaced or rewritten.
Creation failures warn and use defaults where possible. Existing unreadable or
invalid files fail with their path; validation errors identify the offending
setting. CLI usage errors are checked before filesystem effects.

Precedence: **built-in defaults < validated YAML file < explicit CLI flags**.
`--style`, `--theme`, `--no-images`, and `--no-remote-images` override settings;
omitted flags preserve file booleans. The whole file must be valid even if a
CLI flag overrides a value.

The file must be a single YAML mapping. Unknown or duplicate keys, multiple
YAML documents, wrong types, and out-of-range widths are errors. Empty or
comment-only files and omitted keys use defaults; present null/empty values
are errors (omit a key to use its default).

Configuration is startup-only: there is no config live reload, and document
reload does not reread it. Session `m`/`r` toggles are never saved.

## Settings

The commented [config.example.yaml](../config.example.yaml) contains all defaults:

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

| Setting | Allowed values | Meaning |
|---------|----------------|---------|
| `style` | `auto`, `dark`, `light`, `notty` | `auto` follows the terminal's 16-color palette; the others select fixed Glamour styles (`notty` is attribute-only). |
| `theme` | Nonempty local file path, optional | Select a partial theme; relative paths are resolved beside config.yaml. CLI `--theme PATH` wins. |
| `mouse` | `true`, `false` | Start with mouse capture enabled or disabled; `m` toggles it for the session. |
| `picker` | `list`, `vimium` | Presentation used by `p` for visible links and footnote references. |
| `reader` | `true`, `false` | Start in the centered reader view; `r` toggles it for the session. |
| `reader_width` | Integer **1..10000** | Preferred reader viewport width in display columns. |
| `table_cell_width` | Integer **1..10000** | Preferred top-level table body-cell wrap width in display columns. |
| `images` | `true`, `false` | Allow graphics when supported by the terminal. |
| `remote_images` | `true`, `false` | Allow remote image fetching, subject to image security and resource limits. |

Width limits bound per-line allocation and leave safe arithmetic headroom.
Reader geometry is `max(1, min(reader_width, vw-2))`, where `vw` is the available
terminal viewport width. It is centered, with natural-width prose and horizontal
panning rather than prose reflow to the reader width.

Table width is a preferred **body-cell** wrap width, not a document/table clamp:
complete headers and unbreakable tokens may exceed it; short columns stay
compact. See [wrapping and tables](usage.md#wrapping-and-tables).

`images: true` still requires supported terminal graphics; `remote_images: true`
does not bypass image security or resource limits. See [images](usage.md#images).

## Picker and mouse preferences

`picker` configures only the `p` link/footnote picker, not the Markdown file browser.

`picker: list` (default) uses a bottom-attached focus/details panel.
`picker: vimium` uses modest candidate underlines and per-occurrence inline
badges; cramped or colliding targets go to a pageable fallback shelf. `Tab` or
arrows cycle shelf focus and `Enter` activates it; typing labels activates
either kind of target.

Both designs enter with `p`, freeze the same visible targets and coordinates,
retain full URL queries/fragments, and restore the original view on `Esc`.
`j`/`k` remain hint letters. The vimium wheel is frozen; the list wheel moves
focus. Vimium needs at least 24 columns and three document rows; list needs
30 columns and one row. Neither design clicks through its overlay, status bar,
or margins. See [links and footnotes](usage.md#links-and-footnotes) for controls
and coordinate-safety limitations.

Mouse capture defaults on: clicks activate visible targets and the wheel
scrolls. Press `m` to release capture for terminal-native selection and
scrolling. While capture is active, terminal-native selection commonly uses
Shift, but support varies by emulator.


## Custom themes

A theme is a **separate partial YAML mapping**, applied after resolving the base
`style` and its CLI override. `--style` does not discard theme overrides.
The [theme example](../theme.example.yaml) is reusable; it is not installed
or written automatically.

Selection, in order:

1. `--theme PATH` (also `--theme=PATH`), relative to the current working directory.
2. `theme: PATH` in config.yaml, relative to the directory containing config.yaml.
3. `theme.yaml`, then `theme.yml`, beside the resolved config.yaml.

Absolute paths work in both explicit forms. There is no tilde expansion,
include/inheritance chain, additional search path, or network loading. Missing
autodiscovery is normal. A selected missing, unreadable, or invalid file is an
error containing its path; a broken theme.yaml does not fall through to theme.yml.
Only the **selected** theme is read and validated, but the entire config.yaml must
still be valid even when CLI flags override it. Existing config and theme files
are never overwritten. Restart readmd after editing a theme: document reload,
resize and reader toggles rerender raw Markdown with the same startup snapshot.

### Fields and inheritance

All fields are optional. Empty/comment-only theme files and `{}` inherit the
base. Unknown/duplicate keys at any level, nulls, YAML aliases, multiple documents,
wrong types, invalid colors and unsafe glyphs are errors. Omit a field to inherit;
`false` disables an attribute rather than falling back to the base.

Shared text-style fields:

| Field | Values |
|-------|--------|
| `fg` | Decimal ANSI integer **0..255**, quoted `"#RRGGBB"`, or `default` |
| `bg` | Decimal ANSI integer **0..255**, quoted `"#RRGGBB"`, or `none` |
| `bold`, `italic`, `underline`, `strikethrough` | `true` or `false` |

ANSI 0..15 follows the terminal's palette. `fg: default` explicitly restores the
terminal foreground; `bg: none` explicitly clears background, including an
inherited one. They are not color names passed to an upstream color parser.
Unconfigured auto remains palette-only, with no painted backgrounds.
**Notty keeps Markdown colorless even when the theme supplies colors**; glyph and
attribute overrides still apply, and the pager UI is unchanged. Its stock literal
Markdown decorations (such as `**`) remain independent of the bold attribute.

| Role | Shape and scope |
|------|-----------------|
| `headings.h1` through `headings.h6` | Text style, including the base heading prefix. |
| `strong`, `emphasis`, `strikethrough` | Text style for that semantic inline role. |
| `links` | Text style for both link labels and visible URLs; ordinary reference-style links are links, not footnotes. OSC 8 destinations remain complete. |
| `inline_code` | Text style for code and its base padding. |
| `code_block` | **Only `bg`**; never a blanket syntax foreground override. |
| `table.border` | Text style for **top-level table borders only**. Nested tables retain stock layout/borders. |
| `tasks.checked`, `tasks.unchecked` | Text style plus optional `glyph`; applies only to the checkbox, not its separator or task body. |
| `footnotes.reference`, `footnotes.definition` | Text style for semantically validated literal `[^label]` markers, never definition bodies. |
| `callouts` | Shared/per-type controls described below. |

Nesting follows outer-to-inner semantic roles. A child's explicit/base property
shadows the parent; an omitted child property without a base value inherits its
parent. For example, an overridden heading foreground flows into ordinary strong
text, but inline code and links keep their own base colors unless overridden.
An explicit default/none/false takes precedence over both inherited and base
values. Parent styling is restored after each nested role. Stock nested-table
link labels already flatten inner strong/emphasis/code formatting; those inner
role overrides are therefore not represented there, while `links` still styles
the label/URL and stock footer deduplication stays intact. Top-level custom table
links and ordinary links retain nested roles. One pre-existing stock exception
also applies: links inside a strikethrough container flatten to label text without
OSC 8 or a visible URL; an autolink-only strike can disappear. Theme settings do
not change that presentation. Image alt text keeps stock presentation.

Inline backgrounds exclude stock indentation and trailing document padding.
Heading-owned painted padding follows the heading override, including `bg: none`.

Configured checkbox glyphs occupy 1..8 display columns; callout icons and rails
occupy 1..4 (including explicit padding, excluding the icon/title separator).
Each is limited to 128 bytes, must have visible content and cannot contain controls,
newlines or terminal escapes. Combining/ZWJ graphemes and private-use font glyphs
are accepted; measured width and actual font appearance are not identical.

### Code rectangles and footnotes

An explicit code-block background paints the **intrinsic code width**, filling
short/blank interior lines while excluding outer Glamour/document margins and
quote rails. It does not paint to the viewport or wrap code. Syntax foregrounds
and attributes survive token resets. Empty/only-empty code has zero intrinsic
width, so it does not manufacture a visible panel. For painted rectangles only,
each rendered TAB becomes four spaces (not tab-stop alignment), using the existing
cell-display policy. Original lexer input and raw Markdown for copy/edit remain
unchanged. `bg: none` clears backgrounds without adding rectangular padding or
expanding TABs; omission preserves the base's code rendering. Notty never paints.

Validated footnote references default to link-compatible color plus underline;
definition labels use link-label-compatible color plus bold and no underline.
Notty uses only those attributes. One-word definitions are preserved as literal
footnotes rather than accidentally becoming ordinary reference-link definitions.
Code, escaped markers, undefined lookalikes and HTML attributes are not footnote
roles. Repeated markers retain table-cell identity and navigation/history.
The existing conservative provenance mapper may decline ambiguous source/render
counts, unused definitions and quoted/list-prefixed definitions; no arbitrary
bracket matching is used to guess a role.

### Callouts and Nerd Font recipe

Callout enhancement retains its source-provenance boundary: **top-level GFM
alerts** with a standalone `[!TYPE]` first line and supported paragraph/list
contents. Nested alerts and alert quotes containing fences, tables, headings or
other unsupported blocks remain stock. Custom controls work under every selectable
base within that boundary; unconfigured non-auto bases keep their existing stock
callout presentation. Plain blockquotes and body text are not restyled.

`callouts` accepts:

- `preset: unicode|nerd`: omission uses Octicons `    ` and rail `│`;
  `unicode` selects portable icons `ⓘ ✦ ✱ ⚠ ✖` and rail `│`; `nerd` uses the
  same Octicons as the default, with rail `▋`.
- `rail`: shared text style plus `glyph`.
- `title`: shared text style, covering icon and title, not body text.
- `note`, `tip`, `important`, `warning`, `caution`: each accepts `icon`, `rail`
  (same shape as shared rail), and `title` (same text-style shape).

Resolution is built-in per-type color/defaults, preset glyphs, shared controls,
then per-type controls. Enhanced titles stay bold by default; `bold: false`
really disables that. Per-type icons override the preset. Unconfigured auto
callouts and partial themes with no preset inherit the default Octicons. Empty
mappings do not enable enhancement on non-auto bases.

Every enhanced callout uses exactly one icon-to-title separator space, with no
extra automatic reservation. All explicit icon spaces are preserved: `icon: 'i'`
emits `i Note`, `icon: 'i '` emits `i  Note`, and `icon: ' i  '` emits ` i   Note`.
This applies to both presets and custom icons, not rails or task checkboxes.

```yaml
callouts:
  preset: nerd
  rail: {glyph: '▋'}
  title: {bold: false}
  # note: {icon: 'i', title: {fg: 12}}
```

Default and `nerd` icons use one Nerd Fonts family, Octicons:

| Type | Nerd Fonts name | Codepoint |
|------|-----------------|-----------|
| Note | `nf-oct-info` | U+F449 |
| Tip | `nf-oct-light_bulb` | U+F400 |
| Important | `nf-oct-report` | U+F50A |
| Warning | `nf-oct-alert` | U+F421 |
| Caution | `nf-oct-stop` | U+F46E |

These default icons require a compatible Nerd Font or an Octicons-capable symbols
fallback covering all five glyphs. No fonts are bundled or auto-detected. Older
or custom-patched fonts may lack glyphs; optical size, overflow, fallback and
terminal width behavior vary even with identical codepoints.

For portable icons without that font requirement, explicitly select Unicode in
your theme and omit any font-dependent per-type icon overrides:

```yaml
callouts:
  preset: unicode
```

### Renderer boundary

Goldmark and stock Glamour still own Markdown parsing and ordinary layout. A
small render-local adapter annotates existing AST text segments and public string
hooks with nonce-qualified zero-width roles, then composes styles on fresh ANSI.
Top-level table cells strip these markers before measuring/wrapping. Stock nested
tables necessarily carry zero-width markers through their private measurement;
layout-invariance tests cover that exception, and complete-container composition
removes them before cached output, coordinates, picking, search or terminal display.
Malformed/unbalanced markers error rather than leaking or guessing. Raw source and
cached ANSI are never recolored in place. Chroma named styles avoid the shared
`charm` slot; one lock covers registration and all readmd stock rendering reads.

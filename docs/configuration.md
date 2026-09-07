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
Only `--style`, `--no-images`, and `--no-remote-images` override settings;
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

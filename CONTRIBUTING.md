# Contributing

Open an issue before starting a larger feature or architecture change. Small bug
fixes and documentation improvements can go straight to a pull request. Keep
changes focused and add a regression test for behavior changes.

## Setup and checks

Use the Go version declared in [go.mod](go.mod) or a compatible newer patch,
Python 3 for Unix PTY checks, and golangci-lint **v2.13.2** (the CI version).
Install tools using their official instructions; no application config is needed
to build or lint.

```sh
GOWORK=off go mod download
GOWORK=off go mod verify
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -count=1 ./...
GOWORK=off go test -race -count=1 ./...
GOWORK=off golangci-lint config verify
GOWORK=off golangci-lint run
GOWORK=off golangci-lint fmt --diff
```

Use `GOWORK=off golangci-lint fmt` to apply formatting. CI tests on Linux and
macOS; Windows support is not promised. CI combines the full uncached test suite
and race gate in one `GOWORK=off go test -race -count=1 ./...` run.

CLI/config tests must set temporary `HOME` and `XDG_CONFIG_HOME`; never read or
bootstrap a contributor's real config. For manual startup checks:

```sh
scratch=$(mktemp -d)
GOWORK=off go build -o "$scratch/readmd" .
python3 testdata/startup_pty.py "$scratch/readmd"
python3 testdata/file_picker_pty.py "$scratch/readmd"
python3 testdata/image_size_pty.py "$scratch/readmd"
rm -rf "$scratch"
```

The PTY script runs bounded startup/picker checks and creates its own temporary
HOME/XDG directories. The golden corpus in `testdata/corpus/` renders at widths
40/80/120 and checks for no panics, preserved content and table geometry, and no
split graphemes. See [AGENTS.md](AGENTS.md) for architecture constraints, the
development workflow, and test conventions. Use
public dependency APIs rather than forks, preserve raw-source rendering, and
keep grapheme/table geometry tests when changing layout.

Contributions are distributed under the project's GPL-3.0-only license. Preserve
third-party notices and identify the origin and license of any copied code.
Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

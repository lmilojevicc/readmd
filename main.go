package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lmilojevicc/readmd/internal/config"
	"github.com/lmilojevicc/readmd/internal/pager"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "readmd:", err)
		os.Exit(1)
	}
}

const usage = "usage: readmd [--style auto|dark|light|notty] [--theme PATH] [--no-images] [--no-remote-images] [file]"

type cliOpts struct {
	imgs  pager.ImageConfig
	style string
	theme string
	pos   []string
}

func parseArgs(args []string) (cliOpts, error) {
	opts := cliOpts{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--no-images":
			opts.imgs.NoImages = true
		case "--no-remote-images":
			opts.imgs.NoRemote = true
		case "--theme":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "--") {
				return opts, errors.New("--theme requires a path (" + usage + ")")
			}
			opts.theme = args[i]
		case "--style":
			i++
			if i >= len(args) {
				return opts, errors.New("--style requires a value (" + usage + ")")
			}
			opts.style = args[i]
			if err := validateCLIStyle(opts.style); err != nil {
				return opts, err
			}
		default:
			if strings.HasPrefix(a, "--theme=") {
				opts.theme = strings.TrimPrefix(a, "--theme=")
				if strings.TrimSpace(opts.theme) == "" {
					return opts, errors.New("--theme requires a path (" + usage + ")")
				}
				continue
			}
			if strings.HasPrefix(a, "--style=") {
				opts.style = strings.TrimPrefix(a, "--style=")
				if err := validateCLIStyle(opts.style); err != nil {
					return opts, err
				}
				continue
			}
			if strings.HasPrefix(a, "-") && a != "-" {
				return opts, fmt.Errorf("unknown flag: %s (%s)", a, usage)
			}
			opts.pos = append(opts.pos, a)
		}
	}
	return opts, nil
}

func validateCLIStyle(style string) error {
	c := config.Defaults()
	c.Style = style
	return c.Validate()
}

func applyCLI(c config.Config, opts cliOpts) config.Config {
	if opts.style != "" {
		c.Style = opts.style
	}
	if opts.imgs.NoImages {
		c.Images = false
	}
	if opts.imgs.NoRemote {
		c.RemoteImages = false
	}
	return c
}

func run() error {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}
	if len(opts.pos) > 1 {
		return errors.New(usage)
	}

	fi, statErr := os.Stdin.Stat()
	piped := statErr == nil && fi.Mode()&os.ModeCharDevice == 0

	var src []byte
	var name string
	switch {
	case len(opts.pos) == 1:
		name = opts.pos[0]
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		src = b
	case piped:
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		name = "(stdin)"
		src = b
	default:
		if statErr != nil {
			return fmt.Errorf("stat stdin: %w", statErr)
		}
		// Interactive no-argument startup opens the Markdown browser.
	}
	if name != "" && len(src) == 0 {
		return errors.New("empty document")
	}

	in := io.Reader(os.Stdin)
	if piped {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("no terminal available for pager: %w", err)
		}
		defer func() { _ = tty.Close() }() // Read-only TTY; no pending writes.
		in = tty
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if name != "" && name != "(stdin)" {
		name, err = filepath.Abs(name)
		if err != nil {
			return err
		}
	}
	c, theme, err := startupSettings(opts, os.Stderr)
	if err != nil {
		return err
	}
	model, err := pager.NewApplication(string(src), name, cwd, c, theme)
	if err != nil {
		return err
	}
	defer model.Close()

	p := tea.NewProgram(model, tea.WithInput(in), tea.WithFilter(pager.FilterApplicationMessage))
	_, err = p.Run()
	return err
}

func startupModel(source, name string, opts cliOpts, warnings io.Writer) (*pager.Model, error) {
	c, theme, err := startupSettings(opts, warnings)
	if err != nil {
		return nil, err
	}
	model := pager.New(source, name)
	if name != "" && name != "(stdin)" {
		model.SetPath(name)
	}
	model.SetTheme(theme)
	if err := model.Configure(c); err != nil {
		return nil, err
	}

	return model, nil
}

func startupSettings(opts cliOpts, warnings io.Writer) (config.Config, config.Theme, error) {
	c, err := config.Load(warnings)
	if err != nil {
		return c, config.Theme{}, err
	}
	c = applyCLI(c, opts)
	theme, err := config.LoadTheme(c.Theme, opts.theme)
	return c, theme, err
}

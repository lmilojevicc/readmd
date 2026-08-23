package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"readmd/internal/pager"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "readmd:", err)
		os.Exit(1)
	}
}

const usage = "usage: readmd [--style auto|dark|light|notty] [--no-images] [--no-remote-images] [file]"

func run() error {
	var imgs pager.ImageConfig
	var style string
	var pos []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--no-images":
			imgs.NoImages = true
		case "--no-remote-images":
			imgs.NoRemote = true
		case "--style":
			i++
			if i >= len(args) {
				return errors.New("--style requires a value (" + usage + ")")
			}
			style = args[i]
		default:
			if strings.HasPrefix(a, "--style=") {
				style = strings.TrimPrefix(a, "--style=")
				continue
			}
			if strings.HasPrefix(a, "-") && a != "-" {
				return fmt.Errorf("unknown flag: %s (%s)", a, usage)
			}
			pos = append(pos, a)
		}
	}
	if len(pos) > 1 {
		return errors.New(usage)
	}

	fi, statErr := os.Stdin.Stat()
	piped := statErr == nil && fi.Mode()&os.ModeCharDevice == 0

	var src []byte
	var name string
	switch {
	case len(pos) == 1:
		name = pos[0]
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
		return errors.New(usage + " (or pipe markdown on stdin)")
	}
	if len(src) == 0 {
		return errors.New("empty document")
	}

	in := io.Reader(os.Stdin)
	if piped {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("no terminal available for pager: %w", err)
		}
		defer tty.Close()
		in = tty
	}

	model := pager.New(string(src), name)
	if err := model.SetStyle(style); err != nil {
		return err
	}
	if name != "" && name != "(stdin)" {
		model.SetPath(name)
		imgs.DocDir = filepath.Dir(name)
	}
	model.SetImages(imgs)
	defer model.Close()

	p := tea.NewProgram(model, tea.WithInput(in))
	_, err := p.Run()
	return err
}

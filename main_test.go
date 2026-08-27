package main

import (
	"reflect"
	"strings"
	"testing"

	"readmd/internal/pager"
)

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		want    cliOpts
		errPart string
	}{
		{"default nowrap", nil, cliOpts{}, ""},
		{"explicit wrap", []string{"--wrap"}, cliOpts{wrap: true}, ""},
		{"explicit nowrap", []string{"--no-wrap"}, cliOpts{}, ""},
		{"last wins", []string{"--no-wrap", "--wrap"}, cliOpts{wrap: true}, ""},
		{"last wins reversed", []string{"--wrap", "--no-wrap"}, cliOpts{wrap: false}, ""},
		{"with file", []string{"--no-wrap", "doc.md"}, cliOpts{wrap: false, pos: []string{"doc.md"}}, ""},
		{"combined flags", []string{"--style=dark", "--no-images", "--no-remote-images"},
			cliOpts{style: "dark", imgs: pager.ImageConfig{NoImages: true, NoRemote: true}}, ""},
		{"unknown flag", []string{"--bogus"}, cliOpts{}, "unknown flag"},
		{"missing style value", []string{"--style"}, cliOpts{}, "--style requires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgs(tc.args)
			if tc.errPart != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errPart) {
					t.Fatalf("err %v, want containing %q", err, tc.errPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

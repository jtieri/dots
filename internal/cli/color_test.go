package cli

import (
	"path/filepath"
	"testing"

	"github.com/jtieri/dots/internal/iostreams"
	"github.com/jtieri/dots/internal/testenv"
)

func TestColor(t *testing.T) {
	const inSync, inSyncColored = "In sync.\n", "\x1b[32mIn sync.\x1b[0m\n"
	tests := []struct {
		name     string
		args     []string
		terminal bool
		noColor  bool
		want     string
	}{
		{name: "auto, terminal", terminal: true, want: inSyncColored},
		{name: "auto, no terminal", want: inSync},
		{name: "auto, NO_COLOR", terminal: true, noColor: true, want: inSync},
		{name: "always", args: []string{"--color=always"}, want: inSyncColored},
		{name: "always beats NO_COLOR", args: []string{"--color", "always"}, noColor: true, want: inSyncColored},
		{name: "never", args: []string{"--color=never"}, terminal: true, want: inSync},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
			if !tt.noColor {
				t.Setenv("NO_COLOR", "") // empty counts as unset
			}
			t.Setenv("TERM", "xterm-256color")
			streams, _, out, errOut := iostreams.Test()
			streams.OutIsTerminal = tt.terminal
			a := &app{streams: streams, euid: 1000, lockPath: filepath.Join(t.TempDir(), "lock")}
			if code := execute(append([]string{"status"}, tt.args...), a); code != exitOK || out.String() != tt.want {
				t.Errorf("status = %d, %q, stderr %q; want 0, %q", code, out, errOut, tt.want)
			}
		})
	}
}

func TestColorErrors(t *testing.T) {
	testenv.Isolate(t)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	streams, _, _, errOut := iostreams.Test()
	streams.ErrIsTerminal = true
	a := &app{streams: streams, euid: 1000, lockPath: filepath.Join(t.TempDir(), "lock")}
	if code := execute([]string{"help", "nope"}, a); code != exitError ||
		errOut.String() != "\x1b[1;31mdots:\x1b[0m unknown command \"nope\"; dots help lists them\n" {
		t.Errorf("help nope = %d, stderr %q; want the error with dots: in red", code, errOut)
	}

	streams, _, _, errOut = iostreams.Test()
	a = &app{streams: streams, euid: 1000, lockPath: filepath.Join(t.TempDir(), "lock")}
	if code := execute([]string{"status", "--color=yes"}, a); code != exitError ||
		errOut.String() != "dots: --color \"yes\" must be auto, always or never\n" {
		t.Errorf("status --color=yes = %d, stderr %q", code, errOut)
	}
}

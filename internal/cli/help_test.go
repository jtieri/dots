package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jtieri/dots/internal/config"
	"github.com/jtieri/dots/internal/iostreams"
	"github.com/jtieri/dots/internal/testenv"
)

func TestHelp(t *testing.T) {
	testenv.Isolate(t) // nothing set up: help needs no dotfiles
	f := &fixture{Dotfiles: &testenv.Dotfiles{}, t: t}
	code, out, errOut := f.run("", false, "help")
	if code != exitOK || errOut != "" {
		t.Fatalf("help = %d, stderr %q", code, errOut)
	}
	streams, _, _, _ := iostreams.Test()
	root := newRootCmd(&app{streams: streams})
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		if !strings.Contains(out, "  dots "+c.Use+"  ") {
			t.Errorf("the cheat sheet lacks dots %s in its everyday commands", c.Name())
		}
		if !strings.Contains(out, "\n  "+c.Name()+" ") {
			t.Errorf("the cheat sheet does not say what dots %s runs underneath", c.Name())
		}
	}
	for _, s := range config.Settings {
		want := "\n  " + s.Key + " = " + s.DefaultText()
		if s.Env != "" {
			want += "  ($" + s.Env + ")"
		}
		if !strings.Contains(out, want+"\n") {
			t.Errorf("the cheat sheet lacks %q", want)
		}
	}
	for line := range strings.Lines(out) {
		if n := utf8.RuneCountInString(strings.TrimSuffix(line, "\n")); n > 80 {
			t.Errorf("line of %d characters, more than 80: %q", n, line)
		}
	}
}

func TestHelpCommand(t *testing.T) {
	testenv.Isolate(t)
	f := &fixture{Dotfiles: &testenv.Dotfiles{}, t: t}
	code, out, _ := f.run("", false, "help", "sync")
	if code != exitOK || !strings.Contains(out, "The pull only fast-forwards.") || !strings.Contains(out, "--keep-local") {
		t.Errorf("help sync = %d, %q; want sync's help", code, out)
	}
	code, _, errOut := f.run("", false, "help", "nope")
	if want := "dots: unknown command \"nope\"; dots help lists them\n"; code != exitError || errOut != want {
		t.Errorf("help nope = %d, stderr %q; want 2, %q", code, errOut, want)
	}
}

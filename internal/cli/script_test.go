package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/jtieri/dots/internal/iostreams"
	"github.com/jtieri/dots/internal/testenv"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"dots": func() { os.Exit(Main(os.Args[1:], iostreams.System())) },
	})
}

// TestScripts runs the scripts in testdata/script, each in a new home
// directory. testscript starts each script with an environment of its own,
// so nothing from the desktop session, such as DISPLAY or the D-Bus address,
// reaches the programs a script runs.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
		Setup: func(env *testscript.Env) error {
			home := filepath.Join(env.WorkDir, "home")
			for _, kv := range testenv.Vars(home) {
				name, value, _ := strings.Cut(kv, "=")
				env.Setenv(name, value)
			}
			return os.MkdirAll(home, 0o755)
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"dotfiles": cmdDotfiles,
			"exitcode": cmdExitCode,
		},
	})
}

// cmdDotfiles turns $HOME/dotfiles, with the source files under its home
// folder, into a repo with a remote at $WORK/remote.git, then sets up chezmoi
// and applies it without running scripts.
func cmdDotfiles(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: dotfiles")
	}
	home := ts.Getenv("HOME")
	env := append(testenv.Vars(home), "PATH="+ts.Getenv("PATH"))
	if _, err := testenv.InitDotfiles(context.Background(), home, ts.MkAbs("remote.git"), env); err != nil {
		ts.Fatalf("%v", err)
	}
}

// cmdExitCode runs a command like exec, and checks it exits with the given
// code. Its output is kept for stdout and stderr to check, as exec's is.
func cmdExitCode(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exitcode code command [args...]")
	}
	want, err := strconv.Atoi(args[0])
	ts.Check(err)
	err = ts.Exec(args[1], args[2:]...)
	got := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		got = exitErr.ExitCode()
	} else if err != nil {
		ts.Fatalf("%v", err)
	}
	if got != want {
		ts.Fatalf("%s exited %d, want %d", args[1], got, want)
	}
}

package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/jtieri/dot/internal/iostreams"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"dot": func() { os.Exit(Main(os.Args[1:], iostreams.System())) },
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
			env.Setenv("HOME", home)
			env.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			env.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
			env.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
			env.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			env.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			env.Setenv("GIT_AUTHOR_NAME", "Test")
			env.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
			env.Setenv("GIT_COMMITTER_NAME", "Test")
			env.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
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
	repo := filepath.Join(home, "dotfiles")
	remote := ts.MkAbs("remote.git")
	run := func(name string, args ...string) {
		if err := ts.Exec(name, args...); err != nil {
			ts.Fatalf("%s %v: %v\n%s", name, args, err, ts.ReadFile("stderr"))
		}
	}
	ts.Check(os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644))
	run("git", "-C", repo, "init", "--quiet", "--initial-branch=main")
	run("git", "-C", repo, "add", ".")
	run("git", "-C", repo, "commit", "--quiet", "--message=initial")
	run("git", "init", "--quiet", "--bare", "--initial-branch=main", remote)
	run("git", "-C", repo, "remote", "add", "origin", remote)
	run("git", "-C", repo, "push", "--quiet", "--set-upstream", "origin", "main")
	config := filepath.Join(home, ".config", "chezmoi")
	ts.Check(os.MkdirAll(config, 0o755))
	ts.Check(os.WriteFile(filepath.Join(config, "chezmoi.toml"),
		[]byte("sourceDir = \""+repo+"\"\numask = 0o022\n"), 0o644))
	run("chezmoi", "--no-tty", "apply", "--exclude=scripts")
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

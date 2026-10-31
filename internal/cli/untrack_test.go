package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dots/internal/run"
	"github.com/jtieri/dots/internal/testenv"
)

// newUntrackFixture returns a home whose repo holds ~/.a, ~/.b and the
// folder ~/.d, with package lists turned off so that dots push leaves them out.
func newUntrackFixture(t *testing.T) *fixture {
	t.Helper()
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_a":   "a\n",
		"dot_b":   "b\n",
		"dot_d/x": "x\n",
		"dot_d/y": "y\n",
	})
	f := &fixture{Dotfiles: d, t: t}
	testenv.WriteFile(t, f.path(".config/dots/config.toml"), "[push]\npackage_lists = \"\"\n")
	return f
}

// secondMachine sets up another machine from the fixture's remote, as a new
// machine is set up, and returns a fixture for it. Run it before switching to
// it with use.
func (f *fixture) secondMachine() *fixture {
	f.t.Helper()
	home := f.t.TempDir()
	env := append(testenv.Vars(home), "PATH="+os.Getenv("PATH"))
	repo := filepath.Join(home, "dotfiles")
	testenv.WriteFile(f.t, filepath.Join(home, ".config/chezmoi/chezmoi.toml"), "sourceDir = \""+repo+"\"\numask = 0o022\n")
	testenv.WriteFile(f.t, filepath.Join(home, ".config/dots/config.toml"), "[push]\npackage_lists = \"\"\n")
	for _, args := range [][]string{
		{"git", "clone", "--quiet", f.Remote, repo},
		{"chezmoi", "--no-tty", "apply", "--exclude=scripts"},
	} {
		cmd := exec.CommandContext(f.t.Context(), args[0], args[1:]...)
		cmd.Dir, cmd.Env = home, env
		if _, err := run.Output(cmd); err != nil {
			f.t.Fatal(err)
		}
	}
	return &fixture{Dotfiles: &testenv.Dotfiles{
		Home: home, Repo: repo, Source: filepath.Join(repo, "home"), Remote: f.Remote,
	}, t: f.t}
}

// use points the test's environment at f's home, so that dots runs there.
func (f *fixture) use() {
	f.t.Helper()
	for _, kv := range testenv.Vars(f.Home) {
		name, value, _ := strings.Cut(kv, "=")
		f.t.Setenv(name, value)
	}
}

func (f *fixture) exists(name string) bool {
	_, err := os.Lstat(f.path(name))
	return err == nil
}

func TestRm(t *testing.T) {
	a := newUntrackFixture(t)
	b := a.secondMachine()
	testenv.WriteFile(t, b.path(".d/theirs"), "theirs\n")

	a.use()
	code, out, errOut := a.run("", false, "rm", "--yes", "~/.a", "~/.d")
	want := "Will delete from the repo and from every machine, this one included\n  ~/.a\n  ~/.d/x\n  ~/.d/y\n" +
		"Removed ~/.a.\nRemoved ~/.d.\n\nRun dots push to commit and share the change.\n"
	if code != exitOK || out != want {
		t.Errorf("rm = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	if !strings.Contains(errOut, "Copies of this machine's ~/.a are in ") {
		t.Errorf("stderr = %q, want where the copies went", errOut)
	}
	if a.exists(".a") || a.exists(".d") {
		t.Error("want ~/.a and ~/.d gone on this machine")
	}
	if code, _, errOut := a.run("", false, "push", "--yes", "remove a and d"); code != exitOK {
		t.Fatalf("push = %d, stderr %q", code, errOut)
	}

	b.use()
	code, out, errOut = b.run("", false, "sync", "--yes")
	if code != exitOK || !strings.Contains(out, "Will delete\n  ~/.a\n  ~/.d/x\n  ~/.d/y\n") {
		t.Errorf("sync elsewhere = %d, %q, stderr %q; want the files deleted", code, out, errOut)
	}
	if b.exists(".a") || b.exists(".d/x") || !b.exists(".d/theirs") || !b.exists(".b") {
		t.Error("elsewhere: want ~/.a and the tracked files in ~/.d gone, and ~/.d/theirs and ~/.b kept")
	}
	// ~/.d is no longer tracked, so what is left in it is that machine's own.
	if code, out, _ := b.run("", false, "status"); code != exitOK {
		t.Errorf("status elsewhere = %d, %q; want in sync", code, out)
	}
}

func TestForget(t *testing.T) {
	a := newUntrackFixture(t)
	b := a.secondMachine()

	a.use()
	code, out, errOut := a.run("", false, "forget", "--yes", "~/.a", "~/.d")
	want := "Will stop tracking; every machine keeps its own copy\n  ~/.a\n  ~/.d/x\n  ~/.d/y\n" +
		"Stopped tracking ~/.a.\nStopped tracking ~/.d.\n\nRun dots push to commit and share the change.\n"
	if code != exitOK || out != want || errOut != "" {
		t.Errorf("forget = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	if !a.exists(".a") || !a.exists(".d/x") {
		t.Error("want every file kept on this machine")
	}
	if code, _, errOut := a.run("", false, "push", "--yes", "forget a and d"); code != exitOK {
		t.Fatalf("push = %d, stderr %q", code, errOut)
	}
	if code, out, _ := a.run("", false, "status"); code != exitOK {
		t.Errorf("status = %d, %q; want in sync, with the copies not reported", code, out)
	}

	b.use()
	if code, _, errOut := b.run("", false, "sync", "--yes"); code != exitOK {
		t.Fatalf("sync elsewhere = %d, stderr %q", code, errOut)
	}
	if !b.exists(".a") || !b.exists(".d/y") {
		t.Error("elsewhere: want every file kept")
	}
	if code, out, _ := b.run("", false, "status"); code != exitOK {
		t.Errorf("status elsewhere = %d, %q; want in sync", code, out)
	}
}

func TestRmAnswers(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		terminal    bool
		args        []string
		wantCode    int
		wantRemoved bool
		wantErrOut  string
	}{
		{name: "yes", input: "y\n", terminal: true, wantCode: exitOK, wantRemoved: true},
		{name: "enter cancels", input: "\n", terminal: true, wantCode: exitOutOfSync, wantErrOut: "Nothing changed.\n"},
		{
			name: "unknown answer, then no", input: "k\nn\n", terminal: true, wantCode: exitOutOfSync,
			wantErrOut: "Please answer with one of the letters in brackets.",
		},
		{name: "input ends", terminal: true, wantCode: exitError, wantErrOut: "dots: nothing changed: no answer was given\n"},
		{name: "no terminal", wantCode: exitError, wantErrOut: "dots: nothing changed: there is no terminal to ask at; use --yes\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newUntrackFixture(t)
			code, _, errOut := f.run(tt.input, tt.terminal, "rm", "~/.a")
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("rm = %d, stderr %q; want %d and %q", code, errOut, tt.wantCode, tt.wantErrOut)
			}
			if tt.terminal && !strings.Contains(errOut, "Remove it? [y/N] ") {
				t.Errorf("stderr = %q, want the question", errOut)
			}
			if removed := !f.exists(".a"); removed != tt.wantRemoved {
				t.Errorf("~/.a removed: %t, want %t", removed, tt.wantRemoved)
			}
			if !tt.wantRemoved {
				if got := f.Git(t, "status", "--porcelain"); got != "" {
					t.Errorf("repo status = %q, want nothing changed", got)
				}
			}
		})
	}
}

func TestRmRefusalLeavesTheRest(t *testing.T) {
	f := newUntrackFixture(t)
	code, out, errOut := f.run("y\n", true, "forget", "~/.nope", "~/.a", "~/.b")
	if code != exitError || !strings.HasPrefix(errOut, "dots: the repo does not track ~/.nope\n") {
		t.Errorf("forget = %d, stderr %q; want 2 and the untracked file named", code, errOut)
	}
	if !strings.Contains(errOut, "Forget them? [y/N] ") || !strings.Contains(out, "Stopped tracking ~/.a.\nStopped tracking ~/.b.\n") {
		t.Errorf("stdout %q, stderr %q; want the others forgotten", out, errOut)
	}
	if code, _, errOut := f.run("", false, "rm", "~/.nope"); code != exitError || errOut != "dots: the repo does not track ~/.nope\n" {
		t.Errorf("rm of only an untracked file = %d, stderr %q", code, errOut)
	}
}

func TestRmWithoutBackups(t *testing.T) {
	f := newUntrackFixture(t)
	testenv.WriteFile(t, f.path(".config/dots/config.toml"), "[overwrite_backup]\nenabled = false\n[push]\npackage_lists = \"\"\n")
	code, _, errOut := f.run("", false, "rm", "--yes", "~/.a")
	if code != exitOK || errOut != "" {
		t.Errorf("rm = %d, stderr %q; want 0 and no copies", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(f.Home, ".local/state/dots/overwritten")); err == nil {
		t.Error("copies were saved with overwrite_backup.enabled off")
	}
}

func TestForgetEmptyFolder(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_e/.keep": ""})
	f := &fixture{Dotfiles: d, t: t}
	code, out, errOut := f.run("", false, "forget", "--yes", "~/.e")
	if want := "Will stop tracking; every machine keeps its own copy\n  ~/.e\nStopped tracking ~/.e.\n"; code != exitOK || !strings.HasPrefix(out, want) {
		t.Errorf("forget = %d, %q, stderr %q; want it to start with %q", code, out, errOut, want)
	}
}

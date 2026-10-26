package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/testenv"
)

// fixture is a home set up by testenv.NewDotfiles, with helpers for running
// dot in it.
type fixture struct {
	*testenv.Dotfiles
	t *testing.T
}

// newApplyFixture returns a home where applying would update .update, and,
// once changeHere is called, replace a file changed on this machine.
func newApplyFixture(t *testing.T) *fixture {
	t.Helper()
	d := testenv.NewDotfiles(t, map[string]string{"dot_update": "update\n", "dot_here": "here\n"})
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_update"), "update from the repo\n")
	return &fixture{Dotfiles: d, t: t}
}

// changeHere edits ~/.here, so applying would replace a local change.
func (f *fixture) changeHere() {
	testenv.WriteFile(f.t, f.path(".here"), "edited here\n")
}

func (f *fixture) path(name string) string { return filepath.Join(f.Home, name) }

func (f *fixture) read(name string) string {
	f.t.Helper()
	data, err := os.ReadFile(f.path(name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

// run runs dot with args, answering prompts with input. terminal says whether
// stdin and stderr are terminals.
func (f *fixture) run(input string, terminal bool, args ...string) (code int, out, errOut string) {
	f.t.Helper()
	streams, in, stdout, stderr := iostreams.Test()
	in.WriteString(input)
	streams.InIsTerminal, streams.ErrIsTerminal = terminal, terminal
	a := &app{streams: streams, euid: 1000, lockPath: filepath.Join(f.t.TempDir(), "lock")}
	code = execute(args, a)
	return code, stdout.String(), stderr.String()
}

func TestApplyNothingToApply(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	f := &fixture{Dotfiles: d, t: t}
	code, out, _ := f.run("", false, "apply")
	if code != exitOK || out != "Nothing to apply; the home directory matches the repo.\n" {
		t.Errorf("apply = %d, %q; want 0 and nothing to apply", code, out)
	}
}

func TestApplyAnswers(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCode    int
		wantApplied bool
		wantErrOut  string
	}{
		{name: "yes", input: "y\n", wantCode: exitOK, wantApplied: true},
		{name: "yes in full", input: "Yes\n", wantCode: exitOK, wantApplied: true},
		{name: "enter cancels", input: "\n", wantCode: exitOutOfSync, wantErrOut: "Nothing changed in the home directory."},
		{name: "no", input: "n\n", wantCode: exitOutOfSync, wantErrOut: "Nothing changed in the home directory."},
		{name: "unknown answer, then no", input: "k\nn\n", wantCode: exitOutOfSync, wantErrOut: "Please answer with one of the letters in brackets."},
		{name: "input ends", input: "", wantCode: exitError, wantErrOut: "dot: nothing applied: no answer was given"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newApplyFixture(t)
			config := f.path(".config/chezmoi/chezmoi.toml")
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			code, out, errOut := f.run(tt.input, true, "apply")
			if code != tt.wantCode {
				t.Errorf("apply = %d, want %d; stderr %q", code, tt.wantCode, errOut)
			}
			if !strings.HasPrefix(out, "Will update\n  ~/.update\n") {
				t.Errorf("stdout = %q, want the preview first", out)
			}
			if !strings.Contains(errOut, "Apply these changes? [y/d/N] ") || !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("stderr = %q, want the question and %q", errOut, tt.wantErrOut)
			}
			if applied := f.read(".update") == "update from the repo\n"; applied != tt.wantApplied {
				t.Errorf(".update = %q, want applied %t", f.read(".update"), tt.wantApplied)
			}
			if tt.wantApplied && !strings.HasSuffix(out, "Done. The home directory matches the repo.\n") {
				t.Errorf("stdout = %q, want it to end saying the home matches", out)
			}
			// Refreshing the facts left chezmoi's settings as they were.
			if after, err := os.ReadFile(config); err != nil || !bytes.Equal(before, after) {
				t.Errorf("chezmoi.toml changed from %q to %q (%v)", before, after, err)
			}
		})
	}
}

func TestApplyShowsDiff(t *testing.T) {
	f := newApplyFixture(t)
	code, out, _ := f.run("d\ny\n", true, "apply")
	if code != exitOK {
		t.Fatalf("apply = %d, want %d", code, exitOK)
	}
	if !strings.Contains(out, "-update\n+update from the repo\n") {
		t.Errorf("stdout = %q, want the diff", out)
	}
}

func TestApplyLocalChanges(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		args       []string
		env        map[string]string
		wantCode   int
		wantHere   string
		wantBackup bool
		wantOut    string
	}{
		{
			name: "keep", input: "k\n", wantCode: exitOK, wantHere: "edited here\n",
			wantOut: "Done. 1 file still differs from the repo; dot status lists it.\n",
		},
		{
			name: "overwrite", input: "o\n", wantCode: exitOK, wantHere: "here\n", wantBackup: true,
			wantOut: "Done. The home directory matches the repo.\n",
		},
		{
			name: "overwrite without backups", input: "o\n", wantCode: exitOK, wantHere: "here\n",
			env: map[string]string{"DOT_OVERWRITE_BACKUP_ENABLED": "false"},
		},
		{name: "y is not a choice here", input: "y\nn\n", wantCode: exitOutOfSync, wantHere: "edited here\n"},
		{name: "cancel", input: "\n", wantCode: exitOutOfSync, wantHere: "edited here\n"},
		{name: "keep flag", args: []string{"--keep-local"}, wantCode: exitOK, wantHere: "edited here\n"},
		{name: "overwrite flag", args: []string{"--overwrite-local"}, wantCode: exitOK, wantHere: "here\n", wantBackup: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newApplyFixture(t)
			f.changeHere()
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			code, out, errOut := f.run(tt.input, true, append([]string{"apply"}, tt.args...)...)
			if code != tt.wantCode {
				t.Errorf("apply = %d, want %d; stderr %q", code, tt.wantCode, errOut)
			}
			if !strings.Contains(out, "Changed on this machine, would be replaced\n  ~/.here\n") {
				t.Errorf("stdout = %q, want ~/.here listed as changed here", out)
			}
			if got := f.read(".here"); got != tt.wantHere {
				t.Errorf(".here = %q, want %q", got, tt.wantHere)
			}
			if tt.wantCode == exitOK && f.read(".update") != "update from the repo\n" {
				t.Errorf(".update was not applied")
			}
			if !strings.HasSuffix(out, tt.wantOut) {
				t.Errorf("stdout = %q, want it to end with %q", out, tt.wantOut)
			}
			backups, _ := filepath.Glob(f.path(".local/state/dot/overwritten/*/.here"))
			if gotBackup := len(backups) == 1; gotBackup != tt.wantBackup {
				t.Errorf("backups of .here = %v, want one: %t", backups, tt.wantBackup)
			}
			if tt.wantBackup && !strings.Contains(errOut, "Copies of the overwritten files are in "+f.path(".local/state/dot/overwritten/")) {
				t.Errorf("stderr = %q, want it to say where the copies are", errOut)
			}
		})
	}
}

func TestApplyWithoutTerminal(t *testing.T) {
	tests := []struct {
		name       string
		local      bool
		args       []string
		wantCode   int
		wantErrOut string
	}{
		{name: "no flags", wantCode: exitError, wantErrOut: "dot: nothing applied: there is no terminal to ask at; use --yes"},
		{name: "yes", args: []string{"--yes"}, wantCode: exitOK},
		{name: "yes with local changes", local: true, args: []string{"--yes"}, wantCode: exitError, wantErrOut: "add --keep-local or --overwrite-local"},
		{name: "yes and keep", local: true, args: []string{"-y", "--keep-local"}, wantCode: exitOK},
		{name: "both", local: true, args: []string{"--keep-local", "--overwrite-local"}, wantCode: exitError, wantErrOut: "none of the others can be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newApplyFixture(t)
			if tt.local {
				f.changeHere()
			}
			code, _, errOut := f.run("y\n", false, append([]string{"apply"}, tt.args...)...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("apply = %d, stderr %q; want %d and %q", code, errOut, tt.wantCode, tt.wantErrOut)
			}
			if applied := f.read(".update") == "update from the repo\n"; applied != (tt.wantCode == exitOK) {
				t.Errorf("applied = %t, want %t", applied, tt.wantCode == exitOK)
			}
		})
	}
}

func TestApplyPrunesOldBackups(t *testing.T) {
	f := newApplyFixture(t)
	old := f.path(".local/state/dot/overwritten/2020-01-01T00-00-00")
	testenv.WriteFile(t, filepath.Join(old, ".here"), "old\n")
	if code, _, errOut := f.run("", false, "apply", "--yes"); code != exitOK {
		t.Fatalf("apply = %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("%s is still there, want it deleted after 30 days", old)
	}
}

func TestApplyRestartNotices(t *testing.T) {
	f := newApplyFixture(t)
	testenv.WriteFile(t, f.path(".config/dot/config.toml"), `
[[restart_notices]]
program = "dot-test-editor"
files = ["~/.update", "~/.here"]

[[restart_notices]]
program = "dot-test-idle"
files = ["~/.update"]

[[restart_notices]]
program = "dot-test-bar"
files = ["~/.unrelated"]
`)
	testenv.StartProgram(t, "dot-test-editor")
	testenv.StartProgram(t, "dot-test-bar")
	code, _, errOut := f.run("", false, "apply", "--yes")
	if code != exitOK {
		t.Fatalf("apply = %d, stderr %q", code, errOut)
	}
	if want := "dot-test-editor is running; restart it so it uses the new ~/.update.\n"; !strings.HasSuffix(errOut, want) {
		t.Errorf("stderr = %q, want it to end with %q", errOut, want)
	}
	// dot-test-idle is not running, and dot-test-bar's file did not change.
	if strings.Contains(errOut, "dot-test-idle") || strings.Contains(errOut, "dot-test-bar") {
		t.Errorf("stderr = %q, want no notice for the other programs", errOut)
	}
}

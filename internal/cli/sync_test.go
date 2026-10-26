package cli

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/testenv"
)

// newSyncFixture returns a home where another machine has pushed a new
// version of .update that this machine has not pulled.
func newSyncFixture(t *testing.T) *fixture {
	t.Helper()
	d := testenv.NewDotfiles(t, map[string]string{"dot_update": "update\n", "dot_here": "here\n"})
	d.PushFromElsewhere(t, map[string]string{"home/dot_update": "update from elsewhere\n"})
	return &fixture{Dotfiles: d, t: t}
}

// behind returns the number of commits on the remote that the repo has not
// pulled, as of the last fetch.
func (f *fixture) behind() int {
	f.t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(f.Git(f.t, "rev-list", "--count", "HEAD..@{upstream}")))
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestSync(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCode    int
		wantApplied bool
		wantErrOut  string
	}{
		{name: "yes", input: "y\n", wantCode: exitOK, wantApplied: true},
		{name: "cancel keeps the pull", input: "\n", wantCode: exitOutOfSync, wantErrOut: "Nothing changed in the home directory."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			code, out, errOut := f.run(tt.input, true, "sync")
			if code != tt.wantCode {
				t.Errorf("sync = %d, want %d; stderr %q", code, tt.wantCode, errOut)
			}
			if want := "Pulled 1 commit.\n\nWill update\n  ~/.update\n"; !strings.HasPrefix(out, want) {
				t.Errorf("stdout = %q, want it to start with %q", out, want)
			}
			if !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tt.wantErrOut)
			}
			if got := f.behind(); got != 0 {
				t.Errorf("commits not pulled = %d, want 0", got)
			}
			if applied := f.read(".update") == "update from elsewhere\n"; applied != tt.wantApplied {
				t.Errorf(".update = %q, want applied %t", f.read(".update"), tt.wantApplied)
			}
		})
	}
}

func TestSyncNothingToPull(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	f := &fixture{Dotfiles: d, t: t}
	code, out, errOut := f.run("", false, "sync")
	if want := "Nothing to pull.\n\nNothing to apply; the home directory matches the repo.\n"; code != exitOK || out != want {
		t.Errorf("sync = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
}

func TestSyncWithoutTerminal(t *testing.T) {
	tests := []struct {
		name        string
		local       bool
		args        []string
		wantCode    int
		wantApplied bool
		wantErrOut  string
	}{
		{name: "no flags", wantCode: exitError, wantErrOut: "dot: nothing applied: there is no terminal to ask at"},
		{name: "yes", args: []string{"--yes"}, wantCode: exitOK, wantApplied: true},
		{name: "yes with local changes", local: true, args: []string{"--yes"}, wantCode: exitError, wantErrOut: "add --keep-local or --overwrite-local"},
		{name: "keep", local: true, args: []string{"--keep-local"}, wantCode: exitOK, wantApplied: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			if tt.local {
				testenv.WriteFile(t, f.path(".here"), "edited here\n")
			}
			code, _, errOut := f.run("", false, append([]string{"sync"}, tt.args...)...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("sync = %d, stderr %q; want %d and %q", code, errOut, tt.wantCode, tt.wantErrOut)
			}
			// The pull happens before the apply, whatever its outcome.
			if got := f.behind(); got != 0 {
				t.Errorf("commits not pulled = %d, want 0", got)
			}
			if applied := f.read(".update") == "update from elsewhere\n"; applied != tt.wantApplied {
				t.Errorf(".update = %q, want applied %t", f.read(".update"), tt.wantApplied)
			}
			if tt.local && f.read(".here") != "edited here\n" {
				t.Errorf(".here = %q, want the change made here kept", f.read(".here"))
			}
		})
	}
}

func TestSyncStops(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, f *fixture)
		wantBehind int
		wantErrOut string
	}{
		{
			name: "diverged",
			setup: func(t *testing.T, f *fixture) {
				t.Helper()
				testenv.WriteFile(t, filepath.Join(f.Source, "dot_here"), "here from this machine\n")
				f.Git(t, "commit", "--quiet", "--all", "--message=here")
			},
			wantBehind: 1,
			wantErrOut: "dot: nothing was pulled, because this machine and the remote both have new commits " +
				"(1 commit not pushed, 1 commit not pulled); combine them with git",
		},
		{
			name: "uncommitted change in the way",
			setup: func(t *testing.T, f *fixture) {
				t.Helper()
				testenv.WriteFile(t, filepath.Join(f.Source, "dot_update"), "update from this machine\n")
			},
			wantBehind: 1,
			wantErrOut: "dot: nothing was pulled, because it would overwrite uncommitted changes in the repo to ~/.update; " +
				"commit or discard them, then run dot sync again\n",
		},
		{
			name: "detached",
			setup: func(t *testing.T, f *fixture) {
				t.Helper()
				f.Git(t, "checkout", "--quiet", "--detach")
			},
			wantErrOut: "dot: the repo is not on a branch, so there is nothing to pull; switch to main with git switch\n",
		},
		{
			name: "no upstream",
			setup: func(t *testing.T, f *fixture) {
				t.Helper()
				f.Git(t, "checkout", "--quiet", "-b", "local-only")
				t.Setenv("DOT_BRANCH", "local-only")
			},
			wantErrOut: "dot: branch local-only does not track a remote branch, so there is nothing to pull",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			tt.setup(t, f)
			code, out, errOut := f.run("", false, "sync", "--yes")
			if code != exitError || !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("sync = %d, stderr %q; want %d and %q", code, errOut, exitError, tt.wantErrOut)
			}
			if out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
			if tt.wantBehind > 0 {
				if got := f.behind(); got != tt.wantBehind {
					t.Errorf("commits not pulled = %d, want %d", got, tt.wantBehind)
				}
			}
			if f.read(".update") != "update\n" || f.read(".here") != "here\n" {
				t.Errorf(".update = %q, .here = %q; want nothing applied", f.read(".update"), f.read(".here"))
			}
		})
	}
}

func TestSyncOffline(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantApplied bool
	}{
		{name: "apply what is here", input: "y\n", wantApplied: true},
		{name: "cancel", input: "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			f.Git(t, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
			testenv.WriteFile(t, filepath.Join(f.Source, "dot_here"), "here from the repo\n")

			code, out, errOut := f.run(tt.input, true, "sync")
			if code != exitError {
				t.Errorf("sync = %d, want %d", code, exitError)
			}
			if !strings.HasPrefix(errOut, "Could not fetch from the remote: ") ||
				!strings.Contains(errOut, ".\nShowing what the repo already has.\n\n") {
				t.Errorf("stderr = %q, want it to start saying the fetch failed", errOut)
			}
			if want := "dot: nothing was pulled from the remote\n"; !strings.HasSuffix(errOut, want) {
				t.Errorf("stderr = %q, want it to end with %q", errOut, want)
			}
			if want := "Will update\n  ~/.here\n"; !strings.HasPrefix(out, want) {
				t.Errorf("stdout = %q, want it to start with %q", out, want)
			}
			if applied := f.read(".here") == "here from the repo\n"; applied != tt.wantApplied {
				t.Errorf(".here = %q, want applied %t", f.read(".here"), tt.wantApplied)
			}
		})
	}
}

func TestSyncOtherBranch(t *testing.T) {
	f := newSyncFixture(t)
	f.Git(t, "checkout", "--quiet", "-b", "other", "--track", "origin/main")
	code, _, errOut := f.run("", false, "sync", "--yes")
	if code != exitOK || !strings.HasPrefix(errOut, "The repo is on branch other, not main.\n") {
		t.Errorf("sync = %d, stderr %q; want 0 and a warning about the branch", code, errOut)
	}
	if f.read(".update") != "update from elsewhere\n" {
		t.Errorf(".update = %q, want the pulled version", f.read(".update"))
	}
}

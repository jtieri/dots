package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dots/internal/testenv"
)

// newPushFixture returns a home whose repo has an uncommitted change to
// ~/.a, as dots capture leaves it, with package lists turned off unless lists
// is set. pacman, when the lists are on, is a stand-in that lists base.
func newPushFixture(t *testing.T, lists bool) *fixture {
	t.Helper()
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n", "dot_b": "b\n"})
	f := &fixture{Dotfiles: d, t: t}
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a captured\n")
	if !lists {
		testenv.WriteFile(t, f.path(".config/dots/config.toml"), "[push]\npackage_lists = \"\"\n")
		return f
	}
	bin := t.TempDir()
	testenv.WriteFile(t, filepath.Join(bin, "pacman"), "#!/bin/sh\n[ \"$1\" = -Qqen ] && echo base\nexit 0\n")
	chmodFile(t, filepath.Join(bin, "pacman"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

// remoteLog returns the subjects of the remote's commits, newest first.
func (f *fixture) remoteLog() string {
	f.t.Helper()
	return testenv.Run(f.t, f.Remote, "git", "log", "--format=%s")
}

// host returns the machine's name as chezmoi sees it.
func (f *fixture) host() string {
	f.t.Helper()
	return strings.TrimSpace(f.Chezmoi(f.t, "execute-template", "{{ .chezmoi.hostname }}"))
}

func TestPush(t *testing.T) {
	f := newPushFixture(t, true)
	testenv.WriteFile(t, filepath.Join(f.Source, "dot_new"), "new\n")
	code, out, errOut := f.run("", false, "push", "--yes")
	if code != exitOK {
		t.Fatalf("push = %d, stderr %q", code, errOut)
	}
	host := f.host()
	wantPreview := "Will commit changes to\n  ~/.a\n" +
		"Will add to the repo\n  ~/.new\n  scripts/pkglists/" + host + "/aurpkglist.txt\n  scripts/pkglists/" + host + "/pkglist.txt\n" +
		"\nCommitted: update from " + host + ", "
	if !strings.HasPrefix(out, wantPreview) || !strings.HasSuffix(out, "\nPushed 1 commit.\n") {
		t.Errorf("stdout = %q, want the preview, the commit and the push", out)
	}
	if got := f.remoteLog(); !strings.HasPrefix(got, "update from "+host+", ") {
		t.Errorf("remote log = %q, want the commit pushed", got)
	}
	if got := f.Git(t, "status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want everything committed", got)
	}
	if got := testenv.ReadFile(t, filepath.Join(f.Repo, "scripts/pkglists", host, "pkglist.txt")); got != "base\n" {
		t.Errorf("pkglist.txt = %q, want the stand-in's list", got)
	}
}

func TestPushPreview(t *testing.T) {
	f := newPushFixture(t, false)
	if err := os.Rename(filepath.Join(f.Source, "dot_b"), filepath.Join(f.Source, "private_dot_b")); err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, filepath.Join(f.Source, "dot_c"), "c\n")
	f.Git(t, "add", "home/dot_c")
	f.Git(t, "commit", "--quiet", "--message=c")
	f.Git(t, "mv", "home/dot_c", "home/dot_d")
	code, out, errOut := f.run("", false, "push", "--yes")
	want := "Will commit changes to\n  ~/.a\n  ~/.b, now private\n" +
		"Will rename in the repo\n  ~/.d, from ~/.c\n" +
		"Will also push 1 commit made earlier.\n"
	if code != exitOK || !strings.HasPrefix(out, want) {
		t.Errorf("push = %d, %q, stderr %q; want it to start with %q", code, out, errOut, want)
	}
}

func TestPushMessage(t *testing.T) {
	f := newPushFixture(t, false)
	if code, _, errOut := f.run("", false, "push", "--yes", "Fix the A file"); code != exitOK {
		t.Fatalf("push = %d, stderr %q", code, errOut)
	}
	if got := f.remoteLog(); !strings.HasPrefix(got, "Fix the A file\n") {
		t.Errorf("remote log = %q, want the message given", got)
	}
}

func TestPushAnswers(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCode    int
		wantMessage string // the pushed commit's subject, or "" for none
		wantOut     string
		wantErrOut  string
	}{
		{name: "default message", input: "\ny\n", wantCode: exitOK, wantMessage: "update from "},
		{name: "message typed", input: "Tidy Zed's Settings\ny\n", wantCode: exitOK, wantMessage: "Tidy Zed's Settings"},
		{name: "diff, then yes", input: "\nd\ny\n", wantCode: exitOK, wantMessage: "update from ", wantOut: "+a captured\n"},
		{name: "unknown answer, then no", input: "\nx\nn\n", wantCode: exitOutOfSync, wantErrOut: "Please answer with one of the letters in brackets."},
		{name: "enter cancels", input: "\n\n", wantCode: exitOutOfSync, wantErrOut: "Nothing was committed or pushed.\n"},
		{name: "input ends", input: "", wantCode: exitError, wantErrOut: "dots: nothing committed or pushed: no answer was given\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPushFixture(t, true)
			code, out, errOut := f.run(tt.input, true, "push")
			if code != tt.wantCode {
				t.Errorf("push = %d, want %d; stderr %q", code, tt.wantCode, errOut)
			}
			if !strings.Contains(errOut, "Commit message [update from "+f.host()+", ") {
				t.Errorf("stderr = %q, want the message asked for with the default", errOut)
			}
			if !strings.Contains(out, tt.wantOut) || !strings.Contains(errOut, tt.wantErrOut) {
				t.Errorf("stdout %q, stderr %q; want them to contain %q and %q", out, errOut, tt.wantOut, tt.wantErrOut)
			}
			log := f.remoteLog()
			if pushed := !strings.HasPrefix(log, "initial\n"); pushed != (tt.wantMessage != "") ||
				!strings.HasPrefix(log, tt.wantMessage) {
				t.Errorf("remote log = %q, want the newest commit to start %q", log, tt.wantMessage)
			}
			if tt.wantMessage == "" {
				// Nothing committed, and the package lists put back.
				if got := f.Git(t, "status", "--porcelain"); got != " M home/dot_a\n" {
					t.Errorf("status = %q, want only the captured change", got)
				}
			}
		})
	}
}

func TestPushWithoutTerminal(t *testing.T) {
	f := newPushFixture(t, true)
	code, _, errOut := f.run("", false, "push")
	if want := "dots: nothing committed or pushed: there is no terminal to ask at; use --yes\n"; code != exitError || !strings.HasSuffix(errOut, want) {
		t.Errorf("push = %d, stderr %q; want 2 and %q", code, errOut, want)
	}
	if got := f.Git(t, "status", "--porcelain"); got != " M home/dot_a\n" {
		t.Errorf("status = %q, want the package lists put back", got)
	}
}

func TestPushNothing(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	f := &fixture{Dotfiles: d, t: t}
	testenv.WriteFile(t, f.path(".config/dots/config.toml"), "[push]\npackage_lists = \"\"\n")
	code, out, errOut := f.run("", false, "push")
	if code != exitOK || out != "Nothing to push.\n" || errOut != "" {
		t.Errorf("push = %d, %q, stderr %q; want 0 and nothing to push", code, out, errOut)
	}
}

func TestPushUncaptured(t *testing.T) {
	f := newPushFixture(t, false)
	testenv.WriteFile(t, f.path(".b"), "b edited here\n")
	code, _, errOut := f.run("", false, "push", "--yes")
	want := "Changed on this machine but not captured, so left out; dots capture copies it into the repo\n  ~/.b\n\n"
	if code != exitOK || errOut != want {
		t.Errorf("push = %d, stderr %q; want 0, %q", code, errOut, want)
	}
}

func TestPushEarlierCommits(t *testing.T) {
	f := newPushFixture(t, false)
	f.Git(t, "commit", "--quiet", "--all", "--message=made earlier")
	code, out, errOut := f.run("d\ny\n", true, "push")
	if want := "Will push 1 commit made earlier.\nPushed 1 commit.\n"; code != exitOK || out != want {
		t.Errorf("push = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	if !strings.Contains(errOut, "Push? [y/N] Please answer with one of the letters in brackets.\nPush? [y/N] ") ||
		strings.Contains(errOut, "Commit message") {
		t.Errorf("stderr = %q, want only a yes or no to push", errOut)
	}

	f.Git(t, "commit", "--quiet", "--allow-empty", "--message=earlier again")
	testenv.WriteFile(t, filepath.Join(f.Source, "dot_b"), "b captured\n")
	code, out, _ = f.run("", false, "push", "--yes", "now")
	if code != exitOK || !strings.Contains(out, "Will also push 1 commit made earlier.\n") || !strings.HasSuffix(out, "Pushed 2 commits.\n") {
		t.Errorf("push = %d, %q; want both commits pushed", code, out)
	}
}

func TestPushStops(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(f *fixture)
		wantErrOut string
	}{
		{
			name:  "behind",
			setup: func(f *fixture) { f.PushFromElsewhere(f.t, map[string]string{"home/dot_c": "c\n"}) },
			wantErrOut: "dots: nothing was committed, because the remote has 1 commit this machine has not pulled; " +
				"run dots sync, then dots push again\n",
		},
		{
			name:       "detached",
			setup:      func(f *fixture) { f.Git(f.t, "checkout", "--quiet", "--detach") },
			wantErrOut: "dots: the repo is not on a branch, so there is nothing to push; switch to main with git switch\n",
		},
		{
			name: "no upstream",
			setup: func(f *fixture) {
				f.Git(f.t, "checkout", "--quiet", "-b", "local-only")
				f.t.Setenv("DOTS_BRANCH", "local-only")
			},
			wantErrOut: "dots: branch local-only does not track a remote branch, so there is nothing to push to; " +
				"set one with git push --set-upstream origin local-only\n",
		},
		{
			name:       "protected file in the repo",
			setup:      func(f *fixture) { testenv.WriteFile(f.t, filepath.Join(f.Source, "dot_ssh/private_id_rsa"), "key\n") },
			wantErrOut: "dots: the repo has an uncommitted copy of ~/.ssh/id_rsa, which capture.protected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPushFixture(t, false)
			tt.setup(f)
			code, out, errOut := f.run("", false, "push", "--yes")
			if code != exitError || !strings.Contains(errOut, tt.wantErrOut) || out != "" {
				t.Errorf("push = %d, %q, stderr %q; want 2 and %q", code, out, errOut, tt.wantErrOut)
			}
			if got := f.Git(t, "log", "-1", "--format=%s"); got != "initial\n" {
				t.Errorf("last commit = %q, want nothing committed", got)
			}
		})
	}
}

func TestPushOffline(t *testing.T) {
	f := newPushFixture(t, false)
	f.Git(t, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	code, out, errOut := f.run("y\n", true, "push", "offline")
	want := "dots: committed, but nothing was pushed, because the remote could not be reached; the next dots push sends it\n"
	if code != exitError || !strings.HasPrefix(errOut, "Could not fetch from the remote: ") || !strings.HasSuffix(errOut, want) {
		t.Errorf("push = %d, stderr %q; want 2 and %q", code, errOut, want)
	}
	if !strings.Contains(errOut, "Commit without pushing? [y/d/N] ") || !strings.Contains(out, "Committed: offline\n") {
		t.Errorf("stdout %q, stderr %q; want a commit without a push", out, errOut)
	}

	code, _, errOut = f.run("", false, "push", "--yes")
	if want := "dots: nothing was pushed, because the remote could not be reached\n"; code != exitError || !strings.HasSuffix(errOut, want) {
		t.Errorf("push of earlier commits offline = %d, stderr %q; want 2 and %q", code, errOut, want)
	}

	f.Git(t, "remote", "set-url", "origin", f.Remote)
	if code, out, _ := f.run("", false, "push", "--yes"); code != exitOK || out != "Will push 1 commit made earlier.\nPushed 1 commit.\n" {
		t.Errorf("push once online = %d, %q; want the commit pushed", code, out)
	}
}

func TestPushFails(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(f *fixture)
		wantErrOut string
	}{
		{
			// Pushing to another repo that has moved on, as when another
			// machine pushes between the fetch and the push.
			name: "rejected",
			setup: func(f *fixture) {
				ahead := filepath.Join(f.t.TempDir(), "ahead.git")
				testenv.Run(f.t, f.Home, "git", "clone", "--quiet", "--bare", f.Remote, ahead)
				other := filepath.Join(f.t.TempDir(), "other")
				testenv.Run(f.t, f.Home, "git", "clone", "--quiet", ahead, other)
				testenv.WriteFile(f.t, filepath.Join(other, "home/dot_c"), "c\n")
				testenv.Run(f.t, other, "git", "add", ".")
				testenv.Run(f.t, other, "git", "commit", "--quiet", "--message=elsewhere")
				testenv.Run(f.t, other, "git", "push", "--quiet")
				f.Git(f.t, "config", "remote.origin.pushurl", ahead)
			},
			wantErrOut: "dots: committed, but nothing was pushed, because the remote has new commits; run dots sync, then dots push again\n",
		},
		{
			name: "turned down",
			setup: func(f *fixture) {
				hook := filepath.Join(f.Remote, "hooks/pre-receive")
				testenv.WriteFile(f.t, hook, "#!/bin/sh\necho 'no pushes today' >&2\nexit 1\n")
				chmodFile(f.t, hook, 0o755)
			},
			wantErrOut: "dots: committed, but nothing was pushed: remote: no pushes today; dots push tries again\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPushFixture(t, false)
			tt.setup(f)
			code, out, errOut := f.run("", false, "push", "--yes", "this one")
			if code != exitError || errOut != tt.wantErrOut {
				t.Errorf("push = %d, stderr %q; want 2, %q", code, errOut, tt.wantErrOut)
			}
			if !strings.HasSuffix(out, "Committed: this one\n") {
				t.Errorf("stdout = %q, want the commit reported", out)
			}
			if got := f.Git(t, "log", "-1", "--format=%s"); got != "this one\n" {
				t.Errorf("last commit = %q, want the commit kept", got)
			}
		})
	}
}

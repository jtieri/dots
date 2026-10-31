package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dots/internal/testenv"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    []Change
		wantErr bool
	}{
		{name: "empty", out: ""},
		{
			name: "every kind of entry",
			out: " M home/dot_bashrc\x00" +
				"?? home/dot_config/new file\x00" +
				" D home/dot_profile\x00" +
				"R  home/private_dot_x\x00home/dot_x\x00" +
				"AM scripts/setup.sh\x00",
			want: []Change{
				{' ', 'M', "home/dot_bashrc", ""},
				{'?', '?', "home/dot_config/new file", ""},
				{' ', 'D', "home/dot_profile", ""},
				{'R', ' ', "home/private_dot_x", "home/dot_x"},
				{'A', 'M', "scripts/setup.sh", ""},
			},
		},
		{name: "rename without its old path", out: "R  new\x00", wantErr: true},
		{name: "malformed", out: "M\x00", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus([]byte(tt.out))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseStatus() error = %v, wantErr %t", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseStatus() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRepo(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_bashrc":  "bashrc\n",
		"dot_profile": "profile\n",
		"dot_x":       "x\n",
	})
	ctx := t.Context()
	r := &Repo{Dir: d.Repo}

	t.Run("Toplevel", func(t *testing.T) {
		got, err := Toplevel(ctx, d.Source)
		if err != nil || got != d.Repo {
			t.Errorf("Toplevel() = %q, %v; want %q", got, err, d.Repo)
		}
	})

	t.Run("Status", func(t *testing.T) {
		if got, err := r.Status(ctx); err != nil || got != nil {
			t.Fatalf("Status() of a clean repo = %v, %v; want nil, nil", got, err)
		}
		testenv.WriteFile(t, filepath.Join(d.Source, "dot_bashrc"), "edited\n")
		testenv.WriteFile(t, filepath.Join(d.Source, "dot_config/ünï code"), "new\n")
		if err := os.Remove(filepath.Join(d.Source, "dot_profile")); err != nil {
			t.Fatal(err)
		}
		d.Git(t, "mv", "home/dot_x", "home/private_dot_x")
		got, err := r.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []Change{
			{' ', 'M', "home/dot_bashrc", ""},
			{' ', 'D', "home/dot_profile", ""},
			{'R', ' ', "home/private_dot_x", "home/dot_x"},
			{'?', '?', "home/dot_config/ünï code", ""},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Status() mismatch (-want +got):\n%s", diff)
		}
		d.Git(t, "reset", "--quiet", "--hard")
		d.Git(t, "clean", "--quiet", "-d", "--force")
	})

	t.Run("Branch", func(t *testing.T) {
		if got, err := r.Branch(ctx); err != nil || got != "main" {
			t.Errorf("Branch() = %q, %v; want main", got, err)
		}
		d.Git(t, "checkout", "--quiet", "--detach")
		if got, err := r.Branch(ctx); err != nil || got != "" {
			t.Errorf("Branch() when detached = %q, %v; want empty", got, err)
		}
		d.Git(t, "checkout", "--quiet", "main")
	})

	t.Run("fetch and compare with the upstream", func(t *testing.T) {
		if got, err := r.LastFetch(ctx); err != nil || !got.IsZero() {
			t.Fatalf("LastFetch() before any fetch = %v, %v; want the zero time", got, err)
		}
		assertAheadBehind(t, r, 0, 0)

		testenv.WriteFile(t, filepath.Join(d.Source, "dot_bashrc"), "local\n")
		d.Git(t, "commit", "--quiet", "--all", "--message=local")
		assertAheadBehind(t, r, 1, 0)

		d.PushFromElsewhere(t, map[string]string{"home/dot_profile": "remote\n"})
		assertAheadBehind(t, r, 1, 0) // not fetched yet

		before := time.Now().Add(-time.Second)
		if err := r.Fetch(ctx, time.Minute); err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}
		assertAheadBehind(t, r, 1, 1)
		if got, err := r.LastFetch(ctx); err != nil || got.Before(before) {
			t.Errorf("LastFetch() after a fetch = %v, %v; want after %v", got, err, before)
		}
	})

	t.Run("no upstream", func(t *testing.T) {
		d.Git(t, "checkout", "--quiet", "-b", "local-only")
		defer d.Git(t, "checkout", "--quiet", "main")
		if _, _, err := r.AheadBehind(ctx); !errors.Is(err, ErrNoUpstream) {
			t.Errorf("AheadBehind() error = %v, want ErrNoUpstream", err)
		}
	})

	t.Run("fetch from a missing remote fails", func(t *testing.T) {
		// A remote that turns the push down is not ahead, so syncing would not help.
		d.Git(t, "fetch", "--quiet")
		d.Git(t, "rebase", "--quiet", "@{upstream}")
		hook := filepath.Join(d.Remote, "hooks", "pre-receive")
		testenv.WriteFile(t, hook, "#!/bin/sh\nexit 1\n")
		if err := os.Chmod(hook, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := r.Push(ctx); err == nil || errors.Is(err, ErrRejected) {
			t.Errorf("Push() turned down by the remote: error = %v, want one other than ErrRejected", err)
		}

		d.Git(t, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
		defer d.Git(t, "remote", "set-url", "origin", d.Remote)
		if err := r.Fetch(ctx, time.Minute); err == nil {
			t.Error("Fetch() error = nil, want an error")
		}
	})

	t.Run("fetch gives up at the time limit", func(t *testing.T) {
		// An ssh that never answers. It records its process ID, so the test
		// can check it was killed along with git rather than left running.
		pidFile := filepath.Join(t.TempDir(), "ssh.pid")
		d.Git(t, "remote", "set-url", "origin", "ssh://git@example.invalid/dotfiles.git")
		defer d.Git(t, "remote", "set-url", "origin", d.Remote)
		t.Setenv("GIT_SSH_COMMAND", "echo $$ >"+pidFile+"; exec sleep 30 #")
		start := time.Now()
		err := r.Fetch(ctx, 500*time.Millisecond)
		if want := "no answer from the remote within 500ms"; err == nil || err.Error() != want {
			t.Errorf("Fetch() error = %v, want %q", err, want)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("Fetch() returned after %v, want soon after the deadline", elapsed)
		}
		data, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatalf("the ssh stand-in never ran: %v", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		for range 50 {
			if !testenv.Running(pid) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Errorf("ssh (pid %d) is still running after Fetch() returned", pid)
	})
}

func assertAheadBehind(t *testing.T, r *Repo, wantAhead, wantBehind int) {
	t.Helper()
	ahead, behind, err := r.AheadBehind(t.Context())
	if err != nil || ahead != wantAhead || behind != wantBehind {
		t.Errorf("AheadBehind() = %d, %d, %v; want %d, %d", ahead, behind, err, wantAhead, wantBehind)
	}
}

func TestDiffContents(t *testing.T) {
	testenv.Isolate(t)
	ctx := t.Context()
	got, err := DiffContents(ctx, "home/dot_config/a b", []byte("one\ntwo\n"), []byte("one\n2\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"diff --git a/home/dot_config/a b b/home/dot_config/a b\n", "--- a/home/dot_config/a b\t\n", "-two\n+2\n"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("DiffContents() = %q, want it to contain %q", got, want)
		}
	}
	if got, err := DiffContents(ctx, "x", []byte("same\n"), []byte("same\n")); err != nil || len(got) != 0 {
		t.Errorf("DiffContents() of the same contents = %q, %v; want nothing", got, err)
	}
}

func TestFastForward(t *testing.T) {
	tests := []struct {
		name string
		// setup changes the repo, its remote, or both, before the fetch.
		setup     func(t *testing.T, d *testenv.Dotfiles)
		want      int
		wantAhead int
	}{
		{
			name:  "up to date",
			setup: func(*testing.T, *testenv.Dotfiles) {},
		},
		{
			name: "behind",
			setup: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				d.PushFromElsewhere(t, map[string]string{"home/dot_a": "a from elsewhere\n"})
				d.PushFromElsewhere(t, map[string]string{"home/dot_new": "new\n"})
			},
			want: 2,
		},
		{
			name: "ahead only",
			setup: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a from here\n")
				d.Git(t, "commit", "--quiet", "--all", "--message=here")
			},
			wantAhead: 1,
		},
		{
			name: "file deleted here and upstream",
			setup: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				d.PushFromElsewhere(t, map[string]string{"home/dot_a": ""})
				if err := os.Remove(filepath.Join(d.Source, "dot_a")); err != nil {
					t.Fatal(err)
				}
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n", "dot_b": "b\n"})
			tt.setup(t, d)
			// An uncommitted change the upstream leaves alone survives.
			testenv.WriteFile(t, filepath.Join(d.Source, "dot_b"), "b edited here\n")
			d.Git(t, "fetch", "--quiet")
			r := &Repo{Dir: d.Repo}

			got, err := r.FastForward(t.Context())
			if err != nil || got != tt.want {
				t.Fatalf("FastForward() = %d, %v; want %d, nil", got, err, tt.want)
			}
			assertAheadBehind(t, r, tt.wantAhead, 0)
			if data, err := os.ReadFile(filepath.Join(d.Source, "dot_b")); err != nil || string(data) != "b edited here\n" {
				t.Errorf("uncommitted change after FastForward() = %q, %v; want it kept", data, err)
			}
			if got, want := d.Git(t, "status", "--porcelain"), " M home/dot_b\n"; got != want {
				t.Errorf("git status after FastForward() = %q, want %q", got, want)
			}
		})
	}
}

func TestFastForwardDiverged(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n", "dot_b": "b\n"})
	d.PushFromElsewhere(t, map[string]string{"home/dot_a": "a from elsewhere\n"})
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_b"), "b from here\n")
	d.Git(t, "commit", "--quiet", "--all", "--message=here")
	d.Git(t, "fetch", "--quiet")
	r := &Repo{Dir: d.Repo}

	_, err := r.FastForward(t.Context())
	if divErr, ok := errors.AsType[*DivergedError](err); !ok || *divErr != (DivergedError{Ahead: 1, Behind: 1}) {
		t.Fatalf("FastForward() error = %v, want a *DivergedError with 1 and 1", err)
	}
	if want := "the branch and its upstream have diverged: 1 commit only on the branch, 1 only on the upstream"; err.Error() != want {
		t.Errorf("FastForward() error = %q, want %q", err, want)
	}
	assertAheadBehind(t, r, 1, 1)
}

func TestFastForwardBlocked(t *testing.T) {
	tests := []struct {
		name string
		// change makes an uncommitted change that the upstream would
		// overwrite. The upstream changes home/dot_a, adds home/dot_new and
		// renames home/dot_r.
		change    func(t *testing.T, d *testenv.Dotfiles)
		wantPaths []string
	}{
		{
			name: "edited",
			change: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a edited here\n")
			},
			wantPaths: []string{"home/dot_a"},
		},
		{
			name: "untracked",
			change: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				testenv.WriteFile(t, filepath.Join(d.Source, "dot_new"), "new here\n")
			},
			wantPaths: []string{"home/dot_new"},
		},
		{
			name: "renamed",
			change: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				d.Git(t, "mv", "home/dot_a", "home/private_dot_a")
			},
			wantPaths: []string{"home/dot_a"},
		},
		{
			name: "edited where the upstream renames it",
			change: func(t *testing.T, d *testenv.Dotfiles) {
				t.Helper()
				testenv.WriteFile(t, filepath.Join(d.Source, "dot_r"), "r edited here\n")
			},
			wantPaths: []string{"home/dot_r"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n", "dot_r": "r\n"})
			d.PushFromElsewhere(t, map[string]string{
				"home/dot_a":   "a from elsewhere\n",
				"home/dot_new": "new\n",
				"home/dot_r":   "",
				"home/dot_r2":  "r\n",
			})
			d.Git(t, "fetch", "--quiet")
			tt.change(t, d)
			before := d.Git(t, "status", "--porcelain")
			r := &Repo{Dir: d.Repo}

			_, err := r.FastForward(t.Context())
			blockedErr, ok := errors.AsType[*BlockedError](err)
			if !ok {
				t.Fatalf("FastForward() error = %v, want a *BlockedError", err)
			}
			if diff := cmp.Diff(tt.wantPaths, blockedErr.Paths); diff != "" {
				t.Errorf("BlockedError.Paths mismatch (-want +got):\n%s", diff)
			}
			assertAheadBehind(t, r, 0, 1)
			if after := d.Git(t, "status", "--porcelain"); after != before {
				t.Errorf("uncommitted changes after FastForward() = %q, want them unchanged: %q", after, before)
			}
		})
	}
}

func TestFastForwardNoUpstream(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	d.Git(t, "checkout", "--quiet", "-b", "local-only")
	r := &Repo{Dir: d.Repo}
	if _, err := r.FastForward(t.Context()); !errors.Is(err, ErrNoUpstream) {
		t.Errorf("FastForward() error = %v, want ErrNoUpstream", err)
	}
}

// TestFastForwardInterrupt checks that when ctx is done, the merge is
// interrupted as Ctrl-C would rather than killed, so git can clean up.
func TestFastForwardInterrupt(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	d.PushFromElsewhere(t, map[string]string{"home/dot_a": "a from elsewhere\n"})
	d.Git(t, "fetch", "--quiet")

	// A git that hangs in merge until interrupted, and records that it was.
	dir := t.TempDir()
	pidFile, mark := filepath.Join(dir, "merge.pid"), filepath.Join(dir, "interrupted")
	program := filepath.Join(dir, "git")
	testenv.WriteFile(t, program, "#!/bin/sh\n"+
		"case \" $* \" in\n"+
		"*\" merge \"*)\n"+
		"  trap 'kill $!; touch "+mark+"; exit 130' INT\n"+
		"  sleep 30 &\n"+
		"  echo $$ >"+pidFile+"\n"+
		"  wait\n"+
		"  exit 0 ;;\n"+
		"esac\n"+
		"exec git \"$@\"\n")
	if err := os.Chmod(program, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Repo{Dir: d.Repo, Program: program}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error)
	go func() {
		_, err := r.FastForward(ctx)
		done <- err
	}()
	waitForFile(t, pidFile)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("FastForward() error = nil, want an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FastForward() did not return after ctx was canceled")
	}
	if _, err := os.Stat(mark); err != nil {
		t.Errorf("git was not interrupted: %v", err)
	}
}

// waitForFile waits for a line to be written to name.
func waitForFile(t *testing.T, name string) {
	t.Helper()
	for range 500 {
		if data, err := os.ReadFile(name); err == nil && strings.HasSuffix(string(data), "\n") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("nothing written to %s", name)
}

func TestCommit(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n", "dot_b": "b\n", "dot_gone": "gone\n", "dot_old": "old\n"})
	r := &Repo{Dir: d.Repo}
	ctx := t.Context()
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "edited\n")
	testenv.WriteFile(t, filepath.Join(d.Source, "st*r: a b"), "new\n")
	testenv.WriteFile(t, filepath.Join(d.Source, "stxr: a b"), "not named\n")
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_b"), "left uncommitted\n")
	if err := os.Remove(filepath.Join(d.Source, "dot_gone")); err != nil {
		t.Fatal(err)
	}
	d.Git(t, "add", "home/dot_b") // staged, but not named
	d.Git(t, "mv", "home/dot_old", "home/dot_renamed")

	paths := []string{"home/dot_a", "home/st*r: a b", "home/dot_gone", "home/dot_renamed", "home/dot_old"}
	if err := r.Commit(ctx, paths, "update from here"); err != nil {
		t.Fatal(err)
	}
	if got := d.Git(t, "log", "-1", "--format=%s"); got != "update from here\n" {
		t.Errorf("commit message = %q", got)
	}
	got := d.Git(t, "show", "--name-status", "--format=", "HEAD")
	want := "M\thome/dot_a\nD\thome/dot_gone\nR100\thome/dot_old\thome/dot_renamed\nA\thome/st*r: a b\n"
	if got != want {
		t.Errorf("committed:\n%s\nwant\n%s", got, want)
	}
	if got := d.Git(t, "status", "--porcelain"); got != "M  home/dot_b\n?? \"home/stxr: a b\"\n" {
		t.Errorf("left uncommitted: %q, want the files not named", got)
	}
	if err := r.Commit(ctx, nil, "x"); err == nil {
		t.Error("Commit() with no paths: error = nil, want one")
	}
}

func TestStagedDiff(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n", "dot_x": "x\n", "dot_a2": "a2\n"})
	r := &Repo{Dir: d.Repo}
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "edited\n")
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_new"), "new\n")
	if err := os.Rename(filepath.Join(d.Source, "dot_x"), filepath.Join(d.Source, "private_dot_x")); err != nil {
		t.Fatal(err)
	}
	d.Git(t, "mv", "home/dot_a2", "home/dot_b2")
	before := d.Git(t, "status", "--porcelain")
	got, err := r.StagedDiff(t.Context(), []string{"home/dot_a", "home/dot_new", "home/dot_x", "home/private_dot_x", "home/dot_b2", "home/dot_a2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-a\n+edited\n", "new file mode 100644", "+new\n", "rename from home/dot_x\nrename to home/private_dot_x\n", "rename from home/dot_a2\nrename to home/dot_b2\n"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("StagedDiff() = %q, want it to contain %q", got, want)
		}
	}
	if after := d.Git(t, "status", "--porcelain"); after != before {
		t.Errorf("status after StagedDiff() = %q, want it unchanged from %q", after, before)
	}
}

func TestPush(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	r := &Repo{Dir: d.Repo}
	ctx := t.Context()
	commit := func(data string) {
		t.Helper()
		testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), data)
		if err := r.Commit(ctx, []string{"home/dot_a"}, data); err != nil {
			t.Fatal(err)
		}
	}

	commit("pushed\n")
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if got := testenv.Run(t, d.Remote, "git", "log", "-1", "--format=%s"); got != "pushed\n" {
		t.Errorf("remote's last commit = %q, want the pushed one", got)
	}

	// A branch whose upstream has another name, which git's default
	// push.default refuses to push.
	d.Git(t, "push", "--quiet", "origin", "main:trunk")
	d.Git(t, "checkout", "--quiet", "-b", "local", "--track", "origin/trunk")
	commit("to trunk\n")
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if got := testenv.Run(t, d.Remote, "git", "log", "-1", "--format=%s", "trunk"); got != "to trunk\n" {
		t.Errorf("remote trunk's last commit = %q, want the pushed one", got)
	}

	// A remote with another name.
	d.Git(t, "remote", "rename", "origin", "home")
	commit("to home\n")
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	d.Git(t, "remote", "rename", "home", "origin")

	d.Git(t, "checkout", "--quiet", "--detach")
	if err := r.Push(ctx); err == nil {
		t.Error("Push() with HEAD detached: error = nil, want one")
	}
	d.Git(t, "checkout", "--quiet", "main")
	d.Git(t, "branch", "--quiet", "--unset-upstream")
	if err := r.Push(ctx); !errors.Is(err, ErrNoUpstream) {
		t.Errorf("Push() with no upstream: error = %v, want ErrNoUpstream", err)
	}
	d.Git(t, "branch", "--quiet", "--set-upstream-to=origin/main")

	d.PushFromElsewhere(t, map[string]string{"home/dot_b": "b\n"})
	commit("rejected\n")
	if err := r.Push(ctx); !errors.Is(err, ErrRejected) {
		t.Errorf("Push() behind the remote: error = %v, want ErrRejected", err)
	}

	// A remote that turns the push down is not ahead, so syncing would not help.
	d.Git(t, "fetch", "--quiet")
	d.Git(t, "rebase", "--quiet", "@{upstream}")
	hook := filepath.Join(d.Remote, "hooks", "pre-receive")
	testenv.WriteFile(t, hook, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(ctx); err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("Push() turned down by the remote: error = %v, want one other than ErrRejected", err)
	}

	d.Git(t, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	if err := r.Push(ctx); err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("Push() to a missing remote: error = %v, want one other than ErrRejected", err)
	}
}

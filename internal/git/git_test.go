package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/testenv"
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

		other := filepath.Join(t.TempDir(), "other")
		testenv.Run(t, d.Home, "git", "clone", "--quiet", d.Remote, other)
		testenv.WriteFile(t, filepath.Join(other, "home/dot_profile"), "remote\n")
		testenv.Run(t, other, "git", "commit", "--quiet", "--all", "--message=remote")
		testenv.Run(t, other, "git", "push", "--quiet")
		assertAheadBehind(t, r, 1, 0) // not fetched yet

		before := time.Now().Add(-time.Second)
		if err := r.Fetch(ctx); err != nil {
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
		d.Git(t, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
		defer d.Git(t, "remote", "set-url", "origin", d.Remote)
		if err := r.Fetch(ctx); err == nil {
			t.Error("Fetch() error = nil, want an error")
		}
	})

	t.Run("fetch gives up when the context is done", func(t *testing.T) {
		// An ssh that never answers. It records its process ID, so the test
		// can check it was killed along with git rather than left running.
		pidFile := filepath.Join(t.TempDir(), "ssh.pid")
		d.Git(t, "remote", "set-url", "origin", "ssh://git@example.invalid/dotfiles.git")
		defer d.Git(t, "remote", "set-url", "origin", d.Remote)
		t.Setenv("GIT_SSH_COMMAND", "echo $$ >"+pidFile+"; exec sleep 30 #")
		ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := r.Fetch(ctx); err == nil {
			t.Error("Fetch() error = nil, want an error")
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
			if !running(pid) {
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

// running reports whether the process pid exists and has not exited.
func running(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// The state follows the command name, which is in parentheses.
	_, after, _ := strings.Cut(string(data), ") ")
	return after != "" && after[0] != 'Z'
}

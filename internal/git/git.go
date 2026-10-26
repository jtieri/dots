// Package git runs git in the dotfiles repo.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jtieri/dot/internal/run"
)

// ErrNoUpstream is returned when the current branch tracks no remote branch.
var ErrNoUpstream = errors.New("the branch has no upstream")

// Repo is a git repository.
type Repo struct {
	// Dir is the repository's top-level directory.
	Dir string
	// Program is the git executable. If empty, "git" is looked up in PATH.
	Program string
}

func (r *Repo) command(ctx context.Context, args ...string) *exec.Cmd {
	program := r.Program
	if program == "" {
		program = "git"
	}
	// Optional locks would let a background "dot status" get in the way of
	// git commands run by hand at the same time.
	args = append([]string{"--no-optional-locks", "-C", r.Dir}, args...)
	return exec.CommandContext(ctx, program, args...)
}

func (r *Repo) output(ctx context.Context, args ...string) ([]byte, error) {
	return run.Output(r.command(ctx, args...))
}

// Toplevel returns the top-level directory of the repository containing dir.
func Toplevel(ctx context.Context, dir string) (string, error) {
	r := &Repo{Dir: dir}
	out, err := r.output(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// A Change is an uncommitted change to one path, as "git status" reports it.
type Change struct {
	// Index and Worktree are the two status letters: the change staged in
	// the index and the change in the working tree. Both are '?' for an
	// untracked file.
	Index, Worktree byte
	// Path is relative to the top-level directory.
	Path string
	// OrigPath is the path a renamed or copied file had before.
	OrigPath string
}

// Status returns the uncommitted changes, listing every untracked file
// rather than only its directory.
func (r *Repo) Status(ctx context.Context) ([]Change, error) {
	out, err := r.output(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseStatus(out)
}

func parseStatus(out []byte) ([]Change, error) {
	if len(out) == 0 {
		return nil, nil
	}
	fields := strings.Split(string(bytes.TrimSuffix(out, []byte{0})), "\x00")
	var changes []Change
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 || f[2] != ' ' {
			return nil, fmt.Errorf("unexpected entry in git status output: %q", f)
		}
		c := Change{Index: f[0], Worktree: f[1], Path: f[3:]}
		if c.Index == 'R' || c.Index == 'C' {
			i++
			if i == len(fields) {
				return nil, fmt.Errorf("git status output ends after %q", f)
			}
			c.OrigPath = fields[i]
		}
		changes = append(changes, c)
	}
	return changes, nil
}

// Branch returns the name of the current branch, or "" when HEAD is detached.
func (r *Repo) Branch(ctx context.Context) (string, error) {
	cmd := r.command(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	out, err := run.Output(cmd)
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Fetch fetches the current branch's remote, giving up after timeout. git
// and ssh run without a controlling terminal, so they cannot stop to ask for
// a password or passphrase; one that is needed makes the fetch fail instead.
// When ctx is done or the time is up, git and everything it started are
// killed.
func (r *Repo) Fetch(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := r.command(ctx, "fetch", "--quiet")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		// The new session is also a process group, led by git.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	_, err := run.Output(cmd)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("no answer from the remote within %v", timeout)
	}
	return err
}

// AheadBehind returns the number of commits on the current branch that its
// upstream lacks, and the number on the upstream that the branch lacks, as of
// the last fetch.
func (r *Repo) AheadBehind(ctx context.Context) (ahead, behind int, err error) {
	if _, err := r.output(ctx, "rev-parse", "--verify", "--quiet", "@{upstream}"); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return 0, 0, ErrNoUpstream
		}
		return 0, 0, err
	}
	out, err := r.output(ctx, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return 0, 0, err
	}
	a, b, ok := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if !ok {
		return 0, 0, fmt.Errorf("unexpected git rev-list output: %q", out)
	}
	if ahead, err = strconv.Atoi(a); err != nil {
		return 0, 0, err
	}
	if behind, err = strconv.Atoi(b); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// LastFetch returns when the repository was last fetched, or the zero time if
// it never was.
func (r *Repo) LastFetch(ctx context.Context) (time.Time, error) {
	out, err := r.output(ctx, "rev-parse", "--git-path", "FETCH_HEAD")
	if err != nil {
		return time.Time{}, err
	}
	name := strings.TrimSuffix(string(out), "\n")
	if !filepath.IsAbs(name) {
		name = filepath.Join(r.Dir, name)
	}
	info, err := os.Stat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

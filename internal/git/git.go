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

	"github.com/jtieri/dots/internal/run"
	"github.com/jtieri/dots/internal/text"
)

// stopGrace is how long git has to stop after being interrupted before it
// is killed, for commands that change the repo.
const stopGrace = 5 * time.Second

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
	// Optional locks would let a background "dots status" get in the way of
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

// DiffContents returns, as a git diff, the change from the contents before to
// the contents after of the file name, a slash-separated path such as a path
// in the repo. It is empty when the contents are the same. No file is read.
func DiffContents(ctx context.Context, name string, before, after []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "dots-diff-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	for side, data := range map[string][]byte{"a": before, "b": after} {
		file := filepath.Join(dir, side, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(file, data, 0o600); err != nil {
			return nil, err
		}
	}
	r := &Repo{Dir: dir}
	// The sides are named a and b, so without git's own prefixes the
	// diff reads as one of a repo file.
	cmd := r.command(ctx, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv",
		"--no-prefix", "--", "a/"+name, "b/"+name)
	out, err := run.Output(cmd)
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
		return out, nil // the contents differ
	}
	return out, err
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

// DivergedError is returned by FastForward when the branch and its upstream
// both have commits the other lacks.
type DivergedError struct {
	// Ahead and Behind count the commits only on the branch and only on its
	// upstream.
	Ahead, Behind int
}

func (e *DivergedError) Error() string {
	return fmt.Sprintf("the branch and its upstream have diverged: %s only on the branch, %d only on the upstream",
		text.Pluralize(e.Ahead, "commit"), e.Behind)
}

// BlockedError is returned by FastForward when git refused to fast-forward
// because that would overwrite uncommitted changes.
type BlockedError struct {
	// Paths are the files with uncommitted changes that the upstream also
	// changes, relative to the top-level directory.
	Paths []string
	Err   error
}

func (e *BlockedError) Error() string {
	return "the fast-forward would overwrite uncommitted changes to " + strings.Join(e.Paths, ", ")
}

func (e *BlockedError) Unwrap() error { return e.Err }

// FastForward moves the current branch up to its upstream, as of the last
// fetch, and returns the number of commits that brought in. It does nothing
// when the branch already has every upstream commit, and returns a
// *DivergedError when the branch also has commits of its own. Uncommitted
// changes are kept, unless the upstream changes the same files: git then
// refuses, nothing changes, and the error is a *BlockedError.
func (r *Repo) FastForward(ctx context.Context) (int, error) {
	ahead, behind, err := r.AheadBehind(ctx)
	if err != nil {
		return 0, err
	}
	switch {
	case behind == 0:
		return 0, nil
	case ahead > 0:
		return 0, &DivergedError{Ahead: ahead, Behind: behind}
	}
	cmd := r.command(ctx, "merge", "--ff-only", "--quiet", "@{upstream}")
	// Killed while it writes the working tree, git would leave its lock
	// file behind and block every later git command. Interrupted as Ctrl-C
	// would, it removes the lock before exiting.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = stopGrace
	if _, err := run.Output(cmd); err != nil {
		// git decides what blocks it, and is more lenient than an overlap:
		// it allows a file deleted here that the upstream deletes too. The
		// overlap only names the files once git has refused.
		paths, overlapErr := r.overlap(ctx)
		if overlapErr != nil || len(paths) == 0 {
			return 0, err
		}
		return 0, &BlockedError{Paths: paths, Err: err}
	}
	return behind, nil
}

// overlap returns the files with uncommitted changes that the upstream also
// changes.
func (r *Repo) overlap(ctx context.Context) ([]string, error) {
	out, err := r.output(ctx, "diff", "--name-only", "--no-renames", "-z", "HEAD", "@{upstream}")
	if err != nil {
		return nil, err
	}
	incoming := make(map[string]bool)
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			incoming[p] = true
		}
	}
	changes, err := r.Status(ctx)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, c := range changes {
		for _, p := range []string{c.Path, c.OrigPath} {
			if incoming[p] {
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}

// pathspecs returns the arguments that make git read the paths a command
// works on from standard input, and that input. The paths are taken
// literally, so a name with * or : in it means only that file.
func pathspecs(paths []string) (args []string, stdin *strings.Reader) {
	return []string{"--pathspec-from-file=-", "--pathspec-file-nul"}, strings.NewReader(strings.Join(paths, "\x00"))
}

// literal returns a command that runs git with args, taking pathspecs
// literally.
func (r *Repo) literal(ctx context.Context, args ...string) *exec.Cmd {
	return r.command(ctx, append([]string{"--literal-pathspecs"}, args...)...)
}

// Commit commits the changes to paths, and only those, with message. paths
// are relative to the top-level directory and may include new and deleted
// files, and both names of a renamed one.
func (r *Repo) Commit(ctx context.Context, paths []string, message string) error {
	if len(paths) == 0 {
		return errors.New("nothing to commit")
	}
	if err := r.stage(ctx, paths, nil); err != nil {
		return err
	}
	return r.runPaths(ctx, []string{"commit", "--quiet", "--message=" + message}, paths, nil)
}

// stage stages paths as they are in the working tree, in the index that env
// names, or the repo's own when env is nil. A path that is gone is removed
// from the index, or left alone when the index lacks it too, as the old name
// of a staged rename does.
func (r *Repo) stage(ctx context.Context, paths, env []string) error {
	var present, gone []string
	for _, p := range paths {
		_, err := os.Lstat(filepath.Join(r.Dir, filepath.FromSlash(p)))
		switch {
		case err == nil:
			present = append(present, p)
		case errors.Is(err, fs.ErrNotExist):
			gone = append(gone, p)
		default:
			return err
		}
	}
	if len(gone) > 0 {
		if err := r.runPaths(ctx, []string{"rm", "--cached", "--ignore-unmatch", "--quiet"}, gone, env); err != nil {
			return err
		}
	}
	if len(present) > 0 {
		return r.runPaths(ctx, []string{"add"}, present, env)
	}
	return nil
}

// runPaths runs git with args on paths, taken literally. Interrupted, git is
// stopped as Ctrl-C would stop it, so it removes its lock on the index.
func (r *Repo) runPaths(ctx context.Context, args, paths, env []string) error {
	spec, stdin := pathspecs(paths)
	cmd := r.literal(ctx, append(args, spec...)...)
	cmd.Stdin, cmd.Env = stdin, env
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = stopGrace
	_, err := run.Output(cmd)
	return err
}

// StagedDiff returns, as a git diff, what Commit would commit for paths. It
// stages them in a copy of the index, so the real one is left as it was.
func (r *Repo) StagedDiff(ctx context.Context, paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out, err := r.output(ctx, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, err
	}
	index := strings.TrimSuffix(string(out), "\n")
	if !filepath.IsAbs(index) {
		index = filepath.Join(r.Dir, index)
	}
	data, err := os.ReadFile(index)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "dots-index-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	env := append(os.Environ(), "GIT_INDEX_FILE="+tmp.Name())
	if err := r.stage(ctx, paths, env); err != nil {
		return nil, err
	}
	// git diff takes no pathspec file.
	args := append([]string{"diff", "--cached", "--no-color", "--no-ext-diff", "--find-renames", "--"}, paths...)
	cmd := r.literal(ctx, args...)
	cmd.Env = env
	return run.Output(cmd)
}

// ErrRejected is returned by Push when the remote has commits the branch
// lacks.
var ErrRejected = errors.New("the remote has commits this branch does not")

// Push pushes the current branch to its upstream, whatever its name and
// git's push.default setting. As with Fetch, git and ssh cannot stop to ask
// for a password or passphrase. When ctx is done, git and everything it
// started are interrupted as Ctrl-C would, and killed if they have not
// stopped after stopGrace.
func (r *Repo) Push(ctx context.Context) error {
	// With HEAD detached there is no branch, and so no upstream.
	branch, err := r.Branch(ctx)
	if err != nil {
		return err
	}
	remote, err := r.output(ctx, "config", "--get", "branch."+branch+".remote")
	if err != nil {
		return ErrNoUpstream
	}
	merge, err := r.output(ctx, "config", "--get", "branch."+branch+".merge")
	if err != nil {
		return ErrNoUpstream
	}
	refspec := "HEAD:" + strings.TrimSpace(string(merge))
	cmd := r.command(ctx, "push", "--porcelain", strings.TrimSpace(string(remote)), refspec)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = stopGrace
	out, err := run.Output(cmd)
	if err == nil {
		return nil
	}
	// A rejected ref is flagged with a ! in the porcelain output.
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "!\t") && strings.Contains(line, "[rejected]") {
			return fmt.Errorf("%w: %w", ErrRejected, err)
		}
	}
	return err
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

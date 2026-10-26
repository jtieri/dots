package chezmoi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jtieri/dot/internal/run"
)

// Client runs chezmoi with the configuration it finds on its own, the same
// way a chezmoi command typed in a terminal would.
type Client struct {
	// Program is the chezmoi executable. If empty, "chezmoi" is looked up
	// in PATH.
	Program string
}

// command returns a command that runs chezmoi with args. chezmoi never pages
// or colors its output when run this way, and its standard input is empty,
// so a question it did not expect to ask fails at once rather than waiting.
func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	program := c.Program
	if program == "" {
		program = "chezmoi"
	}
	args = append([]string{"--no-tty", "--no-pager", "--color=false"}, args...)
	return exec.CommandContext(ctx, program, args...)
}

// output runs chezmoi with args and returns its standard output.
func (c *Client) output(ctx context.Context, args ...string) ([]byte, error) {
	return run.Output(c.command(ctx, args...))
}

// Init regenerates chezmoi's configuration file from the repo's template,
// which refreshes the facts it records about the machine. The file's contents
// stay the same when nothing has changed.
func (c *Client) Init(ctx context.Context) error {
	_, err := c.output(ctx, "init")
	return err
}

// SourceDir returns chezmoi's source directory: the folder in the dotfiles
// repo that holds the files chezmoi manages. chezmoi reports its default
// location even when it has not been set up, so the directory may not exist.
func (c *Client) SourceDir(ctx context.Context) (string, error) {
	out, err := c.output(ctx, "source-path")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// SourcePath returns the file in the source directory that produces target,
// an absolute path in the home directory.
func (c *Client) SourcePath(ctx context.Context, target string) (string, error) {
	out, err := c.output(ctx, "source-path", target)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// A StatusEntry is one line of "chezmoi status".
type StatusEntry struct {
	// Local says how the file in the home directory changed since chezmoi
	// last wrote it: ' ' (unchanged), 'A', 'D' or 'M'.
	Local byte
	// Apply says what "chezmoi apply" would do: ' ', 'A' (add), 'D'
	// (delete), 'M' (modify) or 'R' (run a script).
	Apply byte
	// Path is relative to the home directory. For a script it is the
	// script's name under .chezmoiscripts.
	Path string
}

// Status returns what differs between the home directory and the repo.
func (c *Client) Status(ctx context.Context) ([]StatusEntry, error) {
	out, err := c.output(ctx, "status")
	if err != nil {
		return nil, err
	}
	return parseStatus(out)
}

func parseStatus(out []byte) ([]StatusEntry, error) {
	var entries []StatusEntry
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSuffix(line, "\n")
		if len(line) < 4 || line[2] != ' ' {
			return nil, fmt.Errorf("unexpected line in chezmoi status output: %q", line)
		}
		entries = append(entries, StatusEntry{Local: line[0], Apply: line[1], Path: line[3:]})
	}
	return entries, nil
}

// ManagedDirs returns the absolute paths of the directories chezmoi manages on
// this machine.
func (c *Client) ManagedDirs(ctx context.Context) ([]string, error) {
	out, err := c.output(ctx, "managed", "--include=dirs", "--path-style=absolute", "-0")
	if err != nil {
		return nil, err
	}
	return splitNul(out), nil
}

// Unmanaged returns the absolute paths of the files and directories inside
// dirs that chezmoi does not manage. An unmanaged directory is listed once,
// without its contents. Each of dirs must exist.
func (c *Client) Unmanaged(ctx context.Context, dirs []string) ([]string, error) {
	if len(dirs) == 0 {
		return nil, nil
	}
	args := append([]string{"unmanaged", "--path-style=absolute", "-0"}, dirs...)
	out, err := c.output(ctx, args...)
	if err != nil {
		return nil, err
	}
	paths := splitNul(out)
	for i, p := range paths {
		paths[i] = filepath.Clean(p)
	}
	return paths, nil
}

func splitNul(b []byte) []string {
	b = bytes.TrimSuffix(b, []byte{0})
	if len(b) == 0 {
		return nil
	}
	return strings.Split(string(b), "\x00")
}

// Cat returns the contents the repo gives target, an absolute path in the
// home directory. For a symbolic link it is the link's target.
func (c *Client) Cat(ctx context.Context, target string) ([]byte, error) {
	return c.output(ctx, "cat", target)
}

// Diff returns, as a git-style diff, the changes applying targets would make.
func (c *Client) Diff(ctx context.Context, targets []string) ([]byte, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	return c.output(ctx, append([]string{"diff"}, targets...)...)
}

// An EntryState is chezmoi's record of what it last wrote to a target.
type EntryState struct {
	// Type is "file", "dir", "symlink", "script" or "remove".
	Type string `json:"type"`
	// ContentsSHA256 is the SHA-256 of a file's contents or a link's
	// target, in hex. It is empty for directories.
	ContentsSHA256 string `json:"contentsSHA256"`
}

// EntryStates returns chezmoi's records of what it last wrote, keyed by
// absolute path.
func (c *Client) EntryStates(ctx context.Context) (map[string]EntryState, error) {
	out, err := c.output(ctx, "state", "dump", "--format=json")
	if err != nil {
		return nil, err
	}
	var dump struct {
		EntryState map[string]EntryState `json:"entryState"`
	}
	if err := json.Unmarshal(out, &dump); err != nil {
		return nil, fmt.Errorf("reading chezmoi state dump: %w", err)
	}
	return dump.EntryState, nil
}

// ApplyOptions control Apply.
type ApplyOptions struct {
	// Targets are absolute paths in the home directory. A script's target
	// is its path under ~/.chezmoiscripts, as Status reports it.
	Targets []string
	// Force overwrites targets changed since chezmoi last wrote them.
	// Without it, applying such a target fails.
	Force bool
	// Stdout and Stderr receive the output of chezmoi and its scripts.
	Stdout, Stderr io.Writer
}

// Apply makes the targets match the repo, running the scripts among them. It
// does nothing when there are no targets, where chezmoi would apply
// everything.
func (c *Client) Apply(ctx context.Context, opts ApplyOptions) error {
	if len(opts.Targets) == 0 {
		return nil
	}
	args := []string{"apply"}
	if opts.Force {
		args = append(args, "--force")
	}
	cmd := c.command(ctx, append(append(args, "--"), opts.Targets...)...)
	cmd.Stdout, cmd.Stderr = opts.Stdout, opts.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chezmoi apply: %w", err)
	}
	return nil
}

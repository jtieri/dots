package chezmoi

import (
	"bytes"
	"context"
	"fmt"
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

// output runs chezmoi with args and returns its standard output. chezmoi
// never prompts, pages or colors its output when run this way.
func (c *Client) output(ctx context.Context, args ...string) ([]byte, error) {
	program := c.Program
	if program == "" {
		program = "chezmoi"
	}
	args = append([]string{"--no-tty", "--no-pager", "--color=false"}, args...)
	return run.Output(exec.CommandContext(ctx, program, args...))
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

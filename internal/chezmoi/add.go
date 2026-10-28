package chezmoi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jtieri/dot/internal/run"
)

// AddOptions control Add.
type AddOptions struct {
	// DryRun works out what adding would change without changing anything.
	DryRun bool
	// Force answers yes to chezmoi's questions. chezmoi asks before it
	// drops a file's template or private attribute, and without Force such
	// an add fails, since chezmoi is never given an answer. It drops a
	// directory's private attribute without asking.
	//
	// chezmoi's own --force would also turn off its secret scanner, so
	// Force answers the questions instead.
	Force bool
	// AllowSecrets adds a file even when chezmoi's scanner finds what looks
	// like a secret in it. Without it, such an add fails.
	AllowSecrets bool
}

// A SourceChange is one entry that adding changes in the source directory.
type SourceChange struct {
	Kind ChangeKind
	// Path is the entry's absolute path after the change. For a deleted
	// entry it is the path it had.
	Path string
	// From is the path a renamed entry had before.
	From string
	// Dir reports whether a created or deleted entry is a directory. It is
	// false for the other kinds of change.
	Dir bool
}

// A ChangeKind says how adding changes an entry in the source directory.
type ChangeKind int

// Kinds of SourceChange.
const (
	Modified ChangeKind = iota
	Created
	Deleted
	// Renamed changes only the entry's name, as when a directory loses its
	// private_ prefix. chezmoi shows a renamed file as one deleted and one
	// created instead.
	Renamed
)

// A SourceDiff is what adding changes in the source directory.
type SourceDiff struct {
	Changes []SourceChange
	// Text is the change as a git diff, for people to read.
	Text []byte
}

// Add makes the source directory's copy of target, an absolute path in the
// home directory, match target, and returns what it changed. A directory is
// added with everything in it that chezmoi does not ignore.
func (c *Client) Add(ctx context.Context, target string, opts AddOptions) (*SourceDiff, error) {
	sourceDir, err := c.configuredSourceDir(ctx)
	if err != nil {
		return nil, err
	}
	secrets := "error"
	if opts.AllowSecrets {
		secrets = "ignore"
	}
	args := []string{"add", "--verbose", "--secrets=" + secrets}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	cmd := c.command(ctx, append(args, "--", target)...)
	if opts.Force {
		cmd.Stdin = strings.NewReader("all\n")
	}
	out, err := run.Output(cmd)
	if err != nil {
		return nil, err
	}
	out = trimQuestions(out)
	changes, err := parseAddDiff(string(out), sourceDir)
	if err != nil {
		return nil, err
	}
	return &SourceDiff{Changes: changes, Text: out}, nil
}

// question ends each question chezmoi asks. chezmoi writes its questions to
// standard output, ahead of the diff.
const question = " (yes/no/all/quit)? "

// trimQuestions removes the questions at the start of out.
func trimQuestions(out []byte) []byte {
	for {
		line, _, _ := bytes.Cut(out, []byte("\n"))
		i := bytes.Index(line, []byte(question))
		if i < 0 {
			return out
		}
		out = out[i+len(question):]
	}
}

// configuredSourceDir returns the source directory as chezmoi's configuration
// names it. The paths in the diffs chezmoi prints are relative to it, rather
// than to the directory .chezmoiroot points to within it.
func (c *Client) configuredSourceDir(ctx context.Context) (string, error) {
	out, err := c.output(ctx, "dump-config", "--format=json")
	if err != nil {
		return "", err
	}
	var config struct {
		SourceDir string `json:"sourceDir"`
	}
	if err := json.Unmarshal(out, &config); err != nil {
		return "", fmt.Errorf("reading chezmoi's configuration: %w", err)
	}
	return config.SourceDir, nil
}

// parseAddDiff reads the git diff that "chezmoi add --verbose" prints into
// the changes it describes. Paths in the diff are relative to sourceDir.
//
// chezmoi does not quote unusual names in the diff's headers, so a path with
// a space in it cannot be told from two paths. Except for renames, which have
// headers of their own, both sides of the header always name the same path,
// which makes its length known.
func parseAddDiff(diff, sourceDir string) ([]SourceChange, error) {
	var changes []SourceChange
	var headers []string
	var c *SourceChange
	for line := range strings.Lines(diff) {
		line = strings.TrimSuffix(line, "\n")
		if rest, ok := strings.CutPrefix(line, "diff --git a/"); ok {
			changes = append(changes, SourceChange{Kind: Modified, Path: sameHeaderPath(rest)})
			headers = append(headers, line)
			c = &changes[len(changes)-1]
			continue
		}
		if c == nil {
			return nil, fmt.Errorf("unexpected line in chezmoi's diff: %q", line)
		}
		switch {
		case strings.HasPrefix(line, "new file mode "):
			c.Kind = Created
			c.Dir = strings.HasPrefix(line, "new file mode 40")
		case strings.HasPrefix(line, "deleted file mode "):
			c.Kind = Deleted
			c.Dir = strings.HasPrefix(line, "deleted file mode 40")
		case strings.HasPrefix(line, "rename from "):
			c.Kind = Renamed
			c.From = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			c.Kind = Renamed
			c.Path = strings.TrimPrefix(line, "rename to ")
		}
	}
	for i := range changes {
		c := &changes[i]
		if c.Path == "" || (c.Kind == Renamed && c.From == "") {
			return nil, fmt.Errorf("unexpected header in chezmoi's diff: %q", headers[i])
		}
		c.Path = filepath.Join(sourceDir, filepath.FromSlash(c.Path))
		if c.From != "" {
			c.From = filepath.Join(sourceDir, filepath.FromSlash(c.From))
		}
	}
	return changes, nil
}

// sameHeaderPath returns p when rest, a diff header after its "diff --git a/",
// is p + " b/" + p, and "" otherwise. A rename's header names two paths, which
// its rename lines give instead.
func sameHeaderPath(rest string) string {
	if n := len(rest) - len(" b/"); n > 0 && n%2 == 0 {
		if p := rest[:n/2]; rest == p+" b/"+p {
			return p
		}
	}
	return ""
}

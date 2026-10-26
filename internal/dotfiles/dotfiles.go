// Package dotfiles finds the home directory's dotfiles: the repo, and the
// folder in it that chezmoi applies from.
package dotfiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/git"
)

// ErrNotSetUp is returned when chezmoi has no source directory on this
// machine.
var ErrNotSetUp = errors.New("chezmoi is not set up on this machine; run chezmoi init first")

// Dotfiles is a home directory and the repo chezmoi keeps it in step with.
type Dotfiles struct {
	Home string
	// Source is chezmoi's source directory, inside the repo.
	Source  string
	Repo    *git.Repo
	Chezmoi *chezmoi.Client
}

// Open finds the dotfiles that cz applies to home.
func Open(ctx context.Context, home string, cz *chezmoi.Client) (*Dotfiles, error) {
	source, err := cz.SourceDir(ctx)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil, ErrNotSetUp
	}
	if err != nil {
		return nil, err
	}
	top, err := git.Toplevel(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("chezmoi's source directory %s is not in a git repository: %w", source, err)
	}
	return &Dotfiles{Home: home, Source: source, Repo: &git.Repo{Dir: top}, Chezmoi: cz}, nil
}

// HomePath returns the file in the home directory, relative to it, that the
// repo file at repoPath produces. repoPath is relative to the top of the repo
// and slash-separated, as git writes it. ok is false for repo files outside
// the source directory and for source files that produce no file, such as
// scripts.
func (d *Dotfiles) HomePath(repoPath string) (rel string, ok bool) {
	dir, err := filepath.Rel(d.Repo.Dir, d.Source)
	if err != nil {
		return "", false
	}
	name := repoPath
	if dir = filepath.ToSlash(dir); dir != "." {
		if name, ok = strings.CutPrefix(repoPath, dir+"/"); !ok {
			return "", false
		}
	}
	return chezmoi.TargetPath(name)
}

// Tilde writes rel, a path relative to the home directory, the way people
// read and write home paths: ~/rel, or ~ for the home directory itself.
func Tilde(rel string) string {
	if rel == "." {
		return "~"
	}
	return "~/" + rel
}

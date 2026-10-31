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

	"github.com/jtieri/dots/internal/chezmoi"
	"github.com/jtieri/dots/internal/git"
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

// Target returns target, an absolute path, relative to the home directory and
// slash-separated. It refuses paths dots never acts on: those outside the
// home directory, the home directory itself, and the repo or a folder that
// holds it.
func (d *Dotfiles) Target(target string) (string, error) {
	rel, err := filepath.Rel(d.Home, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not in the home directory", target)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", errors.New("the home directory itself cannot be named; name the files in it")
	}
	if Within(target, d.Repo.Dir) || Within(d.Repo.Dir, target) {
		return "", fmt.Errorf("%s is the dotfiles repo or holds it", Tilde(rel))
	}
	return rel, nil
}

// Within reports whether name is dir or inside it. Both are absolute, or
// both are slash-separated and relative to the same directory.
func Within(name, dir string) bool {
	return name == dir || strings.HasPrefix(name, strings.TrimSuffix(dir, "/")+"/")
}

// Tilde writes rel, a path relative to the home directory, the way people
// read and write home paths: ~/rel, or ~ for the home directory itself.
func Tilde(rel string) string {
	if rel == "." {
		return "~"
	}
	return "~/" + rel
}

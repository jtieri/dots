// Package dotfiles finds the home directory's dotfiles: the repo, and the
// folder in it that chezmoi applies from.
package dotfiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

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

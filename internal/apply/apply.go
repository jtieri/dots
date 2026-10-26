package apply

import (
	"context"
	"io"
	"path/filepath"
	"time"

	"github.com/jtieri/dot/internal/backup"
	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/dotfiles"
)

// A Choice is what to do about files changed on this machine.
type Choice int

const (
	// Keep leaves them as they are and applies everything else.
	Keep Choice = iota
	// Overwrite replaces them with the repo's versions too.
	Overwrite
)

// Options control Apply.
type Options struct {
	Choice Choice
	// BackupDir is where Overwrite copies files before replacing them. If
	// empty, no copies are made.
	BackupDir string
	// Now names the backup folder.
	Now time.Time
	// Stdout and Stderr receive the output of chezmoi and the setup steps.
	Stdout, Stderr io.Writer
}

// Apply carries out the plan and returns the folder holding the copies made
// before overwriting, or "" if none were made. A failed backup stops Apply
// before anything is overwritten.
func (p *Plan) Apply(ctx context.Context, d *dotfiles.Dotfiles, opts Options) (backupFolder string, err error) {
	if opts.Choice == Overwrite {
		if opts.BackupDir != "" {
			var local []string
			for _, c := range p.Local() {
				local = append(local, c.Path)
			}
			if backupFolder, err = backup.Save(opts.BackupDir, d.Home, local, opts.Now); err != nil {
				return backupFolder, err
			}
		}
	}
	return backupFolder, d.Chezmoi.Apply(ctx, chezmoi.ApplyOptions{
		Targets: targets(d.Home, p.ToApply(opts.Choice)),
		Force:   opts.Choice == Overwrite,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
	})
}

// ToApply returns the changes Apply makes for choice.
func (p *Plan) ToApply(choice Choice) []Change {
	if choice == Overwrite {
		return p.Changes
	}
	return p.Rest()
}

// Diff returns, as a git-style diff, everything the plan would change.
func (p *Plan) Diff(ctx context.Context, d *dotfiles.Dotfiles) ([]byte, error) {
	return d.Chezmoi.Diff(ctx, targets(d.Home, p.Changes))
}

// targets returns the absolute paths changes apply to.
func targets(home string, changes []Change) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = filepath.Join(home, c.Path)
	}
	return out
}

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
	changes := p.Rest()
	if opts.Choice == Overwrite {
		changes = p.Changes
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
	targets := make([]string, len(changes))
	for i, c := range changes {
		targets[i] = filepath.Join(d.Home, c.Path)
	}
	return backupFolder, d.Chezmoi.Apply(ctx, chezmoi.ApplyOptions{
		Targets: targets,
		Force:   opts.Choice == Overwrite,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
	})
}

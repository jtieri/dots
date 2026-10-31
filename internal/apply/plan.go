// Package apply works out what applying the repo to the home directory would
// change, and applies it.
package apply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/jtieri/dots/internal/chezmoi"
	"github.com/jtieri/dots/internal/dotfiles"
)

// A Kind says what applying does to a path, and whether the path was changed
// on this machine.
type Kind int

// Kinds of Change, in the order the preview lists them.
const (
	// Update replaces a file with the repo's newer version.
	Update Kind = iota
	// Create adds a file that is new in the repo.
	Create
	// Delete removes a file the repo no longer has.
	Delete
	// Run runs a setup step.
	Run
	// ChangedHere replaces a file changed on this machine since chezmoi
	// last wrote it.
	ChangedHere
	// Conflict replaces a file changed both on this machine and in the
	// repo since chezmoi last wrote it.
	Conflict
)

// A Change is one thing applying would do.
type Change struct {
	// Path is relative to the home directory. For a setup step it is the
	// script's path under .chezmoiscripts.
	Path string
	Kind Kind
	// DeletedHere is set for a ChangedHere or Conflict file that was
	// deleted on this machine, which applying would bring back.
	DeletedHere bool
}

// Local reports whether applying c would replace something changed on this
// machine.
func (c Change) Local() bool { return c.Kind == ChangedHere || c.Kind == Conflict }

// A Plan is what applying would do, in chezmoi's order.
type Plan struct {
	Changes []Change
}

// Local returns the changes that would replace something changed on this
// machine.
func (p *Plan) Local() []Change { return p.filter(true) }

// Rest returns the changes that would not.
func (p *Plan) Rest() []Change { return p.filter(false) }

func (p *Plan) filter(local bool) []Change {
	var out []Change
	for _, c := range p.Changes {
		if c.Local() == local {
			out = append(out, c)
		}
	}
	return out
}

// Preview refreshes chezmoi's facts about the machine, then works out what
// applying the repo would do. It changes nothing in the home directory.
func Preview(ctx context.Context, d *dotfiles.Dotfiles) (*Plan, error) {
	if err := d.Chezmoi.Init(ctx); err != nil {
		return nil, err
	}
	entries, err := d.Chezmoi.Status(ctx)
	if err != nil {
		return nil, err
	}
	var states map[string]chezmoi.EntryState
	plan := &Plan{}
	for _, e := range entries {
		c := Change{Path: e.Path}
		switch {
		case e.Apply == ' ':
			continue
		case e.Apply == 'R':
			c.Kind = Run
		case e.Local == ' ' && e.Apply == 'A':
			c.Kind = Create
		case e.Local == ' ' && e.Apply == 'D':
			c.Kind = Delete
		case e.Local == ' ':
			c.Kind = Update
		default:
			if states == nil {
				if states, err = d.Chezmoi.EntryStates(ctx); err != nil {
					return nil, err
				}
			}
			repoChanged, err := repoChanged(ctx, d, e, states)
			if err != nil {
				return nil, err
			}
			c.Kind = ChangedHere
			if repoChanged {
				c.Kind = Conflict
			}
			c.DeletedHere = e.Local == 'D'
		}
		plan.Changes = append(plan.Changes, c)
	}
	return plan, nil
}

// repoChanged reports whether the repo's version of a file changed on this
// machine also differs from what chezmoi last wrote there. chezmoi's status
// cannot say: once a file is changed here, it reports the same either way.
func repoChanged(ctx context.Context, d *dotfiles.Dotfiles, e chezmoi.StatusEntry, states map[string]chezmoi.EntryState) (bool, error) {
	if e.Apply == 'D' {
		return true, nil // the repo deletes it
	}
	target := filepath.Join(d.Home, e.Path)
	state, ok := states[target]
	if !ok {
		// chezmoi never wrote it, so a change in the repo cannot be ruled
		// out. Calling it a conflict errs on the side of a warning.
		return true, nil
	}
	if state.Type == "dir" {
		return false, nil // only a directory's permissions can differ
	}
	contents, err := d.Chezmoi.Cat(ctx, target)
	if err != nil {
		return false, err
	}
	if state.Type == "symlink" {
		// chezmoi records a link's target without the newline cat adds.
		contents = bytes.TrimSuffix(contents, []byte("\n"))
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:]) != state.ContentsSHA256, nil
}

// Package push commits the dotfiles repo's changes, so that they can be
// pushed to the other machines.
package push

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jtieri/dots/internal/chezmoi"
	"github.com/jtieri/dots/internal/dotfiles"
	"github.com/jtieri/dots/internal/run"
)

// Options control Prepare.
type Options struct {
	// PackageLists is the folder, relative to the top of the repo, that
	// holds each machine's package lists. Empty leaves them alone.
	PackageLists string
	// Protected are patterns, as path.Match takes them and relative to the
	// home directory, for files that are never committed.
	Protected []string
	// Pacman is the pacman program. If empty, "pacman" is looked up in PATH,
	// and the package lists are left alone when it is not found.
	Pacman string
}

// A Kind says how a commit changes a file.
type Kind int

// Kinds of Item, in the order a preview lists them.
const (
	Changed Kind = iota
	Added
	Deleted
	// Moved is a file whose name in the repo changed. For a dotfile whose
	// home path stayed the same, only its attributes changed.
	Moved
)

// An Item is one file a commit changes.
type Item struct {
	Kind Kind
	// HomePath is the file in the home directory that the repo file
	// produces, relative to it, or "" for other repo files.
	HomePath string
	// RepoPath is relative to the top of the repo.
	RepoPath string
	// From is the repo path a moved file had.
	From string
	// Note says how a moved dotfile's attributes changed, such as "now
	// private".
	Note string
}

// A Plan is what a commit would contain.
type Plan struct {
	// Host is the machine's name, which names its package lists.
	Host  string
	Items []Item
	// Uncaptured lists the files, relative to the home directory, changed
	// on this machine but not in the repo, which the commit leaves out.
	Uncaptured []string

	paths []string
	lists []savedFile
}

// A savedFile is a file as it was before Prepare wrote it.
type savedFile struct {
	name    string
	existed bool
	data    []byte
}

// Prepare works out what committing the repo's uncommitted changes would
// contain, after refreshing this machine's package lists in the repo. Discard
// puts the lists back if nothing is committed.
func Prepare(ctx context.Context, d *dotfiles.Dotfiles, opts Options) (*Plan, error) {
	host, err := d.Chezmoi.Hostname(ctx)
	if err != nil {
		return nil, err
	}
	p := &Plan{Host: host}
	if err := p.refreshLists(ctx, d, opts); err != nil {
		return nil, errors.Join(err, p.Discard())
	}
	if err := p.gather(ctx, d, opts); err != nil {
		return nil, errors.Join(err, p.Discard())
	}
	return p, nil
}

func (p *Plan) gather(ctx context.Context, d *dotfiles.Dotfiles, opts Options) error {
	changes, err := d.Repo.Status(ctx)
	if err != nil {
		return err
	}
	byHome := make(map[string]int) // home path of a deleted or added item to its index
	for _, c := range changes {
		it := Item{RepoPath: c.Path, From: c.OrigPath}
		switch {
		case c.Index == 'R':
			it.Kind = Moved
		case c.Index == 'D' || c.Worktree == 'D':
			it.Kind = Deleted
		case c.Index == '?' || c.Index == 'A':
			it.Kind = Added
		default:
			it.Kind = Changed
		}
		it.HomePath, _ = d.HomePath(c.Path)
		p.paths = append(p.paths, c.Path)
		if c.OrigPath != "" {
			p.paths = append(p.paths, c.OrigPath)
		}
		// git pairs a deleted and an added file as a rename only once
		// both are staged, so the pairs a rename of a dotfile's attributes
		// leaves are matched up here.
		if j, ok := byHome[it.HomePath]; ok && pairs(p.Items[j].Kind, it.Kind) {
			p.Items[j] = moved(p.Items[j], it)
			p.Items[j].Note = attributeNote(d, p.Items[j].From, p.Items[j].RepoPath)
			delete(byHome, it.HomePath)
			continue
		}
		if it.HomePath != "" && (it.Kind == Deleted || it.Kind == Added) {
			byHome[it.HomePath] = len(p.Items)
		}
		if it.Kind == Moved {
			it.Note = attributeNote(d, it.From, it.RepoPath)
		}
		p.Items = append(p.Items, it)
	}
	for _, it := range p.Items {
		if it.Kind == Deleted || it.HomePath == "" {
			continue
		}
		if pattern, ok := protected(it.HomePath, opts.Protected); ok {
			return fmt.Errorf("the repo has an uncommitted copy of %s, which capture.protected (%q) keeps out of the repo, "+
				"so nothing is committed until %s is deleted from the repo", dotfiles.Tilde(it.HomePath), "~/"+pattern, it.RepoPath)
		}
	}
	entries, err := d.Chezmoi.Status(ctx)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Local != ' ' && e.Apply != 'R' {
			p.Uncaptured = append(p.Uncaptured, e.Path)
		}
	}
	return nil
}

func pairs(a, b Kind) bool {
	return (a == Deleted && b == Added) || (a == Added && b == Deleted)
}

// moved joins a deleted and an added item for the same home path into one.
func moved(a, b Item) Item {
	from, to := a, b
	if a.Kind == Added {
		from, to = b, a
	}
	return Item{Kind: Moved, HomePath: to.HomePath, RepoPath: to.RepoPath, From: from.RepoPath}
}

// attributeNote describes how renaming a dotfile from one repo path to
// another changed its attributes, or returns "" when it moved elsewhere.
func attributeNote(d *dotfiles.Dotfiles, from, to string) string {
	fromHome, ok1 := d.HomePath(from)
	toHome, ok2 := d.HomePath(to)
	if !ok1 || !ok2 || fromHome != toHome {
		return ""
	}
	_, before, _ := chezmoi.Parse(path.Base(from), false)
	_, after, _ := chezmoi.Parse(path.Base(to), false)
	switch {
	case !before.Private && after.Private:
		return "now private"
	case before.Private && !after.Private:
		return "no longer private"
	}
	return "permissions changed"
}

// protected returns the pattern that protects rel, a path relative to the
// home directory, matching it or a folder it is in.
func protected(rel string, patterns []string) (string, bool) {
	for p := rel; p != "."; p = path.Dir(p) {
		for _, pattern := range patterns {
			if ok, _ := path.Match(pattern, p); ok {
				return pattern, true
			}
		}
	}
	return "", false
}

// packageLists are the lists kept for each machine and the pacman query that
// makes each: explicitly installed packages from the repositories, and those
// from elsewhere, such as the AUR.
var packageLists = []struct{ name, query string }{
	{"pkglist.txt", "-Qqen"},
	{"aurpkglist.txt", "-Qqem"},
}

// refreshLists writes this machine's package lists into the repo, saving
// them as they were first.
func (p *Plan) refreshLists(ctx context.Context, d *dotfiles.Dotfiles, opts Options) error {
	if opts.PackageLists == "" {
		return nil
	}
	pacman := opts.Pacman
	if pacman == "" {
		found, err := exec.LookPath("pacman")
		if errors.Is(err, exec.ErrNotFound) {
			return nil // not an Arch machine
		}
		if err != nil {
			return err
		}
		pacman = found
	}
	dir := filepath.Join(d.Repo.Dir, filepath.FromSlash(opts.PackageLists), p.Host)
	for _, l := range packageLists {
		list, err := query(ctx, pacman, l.query)
		if err != nil {
			return err
		}
		name := filepath.Join(dir, l.name)
		old, err := os.ReadFile(name)
		existed := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		p.lists = append(p.lists, savedFile{name: name, existed: existed, data: old})
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(name, list, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// query runs a pacman query and returns the packages it lists, one a line.
// pacman exits 1 when there are none, which is not an error.
func query(ctx context.Context, pacman, flags string) ([]byte, error) {
	out, err := run.Output(exec.CommandContext(ctx, pacman, flags))
	runErr, _ := errors.AsType[*run.Error](err)
	exitErr, _ := errors.AsType[*exec.ExitError](err)
	if exitErr != nil && exitErr.ExitCode() == 1 && len(out) == 0 && !strings.Contains(runErr.Stderr, "error:") {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing packages: %w", err)
	}
	return out, nil
}

// Discard puts back the package lists Prepare wrote, for when nothing is
// committed.
func (p *Plan) Discard() error {
	var errs []error
	for _, f := range p.lists {
		if f.existed {
			errs = append(errs, os.WriteFile(f.name, f.data, 0o644))
		} else if err := os.Remove(f.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	p.lists = nil
	return errors.Join(errs...)
}

// Diff returns what the commit would contain, as a git diff.
func (p *Plan) Diff(ctx context.Context, d *dotfiles.Dotfiles) ([]byte, error) {
	return d.Repo.StagedDiff(ctx, p.paths)
}

// Commit commits the plan's files, and only those, with message.
func (p *Plan) Commit(ctx context.Context, d *dotfiles.Dotfiles, message string) error {
	if err := d.Repo.Commit(ctx, p.paths, message); err != nil {
		return err
	}
	p.lists = nil
	return nil
}

// DefaultMessage is the commit message used when none is given.
func DefaultMessage(host string, now time.Time) string {
	return "update from " + host + ", " + now.Format("2006-01-02 15:04")
}

// Package untrack stops the dotfiles repo tracking files: either deleting them
// on every machine, or leaving every machine's copy where it is.
package untrack

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jtieri/dot/internal/backup"
	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/dotfiles"
)

// A Mode says what becomes of the files the repo stops tracking.
type Mode int

const (
	// Remove deletes the files on every machine, this one included. Each
	// file is listed in the repo's .chezmoiremove, so that the other
	// machines delete it when they next apply the repo.
	Remove Mode = iota
	// Forget leaves every machine's copy where it is. The path is listed
	// in the repo's .chezmoiignore, so that the copies are not reported as
	// files the repo does not track.
	Forget
)

// listFile and listHeading name the repo file each mode lists its paths in,
// and the comment written above the first path dot adds to it.
var (
	listFile    = map[Mode]string{Remove: ".chezmoiremove", Forget: ".chezmoiignore"}
	listHeading = map[Mode]string{
		Remove: "# Removed from the repo by dot rm; every machine deletes these.",
		Forget: "# No longer tracked, by dot forget; each machine keeps its own copy.",
	}
)

// A Plan is what untracking one path would do.
type Plan struct {
	Mode Mode
	// Path is the untracked path, relative to the home directory.
	Path string
	// Files are the files and links the repo tracks at Path, relative to
	// the home directory.
	Files []string

	target string
	dir    bool
}

// Preview works out what untracking target, an absolute path in the home
// directory, would do, without changing anything. A reason it cannot be
// untracked is returned as an error.
func Preview(ctx context.Context, d *dotfiles.Dotfiles, target string, mode Mode) (*Plan, error) {
	rel, err := d.Target(target)
	if err != nil {
		return nil, err
	}
	src, err := d.Chezmoi.SourcePath(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("the repo does not track %s", dotfiles.Tilde(rel))
	}
	info, err := os.Lstat(src)
	if err != nil {
		return nil, err
	}
	managed, err := d.Chezmoi.Managed(ctx, "files,symlinks", target)
	if err != nil {
		return nil, err
	}
	p := &Plan{Mode: mode, Path: rel, target: target, dir: info.IsDir()}
	for _, name := range managed {
		file, err := d.Target(name)
		if err != nil {
			return nil, err
		}
		p.Files = append(p.Files, file)
	}
	for _, rel := range p.lines() {
		// chezmoi reads the list as a template.
		if strings.Contains(rel, "{{") {
			return nil, fmt.Errorf("%s has {{ in its name, which cannot be written in %s", rel, listFile[mode])
		}
	}
	return p, nil
}

// lines returns the paths written to the mode's list, relative to the home
// directory. A removed folder is listed file by file, so that other machines
// keep the folder and any files of their own in it; a forgotten folder is
// listed once, which covers everything in it.
func (p *Plan) lines() []string {
	if p.Mode == Remove && p.dir {
		return p.Files
	}
	return []string{p.Path}
}

// Options control Untrack.
type Options struct {
	// BackupDir is where this machine's copies of removed files are saved
	// first. If empty, they are not saved.
	BackupDir string
	Now       time.Time
}

// Untrack carries out p. For Remove it returns the folder this machine's
// copies were saved to, or "" if none were.
func (p *Plan) Untrack(ctx context.Context, d *dotfiles.Dotfiles, opts Options) (saved string, err error) {
	if p.Mode == Remove && opts.BackupDir != "" {
		if saved, err = backup.Save(opts.BackupDir, d.Home, p.Files, opts.Now); err != nil {
			return "", fmt.Errorf("saving copies of the files: %w", err)
		}
	}
	if err := d.Chezmoi.Forget(ctx, p.target); err != nil {
		return saved, err
	}
	list := filepath.Join(d.Source, listFile[p.Mode])
	if err := appendLines(list, listHeading[p.Mode], escapeAll(p.lines())); err != nil {
		return saved, fmt.Errorf("the repo no longer has %s, but listing it in %s failed: %w; "+
			"git status in %s shows what changed", dotfiles.Tilde(p.Path), listFile[p.Mode], err, d.Repo.Dir)
	}
	if p.Mode == Forget {
		return "", nil
	}
	if err := p.deleteHere(ctx, d); err != nil {
		return saved, err
	}
	return saved, nil
}

// deleteHere deletes the removed files on this machine as applying the repo
// would, and a removed folder once nothing is left in it.
func (p *Plan) deleteHere(ctx context.Context, d *dotfiles.Dotfiles) error {
	// chezmoi counts a removed file that is already gone as not managed.
	var targets []string
	for _, rel := range p.Files {
		name := filepath.Join(d.Home, rel)
		if _, err := os.Lstat(name); err == nil {
			targets = append(targets, name)
		}
	}
	if err := d.Chezmoi.Apply(ctx, chezmoi.ApplyOptions{Targets: targets, Force: true}); err != nil {
		return err
	}
	if !p.dir {
		return nil
	}
	// Folders inside it go first, deepest first.
	var dirs []string
	err := filepath.WalkDir(p.target, func(name string, e fs.DirEntry, err error) error {
		if err == nil && e.IsDir() {
			dirs = append(dirs, name)
		}
		return err
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, dir := range slices.Backward(dirs) {
		if err := os.Remove(dir); err != nil && !errors.Is(err, syscall.ENOTEMPTY) {
			return err
		}
	}
	return nil
}

// appendLines adds lines to the end of the file name, creating it if need
// be, with heading above them the first time. Lines already there are not
// added again.
func appendLines(name, heading string, lines []string) error {
	data, err := os.ReadFile(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	have := make(map[string]bool)
	for line := range strings.Lines(string(data)) {
		have[strings.TrimRight(line, "\n")] = true
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteString("\n")
	}
	if !have[heading] {
		if len(data) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(heading + "\n")
	}
	for _, line := range lines {
		if !have[line] {
			b.WriteString(line + "\n")
		}
	}
	return os.WriteFile(name, []byte(b.String()), 0o644)
}

func escapeAll(rels []string) []string {
	out := make([]string, len(rels))
	for i, rel := range rels {
		out[i] = escape(rel)
	}
	return out
}

// escape writes rel as a pattern in .chezmoiremove or .chezmoiignore that
// matches only rel. chezmoi takes a backslash before most of the characters
// its patterns give a meaning to, but not before a backslash itself or a
// leading # or !, which would start a comment or an exclusion; those are
// written as one-character classes instead.
func escape(rel string) string {
	var b strings.Builder
	for i, r := range rel {
		switch {
		case r == '\\':
			b.WriteString(`[\\]`)
		case strings.ContainsRune(`*?[]{}`, r):
			b.WriteRune('\\')
			b.WriteRune(r)
		case i == 0 && r == '#':
			b.WriteString("[#]")
		case i == 0 && r == '!':
			b.WriteString(`[\!]`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

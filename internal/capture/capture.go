// Package capture copies files changed in the home directory into the
// dotfiles repo.
package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jtieri/dot/internal/apply"
	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/git"
	"github.com/jtieri/dot/internal/run"
)

// Options control what may be captured.
type Options struct {
	// Protected are patterns, as path.Match takes them and relative to the
	// home directory, for paths that are never captured. A pattern that
	// matches a folder protects everything in it.
	Protected []string
	// ModifyTemplates are the files the repo keeps as modify templates.
	ModifyTemplates []ModifyTemplate
}

// A ModifyTemplate is a file the repo keeps as a chezmoi modify template, so
// that one part of it stays as each machine has it.
type ModifyTemplate struct {
	// File is relative to the home directory.
	File string
	// Keep matches the part of the file each machine keeps for itself.
	Keep *regexp.Regexp
}

// A Capturer captures paths in the home directory into the repo.
type Capturer struct {
	d    *dotfiles.Dotfiles
	opts Options
	// pending is what applying the repo would change.
	pending *apply.Plan
	// removed holds the absolute paths in the home directory that the
	// repo's .chezmoiremove deletes.
	removed []string
}

// New refreshes chezmoi's facts about the machine and gathers what every
// capture needs to know about the repo.
func New(ctx context.Context, d *dotfiles.Dotfiles, opts Options) (*Capturer, error) {
	plan, err := apply.Preview(ctx, d)
	if err != nil {
		return nil, err
	}
	removed, err := d.Chezmoi.Managed(ctx, "remove")
	if err != nil {
		return nil, err
	}
	return &Capturer{d: d, opts: opts, pending: plan, removed: removed}, nil
}

// Changed returns the files, relative to the home directory, changed on this
// machine since chezmoi last wrote them, other than those deleted here.
func (c *Capturer) Changed() []string { return c.localPaths(false) }

// Deleted returns the files, relative to the home directory, deleted on this
// machine since chezmoi last wrote them.
func (c *Capturer) Deleted() []string { return c.localPaths(true) }

func (c *Capturer) localPaths(deleted bool) []string {
	var paths []string
	for _, ch := range c.pending.Local() {
		if ch.DeletedHere == deleted {
			paths = append(paths, ch.Path)
		}
	}
	return paths
}

// A Plan is what capturing one path would change in the repo.
type Plan struct {
	// Path is the captured path, relative to the home directory.
	Path string
	// Diff is the change to the repo, as a git diff. It is empty when the
	// path already matches the repo.
	Diff []byte
	// New lists the files, relative to the home directory, that the repo
	// does not have yet and every machine will get.
	New []string
	// Unprivate lists the paths, relative to the home directory, that the
	// repo keeps readable only by their owner and would no longer.
	Unprivate []string
	// RepoNewer lists the files, relative to the home directory, that the
	// repo has changed or deleted since chezmoi last wrote them here.
	// Capturing replaces the repo's version.
	RepoNewer []string
	// Secret is what chezmoi's secret scanner found, or "".
	Secret string

	target string
	// For a modify template, the template and its contents before and
	// after. Otherwise the changes chezmoi add makes.
	template              string
	tmplBefore, tmplAfter []byte
	changes               []chezmoi.SourceChange
}

// Preview works out what capturing target, an absolute path in the home
// directory, would change in the repo, without changing anything. A reason
// the path cannot be captured is returned as an error.
func (c *Capturer) Preview(ctx context.Context, target string) (*Plan, error) {
	rel, err := c.check(target)
	if err != nil {
		return nil, err
	}
	p := &Plan{Path: rel, target: target}
	m, isTemplate := c.modifyTemplate(rel)
	for _, ch := range c.pending.Changes {
		// chezmoi cannot tell whether the repo changed a modify template
		// that was also changed here: what the template produces follows
		// the part each machine keeps, so it differs from what chezmoi
		// last wrote whenever that part changed, as after a theme switch.
		repoChanged := ch.Kind == apply.Update || ch.Kind == apply.Delete ||
			(ch.Kind == apply.Conflict && !isTemplate)
		if repoChanged && within(ch.Path, rel) {
			p.RepoNewer = append(p.RepoNewer, ch.Path)
		}
	}
	diff, err := c.dryRun(ctx, p)
	if err != nil {
		return nil, err
	}
	if isTemplate {
		return p, c.previewTemplate(ctx, p, m)
	}
	p.Diff, p.changes = diff.Text, diff.Changes
	if len(p.changes) == 0 {
		if _, err := c.d.Chezmoi.SourcePath(ctx, target); err != nil {
			return nil, fmt.Errorf("%s is ignored on this machine (see .chezmoiignore in the repo), so it cannot be captured",
				dotfiles.Tilde(rel))
		}
		return p, nil
	}
	return p, c.checkChanges(p)
}

// check refuses paths that are never captured, and returns target relative
// to the home directory.
func (c *Capturer) check(target string) (string, error) {
	rel, err := filepath.Rel(c.d.Home, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not in the home directory", target)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", errors.New("the home directory itself cannot be captured; name the files in it")
	}
	if within(target, c.d.Repo.Dir) || within(c.d.Repo.Dir, target) {
		return "", fmt.Errorf("%s is the dotfiles repo or holds it", dotfiles.Tilde(rel))
	}
	if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s does not exist on this machine; dot apply puts back the repo's version", dotfiles.Tilde(rel))
	} else if err != nil {
		return "", err
	}
	if err := c.checkProtected(target); err != nil {
		return "", err
	}
	for _, r := range c.removed {
		if within(r, target) {
			return "", fmt.Errorf("the repo's .chezmoiremove deletes %s on every machine; "+
				"take it off that list before capturing it", c.tilde(r))
		}
	}
	return rel, nil
}

// checkProtected refuses target if it, a folder it is in, or anything in it
// matches a protected pattern.
func (c *Capturer) checkProtected(target string) error {
	return filepath.WalkDir(target, func(name string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(c.d.Home, name)
		if err != nil {
			return err
		}
		for p := filepath.ToSlash(rel); p != "."; p = path.Dir(p) {
			for _, pattern := range c.opts.Protected {
				if ok, _ := path.Match(pattern, p); ok {
					return fmt.Errorf("%s is protected by the capture.protected setting, "+
						"so it is never copied into the repo", dotfiles.Tilde(filepath.ToSlash(rel)))
				}
			}
		}
		return nil
	})
}

// dryRun works out what chezmoi add would change for p, which also runs
// chezmoi's secret scanner on it. A finding is recorded in p, with what
// adding would change despite it.
func (c *Capturer) dryRun(ctx context.Context, p *Plan) (*chezmoi.SourceDiff, error) {
	// Force lets the dry run go past chezmoi's questions; dot asks its own.
	diff, err := c.d.Chezmoi.Add(ctx, p.target, chezmoi.AddOptions{DryRun: true, Force: true})
	if err == nil {
		return diff, nil
	}
	diff, ignoreErr := c.d.Chezmoi.Add(ctx, p.target, chezmoi.AddOptions{DryRun: true, Force: true, AllowSecrets: true})
	if ignoreErr != nil {
		return nil, ignoreErr
	}
	finding := strings.TrimPrefix(run.Reason(err), "chezmoi: ")
	p.Secret = strings.ReplaceAll(finding, c.d.Home+string(filepath.Separator), "~/")
	return diff, nil
}

func (c *Capturer) modifyTemplate(rel string) (ModifyTemplate, bool) {
	i := slices.IndexFunc(c.opts.ModifyTemplates, func(m ModifyTemplate) bool { return m.File == rel })
	if i < 0 {
		return ModifyTemplate{}, false
	}
	return c.opts.ModifyTemplates[i], true
}

// previewTemplate works out the rewritten modify template for p.
func (c *Capturer) previewTemplate(ctx context.Context, p *Plan, m ModifyTemplate) error {
	name := dotfiles.Tilde(p.Path)
	src, err := c.d.Chezmoi.SourcePath(ctx, p.target)
	if err != nil {
		return err
	}
	if _, attrs, _ := chezmoi.Parse(filepath.Base(src), false); !attrs.Modify {
		return fmt.Errorf("the repo should keep %s as a modify template, but it is %s; dot status says more",
			name, c.repoPath(src))
	}
	file, err := os.ReadFile(p.target)
	if err != nil {
		return err
	}
	before, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	after, err := rewriteTemplate(before, file, m.Keep)
	if err != nil {
		return fmt.Errorf("cannot rewrite the template %s from %s: %w", c.repoPath(src), name, err)
	}
	p.template, p.tmplBefore, p.tmplAfter = src, before, after
	p.Diff, err = git.DiffContents(ctx, c.repoPath(src), before, after)
	return err
}

// checkChanges refuses changes that would turn a template, a modify script
// or a create_ file into a plain copy, and records the other changes that
// need a yes first.
func (c *Capturer) checkChanges(p *Plan) error {
	deleted := make(map[string]chezmoi.Attributes)
	for _, ch := range p.changes {
		from := ch.Path
		if ch.Kind == chezmoi.Renamed {
			from = ch.From
		} else if ch.Kind != chezmoi.Deleted {
			continue
		}
		target, attrs, ok := c.parse(from, ch.Kind == chezmoi.Renamed || ch.Dir)
		if !ok {
			continue
		}
		_, isTemplate := c.modifyTemplate(target)
		switch {
		case isTemplate:
			return fmt.Errorf("the repo keeps %s as a modify template; capture it on its own, "+
				"so that dot rewrites the template", dotfiles.Tilde(target))
		case attrs.Template || attrs.Modify:
			return fmt.Errorf("the repo makes %s from %s, and capturing would replace it with a plain copy; "+
				"edit that file instead", dotfiles.Tilde(target), c.repoPath(from))
		case attrs.Create:
			return fmt.Errorf("the repo only creates %s when it is missing, so changes to it stay on this machine; "+
				"to change what new machines get, edit %s", dotfiles.Tilde(target), c.repoPath(from))
		}
		deleted[target] = attrs
	}
	for _, ch := range p.changes {
		if ch.Kind != chezmoi.Created && ch.Kind != chezmoi.Renamed {
			continue
		}
		target, attrs, ok := c.parse(ch.Path, ch.Kind == chezmoi.Renamed || ch.Dir)
		if !ok {
			continue
		}
		before, existed := deleted[target]
		switch {
		case !existed && !ch.Dir:
			p.New = append(p.New, target)
		case existed && before.Private && !attrs.Private:
			p.Unprivate = append(p.Unprivate, target)
		}
	}
	return nil
}

// Capture carries out p. It then checks that the repo produces what is in
// the home directory, and puts the repo back as it was if not. reset lists
// the files whose permissions the repo cannot record, which dot apply will
// change: the repo records only whether a file is private, read-only or
// executable.
func (c *Capturer) Capture(ctx context.Context, p *Plan) (reset []string, err error) {
	if p.template != "" {
		return c.captureTemplate(ctx, p)
	}
	saved, err := save(p.changes)
	if err != nil {
		return nil, err
	}
	diff, err := c.d.Chezmoi.Add(ctx, p.target, chezmoi.AddOptions{
		Force:        len(p.Unprivate) > 0,
		AllowSecrets: p.Secret != "",
	})
	if err != nil {
		return nil, c.putBack(err, c.restore(p.changes, saved))
	}
	if !slices.Equal(diff.Changes, p.changes) {
		// Entries chezmoi created are known not to have existed.
		for _, ch := range diff.Changes {
			if _, ok := saved[ch.Path]; !ok && ch.Kind == chezmoi.Created {
				saved[ch.Path] = savedEntry{}
			}
		}
		err = fmt.Errorf("%s changed while it was being captured", dotfiles.Tilde(p.Path))
		return nil, c.putBack(err, c.restore(diff.Changes, saved))
	}
	if err := c.verify(ctx, p); err != nil {
		return nil, c.putBack(err, c.restore(p.changes, saved))
	}
	return c.resetPermissions(ctx, p)
}

// ErrRestored is wrapped in the error from a capture that failed after
// changing the repo, once the repo has been put back as it was.
var ErrRestored = errors.New("the repo was put back as it was")

// putBack returns err, the reason a capture failed, with the outcome of
// putting the repo back as it was.
func (c *Capturer) putBack(err, restoreErr error) error {
	if restoreErr != nil {
		return errors.Join(err, restoreErr)
	}
	return fmt.Errorf("%w; %w", err, ErrRestored)
}

func (c *Capturer) captureTemplate(ctx context.Context, p *Plan) ([]string, error) {
	if err := writeFileAtomic(p.template, p.tmplAfter); err != nil {
		return nil, err
	}
	if err := c.verify(ctx, p); err != nil {
		var restoreErr error
		if writeErr := writeFileAtomic(p.template, p.tmplBefore); writeErr != nil {
			restoreErr = fmt.Errorf("putting back %s failed: %w", c.repoPath(p.template), writeErr)
		}
		return nil, c.putBack(err, restoreErr)
	}
	return c.resetPermissions(ctx, p)
}

// verify checks that the repo now produces each captured file as it is in
// the home directory.
func (c *Capturer) verify(ctx context.Context, p *Plan) error {
	targets := []string{p.Path}
	if p.template == "" {
		targets = targets[:0]
		for _, ch := range p.changes {
			if (ch.Kind == chezmoi.Created || ch.Kind == chezmoi.Modified) && !ch.Dir {
				if target, _, ok := c.parse(ch.Path, false); ok {
					targets = append(targets, target)
				}
			}
		}
	}
	for _, rel := range targets {
		name := filepath.Join(c.d.Home, rel)
		have, err := contents(name)
		if err != nil {
			return err
		}
		repo, err := c.d.Chezmoi.Cat(ctx, name)
		if err != nil {
			return err
		}
		if !bytes.Equal(repo, have) {
			return fmt.Errorf("after capturing, the repo does not produce %s as it is here", dotfiles.Tilde(rel))
		}
	}
	return nil
}

// contents returns a file's contents, or a symbolic link's target the way
// chezmoi cat prints it.
func contents(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(name)
		return []byte(target + "\n"), err
	}
	return os.ReadFile(name)
}

// resetPermissions returns the paths in p that applying the repo would still
// change. The repo has just taken their contents from them, so only their
// permissions can differ.
func (c *Capturer) resetPermissions(ctx context.Context, p *Plan) ([]string, error) {
	entries, err := c.d.Chezmoi.Status(ctx, p.target)
	if err != nil {
		return nil, err
	}
	var reset []string
	for _, e := range entries {
		if e.Apply == 'M' {
			reset = append(reset, e.Path)
		}
	}
	return reset, nil
}

// parse returns the target, relative to the home directory, and the
// attributes of name, an absolute path in the source directory.
func (c *Capturer) parse(name string, dir bool) (string, chezmoi.Attributes, bool) {
	rel, err := filepath.Rel(c.d.Source, name)
	if err != nil {
		return "", chezmoi.Attributes{}, false
	}
	return chezmoi.Parse(filepath.ToSlash(rel), dir)
}

// repoPath returns name, an absolute path in the repo, relative to the top of
// the repo.
func (c *Capturer) repoPath(name string) string {
	rel, err := filepath.Rel(c.d.Repo.Dir, name)
	if err != nil {
		return name
	}
	return filepath.ToSlash(rel)
}

// tilde writes name, an absolute path in the home directory, as ~/path.
func (c *Capturer) tilde(name string) string {
	rel, err := filepath.Rel(c.d.Home, name)
	if err != nil {
		return name
	}
	return dotfiles.Tilde(filepath.ToSlash(rel))
}

// within reports whether name is dir or inside it. Both are slash-separated
// or both are absolute.
func within(name, dir string) bool {
	return name == dir || strings.HasPrefix(name, strings.TrimSuffix(dir, "/")+"/")
}

// A savedEntry is an entry in the source directory as it was before a
// capture.
type savedEntry struct {
	exists bool
	mode   fs.FileMode
	data   []byte
}

// save records the entries that changes touch.
func save(changes []chezmoi.SourceChange) (map[string]savedEntry, error) {
	saved := make(map[string]savedEntry)
	for _, ch := range changes {
		for _, name := range []string{ch.Path, ch.From} {
			if name == "" {
				continue
			}
			info, err := os.Lstat(name)
			if errors.Is(err, fs.ErrNotExist) {
				saved[name] = savedEntry{}
				continue
			}
			if err != nil {
				return nil, err
			}
			e := savedEntry{exists: true, mode: info.Mode()}
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				target, err := os.Readlink(name)
				if err != nil {
					return nil, err
				}
				e.data = []byte(target)
			case info.Mode().IsRegular():
				if e.data, err = os.ReadFile(name); err != nil {
					return nil, err
				}
			}
			saved[name] = e
		}
	}
	return saved, nil
}

// restore undoes changes, latest first, putting back the entries saved
// before them.
func (c *Capturer) restore(changes []chezmoi.SourceChange, saved map[string]savedEntry) error {
	var errs []error
	for _, ch := range slices.Backward(changes) {
		if ch.Kind == chezmoi.Renamed {
			if _, err := os.Lstat(ch.From); errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, os.Rename(ch.Path, ch.From))
			}
			continue
		}
		e, ok := saved[ch.Path]
		if !ok {
			errs = append(errs, fmt.Errorf("%s changed and was not saved", c.repoPath(ch.Path)))
			continue
		}
		errs = append(errs, put(ch.Path, e))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("putting the repo back failed, so it may be partly changed; "+
			"git status in %s shows what changed: %w", c.d.Repo.Dir, err)
	}
	return nil
}

// put makes the entry at name as e records it. chezmoi add only ever creates
// or renames directories, so e is never a directory that exists.
func put(name string, e savedEntry) error {
	if !e.exists {
		err := os.Remove(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(name); err != nil {
		return err
	}
	if e.mode&fs.ModeSymlink != 0 {
		return os.Symlink(string(e.data), name)
	}
	return os.WriteFile(name, e.data, e.mode.Perm())
}

// writeFileAtomic replaces the file name with data, keeping its permissions,
// so that it is never seen half written.
func writeFileAtomic(name string, data []byte) error {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".dot-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	err = errors.Join(err, f.Chmod(info.Mode().Perm()), f.Close())
	if err == nil {
		err = os.Rename(tmp, name)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// Package status works out whether this machine's dotfiles are in sync with
// the dotfiles repo and with the other machines.
package status

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/git"
	"github.com/jtieri/dot/internal/run"
)

// Version is the version of the report's JSON form. It changes only when a
// field is removed or changes meaning.
const Version = 1

// Options control what Check looks at. dot fills them from its settings.
type Options struct {
	// Branch is the branch every machine should be on.
	Branch string
	// FetchTimeout limits how long the fetch from the remote may take.
	FetchTimeout time.Duration
	// SkipDirs are folders, relative to the home directory, that the leftovers check
	// does not look in, because they hold personal files beside tracked
	// ones. "." is Home itself.
	SkipDirs []string
	// ModifyTemplates are files, relative to the home directory, that the repo must keep
	// as chezmoi modify templates. A plain copy in their place is reported.
	ModifyTemplates []string
}

// A Report holds everything Check found. Paths in the home directory are
// relative to it.
type Report struct {
	Version int  `json:"version"`
	InSync  bool `json:"in_sync"`
	Repo    Repo `json:"repo"`
	// Home lists the differences between the home directory and the repo.
	Home []HomeChange `json:"home"`
	// SetupSteps lists chezmoi scripts waiting to run.
	SetupSteps []string `json:"setup_steps"`
	// Leftovers lists files and folders inside tracked folders that the
	// repo does not track.
	Leftovers []string `json:"leftovers"`
	// ReplacedTemplates lists files from Options.ModifyTemplates that the
	// repo now holds as plain copies.
	ReplacedTemplates []string `json:"replaced_templates"`
}

// Repo describes the dotfiles repo.
type Repo struct {
	Path string `json:"path"`
	// Branch is empty when HEAD is detached.
	Branch     string `json:"branch"`
	WantBranch string `json:"want_branch"`
	NoUpstream bool   `json:"no_upstream"`
	// Ahead counts commits not pushed, Behind commits not pulled.
	Ahead       int           `json:"ahead"`
	Behind      int           `json:"behind"`
	Fetch       Fetch         `json:"fetch"`
	Uncommitted []Uncommitted `json:"uncommitted"`
}

// Fetch is the outcome of fetching from the remote. When it failed, Ahead and
// Behind are as of LastFetch.
type Fetch struct {
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	LastFetch time.Time `json:"last_fetch,omitzero"`
}

// Uncommitted is a change in the repo that has not been committed.
type Uncommitted struct {
	// Change is "modified", "new", "deleted" or "renamed".
	Change string `json:"change"`
	// HomePath is the file in the home directory the repo file produces,
	// or empty when it produces none.
	HomePath string `json:"home_path,omitempty"`
	// RepoPath is relative to the top of the repo.
	RepoPath string `json:"repo_path"`
	// From is the old repo path of a renamed file.
	From string `json:"from,omitempty"`
}

// Kinds of HomeChange.
const (
	ChangedHere   = "changed_here"
	DeletedHere   = "deleted_here"
	RepoNewer     = "repo_newer"
	NewInRepo     = "new_in_repo"
	RemovedInRepo = "removed_in_repo"
)

// HomeChange is a file in the home directory that differs from the repo.
type HomeChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// Check gathers the report on d. The fetch runs alongside the local checks.
// A failed fetch is recorded in the report rather than returned, since the
// local checks still hold.
func Check(ctx context.Context, d *dotfiles.Dotfiles, opts Options) (*Report, error) {
	repo := d.Repo
	r := &Report{Version: Version}
	r.Repo.Path = repo.Dir
	r.Repo.WantBranch = opts.Branch

	var wg sync.WaitGroup
	var fetchErr error
	wg.Go(func() {
		fctx, cancel := context.WithTimeout(ctx, opts.FetchTimeout)
		defer cancel()
		fetchErr = repo.Fetch(fctx)
		if fetchErr != nil && errors.Is(fctx.Err(), context.DeadlineExceeded) {
			fetchErr = fmt.Errorf("no answer from the remote within %v", opts.FetchTimeout)
		}
	})
	err := r.checkLocal(ctx, d, opts)
	wg.Wait()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.Repo.Fetch.OK = fetchErr == nil
	if fetchErr != nil {
		r.Repo.Fetch.Error = run.Reason(fetchErr)
	}
	if r.Repo.Fetch.LastFetch, err = repo.LastFetch(ctx); err != nil {
		return nil, err
	}
	r.Repo.Ahead, r.Repo.Behind, err = repo.AheadBehind(ctx)
	if errors.Is(err, git.ErrNoUpstream) {
		r.Repo.NoUpstream = true
	} else if err != nil {
		return nil, err
	}
	r.InSync = r.inSync()
	return r, nil
}

func (r *Report) checkLocal(ctx context.Context, d *dotfiles.Dotfiles, opts Options) error {
	var err error
	if r.Repo.Branch, err = d.Repo.Branch(ctx); err != nil {
		return err
	}
	if err := r.checkUncommitted(ctx, d); err != nil {
		return err
	}
	if err := r.checkHome(ctx, d); err != nil {
		return err
	}
	if err := r.checkLeftovers(ctx, d, opts); err != nil {
		return err
	}
	return r.checkTemplates(ctx, d, opts)
}

func (r *Report) checkUncommitted(ctx context.Context, d *dotfiles.Dotfiles) error {
	changes, err := d.Repo.Status(ctx)
	if err != nil {
		return err
	}
	for _, c := range changes {
		u := Uncommitted{RepoPath: c.Path, From: c.OrigPath}
		switch {
		case c.Index == 'R':
			u.Change = "renamed"
		case c.Worktree == 'D' || c.Index == 'D':
			u.Change = "deleted"
		case c.Index == '?' || c.Index == 'A':
			u.Change = "new"
		default:
			u.Change = "modified"
		}
		u.HomePath, _ = d.HomePath(c.Path)
		r.Repo.Uncommitted = append(r.Repo.Uncommitted, u)
	}
	return nil
}

func (r *Report) checkHome(ctx context.Context, d *dotfiles.Dotfiles) error {
	entries, err := d.Chezmoi.Status(ctx)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Apply == 'R' {
			r.SetupSteps = append(r.SetupSteps, strings.TrimPrefix(e.Path, ".chezmoiscripts/"))
			continue
		}
		var kind string
		switch {
		case e.Local == 'D':
			kind = DeletedHere
		case e.Local != ' ':
			kind = ChangedHere
		case e.Apply == 'A':
			kind = NewInRepo
		case e.Apply == 'D':
			kind = RemovedInRepo
		default:
			kind = RepoNewer
		}
		r.Home = append(r.Home, HomeChange{Path: e.Path, Kind: kind})
	}
	return nil
}

// checkLeftovers looks directly inside each managed folder, other than the
// skipped ones, for entries the repo does not track. Empty folders are left
// out: apps create them, and they hold nothing worth keeping.
func (r *Report) checkLeftovers(ctx context.Context, d *dotfiles.Dotfiles, opts Options) error {
	managed, err := d.Chezmoi.ManagedDirs(ctx)
	if err != nil {
		return err
	}
	skip := make(map[string]bool)
	for _, dir := range opts.SkipDirs {
		skip[filepath.Join(d.Home, dir)] = true
	}
	checked := make(map[string]bool)
	var dirs []string
	for _, dir := range managed {
		dir = filepath.Clean(dir)
		if skip[dir] {
			continue
		}
		// chezmoi fails on a managed folder that is missing or no longer a
		// folder. Those show up as differences instead.
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			continue
		}
		checked[dir] = true
		dirs = append(dirs, dir)
	}
	unmanaged, err := d.Chezmoi.Unmanaged(ctx, dirs)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, p := range unmanaged {
		if !checked[filepath.Dir(p)] || seen[p] || emptyDir(p) {
			continue
		}
		seen[p] = true
		rel, err := filepath.Rel(d.Home, p)
		if err != nil {
			return err
		}
		r.Leftovers = append(r.Leftovers, rel)
	}
	slices.Sort(r.Leftovers)
	return nil
}

func emptyDir(name string) bool {
	info, err := os.Lstat(name)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(name)
	return err == nil && len(entries) == 0
}

func (r *Report) checkTemplates(ctx context.Context, d *dotfiles.Dotfiles, opts Options) error {
	for _, target := range opts.ModifyTemplates {
		src, err := d.Chezmoi.SourcePath(ctx, filepath.Join(d.Home, target))
		if err != nil {
			// Not managed on this machine, for example ignored here.
			continue
		}
		if !strings.HasPrefix(filepath.Base(src), "modify_") {
			r.ReplacedTemplates = append(r.ReplacedTemplates, target)
		}
	}
	return nil
}

func (r *Report) inSync() bool {
	return r.Repo.Branch == r.Repo.WantBranch &&
		!r.Repo.NoUpstream &&
		r.Repo.Ahead == 0 && r.Repo.Behind == 0 &&
		len(r.Repo.Uncommitted) == 0 &&
		len(r.Home) == 0 &&
		len(r.SetupSteps) == 0 &&
		len(r.Leftovers) == 0 &&
		len(r.ReplacedTemplates) == 0
}

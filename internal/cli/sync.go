package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/git"
	"github.com/jtieri/dot/internal/run"
	"github.com/jtieri/dot/internal/text"
)

func newSyncCmd(a *app) *cobra.Command {
	var flags applyFlags
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull changes from the other machines and apply them",
		Long: `Pull the changes other machines have pushed, then make the home directory
match the repo the same way dot apply does; see dot apply --help.

The pull only fast-forwards. dot stops before applying anything when this
machine and the remote both have commits the other lacks, or when the pull
would overwrite uncommitted changes in the repo. When the remote cannot be
reached, dot offers to apply what the repo already has.

Canceling the apply leaves the repo pulled and the home directory unchanged.

Exits 0 when the repo was pulled and applied, 1 when the apply was canceled,
and 2 on errors, including when nothing could be pulled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.sync(cmd.Context(), flags)
		},
	}
	flags.register(cmd)
	return cmd
}

func (a *app) sync(ctx context.Context, flags applyFlags) error {
	repo := a.dotfiles.Repo
	branch, err := repo.Branch(ctx)
	if err != nil {
		return err
	}
	if branch == "" {
		return fmt.Errorf("the repo is not on a branch, so there is nothing to pull; switch to %s with git switch",
			a.cfg.Branch)
	}
	if branch != a.cfg.Branch {
		fmt.Fprintf(a.streams.ErrOut, "The repo is on branch %s, not %s.\n", branch, a.cfg.Branch)
	}

	if err := repo.Fetch(ctx, a.cfg.FetchTimeout); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(a.streams.ErrOut, "Could not fetch from the remote: %s.\n"+
			"Showing what the repo already has.\n\n", run.Reason(err))
		if err := a.apply(ctx, flags); err != nil && !errors.Is(err, errOutOfSync) {
			return err
		}
		return errors.New("nothing was pulled from the remote")
	}

	pulled, err := repo.FastForward(ctx)
	if err != nil {
		return a.pullError(branch, err)
	}
	if pulled == 0 {
		fmt.Fprintln(a.streams.Out, "Nothing to pull.")
	} else {
		fmt.Fprintf(a.streams.Out, "Pulled %s.\n", text.Pluralize(pulled, "commit"))
	}
	// The blank line sets the pull apart from the apply's preview.
	fmt.Fprintln(a.streams.Out)
	return a.apply(ctx, flags)
}

// pullError explains why the fast-forward failed and what to do about it.
func (a *app) pullError(branch string, err error) error {
	if errors.Is(err, git.ErrNoUpstream) {
		return fmt.Errorf("branch %s does not track a remote branch, so there is nothing to pull; "+
			"set one with git branch --set-upstream-to", branch)
	}
	if divErr, ok := errors.AsType[*git.DivergedError](err); ok {
		return fmt.Errorf("nothing was pulled, because this machine and the remote both have new commits "+
			"(%s not pushed, %s not pulled); combine them with git, for example git pull --rebase, "+
			"then run dot sync again",
			text.Pluralize(divErr.Ahead, "commit"), text.Pluralize(divErr.Behind, "commit"))
	}
	if blockedErr, ok := errors.AsType[*git.BlockedError](err); ok {
		files := make([]string, len(blockedErr.Paths))
		for i, p := range blockedErr.Paths {
			files[i] = a.repoFile(p)
		}
		return fmt.Errorf("nothing was pulled, because it would overwrite uncommitted changes in the repo to %s; "+
			"commit or discard them, then run dot sync again", strings.Join(files, ", "))
	}
	return err
}

// repoFile names a file in the repo the way dot status does: by the file it
// produces in the home directory, or by its path in the repo if there is none.
func (a *app) repoFile(repoPath string) string {
	if rel, ok := a.dotfiles.HomePath(repoPath); ok {
		return dotfiles.Tilde(rel)
	}
	return repoPath
}

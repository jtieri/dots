package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/apply"
	"github.com/jtieri/dot/internal/backup"
	"github.com/jtieri/dot/internal/iostreams"
)

// applyFlags answer apply's questions in advance, for use without a terminal.
type applyFlags struct {
	yes            bool
	keepLocal      bool
	overwriteLocal bool
}

func newApplyCmd(a *app) *cobra.Command {
	var flags applyFlags
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Make the home directory match the repo",
		Long: `Make the home directory match the repo. dot first shows what would change
and asks before changing anything.

Files changed on this machine since chezmoi last wrote them are listed on
their own, and dot asks whether to keep them or overwrite them. Overwritten
files are copied to $XDG_STATE_HOME/dot/overwritten first, unless
overwrite_backup.enabled is false.

Without a terminal dot cannot ask, so --yes is needed, together with
--keep-local or --overwrite-local when files changed here would be replaced.

Exits 0 when the repo was applied or there was nothing to apply, 1 when it
was canceled, and 2 on errors.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.apply(cmd.Context(), flags)
		},
	}
	cmd.Flags().BoolVarP(&flags.yes, "yes", "y", false, "apply without asking")
	cmd.Flags().BoolVar(&flags.keepLocal, "keep-local", false,
		"keep files changed on this machine and apply the rest, without asking")
	cmd.Flags().BoolVar(&flags.overwriteLocal, "overwrite-local", false,
		"overwrite files changed on this machine too, without asking")
	cmd.MarkFlagsMutuallyExclusive("keep-local", "overwrite-local")
	return cmd
}

func (a *app) apply(ctx context.Context, flags applyFlags) error {
	d := a.dotfiles
	plan, err := apply.Preview(ctx, d)
	if err != nil {
		return err
	}
	if len(plan.Changes) == 0 {
		fmt.Fprintln(a.streams.Out, "Nothing to apply; the home directory matches the repo.")
		return nil
	}
	if err := plan.WriteText(a.streams.Out); err != nil {
		return err
	}

	choice, ok, err := a.chooseApply(ctx, plan, flags)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(a.streams.ErrOut, "Nothing changed.")
		return errOutOfSync
	}

	backupDir := backup.DefaultDir(d.Home)
	opts := apply.Options{
		Choice: choice,
		Now:    time.Now(),
		Stdout: a.streams.ErrOut,
		Stderr: a.streams.ErrOut,
	}
	if a.cfg.OverwriteBackup.Enabled {
		opts.BackupDir = backupDir
	}
	folder, err := plan.Apply(ctx, d, opts)
	if folder != "" {
		fmt.Fprintln(a.streams.ErrOut, "Copies of the overwritten files are in", folder)
	}
	if err != nil {
		return err
	}
	if err := backup.Prune(backupDir, a.cfg.OverwriteBackup.KeepDays, opts.Now); err != nil {
		return fmt.Errorf("deleting old copies of overwritten files: %w", err)
	}
	return a.reportApplied(ctx)
}

// chooseApply decides what to do about the plan: from the flags if they
// answer, otherwise by asking. ok is false when the apply is canceled.
func (a *app) chooseApply(ctx context.Context, plan *apply.Plan, flags applyFlags) (choice apply.Choice, ok bool, err error) {
	local := len(plan.Local()) > 0
	switch {
	case flags.keepLocal:
		return apply.Keep, true, nil
	case flags.overwriteLocal:
		return apply.Overwrite, true, nil
	case flags.yes && local:
		return 0, false, errors.New("files changed on this machine would be replaced; " +
			"add --keep-local or --overwrite-local to say what to do with them")
	case flags.yes:
		return apply.Keep, true, nil
	case !a.streams.CanPrompt():
		return 0, false, errors.New("nothing applied: there is no terminal to ask at; use --yes, " +
			"with --keep-local or --overwrite-local if files changed here would be replaced")
	}

	question := "\nApply these changes? [y/d/N] "
	answers := map[string]apply.Choice{"y": apply.Keep, "yes": apply.Keep}
	if local {
		overwrite := "overwrite yours"
		if a.cfg.OverwriteBackup.Enabled {
			overwrite += " (copies are kept first)"
		}
		fmt.Fprintf(a.streams.ErrOut, "\nFiles changed on this machine would be replaced.\n"+
			"  [k] keep yours and apply the rest\n"+
			"  [o] %s\n"+
			"  [d] show the full diff\n"+
			"  [n] cancel (default)\n", overwrite)
		question = "Choice [k/o/d/N]: "
		answers = map[string]apply.Choice{"k": apply.Keep, "o": apply.Overwrite}
	}
	for {
		answer, err := a.streams.Prompt(ctx, question)
		if errors.Is(err, iostreams.ErrNoAnswer) {
			return 0, false, errors.New("nothing applied: no answer was given")
		}
		if err != nil {
			return 0, false, err
		}
		if choice, ok := answers[answer]; ok {
			return choice, true, nil
		}
		switch answer {
		case "", "n", "no":
			return 0, false, nil
		case "d":
			diff, err := plan.Diff(ctx, a.dotfiles)
			if err != nil {
				return 0, false, err
			}
			if err := a.streams.Page(ctx, diff); err != nil {
				return 0, false, err
			}
		default:
			fmt.Fprintln(a.streams.ErrOut, "Please answer with one of the letters in brackets.")
		}
	}
}

// reportApplied says in one line whether the home directory now matches the
// repo. It differs only where files changed here were kept.
func (a *app) reportApplied(ctx context.Context) error {
	entries, err := a.dotfiles.Chezmoi.Status(ctx)
	if err != nil {
		return err
	}
	differ := 0
	for _, e := range entries {
		if e.Apply != ' ' {
			differ++
		}
	}
	// The blank line sets the outcome apart from the preview above it.
	switch differ {
	case 0:
		fmt.Fprintln(a.streams.Out, "\nDone. The home directory matches the repo.")
	case 1:
		fmt.Fprintln(a.streams.Out, "\nDone. 1 file still differs from the repo; dot status lists it.")
	default:
		fmt.Fprintf(a.streams.Out, "\nDone. %d files still differ from the repo; dot status lists them.\n", differ)
	}
	return nil
}

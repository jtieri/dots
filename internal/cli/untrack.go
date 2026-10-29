package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/backup"
	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/untrack"
)

func newRmCmd(a *app) *cobra.Command {
	return newUntrackCmd(a, untrack.Remove, &cobra.Command{
		Use:   "rm file...",
		Short: "Stop tracking files and delete them on every machine",
		Long: `Stop tracking files and delete them on every machine, this one included.

dot takes them out of the repo and lists them in the repo's .chezmoiremove,
so each machine deletes them the next time it applies the repo, after its
preview says so. A folder is listed file by file, so other machines keep the
folder and any files of their own in it. This machine's copies are saved to
$XDG_STATE_HOME/dot/overwritten first, unless overwrite_backup.enabled is
false. dot never commits; dot push does.

Without a terminal dot cannot ask, so --yes is needed.

Exits 0 when the files were removed, 1 when it was canceled, and 2 on
errors.`,
	})
}

func newForgetCmd(a *app) *cobra.Command {
	return newUntrackCmd(a, untrack.Forget, &cobra.Command{
		Use:   "forget file...",
		Short: "Stop tracking files but keep every machine's copy",
		Long: `Stop tracking files, leaving every machine's copy where it is.

dot takes them out of the repo and lists them in the repo's .chezmoiignore,
so the copies are not reported as files the repo does not track. dot never
commits; dot push does.

Without a terminal dot cannot ask, so --yes is needed.

Exits 0 when the files were forgotten, 1 when it was canceled, and 2 on
errors.`,
	})
}

func newUntrackCmd(a *app, mode untrack.Mode, cmd *cobra.Command) *cobra.Command {
	var yes bool
	cmd.Args = cobra.MinimumNArgs(1)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return a.untrack(cmd.Context(), args, mode, yes)
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "go ahead without asking")
	return cmd
}

// untrackWords are what dot rm and dot forget say about what they do.
var untrackWords = map[untrack.Mode]struct {
	heading, verb, done string
}{
	untrack.Remove: {"Will delete from the repo and from every machine, this one included", "Remove", "Removed"},
	untrack.Forget: {"Will stop tracking; every machine keeps its own copy", "Forget", "Stopped tracking"},
}

func (a *app) untrack(ctx context.Context, args []string, mode untrack.Mode, yes bool) error {
	d := a.dotfiles
	words := untrackWords[mode]
	var plans []*untrack.Plan
	var files []string
	var failed bool
	for _, arg := range args {
		target, err := absHomePath(arg, d.Home)
		if err != nil {
			return err
		}
		p, err := untrack.Preview(ctx, d, target, mode)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			a.printError(err)
			failed = true
			continue
		}
		plans = append(plans, p)
		if len(p.Files) == 0 {
			files = append(files, dotfiles.Tilde(p.Path)) // an empty folder
		}
		files = append(files, tildes(p.Files)...)
	}
	if len(plans) == 0 {
		return errReported
	}

	writeList(a.streams.Out, files, words.heading, words.heading)
	ok, err := a.confirmUntrack(ctx, words.verb, len(files), yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(a.streams.ErrOut, "Nothing changed.")
		return errOutOfSync
	}

	opts := untrack.Options{Now: time.Now()}
	backupDir := backup.DefaultDir(d.Home)
	if a.cfg.OverwriteBackup.Enabled {
		opts.BackupDir = backupDir
	}
	var done bool
	for _, p := range plans {
		saved, err := p.Untrack(ctx, d, opts)
		if saved != "" {
			fmt.Fprintf(a.streams.ErrOut, "Copies of this machine's %s are in %s\n", dotfiles.Tilde(p.Path), saved)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			a.printError(err)
			failed = true
			continue
		}
		fmt.Fprintf(a.streams.Out, "%s %s.\n", words.done, dotfiles.Tilde(p.Path))
		done = true
	}
	if done {
		if err := backup.Prune(backupDir, a.cfg.OverwriteBackup.KeepDays, opts.Now); err != nil {
			return fmt.Errorf("deleting old copies of overwritten files: %w", err)
		}
		fmt.Fprintln(a.streams.Out, "\nRun dot push to commit and share the change.")
	}
	if failed {
		return errReported
	}
	return nil
}

// confirmUntrack decides whether to go ahead: from --yes if given, otherwise
// by asking, with verb saying what would be done to the files.
func (a *app) confirmUntrack(ctx context.Context, verb string, files int, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !a.streams.CanPrompt() {
		return false, errors.New("nothing changed: there is no terminal to ask at; use --yes")
	}
	question := verb + " it? [y/N] "
	if files > 1 {
		question = verb + " them? [y/N] "
	}
	for {
		answer, err := a.streams.Prompt(ctx, question)
		if err != nil {
			return false, noAnswer(err, "nothing changed")
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		}
		fmt.Fprintln(a.streams.ErrOut, "Please answer with one of the letters in brackets.")
	}
}

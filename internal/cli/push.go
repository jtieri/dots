package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/git"
	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/push"
	"github.com/jtieri/dot/internal/run"
	"github.com/jtieri/dot/internal/text"
)

func newPushCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "push [message]",
		Short: "Commit the repo's changes and push them to the other machines",
		Long: `Commit the changes in the dotfiles repo and push them, so that dot sync
brings them to your other machines.

dot fetches first, and stops if the remote has commits this machine has not
pulled; dot sync pulls them. It refreshes this machine's package lists in
the repo (see push.package_lists), shows what the commit would contain, and
asks for a message, "update from <host>, <date and time>" unless you give
one, before asking to commit. Commits made earlier and not yet pushed are
pushed too. Files changed on this machine but not captured are listed and
left out; dot capture copies them into the repo.

When the remote cannot be reached, dot offers to commit without pushing, and
the next dot push sends the commit.

Without a terminal dot cannot ask, so --yes is needed, and the message is the
one given or the default.

Exits 0 when everything was pushed or there was nothing to push, 1 when it
was canceled, and 2 on errors, including a commit that could not be pushed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var message string
			if len(args) == 1 {
				message = args[0]
			}
			return a.push(cmd.Context(), message, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "commit and push without asking")
	return cmd
}

func (a *app) push(ctx context.Context, message string, yes bool) error {
	d := a.dotfiles
	branch, err := a.checkBranch(ctx, "push")
	if err != nil {
		return err
	}
	fetchErr := d.Repo.Fetch(ctx, a.cfg.FetchTimeout)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	online := fetchErr == nil
	if !online {
		fmt.Fprintf(a.streams.ErrOut, "Could not fetch from the remote: %s.\n\n", run.Reason(fetchErr))
	}
	ahead, behind, err := d.Repo.AheadBehind(ctx)
	if errors.Is(err, git.ErrNoUpstream) {
		return fmt.Errorf("branch %s does not track a remote branch, so there is nothing to push to; "+
			"set one with git push --set-upstream origin %s", branch, branch)
	}
	if err != nil {
		return err
	}
	if behind > 0 {
		return fmt.Errorf("nothing was committed, because the remote has %s this machine has not pulled; "+
			"run dot sync, then dot push again", text.Pluralize(behind, "commit"))
	}

	plan, err := push.Prepare(ctx, d, push.Options{
		PackageLists: a.cfg.Push.PackageLists,
		Protected:    a.cfg.Capture.Protected,
	})
	if err != nil {
		return err
	}
	writeList(a.streams.ErrOut, tildes(plan.Uncaptured),
		"Changed on this machine but not captured, so left out; dot capture copies it into the repo",
		"Changed on this machine but not captured, so left out; dot capture copies them into the repo")
	if len(plan.Uncaptured) > 0 {
		fmt.Fprintln(a.streams.ErrOut)
	}
	if len(plan.Items) == 0 && ahead == 0 {
		fmt.Fprintln(a.streams.Out, "Nothing to push.")
		return nil
	}
	if len(plan.Items) == 0 && !online {
		return errors.New("nothing was pushed, because the remote could not be reached")
	}
	a.writePushPreview(plan, ahead)

	ok, err := a.confirmPush(ctx, plan, &message, online, yes)
	if err != nil || !ok {
		if discardErr := plan.Discard(); discardErr != nil {
			return errors.Join(err, discardErr)
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(a.streams.ErrOut, "Nothing was committed or pushed.")
		return errOutOfSync
	}

	// What went wrong after a commit is said after it, so the commit is
	// not taken for lost.
	notPushed := "nothing was pushed"
	if len(plan.Items) > 0 {
		if err := plan.Commit(ctx, d, message); err != nil {
			return err
		}
		fmt.Fprintf(a.streams.Out, "\nCommitted: %s\n", message)
		notPushed = "committed, but nothing was pushed"
		ahead++
	}
	if !online {
		return fmt.Errorf("%s, because the remote could not be reached; the next dot push sends it", notPushed)
	}
	if err := d.Repo.Push(ctx); err != nil {
		if errors.Is(err, git.ErrRejected) {
			return fmt.Errorf("%s, because the remote has new commits; run dot sync, then dot push again", notPushed)
		}
		return fmt.Errorf("%s: %s; dot push tries again", notPushed, run.Reason(err))
	}
	fmt.Fprintf(a.streams.Out, "Pushed %s.\n", text.Pluralize(ahead, "commit"))
	return nil
}

// writePushPreview shows what the commit would contain, grouped as dot
// apply's preview is, and the earlier commits that go with it.
func (a *app) writePushPreview(plan *push.Plan, ahead int) {
	var changed, added, deleted, renamed []string
	for _, it := range plan.Items {
		name := a.repoFile(it.RepoPath)
		switch {
		case it.Kind == push.Changed:
			changed = append(changed, name)
		case it.Kind == push.Moved && it.Note != "":
			changed = append(changed, name+", "+it.Note)
		case it.Kind == push.Moved:
			renamed = append(renamed, name+", from "+a.repoFile(it.From))
		case it.Kind == push.Added:
			added = append(added, name)
		case it.Kind == push.Deleted:
			deleted = append(deleted, name)
		}
	}
	w := a.streams.Out
	for _, group := range []struct {
		heading string
		items   []string
	}{
		{"Will commit changes to", changed},
		{"Will add to the repo", added},
		{"Will delete from the repo", deleted},
		{"Will rename in the repo", renamed},
	} {
		writeList(w, group.items, group.heading, group.heading)
	}
	switch {
	case ahead > 0 && len(plan.Items) > 0:
		fmt.Fprintf(w, "Will also push %s made earlier.\n", text.Pluralize(ahead, "commit"))
	case ahead > 0:
		fmt.Fprintf(w, "Will push %s made earlier.\n", text.Pluralize(ahead, "commit"))
	}
}

// confirmPush settles the commit message, from the argument, the default or
// an answer, and decides whether to go on: from --yes if given, otherwise by
// asking. ok is false when the push is canceled.
func (a *app) confirmPush(ctx context.Context, plan *push.Plan, message *string, online, yes bool) (ok bool, err error) {
	committing := len(plan.Items) > 0
	if committing && *message == "" {
		*message = push.DefaultMessage(plan.Host, time.Now())
		if !yes && a.streams.CanPrompt() {
			answer, err := a.streams.PromptLine(ctx, fmt.Sprintf("\nCommit message [%s]: ", *message))
			if err != nil {
				return false, a.noAnswer(err)
			}
			if answer != "" {
				*message = answer
			}
		}
	}
	if yes {
		return true, nil
	}
	if !a.streams.CanPrompt() {
		return false, errors.New("nothing committed or pushed: there is no terminal to ask at; use --yes")
	}
	question := "Push? [y/N] "
	switch {
	case committing && online:
		question = "Commit and push? [y/d/N] "
	case committing:
		question = "Commit without pushing? [y/d/N] "
	}
	for {
		answer, err := a.streams.Prompt(ctx, question)
		if err != nil {
			return false, a.noAnswer(err)
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		case "d":
			if !committing {
				break
			}
			diff, err := plan.Diff(ctx, a.dotfiles)
			if err != nil {
				return false, err
			}
			if err := a.streams.Page(ctx, diff); err != nil {
				return false, err
			}
			continue
		}
		fmt.Fprintln(a.streams.ErrOut, "Please answer with one of the letters in brackets.")
	}
}

// noAnswer turns input ending before an answer into an error that says so.
func (a *app) noAnswer(err error) error {
	if errors.Is(err, iostreams.ErrNoAnswer) {
		return errors.New("nothing committed or pushed: no answer was given")
	}
	return err
}

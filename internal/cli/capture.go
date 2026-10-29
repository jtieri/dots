package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/capture"
	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/iostreams"
)

func newCaptureCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "capture [file...]",
		Short: "Copy files changed on this machine into the repo",
		Long: `Copy files changed on this machine into the repo, so that dot push can
share them with your other machines. Name the files or folders to capture, or
name none to go through each file changed here since chezmoi last wrote it.

dot asks first when a file is new to the repo, when the repo has a version of
it this machine has not applied, when a private file would become readable to
others, or when chezmoi's secret scanner flags it. Files matching
capture.protected are never captured. A file the repo makes from a template
is refused, unless modify_templates lists it, in which case dot rewrites the
template from it. dot never commits; dot push does.

Without a terminal dot cannot ask, so --yes is needed. --yes never makes a
private file readable to others or captures a possible secret: those need a
yes at a terminal.

Exits 0 when every file was captured or already matched the repo, 1 when
something was left as it was, and 2 on errors.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.capture(cmd.Context(), args, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false,
		"capture without asking, except for private files and possible secrets")
	return cmd
}

func (a *app) capture(ctx context.Context, args []string, yes bool) error {
	opts := capture.Options{Protected: a.cfg.Capture.Protected}
	for _, m := range a.cfg.ModifyTemplates {
		opts.ModifyTemplates = append(opts.ModifyTemplates, capture.ModifyTemplate{File: m.File, Keep: m.Keep})
	}
	c, err := capture.New(ctx, a.dotfiles, opts)
	if err != nil {
		return err
	}

	// Files named on the command line are captured without asking unless
	// there is something to ask about; the files changed here are each
	// asked about.
	walk := len(args) == 0
	var targets []string
	var leftAlone bool
	if walk {
		if deleted := c.Deleted(); len(deleted) > 0 {
			writeList(a.streams.ErrOut, tildes(deleted),
				"Deleted on this machine, so not captured; dot apply puts it back, or dot rm deletes it everywhere",
				"Deleted on this machine, so not captured; dot apply puts them back, or dot rm deletes them everywhere")
			fmt.Fprintln(a.streams.ErrOut)
			leftAlone = true
		}
		for _, rel := range c.Changed() {
			targets = append(targets, filepath.Join(a.dotfiles.Home, rel))
		}
		if len(targets) == 0 && !leftAlone {
			fmt.Fprintln(a.streams.Out, "Nothing to capture; no files changed on this machine.")
		} else if len(targets) > 0 && !yes && !a.streams.CanPrompt() {
			return errors.New("nothing captured: there is no terminal to ask at; name the files to capture, or use --yes")
		}
	}
	for _, arg := range args {
		target, err := absHomePath(arg, a.dotfiles.Home)
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}

	var failed, anyCaptured bool
	for i, target := range targets {
		if walk && i > 0 {
			fmt.Fprintln(a.streams.ErrOut)
		}
		outcome, err := a.captureOne(ctx, c, target, walk, yes)
		switch {
		case ctx.Err() != nil:
			a.reportInterrupted(target, err)
			return ctx.Err()
		case errors.Is(err, iostreams.ErrNoAnswer):
			return errors.New("nothing more captured: no answer was given")
		case err != nil:
			a.printError(err)
			failed = true
		case outcome == captured:
			anyCaptured = true
		case outcome == leftAsIs:
			leftAlone = true
		}
	}
	if anyCaptured {
		fmt.Fprintln(a.streams.Out, "\nRun dot push to commit and share the captured changes.")
	}
	switch {
	case failed:
		return errReported
	case leftAlone:
		return errOutOfSync
	}
	return nil
}

// reportInterrupted says what became of the repo when Ctrl-C stopped the
// capture of target with err.
func (a *app) reportInterrupted(target string, err error) {
	switch {
	case errors.Is(err, capture.ErrRestored):
		rel, _ := filepath.Rel(a.dotfiles.Home, target)
		fmt.Fprintf(a.streams.ErrOut, "Stopped capturing %s; the repo was put back as it was.\n",
			dotfiles.Tilde(filepath.ToSlash(rel)))
	case err != nil && !errors.Is(err, context.Canceled):
		// Putting the repo back failed, and the error says how.
		a.printError(err)
	}
}

// A captureOutcome says what became of one path.
type captureOutcome int

const (
	alreadyMatched captureOutcome = iota
	captured
	leftAsIs
)

// captureOne captures target, asking first when walk is set or the plan has
// something to ask about.
func (a *app) captureOne(ctx context.Context, c *capture.Capturer, target string, walk, yes bool) (captureOutcome, error) {
	p, err := c.Preview(ctx, target)
	if err != nil {
		return 0, err
	}
	name := dotfiles.Tilde(p.Path)
	if len(p.Diff) == 0 {
		fmt.Fprintf(a.streams.Out, "%s already matches the repo.\n", name)
		return alreadyMatched, nil
	}
	ok, err := a.confirmCapture(ctx, p, walk, yes)
	if err != nil {
		return 0, err
	}
	if !ok {
		fmt.Fprintf(a.streams.ErrOut, "Left %s as it was.\n", name)
		return leftAsIs, nil
	}
	reset, err := c.Capture(ctx, p)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(a.streams.Out, "Captured %s.\n", name)
	for _, rel := range reset {
		fmt.Fprintf(a.streams.Out, "  The repo cannot keep the permissions of %s, so dot apply will reset them.\n",
			dotfiles.Tilde(rel))
	}
	return captured, nil
}

// confirmCapture decides whether to carry out p: from --yes if it answers,
// otherwise by asking. Making a private file readable to others and
// capturing a possible secret always need a yes at a terminal.
func (a *app) confirmCapture(ctx context.Context, p *capture.Plan, walk, yes bool) (bool, error) {
	name := dotfiles.Tilde(p.Path)
	risky := len(p.Unprivate) > 0 || p.Secret != ""
	ask := walk || risky || len(p.New) > 0 || len(p.RepoNewer) > 0
	if !ask {
		return true, nil
	}
	w := a.streams.ErrOut
	if walk {
		fmt.Fprintf(w, "%s changed on this machine.\n", name)
	}
	writeList(w, tildes(p.New),
		"New to the repo, so every machine will get it",
		"New to the repo, so every machine will get them")
	writeList(w, tildes(p.RepoNewer),
		"Changed in the repo since this machine last applied it; capturing replaces the repo's version",
		"Changed in the repo since this machine last applied them; capturing replaces the repo's versions")
	writeList(w, tildes(p.Unprivate),
		"Private in the repo; capturing makes it readable to others on every machine",
		"Private in the repo; capturing makes them readable to others on every machine")
	if p.Secret != "" {
		writeList(w, []string{p.Secret}, "chezmoi's secret scanner found what may be a secret", "")
	}
	switch {
	case yes && !risky:
		return true, nil
	case !a.streams.CanPrompt() && risky:
		return false, fmt.Errorf("nothing captured from %s: it needs a yes at a terminal, which --yes cannot give", name)
	case !a.streams.CanPrompt():
		return false, fmt.Errorf("nothing captured from %s: there is no terminal to ask at; use --yes", name)
	}
	for {
		answer, err := a.streams.Prompt(ctx, fmt.Sprintf("Capture %s? [y/d/N] ", name))
		if err != nil {
			return false, err
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		case "d":
			if err := a.streams.Page(ctx, p.Diff); err != nil {
				return false, err
			}
		default:
			fmt.Fprintln(w, "Please answer with one of the letters in brackets.")
		}
	}
}

// writeList writes items indented under a heading, one for a single item and
// many for more, or nothing when there are no items.
func writeList(w io.Writer, items []string, one, many string) {
	if len(items) == 0 {
		return
	}
	heading := one
	if len(items) > 1 {
		heading = many
	}
	fmt.Fprintln(w, heading)
	for _, item := range items {
		fmt.Fprintln(w, " ", item)
	}
}

// tildes writes paths relative to the home directory as ~/path.
func tildes(rels []string) []string {
	out := make([]string, len(rels))
	for i, rel := range rels {
		out[i] = dotfiles.Tilde(rel)
	}
	return out
}

// absHomePath returns the absolute path that arg, a path given on the command
// line, names. A leading ~ means the home directory, as in dot's own messages.
func absHomePath(arg, home string) (string, error) {
	if arg == "~" {
		return home, nil
	}
	if rest, ok := strings.CutPrefix(arg, "~/"); ok {
		return filepath.Join(home, rest), nil
	}
	return filepath.Abs(arg)
}

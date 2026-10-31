package cli

import (
	"github.com/spf13/cobra"

	"github.com/jtieri/dots/internal/status"
)

func newStatusCmd(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what is out of sync",
		Long: `Show what is out of sync: changes in the repo not yet committed, commits
not yet pushed or pulled, files in the home directory that differ from the
repo, setup steps waiting to run, and untracked files inside tracked folders.

It fetches from the remote first, giving up after fetch_timeout (10 seconds
unless set). It changes nothing else.

Exits 0 when everything is in sync, 1 when something is not, and 2 when
the check itself failed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := status.Options{
				Branch:       a.cfg.Branch,
				FetchTimeout: a.cfg.FetchTimeout,
				SkipDirs:     a.cfg.Leftovers.Skip,
			}
			for _, m := range a.cfg.ModifyTemplates {
				opts.ModifyTemplates = append(opts.ModifyTemplates, m.File)
			}
			report, err := status.Check(cmd.Context(), a.dotfiles, opts)
			if err != nil {
				return err
			}
			if asJSON {
				err = report.WriteJSON(a.streams.Out)
			} else {
				err = report.WriteText(a.streams.Out, a.streams.OutStyle)
			}
			if err != nil {
				return err
			}
			if !report.InSync {
				return errOutOfSync
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	return cmd
}

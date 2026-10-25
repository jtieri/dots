package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/status"
)

func newStatusCmd(streams *iostreams.Streams) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what is out of sync",
		Long: `Show what is out of sync: changes in the repo not yet committed, commits
not yet pushed or pulled, files in the home directory that differ from the
repo, setup steps waiting to run, and untracked files inside tracked folders.

It fetches from the remote first, giving up after 10 seconds. It changes
nothing else.

Exits 0 when everything is in sync, 1 when something is not, and 2 when
the check itself failed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			report, err := status.Check(cmd.Context(), &chezmoi.Client{}, status.DefaultOptions(home))
			if err != nil {
				return err
			}
			if asJSON {
				err = report.WriteJSON(streams.Out)
			} else {
				err = report.WriteText(streams.Out)
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

// Package cli implements dot's command line.
package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/iostreams"
)

// Exit codes follow grep: 0 for success, 1 when there is something to act
// on, and 2 for errors, including bad usage.
const (
	exitOK    = 0
	exitError = 2
)

// Main runs dot with args and returns the process exit code.
func Main(args []string, streams *iostreams.Streams) int {
	root := newRootCmd(streams)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(streams.ErrOut, "dot:", err)
		return exitError
	}
	return exitOK
}

func newRootCmd(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dot",
		Short: "Keep your dotfiles in sync across machines",
		Long: `dot keeps the dotfiles on each of your machines in sync. It works on top
of chezmoi and git: it reports what differs, and shows you each change before
making it.`,
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// Without these, cobra prints help for an unknown subcommand and
		// exits 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.SetIn(streams.In)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetVersionTemplate("dot {{.Version}}\n")
	return cmd
}

// version reports the version Go recorded in the binary: the module version
// for "go install github.com/jtieri/dot@v1.2.3", or a pseudo-version derived
// from git for a build inside the repository.
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

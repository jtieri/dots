// Package cli implements dot's command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/jtieri/dot/internal/config"
	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/lock"
)

// Exit codes follow grep: 0 for success, 1 when there is something to act
// on, and 2 for errors, including bad usage. An interrupted run exits as a
// shell reports a command killed by Ctrl-C.
const (
	exitOK          = 0
	exitOutOfSync   = 1
	exitError       = 2
	exitInterrupted = 130
)

// errOutOfSync is returned by a command that has reported something to act
// on. Main exits 1 without printing it.
var errOutOfSync = errors.New("out of sync")

// Main runs dot with args and returns the process exit code. Ctrl-C cancels
// the running command, which stops the programs it started.
func Main(args []string, streams *iostreams.Streams) int {
	return run(args, &app{streams: streams, euid: os.Geteuid()})
}

func run(args []string, a *app) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	defer a.close()
	root := newRootCmd(a)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errOutOfSync):
		return exitOutOfSync
	case ctx.Err() != nil:
		return exitInterrupted
	default:
		fmt.Fprintln(a.streams.ErrOut, "dot:", err)
		return exitError
	}
}

// app holds what dot's subcommands share. setup fills in the rest before a
// subcommand runs.
type app struct {
	streams    *iostreams.Streams
	euid       int
	configFile string
	// lockPath is the lock file. If empty, lock.DefaultPath is used.
	lockPath string

	cfg     *config.Config
	release func()
}

// setup runs before every subcommand that works on the dotfiles: it refuses
// to run as root, loads the settings and takes the lock, so only one dot runs
// at a time. Someone at a terminal waits for the lock; anything else, such as
// the background check, gives up at once.
func (a *app) setup(cmd *cobra.Command) error {
	if a.euid == 0 {
		return errors.New("refusing to run as root; run dot as your own user")
	}
	cfg, err := config.Load(a.configFile)
	if err != nil {
		return err
	}
	a.cfg = cfg
	path := a.lockPath
	if path == "" {
		if path, err = lock.DefaultPath(); err != nil {
			return err
		}
	}
	waiting := func() { fmt.Fprintln(a.streams.ErrOut, "Waiting for another dot to finish...") }
	release, err := lock.Acquire(cmd.Context(), path, a.streams.ErrIsTerminal, waiting)
	if errors.Is(err, lock.ErrBusy) {
		return errors.New("another dot is running; try again when it has finished")
	}
	if err != nil {
		return err
	}
	a.release = release
	return nil
}

func (a *app) close() {
	if a.release != nil {
		a.release()
	}
}

// needsSetup reports whether cmd works on the dotfiles. dot itself, help and
// shell completion only print text.
func needsSetup(cmd *cobra.Command) bool {
	if !cmd.HasParent() {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return false
		}
	}
	return true
}

func newRootCmd(a *app) *cobra.Command {
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
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !needsSetup(cmd) {
				return nil
			}
			return a.setup(cmd)
		},
	}
	cmd.SetIn(a.streams.In)
	cmd.SetOut(a.streams.Out)
	cmd.SetErr(a.streams.ErrOut)
	cmd.SetVersionTemplate("dot {{.Version}}\n")
	cmd.PersistentFlags().StringVar(&a.configFile, "config", "",
		"read settings from `file` instead of $DOT_CONFIG or ~/.config/dot/config.toml")
	cmd.AddCommand(newStatusCmd(a))
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

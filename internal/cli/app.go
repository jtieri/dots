package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jtieri/dots/internal/chezmoi"
	"github.com/jtieri/dots/internal/config"
	"github.com/jtieri/dots/internal/dotfiles"
	"github.com/jtieri/dots/internal/iostreams"
	"github.com/jtieri/dots/internal/lock"
)

// app holds what the subcommands share. setup fills in the rest before a
// subcommand runs.
type app struct {
	streams    *iostreams.Streams
	euid       int
	configFile string
	// lockPath is the lock file. If empty, lock.DefaultPath is used.
	lockPath string

	cfg      *config.Config
	dotfiles *dotfiles.Dotfiles
	release  func()
}

// setup runs before every subcommand that works on the dotfiles: it refuses
// to run as root, loads the settings, takes the lock so only one dots command
// runs at a time, and finds the dotfiles. Someone at a terminal waits for the
// lock; anything else, such as the background check, gives up at once.
func (a *app) setup(cmd *cobra.Command) error {
	if a.euid == 0 {
		return errors.New("refusing to run as root; run dots as your own user")
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
	waiting := func() { fmt.Fprintln(a.streams.ErrOut, "Waiting for another dots command to finish...") }
	release, err := lock.Acquire(cmd.Context(), path, a.streams.ErrIsTerminal, waiting)
	if errors.Is(err, lock.ErrBusy) {
		return errors.New("another dots command is running; try again when it has finished")
	}
	if err != nil {
		return err
	}
	a.release = release

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	a.dotfiles, err = dotfiles.Open(cmd.Context(), home, &chezmoi.Client{})
	return err
}

// printError reports err on standard error, as dots reports every error.
func (a *app) printError(err error) {
	fmt.Fprintln(a.streams.ErrOut, a.streams.ErrStyle.Bad("dots:"), err)
}

func (a *app) close() {
	if a.release != nil {
		a.release()
	}
}

// needsSetup reports whether cmd works on the dotfiles. dots itself, help and
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

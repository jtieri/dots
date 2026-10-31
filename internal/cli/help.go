package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jtieri/dots/internal/config"
	"github.com/jtieri/dots/internal/text"
)

// everyday lists the commands the cheat sheet shows, in its order, with what
// each runs underneath.
var everyday = []struct{ name, underneath string }{
	{"status", "git fetch, git status, chezmoi status, chezmoi unmanaged"},
	{"sync", "git fetch, git merge --ff-only, then as dots apply"},
	{"capture", "chezmoi add, after chezmoi add --dry-run to check first"},
	{"push", "git fetch, pacman -Qqen and -Qqem, git add, git commit, git push"},
	{"apply", "chezmoi init, chezmoi status, chezmoi apply"},
	{"rm", "chezmoi forget, a line in .chezmoiremove, chezmoi apply"},
	{"forget", "chezmoi forget, a line in .chezmoiignore"},
}

func newHelpCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Show the cheat sheet, or help for a command",
		Long: `With no command, show the cheat sheet: the everyday commands, what each
runs underneath, and every setting. With a command, show its help, as
dots command --help does.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			if len(args) == 0 {
				return a.streams.Page(cmd.Context(), cheatSheet(root))
			}
			c, _, err := root.Find(args)
			if err != nil || c == root {
				return fmt.Errorf("unknown command %q; dots help lists them", strings.Join(args, " "))
			}
			return c.Help()
		},
	}
}

// cheatSheet writes the cheat sheet for root's commands.
func cheatSheet(root *cobra.Command) []byte {
	var b bytes.Buffer
	b.WriteString("dots keeps the dotfiles on each of your machines in sync, on top of chezmoi\nand git.\n\n")

	b.WriteString("Everyday commands\n")
	width := 0
	for _, e := range everyday {
		width = max(width, len(commandNamed(root, e.name).Use))
	}
	for _, e := range everyday {
		c := commandNamed(root, e.name)
		fmt.Fprintf(&b, "  dots %-*s  %s\n", width, c.Use, c.Short)
	}

	b.WriteString("\nWhat each runs underneath\n")
	for _, e := range everyday {
		fmt.Fprintf(&b, "  %-8s %s\n", e.name, e.underneath)
	}

	b.WriteString("\nSettings\n")
	for _, line := range text.Wrap("In $XDG_CONFIG_HOME/dots/config.toml (~/.config/dots/config.toml), or the file "+
		"$DOTS_CONFIG or --config names. A setting's environment variable overrides the file, and an empty "+
		"one counts as unset. Paths in the home directory are written ~/path, and a list replaces the "+
		"default rather than adding to it.", 76) {
		b.WriteString("  " + line + "\n")
	}
	for _, s := range config.Settings {
		b.WriteString("\n  " + s.Key + " = " + s.DefaultText())
		if s.Env != "" {
			b.WriteString("  ($" + s.Env + ")")
		}
		b.WriteString("\n")
		for _, line := range text.Wrap(s.Help, 72) {
			b.WriteString("      " + line + "\n")
		}
	}

	b.WriteString("\nRun dots <command> --help for more about a command.\n")
	return b.Bytes()
}

// commandNamed returns root's subcommand called name.
func commandNamed(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	panic("cli: no command " + name)
}

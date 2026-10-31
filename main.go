// Command dots keeps the dotfiles on each of your machines in sync, using
// chezmoi and git. Run "dots help" for the list of subcommands.
package main

import (
	"os"

	"github.com/jtieri/dots/internal/cli"
	"github.com/jtieri/dots/internal/iostreams"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], iostreams.System()))
}

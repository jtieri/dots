// Command dot keeps the dotfiles on each of your machines in sync, using
// chezmoi and git. Run "dot help" for the list of subcommands.
package main

import (
	"os"

	"github.com/jtieri/dot/internal/cli"
	"github.com/jtieri/dot/internal/iostreams"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], iostreams.System()))
}

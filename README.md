# dots

`dots` keeps the dotfiles on each of my machines in sync. It sits on top of
[chezmoi](https://www.chezmoi.io/) and git, and turns the everyday jobs into
one command each: checking whether a machine is in sync, pulling and applying
changes from other machines, capturing a change made on this one, and pushing
it.

It never changes anything without showing you first.

## Status

Every command below works and is tested, but `dots` is built around my own
setup, so expect changes before 1.0.

## Commands

| Command | Does |
|---|---|
| `dots status` | Show what is out of sync: uncommitted repo changes, commits not yet pushed or pulled, files that differ from the repo, and untracked files in tracked folders |
| `dots sync` | Pull changes from the other machines and apply them |
| `dots apply` | Make the home directory match the repo |
| `dots capture [file...]` | Copy files changed on this machine into the repo |
| `dots push [message]` | Commit the repo's changes and push them |
| `dots rm file...` | Stop tracking files and delete them on every machine |
| `dots forget file...` | Stop tracking files but keep every machine's copy |
| `dots help` | Show the cheat sheet: the commands, what each runs underneath, and every setting |

The commands that change something preview what they will do and ask first.
Files changed on this machine are copied to `~/.local/state/dots/overwritten/`
before anything overwrites them.
Exit codes are 0 for success or nothing to do, 1 for out of sync or
canceled, and 2 for errors.

`dots` finds the repo through chezmoi's source directory, so it works with
any chezmoi setup whose source directory is in a git repo.

## Requirements

- Go 1.27 or newer to build it
- chezmoi (tested with 2.72.0) and git at run time

## Install

```sh
go install github.com/jtieri/dots@latest
```

This puts `dots` in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when that is
unset.

## Configuration

Settings are optional and live in `~/.config/dots/config.toml`. `dots help`
lists each one with its default and its environment variable.

## Development

Run `make` to list the targets. `make check` runs the full set of checks, so
run it before committing.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).

## License

MIT, see [LICENSE](LICENSE).

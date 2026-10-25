# dot

`dot` keeps the dotfiles on each of my machines in sync. It sits on top of
[chezmoi](https://www.chezmoi.io/) and git, and turns the everyday jobs into
one command each: checking whether a machine is in sync, pulling and applying
changes from other machines, capturing a change made on this one, and pushing
it.

It never changes anything without showing you first.

## Status

Early development. Nothing useful works yet.

## Requirements

- Go 1.27 or newer to build it
- chezmoi and git at run time

## Install

```sh
go install github.com/jtieri/dot@latest
```

This puts `dot` in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when that is
unset.

## Development

Run `make` to list the targets. `make check` runs the same checks as CI, so
run it before committing.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).

## License

MIT, see [LICENSE](LICENSE).

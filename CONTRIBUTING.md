# Contributing

## Building and testing

You need Go 1.27 or newer, plus chezmoi and git: the tests run the real
programs in throwaway home directories. The checks also need golangci-lint and
govulncheck.

Run `make` to list the targets. Before sending a change:

```sh
make check   # tidy, vet, lint and the tests with the race detector
make vuln    # known vulnerabilities in the code dots calls; needs the network
```

CI runs both on Arch Linux for every push and pull request, and weekly.

`make hooks` makes git run `make check` before each push from your clone.
`git push --no-verify` skips it.

## Commits

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).
Keep each commit to one change, and make sure it builds and passes on its own.

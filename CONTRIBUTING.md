# Contributing to skills-registry

Bug fixes, documentation, and focused features are welcome. Open an issue before large changes or additions to the public surface.

## Development

The user-facing repository contains the installers/npm launcher, Go CLI, native macOS app, and static website. Follow the nearest existing patterns and the applicable `AGENTS.md` guidance.

For the CLI, install Go 1.24 or newer and run:

```bash
cd cli
go test ./...
go vet ./...
gofmt -l .
```

CI additionally runs `staticcheck`, `deadcode`, `gocyclo`, and `go build`. Formatting output must be empty. Add a regression test for bug fixes and update `README.md` when behavior changes.

Go code follows standard naming: short lowercase packages, `PascalCase` exported identifiers, `camelCase` unexported identifiers, `Err`-prefixed error variables, short receivers, and preserved initialisms such as `URL`, `SHA`, and `ID`.

For macOS app work, use the Swift package build and tests documented under `mac-app/`. Keep shared CLI/app registry contracts aligned. Website and npm changes should use the commands documented in their own directories.

## Pre-commit

Install [pre-commit](https://pre-commit.com/) and run:

```bash
pre-commit install
pre-commit run --all-files
```

## Commits and pull requests

Use prefixes such as `fix:`, `feat:`, `docs:`, `refactor:`, `test:`, or `chore:`. Keep each pull request focused, explain why the change is needed, and avoid new mandatory runtime dependencies without justification.

Bug reports should include `skills-registry --version`, operating system, the relevant registry `owner/repo`, and a minimal reproduction.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

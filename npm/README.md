# skills-registry

The [Skills Registry](https://skills-registry.dev) CLI — a Charmbracelet TUI plus
headless commands for bootstrapping, browsing, publishing, and syncing an
agent-skills registry backed by a GitHub repo.

This npm package is a **thin launcher**. On install (or first run) it downloads
the matching prebuilt Go binary from the project's
[GitHub Releases](https://github.com/nikships/skills-registry/releases) and execs
it. The binary is the exact same one shipped by `install.sh` / `install.ps1`
(macOS builds are codesigned + notarized by Apple).

## Usage

```sh
# one-off, no install
npx skills-registry

# or install globally
npm install -g skills-registry
skills-registry
```

Supported platforms: macOS (x64/arm64), Linux (x64/arm64), Windows (x64/arm64).

## Notes

- The package version maps directly to a release tag — `skills-registry@X.Y.Z`
  downloads the `vX.Y.Z` release asset.
- If you install with `--ignore-scripts`, the binary is fetched lazily on the
  first invocation instead of during `postinstall`.
- Prefer no network at install time? Use the shell installer instead:
  `curl -fsSL https://raw.githubusercontent.com/nikships/skills-registry/main/install.sh | sh`

### Environment overrides

| Variable | Purpose |
|---|---|
| `SKILLS_REGISTRY_REPO` | Override `owner/repo` (default `nikships/skills-registry`) |
| `SKILLS_REGISTRY_VERSION` | Pin a release tag, e.g. `v0.5.37`, or `latest` |
| `SKILLS_REGISTRY_URL` | Override the full asset download URL |

## License

Apache-2.0

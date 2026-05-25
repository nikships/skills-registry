# Installer

Active contributors: Nik Anand

## What it does

`install.sh` is a 142-line POSIX shell script. It detects the host OS and architecture, downloads the matching `skills-registry` tarball from the latest GitHub Release, extracts the binary, and drops it into `~/.local/bin/skills-registry`. The user invokes it through `curl … | sh` and never sees Python during onboarding — the Python MCP server is installed later by the Go wizard.

```bash
curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh
```

## Why POSIX `sh` and not Bash

The script must run unmodified on macOS (where `/bin/sh` is still BSD-flavored), Debian/Ubuntu, Alpine (`ash`), and inside CI containers that may lack `bash`. Every construct is plain `sh`: `case` for OS/arch dispatch, no arrays, no `[[ … ]]`. `set -eu` is enabled so a missing variable or any failed command aborts immediately.

## OS and arch detection

`detect_os()` and `detect_arch()` shell out to `uname -s` and `uname -m`, normalize the result, and exit `2` for anything they can't match. The supported matrix:

| `uname -s` | Normalized OS |
| --- | --- |
| `Linux`, `linux` | `linux` |
| `Darwin`, `darwin` | `darwin` |

| `uname -m` | Normalized arch |
| --- | --- |
| `x86_64`, `amd64` | `amd64` |
| `arm64`, `aarch64` | `arm64` |

Anything else prints a clear "unsupported" message to stderr and exits `2`. The script never silently downloads the wrong asset.

## Download path

`build_url()` produces the canonical Release asset URL:

- `latest` → `https://github.com/<repo>/releases/latest/download/skills-registry_<os>_<arch>.tar.gz`
- pinned `vX.Y.Z` → `https://github.com/<repo>/releases/download/<version>/skills-registry_<os>_<arch>.tar.gz`

`download_to()` tries `curl -fsSL --retry 3` first, then `wget -q -O` if `curl` isn't on `PATH`. If neither is available, the script exits `1` with a clear "need curl or wget" message.

## Extraction and placement

The script creates a tempdir with `mktemp -d` (fallback to `mktemp -d -t skills-registry-install` on older BSDs), traps `EXIT INT TERM` so the dir is removed even on Ctrl-C, then extracts only the `skills-registry` entry from the tarball:

```sh
tar -xzf "$tarball" -C "$tmpdir" "$BINARY"
```

The binary is moved into `$BIN_DIR` (default `~/.local/bin`), then `chmod +x` is applied. If `$BIN_DIR` is not on `$PATH` the script prints a warning suggesting the user add it to their shell rc — it does not silently mutate the user's profile.

## Environment variables

Every behavior is overridable so the script can be exercised end-to-end without hitting GitHub.

| Variable | Default | Effect |
| --- | --- | --- |
| `SKILLS_REGISTRY_REPO` | `anand-92/skills-registry` | Owner/repo to pull releases from. |
| `SKILLS_REGISTRY_VERSION` | `latest` | Pin to a specific release tag (e.g. `v0.5.1`). |
| `SKILLS_BIN_DIR` | `$HOME/.local/bin` | Install directory. |
| `SKILLS_REGISTRY_OS` | `$(uname -s)` | Override OS detection (useful for testing). |
| `SKILLS_REGISTRY_ARCH` | `$(uname -m)` | Override arch detection. |
| `SKILLS_REGISTRY_URL` | derived | Override the full tarball URL. |
| `SKILLS_REGISTRY_TARBALL` | unset | Use a local tarball path instead of downloading. |
| `SKILLS_REGISTRY_DRY_RUN` | unset | Print the resolved URL/dest and exit `0` without downloading. |

The dry-run knob is used in the release smoke tests: piping through `SKILLS_REGISTRY_DRY_RUN=1` lets CI verify the URL the script would hit without actually pulling bytes.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | Generic / IO failure (download failed, binary not in archive, no `curl` or `wget`). |
| `2` | Unsupported OS or architecture. |

## Key source files

| File | Role |
| --- | --- |
| `install.sh` | The whole installer. POSIX `sh`, 142 LOC. |

## Cross-references

- [overview/architecture](../overview/architecture.md) — how the installer fits into the three-deliverable picture.
- [overview/getting-started](../overview/getting-started.md) — what to do after `install.sh` finishes.
- [apps/cli/wizard-and-hub](cli/wizard-and-hub.md) — the wizard that runs on first invocation of `skills-registry`.

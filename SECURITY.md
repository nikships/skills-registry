# Security policy

## Reporting a vulnerability

If you believe you have found a security issue in `skills-registry`, please report it **privately** via [GitHub Security Advisories](https://github.com/anand-92/skills-registry/security/advisories/new).

Do **not** open a public GitHub issue, discussion, or pull request for security problems. Public reports give attackers a head start before a fix lands.

When you report, please include:

- A description of the issue and its impact.
- Steps to reproduce.
- The version (`skills-registry --version`), your Python (and Go, if relevant) version, and OS.
- Any logs or stack traces you can share.

We will acknowledge your report, investigate, and coordinate a fix and disclosure timeline with you. Credit in the release notes is offered by default; let us know if you'd prefer to stay anonymous.

## Scope and threat model

`skills-registry` ships two things: the **hosted MCP server** at `https://mcp.skills-registry.dev/mcp` (Python, FastMCP, deployed from `infa-not-for-users/` to Railway) and the `skills-registry` Go CLI users install via `install.sh`.

- **Hosted MCP server.** Two read-only tools (`list_skills`, `get_skill`). Auth is OAuth + a GitHub App installation on the user's registry repo. The server runs in a Docker container with no shell, no `gh`, no `git`; every GitHub call uses an installation-scoped GitHub App token fetched per request. The server never writes to user repos.
- **Go CLI.** All writes (`publish` / `sync` / `add` / `remove`) and the wizard's bulk bootstrap path live here. The CLI shells out to the user's authenticated `gh` CLI for single-skill operations and to `git` (with `gh` as the credential helper) for the bulk `git push` in the wizard.

Vulnerabilities we care about (non-exhaustive):

- Path traversal in the CLI's `publish` / `sync` / bootstrap paths that escapes the intended skill folder. We reject paths containing `..` segments via `validateRelPath` in `cli/internal/registry/registry.go` and skip dotfiles + `__pycache__`.
- Resource URIs from the hosted server that disclose unintended paths.
- Crashes or hangs triggered by maliciously crafted `SKILL.md` files (frontmatter parser is intentionally minimal; it never invokes a real YAML deserializer).
- Bypasses of the per-file size cap (`SKILLS_MAX_FILE_BYTES`, default 2 MiB) enforced by the CLI's publish path.
- Supply-chain issues in our Go release tarballs or the hosted server's Docker image.
- OAuth / GitHub App handling issues in the hosted server (token leakage, scope confusion, webhook spoofing).

Out of scope:

- Risks inherent to the model or MCP client consuming a skill (jailbreaks, prompt injection in skill content, etc.). Skills are user-controlled prompts; treat them with the same care as any other prompt.
- Issues that require the attacker to already have write access to your skill registry repo or local cache.

## Supported versions

We support the **latest minor release on `main`**. Older versions do not receive security backports — please upgrade if you can.

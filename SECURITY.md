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

`skills-registry` ships three things: the `skills-registry init` bootstrap, the `skill-registry-mcp` MCP server, and the `skill-registry` Go CLI. All three talk to GitHub exclusively through the user's authenticated `gh` CLI — there is no embedded HTTP client, no direct `git` shell-out, and no SSH agent dependency. `gh auth status` is the only trust anchor; if it fails, every command exits before touching the network.

The MCP server's surface is three tools (`list_skills`, `get_skill`, `publish_skill`) plus a local on-disk cache at `~/.cache/skills-mcp/skills/<slug>/`. The CLI exposes the same operations interactively.

Vulnerabilities we care about (non-exhaustive):

- Path traversal in `publish_skill` or `get_skill` that escapes the intended skill folder. We reject paths containing `..` segments and skip dotfiles + `__pycache__`.
- Resource URIs that disclose unintended paths.
- Crashes or hangs triggered by maliciously crafted `SKILL.md` files (frontmatter parser is intentionally minimal; it never invokes a real YAML deserializer).
- Bypasses of the per-file size cap (`SKILLS_MAX_FILE_BYTES`, default 2 MiB) used by `publish_skill`.
- Supply-chain issues in our published Python wheel or Go release tarballs.

Out of scope:

- Risks inherent to the model or MCP client consuming a skill (jailbreaks, prompt injection in skill content, etc.). Skills are user-controlled prompts; treat them with the same care as any other prompt.
- Issues that require the attacker to already have write access to your skill registry repo or local cache.

## Supported versions

We support the **latest minor release on `main`**. Older versions do not receive security backports — please upgrade if you can.

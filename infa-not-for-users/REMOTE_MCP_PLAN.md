# Remote FastMCP Streamable HTTP MCP Migration

## Summary

Replace the local stdio MCP with a hosted Python/FastMCP Streamable HTTP MCP server. The Go CLI remains the onboarding path for creating and managing the skills repo, but MCP usage moves fully remote.

Users are assumed to already have a CLI-created skills repo. The hosted MCP links to that repo using GitHub OAuth for user login and a shared GitHub App for repo access through installation tokens.

## Deployer Prerequisites

- Create a public HTTPS base URL for the service, e.g. `https://mcp.skills-registry.dev`.
- Reserve these service routes:
  - `/mcp` for Streamable HTTP MCP.
  - `/healthz` for deployment health checks.
  - `/auth/callback` for FastMCP GitHub OAuth.
  - `/github/app/callback` for GitHub App install/setup return.
  - `/github/webhook` for GitHub App webhooks.
- Create a GitHub OAuth App for MCP user/client authentication:
  - Callback URL: `https://mcp.skills-registry.dev/auth/callback`.
  - Store OAuth client ID and client secret as deployment secrets.
- Create a public GitHub App for repository access:
  - Repository permission: `Contents: Read-only`.
  - Metadata read access is implicit.
  - Subscribe to `installation` and `installation_repositories` webhook events.
  - Configure setup URL: `https://mcp.skills-registry.dev/github/app/callback`.
  - Configure webhook URL: `https://mcp.skills-registry.dev/github/webhook`.
  - Generate and securely store App ID, private key PEM, and webhook secret.
- Provision persistent storage:
  - FastMCP OAuth client/token state.
  - Linked repo state: `github_user_id`, `installation_id`, `repo`, `default_branch`.
  - Use encrypted persistent storage for OAuth/token data in production.

## Key Changes

- Replace `skill-registry-mcp` local stdio behavior with a remote HTTP server entrypoint, e.g. `skills_mcp.remote_server:app`.
- Use `FastMCP("skill-registry", auth=auth_provider, stateless_http=True)`.
- Serve MCP via `mcp.http_app(path="/mcp")`.
- Add root-level FastMCP OAuth discovery routes.
- Remove MCP dependencies on local:
  - Python tool install for end users.
  - `gh` CLI.
  - `git`.
  - `~/.config/skills-mcp/registry.toml`.
  - `~/.cache/skills-mcp/skills`.
  - Local filesystem paths from tool responses.
- Update README/setup docs so MCP config points to the hosted `/mcp` URL, not a local command.
- Keep the Go CLI registry workflow functionally unchanged.

## Auth And Repo Linking

- Use FastMCP GitHub OAuth/OAuth Proxy for MCP authentication.
- Configure auth via environment variables:
  - `FASTMCP_SERVER_AUTH`
  - `FASTMCP_SERVER_AUTH_GITHUB_CLIENT_ID`
  - `FASTMCP_SERVER_AUTH_GITHUB_CLIENT_SECRET`
  - `FASTMCP_SERVER_AUTH_GITHUB_BASE_URL`
  - JWT signing key and encrypted persistent storage settings.
- Use the shared GitHub App only for repo access:
  - Server signs a GitHub App JWT.
  - Server mints short-lived installation access tokens.
  - Server calls GitHub REST API directly.
  - GitHub tokens are never exposed to MCP clients.
- Linking flow:
  - User authenticates with GitHub OAuth.
  - User installs the GitHub App on their existing skills repo.
  - Service lists repos available to that installation.
  - User selects the existing skills repo.
  - Service validates that repo contains at least one top-level skill folder with `SKILL.md`.
  - If no repo is linked, MCP tools return setup guidance with the hosted linking URL.

## MCP Tool Behavior

- Remote v1 exposes only:
  - `list_skills`
  - `get_skill`
- `list_skills() -> str`:
  - Reads selected repo with GitHub App installation token.
  - Returns a markdown table with slug, name, and description.
- `get_skill(slug: str) -> str`:
  - Returns only the top-level `SKILL.md` content for v1.
  - Does not return local paths.
  - Does not download supporting files yet.
- Do not include `publish_skill` in remote v1.

## Test Plan

- GitHub App client tests:
  - Mint installation token.
  - List installation repos.
  - List skill folders.
  - Fetch `SKILL.md`.
  - Handle 401, 403, 404, rate limits, missing installation, and missing repo.
- Linking tests:
  - Select valid existing repo.
  - Reject repo without skills.
  - Handle multiple installed repos.
  - Handle App uninstall and repo access removal webhooks.
- MCP tests:
  - Tool list contains only `list_skills` and `get_skill`.
  - `get_skill` returns markdown content, not a path.
  - Unlinked user gets setup guidance.
- HTTP smoke test:
  - Start ASGI app locally.
  - Connect with a FastMCP Streamable HTTP client.
  - Authenticate with mocked OAuth/token state.
  - Call `list_skills` and `get_skill` against mocked GitHub API.

## Assumptions

- No backward compatibility for old local MCP is required.
- Existing CLI-created skills repos remain the source of truth.
- The Go CLI stays functionally unchanged.
- Hosted Python is acceptable because end users do not install Python locally.
- Remote v1 is read-only.
- Supporting files and remote publishing are future features.

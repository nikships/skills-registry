# Skills Registry — Remote MCP Production Handoff

> **Audience:** the next engineer picking up `skills-registry-mcp` after the
> initial production launch.
>
> **You inherit:** a live remote MCP server at
> `https://mcp.skills-registry.dev`, all GitHub apps wired to the production
> host, and a green CI pipeline on `main`. The branch
> `feat/remote-mcp-server` has been merged. The Go CLI still installs the
> *local* PyPI MCP — closing that loop is the next big rock.

---

## 1. What's live right now

| Surface | URL / value | Status |
|---|---|---|
| Public host | `https://mcp.skills-registry.dev` | ✅ Online |
| Healthcheck | `GET /healthz` → `{"status":"ok"}` | ✅ 200 |
| OAuth metadata | `GET /.well-known/oauth-authorization-server` | ✅ 200, FastMCP discovery JSON |
| TLS | Let's Encrypt (issuer `E8`) | ✅ valid through 2026-08-23 |
| MCP transport | Streamable HTTP at `POST /mcp` (FastMCP 3.x) | ✅ Bound |
| Tools | `list_skills`, `get_skill` (read-only v1) | ✅ Registered |
| GitHub OAuth App | "Skills Registry" (`Ov23liS1hkOMtFsSHFq8`) → `/auth/callback` | ✅ Production URL |
| GitHub App | "Skills Registry MCP" (`3846201`, slug `skills-registry-mcp`) | ✅ Setup + webhook URLs production |
| DNS | Porkbun: `mcp` CNAME → `yd95uzxw.up.railway.app`, `_railway-verify.mcp` TXT | ✅ Resolving |

---

## 2. Production infrastructure

### Railway

- **Project:** `skills-registry-mcp` (ID `fff5f331-b635-4a25-b42e-267eab4f5a3f`)
- **Region:** US East (Virginia)
- **Service:** `skills-registry` (ID `f3b28f8a-f622-4011-b420-3b34c8a863f8`)
- **Environment:** `production` (ID `686735cd-47f7-49c0-9f7e-6320bbe9e09d`)
- **Source:** GitHub repo `anand-92/skills-registry`, branch `main`, auto-deploys on push.
- **Builder:** Dockerfile (configured via `railway.json`)
- **Volume:** `skills-registry-volume`, 5 GB at `/data` (state `READY`)
- **Healthcheck:** `/healthz`, timeout 30 s
- **Replicas:** 1 (volume attachment prevents horizontal scaling — by design)
- **Restart policy:** `ON_FAILURE`, max 5 retries
- **Postgres pod:** still present from the original provisioning, **idle** — no consumer. See §6.

### Domain

- **Registrar:** Porkbun (`skills-registry.dev`)
- **Nameservers:** Porkbun
- **Records added for prod:**
  - `mcp` CNAME → `yd95uzxw.up.railway.app` (TTL 600)
  - `_railway-verify.mcp` TXT → `railway-verify=…` (Railway domain verification, can stay)

### GitHub apps

- **OAuth App** (`Skills Registry`, ID `3621956`)
  - Client ID: `Ov23liS1hkOMtFsSHFq8`
  - Authorization callback: `https://mcp.skills-registry.dev/auth/callback`
  - Homepage: `https://skills-registry.dev`
- **GitHub App** (`Skills Registry MCP`, ID `3846201`)
  - Slug: `skills-registry-mcp`
  - Permissions: `Contents: Read-only`, `Metadata: Read-only`
  - Events: `installation`, `installation_repositories`
  - Setup URL: `https://mcp.skills-registry.dev/github/app/callback`
  - Webhook URL: `https://mcp.skills-registry.dev/github/webhook`

### Environment variables (service-scoped)

These live on the `skills-registry` Railway service, set via the Variables tab:

```
FASTMCP_SERVER_AUTH_GITHUB_BASE_URL=https://mcp.skills-registry.dev
FASTMCP_SERVER_AUTH_GITHUB_CLIENT_ID=Ov23liS1hkOMtFsSHFq8
FASTMCP_SERVER_AUTH_GITHUB_CLIENT_SECRET=<oauth app secret>
GITHUB_APP_ID=3846201
GITHUB_APP_SLUG=skills-registry-mcp
GITHUB_APP_PRIVATE_KEY=<RSA PEM, multiline>
GITHUB_APP_WEBHOOK_SECRET=<random>
JWT_SIGNING_KEY=<urlsafe random>
STORAGE_ENCRYPTION_KEY=<Fernet key>
FASTMCP_STORAGE_DIR=/data/oauth
```

`HOST=0.0.0.0` and `PORT` come from the image / Railway respectively.

---

## 3. What's left to do (in priority order)

### A. End-to-end smoke test with a real MCP client *(blocker)*

The server is up but no one has actually connected to it from a real client.
Run through this sequence yourself before announcing the launch:

1. Add `https://mcp.skills-registry.dev/mcp` as a remote MCP in Claude
   Desktop (or Cursor / VS Code Copilot) with `auth: oauth`.
2. Trigger a tool call → expect an OAuth browser pop-up to GitHub.
3. Authorize. The client should auto-reconnect and list `list_skills` /
   `get_skill` in its tool catalogue.
4. Call `list_skills` → expect the "no repo linked yet, install the App"
   markdown.
5. Click the install URL, install the App on your registry repo, wait
   ~3 s for the webhook.
6. Call `list_skills` again → expect a markdown table of skill slugs.
7. Call `get_skill(slug=...)` → expect raw `SKILL.md`.

If anything 401s or hangs, check Railway logs first (the FastMCP JWT and the
encryption-wrapper failures are the two most likely culprits).

### B. Point the Go CLI at the hosted MCP

`cli/internal/bootstrap/mcp_install.go` currently runs
`uv tool install --force skills-registry` and writes an MCP config that
spawns the local `skill-registry-mcp` console script. With the hosted server
live, the wizard should *instead* (or *additionally*) write a remote-MCP
snippet pointing at `https://mcp.skills-registry.dev/mcp`.

Concretely:
- Update `cli/internal/bootstrap/skillmd.go` (the `SkillMd` template) and
  wherever the wizard generates the MCP config blob (look for "claude
  desktop" / "mcpServers" strings).
- Decide whether to drop the local install path entirely or keep both
  (recommendation: keep both, default to hosted, but offer `--local` for
  air-gapped / privacy-conscious users).
- Update `README.md` and `docs/registry.md` accordingly.

This is the biggest remaining piece of work and the reason the launch is
not "done" yet.

### C. Documentation refresh

- `README.md` still describes the local-stdio MCP install. Add a "Hosted
  MCP" section at the top with the one-liner JSON snippet:
  ```json
  {
    "mcpServers": {
      "skills-registry": {
        "url": "https://mcp.skills-registry.dev/mcp"
      }
    }
  }
  ```
- `docs/registry.md` describes the on-machine `gh api`-based architecture.
  Add a "Remote MCP (hosted)" section that explains the GitHub App /
  installation-token flow and links to `src/skills_mcp/remote_server.py`.
- The `feat/remote-mcp-server` branch already deleted the old
  `registry_server.py` etc.; double-check `CLAUDE.md` / `AGENTS.md`
  references aren't stale (some still reference `list_skills`/`get_skill`
  as gh-api-backed when they're now App-token-backed on the server side).

### D. Delete or repurpose the idle Postgres pod

The Postgres service in the Railway project has no consumer and costs ~$5/mo.
Either delete it or wire it up to something (audit log? per-user analytics?).
Keeping it "just in case" is not a decision; pick one.

### E. CI: lock the version in CI / release workflow

`release.yml` builds the Python wheel via `hatch-vcs` from a tag. The
Dockerfile now hard-codes `SETUPTOOLS_SCM_PRETEND_VERSION=0.6.0` so Railway
builds work without the `.git` directory. When you cut the next release
(`v0.7.x`), bump that pinned version in `Dockerfile` in the same PR so the
container image and the PyPI wheel agree.

A cleaner long-term fix is to either (a) include `.git` in the Docker build
context (adds ~5 MB), or (b) wire the release workflow to write
`src/skills_mcp/_version.py` before `docker build`, so the Dockerfile can
read it without setuptools-scm at all.

---

## 4. Potential enhancements / improvements

Ordered by ROI, roughly. None are blockers; pick based on which user pain
shows up first.

### Server-side

- **`select_repo` tool.** Today the server auto-picks the first installed
  repo matching `*skills*`, falling back to the first repo containing any
  `SKILL.md`. If a user installs the App on three repos, they can't override
  the choice. Add a tool that lists installed repos and lets the client pick.
- **`publish_skill` / `update_skill` tools.** v1 is read-only. Adding writes
  needs (a) bumping the GitHub App's `Contents` permission to `Read & write`
  (forces every user to reinstall — friction), and (b) a token-scoping
  story (today the installation token is App-scoped, not user-scoped, so a
  publish call on behalf of user X could write to a repo X doesn't own if
  the App is installed there). Both solvable but need thought.
- **Structured logging + tracing.** Currently the server logs to stderr with
  default uvicorn formatting. Railway has decent log search but no metrics,
  no distributed tracing, no request IDs surfaced to clients. Pick one of:
  - JSON-structured stderr (zero deps, Railway already aggregates)
  - OpenTelemetry → Honeycomb / Grafana Cloud (~$0 at this volume)
- **Rate limiting.** A misbehaving client can burn the App installation
  token's GitHub API budget (5000 req/hr/installation). Simple in-memory
  rate limiter keyed on `(user_id, tool_name)` would block this without
  adding deps.
- **Webhook delivery resilience.** Right now if `/github/webhook` 500s,
  GitHub retries with exponential backoff and the user sees "no repo
  linked" until the retry lands. Add a `/setup/relink` admin tool (or a
  background task that polls `/installations` for the OAuth user) as a
  belt-and-suspenders fallback.
- **Multi-skill discovery.** `list_skills` returns a flat table. For users
  with 50+ skills, paginated / category-filtered output would be more
  usable. Frontmatter already has `category` and `tags` fields — surface
  them.
- **Cache `list_skills` per installation.** Each call hits the GitHub
  Contents API + parses every `SKILL.md`. Cache the result for 60 s keyed
  on `(installation_id, tree_sha)` — invalidate on webhook push events
  (would need to add `push` to the App's event list).
- **Health endpoint deeper than `200`.** `/healthz` returns ok unconditionally.
  Make it also verify (a) GitHub App JWT minting works, (b) the volume is
  writable, (c) the storage encryption key decrypts a canary value. Catches
  silent misconfig at deploy time.

### Client / DX

- **`browse_registry(owner, repo)` tool.** Read-only access to *any* public
  registry, not just the user's own. Two-line change in `github_api.py` to
  drop the installation-token dep for public reads.
- **Multi-registry per user.** Config is one-repo today. A `[registries]`
  array + a `connect <owner/repo>` tool would let a power user pull from
  several registries simultaneously.
- **Web onboarding page.** Today the landing page at `/` is the FastMCP
  default. A custom `/` with the install-the-App button, a screenshot of
  Claude Desktop wired up, and a copy-pasteable JSON snippet would shave a
  lot off first-touch friction.
- **Stripe / subscription gating.** If usage gets serious, you'll want a
  paid tier. FastMCP's middleware story makes adding a per-user quota
  straightforward; the question is billing infra, not code.

### Operational

- **Pin `uv` version in `Dockerfile`.** Today it's `pip install --no-cache-dir uv`
  (unpinned). A breaking uv release could break the image build silently.
- **Add `Dependabot`** for `pyproject.toml` and the GitHub Actions workflows.
- **Coverage upload.** CI generates `coverage.xml` but doesn't upload it
  anywhere. Wire to Codecov (free for OSS).
- **Backups for the volume.** Railway doesn't snapshot volumes automatically
  on the Hobby plan. The volume holds encrypted OAuth tokens + per-user
  installation→repo links. Losing it means every user has to re-link.
  Consider a nightly `rsync` to S3 or a Railway-side cron that dumps the
  storage tree to GitHub as an encrypted artifact.
- **Region failover.** Single region (US East) means a Railway regional
  outage = full outage. Hobby plan can't multi-region; the move would be
  to Pro + a stateless storage backend (Redis with replication, or DynamoDB).
- **CDN in front.** Railway's edge is fine, but a Cloudflare layer would
  give you (a) caching for `/healthz` + `/.well-known/...`, (b) free DDoS
  protection, (c) better TLS visibility (Railway-managed Let's Encrypt has
  no transparency hooks).

### Security

- **Rotate the OAuth App client secret.** It's been in CodeMirror /
  clipboard during setup. Rotate on a quarterly cadence (and after any
  laptop loss).
- **Rotate `STORAGE_ENCRYPTION_KEY` with a migration path.** Today rotating
  it invalidates every stored OAuth token (forces re-auth for all users).
  Multi-key support — read with old key, encrypt new writes with new key —
  would let you rotate without user impact.
- **Add a `SECURITY.md` disclosure email** distinct from the GitHub-owner
  email. (`SECURITY.md` already exists; check it points somewhere
  monitored.)
- **CSP + standard security headers** on the HTML responses (`/`, install
  callback pages). FastMCP doesn't ship these; add a Starlette middleware.

---

## 5. Common operations

```bash
# Tail production logs (requires Railway CLI + project token)
export RAILWAY_TOKEN=<deploy-automation token>
railway logs --service skills-registry

# Probe production
curl https://mcp.skills-registry.dev/healthz
curl https://mcp.skills-registry.dev/.well-known/oauth-authorization-server | jq

# Force a redeploy (pushes an empty commit; auto-deploys via GitHub integration)
git commit --allow-empty -m "chore: force redeploy"
git push origin main

# Read a Railway env var
railway variables --service skills-registry --kv | grep VAR_NAME

# Set a Railway env var (service-scoped)
echo "value" | railway variables --service skills-registry --set VAR_NAME=- --skip-deploys

# DNS spot-check
dig +short mcp.skills-registry.dev CNAME
echo | openssl s_client -connect mcp.skills-registry.dev:443 -servername mcp.skills-registry.dev 2>/dev/null | openssl x509 -noout -subject -dates
```

---

## 6. Where to look when things go wrong

| Symptom | First place to look |
|---|---|
| `/healthz` non-200 | Railway → service → Deployments → latest → Deploy Logs |
| `401` on `/mcp` | `JWT_SIGNING_KEY` or `STORAGE_ENCRYPTION_KEY` mismatch (most often after env var edits) |
| Webhook 401 / signature failure | `GITHUB_APP_WEBHOOK_SECRET` mismatch between Railway and GitHub App settings |
| OAuth redirect loop | OAuth App callback URL ≠ `<base_url>/auth/callback`; check both Railway env and GitHub OAuth App settings |
| `list_skills` returns "no repo linked" after install | Webhook isn't reaching us. GitHub App → Advanced → Recent Deliveries (look for non-200 responses) |
| TLS cert renewal failure | Railway → service → Settings → Networking → Custom Domain status |
| Build fails with `setuptools-scm was unable to detect version` | The pinned version in `Dockerfile` is missing — see §3.E |
| Volume mount `Permission denied` | Container must run as root (Railway volumes mount root-owned); confirm no `USER` directive in `Dockerfile` |

---

## 7. Repository map (post-launch)

```text
src/skills_mcp/
  remote_server.py     # FastMCP app assembly, env-var validation, tool registration
  github_app.py        # App JWT minter + installation-token exchange
  github_api.py        # Read-only REST wrapper (list_skill_folders, get_skill_md)
  linking.py           # Per-user KV state: user_id → {installation_id, repo, branch}
  webhooks.py          # /github/webhook handler (signature verify + auto-link)
  setup_routes.py      # /, /healthz, /github/app/callback (unauthenticated)
  frontmatter.py       # SKILL.md frontmatter parser (shared with the Go CLI)

cli/                   # Go CLI — still installs the *local* MCP, see §3.B
tests/                 # pytest suite (81 tests, all green on main)
Dockerfile             # python:3.12-slim, root user, mkdir /data/oauth at startup
railway.json           # builder=DOCKERFILE, healthcheck=/healthz
HANDOFF.md             # this file
```

---

**Last updated:** 2026-05-25, immediately after the production launch at
`https://mcp.skills-registry.dev`. The previous version of this doc (the
pre-launch playbook) is preserved in git history at the merge commit of
PR #17 if you want the original step-by-step provisioning notes.

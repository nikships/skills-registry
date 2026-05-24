"""HTTP routes for the GitHub App install flow + landing page.

Three routes:

* ``GET /`` — minimal landing page so curious browsers don't see a stack
  trace at the apex. Mostly a "what is this and how do I connect?" pointer.
* ``GET /github/app/callback`` — GitHub redirects here after the user
  completes an App install. We don't need to do anything synchronous —
  the ``installation.created`` webhook does the real work — so we just
  render a success page so the user knows the round-trip completed.
* ``GET /healthz`` — JSON liveness probe for Railway / external monitors.

These are all unauthenticated by design (the FastMCP auth provider doesn't
apply to ``custom_route``).
"""

from __future__ import annotations

from starlette.requests import Request
from starlette.responses import HTMLResponse, JSONResponse, Response

_SUCCESS_HTML = """<!doctype html>
<html lang=en>
<head>
  <meta charset=utf-8>
  <title>Skills Registry — install complete</title>
  <style>
    body {{ font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
            sans-serif; max-width: 38rem; margin: 4rem auto; padding: 0 1rem;
            color: #1a1a1a; line-height: 1.55; }}
    h1   {{ font-size: 1.6rem; margin-bottom: 0.5rem; }}
    code {{ background: #f4f4f4; padding: 0 0.3rem; border-radius: 3px; }}
    .ok  {{ color: #1a7f3c; }}
  </style>
</head>
<body>
  <h1 class=ok>You're connected.</h1>
  <p>The Skills Registry GitHub App is installed on
     <code>{repos}</code>. Go back to your MCP client (Claude Desktop,
     Cursor, VS Code, etc.) and try <code>list_skills</code> — it should
     return your skills from this repo.</p>
  <p>If you need to switch which repo is linked, re-run the App install
     and grant access to just the repo you want.</p>
</body>
</html>"""


_LANDING_HTML = """<!doctype html>
<html lang=en>
<head>
  <meta charset=utf-8>
  <title>Skills Registry MCP</title>
  <style>
    body {{ font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
            sans-serif; max-width: 38rem; margin: 4rem auto; padding: 0 1rem;
            color: #1a1a1a; line-height: 1.55; }}
    code {{ background: #f4f4f4; padding: 0 0.3rem; border-radius: 3px; }}
    a    {{ color: #1462ad; }}
  </style>
</head>
<body>
  <h1>Skills Registry MCP</h1>
  <p>This is the remote MCP endpoint for the
     <a href=https://github.com/anand-92/skills-registry>Skills Registry</a>
     project.</p>
  <p>To connect, point your MCP client at <code>{mcp_url}</code>. The first
     connection will open a GitHub OAuth window to authorize access.</p>
  <p>Once connected, install the GitHub App on your skills repo:
     <a href={install_url}>{install_url}</a></p>
</body>
</html>"""


def make_routes(*, install_url: str, mcp_url: str) -> dict[str, object]:
	"""Build the unauthenticated route handlers.

	Returns a mapping of path → handler so :func:`build_app` can wire each
	one with the FastMCP ``custom_route`` decorator.
	"""

	async def healthz(_request: Request) -> Response:
		return JSONResponse({"status": "ok"})

	async def landing(_request: Request) -> Response:
		return HTMLResponse(_LANDING_HTML.format(install_url=install_url, mcp_url=mcp_url))

	async def install_callback(request: Request) -> Response:
		# GitHub passes ``?installation_id=...&setup_action=install`` and may
		# include ``state=...`` if we set one on the install URL. We don't
		# rely on these here — the webhook does the actual linking — but we
		# do echo the install ID so the user sees that something happened.
		install_id = request.query_params.get("installation_id", "(unknown)")
		return HTMLResponse(_SUCCESS_HTML.format(repos=f"installation {install_id}"))

	return {
		"/healthz": healthz,
		"/": landing,
		"/github/app/callback": install_callback,
	}

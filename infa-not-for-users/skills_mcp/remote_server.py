"""``skills-registry-mcp`` — remote FastMCP server.

Hosted entry point for the Skills Registry. Replaces the old stdio MCP
server entirely: end users no longer install anything from PyPI; they just
point their MCP client at the hosted URL and authenticate via GitHub OAuth.

Wire-up:

* :class:`fastmcp.server.auth.providers.github.GitHubProvider` for OAuth
  (uses our pre-registered OAuth App, since GitHub doesn't support DCR).
* :class:`key_value.aio.stores.filetree.FileTreeStore` on a Railway volume
  for persistent OAuth/link state, wrapped in ``FernetEncryptionWrapper``.
* :class:`github_app.GitHubAppClient` for repo-scoped REST access via the
  Skills Registry GitHub App.
* :class:`linking.LinkStore` to map authenticated users to their installed
  registry repo.
* :class:`webhooks.WebhookHandler` to react to install / uninstall events.

Two MCP tools (read-only):

* ``search_skills(query)`` → fuzzy-ranked markdown table (top 10). A
  non-empty query is required; an empty / whitespace-only query returns
  a "search requires a term" message rather than dumping the registry.
* ``get_skill(slug)`` → verbatim ``SKILL.md`` contents.

No filesystem caching of skill content (the registry repo is the source of
truth and GitHub's edge caches handle the rest).
"""

from __future__ import annotations

import logging
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from cryptography.fernet import Fernet
from fastmcp import FastMCP
from fastmcp.server.auth.providers.github import GitHubProvider
from fastmcp.server.dependencies import get_access_token
from key_value.aio.stores.filetree import (
	FileTreeStore,
	FileTreeV1CollectionSanitizationStrategy,
	FileTreeV1KeySanitizationStrategy,
)
from key_value.aio.wrappers.encryption import FernetEncryptionWrapper
from starlette.requests import Request
from starlette.responses import Response

from . import __version__
from .analytics import posthog_client
from .github_api import SkillSummary, get_skill_md, search_skills, slugify
from .github_app import GitHubAppClient, GitHubAppCredentials
from .linking import DeliveryStore, LinkedRepo, LinkStore
from .middleware import build_middleware_stack
from .setup_routes import make_routes
from .webhooks import WebhookHandler

log = logging.getLogger("skills_mcp.remote_server")


# --------------------------------------------------------------------- env


@dataclass(frozen=True)
class ServerSettings:
	"""All deployment-time configuration, validated at boot."""

	base_url: str
	github_client_id: str
	github_client_secret: str
	github_app_id: str
	github_app_private_key: str
	github_app_webhook_secret: str
	github_app_slug: str
	jwt_signing_key: str
	storage_dir: Path
	storage_encryption_key: str
	host: str
	port: int

	@property
	def install_url(self) -> str:
		return f"https://github.com/apps/{self.github_app_slug}/installations/new"

	@property
	def mcp_url(self) -> str:
		return f"{self.base_url.rstrip('/')}/mcp"


def load_settings() -> ServerSettings:
	"""Read every required env var; fail fast and noisily if any are missing.

	We deliberately avoid silent defaults for anything that would let the
	server boot with broken auth or unencrypted storage. Per the global
	"fail-fast" guideline: misconfig caught at boot is misconfig solved.
	"""
	base_url = _require("FASTMCP_SERVER_AUTH_GITHUB_BASE_URL")
	storage_dir = Path(_require("FASTMCP_STORAGE_DIR")).expanduser().resolve()
	# Create the dir early so Fernet/FileTreeStore don't blow up later.
	storage_dir.mkdir(parents=True, exist_ok=True)
	return ServerSettings(
		base_url=base_url.rstrip("/"),
		github_client_id=_require("FASTMCP_SERVER_AUTH_GITHUB_CLIENT_ID"),
		github_client_secret=_require("FASTMCP_SERVER_AUTH_GITHUB_CLIENT_SECRET"),
		github_app_id=_require("GITHUB_APP_ID"),
		github_app_private_key=_require("GITHUB_APP_PRIVATE_KEY"),
		github_app_webhook_secret=_require("GITHUB_APP_WEBHOOK_SECRET"),
		github_app_slug=_require("GITHUB_APP_SLUG"),
		jwt_signing_key=_require("JWT_SIGNING_KEY"),
		storage_dir=storage_dir,
		storage_encryption_key=_require("STORAGE_ENCRYPTION_KEY"),
		host=os.environ.get("HOST", "0.0.0.0"),
		port=int(os.environ.get("PORT", "8000")),
	)


def _require(name: str) -> str:
	value = os.environ.get(name, "").strip()
	if not value:
		raise OSError(f"Missing required env var: {name}")
	return value


# ---------------------------------------------------------------- assembly


def build_storage(settings: ServerSettings) -> FernetEncryptionWrapper:
	"""Construct the encrypted persistent KV used by both auth + linking.

	FileTreeStore is the docs-recommended single-server backend (see
	``/servers/storage-backends``). Sanitization strategies are *required* —
	without them, OAuth client IDs that look like URLs (e.g. Claude's
	``https://claude.ai/oauth/claude-code-client-metadata``) crash the
	filesystem layer.
	"""
	base = FileTreeStore(
		data_directory=settings.storage_dir,
		key_sanitization_strategy=FileTreeV1KeySanitizationStrategy(settings.storage_dir),
		collection_sanitization_strategy=FileTreeV1CollectionSanitizationStrategy(
			settings.storage_dir
		),
	)
	return FernetEncryptionWrapper(
		key_value=base,
		fernet=Fernet(settings.storage_encryption_key),
	)


def build_auth_provider(
	settings: ServerSettings,
	storage: FernetEncryptionWrapper,
) -> GitHubProvider:
	# Request zero OAuth scopes. The only piece of user data this server
	# touches is the GitHub ``id`` (the ``sub`` claim) returned from
	# ``GET /user`` — which is public profile data, available with no
	# scope at all. All repo access goes through the GitHub App installation
	# token, not the user's OAuth token, so we never need ``repo``,
	# ``read:user``, or anything else.
	#
	# FastMCP's GitHubProvider defaults ``required_scopes`` to ``["user"]``,
	# which GitHub renders on the consent screen as "Full access … read and
	# write all user data (private email addresses, private profile
	# information, followers)". That's egregiously over-provisioned for
	# identity-only OAuth — passing ``[]`` reduces the consent prompt to
	# the minimum GitHub will render.
	return GitHubProvider(
		client_id=settings.github_client_id,
		client_secret=settings.github_client_secret,
		base_url=settings.base_url,
		jwt_signing_key=settings.jwt_signing_key,
		client_storage=storage,
		required_scopes=[],
	)


def build_server(settings: ServerSettings) -> tuple[FastMCP, LinkStore, GitHubAppClient]:
	"""Assemble the FastMCP server + register tools + custom routes.

	Returns the server plus the link store and App client so callers (like
	tests) can poke at them without re-reading env vars.

	Wires the production middleware stack (error handling, rate limiting,
	structured logging) via :func:`skills_mcp.middleware.build_middleware_stack`
	and turns on ``mask_error_details=True`` so raw exception text from
	GitHub never reaches the MCP client.
	"""
	storage = build_storage(settings)
	auth = build_auth_provider(settings, storage)
	link_store = LinkStore(storage)
	delivery_store = DeliveryStore(storage)
	app_client = GitHubAppClient(
		GitHubAppCredentials(
			app_id=settings.github_app_id,
			private_key_pem=settings.github_app_private_key,
		),
	)

	server = FastMCP(
		"skills-registry",
		instructions=(
			"Hosted GitHub-backed skill registry. Authenticate via GitHub OAuth, "
			"install the Skills Registry GitHub App on your skills repo, then "
			'call `search_skills(query="...")` to fuzzy-find skills (a non-empty '
			"query is required) and `get_skill(slug=...)` to read one. The "
			"server keeps no local cache; the registry repo is the source of "
			"truth."
		),
		version=__version__,
		auth=auth,
		middleware=build_middleware_stack(),
		mask_error_details=True,
	)
	_register_tools(server, link_store, app_client, install_url=settings.install_url)
	_register_routes(server, settings, app_client, link_store, delivery_store)
	return server, link_store, app_client


# -------------------------------------------------------------------- tools


def _register_tools(
	server: FastMCP,
	link_store: LinkStore,
	app_client: GitHubAppClient,
	*,
	install_url: str,
) -> None:
	@server.tool(
		name="search_skills",
		description=(
			"Fuzzy-search skills in your linked GitHub skill registry. Pass a "
			"non-empty `query` (matched against slug, name, and description "
			"with fzf V1-style scoring) and the top 10 ranked matches are "
			"returned as a markdown table with slug, name, and description. "
			"An empty or whitespace-only query returns no results — use a "
			"specific search term."
		),
		tags={"skills", "registry"},
		annotations={"readOnlyHint": True, "openWorldHint": True, "destructiveHint": False},
	)
	async def search_skills_tool(query: str) -> str:
		link = await _resolve_link(link_store, install_url=install_url)
		if isinstance(link, str):
			_track_not_linked(_current_user_id(), "search_skills")
			return link
		q_stripped = query.strip()
		if not q_stripped:
			return (
				"`search_skills` requires a non-empty `query`. Pass a term "
				"to fuzzy-match against slug, name, and description."
			)
		token = await app_client.mint_installation_token(link.installation_id)
		summaries = await search_skills(token, link.repo, query=q_stripped)
		posthog_client.capture(
			distinct_id=_current_user_id(),
			event="search_skills_called",
			properties={"query": q_stripped, "skill_count": len(summaries)},
		)
		if not summaries:
			return f"No skills matching `{query}` found in `{link.repo}`."
		return _format_skills_table(link.repo, summaries)

	@server.tool(
		name="get_skill",
		description=(
			"Fetch a single skill's `SKILL.md` from your linked registry repo. "
			"Returns the file contents verbatim (markdown). Supporting files "
			"(scripts, assets) are not returned in v1 — read `SKILL.md` for "
			"links to them."
		),
		tags={"skills", "registry"},
		annotations={"readOnlyHint": True, "openWorldHint": True},
	)
	async def get_skill(slug: str) -> str:
		link = await _resolve_link(link_store, install_url=install_url)
		if isinstance(link, str):
			_track_not_linked(_current_user_id(), "get_skill")
			return link
		token = await app_client.mint_installation_token(link.installation_id)
		normalized = slugify(slug)
		content = await get_skill_md(token, link.repo, normalized)
		posthog_client.capture(
			distinct_id=_current_user_id(),
			event="get_skill_called",
			properties={"slug": normalized, "found": content is not None},
		)
		if content is None:
			return f"Skill `{slug}` not found in `{link.repo}`."
		return content


async def _resolve_link(link_store: LinkStore, *, install_url: str) -> LinkedRepo | str:
	"""Return the user's link, or a friendly setup-needed message as a string.

	A ``str`` result means "tell the MCP client the user needs to install
	the GitHub App"; a :class:`LinkedRepo` means we're good to call the API.
	"""
	token = get_access_token()
	user_id = str(token.claims.get("sub", "") or "")
	if not user_id:
		return "Could not identify GitHub user from token claims. Try re-authenticating."
	link = await link_store.get_link(user_id)
	if link is None:
		return (
			"No skills repo linked. Install the Skills Registry GitHub App on "
			f"your registry repo, then retry:\n\n  {install_url}\n\n"
			"After installing, this MCP server will auto-detect your repo via "
			"webhook within a few seconds."
		)
	return link


def _current_user_id() -> str:
	"""Return the GitHub numeric user ID from the active OAuth token, or 'anonymous'."""
	token = get_access_token()
	if token is None:
		return "anonymous"
	sub = token.claims.get("sub") if token.claims else None
	return str(sub) if sub else "anonymous"


def _track_not_linked(user_id: str, tool_name: str) -> None:
	posthog_client.capture(
		distinct_id=user_id,
		event="user_not_linked",
		properties={"tool_name": tool_name},
	)


def _format_skills_table(repo: str, summaries: list[SkillSummary]) -> str:
	"""Render the registry listing as a markdown table for MCP clients."""
	plural = "" if len(summaries) == 1 else "s"
	lines = [
		f"Registry: `{repo}` ({len(summaries)} skill{plural})",
		"",
		"| slug | name | description |",
		"| --- | --- | --- |",
	]
	for s in summaries:
		desc = s.description.replace("|", "\\|").replace("\n", " ")
		lines.append(f"| `{s.slug}` | {s.name} | {desc} |")
	lines.append("")
	lines.append('Use `get_skill(slug="<slug>")` to read the SKILL.md for any row above.')
	return "\n".join(lines)


# ------------------------------------------------------------- custom routes


def _register_routes(
	server: FastMCP,
	settings: ServerSettings,
	app_client: GitHubAppClient,
	link_store: LinkStore,
	delivery_store: DeliveryStore,
) -> None:
	routes = make_routes(install_url=settings.install_url, mcp_url=settings.mcp_url)
	for path, handler in routes.items():
		server.custom_route(path, methods=["GET"])(handler)

	webhook = WebhookHandler(
		secret=settings.github_app_webhook_secret,
		app_client=app_client,
		link_store=link_store,
		delivery_store=delivery_store,
	)

	# Starlette's router dispatches plain ``async def`` endpoints with
	# ``(request)`` but treats class instances with ``__call__`` as ASGI3
	# apps — calling them with ``(scope, receive, send)``. Wrap the handler
	# in a function so the endpoint dispatch path fires; passing the
	# instance directly raises ``TypeError: __call__() takes 2 positional
	# arguments but 4 were given`` on every webhook delivery.
	async def webhook_endpoint(request: Request) -> Response:
		return await webhook(request)

	server.custom_route("/github/webhook", methods=["POST"])(webhook_endpoint)


# ---------------------------------------------------------------- entry


def build_app(settings: ServerSettings | None = None) -> tuple[ServerSettings, Any]:
	"""Build the ASGI app for uvicorn / Railway. Importable from gunicorn too."""
	settings = settings or load_settings()
	server, _link_store, _app_client = build_server(settings)
	asgi_app = server.http_app(path="/mcp")
	return settings, asgi_app


def main() -> int:
	logging.basicConfig(
		level=os.environ.get("SKILLS_LOG_LEVEL", "INFO").upper(),
		format="%(asctime)s %(levelname)s %(name)s: %(message)s",
		stream=sys.stderr,
	)
	try:
		settings, app = build_app()
	except OSError as exc:
		print(f"skills-registry-mcp: {exc}", file=sys.stderr)
		return 2
	import uvicorn  # local import — only needed when running as a script

	uvicorn.run(
		app,
		host=settings.host,
		port=settings.port,
		log_level=os.environ.get("SKILLS_LOG_LEVEL", "INFO").lower(),
	)
	return 0


__all__ = ["build_app", "build_server", "load_settings", "main", "ServerSettings"]


if __name__ == "__main__":
	sys.exit(main())

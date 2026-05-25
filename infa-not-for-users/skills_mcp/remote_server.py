"""``skill-registry-mcp`` — remote FastMCP server.

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

Two MCP tools (read-only v1 per ``REMOTE_MCP_PLAN.md``):

* ``list_skills`` → markdown table from the linked repo.
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

from . import __version__
from .github_api import SkillSummary, get_skill_md, list_skill_folders, slugify
from .github_app import GitHubAppClient, GitHubAppCredentials
from .linking import LinkedRepo, LinkStore
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
	return GitHubProvider(
		client_id=settings.github_client_id,
		client_secret=settings.github_client_secret,
		base_url=settings.base_url,
		jwt_signing_key=settings.jwt_signing_key,
		client_storage=storage,
	)


def build_server(settings: ServerSettings) -> tuple[FastMCP, LinkStore, GitHubAppClient]:
	"""Assemble the FastMCP server + register tools + custom routes.

	Returns the server plus the link store and App client so callers (like
	tests) can poke at them without re-reading env vars.
	"""
	storage = build_storage(settings)
	auth = build_auth_provider(settings, storage)
	link_store = LinkStore(storage)
	app_client = GitHubAppClient(
		GitHubAppCredentials(
			app_id=settings.github_app_id,
			private_key_pem=settings.github_app_private_key,
		),
	)

	server = FastMCP(
		"skill-registry",
		instructions=(
			"Hosted GitHub-backed skill registry. Authenticate via GitHub OAuth, "
			"install the Skills Registry GitHub App on your skills repo, then "
			"call `list_skills` to discover skills and `get_skill(slug=...)` to "
			"read one. The server keeps no local cache; the registry repo is "
			"the source of truth."
		),
		version=__version__,
		auth=auth,
	)
	_register_tools(server, link_store, app_client, install_url=settings.install_url)
	_register_routes(server, settings, app_client, link_store)
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
		name="list_skills",
		description=(
			"List every skill in your linked GitHub skill registry. Returns a "
			"markdown table with slug, name, and description."
		),
		tags={"skills", "registry"},
		annotations={"readOnlyHint": True, "openWorldHint": True},
	)
	async def list_skills() -> str:
		link = await _resolve_link(link_store, install_url=install_url)
		if isinstance(link, str):
			return link
		token = await app_client.mint_installation_token(link.installation_id)
		summaries = await list_skill_folders(token, link.repo)
		if not summaries:
			return (
				f"No skills found in `{link.repo}`. Add a skill with `SKILL.md` "
				"using the `skill-registry` CLI and they'll appear here."
			)
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
			return link
		token = await app_client.mint_installation_token(link.installation_id)
		content = await get_skill_md(token, link.repo, slugify(slug))
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
) -> None:
	routes = make_routes(install_url=settings.install_url, mcp_url=settings.mcp_url)
	for path, handler in routes.items():
		server.custom_route(path, methods=["GET"])(handler)

	webhook = WebhookHandler(
		secret=settings.github_app_webhook_secret,
		app_client=app_client,
		link_store=link_store,
	)
	server.custom_route("/github/webhook", methods=["POST"])(webhook)


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
		print(f"skill-registry-mcp: {exc}", file=sys.stderr)
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

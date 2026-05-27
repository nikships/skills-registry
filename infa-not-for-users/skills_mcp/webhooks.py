"""GitHub App webhook handler mounted at ``/github/webhook``.

Subscribed events:

* ``installation`` — ``created`` / ``deleted`` / ``suspend`` / ``unsuspend``.
* ``installation_repositories`` — ``added`` / ``removed``.

What we do on each:

* ``installation.created`` — pick the best skills repo from the installed
  set, write a ``LinkedRepo`` for the user. We DON'T auto-recover an OAuth
  identity here; instead we trust the ``installation.account.id`` GitHub
  sends as the linkable user identifier. The MCP server later confirms it
  matches the authenticated user's GitHub ``sub`` claim before serving tools.
* ``installation.deleted`` / ``installation.suspend`` — drop the link.
* ``installation_repositories.removed`` — if the linked repo was removed,
  attempt to re-pick from what's left; if nothing's left, drop the link.

Two gates run before any handler:

1. HMAC-SHA256 signature verification against ``GITHUB_APP_WEBHOOK_SECRET``
   (constant-time compare).
2. Replay protection via :class:`~skills_mcp.linking.DeliveryStore`: every
   webhook carries an ``X-GitHub-Delivery`` UUID. If we've already
   processed that ID within the redelivery window, we return 200 without
   re-running the handler. This makes replays — whether legitimate (GitHub
   manual resend) or hostile (attacker replaying a captured signed body) —
   no-ops instead of state mutations.
"""

from __future__ import annotations

import hashlib
import hmac
import json
import logging
from typing import Any

from starlette.requests import Request
from starlette.responses import JSONResponse, Response

from .analytics import posthog_client
from .github_api import repo_has_skills
from .github_app import GitHubAppClient, GitHubAppError
from .linking import DeliveryStore, LinkedRepo, LinkStore

log = logging.getLogger("skills_mcp.webhooks")


class WebhookHandler:
	"""Single-purpose Starlette callback. Hold this in the server module and
	pass it to ``mcp.custom_route``."""

	def __init__(
		self,
		*,
		secret: str,
		app_client: GitHubAppClient,
		link_store: LinkStore,
		delivery_store: DeliveryStore,
	) -> None:
		if not secret:
			raise ValueError("GitHubAppWebhookHandler requires a non-empty secret")
		self._secret = secret.encode("utf-8")
		self._app = app_client
		self._links = link_store
		self._deliveries = delivery_store

	async def __call__(self, request: Request) -> Response:
		body = await request.body()
		signature = request.headers.get("X-Hub-Signature-256", "")
		if not _verify_signature(self._secret, body, signature):
			log.warning("Rejected webhook with bad signature")
			posthog_client.capture(
				distinct_id="server",
				event="webhook_rejected",
			)
			return JSONResponse({"error": "bad signature"}, status_code=401)

		# Replay-protection check runs after signature verification: an
		# unsigned replay would already be rejected above, so we only
		# spend a KV lookup on requests that proved they hold the secret.
		delivery_id = request.headers.get("X-GitHub-Delivery", "")
		event = request.headers.get("X-GitHub-Event", "")
		if delivery_id and await self._deliveries.seen(delivery_id):
			log.info("Deduped webhook delivery %s", delivery_id)
			posthog_client.capture(
				distinct_id="server",
				event="webhook_deduped",
				properties={"event_type": event},
			)
			return JSONResponse({"deduped": delivery_id})

		try:
			payload = json.loads(body.decode("utf-8"))
		except (json.JSONDecodeError, UnicodeDecodeError):
			return JSONResponse({"error": "invalid json"}, status_code=400)

		action = payload.get("action", "")
		log.info("Webhook %s.%s delivery=%s received", event, action, delivery_id)

		handler = self._route(event, action)
		if handler is None:
			# Mark even ignored events as seen so a replay doesn't trip
			# the routing branch a second time.
			await self._deliveries.mark(delivery_id)
			return JSONResponse({"ignored": f"{event}.{action}"})

		try:
			await handler(payload)
		except GitHubAppError as exc:
			log.error("GitHub API failure handling %s.%s: %s", event, action, exc)
			# Don't mark as seen on transient errors; GitHub may retry and
			# we want the next attempt to actually run.
			return JSONResponse({"error": "github api"}, status_code=502)
		except (ValueError, KeyError, TypeError) as exc:
			log.exception("Malformed payload for %s.%s: %s", event, action, exc)
			# Bad payloads won't get better on retry — mark as seen so
			# GitHub stops re-sending the same broken event.
			await self._deliveries.mark(delivery_id)
			return JSONResponse({"error": "bad payload"}, status_code=400)
		await self._deliveries.mark(delivery_id)
		return JSONResponse({"ok": True})

	def _route(self, event: str, action: str) -> Any:
		"""Map (event, action) → bound handler, or ``None`` to ignore.

		Ignored events get a 200 with ``{ignored: ...}`` so GitHub stops
		retrying.
		"""
		match (event, action):
			case ("installation", "created" | "unsuspend"):
				return self._on_installation_created
			case ("installation", "deleted" | "suspend"):
				return self._on_installation_removed
			case ("installation_repositories", "added" | "removed"):
				return self._on_repos_changed
		return None

	# ---------------------------------------------------------- handlers

	async def _on_installation_created(self, payload: dict[str, Any]) -> None:
		install = payload["installation"]
		installation_id = int(install["id"])
		account_id = install.get("account", {}).get("id")
		if not account_id:
			log.warning("installation.created missing account.id; skipping link")
			return
		await self._adopt_best_repo(installation_id, str(account_id))

	async def _on_installation_removed(self, payload: dict[str, Any]) -> None:
		install = payload["installation"]
		installation_id = int(install["id"])
		user_id = await self._links.user_for_installation(installation_id)
		await self._links.delete_installation(installation_id)
		posthog_client.capture(
			distinct_id=user_id if user_id is not None else "server",
			event="repo_unlinked",
			properties={"installation_id": installation_id},
		)

	async def _on_repos_changed(self, payload: dict[str, Any]) -> None:
		install = payload["installation"]
		installation_id = int(install["id"])
		user_id = await self._links.user_for_installation(installation_id)
		if user_id is None:
			# Not linked yet — webhook will re-fire when the install completes.
			return
		await self._adopt_best_repo(installation_id, user_id)

	# ---------------------------------------------------- repo auto-select

	async def _adopt_best_repo(self, installation_id: int, user_id: str) -> None:
		token = await self._app.mint_installation_token(installation_id)
		repos = await self._app.list_installation_repos(token)
		if not repos:
			log.info("Installation %s has no repos; dropping link", installation_id)
			await self._links.delete_installation(installation_id)
			return
		chosen = await _pick_skills_repo(token, repos)
		if chosen is None:
			# We still link to *something* so the user gets a clear error from
			# the tools rather than radio silence — they can re-install on the
			# right repo and the next webhook will fix it.
			chosen = repos[0]
			log.warning(
				"No SKILL.md-bearing repo in install %s; tentatively linking %s",
				installation_id,
				chosen.full_name,
			)
		await self._links.set_link(
			user_id,
			LinkedRepo(
				installation_id=installation_id,
				repo=chosen.full_name,
				default_branch=chosen.default_branch,
			),
		)
		posthog_client.capture(
			distinct_id=user_id,
			event="repo_linked",
			properties={
				"installation_id": installation_id,
				"repo_name": chosen.full_name.split("/")[-1],
			},
		)


async def _pick_skills_repo(token: str, repos: list[Any]) -> Any:
	"""Prefer the obvious "this is a skills registry" repo when present.

	Selection order:
	1. ``*skills*`` substring in name AND contains SKILL.md folders.
	2. Any repo that contains SKILL.md folders.
	3. ``None`` (caller fallbacks to first repo with a warning).
	"""
	candidates = [r for r in repos if await repo_has_skills(token, r.full_name)]
	if not candidates:
		return None
	for repo in candidates:
		if "skills" in repo.full_name.lower().split("/")[-1]:
			return repo
	return candidates[0]


def _verify_signature(secret: bytes, body: bytes, header: str) -> bool:
	"""Constant-time check of the ``X-Hub-Signature-256`` header.

	GitHub sends ``sha256=<hex>``. If the header is malformed or missing,
	we reject — the alternative is letting unauthenticated POSTs reset
	users' link state.
	"""
	if not header.startswith("sha256="):
		return False
	expected = header.split("=", 1)[1].strip()
	digest = hmac.new(secret, body, hashlib.sha256).hexdigest()
	return hmac.compare_digest(expected, digest)

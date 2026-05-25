"""GitHub App client: mint JWTs, exchange for installation access tokens.

The remote MCP server never sees a user's personal GitHub credentials. Instead,
the deployer registers a public **GitHub App**, the user installs it on their
registry repo, and the server signs short-lived (≤9 min) RS256 JWTs as the
App to mint per-installation REST tokens (≤1 hour). Those installation tokens
are what we use to call ``GET /repos/.../contents/...``.

Why this shape:

* User OAuth tokens leak the user's whole GitHub identity; installation tokens
  are scoped to the repos the user explicitly granted the App.
* App JWTs are signed with our private key and verified by GitHub, so the
  trust boundary is "anyone holding the private key", not "anyone holding any
  user's token".
* Installation tokens auto-expire — no revocation logic needed for normal
  operation.

We talk to GitHub's REST API exclusively over httpx. No ``gh`` binary, no
``git``, no SSH — the container can be the smallest python:slim image.
"""

from __future__ import annotations

import asyncio
import logging
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any, TypeVar

import httpx
import jwt

log = logging.getLogger("skills_mcp.github_app")

T = TypeVar("T")

# GitHub caps App JWTs at 10 minutes; 9 leaves headroom for clock skew.
_APP_JWT_TTL_S = 9 * 60

# We re-mint installation tokens on demand rather than caching to keep this
# layer stateless; the volume of tool calls per request is tiny.
_GITHUB_API = "https://api.github.com"
_API_VERSION = "2022-11-28"


class GitHubAppError(RuntimeError):
	"""Raised when an App-scoped GitHub API call fails non-recoverably."""

	def __init__(self, message: str, *, status: int | None = None) -> None:
		super().__init__(message)
		self.status = status


@dataclass(frozen=True)
class GitHubAppCredentials:
	"""All the secrets needed to act as the GitHub App."""

	app_id: str
	private_key_pem: str

	def __post_init__(self) -> None:
		if not self.app_id.strip():
			raise ValueError("GitHub App ID must be non-empty.")
		if "BEGIN" not in self.private_key_pem or "PRIVATE KEY" not in self.private_key_pem:
			raise ValueError("GitHub App private key must be a PEM-encoded RSA private key.")


@dataclass(frozen=True)
class InstallationRepo:
	"""One repository accessible to an App installation."""

	full_name: str  # ``owner/repo``
	default_branch: str


class GitHubAppClient:
	"""Stateless client: mint JWTs, mint installation tokens, list repos.

	A single instance is safe to share across requests — there's no mutable
	state and httpx is async-safe. We construct one ``AsyncClient`` per call
	to avoid leaking connections during shutdown; tool calls are infrequent
	enough that the connection-pool win isn't worth the lifecycle complexity.
	"""

	def __init__(self, creds: GitHubAppCredentials, *, http_timeout_s: float = 10.0) -> None:
		self._creds = creds
		self._timeout = http_timeout_s

	# ---------------------------------------------------------------- JWT minting

	def mint_app_jwt(self, now: int | None = None) -> str:
		"""Sign a JWT as the App. Used as the bearer token for App-scoped
		endpoints like ``POST /app/installations/{id}/access_tokens``."""
		issued_at = now if now is not None else int(time.time())
		payload = {
			# GitHub recommends backdating ``iat`` by 60 seconds to absorb clock skew.
			"iat": issued_at - 60,
			"exp": issued_at + _APP_JWT_TTL_S,
			"iss": self._creds.app_id,
		}
		return jwt.encode(payload, self._creds.private_key_pem, algorithm="RS256")

	# ------------------------------------------------------- installation tokens

	async def mint_installation_token(self, installation_id: int) -> str:
		"""Exchange an App JWT for an installation access token.

		Installation tokens last ~1 hour; callers should not cache aggressively.
		"""
		app_jwt = self.mint_app_jwt()
		url = f"{_GITHUB_API}/app/installations/{installation_id}/access_tokens"
		async with httpx.AsyncClient(timeout=self._timeout) as http:
			resp = await http.post(url, headers=_headers(app_jwt))
		if resp.status_code != httpx.codes.CREATED:
			raise GitHubAppError(
				f"mint installation token failed: {resp.status_code} {resp.text}",
				status=resp.status_code,
			)
		body = resp.json()
		token = body.get("token")
		if not isinstance(token, str) or not token:
			raise GitHubAppError(f"installation token response missing 'token': {body!r}")
		return token

	# --------------------------------------------------------- installation repos

	async def list_installation_repos(self, installation_token: str) -> list[InstallationRepo]:
		"""Return every repo this installation has access to.

		GitHub paginates at 100 per page; we walk all pages so deployments
		with sprawling installations still see every repo.
		"""
		repos: list[InstallationRepo] = []
		page = 1
		async with httpx.AsyncClient(timeout=self._timeout) as http:
			while True:
				resp = await http.get(
					f"{_GITHUB_API}/installation/repositories",
					headers=_headers(installation_token),
					params={"per_page": "100", "page": str(page)},
				)
				if resp.status_code != httpx.codes.OK:
					raise GitHubAppError(
						f"list installation repos failed: {resp.status_code} {resp.text}",
						status=resp.status_code,
					)
				body = resp.json()
				entries = body.get("repositories", []) if isinstance(body, dict) else []
				repos.extend(parsed for parsed in (_parse_repo(e) for e in entries) if parsed)
				if len(entries) < 100:
					break
				page += 1
		return repos


def _headers(bearer: str) -> dict[str, str]:
	return {
		"Accept": "application/vnd.github+json",
		"Authorization": f"Bearer {bearer}",
		"X-GitHub-Api-Version": _API_VERSION,
	}


def _parse_repo(entry: Any) -> InstallationRepo | None:
	if not isinstance(entry, dict) or not isinstance(entry.get("full_name"), str):
		return None
	return InstallationRepo(
		full_name=entry["full_name"],
		default_branch=entry.get("default_branch") or "main",
	)


# ------------------------------------------------------------ retry helpers


async def with_retry(
	coro_factory: Callable[[], Awaitable[T]],
	*,
	attempts: int = 3,
	base_delay_s: float = 0.5,
	retry_on: tuple[int, ...] = (502, 503, 504),
) -> T:
	"""Run ``coro_factory()`` with exponential backoff on transient errors.

	Shared retry policy for GitHub-touching code: on a status code in
	``retry_on``, sleep ``base_delay_s * 2**attempt`` and retry. Non-matching
	errors and the final attempt re-raise.
	"""
	for attempt in range(attempts):
		try:
			return await coro_factory()
		except GitHubAppError as exc:
			if exc.status not in retry_on or attempt == attempts - 1:
				raise
			delay = base_delay_s * (2**attempt)
			log.warning(
				"GitHub call retry %d/%d after %s; sleeping %.1fs",
				attempt + 1,
				attempts,
				exc,
				delay,
			)
			await asyncio.sleep(delay)
	# The loop returns or re-raises on every iteration; this is unreachable.
	raise AssertionError("with_retry: loop exited without returning or raising")

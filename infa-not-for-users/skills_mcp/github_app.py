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

Installation tokens are cached in-process per ``installation_id`` until they
near expiry. The cache is intentionally per-process: Railway runs one
container today, and the existing :class:`FileTreeStore` linking backend is
also single-instance. Horizontal scale would require swapping in Redis for
both pieces in lockstep — see ``infa-not-for-users/README.md``.
"""

from __future__ import annotations

import asyncio
import datetime as _dt
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

_GITHUB_API = "https://api.github.com"
_API_VERSION = "2022-11-28"

# Refresh an installation token this many seconds before its advertised
# ``expires_at`` so concurrent in-flight calls aren't racing the cutoff.
_TOKEN_REFRESH_SKEW_S = 60.0

# Fallback TTL when GitHub omits ``expires_at`` (shouldn't happen, but the
# API contract permits it in theory). Installation tokens last ~1 hour; we
# expire ours at 50 minutes to stay safe.
_TOKEN_DEFAULT_TTL_S = 50 * 60.0


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
	"""Mint JWTs, mint installation tokens (cached), list repos.

	A single instance is safe to share across requests. We construct one
	``AsyncClient`` per call to avoid leaking connections during shutdown;
	tool calls are infrequent enough that the connection-pool win isn't
	worth the lifecycle complexity.

	Installation tokens *are* cached because they're valid ~1 hour and
	minting one costs a full round trip (JWT sign + POST to GitHub) that
	would otherwise be paid on every single tool call. The cache is keyed
	on ``installation_id``. Cache hits return without acquiring any lock;
	cache misses funnel through an ``asyncio.Lock`` (double-checked) so
	concurrent first-time requests don't both fire the mint.
	"""

	def __init__(self, creds: GitHubAppCredentials, *, http_timeout_s: float = 10.0) -> None:
		self._creds = creds
		self._timeout = http_timeout_s
		# installation_id → (token, monotonic_expires_at). Monotonic time
		# keeps us correct across wall-clock jumps (NTP slew on Railway).
		self._token_cache: dict[int, tuple[str, float]] = {}
		self._cache_lock = asyncio.Lock()

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
		"""Return a valid installation token, minting fresh only when needed.

		Cache hit (token still valid for ``_TOKEN_REFRESH_SKEW_S``+ seconds)
		returns immediately without touching the lock — the hot path is
		fully concurrent. Only cache miss / near-expiry takes the lock,
		re-checks the cache (in case a peer just minted), and otherwise
		burns a fresh mint round-trip. This double-checked pattern keeps
		the "one mint per installation per refresh window" guarantee
		without serializing the common case.
		"""
		cached = self._token_cache.get(installation_id)
		if cached is not None and cached[1] - time.monotonic() > _TOKEN_REFRESH_SKEW_S:
			return cached[0]
		async with self._cache_lock:
			cached = self._token_cache.get(installation_id)
			if cached is not None and cached[1] - time.monotonic() > _TOKEN_REFRESH_SKEW_S:
				return cached[0]
			token, ttl_s = await self._fetch_installation_token(installation_id)
			expires_at = time.monotonic() + ttl_s
			self._token_cache[installation_id] = (token, expires_at)
			return token

	async def _fetch_installation_token(self, installation_id: int) -> tuple[str, float]:
		"""Exchange an App JWT for a fresh installation token + its TTL.

		Returns ``(token, ttl_in_seconds)``. TTL comes from GitHub's
		``expires_at`` ISO timestamp when present; falls back to a safe
		default when the field is missing or unparseable.
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
		return token, _ttl_from_expires_at(body.get("expires_at"))

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
				repos.extend(filter(None, map(_parse_repo, entries)))
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


def _ttl_from_expires_at(value: Any) -> float:
	"""Compute seconds-until-expiry from GitHub's ``expires_at`` ISO string.

	GitHub returns ISO 8601 timestamps like ``2024-06-01T12:34:56Z``. We
	convert to a wall-clock delta, then return the positive remaining
	seconds. Missing / malformed / already-expired inputs fall back to
	``_TOKEN_DEFAULT_TTL_S`` (50 min) — safer than zero, which would
	trigger an immediate re-mint loop.
	"""
	if not isinstance(value, str) or not value:
		return _TOKEN_DEFAULT_TTL_S
	try:
		# ``fromisoformat`` accepts ``Z`` from Python 3.11+; normalize for
		# 3.10 compatibility. The subtraction also lives in the try block
		# because a parsed-but-naive datetime would raise ``TypeError`` when
		# subtracted from the timezone-aware ``now`` — same fall-back applies.
		normalized = value.replace("Z", "+00:00")
		expires = _dt.datetime.fromisoformat(normalized)
		now = _dt.datetime.now(_dt.timezone.utc)
		remaining = (expires - now).total_seconds()
	except (ValueError, TypeError):
		return _TOKEN_DEFAULT_TTL_S
	if remaining <= 0:
		return _TOKEN_DEFAULT_TTL_S
	return remaining


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

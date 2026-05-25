"""Read-only GitHub REST wrapper used by the MCP tool handlers.

Operates on an **installation access token** minted via :mod:`github_app`.
The tools call :func:`list_skill_folders` and :func:`get_skill_md`; both walk
the GitHub REST Contents API rather than the Git Data API so we never have
to chase tree SHAs for read-only operations. (Writes would still want the
Git Data API for atomicity, but remote v1 is read-only.)

Network rules: one ``AsyncClient`` per public call, 10s default timeout, no
internal retries. Per-folder ``SKILL.md`` fetches run in parallel but are
bounded by :data:`_FANOUT_CONCURRENCY`. The cap matters because a registry
with hundreds of folders would otherwise burst hundreds of concurrent calls
into GitHub's Contents API and trip the secondary rate limit (≈ 80 RPS per
source IP); the MCP-level rate limiter is per-user, not per-GitHub-call.
"""

from __future__ import annotations

import asyncio
import base64
import logging
import re
from dataclasses import dataclass
from typing import Any

import httpx

from .frontmatter import first_paragraph, parse_frontmatter
from .github_app import GitHubAppError

log = logging.getLogger("skills_mcp.github_api")

_GITHUB_API = "https://api.github.com"
_API_VERSION = "2022-11-28"
_SLUG_RE = re.compile(r"[^a-z0-9]+")
# Reused by ``_score_skill`` to split queries and field text into tokens.
# Hoisted out of the function body so we don't re-parse the pattern on
# every (query, summary) pair — ``search_skills`` calls the scorer once
# per folder in the registry.
_TOKEN_RE = re.compile(r"[^a-z0-9]+")

# Cap on concurrent SKILL.md fetches per ``list_skill_folders`` /
# ``repo_has_skills`` invocation. Set to 8 because (a) it's the httpx
# default connection-pool soft limit, so we don't pay reconnection cost,
# and (b) it keeps even a 500-folder registry under GitHub's secondary
# rate-limit headroom.
_FANOUT_CONCURRENCY = 8


def slugify(name: str) -> str:
	"""Normalize a skill name into a filesystem-safe registry slug.

	Kept here (not in github_app) because it's a registry-format concern.
	Same rules the Go CLI uses so slugs round-trip cleanly between the two
	implementations.
	"""
	return _SLUG_RE.sub("_", name.strip().lower()).strip("_") or "skill"


@dataclass(frozen=True)
class SkillSummary:
	"""One row in the registry listing returned by :func:`list_skill_folders`."""

	slug: str
	name: str
	description: str


def _score_skill(query: str, summary: SkillSummary) -> int:
	"""Lightweight fuzzy scorer for skill summaries.

	Slightly prioritizes field relevance and token matching.
	Weights: name = 2x, description = 1x, slug = 1x.
	"""
	q = query.strip().lower()
	if not q:
		return 0

	slug = summary.slug.lower()
	name = summary.name.lower()
	desc = summary.description.lower()

	# Split query into tokens (non-empty lowercase words)
	q_tokens = [t for t in _TOKEN_RE.split(q) if t]

	score = 0
	for field, w in [(slug, 1), (name, 2), (desc, 1)]:
		if field == q:
			score += 1000 * w
		elif field.startswith(q):
			score += 500 * w
		elif q in field:
			score += 250 * w

		f_tokens = {t for t in _TOKEN_RE.split(field) if t}
		overlap_count = sum(1 for t in q_tokens if t in f_tokens)
		score += 100 * w * overlap_count

	return score


async def search_skills(
	token: str,
	repo: str,
	query: str = "",
	*,
	timeout_s: float = 10.0,
) -> list[SkillSummary]:
	"""Search skills via fuzzy matching.

	If query is empty or whitespace, returns all skills sorted by slug (alphabetical).
	Otherwise, scores summaries, sorts by score descending (with slug alphabetical
	sub-sort for determinism), and returns the top 10 matches.
	"""
	summaries = await list_skill_folders(token, repo, timeout_s=timeout_s)
	q_stripped = query.strip()
	if not q_stripped:
		return summaries

	scored_summaries = []
	for s in summaries:
		score = _score_skill(q_stripped, s)
		if score > 0:
			scored_summaries.append((score, s))

	scored_summaries.sort(key=lambda item: (-item[0], item[1].slug))
	return [s for _, s in scored_summaries[:10]]


async def list_skill_folders(
	token: str,
	repo: str,
	*,
	timeout_s: float = 10.0,
) -> list[SkillSummary]:
	"""Enumerate top-level folders in ``repo`` that contain a ``SKILL.md``.

	Skips dotfolders and well-known noise (``node_modules``, ``__pycache__``).
	Each entry is hydrated by reading the folder's ``SKILL.md`` in parallel,
	but parallelism is capped at :data:`_FANOUT_CONCURRENCY` so a large
	registry can't burst hundreds of simultaneous calls into GitHub.
	A folder without ``SKILL.md`` is silently dropped — it's not a skill.
	"""
	sem = asyncio.Semaphore(_FANOUT_CONCURRENCY)
	async with httpx.AsyncClient(timeout=timeout_s) as http:
		entries = await _contents(http, repo, "", token)
		if not isinstance(entries, list):
			return []
		folders = [e["name"] for e in entries if _is_skill_folder_entry(e)]
		results = await asyncio.gather(
			*(_summarize_folder(http, repo, name, token, sem) for name in folders),
			return_exceptions=True,
		)
	summaries: list[SkillSummary] = []
	for slug, result in zip(folders, results, strict=False):
		if isinstance(result, BaseException):
			log.warning("Skipping skill %s in %s: %s", slug, repo, result)
			continue
		if result is not None:
			summaries.append(result)
	summaries.sort(key=lambda s: s.slug)
	return summaries


async def get_skill_md(
	token: str,
	repo: str,
	slug: str,
	*,
	timeout_s: float = 10.0,
) -> str | None:
	"""Return the verbatim contents of ``<slug>/SKILL.md`` from ``repo``.

	Returns ``None`` if the file does not exist (404). Other errors raise.
	"""
	async with httpx.AsyncClient(timeout=timeout_s) as http:
		return await _fetch_skill_md(http, repo, slug, token)


async def repo_has_skills(token: str, repo: str, *, timeout_s: float = 10.0) -> bool:
	"""Cheap "does this repo look like a skills registry?" check.

	True iff the top-level listing contains at least one folder with a
	``SKILL.md``. Probes folders sequentially and short-circuits on the
	first hit so a repo whose first folder is a skill costs exactly two
	GitHub calls, regardless of how many other folders sit alongside.
	Used by the webhook handler to pick which repo to link when an
	installation grants access to multiple.
	"""
	async with httpx.AsyncClient(timeout=timeout_s) as http:
		entries = await _contents(http, repo, "", token, allow_404=True)
		if not isinstance(entries, list):
			return False
		for entry in entries:
			if not _is_skill_folder_entry(entry):
				continue
			if await _fetch_skill_md(http, repo, entry["name"], token) is not None:
				return True
	return False


# --------------------------------------------------------------- internals


async def _summarize_folder(
	http: httpx.AsyncClient,
	repo: str,
	slug: str,
	token: str,
	sem: asyncio.Semaphore,
) -> SkillSummary | None:
	# Acquire under the per-call semaphore so concurrent folder probes
	# stay below ``_FANOUT_CONCURRENCY``. Released automatically on exit.
	async with sem:
		text = await _fetch_skill_md(http, repo, slug, token)
	if text is None:
		return None
	name, description = _parse_skill_md(text, default_name=slug)
	return SkillSummary(slug=slug, name=name, description=description)


async def _fetch_skill_md(
	http: httpx.AsyncClient,
	repo: str,
	slug: str,
	token: str,
) -> str | None:
	"""Fetch and base64-decode ``<slug>/SKILL.md``; ``None`` if absent/invalid."""
	blob = await _contents(http, repo, f"{slug}/SKILL.md", token, allow_404=True)
	if not isinstance(blob, dict) or blob.get("encoding") != "base64":
		return None
	return base64.b64decode(blob["content"]).decode("utf-8", errors="replace")


def _is_skill_folder_entry(entry: Any) -> bool:
	if not isinstance(entry, dict) or entry.get("type") != "dir":
		return False
	name = entry.get("name", "")
	if not isinstance(name, str) or name.startswith("."):
		return False
	return name not in {"node_modules", "__pycache__"}


async def _contents(
	http: httpx.AsyncClient,
	repo: str,
	path: str,
	token: str,
	*,
	allow_404: bool = False,
) -> Any:
	url = f"{_GITHUB_API}/repos/{repo}/contents/{path}"
	resp = await http.get(url, headers=_headers(token))
	if resp.status_code == httpx.codes.NOT_FOUND and allow_404:
		return None
	if resp.status_code != httpx.codes.OK:
		raise GitHubAppError(
			f"GET {url} → {resp.status_code} {resp.text[:200]}",
			status=resp.status_code,
		)
	return resp.json()


def _headers(token: str) -> dict[str, str]:
	return {
		"Accept": "application/vnd.github+json",
		"Authorization": f"Bearer {token}",
		"X-GitHub-Api-Version": _API_VERSION,
	}


def _parse_skill_md(text: str, *, default_name: str) -> tuple[str, str]:
	"""Pull ``name`` and ``description`` from frontmatter; fall back to body.

	Same contract as the Go CLI so a skill round-trips identically between
	the two implementations.
	"""
	meta, body = parse_frontmatter(text)
	name = meta.get("name", default_name).strip() or default_name
	description = meta.get("description") or first_paragraph(body)
	description = " ".join(description.split())
	return name, description[:300]

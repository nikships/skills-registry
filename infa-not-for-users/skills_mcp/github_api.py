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

# Word-boundary characters used by the fuzzy scorer to award bonus points
# when a matched char lands right after one of these delimiters.
_WORD_BOUNDARY_CHARS = frozenset(" \t\n_-/.\\:")

# Scoring constants — chosen to mirror fzf's V1 weighting so callers get
# the same ranking intuition as the canonical industry-standard fuzzy
# finder. Tuning these is a contract change with the Go CLI; bump both
# in lockstep (see ``cli/cmd/skills-registry/search.go``).
_BASE_MATCH_SCORE = 16
_BOUNDARY_BONUS = 8
_CAMEL_BONUS = 7
_CONSECUTIVE_BONUS = 5
_CASE_BONUS = 1
_GAP_PENALTY = 2

# Field weights for ``_score_skill``. Name is the most semantically
# precise label; slug and description are tiebreakers. Aligned with the
# Go CLI.
_FIELD_WEIGHTS = (("name", 2), ("slug", 1), ("description", 1))

# Hard cap on results returned by ``search_skills`` when a query is
# given. Matches the CLI.
_SEARCH_TOP_N = 10

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


def _find_alignment(q_lower: str, t_lower: str) -> list[int] | None:
	"""Locate the tightest right-anchored alignment of ``q_lower`` in
	``t_lower``.

	Returns the indices in ``t_lower`` where each query char matches, or
	``None`` if any query char doesn't appear in order. Does a forward
	pass to find any valid alignment, then a backward pass from the end
	index to snap each match onto its latest valid position — so
	``"tool"`` against ``"python tooling"`` lines up on the contiguous
	``tool`` inside ``tooling`` rather than the scattered alignment a
	pure forward greedy walk would pick.
	"""
	q_len = len(q_lower)
	# Forward pass — find any valid alignment; we only need the end index.
	qi = 0
	end_idx = -1
	for ti, ch in enumerate(t_lower):
		if ch == q_lower[qi]:
			qi += 1
			if qi == q_len:
				end_idx = ti
				break
	if end_idx < 0:
		return None
	# Backward tighten.
	matches = [0] * q_len
	qi = q_len - 1
	ti = end_idx
	while ti >= 0 and qi >= 0:
		if t_lower[ti] == q_lower[qi]:
			matches[qi] = ti
			qi -= 1
		ti -= 1
	if qi >= 0:
		# Defensive: the forward pass already proved a match exists, so
		# this branch is unreachable in practice. Return None rather
		# than asserting because the caller treats it as no-match.
		return None
	return matches


def _char_score(
	q_pos: int,
	t_pos: int,
	matches: list[int],
	query: str,
	text: str,
	t_lower: str,
) -> int:
	"""Score one matched char in the alignment produced by
	``_find_alignment``."""
	score = _BASE_MATCH_SCORE
	if t_pos == 0 or t_lower[t_pos - 1] in _WORD_BOUNDARY_CHARS:
		score += _BOUNDARY_BONUS
	elif t_pos > 0 and text[t_pos].isupper() and text[t_pos - 1].islower():
		score += _CAMEL_BONUS
	if q_pos > 0 and matches[q_pos] == matches[q_pos - 1] + 1:
		score += _CONSECUTIVE_BONUS
	if text[t_pos] == query[q_pos]:
		score += _CASE_BONUS
	return score


def _fuzzy_score(query: str, text: str) -> int:
	"""Score a (query, text) pair using fzf V1-style fuzzy matching.

	See ``_find_alignment`` for the alignment logic and ``_char_score``
	for the per-char weighting. Returns 0 when no alignment exists.

	Mirrors ``fuzzyScore`` in ``cli/cmd/skills-registry/search.go``;
	bump both together.
	"""
	if not query or not text:
		return 0
	q_lower = query.lower()
	t_lower = text.lower()
	q_len = len(q_lower)
	if q_len > len(t_lower):
		return 0
	matches = _find_alignment(q_lower, t_lower)
	if matches is None:
		return 0
	score = sum(
		_char_score(q_pos, t_pos, matches, query, text, t_lower)
		for q_pos, t_pos in enumerate(matches)
	)
	span = matches[-1] - matches[0] + 1
	score -= (span - q_len) * _GAP_PENALTY
	return max(0, score)


def _score_skill(query: str, summary: SkillSummary) -> int:
	"""Score a skill summary against a query.

	Sums the fuzzy scores of slug / name / description, each weighted by
	``_FIELD_WEIGHTS``. Returns 0 when no field matches.

	Aligned with the Go CLI implementation; both sides MUST stay in sync
	or callers will see different rankings depending on which surface
	(MCP or CLI) they use.
	"""
	q = query.strip()
	if not q:
		return 0
	return sum(
		_fuzzy_score(q, getattr(summary, field)) * weight for field, weight in _FIELD_WEIGHTS
	)


async def search_skills(
	token: str,
	repo: str,
	query: str = "",
	*,
	timeout_s: float = 10.0,
) -> list[SkillSummary]:
	"""Search skills via fuzzy matching.

	An empty / whitespace-only query returns an empty list — ``search``
	requires a search term. Callers wanting the full registry should use
	``list_skill_folders`` directly (or the CLI's ``skills-registry list``).

	Non-empty queries are scored via ``_score_skill`` (fzf V1-style with
	field weighting), filtered to non-zero scores, sorted by score
	descending with an alphabetical-slug tiebreaker, and capped at
	``_SEARCH_TOP_N`` results.
	"""
	q_stripped = query.strip()
	if not q_stripped:
		return []

	summaries = await list_skill_folders(token, repo, timeout_s=timeout_s)
	scored_summaries: list[tuple[int, SkillSummary]] = []
	for s in summaries:
		score = _score_skill(q_stripped, s)
		if score > 0:
			scored_summaries.append((score, s))

	scored_summaries.sort(key=lambda item: (-item[0], item[1].slug))
	return [s for _, s in scored_summaries[:_SEARCH_TOP_N]]


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
		elif result is not None:
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

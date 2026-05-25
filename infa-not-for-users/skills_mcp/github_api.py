"""Read-only GitHub REST wrapper used by the MCP tool handlers.

Operates on an **installation access token** minted via :mod:`github_app`.
The tools call :func:`list_skill_folders` and :func:`get_skill_md`; both walk
the GitHub REST Contents API rather than the Git Data API so we never have
to chase tree SHAs for read-only operations. (Writes would still want the
Git Data API for atomicity, but remote v1 is read-only.)

Network rules: one ``AsyncClient`` per public call, 10s default timeout, no
internal retries. Per-folder ``SKILL.md`` fetches run in parallel via
``asyncio.gather`` so a registry with N skills costs ~N concurrent requests,
not N sequential ones.
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


async def list_skill_folders(
	token: str,
	repo: str,
	*,
	timeout_s: float = 10.0,
) -> list[SkillSummary]:
	"""Enumerate top-level folders in ``repo`` that contain a ``SKILL.md``.

	Skips dotfolders and well-known noise (``node_modules``, ``__pycache__``).
	Each entry is hydrated by reading the folder's ``SKILL.md`` in parallel.
	A folder without ``SKILL.md`` is silently dropped — it's not a skill.
	"""
	async with httpx.AsyncClient(timeout=timeout_s) as http:
		entries = await _contents(http, repo, "", token)
		if not isinstance(entries, list):
			return []
		folders = [e["name"] for e in entries if _is_skill_folder_entry(e)]
		results = await asyncio.gather(
			*(_summarize_folder(http, repo, name, token) for name in folders),
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
	``SKILL.md``. Used by the webhook handler to pick which repo to link
	when an installation grants access to multiple.
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
) -> SkillSummary | None:
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

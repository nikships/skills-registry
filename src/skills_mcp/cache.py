"""Local skill cache for ``get_skill``.

Caches each downloaded skill at ``~/.cache/skills-mcp/skills/<slug>/`` with a
sibling ``<slug>.meta.json`` storing the registry tree SHA at the time of
download. On the next ``get_skill`` call we ask the registry for the current
SHA and skip the network round-trip if it matches.
"""

from __future__ import annotations

import json
import os
import shutil
from dataclasses import dataclass
from pathlib import Path


def cache_root() -> Path:
	base = os.environ.get("XDG_CACHE_HOME")
	root = Path(base).expanduser() if base else Path.home() / ".cache"
	return root / "skills-mcp" / "skills"


@dataclass(frozen=True)
class CachedSkill:
	"""A previously-downloaded skill on disk."""

	slug: str
	path: Path
	tree_sha: str


def lookup(slug: str) -> CachedSkill | None:
	"""Return cached skill info if a valid entry exists, else ``None``."""
	folder = cache_root() / slug
	meta_path = cache_root() / f"{slug}.meta.json"
	if not folder.is_dir() or not meta_path.is_file():
		return None
	try:
		data = json.loads(meta_path.read_text(encoding="utf-8"))
	except (OSError, json.JSONDecodeError):
		return None
	sha = data.get("tree_sha")
	if not isinstance(sha, str) or not sha:
		return None
	return CachedSkill(slug=slug, path=folder, tree_sha=sha)


def reserve(slug: str) -> Path:
	"""Return an empty folder ready to receive a fresh download.

	If a previous version exists it is wiped. Caller is responsible for
	calling :func:`commit` after writing files so the meta file matches.
	"""
	folder = cache_root() / slug
	if folder.exists():
		shutil.rmtree(folder)
	folder.mkdir(parents=True, exist_ok=True)
	return folder


def commit(slug: str, tree_sha: str) -> None:
	"""Write the meta file recording ``tree_sha`` for the cached download."""
	meta_path = cache_root() / f"{slug}.meta.json"
	meta_path.parent.mkdir(parents=True, exist_ok=True)
	meta_path.write_text(
		json.dumps({"tree_sha": tree_sha}, indent=2) + "\n",
		encoding="utf-8",
	)

"""Shared pytest fixtures for the skills-mcp test suite."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from pathlib import Path

import pytest


def _build_text(body: str, frontmatter: Mapping[str, str] | None) -> str:
	if not frontmatter:
		return body
	lines = ["---"]
	for key, value in frontmatter.items():
		lines.append(f"{key}: {value}")
	lines.append("---")
	if body and not body.startswith("\n"):
		lines.append("")
	return "\n".join(lines) + ("\n" + body if body else "\n")


@pytest.fixture
def make_skill() -> Callable[..., Path]:
	"""Return a helper that writes a fake SKILL.md file inside ``root``.

	Usage: ``main = make_skill(root, "my-skill", "Body text", frontmatter={"name": "X"})``.

	Returns the path to the created SKILL.md file. The skill's folder is
	``root/<name>`` and the file name defaults to ``SKILL.md``.
	"""

	def _make(
		root: Path,
		name: str,
		body: str = "",
		frontmatter: Mapping[str, str] | None = None,
		main_file_name: str = "SKILL.md",
	) -> Path:
		folder = root / name
		folder.mkdir(parents=True, exist_ok=True)
		main = folder / main_file_name
		main.write_text(_build_text(body, frontmatter), encoding="utf-8")
		return main

	return _make

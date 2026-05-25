"""Tests for the read-only GitHub REST wrapper used by the tools."""

from __future__ import annotations

import base64
from collections.abc import Callable
from typing import Any

import httpx
import pytest

from skills_mcp.github_api import (
	get_skill_md,
	list_skill_folders,
	repo_has_skills,
	slugify,
)
from skills_mcp.github_app import GitHubAppError


def test_slugify_normalizes() -> None:
	assert slugify("Hello World!") == "hello_world"
	assert slugify("  multiple   spaces  ") == "multiple_spaces"
	assert slugify("CamelCase-mixed_123") == "camelcase_mixed_123"
	# Falls back to "skill" when input would normalize to empty.
	assert slugify("!!!") == "skill"


async def test_list_skill_folders_returns_summaries(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/": _dir_listing(["alpha", "beta"]),
			"https://api.github.com/repos/acme/skills/contents/alpha/SKILL.md": _skill_md(
				"alpha", "Alpha", "First skill"
			),
			"https://api.github.com/repos/acme/skills/contents/beta/SKILL.md": _skill_md(
				"beta", "Beta", "Second skill"
			),
		}
	)
	_install_mock_transport(monkeypatch, handler)

	summaries = await list_skill_folders("token", "acme/skills")
	assert len(summaries) == 2
	# Result is sorted by slug.
	assert summaries[0].slug == "alpha"
	assert summaries[0].name == "Alpha"
	assert summaries[0].description == "First skill"
	assert summaries[1].slug == "beta"


async def test_list_skill_folders_skips_dot_and_known_noise(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/": _dir_listing(
				[".github", "node_modules", "__pycache__", "real_skill"],
			),
			"https://api.github.com/repos/acme/skills/contents/real_skill/SKILL.md": _skill_md(
				"real_skill", "Real", "Yes"
			),
		}
	)
	_install_mock_transport(monkeypatch, handler)

	summaries = await list_skill_folders("token", "acme/skills")
	assert [s.slug for s in summaries] == ["real_skill"]


async def test_list_skill_folders_drops_folder_without_skill_md(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/": _dir_listing(
				["with_md", "naked"]
			),
			"https://api.github.com/repos/acme/skills/contents/with_md/SKILL.md": _skill_md(
				"with_md", "Has md", "y"
			),
			# `naked/SKILL.md` returns 404 → silently dropped.
			"https://api.github.com/repos/acme/skills/contents/naked/SKILL.md": (404, ""),
		}
	)
	_install_mock_transport(monkeypatch, handler)

	summaries = await list_skill_folders("token", "acme/skills")
	assert [s.slug for s in summaries] == ["with_md"]


async def test_get_skill_md_returns_decoded_text(monkeypatch: pytest.MonkeyPatch) -> None:
	body = "---\nname: Foo\n---\nHello"
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/foo/SKILL.md": _file_blob(body),
		}
	)
	_install_mock_transport(monkeypatch, handler)

	content = await get_skill_md("token", "acme/skills", "foo")
	assert content == body


async def test_get_skill_md_returns_none_on_404(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/missing/SKILL.md": (404, ""),
		}
	)
	_install_mock_transport(monkeypatch, handler)
	assert await get_skill_md("token", "acme/skills", "missing") is None


async def test_get_skill_md_raises_on_other_error(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/foo/SKILL.md": (500, "boom"),
		}
	)
	_install_mock_transport(monkeypatch, handler)
	with pytest.raises(GitHubAppError) as exc:
		await get_skill_md("token", "acme/skills", "foo")
	assert exc.value.status == 500


async def test_repo_has_skills_true(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/": _dir_listing(["foo"]),
			"https://api.github.com/repos/acme/skills/contents/foo/SKILL.md": _skill_md(
				"foo", "Foo", "x"
			),
		}
	)
	_install_mock_transport(monkeypatch, handler)
	assert await repo_has_skills("token", "acme/skills") is True


async def test_repo_has_skills_false_when_no_skill_md(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/": _dir_listing(["docs"]),
			"https://api.github.com/repos/acme/skills/contents/docs/SKILL.md": (404, ""),
		}
	)
	_install_mock_transport(monkeypatch, handler)
	assert await repo_has_skills("token", "acme/skills") is False


async def test_repo_has_skills_false_when_listing_404(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/empty/repo/contents/": (404, ""),
		}
	)
	_install_mock_transport(monkeypatch, handler)
	assert await repo_has_skills("token", "empty/repo") is False


# ------------------------------------------------------------ helpers


def _install_mock_transport(
	monkeypatch: pytest.MonkeyPatch,
	handler: Callable[[httpx.Request], httpx.Response],
) -> None:
	real = httpx.AsyncClient

	def fake(*args: Any, **kwargs: Any) -> httpx.AsyncClient:
		kwargs["transport"] = httpx.MockTransport(handler)
		return real(*args, **kwargs)

	monkeypatch.setattr(httpx, "AsyncClient", fake)


def _handler(
	responses: dict[str, Any],
) -> Callable[[httpx.Request], httpx.Response]:
	def _inner(request: httpx.Request) -> httpx.Response:
		key = str(request.url).split("?", 1)[0]
		body = responses.get(key)
		if body is None:
			return httpx.Response(404, text=f"unmocked: {key}")
		if isinstance(body, tuple):
			status, text = body
			return httpx.Response(status, text=text)
		return httpx.Response(200, json=body)

	return _inner


def _dir_listing(names: list[str]) -> list[dict[str, Any]]:
	return [{"name": n, "type": "dir"} for n in names]


def _file_blob(text: str) -> dict[str, Any]:
	return {
		"encoding": "base64",
		"content": base64.b64encode(text.encode("utf-8")).decode("ascii"),
	}


def _skill_md(slug: str, name: str, description: str) -> dict[str, Any]:
	body = f"---\nname: {name}\ndescription: {description}\n---\nbody for {slug}\n"
	return _file_blob(body)

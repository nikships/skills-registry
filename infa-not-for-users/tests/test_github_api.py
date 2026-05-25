"""Tests for the read-only GitHub REST wrapper used by the tools."""

from __future__ import annotations

import base64
from collections.abc import Callable
from typing import Any

import httpx
import pytest

from skills_mcp.github_api import (
	SkillSummary,
	_score_skill,
	get_skill_md,
	list_skill_folders,
	repo_has_skills,
	search_skills,
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


async def test_list_skill_folders_caps_concurrent_fanout(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	"""A registry with N folders fires ≤ _FANOUT_CONCURRENCY in flight at once.

	Without the semaphore, ``asyncio.gather`` would launch all N folder
	probes simultaneously. We assert the peak in-flight count stays at or
	below the documented cap by gating each mock response on a counter +
	a small ``asyncio.sleep`` so any unbounded fan-out becomes visible
	through the counter.
	"""
	import asyncio

	# Pick a folder count far larger than the cap so unbounded gather
	# would clearly trip the assertion.
	folder_count = 50
	folder_names = [f"skill_{i:03d}" for i in range(folder_count)]

	in_flight = 0
	peak = 0
	lock = asyncio.Lock()

	async def gated_response(slug: str) -> httpx.Response:
		nonlocal in_flight, peak
		async with lock:
			in_flight += 1
			peak = max(peak, in_flight)
		# Yield once so other coroutines get a chance to start and we can
		# observe the true peak concurrency.
		await asyncio.sleep(0)
		async with lock:
			in_flight -= 1
		return httpx.Response(
			200,
			json={
				"encoding": "base64",
				"content": base64.b64encode(f"---\nname: {slug}\n---\nbody".encode()).decode(
					"ascii"
				),
			},
		)

	# httpx.MockTransport only supports a sync handler returning a
	# coroutine; build it manually so each request awaits the gated
	# response.
	async def async_handler(request: httpx.Request) -> httpx.Response:
		url = str(request.url).split("?", 1)[0]
		if url == "https://api.github.com/repos/big/registry/contents/":
			return httpx.Response(200, json=[{"name": n, "type": "dir"} for n in folder_names])
		slug = url.rsplit("/", 2)[-2]
		return await gated_response(slug)

	transport = httpx.MockTransport(async_handler)
	real = httpx.AsyncClient

	def fake(*args: Any, **kwargs: Any) -> httpx.AsyncClient:
		kwargs["transport"] = transport
		return real(*args, **kwargs)

	monkeypatch.setattr(httpx, "AsyncClient", fake)

	summaries = await list_skill_folders("token", "big/registry")
	assert len(summaries) == folder_count
	# 8 is the documented cap (see _FANOUT_CONCURRENCY in github_api).
	assert peak <= 8, f"fan-out cap exceeded: peak={peak}"


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


def test_score_skill() -> None:
	s = SkillSummary(
		slug="git_tools",
		name="Git Helper Tools",
		description="Provides advanced git commit and status helpers.",
	)
	assert _score_skill("", s) == 0
	# Exact match name: 1000 * 2 (exact name) + 100 * 2 * 3 (tokens: "git", "helper", "tools" are all in the name) + description token overlaps etc.
	# Let's check >= 2000
	assert _score_skill("Git Helper Tools", s) >= 2000
	# Exact match slug
	assert _score_skill("git_tools", s) >= 1000
	# Exact match description
	assert _score_skill("Provides advanced git commit and status helpers.", s) >= 1000
	# Prefix match (exact is tried first, then prefix)
	assert _score_skill("git_t", s) >= 500
	# No match keys
	assert _score_skill("completely_unrelated", s) == 0


async def test_search_skills(monkeypatch: pytest.MonkeyPatch) -> None:
	handler = _handler(
		{
			"https://api.github.com/repos/acme/skills/contents/": _dir_listing(
				["git_tools", "python_lint", "js_format"]
			),
			"https://api.github.com/repos/acme/skills/contents/git_tools/SKILL.md": _skill_md(
				"git_tools", "Git Tools", "Run git status and commits"
			),
			"https://api.github.com/repos/acme/skills/contents/python_lint/SKILL.md": _skill_md(
				"python_lint", "Python Linting", "Run ruff on your codebase"
			),
			"https://api.github.com/repos/acme/skills/contents/js_format/SKILL.md": _skill_md(
				"js_format", "JS Formatter", "Run prettier beautifully"
			),
		}
	)
	_install_mock_transport(monkeypatch, handler)

	# Empty query returns all sorted alphabetically (git_tools, js_format, python_lint)
	all_skills = await search_skills("token", "acme/skills", "")
	assert len(all_skills) == 3
	assert all_skills[0].slug == "git_tools"
	assert all_skills[1].slug == "js_format"
	assert all_skills[2].slug == "python_lint"

	# Fuzzy search specific
	git_search = await search_skills("token", "acme/skills", "Git")
	assert len(git_search) == 1
	assert git_search[0].slug == "git_tools"

	# Fuzzy search matching multiple
	run_search = await search_skills("token", "acme/skills", "Run")
	assert len(run_search) == 3
	# Enforce deterministic sort behavior for search_skills results.
	assert [s.slug for s in run_search] == ["git_tools", "js_format", "python_lint"]

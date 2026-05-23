"""Tests for ``skills_mcp.cache``."""

from __future__ import annotations

from pathlib import Path

import pytest

from skills_mcp import cache


def test_reserve_creates_empty_folder(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
	folder = cache.reserve("foo")
	assert folder.is_dir()
	assert not any(folder.iterdir())


def test_reserve_wipes_existing_folder(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
	first = cache.reserve("foo")
	(first / "old.txt").write_text("stale")
	second = cache.reserve("foo")
	assert second.is_dir()
	assert not (second / "old.txt").exists()


def test_lookup_returns_none_without_meta(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
	cache.reserve("foo")
	# No commit() called yet, so meta is missing.
	assert cache.lookup("foo") is None


def test_lookup_returns_cached_entry(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
	folder = cache.reserve("foo")
	(folder / "SKILL.md").write_text("hi")
	cache.commit("foo", "abc123")
	hit = cache.lookup("foo")
	assert hit is not None
	assert hit.slug == "foo"
	assert hit.tree_sha == "abc123"
	assert hit.path == folder


def test_lookup_handles_corrupt_meta(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
	folder = cache.reserve("foo")
	(folder / "SKILL.md").write_text("hi")
	(cache.cache_root() / "foo.meta.json").write_text("garbage")
	assert cache.lookup("foo") is None

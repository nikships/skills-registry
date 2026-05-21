"""Tests for ``skills_mcp.gather``."""

from __future__ import annotations

import argparse
import os
from pathlib import Path
from typing import Any

import pytest

from skills_mcp.gather import (
	KNOWN_DOT_DIRS,
	SKILL_SUBDIRS,
	Plan,
	Source,
	_content_hash,
	build_plan,
	cmd_gather,
	delete_sources,
	execute_plan,
	find_source_dirs,
)

# ---------------------------------------------------------------------------
# find_source_dirs


def test_find_source_dirs_empty_when_no_dot_folders(tmp_path: Path) -> None:
	# tmp_path is a fresh dir with nothing in it; no dot folders exist.
	assert find_source_dirs(home=tmp_path, cwd=tmp_path) == []


def test_find_source_dirs_picks_up_known_dot_folders(tmp_path: Path) -> None:
	home = tmp_path / "home"
	(home / ".claude" / "skills").mkdir(parents=True)
	(home / ".factory" / "skills").mkdir(parents=True)
	sources = find_source_dirs(home=home, cwd=home)
	labels = {s.label for s in sources}
	assert labels == {"~/.claude/skills", "~/.factory/skills"}


def test_find_source_dirs_separates_home_and_cwd(tmp_path: Path) -> None:
	home = tmp_path / "home"
	cwd = tmp_path / "repo"
	(home / ".claude" / "skills").mkdir(parents=True)
	(cwd / ".claude" / "skills").mkdir(parents=True)
	sources = find_source_dirs(home=home, cwd=cwd)
	labels = {s.label for s in sources}
	assert labels == {"~/.claude/skills", "./.claude/skills"}


def test_find_source_dirs_does_not_double_count_when_home_equals_cwd(
	tmp_path: Path,
) -> None:
	home = tmp_path / "home"
	(home / ".claude" / "skills").mkdir(parents=True)
	sources = find_source_dirs(home=home, cwd=home)
	assert len(sources) == 1
	assert sources[0].label == "~/.claude/skills"


def test_find_source_dirs_appends_extra(tmp_path: Path) -> None:
	home = tmp_path / "home"
	home.mkdir()
	extra = tmp_path / "team-skills"
	extra.mkdir()
	sources = find_source_dirs([extra], home=home, cwd=home)
	assert len(sources) == 1
	assert sources[0].path == extra.resolve()


def test_find_source_dirs_dedupes_extra_against_known(tmp_path: Path) -> None:
	home = tmp_path / "home"
	known = home / ".claude" / "skills"
	known.mkdir(parents=True)
	sources = find_source_dirs([known], home=home, cwd=home)
	# Known wins; extra is deduped out by resolved path.
	assert len(sources) == 1
	assert sources[0].label == "~/.claude/skills"


def test_find_source_dirs_ignores_nonexistent_extra(
	tmp_path: Path,
	caplog: pytest.LogCaptureFixture,
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	missing = tmp_path / "nope"
	with caplog.at_level("WARNING", logger="skills_mcp.gather"):
		sources = find_source_dirs([missing], home=home, cwd=home)
	assert sources == []
	assert any("not a directory" in r.message for r in caplog.records)


def test_known_dot_dirs_covers_user_request() -> None:
	# The user explicitly named these. Don't regress.
	for name in (".agent", ".claude", ".factory", ".junie"):
		assert name in KNOWN_DOT_DIRS


def test_skill_subdirs_includes_skills() -> None:
	assert "skills" in SKILL_SUBDIRS


# ---------------------------------------------------------------------------
# _content_hash


def test_content_hash_is_deterministic(tmp_path: Path) -> None:
	a = tmp_path / "a.md"
	a.write_text("hello", encoding="utf-8")
	assert _content_hash(a) == _content_hash(a)


def test_content_hash_differs_on_different_content(tmp_path: Path) -> None:
	a = tmp_path / "a.md"
	b = tmp_path / "b.md"
	a.write_text("hello", encoding="utf-8")
	b.write_text("hello!", encoding="utf-8")
	assert _content_hash(a) != _content_hash(b)


# ---------------------------------------------------------------------------
# build_plan


def _src(path: Path, label: str) -> Source:
	return Source(path=path.resolve(), label=label)


def test_build_plan_single_skill(tmp_path: Path, make_skill: Any) -> None:
	src_dir = tmp_path / "src"
	src_dir.mkdir()
	make_skill(src_dir, "alpha", body="body")
	dest = tmp_path / "dest"
	plan = build_plan([_src(src_dir, "~/.claude/skills")], "SKILL.md", dest)
	assert len(plan.entries) == 1
	assert plan.entries[0].slug == "alpha"
	assert plan.entries[0].dst_folder == dest.resolve() / "alpha"
	assert plan.conflicts == []


def test_build_plan_identical_content_is_not_a_conflict(tmp_path: Path, make_skill: Any) -> None:
	a = tmp_path / "a"
	b = tmp_path / "b"
	a.mkdir()
	b.mkdir()
	make_skill(a, "dupe", body="same body", frontmatter={"name": "dupe"})
	make_skill(b, "dupe", body="same body", frontmatter={"name": "dupe"})
	plan = build_plan(
		[_src(a, "~/.claude/skills"), _src(b, "~/.factory/skills")], "SKILL.md", tmp_path / "dest"
	)
	assert len(plan.entries) == 1
	assert plan.conflicts == []
	assert "identical copy also in" in plan.entries[0].note


def test_build_plan_different_content_skip_strategy(tmp_path: Path, make_skill: Any) -> None:
	a = tmp_path / "a"
	b = tmp_path / "b"
	a.mkdir()
	b.mkdir()
	make_skill(a, "dup", body="first version", frontmatter={"name": "dup"})
	make_skill(b, "dup", body="second version", frontmatter={"name": "dup"})
	plan = build_plan(
		[_src(a, "~/.claude/skills"), _src(b, "~/.factory/skills")],
		"SKILL.md",
		tmp_path / "dest",
		on_conflict="skip",
	)
	assert len(plan.entries) == 1
	assert plan.entries[0].src_label == "~/.claude/skills"
	assert len(plan.conflicts) == 1
	assert plan.conflicts[0].resolution == "skip"
	assert "kept first" in plan.entries[0].note


def test_build_plan_different_content_newest_strategy(tmp_path: Path, make_skill: Any) -> None:
	a = tmp_path / "a"
	b = tmp_path / "b"
	a.mkdir()
	b.mkdir()
	older = make_skill(a, "dup", body="older", frontmatter={"name": "dup"})
	newer = make_skill(b, "dup", body="newer", frontmatter={"name": "dup"})
	# Force mtimes so the test is deterministic regardless of filesystem timing.
	os.utime(older, (1, 1))
	os.utime(newer, (2, 2))
	plan = build_plan(
		[_src(a, "~/.claude/skills"), _src(b, "~/.factory/skills")],
		"SKILL.md",
		tmp_path / "dest",
		on_conflict="newest",
	)
	assert len(plan.entries) == 1
	assert plan.entries[0].src_label == "~/.factory/skills"


def test_build_plan_different_content_rename_strategy(tmp_path: Path, make_skill: Any) -> None:
	a = tmp_path / "a"
	b = tmp_path / "b"
	a.mkdir()
	b.mkdir()
	make_skill(a, "dup", body="one", frontmatter={"name": "dup"})
	make_skill(b, "dup", body="two", frontmatter={"name": "dup"})
	plan = build_plan(
		[_src(a, "~/.claude/skills"), _src(b, "~/.factory/skills")],
		"SKILL.md",
		tmp_path / "dest",
		on_conflict="rename",
	)
	slugs = sorted(e.slug for e in plan.entries)
	assert slugs == ["dup", "dup-2"]


def test_build_plan_unknown_strategy_raises(tmp_path: Path, make_skill: Any) -> None:
	a = tmp_path / "a"
	b = tmp_path / "b"
	a.mkdir()
	b.mkdir()
	make_skill(a, "dup", body="one", frontmatter={"name": "dup"})
	make_skill(b, "dup", body="two", frontmatter={"name": "dup"})
	with pytest.raises(ValueError):
		build_plan(
			[_src(a, "x"), _src(b, "y")],
			"SKILL.md",
			tmp_path / "dest",
			on_conflict="explode",
		)


# ---------------------------------------------------------------------------
# execute_plan


def _make_simple_plan(tmp_path: Path, make_skill: Any) -> Plan:
	src_dir = tmp_path / "src"
	src_dir.mkdir()
	make_skill(src_dir, "alpha", body="body")
	dest = tmp_path / "dest"
	return build_plan([_src(src_dir, "test")], "SKILL.md", dest)


def test_execute_plan_copies_folder(tmp_path: Path, make_skill: Any) -> None:
	plan = _make_simple_plan(tmp_path, make_skill)
	written = execute_plan(plan, log_fn=lambda *_: None)
	assert written == 1
	out = tmp_path / "dest" / "alpha" / "SKILL.md"
	assert out.is_file()
	assert "body" in out.read_text(encoding="utf-8")


def test_execute_plan_symlinks_when_requested(tmp_path: Path, make_skill: Any) -> None:
	plan = _make_simple_plan(tmp_path, make_skill)
	written = execute_plan(plan, symlink=True, log_fn=lambda *_: None)
	assert written == 1
	link = tmp_path / "dest" / "alpha"
	assert link.is_symlink()


def test_execute_plan_skips_existing_without_force(tmp_path: Path, make_skill: Any) -> None:
	plan = _make_simple_plan(tmp_path, make_skill)
	(tmp_path / "dest").mkdir()
	(tmp_path / "dest" / "alpha").mkdir()
	(tmp_path / "dest" / "alpha" / "MARKER").write_text("existing", encoding="utf-8")
	written = execute_plan(plan, log_fn=lambda *_: None)
	assert written == 0
	assert (tmp_path / "dest" / "alpha" / "MARKER").exists()


def test_execute_plan_overwrites_existing_with_force(tmp_path: Path, make_skill: Any) -> None:
	plan = _make_simple_plan(tmp_path, make_skill)
	(tmp_path / "dest").mkdir()
	(tmp_path / "dest" / "alpha").mkdir()
	(tmp_path / "dest" / "alpha" / "MARKER").write_text("existing", encoding="utf-8")
	written = execute_plan(plan, force=True, log_fn=lambda *_: None)
	assert written == 1
	assert not (tmp_path / "dest" / "alpha" / "MARKER").exists()
	assert (tmp_path / "dest" / "alpha" / "SKILL.md").is_file()


# ---------------------------------------------------------------------------
# delete_sources


def test_delete_sources_removes_each_source_folder(tmp_path: Path, make_skill: Any) -> None:
	plan = _make_simple_plan(tmp_path, make_skill)
	execute_plan(plan, log_fn=lambda *_: None)
	assert (tmp_path / "src" / "alpha").exists()
	removed = delete_sources(plan, log_fn=lambda *_: None)
	assert removed == 1
	assert not (tmp_path / "src" / "alpha").exists()


def test_delete_sources_handles_already_gone(tmp_path: Path, make_skill: Any) -> None:
	plan = _make_simple_plan(tmp_path, make_skill)
	# Source already removed before delete_sources is called.
	import shutil

	shutil.rmtree(tmp_path / "src" / "alpha")
	removed = delete_sources(plan, log_fn=lambda *_: None)
	assert removed == 0


# ---------------------------------------------------------------------------
# cmd_gather (the orchestrator)


def _ns(**overrides: Any) -> argparse.Namespace:
	defaults: dict[str, Any] = {
		"dest": None,
		"source": [],
		"main_file": "SKILL.md",
		"on_conflict": "skip",
		"symlink": False,
		"force": False,
		"dry_run": False,
		"yes": True,  # bypass prompts in tests
		"delete_sources": False,
		"keep_sources": True,
	}
	defaults.update(overrides)
	return argparse.Namespace(**defaults)


def test_cmd_gather_creates_dest_when_no_sources(
	tmp_path: Path,
	monkeypatch: pytest.MonkeyPatch,
	capsys: pytest.CaptureFixture[str],
) -> None:
	# Point HOME and cwd at empty dirs so find_source_dirs returns nothing.
	monkeypatch.setenv("HOME", str(tmp_path / "home"))
	monkeypatch.setenv("USERPROFILE", str(tmp_path / "home"))
	(tmp_path / "home").mkdir()
	monkeypatch.chdir(tmp_path / "home")
	dest = tmp_path / "home" / "my-skills"
	rc = cmd_gather(_ns())
	assert rc == 0
	assert dest.is_dir()
	out = capsys.readouterr().out
	assert "No source skill folders found" in out
	assert "Created" in out


def test_cmd_gather_creates_dest_when_sources_are_empty(
	tmp_path: Path,
	monkeypatch: pytest.MonkeyPatch,
	capsys: pytest.CaptureFixture[str],
) -> None:
	# Source directories exist but contain no skills.
	home = tmp_path / "home"
	home.mkdir()
	(home / ".claude" / "skills").mkdir(parents=True)
	monkeypatch.setenv("HOME", str(home))
	monkeypatch.setenv("USERPROFILE", str(home))
	monkeypatch.chdir(home)
	dest = home / "my-skills"
	rc = cmd_gather(_ns())
	assert rc == 0
	assert dest.is_dir()
	out = capsys.readouterr().out
	assert "No skills found in any source" in out
	assert "Created" in out


def test_cmd_gather_dry_run_writes_nothing(
	tmp_path: Path,
	monkeypatch: pytest.MonkeyPatch,
	make_skill: Any,
	capsys: pytest.CaptureFixture[str],
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	skills_dir = home / ".claude" / "skills"
	skills_dir.mkdir(parents=True)
	make_skill(skills_dir, "alpha", body="x")
	dest = tmp_path / "dest"
	monkeypatch.setenv("HOME", str(home))
	monkeypatch.setenv("USERPROFILE", str(home))
	monkeypatch.chdir(home)
	rc = cmd_gather(_ns(dest=str(dest), dry_run=True))
	assert rc == 0
	assert not dest.exists()
	out = capsys.readouterr().out
	assert "dry run" in out


def test_cmd_gather_copies_and_keeps_sources(
	tmp_path: Path,
	monkeypatch: pytest.MonkeyPatch,
	make_skill: Any,
) -> None:
	home = tmp_path / "home"
	skills_dir = home / ".claude" / "skills"
	skills_dir.mkdir(parents=True)
	make_skill(skills_dir, "alpha", body="x")
	dest = tmp_path / "dest"
	monkeypatch.setenv("HOME", str(home))
	monkeypatch.setenv("USERPROFILE", str(home))
	monkeypatch.chdir(home)
	rc = cmd_gather(_ns(dest=str(dest), keep_sources=True))
	assert rc == 0
	assert (dest / "alpha" / "SKILL.md").is_file()
	assert (skills_dir / "alpha" / "SKILL.md").is_file()


def test_cmd_gather_deletes_sources_when_requested(
	tmp_path: Path,
	monkeypatch: pytest.MonkeyPatch,
	make_skill: Any,
) -> None:
	home = tmp_path / "home"
	skills_dir = home / ".claude" / "skills"
	skills_dir.mkdir(parents=True)
	make_skill(skills_dir, "alpha", body="x")
	dest = tmp_path / "dest"
	monkeypatch.setenv("HOME", str(home))
	monkeypatch.setenv("USERPROFILE", str(home))
	monkeypatch.chdir(home)
	rc = cmd_gather(_ns(dest=str(dest), delete_sources=True, keep_sources=False))
	assert rc == 0
	assert (dest / "alpha" / "SKILL.md").is_file()
	assert not (skills_dir / "alpha").exists()


def test_cmd_gather_rejects_conflicting_delete_flags(
	tmp_path: Path,
	capsys: pytest.CaptureFixture[str],
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.setenv("HOME", str(tmp_path))
	monkeypatch.setenv("USERPROFILE", str(tmp_path))
	monkeypatch.chdir(tmp_path)
	rc = cmd_gather(_ns(delete_sources=True, keep_sources=True))
	assert rc == 2
	assert "mutually exclusive" in capsys.readouterr().err


def test_cmd_gather_rejects_dest_inside_source(
	tmp_path: Path,
	monkeypatch: pytest.MonkeyPatch,
	make_skill: Any,
	capsys: pytest.CaptureFixture[str],
) -> None:
	home = tmp_path / "home"
	skills_dir = home / ".claude" / "skills"
	skills_dir.mkdir(parents=True)
	make_skill(skills_dir, "alpha", body="x")
	# Dest is a child of the source.
	dest = skills_dir / "consolidated"
	monkeypatch.setenv("HOME", str(home))
	monkeypatch.setenv("USERPROFILE", str(home))
	monkeypatch.chdir(home)
	rc = cmd_gather(_ns(dest=str(dest)))
	assert rc == 2
	assert "inside source" in capsys.readouterr().err

"""Tests for ``skills_mcp.add``."""

from __future__ import annotations

import argparse
from pathlib import Path
from typing import Any

import pytest

from skills_mcp.add import (
	_is_git_url,
	_is_local_path,
	_resolve_git_url,
	cmd_add,
	register_subparser,
)

# ---------------------------------------------------------------------------
# URL / source parsing helpers


def test_github_shorthand() -> None:
	assert (
		_resolve_git_url("vercel-labs/agent-skills")
		== "https://github.com/vercel-labs/agent-skills.git"
	)


def test_full_github_url() -> None:
	assert (
		_resolve_git_url("https://github.com/vercel-labs/agent-skills")
		== "https://github.com/vercel-labs/agent-skills.git"
	)


def test_github_tree_url() -> None:
	assert (
		_resolve_git_url("https://github.com/vercel-labs/agent-skills/tree/main/skills/web-design")
		== "https://github.com/vercel-labs/agent-skills.git"
	)


def test_gitlab_url_passthrough() -> None:
	assert _resolve_git_url("https://gitlab.com/org/repo") == "https://gitlab.com/org/repo"


def test_git_ssh_url_passthrough() -> None:
	assert (
		_resolve_git_url("git@github.com:vercel-labs/agent-skills.git")
		== "git@github.com:vercel-labs/agent-skills.git"
	)


def test_is_git_url() -> None:
	assert _is_git_url("https://github.com/foo/bar")
	assert _is_git_url("git@github.com:foo/bar.git")
	assert _is_git_url("http://example.com/repo")
	assert not _is_local_path("https://github.com/foo/bar")


def test_is_local_path() -> None:
	assert _is_local_path("./foo")
	assert _is_local_path("../foo")
	assert _is_local_path("~/foo")
	assert _is_local_path("/absolute/path")


# ---------------------------------------------------------------------------
# register_subparser


def test_register_subparser_returns_parser(subparsers: Any) -> None:
	sp = register_subparser(subparsers)
	assert sp.prog.endswith(" add")


# ---------------------------------------------------------------------------
# cmd_add with local paths (no network / git needed)


def _ns(**overrides: Any) -> argparse.Namespace:
	defaults: dict[str, Any] = {
		"source": None,
		"dest": None,
		"main_file": "SKILL.md",
		"skill": [],
		"list": False,
		"force": False,
		"yes": True,  # bypass prompts in tests
	}
	defaults.update(overrides)
	return argparse.Namespace(**defaults)


def test_cmd_add_list_local_skills(
	tmp_path: Path,
	make_skill: Any,
	monkeypatch: pytest.MonkeyPatch,
	capsys: pytest.CaptureFixture[str],
) -> None:
	make_skill(tmp_path, "alpha", body="alpha body", frontmatter={"name": "Alpha"})
	make_skill(tmp_path, "bravo", body="bravo body", frontmatter={"name": "Bravo"})
	monkeypatch.setenv("HOME", str(tmp_path / "home"))
	rc = cmd_add(_ns(source=str(tmp_path), list=True))
	assert rc == 0
	out = capsys.readouterr().out
	assert "alpha" in out
	assert "bravo" in out


def test_cmd_add_installs_skills_to_default_dest(
	tmp_path: Path,
	make_skill: Any,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	make_skill(tmp_path, "alpha", body="alpha body", frontmatter={"name": "Alpha"})
	monkeypatch.setenv("HOME", str(home))
	rc = cmd_add(_ns(source=str(tmp_path)))
	assert rc == 0
	assert (home / "my-skills" / "alpha" / "SKILL.md").is_file()


def test_cmd_add_installs_skills_to_custom_dest(
	tmp_path: Path,
	make_skill: Any,
) -> None:
	dest = tmp_path / "custom-dest"
	make_skill(tmp_path, "alpha", body="alpha body", frontmatter={"name": "Alpha"})
	rc = cmd_add(_ns(source=str(tmp_path), dest=str(dest)))
	assert rc == 0
	assert (dest / "alpha" / "SKILL.md").is_file()


def test_cmd_add_skips_existing_without_force(
	tmp_path: Path,
	make_skill: Any,
	monkeypatch: pytest.MonkeyPatch,
	capsys: pytest.CaptureFixture[str],
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	dest = home / "my-skills"
	dest.mkdir(parents=True)
	(dest / "alpha").mkdir()
	(dest / "alpha" / "SKILL.md").write_text("existing", encoding="utf-8")
	make_skill(tmp_path, "alpha", body="new body", frontmatter={"name": "Alpha"})
	monkeypatch.setenv("HOME", str(home))
	rc = cmd_add(_ns(source=str(tmp_path)))
	assert rc == 0
	# Existing file should still be there
	assert (dest / "alpha" / "SKILL.md").read_text(encoding="utf-8") == "existing"
	out = capsys.readouterr().out
	assert "Skipped 1 existing skill" in out or "skip alpha" in out


def test_cmd_add_overwrites_with_force(
	tmp_path: Path,
	make_skill: Any,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	dest = home / "my-skills"
	dest.mkdir(parents=True)
	(dest / "alpha").mkdir()
	(dest / "alpha" / "SKILL.md").write_text("existing", encoding="utf-8")
	make_skill(tmp_path, "alpha", body="new body", frontmatter={"name": "Alpha"})
	monkeypatch.setenv("HOME", str(home))
	rc = cmd_add(_ns(source=str(tmp_path), force=True))
	assert rc == 0
	assert "new body" in (dest / "alpha" / "SKILL.md").read_text(encoding="utf-8")


def test_cmd_add_filters_by_skill_name(
	tmp_path: Path,
	make_skill: Any,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	make_skill(tmp_path, "alpha", body="alpha body", frontmatter={"name": "Alpha"})
	make_skill(tmp_path, "bravo", body="bravo body", frontmatter={"name": "Bravo"})
	monkeypatch.setenv("HOME", str(home))
	rc = cmd_add(_ns(source=str(tmp_path), skill=["alpha"]))
	assert rc == 0
	assert (home / "my-skills" / "alpha" / "SKILL.md").is_file()
	assert not (home / "my-skills" / "bravo").exists()


def test_cmd_add_filters_by_slug(
	tmp_path: Path,
	make_skill: Any,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	home = tmp_path / "home"
	home.mkdir()
	make_skill(tmp_path, "hello-world", body="hw body", frontmatter={"name": "Hello World"})
	make_skill(tmp_path, "bravo", body="bravo body", frontmatter={"name": "Bravo"})
	monkeypatch.setenv("HOME", str(home))
	rc = cmd_add(_ns(source=str(tmp_path), skill=["hello_world"]))
	assert rc == 0
	assert (home / "my-skills" / "hello_world" / "SKILL.md").is_file()
	assert not (home / "my-skills" / "bravo").exists()


def test_cmd_add_no_source_returns_error(
	capsys: pytest.CaptureFixture[str],
) -> None:
	rc = cmd_add(_ns(source=None))
	assert rc == 2
	assert "no source provided" in capsys.readouterr().err


def test_cmd_add_local_path_not_found_returns_error(
	capsys: pytest.CaptureFixture[str],
) -> None:
	rc = cmd_add(_ns(source="/nonexistent/path/here"))
	assert rc == 1
	assert "does not exist" in capsys.readouterr().err


def test_cmd_add_empty_source_returns_error(
	tmp_path: Path,
	capsys: pytest.CaptureFixture[str],
) -> None:
	rc = cmd_add(_ns(source=str(tmp_path)))
	assert rc == 1
	assert "No skills found" in capsys.readouterr().err


def test_cmd_add_skill_filter_no_match_returns_error(
	tmp_path: Path,
	make_skill: Any,
	capsys: pytest.CaptureFixture[str],
) -> None:
	make_skill(tmp_path, "alpha", body="alpha body", frontmatter={"name": "Alpha"})
	rc = cmd_add(_ns(source=str(tmp_path), skill=["nonexistent"]))
	assert rc == 1
	assert "None of the requested skills" in capsys.readouterr().err


@pytest.fixture
def subparsers() -> Any:
	parser = argparse.ArgumentParser()
	return parser.add_subparsers(dest="command")

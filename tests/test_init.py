"""Tests for ``skills_mcp.init`` (the thin bootstrap shim)."""

from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Any

import pytest

from skills_mcp import init


@pytest.fixture
def stub_gh(monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setattr(init, "ensure_authed", lambda: Path("/fake/gh"))


def _make_args(**overrides: Any) -> Any:
	import argparse

	# ``skip_install=True`` by default so tests don't trigger the MCP
	# entry-point auto-install machinery unless they opt in explicitly.
	defaults = {
		"skip_download": True,
		"skip_install": True,
		"repo": None,
		"visibility": None,
		"no_agents": False,
	}
	defaults.update(overrides)
	return argparse.Namespace(**defaults)


def test_cmd_init_aborts_when_gh_missing(
	monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
	def raise_missing() -> Path:
		raise init.GhNotFoundError("install gh first")

	monkeypatch.setattr(init, "ensure_authed", raise_missing)
	rc = init.cmd_init(_make_args())
	assert rc == 3
	assert "install gh first" in capsys.readouterr().err


def test_cmd_init_aborts_when_unauthed(
	monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
	def raise_unauthed() -> Path:
		raise init.GhNotAuthedError("run gh auth login")

	monkeypatch.setattr(init, "ensure_authed", raise_unauthed)
	rc = init.cmd_init(_make_args())
	assert rc == 4
	assert "gh auth login" in capsys.readouterr().err


def test_cmd_init_skip_download_requires_existing_binary(
	monkeypatch: pytest.MonkeyPatch,
	tmp_path: Path,
	stub_gh: None,
	capsys: pytest.CaptureFixture[str],
) -> None:
	monkeypatch.setenv("SKILLS_BIN_DIR", str(tmp_path))
	rc = init.cmd_init(_make_args(skip_download=True))
	assert rc == 1
	assert "skip-download" in capsys.readouterr().err


def test_cmd_init_execs_into_go_binary(
	monkeypatch: pytest.MonkeyPatch,
	tmp_path: Path,
	stub_gh: None,
) -> None:
	monkeypatch.setenv("SKILLS_BIN_DIR", str(tmp_path))
	binary = tmp_path / init.BINARY_NAME
	binary.write_text("#!/bin/sh\nexit 0\n")
	binary.chmod(0o755)

	calls: list[list[str]] = []

	def fake_execv(path: str, args: list[str]) -> None:
		calls.append([path, *args])

	monkeypatch.setattr(init.os, "execv", fake_execv)
	rc = init.cmd_init(_make_args(skip_download=True))
	assert rc == 0
	assert len(calls) == 1
	assert calls[0][0] == str(binary)
	assert "bootstrap" in calls[0]


def test_cmd_init_passes_flags_to_binary(
	monkeypatch: pytest.MonkeyPatch,
	tmp_path: Path,
	stub_gh: None,
) -> None:
	monkeypatch.setenv("SKILLS_BIN_DIR", str(tmp_path))
	binary = tmp_path / init.BINARY_NAME
	binary.write_text("#!/bin/sh\nexit 0\n")
	binary.chmod(0o755)

	calls: list[list[str]] = []
	monkeypatch.setattr(init.os, "execv", lambda p, a: calls.append([p, *a]))

	init.cmd_init(
		_make_args(
			skip_download=True,
			repo="alice/skills",
			visibility="private",
			no_agents=True,
		)
	)
	assert calls
	argv = calls[0]
	assert "--repo" in argv and "alice/skills" in argv
	assert "--visibility" in argv and "private" in argv
	assert "--no-agents" in argv


def test_install_dir_respects_env(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
	monkeypatch.setenv("SKILLS_BIN_DIR", str(tmp_path / "custom"))
	assert init._install_dir() == (tmp_path / "custom").resolve()


def test_install_dir_default(monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.delenv("SKILLS_BIN_DIR", raising=False)
	assert init._install_dir() == Path.home() / ".local" / "bin"


def test_platform_pattern_raises_on_unknown(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.setattr(init.platform, "system", lambda: "Plan9")
	with pytest.raises(init.CliDownloadError, match="Unsupported platform"):
		init._platform_asset_pattern()


def test_platform_pattern_returns_known() -> None:
	# Whatever the actual host is, the tokens must be in our supported sets.
	os_token, arch_token = init._platform_asset_pattern()
	assert os_token in {"darwin", "linux", "windows"}
	assert arch_token in {"amd64", "arm64"}


# ---------------------------------------------------------------------------
# MCP entry-point auto-install (the actual user-visible fix).


def test_mcp_entry_point_present_walks_fallback_dirs(
	monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
	# A binary exists in one of the curated fallback dirs.
	fake_dir = tmp_path / "bin"
	fake_dir.mkdir()
	fake_bin = fake_dir / init.MCP_ENTRY_POINT
	fake_bin.write_text("#!/bin/sh\nexit 0\n")
	fake_bin.chmod(0o755)
	monkeypatch.setattr(init, "_MCP_FALLBACK_DIRS", (fake_dir,))
	assert init._mcp_entry_point_present()


def test_mcp_entry_point_present_returns_false_when_missing(
	monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
	# Empty fallback dir — even if `shutil.which` could find a venv-local
	# copy, we deliberately ignore it (see `_mcp_entry_point_present`).
	monkeypatch.setattr(init, "_MCP_FALLBACK_DIRS", (tmp_path / "nope",))
	assert not init._mcp_entry_point_present()


def test_mcp_entry_point_present_ignores_path(
	monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
	# Sanity check: even with a binary on PATH but missing from the
	# curated fallback dirs (e.g. inside a uvx cache), we report missing.
	# Plant a fake on PATH but not in the fallback dir.
	path_dir = tmp_path / "venvbin"
	path_dir.mkdir()
	bin_on_path = path_dir / init.MCP_ENTRY_POINT
	bin_on_path.write_text("#!/bin/sh\nexit 0\n")
	bin_on_path.chmod(0o755)
	monkeypatch.setenv("PATH", str(path_dir))
	monkeypatch.setattr(init, "_MCP_FALLBACK_DIRS", (tmp_path / "absent",))
	assert not init._mcp_entry_point_present()


def test_ensure_mcp_entry_point_short_circuits_when_present(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.delenv("SKILLS_SKIP_INSTALL", raising=False)
	monkeypatch.setattr(init, "_mcp_entry_point_present", lambda: True)
	calls: list[list[str]] = []
	monkeypatch.setattr(init.subprocess, "run", lambda *a, **kw: calls.append(list(a[0])))
	init._ensure_mcp_entry_point()
	assert calls == []


def test_ensure_mcp_entry_point_skipped_by_env(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.setenv("SKILLS_SKIP_INSTALL", "1")
	# Even if the binary is missing the install path must not run.
	monkeypatch.setattr(init, "_mcp_entry_point_present", lambda: False)
	called = False

	def fake_run(*_a: Any, **_kw: Any) -> Any:
		nonlocal called
		called = True
		raise AssertionError("install must not run when SKILLS_SKIP_INSTALL is set")

	monkeypatch.setattr(init.subprocess, "run", fake_run)
	init._ensure_mcp_entry_point()
	assert called is False


def test_ensure_mcp_entry_point_tries_installers_until_success(
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.delenv("SKILLS_SKIP_INSTALL", raising=False)
	# Presence is checked once up front (False — missing), then once after
	# every install attempt that exited cleanly (the first install returns
	# rc=1 so its presence check is short-circuited; the second returns
	# rc=0 and must see the binary).
	presence = iter([False, True])
	monkeypatch.setattr(init, "_mcp_entry_point_present", lambda: next(presence))

	# Pretend uv, pipx, and python -m pip are all available.
	monkeypatch.setattr(init.shutil, "which", lambda name: f"/fake/{name}")

	calls: list[list[str]] = []
	results = iter(
		[
			subprocess.CompletedProcess(args=[], returncode=1, stdout="", stderr="boom"),
			subprocess.CompletedProcess(args=[], returncode=0, stdout="", stderr=""),
		]
	)

	def fake_run(argv: list[str], **_kw: Any) -> subprocess.CompletedProcess:
		calls.append(list(argv))
		return next(results)

	monkeypatch.setattr(init.subprocess, "run", fake_run)
	init._ensure_mcp_entry_point()
	# First installer failed, second succeeded; third never tried.
	assert len(calls) == 2
	assert calls[0][:3] == ["uv", "tool", "install"]
	assert calls[1][:2] == ["pipx", "install"]


def test_ensure_mcp_entry_point_warns_when_all_installers_fail(
	monkeypatch: pytest.MonkeyPatch,
	capsys: pytest.CaptureFixture[str],
) -> None:
	monkeypatch.delenv("SKILLS_SKIP_INSTALL", raising=False)
	monkeypatch.setattr(init, "_mcp_entry_point_present", lambda: False)
	monkeypatch.setattr(init.shutil, "which", lambda name: f"/fake/{name}")
	monkeypatch.setattr(
		init.subprocess,
		"run",
		lambda *_a, **_kw: subprocess.CompletedProcess(
			args=[], returncode=1, stdout="", stderr="nope"
		),
	)
	init._ensure_mcp_entry_point()
	err = capsys.readouterr().err
	assert "Could not auto-install" in err
	assert init.PYPI_DIST in err


def test_cmd_init_invokes_install_when_entry_point_missing(
	monkeypatch: pytest.MonkeyPatch,
	tmp_path: Path,
	stub_gh: None,
) -> None:
	monkeypatch.setenv("SKILLS_BIN_DIR", str(tmp_path))
	binary = tmp_path / init.BINARY_NAME
	binary.write_text("#!/bin/sh\nexit 0\n")
	binary.chmod(0o755)
	monkeypatch.setattr(init.os, "execv", lambda *_a, **_kw: None)

	called = False

	def fake_ensure() -> None:
		nonlocal called
		called = True

	monkeypatch.setattr(init, "_ensure_mcp_entry_point", fake_ensure)
	init.cmd_init(_make_args(skip_download=True, skip_install=False))
	assert called is True


def test_cmd_init_skip_install_flag_blocks_install(
	monkeypatch: pytest.MonkeyPatch,
	tmp_path: Path,
	stub_gh: None,
) -> None:
	monkeypatch.setenv("SKILLS_BIN_DIR", str(tmp_path))
	binary = tmp_path / init.BINARY_NAME
	binary.write_text("#!/bin/sh\nexit 0\n")
	binary.chmod(0o755)
	monkeypatch.setattr(init.os, "execv", lambda *_a, **_kw: None)

	def fake_ensure() -> None:
		raise AssertionError("install must not run when --skip-install is passed")

	monkeypatch.setattr(init, "_ensure_mcp_entry_point", fake_ensure)
	init.cmd_init(_make_args(skip_download=True, skip_install=True))

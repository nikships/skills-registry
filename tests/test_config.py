"""Tests for ``skills_mcp.config``."""

from __future__ import annotations

from pathlib import Path

import pytest

from skills_mcp import config


def test_load_uses_env_var_when_set(monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("SKILLS_REGISTRY", "alice/skills")
	cfg = config.load()
	assert cfg.repo == "alice/skills"
	assert cfg.default_branch == "main"


def test_load_env_var_supports_branch_suffix(monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("SKILLS_REGISTRY", "alice/skills@develop")
	cfg = config.load()
	assert cfg.repo == "alice/skills"
	assert cfg.default_branch == "develop"


def test_load_rejects_malformed_env(monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.setenv("SKILLS_REGISTRY", "alice")
	with pytest.raises(config.ConfigError, match="Invalid"):
		config.load()


def test_load_reads_toml_file(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.delenv("SKILLS_REGISTRY", raising=False)
	monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path))
	(tmp_path / "skills-mcp").mkdir()
	(tmp_path / "skills-mcp" / "registry.toml").write_text(
		'[registry]\nrepo = "bob/skills"\ndefault_branch = "main"\n'
	)
	cfg = config.load()
	assert cfg.repo == "bob/skills"


def test_load_missing_config_raises(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.delenv("SKILLS_REGISTRY", raising=False)
	monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path))
	with pytest.raises(config.ConfigError, match="No registry configured"):
		config.load()


def test_save_and_load_roundtrip(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.delenv("SKILLS_REGISTRY", raising=False)
	monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path))
	written = config.save(config.RegistryConfig(repo="carol/skills", default_branch="trunk"))
	assert written.exists()
	cfg = config.load()
	assert cfg.repo == "carol/skills"
	assert cfg.default_branch == "trunk"


def test_load_toml_missing_repo_field(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
	monkeypatch.delenv("SKILLS_REGISTRY", raising=False)
	monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path))
	(tmp_path / "skills-mcp").mkdir()
	(tmp_path / "skills-mcp" / "registry.toml").write_text("[registry]\n")
	with pytest.raises(config.ConfigError, match="no \\[registry\\]\\.repo"):
		config.load()

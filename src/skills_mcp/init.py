"""``skills-registry init`` — thin bootstrap that hands off to the Go CLI.

This module is intentionally minimal. It owns three jobs:

1. Verify ``gh`` is installed and authenticated.
2. Persist the ``skill-registry-mcp`` console script on disk (``uv tool
   install`` / ``pipx install`` / ``pip install --user``). Without this
   the entry point only exists inside the ephemeral ``uvx`` cache, and
   desktop MCP clients (Claude Desktop, Cursor, Codex, …) — which launch
   the server from a stripped environment — cannot find it. Opt out
   with ``--skip-install`` or ``SKILLS_SKIP_INSTALL=1``.
3. Download the ``skill-registry`` Go binary into ``~/.local/bin`` (or
   ``SKILLS_BIN_DIR``) and ``exec`` into it.

All TUI work — repo prompts, agent multi-select, conflict resolution —
lives in the Go binary so we maintain exactly one TUI codebase.
"""

from __future__ import annotations

import argparse
import logging
import os
import platform
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from pathlib import Path

from .gh import GhNotAuthedError, GhNotFoundError, ensure_authed

log = logging.getLogger("skills_mcp.init")

# Where the Go binary lives in the published release tarballs.
DEFAULT_CLI_REPO = os.environ.get("SKILLS_CLI_REPO", "anand-92/skills-registry")
BINARY_NAME = "skill-registry"

# Name of the Python console script that desktop MCP clients launch.
MCP_ENTRY_POINT = "skill-registry-mcp"

# PyPI distribution name installed to provide :data:`MCP_ENTRY_POINT`.
PYPI_DIST = "skills-registry"

# Fallback locations probed when looking for an existing MCP entry point.
# Mirrors ``locateMCPBinary`` in ``cli/cmd/skill-registry/bootstrap.go`` so
# the absolute path the Go bootstrap emits matches what we install.
_MCP_FALLBACK_DIRS: tuple[Path, ...] = (
	Path.home() / ".local" / "bin",
	Path("/opt/homebrew/bin"),
	Path("/usr/local/bin"),
	Path("/usr/bin"),
)


def cmd_init(args: argparse.Namespace) -> int:
	"""Top-level driver for ``skills-registry init``."""
	try:
		gh = ensure_authed()
	except GhNotFoundError as exc:
		print(f"\n{exc}\n", file=sys.stderr)
		return 3
	except GhNotAuthedError as exc:
		print(f"\n{exc}\n", file=sys.stderr)
		return 4

	# Persist `skill-registry-mcp` before downloading the Go binary so the
	# snippet the Go bootstrap prints at the end points at a binary that
	# actually exists on disk. The Go binary's `locateMCPBinary()` checks
	# the same fallback dirs we install into.
	if not args.skip_install:
		_ensure_mcp_entry_point()

	dest_dir = _install_dir()
	dest_dir.mkdir(parents=True, exist_ok=True)
	binary = dest_dir / BINARY_NAME

	if args.skip_download and not binary.exists():
		print(
			f"--skip-download set but no binary at {binary}. Aborting.",
			file=sys.stderr,
		)
		return 1

	if not args.skip_download:
		try:
			_download_cli(gh=gh, dest_dir=dest_dir)
		except CliDownloadError as exc:
			print(f"\n{exc}\n", file=sys.stderr)
			return 5

	if not binary.exists() or not os.access(binary, os.X_OK):
		print(
			f"\nExpected {BINARY_NAME} at {binary} but it is missing or not "
			"executable. Try re-running `skills-registry init` or follow the manual "
			"install steps printed above.\n",
			file=sys.stderr,
		)
		return 5

	# Hand off to the Go binary. It owns the rest of the bootstrap flow.
	bootstrap_args = [str(binary), "bootstrap"]
	if args.no_agents:
		bootstrap_args.append("--no-agents")
	if args.repo:
		bootstrap_args.extend(["--repo", args.repo])
	if args.visibility:
		bootstrap_args.extend(["--visibility", args.visibility])
	log.info("Executing %s", " ".join(bootstrap_args))
	os.execv(bootstrap_args[0], bootstrap_args)
	return 0  # pragma: no cover - never reached


# ---------------------------------------------------------------------------
# Helpers


def _install_dir() -> Path:
	"""Where the Go binary lands. Honors ``SKILLS_BIN_DIR``."""
	override = os.environ.get("SKILLS_BIN_DIR")
	if override:
		return Path(override).expanduser().resolve()
	return Path.home() / ".local" / "bin"


def _mcp_entry_point_present() -> bool:
	"""True if a desktop MCP client could launch ``skill-registry-mcp``.

	Mirrors the candidate list in ``locateMCPBinary`` (Go) exactly: only
	the curated fallback dirs count. We deliberately do **not** trust
	``shutil.which`` here, because under ``uvx`` it cheerfully resolves
	to a binary inside an ephemeral cache that disappears the moment
	this process exits — exactly the case the auto-install machinery is
	supposed to fix. A positive result here guarantees the Go bootstrap
	will embed a stable absolute path in the snippet it prints.
	"""
	exe_names = (MCP_ENTRY_POINT, f"{MCP_ENTRY_POINT}.exe")
	for directory in _MCP_FALLBACK_DIRS:
		for name in exe_names:
			candidate = directory / name
			if candidate.is_file() and os.access(candidate, os.X_OK):
				return True
	return False


def _candidate_installers() -> list[tuple[str, list[str]]]:
	"""Ordered list of ``(label, argv)`` install attempts.

	`uv tool` is preferred because it is always available when the user
	just ran ``uvx skills-registry init`` (uvx ships with uv). `pipx` is
	the next-best persistent installer. Falling all the way back to
	``pip install --user`` is a last resort — on macOS the user-base
	`Scripts` dir is not always on PATH, but the Go binary's curated
	fallback list catches it anyway.
	"""
	candidates: list[tuple[str, list[str]]] = []
	if shutil.which("uv"):
		candidates.append(("uv tool install", ["uv", "tool", "install", "--force", PYPI_DIST]))
	if shutil.which("pipx"):
		candidates.append(("pipx install", ["pipx", "install", "--force", PYPI_DIST]))
	# Always have a final fallback that does not require uv or pipx.
	candidates.append(
		(
			"pip install --user",
			[sys.executable, "-m", "pip", "install", "--user", "--upgrade", PYPI_DIST],
		)
	)
	return candidates


def _ensure_mcp_entry_point() -> None:
	"""Persist the ``skill-registry-mcp`` console script on disk.

	No-op when the binary is already reachable or when the user has set
	``SKILLS_SKIP_INSTALL=1``. On failure we log a manual-install hint
	but do **not** abort init — the Go bootstrap is still useful, and
	the user may want to wire the MCP server up by hand later.
	"""
	if os.environ.get("SKILLS_SKIP_INSTALL"):
		log.debug("SKILLS_SKIP_INSTALL set; skipping MCP entry-point install.")
		return
	if _mcp_entry_point_present():
		log.debug("`%s` already on disk; skipping install.", MCP_ENTRY_POINT)
		return
	print(
		f"Installing `{MCP_ENTRY_POINT}` so desktop MCP clients can launch it…",
		file=sys.stderr,
	)
	for label, argv in _candidate_installers():
		log.debug("Trying %s: %s", label, " ".join(argv))
		try:
			result = subprocess.run(argv, capture_output=True, text=True, check=False)
		except OSError as exc:  # pragma: no cover - extremely rare
			log.debug("%s failed to launch: %s", label, exc)
			continue
		if result.returncode == 0 and _mcp_entry_point_present():
			print(f"  ✓ installed via `{label}`", file=sys.stderr)
			return
		log.debug(
			"%s exited %s: %s",
			label,
			result.returncode,
			(result.stderr or result.stdout).strip(),
		)
	print(
		"\n"
		f"! Could not auto-install `{MCP_ENTRY_POINT}`. Continuing — the Go bootstrap\n"
		"  will still run, but the MCP snippet it prints will refer to a binary\n"
		"  that does not yet exist. Install it manually with one of:\n"
		f"    uv tool install {PYPI_DIST}\n"
		f"    pipx install {PYPI_DIST}\n"
		f"    python -m pip install --user {PYPI_DIST}\n",
		file=sys.stderr,
	)


class CliDownloadError(RuntimeError):
	"""Raised when we can't fetch the Go CLI binary."""


def _platform_asset_pattern() -> tuple[str, str]:
	"""Return (os_token, arch_token) used in release asset filenames."""
	system = platform.system().lower()
	machine = platform.machine().lower()

	os_token = {"darwin": "darwin", "linux": "linux", "windows": "windows"}.get(system)
	if not os_token:
		raise CliDownloadError(
			f"Unsupported platform: {system}. Build from source or use "
			f"`go install github.com/{DEFAULT_CLI_REPO}/cli/cmd/skill-registry@latest`."
		)
	arch_token = {
		"x86_64": "amd64",
		"amd64": "amd64",
		"arm64": "arm64",
		"aarch64": "arm64",
	}.get(machine)
	if not arch_token:
		raise CliDownloadError(f"Unsupported architecture: {machine}. Build from source.")
	return os_token, arch_token


def _download_cli(*, gh: Path, dest_dir: Path) -> None:
	"""Download the right release tarball via ``gh``; fall back to ``go install``."""
	os_token, arch_token = _platform_asset_pattern()
	# Asset naming convention used by goreleaser default config.
	pattern = f"skill-registry_{os_token}_{arch_token}*"
	print(f"Downloading skill-registry ({os_token}/{arch_token})…", file=sys.stderr)
	try:
		_gh_release_download(gh, pattern, dest_dir)
		return
	except CliDownloadError as exc:
		log.warning("gh release download failed: %s", exc)

	if shutil.which("go"):
		print("Falling back to `go install`…", file=sys.stderr)
		install_target = f"github.com/{DEFAULT_CLI_REPO}/cli/cmd/skill-registry@latest"
		env = os.environ.copy()
		env.setdefault("GOBIN", str(dest_dir))
		result = subprocess.run(["go", "install", install_target], env=env, check=False)
		if result.returncode == 0 and (dest_dir / BINARY_NAME).is_file():
			return
		raise CliDownloadError(
			"Both `gh release download` and `go install` failed. "
			f"Download manually from https://github.com/{DEFAULT_CLI_REPO}/releases "
			f"and place the binary at {dest_dir / BINARY_NAME}."
		)

	raise CliDownloadError(
		"`gh release download` could not find a matching asset and `go` is not "
		f"available. Download the {os_token}/{arch_token} binary from "
		f"https://github.com/{DEFAULT_CLI_REPO}/releases and place it at "
		f"{dest_dir / BINARY_NAME}."
	)


def _gh_release_download(gh: Path, pattern: str, dest_dir: Path) -> None:
	"""Run ``gh release download`` and unpack whatever asset matches."""
	with tempfile.TemporaryDirectory(prefix="skill-registry-dl-") as tmp_str:
		tmp_dir = Path(tmp_str)
		cmd = [
			str(gh),
			"release",
			"download",
			"--repo",
			DEFAULT_CLI_REPO,
			"--pattern",
			pattern,
			"--dir",
			str(tmp_dir),
			"--clobber",
		]
		log.debug("Running: %s", " ".join(cmd))
		result = subprocess.run(cmd, capture_output=True, text=True, check=False)
		if result.returncode != 0:
			raise CliDownloadError(
				"gh release download failed: "
				+ (result.stderr.strip() or result.stdout.strip() or "<no output>")
			)
		_extract_binary(tmp_dir, dest_dir)


def _extract_binary(src_dir: Path, dest_dir: Path) -> None:
	"""Unpack the matched tarball/zip and copy the binary into ``dest_dir``."""
	candidates = list(src_dir.iterdir())
	if not candidates:
		raise CliDownloadError("No release assets matched the pattern.")
	target = dest_dir / BINARY_NAME
	for asset in candidates:
		if asset.suffix == ".gz" or asset.name.endswith(".tar.gz") or asset.name.endswith(".tgz"):
			with tarfile.open(asset, "r:gz") as tar:
				tar.extractall(src_dir, filter="data")
		elif asset.suffix == ".zip":
			with zipfile.ZipFile(asset) as zf:
				zf.extractall(src_dir)
	# Find the actual binary anywhere inside src_dir.
	for path in src_dir.rglob(BINARY_NAME):
		if path.is_file():
			target.parent.mkdir(parents=True, exist_ok=True)
			shutil.copy2(path, target)
			target.chmod(target.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
			return
	# Windows: file is skill-registry.exe.
	for path in src_dir.rglob(f"{BINARY_NAME}.exe"):
		if path.is_file():
			shutil.copy2(path, target.with_suffix(".exe"))
			return
	raise CliDownloadError(
		f"Could not locate `{BINARY_NAME}` inside the downloaded asset(s): "
		+ ", ".join(p.name for p in candidates)
	)


# ---------------------------------------------------------------------------
# Argparse glue (the actual subparser is wired in __main__.py).


def register_subparser(subparsers: argparse._SubParsersAction) -> argparse.ArgumentParser:
	sp = subparsers.add_parser(
		"init",
		help="Bootstrap a GitHub-backed skill registry.",
		description=(
			"Verify `gh` CLI is installed and authenticated, install the "
			"`skill-registry` Go CLI, then hand off to it for the interactive "
			"bootstrap flow (repo creation, multi-select agent install)."
		),
	)
	sp.add_argument(
		"--skip-download",
		action="store_true",
		help="Don't download/refresh the Go binary; use the one already in PATH.",
	)
	sp.add_argument(
		"--skip-install",
		action="store_true",
		help=(
			"Don't auto-install the `skill-registry-mcp` console script. "
			"Useful when you manage the entry point yourself "
			"(or set `SKILLS_SKIP_INSTALL=1`)."
		),
	)
	sp.add_argument(
		"--repo",
		metavar="OWNER/REPO",
		help="Skip the repo-name prompt and use this slug.",
	)
	sp.add_argument(
		"--visibility",
		choices=["public", "private"],
		help="Skip the visibility prompt.",
	)
	sp.add_argument(
		"--no-agents",
		action="store_true",
		help="Don't install the skill-registry SKILL.md into any agent folders.",
	)
	return sp

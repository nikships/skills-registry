"""``skills-registry init`` — thin bootstrap that hands off to the Go CLI.

This module is intentionally minimal. It owns three jobs:

1. Verify ``gh`` is installed and authenticated.
2. Make ``skill-registry-mcp`` available as a persistent binary so MCP
   clients can launch it without relying on the user's ``uvx`` cache.
3. Download the ``skill-registry`` Go binary and ``exec`` into it.

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


def cmd_init(args: argparse.Namespace) -> int:  # noqa: ARG001 - args reserved
	"""Top-level driver for ``skills-registry init``."""
	try:
		gh = ensure_authed()
	except GhNotFoundError as exc:
		print(f"\n{exc}\n", file=sys.stderr)
		return 3
	except GhNotAuthedError as exc:
		print(f"\n{exc}\n", file=sys.stderr)
		return 4

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

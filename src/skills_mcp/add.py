"""``skills-mcp add`` — install skills from a git repository or local path."""

from __future__ import annotations

import argparse
import logging
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from urllib.parse import urlparse

from .__main__ import Skill, discover_skills

log = logging.getLogger("skills_mcp.add")

# GitHub shorthand: owner/repo
_GITHUB_SHORTHAND_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")

DEFAULT_DEST_NAME = "my-skills"


def _is_git_url(source: str) -> bool:
	return source.startswith(("https://", "http://", "git@", "git://", "ssh://"))


def _is_local_path(source: str) -> bool:
	return source.startswith(("./", "../", "~/", "/")) or Path(source).expanduser().exists()


def _resolve_git_url(source: str) -> str:
	"""Turn a source string into a cloneable Git URL."""
	if _GITHUB_SHORTHAND_RE.match(source):
		return f"https://github.com/{source}.git"
	# GitHub tree/blob URLs -> convert to raw repo URL if needed
	if source.startswith("https://github.com/"):
		parsed = urlparse(source)
		# Strip /tree/... or /blob/... paths — we clone the whole repo
		if parsed.path.startswith("/"):
			parts = parsed.path.strip("/").split("/")
			if len(parts) >= 2:
				owner, repo = parts[0], parts[1]
				# Remove .git if present
				repo = repo.removesuffix(".git")
				return f"https://github.com/{owner}/{repo}.git"
	return source


def _clone_repo(url: str) -> Path:
	"""Clone a git repository into a temporary directory. Returns the temp path."""
	tmp_dir = tempfile.mkdtemp(prefix="skills-mcp-add-")
	cmd = ["git", "clone", "--depth", "1", "--single-branch", url, tmp_dir]
	log.debug("Running: %s", " ".join(cmd))
	try:
		subprocess.run(
			cmd,
			capture_output=True,
			text=True,
			check=True,
		)
	except subprocess.CalledProcessError as exc:
		shutil.rmtree(tmp_dir, ignore_errors=True)
		msg = f"git clone failed: {exc.stderr.strip() or exc.stdout.strip() or exc}"
		raise RuntimeError(msg) from exc
	except FileNotFoundError as exc:
		shutil.rmtree(tmp_dir, ignore_errors=True)
		raise RuntimeError("git is not installed or not on PATH") from exc
	return Path(tmp_dir)


def _resolve_source(source: str) -> Path:
	"""Resolve a source string to a local directory path."""
	if _is_local_path(source):
		p = Path(source).expanduser().resolve()
		if not p.is_dir():
			raise SystemExit(f"Local path does not exist or is not a directory: {source}")
		return p
	url = _resolve_git_url(source)
	print(f"Cloning {url} ...", file=sys.stderr)
	return _clone_repo(url)


def _is_interactive() -> bool:
	return sys.stdin.isatty() and sys.stdout.isatty()


def _ask(prompt: str, *, default: bool = False) -> bool:
	"""Yes/No prompt. Returns ``default`` when not interactive."""
	if not _is_interactive():
		return default
	suffix = " [Y/n]: " if default else " [y/N]: "
	try:
		print(prompt + suffix, end="", flush=True)
		resp = input().strip().lower()
	except (EOFError, KeyboardInterrupt):
		print()
		return False
	if not resp:
		return default
	return resp in {"y", "yes"}


def _copy_skill(skill: Skill, dest: Path, *, force: bool = False) -> bool:
	"""Copy a skill folder to the destination. Returns True if written."""
	if dest.exists() or dest.is_symlink():
		if not force:
			return False
		if dest.is_symlink() or dest.is_file():
			dest.unlink()
		else:
			shutil.rmtree(dest)
	shutil.copytree(skill.folder, dest, symlinks=False)
	return True


def cmd_add(args: argparse.Namespace) -> int:
	"""Top-level driver for ``skills-mcp add <source>``."""
	if not args.source:
		print(
			"error: no source provided. Usage: skills-mcp add <owner/repo|url|path>",
			file=sys.stderr,
		)
		return 2

	source_str = args.source
	dest = Path(args.dest or (Path.home() / DEFAULT_DEST_NAME)).expanduser().resolve()
	main_file = args.main_file

	# Resolve source to a local path
	try:
		local_source = _resolve_source(source_str)
	except (RuntimeError, SystemExit) as exc:
		print(f"error: {exc}", file=sys.stderr)
		return 1

	# Is this a temporary clone we'll need to clean up?
	temp_clone = not _is_local_path(source_str)

	try:
		# Discover skills
		skills = discover_skills([local_source], main_file)
		if not skills:
			print(f"No skills found in {source_str}.", file=sys.stderr)
			return 1

		# Filter by requested skill names/slugs
		if args.skill:
			wanted = {s.lower().replace(" ", "_").replace("-", "_") for s in args.skill}
			filtered = []
			for s in skills:
				if s.slug in wanted or s.name.lower() in wanted:
					filtered.append(s)
			if not filtered:
				print(
					f"None of the requested skills ({', '.join(args.skill)}) found in {source_str}.",
					file=sys.stderr,
				)
				return 1
			skills = filtered

		# List mode
		if args.list:
			print(f"Skills available in {source_str}:")
			for s in skills:
				print(f"  {s.slug}  –  {s.name}")
				if s.description:
					print(f"      {s.description[:120]}")
			return 0

		# Plan what we'll install
		print(f"\nDestination: {dest}")
		print(f"Found {len(skills)} skill(s) to install from {source_str}.\n")
		for s in skills:
			status = "(exists — will overwrite)" if (dest / s.slug).exists() and args.force else ""
			print(f"  [+] {s.slug:30s}  –  {s.name} {status}")

		# Prompt
		if not args.yes and not _ask("\nProceed with installation?", default=True):
			print("Aborted.")
			return 0

		dest.mkdir(parents=True, exist_ok=True)
		installed = 0
		skipped = 0
		for s in skills:
			dst = dest / s.slug
			if _copy_skill(s, dst, force=args.force):
				installed += 1
			else:
				skipped += 1
				print(f"  skip {s.slug} (already at {dst})")

		verb = "Installed" if installed == 1 else "Installed"
		print(f"\n✓ {verb} {installed} skill(s) to {dest}.")
		if skipped:
			print(f"  Skipped {skipped} existing skill(s). Use --force to overwrite.")
		return 0

	finally:
		if temp_clone:
			shutil.rmtree(local_source, ignore_errors=True)


def register_subparser(subparsers: argparse._SubParsersAction) -> argparse.ArgumentParser:
	"""Wire up the ``add`` subcommand on the given argparse subparsers object."""
	sp = subparsers.add_parser(
		"add",
		help="Install skills from a git repository or local path into your skills folder.",
		description=(
			"Clone or use a local path, discover skills inside it, and copy them into your "
			"destination skills folder (default: ~/my-skills). Supports GitHub shorthand "
			"(owner/repo), full URLs, GitLab, git URLs, and local directories."
		),
	)
	sp.add_argument(
		"source",
		metavar="SOURCE",
		help=(
			"Source to install from. Examples: owner/repo, https://github.com/owner/repo, "
			"git@host:path, or ./my-local-skills"
		),
	)
	sp.add_argument(
		"--dest",
		help="Destination directory (default: ~/my-skills).",
	)
	sp.add_argument(
		"--main-file",
		default="SKILL.md",
		help="Marker filename for a skill folder (default: SKILL.md).",
	)
	sp.add_argument(
		"--skill",
		action="append",
		default=[],
		metavar="NAME",
		help="Install only specific skill(s) by slug or name. Repeatable.",
	)
	sp.add_argument(
		"--list",
		"-l",
		action="store_true",
		help="List available skills without installing.",
	)
	sp.add_argument(
		"--force",
		"-f",
		action="store_true",
		help="Overwrite existing destination skill folders.",
	)
	sp.add_argument(
		"--yes",
		"-y",
		action="store_true",
		help="Skip the proceed prompt before installing.",
	)
	return sp

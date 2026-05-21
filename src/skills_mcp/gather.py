"""``skills-mcp gather`` — consolidate skills from common AI tool dot-folders.

Scans well-known locations (``~/.claude/skills``, ``~/.factory/skills``,
``./.cursor/skills``, ...), copies every skill it finds into a single
destination directory, dedupes by slug (content-aware), and — at the user's
request — removes the originals so they no longer auto-load into your AI
tools and consume context at startup.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import logging
import os
import shutil
import sys
from collections.abc import Callable, Sequence
from dataclasses import dataclass, field
from pathlib import Path

log = logging.getLogger("skills_mcp.gather")


# Dot folders we scan for skills under $HOME and the current working dir.
# Order matters: earlier entries "win" duplicate-slug ties when ``on_conflict=skip``.
# Sourced from https://github.com/vercel-labs/skills (supported-agents table)
# plus a handful of additional tool-specific folders.
KNOWN_DOT_DIRS: tuple[str, ...] = (
	# Most popular / historically present first (wins on skip conflicts)
	".claude",
	".claude-code",
	".factory",
	".codex",
	".cursor",
	".junie",
	".aider",
	".continue",
	".windsurf",
	".codeium",
	".zed",
	".agent",
	".agents",
	".anthropic",
	".openai",
	".cline",
	".roo",
	".roocode",
	".gemini",
	".antigravity",
	# Remaining agents from vercel-labs/skills
	".aider-desk",
	".augment",
	".bob",
	".codeartsdoer",
	".codebuddy",
	".codemaker",
	".codestudio",
	".commandcode",
	".copilot",
	".cortex",
	".crush",
	".deepagents",
	".devin",
	".firebender",
	".forge",
	".goose",
	".hermes",
	".iflow",
	".kilocode",
	".kiro",
	".kode",
	".mcpjam",
	".mux",
	".opencode",
	".openclaw",
	".openhands",
	".pi",
	".qoder",
	".qwen",
	".rovodev",
	".tabnine",
	".trae",
	".trae-cn",
	".vibe",
	".zencoder",
	".neovate",
	".pochi",
	".adal",
	".snowflake",
)

# Subdirectories within those dot-folders that may hold skills.
SKILL_SUBDIRS: tuple[str, ...] = ("skills",)

DEFAULT_DEST_NAME = "my-skills"


@dataclass(frozen=True)
class Source:
	"""A directory that contains one or more skill folders."""

	path: Path  # resolved absolute path
	label: str  # human-readable label (e.g. ``~/.claude/skills``)


@dataclass
class PlanEntry:
	"""A single skill that will be written to the destination."""

	slug: str
	src_folder: Path
	src_label: str
	dst_folder: Path
	note: str = ""


@dataclass
class Conflict:
	"""Multiple skills resolved to the same slug with non-identical content."""

	slug: str
	entries: list[tuple[Path, str]] = field(default_factory=list)
	resolution: str = "skip"


@dataclass
class Plan:
	"""The full plan for a gather invocation."""

	sources: list[Source]
	dest: Path
	entries: list[PlanEntry]
	conflicts: list[Conflict]


# ---------------------------------------------------------------------------
# Discovery


def find_source_dirs(
	extra: Sequence[Path] = (),
	*,
	home: Path | None = None,
	cwd: Path | None = None,
) -> list[Source]:
	"""Discover known skill source directories.

	Looks under ``$HOME`` and the current working directory for each combination
	of :data:`KNOWN_DOT_DIRS` and :data:`SKILL_SUBDIRS`. Any extra paths the
	caller passes are appended (deduplicated by resolved path).
	"""
	home = (home or Path.home()).resolve()
	cwd = (cwd or Path.cwd()).resolve()

	bases: list[tuple[Path, str]] = [(home, "~")]
	if cwd != home:
		bases.append((cwd, "."))

	out: list[Source] = []
	seen: set[Path] = set()

	for base, prefix in bases:
		for dot in KNOWN_DOT_DIRS:
			for sub in SKILL_SUBDIRS:
				p = base / dot / sub
				if not p.is_dir():
					continue
				rp = p.resolve()
				if rp in seen:
					continue
				seen.add(rp)
				out.append(Source(path=rp, label=f"{prefix}/{dot}/{sub}"))

	for p in extra:
		rp = Path(p).expanduser().resolve()
		if not rp.is_dir():
			log.warning("Skipping --source path (not a directory): %s", p)
			continue
		if rp in seen:
			continue
		seen.add(rp)
		out.append(Source(path=rp, label=str(p)))

	return out


def _content_hash(path: Path) -> str:
	"""SHA-256 of a file's bytes. Used for content-aware dedupe."""
	h = hashlib.sha256()
	h.update(path.read_bytes())
	return h.hexdigest()


def _mtime(path: Path) -> float:
	try:
		return path.stat().st_mtime
	except OSError:
		return 0.0


# ---------------------------------------------------------------------------
# Planning


def build_plan(
	sources: Sequence[Source],
	main_file: str,
	dest: Path,
	*,
	on_conflict: str = "skip",
) -> Plan:
	"""Compute what a gather run would do without touching the filesystem."""
	# Imported lazily to avoid a circular import with the main module.
	from .__main__ import Skill, discover_skills

	dest = Path(dest).expanduser().resolve()

	raw: list[tuple[Skill, Source]] = []
	for src in sources:
		for skill in discover_skills([src.path], main_file):
			raw.append((skill, src))

	by_slug: dict[str, list[tuple[Skill, Source]]] = {}
	for skill, src in raw:
		by_slug.setdefault(skill.slug, []).append((skill, src))

	entries: list[PlanEntry] = []
	conflicts: list[Conflict] = []
	used: set[str] = set()

	for slug, group in sorted(by_slug.items()):
		if len(group) == 1:
			skill, src = group[0]
			entries.append(
				PlanEntry(
					slug=slug,
					src_folder=skill.folder,
					src_label=src.label,
					dst_folder=dest / slug,
				)
			)
			used.add(slug)
			continue

		# Multiple skills resolved to the same slug. Are they content-identical?
		hashes = {_content_hash(s.main_file) for s, _ in group}
		if len(hashes) == 1:
			skill, src = group[0]
			extras = ", ".join(s.label for _, s in group[1:])
			entries.append(
				PlanEntry(
					slug=slug,
					src_folder=skill.folder,
					src_label=src.label,
					dst_folder=dest / slug,
					note=f"identical copy also in {extras}",
				)
			)
			used.add(slug)
			continue

		pairs = [(s.folder, src.label) for s, src in group]

		if on_conflict == "skip":
			skill, src = group[0]
			entries.append(
				PlanEntry(
					slug=slug,
					src_folder=skill.folder,
					src_label=src.label,
					dst_folder=dest / slug,
					note=f"kept first; skipped {len(group) - 1} other version(s)",
				)
			)
			conflicts.append(Conflict(slug=slug, entries=pairs, resolution="skip"))
			used.add(slug)
		elif on_conflict == "newest":
			best = max(group, key=lambda sg: _mtime(sg[0].main_file))
			skill, src = best
			entries.append(
				PlanEntry(
					slug=slug,
					src_folder=skill.folder,
					src_label=src.label,
					dst_folder=dest / slug,
					note=f"newest of {len(group)} by mtime",
				)
			)
			conflicts.append(Conflict(slug=slug, entries=pairs, resolution="newest"))
			used.add(slug)
		elif on_conflict == "rename":
			for i, (skill, src) in enumerate(group):
				candidate = slug if i == 0 else f"{slug}-{i + 1}"
				j = i
				while candidate in used:
					j += 1
					candidate = f"{slug}-{j + 1}"
				used.add(candidate)
				entries.append(
					PlanEntry(
						slug=candidate,
						src_folder=skill.folder,
						src_label=src.label,
						dst_folder=dest / candidate,
						note="renamed to avoid conflict" if candidate != slug else "",
					)
				)
			conflicts.append(Conflict(slug=slug, entries=pairs, resolution="rename"))
		else:
			raise ValueError(f"unknown conflict strategy: {on_conflict!r}")

	# Stable, slug-sorted output.
	entries.sort(key=lambda e: e.slug)
	return Plan(sources=list(sources), dest=dest, entries=entries, conflicts=conflicts)


# ---------------------------------------------------------------------------
# Execution


def execute_plan(
	plan: Plan,
	*,
	symlink: bool = False,
	force: bool = False,
	log_fn: Callable[[str], None] = print,
) -> int:
	"""Copy / symlink each planned skill into ``plan.dest``.

	Returns the number of skill folders actually written (existing destinations
	without ``force`` are skipped, not counted).
	"""
	plan.dest.mkdir(parents=True, exist_ok=True)
	written = 0
	for entry in plan.entries:
		dst = entry.dst_folder
		if dst.exists() or dst.is_symlink():
			if not force:
				log_fn(f"  skip {entry.slug} (already at {dst})")
				continue
			if dst.is_symlink() or dst.is_file():
				dst.unlink()
			else:
				shutil.rmtree(dst)
		if symlink:
			dst.symlink_to(entry.src_folder, target_is_directory=True)
		else:
			shutil.copytree(entry.src_folder, dst, symlinks=False)
		written += 1
	return written


def delete_sources(
	plan: Plan,
	*,
	log_fn: Callable[[str], None] = print,
) -> int:
	"""Remove every source skill folder named in ``plan``. Returns count removed."""
	removed = 0
	for entry in plan.entries:
		src = entry.src_folder
		if not src.exists():
			continue
		try:
			shutil.rmtree(src)
		except OSError as exc:
			log_fn(f"  failed to remove {src}: {exc}")
			continue
		removed += 1
	return removed


# ---------------------------------------------------------------------------
# CLI glue


def _is_interactive() -> bool:
	return sys.stdin.isatty() and sys.stdout.isatty()


def _ask(prompt: str, *, default: bool = False, stream=None) -> bool:
	"""Yes/No prompt. Returns ``default`` when stdin/stdout aren't a TTY."""
	stream = stream or sys.stdout
	if not _is_interactive():
		return default
	suffix = " [Y/n]: " if default else " [y/N]: "
	try:
		print(prompt + suffix, end="", file=stream, flush=True)
		resp = input().strip().lower()
	except (EOFError, KeyboardInterrupt):
		print(file=stream)
		return False
	if not resp:
		return default
	return resp in {"y", "yes"}


def print_plan(plan: Plan, *, out=None) -> None:
	out = out or sys.stdout
	if plan.sources:
		print("Sources scanned:", file=out)
		for s in plan.sources:
			print(f"  {s.label:30s}  ({s.path})", file=out)

	dest_state = " (exists)" if plan.dest.exists() else " (will create)"
	print(f"\nDestination: {plan.dest}{dest_state}", file=out)

	skipped_dupes = sum(
		max(0, len(c.entries) - 1) for c in plan.conflicts if c.resolution != "rename"
	)
	summary = (
		f"\nFound {len(plan.entries)} skill(s) to write; {len(plan.conflicts)} slug conflict(s)"
	)
	if skipped_dupes:
		summary += f", {skipped_dupes} dupe(s) skipped"
	print(summary + ".\n", file=out)

	for entry in plan.entries:
		line = f"  [+] {entry.slug:30s}  ← {entry.src_label}/{entry.src_folder.name}"
		if entry.note:
			line += f"   ({entry.note})"
		print(line, file=out)

	if plan.conflicts:
		print("\nConflicts (different content for the same slug):", file=out)
		for c in plan.conflicts:
			print(f"  • {c.slug}  → {c.resolution}", file=out)
			for folder, label in c.entries:
				print(f"      {label}/{folder.name}", file=out)


def _claude_desktop_config_path() -> Path | None:
	"""Return the Claude Desktop MCP config path for the current platform."""
	system = sys.platform
	if system == "darwin":
		return (
			Path.home()
			/ "Library"
			/ "Application Support"
			/ "Claude"
			/ "claude_desktop_config.json"
		)
	if system == "win32":
		appdata = os.environ.get("APPDATA")
		if appdata:
			return Path(appdata) / "Claude" / "claude_desktop_config.json"
		return None
	# Linux and others
	return Path.home() / ".config" / "Claude" / "claude_desktop_config.json"


def _update_json_config(path: Path, dest_str: str) -> bool:
	"""Add or update the ``skills`` server entry in an MCP JSON config file."""
	data: dict = {}
	if path.exists():
		try:
			text = path.read_text(encoding="utf-8").strip()
			if text:
				data = json.loads(text)
		except json.JSONDecodeError:
			return False
	data.setdefault("mcpServers", {})
	data["mcpServers"]["skills"] = {
		"command": "skills-mcp",
		"env": {"SKILLS_ROOT": dest_str},
	}
	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
	return True


def _update_toml_config(path: Path, dest_str: str) -> bool:
	"""Add or update the ``[mcp_servers.skills]`` section in a TOML file."""
	lines: list[str] = []
	if path.exists():
		lines = path.read_text(encoding="utf-8").splitlines()

	# Strip existing [mcp_servers.skills] section and everything until the next
	# top-level section.
	new_lines: list[str] = []
	skip = False
	for line in lines:
		stripped = line.strip()
		if stripped == "[mcp_servers.skills]":
			skip = True
			continue
		if skip and stripped.startswith("["):
			skip = False
		if not skip:
			new_lines.append(line)

	# Append new section at the end.
	new_lines.append("")
	new_lines.append("[mcp_servers.skills]")
	new_lines.append('command = "skills-mcp"')
	new_lines.append(f'env = {{ SKILLS_ROOT = "{dest_str}" }}')
	new_lines.append("")

	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_text("\n".join(new_lines) + "\n", encoding="utf-8")
	return True


def _auto_configure_clients(dest: Path, dry_run: bool) -> dict[str, bool]:
	"""Attempt to update known MCP client configs with the destination path.

	Returns a mapping ``client_label -> updated`` where ``updated`` is True
	when the config file was found and successfully written.
	"""
	dest_str = str(dest)
	results: dict[str, bool] = {}

	candidates: list[tuple[str, Path | None, Callable[[Path, str], bool]]] = [
		("Claude Code", Path.home() / ".claude" / "mcp.json", _update_json_config),
		("Claude Desktop", _claude_desktop_config_path(), _update_json_config),
		("Cursor", Path.home() / ".cursor" / "mcp.json", _update_json_config),
		("VS Code / Copilot", Path.home() / ".copilot" / "mcp.json", _update_json_config),
		("Codex", Path.home() / ".codex" / "config.toml", _update_toml_config),
	]

	for label, path, updater in candidates:
		if path is None:
			results[label] = False
			continue
		if not path.exists() and dry_run:
			results[label] = False
			continue
		if path.exists():
			if dry_run:
				# In dry-run we report it as "would update" but don't write.
				results[label] = True
			else:
				try:
					results[label] = updater(path, dest_str)
				except OSError:
					results[label] = False
		else:
			results[label] = False

	return results


def _show_client_setup(dest: Path, auto_results: dict[str, bool], dry_run: bool) -> None:
	"""Print a summary of auto-configured clients and manual snippets for the rest."""
	dest_str = str(dest)
	sep = "─" * 56
	verb = "would update" if dry_run else "updated"

	updated = [label for label, ok in auto_results.items() if ok]
	missing = [label for label, ok in auto_results.items() if not ok]

	if updated:
		print(f"\n{sep}")
		print(f"  ✓ {verb.capitalize()} {len(updated)} MCP client config(s):")
		for label in updated:
			print(f"    • {label}")
		print(sep)

	if not missing:
		print("\n  All known MCP clients are configured. Restart them to pick up the change.")
		return

	print(f"\n{sep}")
	print("  Copy-paste the snippets below for clients that need manual setup:")
	print(sep)

	if "Claude Code" in missing:
		print("\n  Claude Code:")
		print("    claude mcp add skills -- skills-mcp")
		print(f"    (or: SKILLS_ROOT={dest_str} claude mcp add skills -- skills-mcp)")

	if any(m in {"Claude Desktop", "Cursor", "VS Code / Copilot"} for m in missing):
		print("\n  Claude Desktop / Cursor / VS Code (mcp.json):")
		config = {
			"mcpServers": {
				"skills": {
					"command": "skills-mcp",
					"env": {"SKILLS_ROOT": dest_str},
				}
			}
		}
		for line in json.dumps(config, indent=2).splitlines():
			print(f"    {line}")

	if "Codex" in missing:
		print("\n  Codex (~/.codex/config.toml):")
		print("    [mcp_servers.skills]")
		print('    command = "skills-mcp"')
		print(f'    env = {{ SKILLS_ROOT = "{dest_str}" }}')

	print("\n  If `skills-mcp` is not on your PATH, use the absolute path")
	print("  (e.g. ~/.local/bin/skills-mcp or wherever uv/pip installed it).")
	print(f"\n{sep}")


def cmd_gather(args: argparse.Namespace) -> int:
	"""Top-level driver for ``skills-mcp gather``."""
	if args.delete_sources and args.keep_sources:
		print("error: --delete-sources and --keep-sources are mutually exclusive", file=sys.stderr)
		return 2

	extra = [Path(s) for s in (args.source or [])]
	sources = find_source_dirs(extra=extra)
	dest = Path(args.dest or (Path.home() / DEFAULT_DEST_NAME)).expanduser().resolve()

	if not sources:
		print(
			"No source skill folders found. Tried these subdirectories under $HOME and the "
			"current directory:\n"
			"  "
			+ ", ".join("./" + d + "/" + s for d in KNOWN_DOT_DIRS for s in SKILL_SUBDIRS)
			+ f"\n\nCreated {dest} for your skills."
		)
		dest.mkdir(parents=True, exist_ok=True)
		auto_results = _auto_configure_clients(dest, dry_run=False)
		_show_client_setup(dest, auto_results, dry_run=False)
		return 0

	# Refuse to write into a destination that lives inside one of the sources —
	# we'd recurse on the next run and possibly clobber the originals.
	for s in sources:
		try:
			dest.relative_to(s.path)
		except ValueError:
			continue
		print(
			f"error: destination {dest} lives inside source {s.path}. "
			"Pick a --dest outside every source.",
			file=sys.stderr,
		)
		return 2

	plan = build_plan(
		sources=sources,
		main_file=args.main_file,
		dest=dest,
		on_conflict=args.on_conflict,
	)

	if not plan.entries:
		print(f"No skills found in any source. Created {dest} for your skills.")
		dest.mkdir(parents=True, exist_ok=True)
		auto_results = _auto_configure_clients(dest, dry_run=False)
		_show_client_setup(dest, auto_results, dry_run=False)
		return 0

	print_plan(plan)

	if args.dry_run:
		print("\n(dry run — nothing written, nothing deleted)")
		auto_results = _auto_configure_clients(dest, dry_run=True)
		_show_client_setup(dest, auto_results, dry_run=True)
		return 0

	if not args.yes and not _ask("\nProceed with copy?", default=True):
		print("Aborted.")
		return 0

	written = execute_plan(plan, symlink=args.symlink, force=args.force)
	verb = "Linked" if args.symlink else "Wrote"
	print(f"\n✓ {verb} {written} skill folder(s) to {dest}.")

	if args.keep_sources:
		delete = False
	elif args.delete_sources:
		delete = True
	else:
		print(
			"\nThe source folders below can be removed so they no longer auto-load into your AI "
			"tools and consume context at startup. They've already been copied to the destination."
		)
		for entry in plan.entries:
			print(f"  - {entry.src_label}/{entry.src_folder.name}")
		delete = _ask("\nDelete source folders?", default=False)

	if delete:
		n = delete_sources(plan)
		print(f"✓ Removed {n} source skill folder(s).")

	auto_results = _auto_configure_clients(dest, dry_run=False)
	_show_client_setup(dest, auto_results, dry_run=False)
	return 0


def register_subparser(subparsers: argparse._SubParsersAction) -> argparse.ArgumentParser:
	"""Wire up the ``gather`` subcommand on the given argparse subparsers object."""
	sp = subparsers.add_parser(
		"gather",
		help="Find skills in known AI dot-folders and combine them into one root.",
		description=(
			"Scan known AI-tool dot-folders (~/.claude/skills, ~/.factory/skills, "
			"./.cursor/skills, ...) and copy every skill into one directory so a single "
			"MCP server can serve them. Identical copies are deduped silently; same-slug "
			"folders with different content trigger the --on-conflict strategy. After "
			"copying, you'll be asked whether to delete the originals."
		),
	)
	sp.add_argument(
		"--dest",
		help="Destination directory (default: ~/my-skills).",
	)
	sp.add_argument(
		"--source",
		action="append",
		default=[],
		metavar="PATH",
		help="Additional source skill directory; repeatable.",
	)
	sp.add_argument(
		"--main-file",
		default="SKILL.md",
		help="Marker filename for a skill folder (default: SKILL.md).",
	)
	sp.add_argument(
		"--on-conflict",
		choices=["skip", "newest", "rename"],
		default="skip",
		help="When two skills resolve to the same slug with different content. "
		"skip=keep first, newest=keep latest mtime, rename=suffix later ones with -2/-3.",
	)
	sp.add_argument(
		"--symlink",
		action="store_true",
		help="Symlink each skill folder into the destination instead of copying.",
	)
	sp.add_argument(
		"--force",
		action="store_true",
		help="Overwrite existing destination skill folders.",
	)
	sp.add_argument(
		"--dry-run",
		action="store_true",
		help="Show the plan and exit. Write nothing, delete nothing.",
	)
	sp.add_argument(
		"--yes",
		"-y",
		action="store_true",
		help="Skip the proceed prompt before copying. Does NOT auto-delete sources.",
	)
	sp.add_argument(
		"--delete-sources",
		action="store_true",
		help="Delete originals after copying (no extra prompt).",
	)
	sp.add_argument(
		"--keep-sources",
		action="store_true",
		help="Never prompt to delete sources; always keep them.",
	)
	return sp

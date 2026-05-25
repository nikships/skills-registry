"""Shared SKILL.md frontmatter helpers.

Used by :mod:`skills_mcp.registry_api` when summarizing registry entries.
The parser is intentionally minimal (flat ``key: value`` plus block-scalar
continuations) so we avoid adding PyYAML as a runtime dependency.
"""

from __future__ import annotations

# YAML block-scalar indicators that introduce a multi-line value:
#   >    folded (newlines → spaces)
#   >-   folded, strip trailing newline
#   |    literal (preserve newlines)
#   |-   literal, strip trailing newline
# We treat ``+`` chomping the same as the default — we collapse via splitlines
# downstream, so trailing-newline behaviour is irrelevant.
_BLOCK_SCALAR_MARKERS = {">", ">-", ">+", "|", "|-", "|+"}


def _find_frontmatter_end(lines: list[str]) -> int | None:
	"""Return the index of the closing ``---`` fence, or ``None`` if missing."""
	for i in range(1, len(lines)):
		if lines[i].strip() == "---":
			return i
	return None


def _collect_block_lines(body_lines: list[str], start: int) -> tuple[list[str], int]:
	"""Gather the indented (or blank) lines that form a YAML block scalar.

	Returns the collected lines (stripped) and the index of the first line
	that didn't belong to the block (so the caller can resume from there).
	"""
	block_lines: list[str] = []
	i = start
	while i < len(body_lines):
		peek = body_lines[i]
		if peek.strip() == "":
			block_lines.append("")
			i += 1
			continue
		if not peek.startswith((" ", "\t")):
			break
		block_lines.append(peek.strip())
		i += 1
	return block_lines, i


def _fold_block(block_lines: list[str], folded: bool) -> str:
	"""Render collected block-scalar lines per YAML folding rules."""
	if not folded:
		return "\n".join(block_lines).rstrip("\n")
	# Fold: blank line → paragraph break (\n\n), otherwise join with " ".
	paragraphs: list[list[str]] = [[]]
	for ln in block_lines:
		if ln == "":
			if paragraphs[-1]:
				paragraphs.append([])
		else:
			paragraphs[-1].append(ln)
	return "\n\n".join(" ".join(p) for p in paragraphs if p)


def _collect_plain_continuation_lines(body_lines: list[str], start: int) -> tuple[list[str], int]:
	"""Gather plain-scalar continuation lines starting at ``start``.

	A plain (implicit) YAML scalar (``description: first line`` followed
	by additional indented non-blank lines) continues onto those lines and
	folds them with single spaces. The first blank line, non-indented
	line, indented comment line, or EOF terminates the scalar. Indented
	comments are left for the outer loop's ``startswith("#")`` skip to
	absorb so the parser preserves the legacy "comments are ignored"
	contract.
	"""
	cont: list[str] = []
	i = start
	while i < len(body_lines):
		peek = body_lines[i]
		stripped = peek.strip()
		if stripped == "" or stripped.startswith("#"):
			break
		if not peek.startswith((" ", "\t")):
			break
		cont.append(stripped)
		i += 1
	return cont, i


def parse_frontmatter(text: str) -> tuple[dict[str, str], str]:
	"""Extract a YAML-ish frontmatter block (``--- ... ---``) from the top of a file.

	Supports flat ``key: value`` pairs and YAML block scalars introduced by
	``>``, ``>-``, ``|``, or ``|-`` (subsequent indented lines are folded into
	a single value). Lists and nested mappings are still ignored.
	"""
	if not text.startswith("---"):
		return {}, text
	lines = text.splitlines()
	end = _find_frontmatter_end(lines)
	if end is None:
		return {}, text

	meta: dict[str, str] = {}
	body_lines = lines[1:end]
	i = 0
	while i < len(body_lines):
		raw = body_lines[i]
		stripped = raw.lstrip()
		if not stripped or stripped.startswith("#") or ":" not in raw:
			i += 1
			continue
		k, v = raw.split(":", 1)
		key = k.strip()
		value_text = v.strip()

		# YAML allows an inline comment after the block-scalar indicator
		# (e.g. ``description: > # multi-line``). Split on whitespace and
		# match the first token so a trailing comment doesn't hide the
		# marker from us.
		parts = value_text.split()
		head = parts[0] if parts else ""
		if head in _BLOCK_SCALAR_MARKERS:
			block_lines, i = _collect_block_lines(body_lines, i + 1)
			meta[key] = _fold_block(block_lines, folded=head.startswith(">"))
			continue

		# Plain (implicit) scalar. YAML lets the value continue onto
		# subsequent indented lines, which fold into the value with
		# single-space separators. We only attempt the fold when the
		# key line itself carries a non-empty value — an empty value
		# (``metadata:``) is the YAML signal for a nested mapping or
		# sequence, which this flat parser intentionally ignores.
		value = value_text.strip('"').strip("'")
		if value:
			cont, next_i = _collect_plain_continuation_lines(body_lines, i + 1)
			if cont:
				value = " ".join([value, *cont])
				i = next_i
			else:
				i += 1
		else:
			i += 1
		meta[key] = value

	body = "\n".join(lines[end + 1 :]).lstrip("\n")
	return meta, body


def first_paragraph(text: str, limit: int = 240) -> str:
	"""Return the first non-heading paragraph (≤ ``limit`` chars).

	Used as a description fallback when a SKILL.md has no ``description:``
	frontmatter key.
	"""
	for block in text.split("\n\n"):
		cleaned = " ".join(block.strip().split())
		if cleaned and not cleaned.startswith("#"):
			return cleaned[:limit]
	return text.strip()[:limit]

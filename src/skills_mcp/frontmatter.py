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


def parse_frontmatter(text: str) -> tuple[dict[str, str], str]:
	"""Extract a YAML-ish frontmatter block (``--- ... ---``) from the top of a file.

	Supports flat ``key: value`` pairs and YAML block scalars introduced by
	``>``, ``>-``, ``|``, or ``|-`` (subsequent indented lines are folded into
	a single value). Lists and nested mappings are still ignored.
	"""
	if not text.startswith("---"):
		return {}, text
	lines = text.splitlines()
	end = None
	for i in range(1, len(lines)):
		if lines[i].strip() == "---":
			end = i
			break
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
			# Collect subsequent indented (or blank) lines as the block value.
			# Indentation rules: any line indented past column 0 belongs to the
			# block, blank lines are paragraph breaks. We stop at the first
			# zero-indent non-blank line.
			folded = head.startswith(">")
			block_lines: list[str] = []
			i += 1
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
			if folded:
				# Fold: blank line → paragraph break (\n\n), otherwise join with " ".
				paragraphs: list[list[str]] = [[]]
				for ln in block_lines:
					if ln == "":
						if paragraphs[-1]:
							paragraphs.append([])
					else:
						paragraphs[-1].append(ln)
				value = "\n\n".join(" ".join(p) for p in paragraphs if p)
			else:
				value = "\n".join(block_lines).rstrip("\n")
			meta[key] = value
			continue

		meta[key] = value_text.strip('"').strip("'")
		i += 1

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

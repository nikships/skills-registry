"""Tests for ``skills_mcp.frontmatter.parse_frontmatter``."""

from __future__ import annotations

from skills_mcp.frontmatter import parse_frontmatter


def test_no_frontmatter_returns_full_text_and_empty_meta() -> None:
	text = "# Just a heading\n\nSome body text."
	meta, body = parse_frontmatter(text)
	assert meta == {}
	assert body == text


def test_simple_frontmatter_is_parsed() -> None:
	text = "---\nname: My Skill\ndescription: Does something useful\n---\nBody here.\n"
	meta, body = parse_frontmatter(text)
	assert meta == {"name": "My Skill", "description": "Does something useful"}
	# splitlines() drops the trailing newline, so body has no trailing \n either.
	assert body == "Body here."


def test_quoted_values_are_stripped() -> None:
	text = "---\nname: \"Quoted Name\"\ndescription: 'single quoted'\n---\nrest\n"
	meta, _ = parse_frontmatter(text)
	assert meta["name"] == "Quoted Name"
	assert meta["description"] == "single quoted"


def test_comments_are_ignored() -> None:
	text = "---\n# a comment\nname: keep\n  # indented comment\n---\nbody\n"
	meta, _ = parse_frontmatter(text)
	assert meta == {"name": "keep"}


def test_lines_without_colon_are_skipped() -> None:
	text = "---\nname: only-this\nthis line has no colon\n---\nbody"
	meta, _ = parse_frontmatter(text)
	assert meta == {"name": "only-this"}


def test_unterminated_frontmatter_returns_full_text() -> None:
	text = "---\nname: Whoops\nno-closing-marker here\nstill no marker\n"
	meta, body = parse_frontmatter(text)
	assert meta == {}
	assert body == text


def test_value_with_internal_colon_is_preserved() -> None:
	text = "---\nurl: https://example.com/path:thing\n---\nbody"
	meta, _ = parse_frontmatter(text)
	assert meta["url"] == "https://example.com/path:thing"


def test_body_leading_blank_lines_are_stripped() -> None:
	text = "---\nname: x\n---\n\n\nhello\n"
	_, body = parse_frontmatter(text)
	assert body == "hello"


def test_only_frontmatter_no_body() -> None:
	text = "---\nname: only\n---\n"
	meta, body = parse_frontmatter(text)
	assert meta == {"name": "only"}
	assert body == ""


def test_empty_string_input() -> None:
	meta, body = parse_frontmatter("")
	assert meta == {}
	assert body == ""


def test_folded_block_scalar_description() -> None:
	# YAML "folded" scalar: newlines become spaces, blank lines become paragraph breaks.
	# This is the common SKILL.md pattern we were silently dropping before.
	text = (
		"---\n"
		"name: my-skill\n"
		"description: >\n"
		"  Build terminal UIs with Charmbracelet (Bubble Tea, Lip Gloss).\n"
		"  Use when: Go TUI, shell prompts/spinners.\n"
		"---\n"
		"body"
	)
	meta, _ = parse_frontmatter(text)
	assert meta["name"] == "my-skill"
	assert meta["description"] == (
		"Build terminal UIs with Charmbracelet (Bubble Tea, Lip Gloss). "
		"Use when: Go TUI, shell prompts/spinners."
	)


def test_folded_strip_block_scalar() -> None:
	# ">-" strips trailing newline; for our purposes it matches ">".
	text = "---\ndescription: >-\n  Hello world.\n  Second line.\n---\nbody"
	meta, _ = parse_frontmatter(text)
	assert meta["description"] == "Hello world. Second line."


def test_literal_block_scalar_preserves_newlines() -> None:
	text = "---\ndescription: |\n  line one\n  line two\n---\nbody"
	meta, _ = parse_frontmatter(text)
	assert meta["description"] == "line one\nline two"


def test_block_scalar_marker_alone_not_stored_as_value() -> None:
	# Regression: the old parser stored ">" verbatim as the description, so
	# the registry listing rendered "> " for every skill that used the folded
	# YAML scalar pattern.
	text = "---\ndescription: >\n  Some real description here.\n---\nbody"
	meta, _ = parse_frontmatter(text)
	assert meta["description"] != ">"
	assert "Some real description here." in meta["description"]


def test_block_scalar_with_inline_comment() -> None:
	# YAML lets you stick a comment on the indicator line itself. The
	# previous version required an exact match against the marker set so a
	# trailing comment hid the block from us.
	text = (
		"---\n"
		"description: > # short label\n"
		"  Real description here that spans\n"
		"  multiple lines.\n"
		"---\n"
		"body"
	)
	meta, _ = parse_frontmatter(text)
	assert meta["description"] == "Real description here that spans multiple lines."


def test_block_scalar_followed_by_next_key() -> None:
	text = "---\ndescription: >\n  Two-line\n  description.\nname: my-skill\n---\nbody"
	meta, _ = parse_frontmatter(text)
	assert meta["description"] == "Two-line description."
	assert meta["name"] == "my-skill"

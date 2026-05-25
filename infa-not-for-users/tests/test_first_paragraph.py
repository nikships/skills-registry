"""Tests for ``skills_mcp.frontmatter.first_paragraph``."""

from __future__ import annotations

from skills_mcp.frontmatter import first_paragraph


def test_returns_first_non_heading_paragraph() -> None:
	text = "# Title\n\nFirst real paragraph.\n\nSecond paragraph."
	assert first_paragraph(text) == "First real paragraph."


def test_skips_leading_heading_paragraph() -> None:
	text = "# Heading only\n\nActual body content here."
	assert first_paragraph(text) == "Actual body content here."


def test_collapses_internal_whitespace() -> None:
	text = "Line one\n   line two with   spaces\n\nnext"
	assert first_paragraph(text) == "Line one line two with spaces"


def test_respects_limit_parameter() -> None:
	body = "x" * 500
	assert first_paragraph(body, limit=10) == "x" * 10


def test_default_limit_is_240() -> None:
	body = "y" * 500
	assert len(first_paragraph(body)) == 240


def test_empty_text_returns_empty_string() -> None:
	assert first_paragraph("") == ""


def test_only_headings_falls_back_to_truncated_text() -> None:
	text = "# Heading one\n\n## Heading two"
	# All paragraphs are headings → falls back to raw text (stripped, truncated).
	result = first_paragraph(text, limit=50)
	assert result == text.strip()[:50]


def test_whitespace_only_input_returns_empty_string() -> None:
	assert first_paragraph("   \n\n\t\n") == ""


def test_paragraphs_separated_by_blank_lines() -> None:
	text = "alpha\n\nbeta\n\ngamma"
	assert first_paragraph(text) == "alpha"


def test_leading_blank_lines_are_skipped() -> None:
	text = "\n\n\nfirst real line\n\nnext"
	assert first_paragraph(text) == "first real line"

// Package frontmatter is the one frontmatter contract every listing surface
// shares: local scan (`sync`, `bootstrap`, `add`), registry reads (`list`,
// `search`, `get`), and — via its Swift mirror in
// `mac-app/Sources/SkillsRegistryCore/Frontmatter.swift` — the macOS app.
//
// It is deliberately a flat parser, not full YAML: it reads top-level
// `key: value` lines plus folded/literal block scalars (`>`, `>-`, `|`,
// `|-`) into a string map and ignores keys it does not know. Values keep
// their literal text — a numeric `name: 123` stays `"123"`, a flow list
// stays `"[a, b]"` — and surrounding quote characters are stripped without
// interpreting escapes. Nested mappings and sequences are not understood; duplicate keys resolve last-wins.
// Callers that need the display name/description apply their own fallbacks
// (folder name, first paragraph) on top of the returned map.
package frontmatter

import "strings"

// blockScalarMarkers are the YAML scalar indicators that introduce a
// multi-line value. We don't distinguish keep/strip/clip chomping because the
// caller folds whitespace later.
var blockScalarMarkers = map[string]bool{
	">": true, ">-": true, ">+": true,
	"|": true, "|-": true, "|+": true,
}

// Parse reads a frontmatter line block and returns the top-level scalar
// values. Supports “key: value“ and YAML folded/literal block scalars
// introduced by “>“, “>-“, “|“, “|-“. Nested mappings and sequences are
// ignored.
func Parse(body []string) map[string]string {
	out := map[string]string{}
	i := 0
	for i < len(body) {
		raw := body[i]
		stripped := strings.TrimSpace(raw)
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(raw, ":") {
			i++
			continue
		}
		k, v, _ := strings.Cut(raw, ":")
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)

		// YAML allows an inline comment after the block-scalar indicator
		// (e.g. "description: > # multi-line"). Compare against the first
		// whitespace-separated token so the comment doesn't make us miss it.
		head := val
		if fields := strings.Fields(val); len(fields) > 0 {
			head = fields[0]
		}
		if blockScalarMarkers[head] {
			folded := strings.HasPrefix(head, ">")
			block, nextI := collectBlockLines(body, i+1)
			i = nextI
			if folded {
				out[key] = foldBlockScalar(block)
			} else {
				out[key] = strings.TrimRight(strings.Join(block, "\n"), "\n")
			}
			continue
		}

		// Plain (implicit) scalar. YAML lets the value continue onto
		// subsequent indented lines, which fold into the value with
		// single-space separators. We only attempt the fold when the
		// key line itself carries a non-empty value — an empty value
		// ("metadata:") is the YAML signal for a nested mapping or
		// sequence, which this flat parser intentionally ignores.
		value := strings.Trim(val, "'\"")
		if value != "" {
			cont, nextI := collectPlainContinuationLines(body, i+1)
			if len(cont) > 0 {
				pieces := append([]string{value}, cont...)
				value = strings.Join(pieces, " ")
				i = nextI
			} else {
				i++
			}
		} else {
			i++
		}
		out[key] = value
	}
	return out
}

// collectPlainContinuationLines walks the lines after a plain-scalar key,
// returning the stripped continuation lines and the index of the first line
// that no longer belongs to the scalar. The scalar ends at a blank line, a
// non-indented line, an indented comment ("  # …"), or EOF. Indented
// comments are intentionally left to the outer loop's comment-skip so the
// "comments are ignored" contract still holds.
func collectPlainContinuationLines(body []string, start int) ([]string, int) {
	var cont []string
	i := start
	for i < len(body) {
		peek := body[i]
		stripped := strings.TrimSpace(peek)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			break
		}
		if !strings.HasPrefix(peek, " ") && !strings.HasPrefix(peek, "\t") {
			break
		}
		cont = append(cont, stripped)
		i++
	}
	return cont, i
}

// collectBlockLines gathers the indented continuation lines of a YAML block
// scalar starting at `start`. Returns the collected lines and the index of
// the first non-continuation line.
func collectBlockLines(body []string, start int) ([]string, int) {
	var block []string
	i := start
	for i < len(body) {
		peek := body[i]
		if strings.TrimSpace(peek) == "" {
			block = append(block, "")
			i++
			continue
		}
		if !strings.HasPrefix(peek, " ") && !strings.HasPrefix(peek, "\t") {
			break
		}
		block = append(block, strings.TrimSpace(peek))
		i++
	}
	return block, i
}

// foldBlockScalar joins block lines using YAML folded-scalar rules: blank
// lines separate paragraphs (joined with "\n\n"), consecutive non-blank lines
// are joined with " ".
func foldBlockScalar(block []string) string {
	var paragraphs [][]string
	var current []string
	for _, ln := range block {
		if ln == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, current)
				current = nil
			}
			continue
		}
		current = append(current, ln)
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, current)
	}
	parts := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		parts = append(parts, strings.Join(p, " "))
	}
	return strings.Join(parts, "\n\n")
}

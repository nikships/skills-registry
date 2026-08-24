// Package main — provenance stamping for untrusted imports.
//
// A skill imported from a stranger's repository is a copy: the registry commit
// message records where it came from, but the file itself does not. A commit
// message is not where an agent, a reviewer, or a later `git log`-less clone
// looks. So an untrusted import writes two extra frontmatter keys onto the
// copy before it is published:
//
//	category:   the public index's category for the source folder, when it had one
//	source_url: the GitHub folder URL the copy was fetched from
//
// Nothing else about the file changes. The body is the upstream skill verbatim,
// existing keys keep their order and their values, and a skill already
// carrying either key keeps its own. Registry skills predating this carry
// neither key and stay valid, and `publish` of a local folder never gains them:
// a folder the user wrote is not an import.
package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nikships/skills-registry/cli/internal/registry"
	"github.com/nikships/skills-registry/cli/internal/scan"
	"github.com/nikships/skills-registry/cli/internal/trust"
)

const (
	// provenanceCategoryKey and provenanceSourceKey are the two stamped keys.
	provenanceCategoryKey = "category"
	provenanceSourceKey   = "source_url"

	// provenanceDefaultRef pins a folder URL built for a source that named no
	// ref (`owner/repo`, or a repository URL). GitHub resolves `HEAD` to the
	// repository's default branch, so the URL stays truthful without inventing
	// a branch name the import never saw.
	provenanceDefaultRef = "HEAD"

	// maxCategoryLen bounds the category taken from the public index. The value
	// is third-party text written into a file every agent then loads, so a
	// hostile or broken row cannot turn one frontmatter line into a payload.
	maxCategoryLen = 64
)

// frontmatterKey is one key/value pair to merge, kept as a slice element
// rather than a map entry so the written order is deterministic.
type frontmatterKey struct {
	key   string
	value string
}

// stampProvenance writes the provenance keys onto every candidate skill's
// SKILL.md, for an untrusted source only. A trusted source is the user's own
// content and is published byte-for-byte as it always was.
//
// It runs after the import gate, never before: the gate's local scan must read
// the upstream file, not one this function has already edited.
func stampProvenance(untrusted bool, source, fetchRoot, category string, skills []scan.Skill) error {
	if !untrusted {
		return nil
	}
	for _, sk := range skills {
		keys := provenanceKeys(source, fetchRoot, category, sk.Folder)
		if len(keys) == 0 {
			continue
		}
		if err := mergeSkillFrontmatter(filepath.Join(sk.Folder, scan.MainFileName), keys); err != nil {
			return fmt.Errorf("stamp provenance on %s: %w", sk.Slug, err)
		}
	}
	return nil
}

// provenanceKeys builds the pairs to merge for one skill folder. The category
// is omitted when the index had no row (or no category for it), because an
// invented category is worse than an absent one.
func provenanceKeys(source, fetchRoot, category, folder string) []frontmatterKey {
	var out []frontmatterKey
	if c := boundedCategory(category); c != "" {
		out = append(out, frontmatterKey{key: provenanceCategoryKey, value: c})
	}
	if u := sourceURLFor(source, fetchRoot, folder); u != "" {
		out = append(out, frontmatterKey{key: provenanceSourceKey, value: u})
	}
	return out
}

// boundedCategory normalizes the index's category to a single short line. The
// index is third-party, so a multi-line or overlong value is collapsed and
// clipped rather than written verbatim into a file agents load as instructions.
func boundedCategory(category string) string {
	c := strings.Join(strings.Fields(category), " ")
	if r := []rune(c); len(r) > maxCategoryLen {
		c = strings.TrimSpace(string(r[:maxCategoryLen]))
	}
	return c
}

// sourceURLFor renders the URL one fetched skill folder came from.
//
// The URL names the folder, so it ends in the skill's own directory. For a
// GitHub source the repository, ref, and path are reassembled from the source
// URL plus the folder's position under the fetch root, which keeps a folder of
// skills honest: each skill gets its own subfolder URL rather than the parent's.
// A non-GitHub remote (GitLab, a `git@` remote) has no folder-URL form this
// code can derive, so the source string is recorded as given, minus any
// userinfo.
func sourceURLFor(source, fetchRoot, folder string) string {
	rel := relSlashPath(fetchRoot, folder)
	if target, ok := registry.ParseGitHubURL(source); ok {
		return githubFolderURL(target, rel)
	}
	if owner, repo, ok := trust.ParseOwnerRepo(source); ok {
		return githubFolderURL(registry.GitHubTarget{Owner: owner, Repo: repo}, rel)
	}
	return redactSourceUserInfo(source)
}

// githubFolderURL renders target with its path extended to the fetched skill
// folder.
func githubFolderURL(target registry.GitHubTarget, rel string) string {
	target.Path = repoFolderPath(target.Path, rel)
	if target.Ref == "" {
		// WebURL drops the path without a ref, and a clone-path source
		// (`owner/repo`, a bare repository URL) never carries one.
		target.Ref = provenanceDefaultRef
	}
	return target.WebURL()
}

// repoFolderPath composes the repository path of a fetched skill folder from
// the source URL's own path and the folder's path relative to the fetch root.
//
// A folder fetch writes the URL's last segment as the top directory under the
// fetch root, so a relative path starting with that segment already covers it;
// a clone writes the repository root instead, and its relative path is the
// repository path outright. A `/blob/` URL naming SKILL.md itself resolves to
// the file's directory, matching what the fetch did.
func repoFolderPath(urlPath, rel string) string {
	base := strings.Trim(urlPath, "/")
	if path.Base(base) == scan.MainFileName {
		if base = path.Dir(base); base == "." {
			base = ""
		}
	}
	if rel == "" {
		return base
	}
	segs := strings.Split(rel, "/")
	if base != "" && segs[0] == path.Base(base) {
		rel = strings.Join(segs[1:], "/")
	}
	return path.Join(base, rel)
}

// relSlashPath returns folder's slash-separated path relative to root, or ""
// when folder is root or lies outside it.
func relSlashPath(root, folder string) string {
	rel, err := filepath.Rel(root, folder)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}

// mergeSkillFrontmatter rewrites one SKILL.md with the keys merged in, leaving
// the file untouched when nothing changed.
func mergeSkillFrontmatter(mainPath string, keys []frontmatterKey) error {
	raw, err := os.ReadFile(mainPath)
	if err != nil {
		return err
	}
	merged, changed := mergeFrontmatter(string(raw), keys)
	if !changed {
		return nil
	}
	info, err := os.Stat(mainPath)
	if err != nil {
		return err
	}
	return os.WriteFile(mainPath, []byte(merged), info.Mode().Perm())
}

// mergeFrontmatter merges keys into text's YAML frontmatter and reports
// whether the document changed.
//
// The document is edited line by line rather than parsed and re-serialized, so
// every unrelated line — key order, comments, block scalars, quoting style —
// survives byte-for-byte. A key already present keeps its value unless that
// value is empty; a missing key is appended just before the closing `---`.
//
// A document with no frontmatter gains a block holding only these keys, which
// leaves the name and description falling back to the folder and body exactly
// as they did before. A document whose block is never closed is left alone:
// guessing where its metadata ends would risk rewriting the body.
func mergeFrontmatter(text string, keys []frontmatterKey) (string, bool) {
	if len(keys) == 0 {
		return text, false
	}
	if !strings.HasPrefix(text, "---") {
		return prependFrontmatter(text, keys), true
	}
	lines := strings.Split(text, "\n")
	end := closingFenceIndex(lines)
	if end < 0 {
		return text, false
	}
	changed := false
	for _, k := range keys {
		line := k.key + ": " + yamlScalar(k.value)
		if at, held := topLevelKeyLine(lines[1:end], k.key); held {
			if frontmatterValue(lines[1+at]) != "" {
				continue
			}
			lines[1+at] = line
			changed = true
			continue
		}
		lines = append(lines[:end], append([]string{line}, lines[end:]...)...)
		end++
		changed = true
	}
	if !changed {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// prependFrontmatter gives a document with no frontmatter one carrying just
// the provenance keys.
func prependFrontmatter(text string, keys []frontmatterKey) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		b.WriteString(k.key + ": " + yamlScalar(k.value) + "\n")
	}
	b.WriteString("---\n")
	b.WriteString(text)
	return b.String()
}

// closingFenceIndex returns the index of the frontmatter block's closing
// `---`, or -1 when the block is never closed.
func closingFenceIndex(lines []string) int {
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i
		}
	}
	return -1
}

// topLevelKeyLine finds a top-level `key:` line in a frontmatter block.
// Indented lines are continuations (a block scalar's text, a nested mapping),
// so they never match.
func topLevelKeyLine(block []string, key string) (int, bool) {
	for i, raw := range block {
		if raw == "" || strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
			continue
		}
		name, _, ok := strings.Cut(raw, ":")
		if ok && strings.TrimSpace(name) == key {
			return i, true
		}
	}
	return 0, false
}

// frontmatterValue returns a key line's value, with surrounding quotes
// stripped so `category: ""` counts as empty and gets filled.
func frontmatterValue(line string) string {
	_, v, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	return strings.TrimSpace(v)
}

// yamlScalarIndicators are the characters YAML gives special meaning at the
// start of a plain scalar.
const yamlScalarIndicators = "-?:,[]{}#&*!|>'\"%@`"

// yamlScalar renders a value as a YAML scalar, quoting only when a plain one
// would be ambiguous. A URL stays unquoted, because a colon is only special
// when whitespace follows it — which keeps the stamped line as readable as the
// rest of the frontmatter. The category comes from a third-party index, so a
// value carrying a newline, a quote, or a leading indicator is quoted rather
// than trusted to be well behaved.
func yamlScalar(v string) string {
	switch {
	case v == "":
		return `""`
	case strings.TrimSpace(v) != v,
		strings.ContainsAny(v, "\n\r\"'#"),
		strings.Contains(v, ": "),
		strings.HasSuffix(v, ":"),
		strings.ContainsRune(yamlScalarIndicators, rune(v[0])):
		return strconv.Quote(v)
	default:
		return v
	}
}

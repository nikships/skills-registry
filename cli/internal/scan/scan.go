// Package scan finds local skills inside every known AI tool dot-folder.
// Local skill discovery + the source-dir
// enumeration that used to live in gather.py.
package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// MainFileName is the marker that identifies a skill folder.
const MainFileName = "SKILL.md"

// Skill mirrors skills_mcp.Skill (Python).
type Skill struct {
	Slug        string
	Name        string
	Description string
	Folder      string // absolute path to the folder containing SKILL.md
	Source      string // human label, e.g. "~/.claude/skills"
}

// Hash returns the SHA-256 of the skill's SKILL.md file. Used for content-aware
// dedupe when the same slug shows up in multiple dot-folders.
func (s Skill) Hash() (string, error) {
	f, err := os.Open(filepath.Join(s.Folder, MainFileName))
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Source is one directory that may contain skills.
type Source struct {
	Path  string
	Label string
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify normalizes a name to a filesystem-safe identifier.
// Identical algorithm to Python's _slug.
func Slugify(name string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "skill"
	}
	return s
}

// DiscoverSources returns every known skill-bearing directory under $HOME and cwd.
func DiscoverSources(home, cwd string, extra []string, dotDirs []string) []Source {
	want := map[string]struct{}{}

	bases := []struct {
		root, prefix string
	}{
		{home, "~"},
	}
	if cwd != home {
		bases = append(bases, struct{ root, prefix string }{cwd, "."})
	}

	var sources []Source
	for _, base := range bases {
		for _, dot := range dotDirs {
			p := filepath.Join(base.root, dot, "skills")
			info, err := os.Stat(p)
			if err != nil || !info.IsDir() {
				continue
			}
			abs, _ := filepath.Abs(p)
			if _, dup := want[abs]; dup {
				continue
			}
			want[abs] = struct{}{}
			sources = append(sources, Source{
				Path:  abs,
				Label: base.prefix + "/" + dot + "/skills",
			})
		}
	}
	for _, e := range extra {
		abs, err := filepath.Abs(e)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, dup := want[abs]; dup {
			continue
		}
		want[abs] = struct{}{}
		sources = append(sources, Source{Path: abs, Label: e})
	}
	return sources
}

// Discover walks each source and returns every skill folder.
func Discover(sources []Source) ([]Skill, error) {
	var out []Skill
	seen := map[string]struct{}{}
	for _, src := range sources {
		paths, err := findMainFiles(src.Path)
		if err != nil {
			return nil, err
		}
		for _, mainPath := range paths {
			skill, err := load(src, mainPath)
			if err != nil {
				continue
			}
			if _, dup := seen[skill.Slug]; dup {
				continue
			}
			seen[skill.Slug] = struct{}{}
			out = append(out, skill)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func findMainFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable subtrees rather than aborting the whole scan.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && d.Name() == MainFileName {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func load(src Source, mainPath string) (Skill, error) {
	folder := filepath.Dir(mainPath)
	text, err := os.ReadFile(mainPath)
	if err != nil {
		return Skill{}, err
	}
	meta, body := parseFrontmatter(string(text))
	rawName := strings.TrimSpace(meta["name"])
	if rawName == "" {
		rawName = filepath.Base(folder)
	}
	desc := strings.TrimSpace(meta["description"])
	if desc == "" {
		desc = firstParagraph(body, 240)
	}
	if desc == "" {
		desc = "Skill: " + rawName
	}
	return Skill{
		Slug:        Slugify(rawName),
		Name:        rawName,
		Description: desc,
		Folder:      folder,
		Source:      src.Label,
	}, nil
}

func parseFrontmatter(text string) (map[string]string, string) {
	if !strings.HasPrefix(text, "---") {
		return map[string]string{}, text
	}
	lines := strings.Split(text, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return map[string]string{}, text
	}
	block := strings.Join(lines[1:end], "\n")
	out := map[string]string{}
	parsed := map[string]any{}
	if err := yaml.Unmarshal([]byte(block), &parsed); err == nil {
		for k, v := range parsed {
			switch s := v.(type) {
			case string:
				out[k] = strings.TrimSpace(s)
			default:
				out[k] = strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(toString(v)), "\n", " "))
			}
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimLeft(body, "\n")
	return out, body
}

func firstParagraph(text string, limit int) string {
	for _, block := range strings.Split(text, "\n\n") {
		cleaned := strings.Join(strings.Fields(strings.TrimSpace(block)), " ")
		if cleaned == "" || strings.HasPrefix(cleaned, "#") {
			continue
		}
		if len(cleaned) > limit {
			return cleaned[:limit]
		}
		return cleaned
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > limit {
		return trimmed[:limit]
	}
	return trimmed
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, toString(item))
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

// CleanupEntry is a direct child of a `<dot>/skills/` directory that is a
// candidate for removal after bootstrap pushes everything to the registry.
type CleanupEntry struct {
	Path      string // absolute path to the entry (folder or symlink)
	Source    string // human label, e.g. "~/.claude/skills"
	IsSymlink bool
}

// EntriesForCleanup sweeps every source's direct children and returns each
// entry whose name matches a known registry slug. This is the post-publish
// cleanup primitive: anything that mirrors a registry slug is dead weight that
// every coding agent re-reads each session.
//
// Rules:
//   - Skip the literal name "skills-registry" (that's our SKILL.md install
//     target, written by bootstrap.InstallSkillMd) and dotfiles (.DS_Store).
//   - Match by literal name OR Slugify(name): folder names on disk often
//     contain hyphens (e.g. "agp-9-upgrade"), but Slugify normalizes those
//     to underscores ("agp_9_upgrade") and the registry stores skills under
//     the slug. Direct name-equality alone would miss every hyphenated skill.
//   - Real directories must contain a sibling SKILL.md to be eligible; this
//     protects against accidentally deleting unrelated content that happens
//     to share a name with a slug.
//   - Symlinks are accepted regardless of their target (so we clean up
//     redirects whose targets have already been removed).
//
// Unlike Discover, this function does NOT slug-dedupe across sources — if the
// same slug exists in five dot-folders, all five entries are returned. That's
// the whole point: the previous slug-deduped cleanup left N-1 copies behind.
func EntriesForCleanup(sources []Source, registrySlugs map[string]struct{}) []CleanupEntry {
	var entries []CleanupEntry
	for _, src := range sources {
		list, err := os.ReadDir(src.Path)
		if err != nil {
			continue
		}
		for _, e := range list {
			name := e.Name()
			if name == "skills-registry" || strings.HasPrefix(name, ".") {
				continue
			}
			if _, ok := registrySlugs[name]; !ok {
				if _, ok := registrySlugs[Slugify(name)]; !ok {
					continue
				}
			}
			full := filepath.Join(src.Path, name)
			// e.Type() can omit ModeSymlink (or return zero) on filesystems
			// that report DT_UNKNOWN from getdents — e.g. XFS formatted with
			// ftype=0, some NFS configurations, and certain overlayfs mounts.
			// e.Info() falls back to an explicit lstat so we always classify
			// symlinks correctly, even in those environments. The extra
			// syscall per dirent is negligible at the scale we walk.
			info, err := e.Info()
			if err != nil {
				continue
			}
			isSymlink := info.Mode()&os.ModeSymlink != 0
			if !isSymlink {
				if !info.IsDir() {
					continue
				}
				if _, err := os.Stat(filepath.Join(full, MainFileName)); err != nil {
					continue
				}
			}
			entries = append(entries, CleanupEntry{
				Path:      full,
				Source:    src.Label,
				IsSymlink: isSymlink,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Source != entries[j].Source {
			return entries[i].Source < entries[j].Source
		}
		return entries[i].Path < entries[j].Path
	})
	return entries
}

// DedupeAgainst returns skills from `local` whose slugs are NOT present in the
// `remote` slug set. Used by `skills-registry sync` to compute the diff.
func DedupeAgainst(local []Skill, remoteSlugs map[string]struct{}) []Skill {
	out := make([]Skill, 0, len(local))
	for _, s := range local {
		if _, dup := remoteSlugs[s.Slug]; dup {
			continue
		}
		out = append(out, s)
	}
	return out
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/scan"
)

// TestPurgeLocalSkillsRemovesFolders verifies the happy path: every
// skill folder discovered under the known dot-folders is removed and
// the counters report the expected (deleted, failed) pair.
func TestPurgeLocalSkillsRemovesFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeSkills := filepath.Join(home, ".claude", "skills")
	cursorSkills := filepath.Join(home, ".cursor", "skills")
	if err := os.MkdirAll(filepath.Join(claudeSkills, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cursorSkills, "bravo"), 0o755); err != nil {
		t.Fatalf("mkdir bravo: %v", err)
	}
	// Distinct names so scan.Discover keeps both rows (it slug-dedupes).
	for path, name := range map[string]string{
		filepath.Join(claudeSkills, "alpha", "SKILL.md"): "alpha",
		filepath.Join(cursorSkills, "bravo", "SKILL.md"): "bravo",
	} {
		body := "---\nname: " + name + "\n---\nbody"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	// Switch to a deterministic cwd so DiscoverSources doesn't pull in
	// the developer's actual project tree.
	cwd := t.TempDir()
	mustChdir(t, cwd)

	skills, err := discoverLocalSkills()
	if err != nil {
		t.Fatalf("discoverLocalSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("discovered %d skills, want 2", len(skills))
	}

	deleted, failed, err := purgeLocalSkills(context.Background(), skills)
	if err != nil {
		t.Fatalf("purgeLocalSkills returned err: %v", err)
	}
	if deleted != 2 || failed != 0 {
		t.Fatalf("counters = (%d,%d), want (2,0)", deleted, failed)
	}
	for _, sk := range skills {
		if _, err := os.Stat(sk.Folder); !os.IsNotExist(err) {
			t.Errorf("folder %s still exists after purge", sk.Folder)
		}
	}
}

// TestPurgeLocalSkillsRefusesUnknownRoot guards the safety contract:
// passing in a skill whose folder is OUTSIDE any known dot-folder must
// be rejected (counted as failed, never deleted). This protects against
// a bad caller asking purgeLocalSkills to wipe an arbitrary path.
func TestPurgeLocalSkillsRefusesUnknownRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustChdir(t, t.TempDir())

	// Create a "skill" folder OUTSIDE every dot-folder.
	rogue := filepath.Join(t.TempDir(), "rogue-skill")
	if err := os.MkdirAll(rogue, 0o755); err != nil {
		t.Fatalf("mkdir rogue: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rogue, "SKILL.md"), []byte("---\nname: x\n---\nbody"), 0o644); err != nil {
		t.Fatalf("write rogue: %v", err)
	}

	skills := []scan.Skill{{Slug: "rogue", Folder: rogue, Source: "rogue"}}
	deleted, failed, err := purgeLocalSkills(context.Background(), skills)
	if err != nil {
		t.Fatalf("purgeLocalSkills returned err: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
	if _, err := os.Stat(rogue); err != nil {
		t.Errorf("rogue folder was removed even though it was outside dot-folders: %v", err)
	}
}

// TestPurgeLocalSkillsKeepsMetaSkill pins the carve-out for the
// bootstrapped `skills-registry` meta-skill: Purge must never wipe it,
// even when the folder lives under a known dot-folder allow-list root.
// Re-bootstrapping would otherwise be the only way to restore the
// agent's gateway back into the registry.
func TestPurgeLocalSkillsKeepsMetaSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustChdir(t, t.TempDir())

	claudeSkills := filepath.Join(home, ".claude", "skills")
	meta := filepath.Join(claudeSkills, "skills-registry")
	regular := filepath.Join(claudeSkills, "alpha")
	// Distinct frontmatter names so scan.Discover's slug-dedupe keeps
	// both rows (the rest of the test depends on both surviving the scan).
	fixtures := map[string]string{
		meta:    "skills-registry",
		regular: "alpha",
	}
	for dir, name := range fixtures {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		body := "---\nname: " + name + "\n---\nbody"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", dir, err)
		}
	}

	skills, err := discoverLocalSkills()
	if err != nil {
		t.Fatalf("discoverLocalSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("discovered %d skills, want 2 (meta + alpha)", len(skills))
	}

	// Discover-side filter (what the hub flow actually feeds the
	// deleter): the meta-skill row must be stripped.
	filtered := filterMetaSkill(skills)
	if len(filtered) != 1 || filepath.Base(filtered[0].Folder) != "alpha" {
		t.Fatalf("filterMetaSkill = %+v, want only alpha", filtered)
	}

	// Defense-in-depth: even if a caller passes the unfiltered slice,
	// purgeLocalSkills must skip the meta-skill (silent — neither
	// counted as deleted nor failed).
	deleted, failed, err := purgeLocalSkills(context.Background(), skills)
	if err != nil {
		t.Fatalf("purgeLocalSkills returned err: %v", err)
	}
	if deleted != 1 || failed != 0 {
		t.Fatalf("counters = (%d,%d), want (1,0) — meta-skill should be a silent skip", deleted, failed)
	}
	if _, err := os.Stat(meta); err != nil {
		t.Errorf("meta-skill folder was removed by purge: %v", err)
	}
	if _, err := os.Stat(regular); !os.IsNotExist(err) {
		t.Errorf("alpha folder should have been removed, got err=%v", err)
	}
}

// TestPurgeLocalSkillsEmptyIsNoOp confirms the zero-skill case returns
// (0, 0, nil) without touching the filesystem or even probing for
// allowed roots.
func TestPurgeLocalSkillsEmptyIsNoOp(t *testing.T) {
	deleted, failed, err := purgeLocalSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty purge returned err: %v", err)
	}
	if deleted != 0 || failed != 0 {
		t.Errorf("counters = (%d,%d), want (0,0)", deleted, failed)
	}
}

// TestPathUnderAnyRootMatchesSubdir is the targeted unit test for the
// allow-list helper: a folder directly inside a root must match; a
// folder outside every root must not; the root itself must NOT match
// (otherwise os.RemoveAll on it would wipe every sibling skill).
func TestPathUnderAnyRootMatchesSubdir(t *testing.T) {
	root := t.TempDir()
	if !pathUnderAnyRoot(filepath.Join(root, "child"), []string{root}) {
		t.Error("child of root should match")
	}
	if pathUnderAnyRoot(t.TempDir(), []string{root}) {
		t.Error("unrelated tempdir should not match")
	}
	if pathUnderAnyRoot(filepath.Join(root, "..", "sibling"), []string{root}) {
		t.Error("../sibling traversal should not match")
	}
	// Regression guard: the root itself must be refused. A skill.Folder
	// resolving to its allow-list root would otherwise let os.RemoveAll
	// wipe the entire root and every sibling skill under it.
	if pathUnderAnyRoot(root, []string{root}) {
		t.Error("root itself should not match")
	}
}

// mustChdir is a t.Helper that swaps the test goroutine's working
// directory to dir and registers a cleanup to restore the prior cwd.
// Used by tests that need a deterministic CWD for DiscoverSources.
//
// os.Getwd() may legitimately fail at test start when an earlier test
// in the same package chdir'd into a t.TempDir() and that dir was
// cleaned up at test exit (Go testing leaves the goroutine's cwd
// pointing at a deleted directory). In that case there is nothing to
// "restore" on cleanup, so we record an empty prev and skip the chdir
// back. This is the documented pattern for resilient cwd handling in
// the Go test corpus.
func mustChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		// Previous cwd is gone (likely a deleted t.TempDir from an
		// earlier test). Don't try to restore; chdir to `dir` is still
		// safe because Chdir uses the path argument directly.
		prev = ""
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if prev == "" {
			return
		}
		if err := os.Chdir(prev); err != nil {
			t.Logf("restore chdir %s: %v", prev, err)
		}
	})
	// Sanity check: ensure we're at the expected place (handles macOS
	// /private/var ↔ /var symlink weirdness for downstream callers).
	got, _ := os.Getwd()
	if !strings.HasSuffix(got, filepath.Base(dir)) {
		t.Logf("chdir landed at %q, expected suffix %q", got, filepath.Base(dir))
	}
}

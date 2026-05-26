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
// folder outside every root must not.
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
}

// mustChdir is a t.Helper that swaps the test goroutine's working
// directory to dir and registers a cleanup to restore the prior cwd.
// Used by tests that need a deterministic CWD for DiscoverSources.
func mustChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
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

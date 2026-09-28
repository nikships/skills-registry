package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/cache"
	"github.com/nikships/skills-registry/cli/internal/registry"
)

func TestResolveDest(t *testing.T) {
	t.Run("empty dest uses default parent", func(t *testing.T) {
		// Unit test for the empty-destFlag rule: result must be
		// "<defaultParent>/<canonSlug>" with no reuse signal.
		parent := t.TempDir()
		got, reused := resolveDest("agp-9-upgrade", "", parent)
		want := filepath.Join(parent, "agp_9_upgrade")
		if got != want {
			t.Fatalf("dest = %q, want %q", got, want)
		}
		if reused != "" {
			t.Fatalf("reused = %q, want empty", reused)
		}
	})

	t.Run("empty dest in production uses cache.CacheRoot()", func(t *testing.T) {
		// Regression guard for issue #29: DownloadSkill passes
		// cache.CacheRoot() as the default parent, so an empty --dest
		// must land under the global cache — never cwd/.agents/.
		t.Setenv("XDG_CACHE_HOME", "")
		home := t.TempDir()
		t.Setenv("HOME", home)
		got, _ := resolveDest("agp-9-upgrade", "", cache.CacheRoot())
		want := filepath.Join(home, ".cache", "skills-registry", "skills", "agp_9_upgrade")
		if got != want {
			t.Fatalf("dest = %q, want %q", got, want)
		}
		cwd, _ := os.Getwd()
		stray := filepath.Join(cwd, ".agents") + string(filepath.Separator)
		if strings.HasPrefix(got, stray) {
			t.Fatalf("dest %q must not live under %q", got, stray)
		}
	})

	t.Run("matching basename used as-is", func(t *testing.T) {
		tmp := t.TempDir()
		explicit := filepath.Join(tmp, "agp_9_upgrade")
		got, reused := resolveDest("agp_9_upgrade", explicit, tmp)
		if got != explicit {
			t.Fatalf("dest = %q, want %q", got, explicit)
		}
		if reused != "" {
			t.Fatalf("reused = %q, want empty", reused)
		}
	})

	t.Run("hyphenated basename slugifies to same canonical slug", func(t *testing.T) {
		tmp := t.TempDir()
		// The user typed the hyphenated form; basename slugifies to the
		// same canon, so we honor the user's literal path (no rewriting).
		explicit := filepath.Join(tmp, "agp-9-upgrade")
		got, reused := resolveDest("agp_9_upgrade", explicit, tmp)
		if got != explicit {
			t.Fatalf("dest = %q, want %q", got, explicit)
		}
		if reused != "" {
			t.Fatalf("reused = %q, want empty", reused)
		}
	})

	t.Run("dest treated as parent when basename does not match", func(t *testing.T) {
		tmp := t.TempDir()
		got, reused := resolveDest("agp_9_upgrade", tmp, tmp)
		want := filepath.Join(tmp, "agp_9_upgrade")
		if got != want {
			t.Fatalf("dest = %q, want %q", got, want)
		}
		if reused != "" {
			t.Fatalf("reused = %q, want empty", reused)
		}
	})

	t.Run("reuses existing sibling with equivalent slug", func(t *testing.T) {
		// Simulates the original bug: ~/.factory/skills/agp-9-upgrade already
		// exists; user invokes `get agp_9_upgrade --dest .../agp_9_upgrade`.
		// We should reuse the existing folder instead of creating a duplicate.
		parent := t.TempDir()
		existing := filepath.Join(parent, "agp-9-upgrade")
		if err := os.MkdirAll(existing, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		requested := filepath.Join(parent, "agp_9_upgrade")
		got, reused := resolveDest("agp_9_upgrade", requested, parent)
		if got != existing {
			t.Fatalf("dest = %q, want %q (the existing sibling)", got, existing)
		}
		if reused != existing {
			t.Fatalf("reused = %q, want %q", reused, existing)
		}
	})

	t.Run("no false-positive when sibling already matches exactly", func(t *testing.T) {
		// If the folder we'd write to already exists, that's not a collision —
		// it's the happy "re-fetch the same skill" path. resolveDest should
		// return the same path with no reuse warning.
		parent := t.TempDir()
		final := filepath.Join(parent, "agp_9_upgrade")
		if err := os.MkdirAll(final, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got, reused := resolveDest("agp_9_upgrade", final, parent)
		if got != final {
			t.Fatalf("dest = %q, want %q", got, final)
		}
		if reused != "" {
			t.Fatalf("reused = %q, want empty (same path is not a collision)", reused)
		}
	})

	t.Run("parent-form dest also reuses existing sibling", func(t *testing.T) {
		// User passes a parent directory; the resolved path would be
		// parent/<slug>, but a slug-equivalent sibling already lives there.
		parent := t.TempDir()
		existing := filepath.Join(parent, "agp-9-upgrade")
		if err := os.MkdirAll(existing, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got, reused := resolveDest("agp_9_upgrade", parent, parent)
		if got != existing {
			t.Fatalf("dest = %q, want %q", got, existing)
		}
		if reused != existing {
			t.Fatalf("reused = %q, want %q", reused, existing)
		}
	})
}

// TestDownloadSkillUnknownSlugFails verifies the cli-tui-2 fix at the
// DownloadSkill level: an unknown slug returns ErrSlugNotFound (naming
// the slug, pointing at search/list) and leaves no directory behind —
// instead of the old exit-0 fake success with an empty folder.
func TestDownloadSkillUnknownSlugFails(t *testing.T) {
	t.Setenv("SKILLS_MIRROR_DISABLE", "1")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	bin := stubGHForRemove(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "real-skill", "type": "dir", "sha": "tree-1"},
			},
		},
	})
	installGHEnv(t, bin)
	client, err := registry.New("x/y", "main")
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}

	_, _, err = DownloadSkill(context.Background(), client, "no-such-skill", "")
	if !errors.Is(err, registry.ErrSlugNotFound) {
		t.Fatalf("DownloadSkill = %v, want ErrSlugNotFound", err)
	}
	for _, want := range []string{"no_such_skill", "search", "list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(cache.CacheRoot(), "no_such_skill")); !os.IsNotExist(statErr) {
		t.Fatalf("unknown-slug get must not create a cache dir, stat = %v", statErr)
	}
}

// TestDownloadSkillKnownSlugSucceeds is the happy-path companion: a slug
// the registry actually has still downloads through DownloadSkill, so
// the new not-found check can't regress normal fetches.
func TestDownloadSkillKnownSlugSucceeds(t *testing.T) {
	t.Setenv("SKILLS_MIRROR_DISABLE", "1")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	bin := stubGHForRemove(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "real-skill", "type": "dir", "sha": "tree-1"},
			},
		},
		{
			"key": "GET repos/x/y/contents/real-skill",
			"body": []map[string]any{
				{"name": "SKILL.md", "type": "file"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/real-skill/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": "IyBSZWFs"},
		},
	})
	installGHEnv(t, bin)
	client, err := registry.New("x/y", "main")
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}

	finalDest, reused, err := DownloadSkill(context.Background(), client, "real-skill", "")
	if err != nil {
		t.Fatalf("DownloadSkill: %v", err)
	}
	if reused != "" {
		t.Fatalf("reused = %q, want empty", reused)
	}
	got, err := os.ReadFile(filepath.Join(finalDest, "SKILL.md"))
	if err != nil || string(got) != "# Real" {
		t.Fatalf("SKILL.md missing or wrong content: %q %v", got, err)
	}
}

// TestRunGetJSONUnknownSlugEmitsError pins the JSON half of the contract:
// `get <unknown> --json` prints {"error": ...} to stdout and returns a
// non-nil error (so the caller exits non-zero), with no dir created.
func TestRunGetJSONUnknownSlugEmitsError(t *testing.T) {
	t.Setenv("SKILLS_MIRROR_DISABLE", "1")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeRegistryConfig(t, "x/y")
	bin := stubGHForRemove(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "real-skill", "type": "dir", "sha": "tree-1"},
			},
		},
	})
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runGetJSON(context.Background(), "no-such-skill", ""); err == nil {
		t.Fatal("runGetJSON should return an error for an unknown slug")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &payload); err != nil {
		t.Fatalf("invalid JSON %q: %v", buf.String(), err)
	}
	msg, ok := payload["error"]
	if !ok || msg == "" {
		t.Fatalf("expected {\"error\": ...}, got %v", payload)
	}
	if !strings.Contains(msg, "no_such_skill") {
		t.Errorf("error %q should name the slug", msg)
	}
	if _, statErr := os.Stat(filepath.Join(cache.CacheRoot(), "no_such_skill")); !os.IsNotExist(statErr) {
		t.Fatalf("unknown-slug get must not create a cache dir, stat = %v", statErr)
	}
}

// TestSlugNotFoundErrorShape pins the message wording: it wraps the
// registry sentinel (errors.Is keeps working) and suggests the
// discovery commands.
func TestSlugNotFoundErrorShape(t *testing.T) {
	err := slugNotFoundError("x/y", "no_such_skill")
	if !errors.Is(err, registry.ErrSlugNotFound) {
		t.Fatalf("expected ErrSlugNotFound, got %v", err)
	}
	for _, want := range []string{`no_such_skill`, "x/y", "search", "list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

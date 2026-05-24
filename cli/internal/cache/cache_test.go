package cache

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCacheRootXDGCacheHome verifies that an explicit XDG_CACHE_HOME
// wins over the HOME fallback, matching cache.py's resolution order.
func TestCacheRootXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	got := CacheRoot()
	want := filepath.Join("/tmp/xdg-cache", "skills-mcp", "skills")
	if got != want {
		t.Errorf("CacheRoot() = %q, want %q", got, want)
	}
}

// TestCacheRootHomeFallback verifies the HOME-based fallback when
// XDG_CACHE_HOME is unset (or empty).
func TestCacheRootHomeFallback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/tmp/test-home")
	got := CacheRoot()
	want := filepath.Join("/tmp/test-home", ".cache", "skills-mcp", "skills")
	if got != want {
		t.Errorf("CacheRoot() = %q, want %q", got, want)
	}
}

// TestCacheRootContainsExpectedSegments is a defensive check that
// guarantees the path always ends in `skills-mcp/skills` so the hub's
// Settings view never surfaces a misleading location.
func TestCacheRootContainsExpectedSegments(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/tmp/somewhere")
	got := CacheRoot()
	if !strings.HasSuffix(got, filepath.Join("skills-mcp", "skills")) {
		t.Errorf("CacheRoot() = %q, want suffix %q", got,
			filepath.Join("skills-mcp", "skills"))
	}
}

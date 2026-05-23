package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocateMCPBinaryFindsLocalBin covers the most common case: `uv tool
// install` and `pipx install` both symlink the entry point into
// ~/.local/bin.
func TestLocateMCPBinaryFindsLocalBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "skill-registry-mcp")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := locateMCPBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Fatalf("locateMCPBinary() = %q, want %q", got, target)
	}
}

// TestLocateMCPBinaryFindsUvToolDataDir covers the fallback when the
// ~/.local/bin symlink is missing but the file still exists at uv's
// canonical install location.
func TestLocateMCPBinaryFindsUvToolDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "share", "uv", "tools", "skills-registry", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "skill-registry-mcp")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := locateMCPBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Fatalf("locateMCPBinary() = %q, want %q", got, target)
	}
}

// TestLocateMCPBinaryFallsBackToBareName documents the missing-binary
// behavior: callers still get a usable string (so MCPJSONSnippet can
// render *something*) and an error explaining the situation. Skipped
// when the host happens to have the binary at one of the system-wide
// fallback paths (which we can't sandbox with HOME alone).
func TestLocateMCPBinaryFallsBackToBareName(t *testing.T) {
	for _, sys := range []string{
		"/opt/homebrew/bin/skill-registry-mcp",
		"/usr/local/bin/skill-registry-mcp",
	} {
		if _, err := os.Stat(sys); err == nil {
			t.Skipf("system path %s exists; can't isolate locateMCPBinary", sys)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := locateMCPBinary()
	if err == nil {
		t.Fatal("expected error when no binary is present")
	}
	if !strings.HasSuffix(got, "skill-registry-mcp") {
		t.Fatalf("expected fallback name, got %q", got)
	}
}

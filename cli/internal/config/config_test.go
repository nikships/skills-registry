package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvVar(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY", "alice/skills")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Repo != "alice/skills" || cfg.DefaultBranch != "main" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadEnvVarWithBranch(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY", "alice/skills@develop")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultBranch != "develop" {
		t.Fatalf("expected develop, got %q", cfg.DefaultBranch)
	}
}

func TestLoadEnvVarInvalid(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY", "alice")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for malformed env value")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SKILLS_REGISTRY", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "skills-registry"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[registry]\nrepo = \"bob/skills\"\ndefault_branch = \"trunk\"\n"
	if err := os.WriteFile(filepath.Join(dir, "skills-registry", "registry.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Repo != "bob/skills" || cfg.DefaultBranch != "trunk" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SKILLS_REGISTRY", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	_, err := Load()
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("expected ErrMissing, got %v", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SKILLS_REGISTRY", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	path, err := Save(Config{Repo: "carol/skills", DefaultBranch: "trunk"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Repo != "carol/skills" || cfg.DefaultBranch != "trunk" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

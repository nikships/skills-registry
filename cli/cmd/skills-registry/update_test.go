package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoUpdateEnabled(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY_AUTO_UPDATE", "yes")
	if !autoUpdateEnabled() {
		t.Fatal("expected auto update to be enabled")
	}
	t.Setenv("SKILLS_REGISTRY_AUTO_UPDATE", "")
	if autoUpdateEnabled() {
		t.Fatal("expected auto update to be disabled by default")
	}
}

func TestRunAutoUpdateUsesRunnerAndSwallowsErrors(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY_AUTO_UPDATE", "1")
	orig := updateRunner
	t.Cleanup(func() { updateRunner = orig })
	called := false
	updateRunner = func(context.Context, updateOpts) (updateResult, error) {
		called = true
		return updateResult{}, errors.New("boom")
	}
	var stderr bytes.Buffer
	runAutoUpdate(context.Background(), &stderr)
	if !called {
		t.Fatal("expected auto update runner to be called")
	}
	if !strings.Contains(stderr.String(), "warning: auto-update failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUpdateAssetNameSupported(t *testing.T) {
	got, err := updateAssetName("darwin", "arm64")
	if err != nil {
		t.Fatalf("updateAssetName returned err: %v", err)
	}
	if got != "skills-registry_darwin_arm64.tar.gz" {
		t.Fatalf("asset = %q", got)
	}
}

func TestUpdateAssetNameRejectsUnsupported(t *testing.T) {
	if _, err := updateAssetName("windows", "amd64"); err == nil {
		t.Fatal("expected unsupported OS error")
	}
	if _, err := updateAssetName("linux", "386"); err == nil {
		t.Fatal("expected unsupported arch error")
	}
}

func TestUpdateTargetPathResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-skills-registry")
	link := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	got, err := updateTargetPath(link)
	if err != nil {
		t.Fatalf("updateTargetPath: %v", err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if got != want {
		t.Fatalf("target path = %q, want %q", got, want)
	}
}

func TestVersionMatches(t *testing.T) {
	if !versionMatches("0.5.1", "v0.5.1") {
		t.Fatal("expected version to match tag with v prefix")
	}
	if versionMatches("dev", "v0.5.1") {
		t.Fatal("dev should never be treated as up to date")
	}
}

func TestPerformUpdateDryRunPinnedVersion(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "skills-registry")
	res, err := performUpdate(context.Background(), updateOpts{
		version: "v9.9.9",
		binPath: bin,
		dryRun:  true,
	})
	if err != nil {
		t.Fatalf("performUpdate dry run: %v", err)
	}
	if res.Updated {
		t.Fatal("dry run should not report Updated=true")
	}
	if res.Version != "v9.9.9" || res.Path != bin {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
	if !strings.Contains(res.Message, "would install") {
		t.Fatalf("dry-run message = %q", res.Message)
	}
}

func TestPerformUpdateFromLocalTarball(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatalf("write old bin: %v", err)
	}
	tarball := filepath.Join(dir, "update.tar.gz")
	writeUpdateTarball(t, tarball, "new")

	res, err := performUpdate(context.Background(), updateOpts{
		version: "v9.9.9",
		binPath: bin,
		tarball: tarball,
		force:   true,
	})
	if err != nil {
		t.Fatalf("performUpdate local tarball: %v", err)
	}
	if !res.Updated {
		t.Fatal("expected Updated=true")
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read updated bin: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("updated binary body = %q", body)
	}
}

func writeUpdateTarball(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tarball: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "skills-registry",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close tarball: %v", err)
	}
}

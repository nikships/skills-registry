package main

// Tests for the top-level install.sh shell script.
//
// install.sh is the curl|sh entry point for users who don't already
// have the Go binary on disk. The tests here drive it via /bin/sh with
// environment overrides so we can:
//
//   - Verify OS/arch detection and URL construction for every
//     supported (darwin|linux) × (amd64|arm64) combination.
//   - Verify it exits with a clear error (code 2) on unsupported
//     platforms instead of silently downloading something wrong.
//   - Verify the end-to-end install path drops the binary at
//     $SKILLS_BIN_DIR/skills-registry with executable permission.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// scriptPath returns the absolute path to install.sh at the repo root.
// We resolve it relative to this test file so the test works no matter
// where `go test` is invoked from.
func scriptPath(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// cli/cmd/skills-registry/install_script_test.go → repo root
	p := filepath.Join(filepath.Dir(here), "..", "..", "..", "install.sh")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("install.sh not found at %s: %v", abs, err)
	}
	return abs
}

// runScript runs install.sh under /bin/sh with the supplied env
// overrides. Returns stdout, stderr, and exit code.
func runScript(t *testing.T, env map[string]string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", scriptPath(t))
	cmd.Env = append(os.Environ(), envSlice(env)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running install.sh: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// TestInstallScriptURLConstruction covers INSTALL-001..004: every
// supported OS/arch combination resolves to the expected GitHub
// Releases URL.
func TestInstallScriptURLConstruction(t *testing.T) {
	cases := []struct {
		name   string
		os     string
		arch   string
		expect string
	}{
		{
			name:   "darwin/arm64",
			os:     "Darwin",
			arch:   "arm64",
			expect: "https://github.com/anand-92/skills-registry/releases/latest/download/skills-registry_darwin_arm64.tar.gz",
		},
		{
			name:   "darwin/amd64",
			os:     "Darwin",
			arch:   "x86_64",
			expect: "https://github.com/anand-92/skills-registry/releases/latest/download/skills-registry_darwin_amd64.tar.gz",
		},
		{
			name:   "linux/amd64",
			os:     "Linux",
			arch:   "amd64",
			expect: "https://github.com/anand-92/skills-registry/releases/latest/download/skills-registry_linux_amd64.tar.gz",
		},
		{
			name:   "linux/arm64",
			os:     "Linux",
			arch:   "aarch64",
			expect: "https://github.com/anand-92/skills-registry/releases/latest/download/skills-registry_linux_arm64.tar.gz",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errStr, code := runScript(t, map[string]string{
				"SKILLS_REGISTRY_OS":      tc.os,
				"SKILLS_REGISTRY_ARCH":    tc.arch,
				"SKILLS_REGISTRY_DRY_RUN": "1",
			})
			if code != 0 {
				t.Fatalf("expected exit 0, got %d (stderr: %s)", code, errStr)
			}
			got := strings.TrimSpace(out)
			if got != tc.expect {
				t.Fatalf("URL mismatch:\n  got:  %s\n  want: %s", got, tc.expect)
			}
		})
	}
}

// TestInstallScriptPinnedVersion verifies that SKILLS_REGISTRY_VERSION
// switches from /latest/ to a tag-specific download URL.
func TestInstallScriptPinnedVersion(t *testing.T) {
	out, errStr, code := runScript(t, map[string]string{
		"SKILLS_REGISTRY_OS":      "Linux",
		"SKILLS_REGISTRY_ARCH":    "amd64",
		"SKILLS_REGISTRY_VERSION": "v9.9.9",
		"SKILLS_REGISTRY_DRY_RUN": "1",
	})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, errStr)
	}
	want := "https://github.com/anand-92/skills-registry/releases/download/v9.9.9/skills-registry_linux_amd64.tar.gz"
	if got := strings.TrimSpace(out); got != want {
		t.Fatalf("URL mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

// TestInstallScriptUnsupportedOS covers half of INSTALL-005: an
// unsupported uname -s exits 2 with a clear "unsupported OS" message.
func TestInstallScriptUnsupportedOS(t *testing.T) {
	_, errStr, code := runScript(t, map[string]string{
		"SKILLS_REGISTRY_OS":      "FreeBSD",
		"SKILLS_REGISTRY_ARCH":    "amd64",
		"SKILLS_REGISTRY_DRY_RUN": "1",
	})
	if code != 2 {
		t.Fatalf("expected exit 2 for unsupported OS, got %d (stderr: %s)", code, errStr)
	}
	if !strings.Contains(errStr, "unsupported OS: FreeBSD") {
		t.Fatalf("stderr missing OS error message:\n%s", errStr)
	}
}

// TestInstallScriptUnsupportedArch covers the other half of
// INSTALL-005: an unsupported uname -m exits 2 with a clear message.
func TestInstallScriptUnsupportedArch(t *testing.T) {
	_, errStr, code := runScript(t, map[string]string{
		"SKILLS_REGISTRY_OS":      "Linux",
		"SKILLS_REGISTRY_ARCH":    "mips",
		"SKILLS_REGISTRY_DRY_RUN": "1",
	})
	if code != 2 {
		t.Fatalf("expected exit 2 for unsupported arch, got %d (stderr: %s)", code, errStr)
	}
	if !strings.Contains(errStr, "unsupported architecture: mips") {
		t.Fatalf("stderr missing arch error message:\n%s", errStr)
	}
}

// TestInstallScriptEndToEnd covers INSTALL-006: when an actual
// installation runs (with a stubbed local tarball + sandboxed
// SKILLS_BIN_DIR), the binary ends up at $BIN_DIR/skills-registry
// with executable permission and the success line is printed.
func TestInstallScriptEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	tarball := filepath.Join(tmp, "fixture.tar.gz")
	if err := writeFixtureTarball(tarball); err != nil {
		t.Fatalf("fixture tarball: %v", err)
	}
	binDir := filepath.Join(tmp, "bin")

	out, errStr, code := runScript(t, map[string]string{
		"SKILLS_REGISTRY_OS":      "Darwin",
		"SKILLS_REGISTRY_ARCH":    "arm64",
		"SKILLS_BIN_DIR":          binDir,
		"SKILLS_REGISTRY_TARBALL": tarball,
	})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:%s\nstderr:%s", code, out, errStr)
	}

	installed := filepath.Join(binDir, "skills-registry")
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
	// Owner-executable bit must be set.
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("installed binary is not executable: mode=%v", info.Mode())
	}

	if !strings.Contains(out, "Run `skills-registry` to get started.") {
		t.Fatalf("stdout missing success line:\n%s", out)
	}
}

// writeFixtureTarball builds a minimal gzipped tar containing a single
// `skills-registry` shell script. Stays in pure Go (no shelling out to
// `tar`) so the test works identically on every developer machine.
func writeFixtureTarball(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	body := []byte("#!/bin/sh\necho hello\n")
	hdr := &tar.Header{
		Name: "skills-registry",
		Mode: 0o755,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(body); err != nil {
		return err
	}
	return nil
}

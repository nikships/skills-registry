package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// newTestInstaller returns an MCPInstaller wired up to in-memory stubs.
// Callers tweak fields after construction. Defaults: SkipInstall returns
// false, RunInstaller is unset (callers must wire it), LookPath rejects
// everything, Stderr is captured.
func newTestInstaller(t *testing.T) (*MCPInstaller, *bytes.Buffer) {
	t.Helper()
	stderr := &bytes.Buffer{}
	return &MCPInstaller{
		FallbackDirs: []string{t.TempDir()},
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
		RunInstaller: func(context.Context, []string) (int, error) {
			t.Fatalf("RunInstaller called unexpectedly")
			return 0, nil
		},
		SkipInstall: func() bool { return false },
		Stderr:      stderr,
		GOOS:        runtime.GOOS,
	}, stderr
}

func TestEntryPointPresentFindsBinaryInFallbackDir(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, MCPEntryPoint)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &MCPInstaller{FallbackDirs: []string{dir}, GOOS: runtime.GOOS}
	if !m.entryPointPresent() {
		t.Fatalf("expected entryPointPresent=true for %s", binPath)
	}
}

func TestEntryPointPresentReturnsFalseWhenMissing(t *testing.T) {
	m := &MCPInstaller{FallbackDirs: []string{t.TempDir()}, GOOS: runtime.GOOS}
	if m.entryPointPresent() {
		t.Fatalf("expected entryPointPresent=false on empty dir")
	}
}

func TestEntryPointPresentIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	// A *directory* named exactly like the binary must not count.
	if err := os.Mkdir(filepath.Join(dir, MCPEntryPoint), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &MCPInstaller{FallbackDirs: []string{dir}, GOOS: runtime.GOOS}
	if m.entryPointPresent() {
		t.Fatalf("a directory named %q must not satisfy the presence check", MCPEntryPoint)
	}
}

func TestEntryPointPresentChecksDotExeOnWindows(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, MCPEntryPoint+".exe")
	if err := os.WriteFile(binPath, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &MCPInstaller{FallbackDirs: []string{dir}, GOOS: "windows"}
	if !m.entryPointPresent() {
		t.Fatalf("expected .exe candidate to satisfy presence check on windows")
	}
}

func TestCandidatesIncludesUvAndPipxWhenAvailable(t *testing.T) {
	m, _ := newTestInstaller(t)
	m.LookPath = func(name string) (string, error) {
		// Pretend uv, pipx, and python3 are all installed.
		return "/fake/" + name, nil
	}
	got := m.candidates()
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates, got %d (%+v)", len(got), got)
	}
	if got[0].Label != "uv tool install" || got[0].Argv[0] != "uv" {
		t.Fatalf("first candidate must be uv, got %+v", got[0])
	}
	if got[1].Label != "pipx install" || got[1].Argv[0] != "pipx" {
		t.Fatalf("second candidate must be pipx, got %+v", got[1])
	}
	if got[2].Label != "pip install --user" || got[2].Argv[0] != "python3" {
		t.Fatalf("third candidate must be python3 pip, got %+v", got[2])
	}
	wantTail := []string{"-m", "pip", "install", "--user", "--upgrade", PYPIDist}
	if !reflect.DeepEqual(got[2].Argv[1:], wantTail) {
		t.Fatalf("python argv tail = %v, want %v", got[2].Argv[1:], wantTail)
	}
}

func TestCandidatesAlwaysIncludesPipFallback(t *testing.T) {
	m, _ := newTestInstaller(t)
	// LookPath rejects everything → no uv, no pipx, no python lookups
	// succeed. The pip fallback must still be appended.
	got := m.candidates()
	if len(got) != 1 {
		t.Fatalf("expected only pip fallback, got %d (%+v)", len(got), got)
	}
	if got[0].Label != "pip install --user" {
		t.Fatalf("expected pip fallback, got %+v", got[0])
	}
	// Even with python3 missing on LookPath, we default to "python3" so
	// the argv is structurally well-formed. (At Run time, exec will fail
	// and Ensure moves on to the manual-install hint.)
	if got[0].Argv[0] != "python3" {
		t.Fatalf("expected python3 default, got %q", got[0].Argv[0])
	}
}

func TestCandidatesFallsBackToPlainPython(t *testing.T) {
	m, _ := newTestInstaller(t)
	m.LookPath = func(name string) (string, error) {
		if name == "python" {
			return "/usr/bin/python", nil
		}
		return "", os.ErrNotExist
	}
	got := m.candidates()
	if len(got) != 1 {
		t.Fatalf("expected only pip fallback, got %d (%+v)", len(got), got)
	}
	if got[0].Argv[0] != "python" {
		t.Fatalf("expected fallback to 'python' when 'python3' is missing, got %q", got[0].Argv[0])
	}
}

func TestEnsureSkipsWhenEnvVarSet(t *testing.T) {
	m, stderr := newTestInstaller(t)
	m.SkipInstall = func() bool { return true }
	m.RunInstaller = func(context.Context, []string) (int, error) {
		t.Fatalf("RunInstaller must not be called when SkipInstall returns true")
		return 0, nil
	}
	m.Ensure(context.Background())
	if stderr.Len() != 0 {
		t.Fatalf("expected no output when skipping, got %q", stderr.String())
	}
}

func TestEnsureSkipsWhenAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, MCPEntryPoint)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	m := &MCPInstaller{
		FallbackDirs: []string{dir},
		LookPath:     func(string) (string, error) { return "/fake/x", nil },
		RunInstaller: func(context.Context, []string) (int, error) {
			t.Fatalf("RunInstaller must not be called when binary is already present")
			return 0, nil
		},
		SkipInstall: func() bool { return false },
		Stderr:      stderr,
		GOOS:        runtime.GOOS,
	}
	m.Ensure(context.Background())
	if stderr.Len() != 0 {
		t.Fatalf("expected no output when already present, got %q", stderr.String())
	}
}

// TestEnsureTriesInstallersUntilSuccess covers INSTALL-007: uv → pipx →
// pip, in order, stopping on first success. The first attempt fails with
// rc=1; the second succeeds and is verified to land the binary in a
// fallback dir; the third must never be called.
func TestEnsureTriesInstallersUntilSuccess(t *testing.T) {
	dir := t.TempDir()
	stderr := &bytes.Buffer{}
	var calls [][]string
	rcs := []int{1, 0}
	m := &MCPInstaller{
		FallbackDirs: []string{dir},
		LookPath:     func(string) (string, error) { return "/fake/x", nil },
		SkipInstall:  func() bool { return false },
		Stderr:       stderr,
		GOOS:         runtime.GOOS,
	}
	m.RunInstaller = func(_ context.Context, argv []string) (int, error) {
		calls = append(calls, append([]string(nil), argv...))
		rc := rcs[len(calls)-1]
		if rc == 0 {
			// "Install" by planting the binary in the fallback dir.
			binPath := filepath.Join(dir, MCPEntryPoint)
			if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return rc, nil
	}
	m.Ensure(context.Background())

	if len(calls) != 2 {
		t.Fatalf("expected 2 install attempts, got %d (%+v)", len(calls), calls)
	}
	if calls[0][0] != "uv" || calls[0][1] != "tool" || calls[0][2] != "install" {
		t.Fatalf("first call must be uv tool install, got %v", calls[0])
	}
	if calls[1][0] != "pipx" || calls[1][1] != "install" {
		t.Fatalf("second call must be pipx install, got %v", calls[1])
	}
	out := stderr.String()
	if !strings.Contains(out, "✓ installed via `pipx install`") {
		t.Fatalf("expected pipx success banner, got %q", out)
	}
	if strings.Contains(out, "Could not auto-install") {
		t.Fatalf("manual-install hint must NOT appear on success, got %q", out)
	}
}

// TestEnsureSkipsAttemptsThatLaunchError covers Python's OSError branch:
// an install attempt whose RunInstaller returns a non-nil error is
// skipped silently, and the loop moves on to the next candidate. (We
// don't claim success even if the binary somehow materialized — a launch
// error means the install probably didn't run.)
func TestEnsureSkipsAttemptsThatLaunchError(t *testing.T) {
	dir := t.TempDir()
	stderr := &bytes.Buffer{}
	var calls [][]string
	m := &MCPInstaller{
		FallbackDirs: []string{dir},
		LookPath:     func(string) (string, error) { return "/fake/x", nil },
		SkipInstall:  func() bool { return false },
		Stderr:       stderr,
		GOOS:         runtime.GOOS,
	}
	m.RunInstaller = func(_ context.Context, argv []string) (int, error) {
		calls = append(calls, append([]string(nil), argv...))
		if argv[0] == "uv" {
			return -1, errors.New("uv: not found")
		}
		// pipx and pip both exit non-zero so we end at the manual hint.
		return 1, nil
	}
	m.Ensure(context.Background())

	if len(calls) != 3 {
		t.Fatalf("expected 3 attempts (uv launch err, pipx fail, pip fail), got %d (%+v)", len(calls), calls)
	}
	out := stderr.String()
	if !strings.Contains(out, "Could not auto-install") {
		t.Fatalf("expected manual-install hint, got %q", out)
	}
}

// TestEnsurePrintsManualHintOnTotalFailure covers INSTALL-008: when every
// installer fails, the function prints a clear manual-install hint and
// returns normally (no panic, no abort).
func TestEnsurePrintsManualHintOnTotalFailure(t *testing.T) {
	dir := t.TempDir()
	stderr := &bytes.Buffer{}
	m := &MCPInstaller{
		FallbackDirs: []string{dir},
		LookPath:     func(string) (string, error) { return "/fake/x", nil },
		SkipInstall:  func() bool { return false },
		Stderr:       stderr,
		GOOS:         runtime.GOOS,
		RunInstaller: func(context.Context, []string) (int, error) { return 1, nil },
	}
	m.Ensure(context.Background())
	out := stderr.String()
	want := []string{
		"Could not auto-install",
		"uv tool install " + PYPIDist,
		"pipx install " + PYPIDist,
		"pip install --user " + PYPIDist,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("manual-install hint missing %q in:\n%s", w, out)
		}
	}
}

// TestEnsureDoesNotClaimSuccessWhenBinaryStillMissing guards against a
// subtle Python-mirror behavior: an installer that exits 0 but lands the
// binary somewhere outside our fallback dirs must NOT be treated as
// success. We try the next installer instead.
func TestEnsureDoesNotClaimSuccessWhenBinaryStillMissing(t *testing.T) {
	dir := t.TempDir()
	stderr := &bytes.Buffer{}
	var calls [][]string
	m := &MCPInstaller{
		FallbackDirs: []string{dir},
		LookPath:     func(string) (string, error) { return "/fake/x", nil },
		SkipInstall:  func() bool { return false },
		Stderr:       stderr,
		GOOS:         runtime.GOOS,
	}
	m.RunInstaller = func(_ context.Context, argv []string) (int, error) {
		calls = append(calls, append([]string(nil), argv...))
		// Every installer claims success (rc=0) but none plants the
		// binary in our fallback dir → Ensure must NOT short-circuit
		// after the first; it must keep going and ultimately hit the
		// manual-install hint.
		return 0, nil
	}
	m.Ensure(context.Background())

	if len(calls) != 3 {
		t.Fatalf("expected all 3 installers to be tried, got %d (%+v)", len(calls), calls)
	}
	if !strings.Contains(stderr.String(), "Could not auto-install") {
		t.Fatalf("expected manual-install hint when binary stays missing, got %q", stderr.String())
	}
}

// TestEnsureMCPEntryPointPublicAPI exercises the package-level wrapper
// against a tempdir HOME so we don't depend on the host machine's PATH.
// SkipInstallEnv short-circuits the flow.
func TestEnsureMCPEntryPointPublicAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(SkipInstallEnv, "1")
	// With SKILLS_SKIP_INSTALL=1, the function must return cleanly and
	// must not touch the filesystem under HOME.
	EnsureMCPEntryPoint(context.Background())
}

// TestRunInstallerCommandReportsExitCode confirms the default exec wrapper
// correctly maps a non-zero exit code into (rc, nil) — *not* (rc, err) —
// so Ensure's "rc == 0 && present" check sees the actual exit code.
func TestRunInstallerCommandReportsExitCode(t *testing.T) {
	// /bin/sh -c "exit 7" is portable across darwin and linux test runners.
	rc, err := runInstallerCommand(context.Background(), []string{"/bin/sh", "-c", "exit 7"})
	if err != nil {
		t.Fatalf("expected nil error for non-zero exit, got %v", err)
	}
	if rc != 7 {
		t.Fatalf("expected rc=7, got %d", rc)
	}
}

// TestRunInstallerCommandReturnsErrorWhenBinaryMissing confirms a missing
// binary surfaces as a non-nil error (and therefore an Ensure-loop skip),
// not as a fake exit code 0.
func TestRunInstallerCommandReturnsErrorWhenBinaryMissing(t *testing.T) {
	rc, err := runInstallerCommand(context.Background(), []string{
		filepath.Join(t.TempDir(), "definitely-does-not-exist-binary"),
	})
	if err == nil {
		t.Fatalf("expected error when binary is missing, got rc=%d nil", rc)
	}
	// Make sure we didn't accidentally classify it as an ExitError.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("missing-binary error must NOT be an ExitError, got %v", err)
	}
}

// TestRunInstallerCommandEmptyArgvErrors verifies the guard against
// accidentally exec-ing with no command.
func TestRunInstallerCommandEmptyArgvErrors(t *testing.T) {
	if _, err := runInstallerCommand(context.Background(), nil); err == nil {
		t.Fatalf("expected error on empty argv")
	}
}

// TestDefaultMCPInstallerUsesLocateMCPBinaryDirs documents the requirement
// that defaultMCPInstaller's FallbackDirs MUST stay in sync with the
// candidate list inside “locateMCPBinary“ in
// cli/cmd/skill-registry/bootstrap.go. If you add or remove a directory
// there, mirror it here.
func TestDefaultMCPInstallerUsesLocateMCPBinaryDirs(t *testing.T) {
	t.Setenv("HOME", "/home/test-user")
	m := defaultMCPInstaller()
	want := []string{
		"/home/test-user/.local/bin",
		"/home/test-user/.local/share/uv/tools/skills-registry/bin",
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	if !reflect.DeepEqual(m.FallbackDirs, want) {
		t.Fatalf("defaultMCPInstaller FallbackDirs drifted from locateMCPBinary's list.\n"+
			"got:  %v\nwant: %v\n"+
			"Keep cli/internal/bootstrap/mcp_install.go in sync with "+
			"cli/cmd/skill-registry/bootstrap.go::locateMCPBinary.",
			m.FallbackDirs, want)
	}
	if m.Stderr != io.Discard {
		t.Fatalf("default Stderr must be io.Discard (TUI-safe)")
	}
	if m.GOOS != runtime.GOOS {
		t.Fatalf("default GOOS must match runtime.GOOS, got %q", m.GOOS)
	}
	// Sanity check: SkipInstall reads the env var.
	t.Setenv(SkipInstallEnv, "")
	if m.SkipInstall() {
		t.Fatalf("SkipInstall must be false when env var is empty")
	}
	t.Setenv(SkipInstallEnv, "1")
	if !m.SkipInstall() {
		t.Fatalf("SkipInstall must be true when env var is set")
	}
}

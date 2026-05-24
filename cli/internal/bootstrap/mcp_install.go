package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// MCPEntryPoint is the console-script name desktop MCP clients
// (Claude Desktop, Cursor, VS Code/Copilot, Codex, …) invoke.
const MCPEntryPoint = "skill-registry-mcp"

// PYPIDist is the PyPI distribution that provides MCPEntryPoint.
const PYPIDist = "skills-registry"

// SkipInstallEnv, when set to any non-empty value, short-circuits
// EnsureMCPEntryPoint. Mirrors the Python “SKILLS_SKIP_INSTALL“ knob.
const SkipInstallEnv = "SKILLS_SKIP_INSTALL"

// EnsureMCPEntryPoint persists the skill-registry-mcp console script on
// disk so desktop MCP clients can launch it from the stripped subprocess
// environment they spawn it in.
//
// It is a no-op when the SkipInstallEnv environment variable is set or
// when an MCP entry-point binary is already present in one of the curated
// fallback dirs (matching locateMCPBinary's candidate list). Otherwise it
// tries, in order:
//
//  1. uv tool install --force skills-registry
//  2. pipx install --force skills-registry
//  3. python3 -m pip install --user --upgrade skills-registry
//
// The first attempt whose process exits 0 AND lands a binary in one of
// the fallback dirs wins. On total failure, a manual-install hint is
// written to stderr but the function returns normally — the caller's
// bootstrap flow is never aborted by an install failure.
//
// Mirrors “src/skills_mcp/init.py::_ensure_mcp_entry_point“.
func EnsureMCPEntryPoint(ctx context.Context) {
	defaultMCPInstaller().Ensure(ctx)
}

// MCPInstaller exposes the EnsureMCPEntryPoint dependencies as overridable
// fields so tests can drive the flow without touching the host filesystem
// or PATH. Wizard and standalone-bootstrap callers should use
// EnsureMCPEntryPoint instead.
type MCPInstaller struct {
	// FallbackDirs is the priority-ordered list of directories to probe
	// for an existing MCPEntryPoint binary. Must mirror locateMCPBinary's
	// candidate list (cli/cmd/skill-registry/bootstrap.go) so the
	// absolute path the wire-up snippet prints matches what we install.
	FallbackDirs []string

	// LookPath defaults to exec.LookPath. Tests can stub which installers
	// are "available" by returning a not-found error for the named binary.
	LookPath func(name string) (string, error)

	// RunInstaller invokes argv and returns the process exit code (0 on
	// success, non-zero on failure). A non-nil error means the process
	// could not be launched at all (binary missing, permission denied,
	// …) — those attempts are silently skipped, matching Python's
	// ``OSError`` branch.
	RunInstaller func(ctx context.Context, argv []string) (int, error)

	// SkipInstall short-circuits Ensure when it returns true. The default
	// reads the SkipInstallEnv environment variable.
	SkipInstall func() bool

	// Stderr is where status / hint messages are written.
	Stderr io.Writer

	// GOOS lets tests pretend to be on Windows so the ``.exe`` candidate
	// gets exercised.
	GOOS string
}

// defaultMCPInstaller returns an MCPInstaller wired up to real system
// calls. Fallback dirs match locateMCPBinary in bootstrap.go.
func defaultMCPInstaller() *MCPInstaller {
	home, _ := os.UserHomeDir()
	return &MCPInstaller{
		FallbackDirs: []string{
			filepath.Join(home, ".local", "bin"),
			// Direct path inside uv's tool data dir, in case the
			// ~/.local/bin symlink ever drifts.
			filepath.Join(home, ".local", "share", "uv", "tools", "skills-registry", "bin"),
			"/opt/homebrew/bin",
			"/usr/local/bin",
		},
		LookPath:     exec.LookPath,
		RunInstaller: runInstallerCommand,
		SkipInstall:  func() bool { return os.Getenv(SkipInstallEnv) != "" },
		Stderr:       io.Discard,
		GOOS:         runtime.GOOS,
	}
}

// installerAttempt is one (label, argv) candidate.
type installerAttempt struct {
	Label string
	Argv  []string
}

// Ensure runs the install flow described in EnsureMCPEntryPoint.
func (m *MCPInstaller) Ensure(ctx context.Context) {
	if m.SkipInstall != nil && m.SkipInstall() {
		return
	}
	if m.entryPointPresent() {
		return
	}
	fmt.Fprintf(m.Stderr,
		"Installing `%s` so desktop MCP clients can launch it…\n",
		MCPEntryPoint)
	for _, attempt := range m.candidates() {
		rc, err := m.RunInstaller(ctx, attempt.Argv)
		if err != nil {
			// Couldn't launch the binary at all. Move on, matching
			// Python's OSError branch.
			continue
		}
		if rc == 0 && m.entryPointPresent() {
			fmt.Fprintf(m.Stderr, "  ✓ installed via `%s`\n", attempt.Label)
			return
		}
	}
	m.printManualHint()
}

// printManualHint writes the same fallback message the Python shim prints
// when every install attempt failed.
func (m *MCPInstaller) printManualHint() {
	fmt.Fprintf(m.Stderr,
		"\n"+
			"! Could not auto-install `%s`. Continuing — the Go bootstrap\n"+
			"  will still run, but the MCP snippet it prints will refer to a binary\n"+
			"  that does not yet exist. Install it manually with one of:\n"+
			"    uv tool install %s\n"+
			"    pipx install %s\n"+
			"    python -m pip install --user %s\n",
		MCPEntryPoint, PYPIDist, PYPIDist, PYPIDist)
}

// candidates returns the ordered installers to attempt. uv and pipx are
// dropped if their binary isn't on PATH; the final pip fallback is always
// included so the function still has something to try on systems without
// either.
func (m *MCPInstaller) candidates() []installerAttempt {
	out := make([]installerAttempt, 0, 3)
	if m.lookPath("uv") {
		out = append(out, installerAttempt{
			Label: "uv tool install",
			Argv:  []string{"uv", "tool", "install", "--force", PYPIDist},
		})
	}
	if m.lookPath("pipx") {
		out = append(out, installerAttempt{
			Label: "pipx install",
			Argv:  []string{"pipx", "install", "--force", PYPIDist},
		})
	}
	out = append(out, installerAttempt{
		Label: "pip install --user",
		Argv:  []string{m.pythonExe(), "-m", "pip", "install", "--user", "--upgrade", PYPIDist},
	})
	return out
}

// lookPath returns true when the named binary resolves via LookPath.
func (m *MCPInstaller) lookPath(name string) bool {
	if m.LookPath == nil {
		return false
	}
	_, err := m.LookPath(name)
	return err == nil
}

// pythonExe picks the python interpreter to drive “-m pip“. Prefers
// “python3“ (matches the F1.2 spec); falls back to “python“ so we
// still produce a runnable argv on systems where only the unversioned
// name exists. If both are missing, the exec will fail and Ensure will
// move on to the manual-install hint.
func (m *MCPInstaller) pythonExe() string {
	if m.lookPath("python3") {
		return "python3"
	}
	if m.lookPath("python") {
		return "python"
	}
	return "python3"
}

// entryPointPresent walks the curated fallback dirs and returns true on
// the first executable file with the right name. Mirrors locateMCPBinary's
// candidate list so a positive result here guarantees the wire-up snippet
// will embed a stable absolute path.
func (m *MCPInstaller) entryPointPresent() bool {
	exeNames := []string{MCPEntryPoint}
	if m.GOOS == "windows" {
		exeNames = []string{MCPEntryPoint + ".exe", MCPEntryPoint}
	}
	for _, dir := range m.FallbackDirs {
		for _, name := range exeNames {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil || info.IsDir() {
				continue
			}
			return true
		}
	}
	return false
}

// runInstallerCommand executes argv with output discarded — only the exit
// code matters for the success decision, and noisy installer chatter would
// clutter the bootstrap flow. Matches Python's “capture_output=True“
// behavior.
func runInstallerCommand(ctx context.Context, argv []string) (int, error) {
	if len(argv) == 0 {
		return -1, errors.New("empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

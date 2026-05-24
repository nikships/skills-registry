package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// TestErrToastSurfacesActionAndMessage pins the F3.2 toast formatter:
// failures show the action name + flattened error message with a leading
// ✗ glyph so the user spots the problem at a glance on the next frame.
func TestErrToastSurfacesActionAndMessage(t *testing.T) {
	tt := errToast("sync", errors.New("gh auth status: not authenticated"))
	if tt.ok {
		t.Error("error toast should set ok=false")
	}
	if !strings.HasPrefix(tt.text, "✗ sync:") {
		t.Errorf("toast text missing \"✗ sync:\" prefix: %q", tt.text)
	}
	if !strings.Contains(tt.text, "not authenticated") {
		t.Errorf("toast text missing underlying error: %q", tt.text)
	}
}

// TestErrToastFlattensNewlines verifies multi-line errors collapse to a
// bullet-separated single line so the toast never pushes the footer
// off-screen. Mirrors the listmodel.go renderToast pattern.
func TestErrToastFlattensNewlines(t *testing.T) {
	tt := errToast("publish", errors.New("line one\nline two\nline three"))
	if strings.Contains(tt.text, "\n") {
		t.Errorf("toast text contains newline (must be flattened): %q", tt.text)
	}
	if !strings.Contains(tt.text, " · ") {
		t.Errorf("toast text missing bullet separator: %q", tt.text)
	}
}

// TestErrToastDemotesCancellation pins the cancellation branch: a user
// hitting ctrl+c inside a sub-action should land on a neutral
// "cancelled" toast, not a red failure. context.Canceled is the
// canonical signal both bubble-tea and the gh client surface for user
// cancellation, so errors.Is is the right check.
func TestErrToastDemotesCancellation(t *testing.T) {
	tt := errToast("add", context.Canceled)
	if !tt.ok {
		t.Error("cancellation toast should be neutral (ok=true)")
	}
	if !strings.Contains(tt.text, "cancelled") {
		t.Errorf("toast text missing \"cancelled\": %q", tt.text)
	}
	if strings.Contains(tt.text, "✗") {
		t.Errorf("cancellation toast should not show ✗ glyph: %q", tt.text)
	}
}

// TestErrToastDemotesWrappedCancellation guarantees the errors.Is check
// handles fmt.Errorf("...: %w", context.Canceled) wrappers — common
// when the cancellation comes from a downstream subprocess.
func TestErrToastDemotesWrappedCancellation(t *testing.T) {
	wrapped := errors.Join(errors.New("git push: command interrupted"), context.Canceled)
	tt := errToast("sync", wrapped)
	if !tt.ok {
		t.Error("wrapped cancellation should still be neutral")
	}
	if !strings.Contains(tt.text, "cancelled") {
		t.Errorf("wrapped cancellation toast missing \"cancelled\": %q", tt.text)
	}
}

// TestDispatchHubActionRemoveMissingConfig pins the F4.1 wiring: with
// no config on disk, runRemoveFromHub fails fast in
// loadRegistryForRemove and surfaces an error toast that still names
// the action. We point XDG_CONFIG_HOME at an isolated tempdir so the
// test never reads or writes the developer's real registry.toml.
//
// runRemoveFromHub also tries to launch a Bubble Tea program to prompt
// for the slug. In a non-TTY test environment that call returns an
// error immediately, which is exactly what we want: the toast names
// the action, ok=false, and fatal stays nil so the hub loop continues.
func TestDispatchHubActionRemoveMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SKILLS_REGISTRY", "")
	r := dispatchHubAction(context.Background(), tui.HubActionRemove)
	if r.ok {
		t.Errorf("remove without config/TTY should produce error toast, got ok=true: %q", r.text)
	}
	if !strings.Contains(r.text, "remove") {
		t.Errorf("remove toast should name the action: %q", r.text)
	}
	if strings.Contains(r.text, "F4.1") {
		t.Errorf("remove still using placeholder text: %q", r.text)
	}
	if r.fatal != nil {
		t.Errorf("remove dispatch set fatal=%v, want nil so loop continues", r.fatal)
	}
}

// TestDispatchHubActionSettingsMissingConfig pins the F3.3 wiring: with
// no config on disk, runSettingsFromHub bails before launching the
// alt-screen TUI and surfaces an "ErrMissing"-style error toast that
// still names the action (so the hub frame says ✗ settings: …). We
// point XDG_CONFIG_HOME at an isolated tempdir so the test never reads
// or writes the developer's real registry.toml.
func TestDispatchHubActionSettingsMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SKILLS_REGISTRY", "")
	r := dispatchHubAction(context.Background(), tui.HubActionSettings)
	if r.ok {
		t.Errorf("settings without config should produce error toast, got ok=true: %q", r.text)
	}
	if !strings.Contains(r.text, "settings") {
		t.Errorf("settings toast should name the action: %q", r.text)
	}
	// Crucially, the F3.3 placeholder is gone — the dispatch now runs
	// the real flow.
	if strings.Contains(r.text, "wiring lands in F3.3") {
		t.Errorf("settings still using placeholder text: %q", r.text)
	}
}

// TestSettingsSaverWritesConfig verifies the closure passed to
// tui.NewSettings round-trips through config.Save and produces a path
// the user can find on disk. Uses XDG_CONFIG_HOME isolation so the
// developer's real registry.toml is untouched.
func TestSettingsSaverWritesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SKILLS_REGISTRY", "")
	saver := settingsSaver()
	path, err := saver("new-owner/new-repo", "develop")
	if err != nil {
		t.Fatalf("saver returned err: %v", err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("saver wrote to %q, expected prefix %q", path, dir)
	}
	if !strings.HasSuffix(path, "registry.toml") {
		t.Errorf("saver wrote to %q, want suffix registry.toml", path)
	}
	// The follow-up Load() should round-trip the values verbatim.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("post-save Load() err: %v", err)
	}
	if cfg.Repo != "new-owner/new-repo" {
		t.Errorf("post-save repo = %q, want new-owner/new-repo", cfg.Repo)
	}
	if cfg.DefaultBranch != "develop" {
		t.Errorf("post-save branch = %q, want develop", cfg.DefaultBranch)
	}
}

// TestSettingsSaverRejectsBadRepo guarantees the saver propagates
// config.Save validation failures back to the SettingsModel so it can
// surface them as an error caption.
func TestSettingsSaverRejectsBadRepo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SKILLS_REGISTRY", "")
	saver := settingsSaver()
	if _, err := saver("not-a-valid-repo", "main"); err == nil {
		t.Fatal("saver accepted invalid repo, want error")
	}
}

// TestDispatchHubActionUnknown verifies the defensive default branch.
// An unrecognized action ID (e.g. a future card whose handler hasn't
// been wired) produces an error toast and keeps the loop alive rather
// than panicking or returning an unhandled value.
func TestDispatchHubActionUnknown(t *testing.T) {
	r := dispatchHubAction(context.Background(), "bogus")
	if r.ok {
		t.Error("unknown action should produce error toast (ok=false)")
	}
	if !strings.Contains(r.text, "bogus") {
		t.Errorf("unknown-action toast should name the offending ID: %q", r.text)
	}
	if r.fatal != nil {
		t.Errorf("unknown action should not set fatal err: %v", r.fatal)
	}
}

// TestHubToastZeroValue documents the empty-toast contract: a
// zero-value hubToast does not carry text and does not flag fatal,
// which is the steady-state used by the first hub iteration before any
// action has run.
func TestHubToastZeroValue(t *testing.T) {
	var zero hubToast
	if zero.text != "" {
		t.Errorf("zero hubToast text = %q, want \"\"", zero.text)
	}
	if zero.ok {
		t.Error("zero hubToast ok = true, want false")
	}
	if zero.fatal != nil {
		t.Errorf("zero hubToast fatal = %v, want nil", zero.fatal)
	}
}

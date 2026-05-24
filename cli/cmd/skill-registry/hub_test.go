package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/config"
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

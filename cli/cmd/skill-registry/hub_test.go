package main

import (
	"context"
	"errors"
	"strings"
	"testing"

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

// TestDispatchHubActionRemovePlaceholder pins the remove tile's interim
// behavior: F4.1 will replace this branch with a real flow, but until
// then it must produce a neutral toast that doesn't fail the loop.
func TestDispatchHubActionRemovePlaceholder(t *testing.T) {
	r := dispatchHubAction(context.Background(), tui.HubActionRemove)
	if !r.ok {
		t.Errorf("remove placeholder should be neutral (ok=true), got ok=%v", r.ok)
	}
	if !strings.Contains(r.text, "F4.1") {
		t.Errorf("remove placeholder should reference follow-up feature: %q", r.text)
	}
	if r.fatal != nil {
		t.Errorf("remove placeholder set fatal=%v", r.fatal)
	}
}

// TestDispatchHubActionSettingsPlaceholder mirrors the remove placeholder
// test for the Settings tile (deferred to F3.3).
func TestDispatchHubActionSettingsPlaceholder(t *testing.T) {
	r := dispatchHubAction(context.Background(), tui.HubActionSettings)
	if !r.ok {
		t.Errorf("settings placeholder should be neutral (ok=true), got ok=%v", r.ok)
	}
	if !strings.Contains(r.text, "F3.3") {
		t.Errorf("settings placeholder should reference follow-up feature: %q", r.text)
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

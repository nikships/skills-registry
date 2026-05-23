package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/config"
)

// TestBareRouteDecision exercises the pure routing matrix from runRoot.
// Each row corresponds to one assertion from the F1.3 spec:
//
//	(a) ErrMissing  + TTY     → wizard
//	(b) nil         + TTY     → hub
//	(c) anything    + non-TTY → help
//
// Plus the malformed-config passthrough (anything else → caller surfaces
// the error).
func TestBareRouteDecision(t *testing.T) {
	otherErr := errors.New("broken registry.toml")
	cases := []struct {
		name    string
		isTTY   bool
		loadErr error
		want    bareRoute
	}{
		{"non-tty + no config error", false, nil, bareRouteHelp},
		{"non-tty + missing config", false, config.ErrMissing, bareRouteHelp},
		{"non-tty + load error", false, otherErr, bareRouteHelp},
		{"tty + missing config goes to wizard", true, config.ErrMissing, bareRouteWizard},
		{"tty + config loaded goes to hub", true, nil, bareRouteHub},
		{"tty + non-missing load error surfaces", true, otherErr, bareRouteError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bareRouteDecision(tc.isTTY, tc.loadErr)
			if got != tc.want {
				t.Fatalf("bareRouteDecision(%v, %v) = %v, want %v",
					tc.isTTY, tc.loadErr, got, tc.want)
			}
		})
	}
}

// TestBareRouteDecisionWrappedErrMissing pins down that callers can use
// fmt.Errorf("...: %w", config.ErrMissing) without breaking routing —
// runRoot relies on errors.Is, not equality.
func TestBareRouteDecisionWrappedErrMissing(t *testing.T) {
	wrapped := errors.Join(errors.New("read config"), config.ErrMissing)
	got := bareRouteDecision(true, wrapped)
	if got != bareRouteWizard {
		t.Fatalf("wrapped ErrMissing routed to %v, want bareRouteWizard", got)
	}
}

// TestRootCmdRegistersAllSubcommands guards against a regression where
// a future refactor of newRootCmd accidentally drops a subcommand.
// All existing subcommands must still be reachable after routing was
// added (ROUTING-004: explicit subcommand bypasses hub/wizard).
func TestRootCmdRegistersAllSubcommands(t *testing.T) {
	root := newRootCmd()
	expected := []string{"bootstrap", "list", "get", "sync", "add", "publish"}
	for _, name := range expected {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("subcommand %q not registered: %v", name, err)
		}
		if cmd == root {
			t.Fatalf("subcommand %q lookup returned root command", name)
		}
		if cmd.Name() != name {
			t.Fatalf("subcommand lookup for %q returned %q", name, cmd.Name())
		}
	}
}

// TestRootCmdRejectsExtraArgs covers the NoArgs guard on the root. A
// bare invocation with a stray positional should fail with a useful
// cobra error rather than falling through to routing with garbage args.
func TestRootCmdRejectsExtraArgs(t *testing.T) {
	root := newRootCmd()
	if root.Args == nil {
		t.Fatal("root.Args is nil; bare command should reject extraneous args")
	}
	if err := root.Args(root, []string{"surprise"}); err == nil {
		t.Fatal("expected error for stray positional arg, got nil")
	}
	if err := root.Args(root, nil); err != nil {
		t.Fatalf("expected nil for empty args, got %v", err)
	}
}

// TestRootCmdHelpDoesNotTriggerRouting verifies ROUTING-006: --help
// always renders usage text regardless of first-run state. cobra
// intercepts --help before RunE, so this exercises the integration
// path without touching the filesystem.
func TestRootCmdHelpDoesNotTriggerRouting(t *testing.T) {
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "skill-registry") {
		t.Fatalf("help output missing program name:\n%s", out)
	}
	// The long description's hint about subcommands should make it
	// into the help text — cheap sanity check that we wired Long.
	if !strings.Contains(out, "skill-registry list") {
		t.Fatalf("help output missing subcommand example:\n%s", out)
	}
}

// TestRootCmdHelpSubcommandDoesNotTriggerRouting mirrors the previous
// test for `skill-registry help` (cobra's auto-injected help command).
// Same guarantee: routing must not fire when the user is asking for
// usage info.
func TestRootCmdHelpSubcommandDoesNotTriggerRouting(t *testing.T) {
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help subcommand returned error: %v", err)
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "skill-registry") {
		t.Fatalf("help subcommand output missing program name:\n%s", out)
	}
}

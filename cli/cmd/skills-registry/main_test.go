package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
)

// TestBareRouteDecision exercises the pure routing matrix from runRoot.
// Each row corresponds to one assertion from the F1.3 / F1.4 spec:
//
//	(a) ErrMissing  + TTY + !json → wizard
//	(b) nil         + TTY + !json → hub
//	(c) anything    + non-TTY     → help
//	(d) anything    + TTY  + json → help (F1.4: --json suppresses TUI)
//
// Plus the malformed-config passthrough (TTY + non-missing load error
// + !json → caller surfaces the error).
func TestBareRouteDecision(t *testing.T) {
	otherErr := errors.New("broken registry.toml")
	cases := []struct {
		name     string
		isTTY    bool
		jsonMode bool
		loadErr  error
		want     bareRoute
	}{
		{"non-tty + no config error", false, false, nil, bareRouteHelp},
		{"non-tty + missing config", false, false, config.ErrMissing, bareRouteHelp},
		{"non-tty + load error", false, false, otherErr, bareRouteHelp},
		{"tty + missing config goes to wizard", true, false, config.ErrMissing, bareRouteWizard},
		{"tty + config loaded goes to hub", true, false, nil, bareRouteHub},
		{"tty + non-missing load error surfaces", true, false, otherErr, bareRouteError},
		// F1.4: --json forces help regardless of config state when it
		// would otherwise launch a TUI (wizard or hub).
		{"tty + json + missing config", true, true, config.ErrMissing, bareRouteHelp},
		{"tty + json + config loaded", true, true, nil, bareRouteHelp},
		{"tty + json + load error", true, true, otherErr, bareRouteHelp},
		// --json on a non-TTY environment must not regress the non-TTY
		// short-circuit either.
		{"non-tty + json + missing config", false, true, config.ErrMissing, bareRouteHelp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bareRouteDecision(tc.isTTY, tc.jsonMode, tc.loadErr)
			if got != tc.want {
				t.Fatalf("bareRouteDecision(%v, %v, %v) = %v, want %v",
					tc.isTTY, tc.jsonMode, tc.loadErr, got, tc.want)
			}
		})
	}
}

// TestBareRouteDecisionWrappedErrMissing pins down that callers can use
// fmt.Errorf("...: %w", config.ErrMissing) without breaking routing —
// runRoot relies on errors.Is, not equality.
func TestBareRouteDecisionWrappedErrMissing(t *testing.T) {
	wrapped := errors.Join(errors.New("read config"), config.ErrMissing)
	got := bareRouteDecision(true, false, wrapped)
	if got != bareRouteWizard {
		t.Fatalf("wrapped ErrMissing routed to %v, want bareRouteWizard", got)
	}
}

// TestRootCmdRegistersJSONFlag verifies F1.4's central wiring: the
// persistent --json flag must be registered on the root cobra command
// so cobra propagates it to every subcommand at parse time. Without
// this, `skills-registry list --json` would fail with "unknown flag".
func TestRootCmdRegistersJSONFlag(t *testing.T) {
	root := newRootCmd()
	f := root.PersistentFlags().Lookup(jsonout.FlagName)
	if f == nil {
		t.Fatalf("root command is missing persistent flag --%s", jsonout.FlagName)
	}
	if f.DefValue != "false" {
		t.Fatalf("default value for --%s = %q, want \"false\"", jsonout.FlagName, f.DefValue)
	}
}

// TestRootJSONFlagPropagatesToSubcommands is the higher-level
// integration check: cobra must parse --json on a subcommand
// invocation and flip jsonout.Enabled() to true before the subcommand
// runs. This is the contract every subcommand will rely on in F4.2.
func TestRootJSONFlagPropagatesToSubcommands(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(false)

	root := newRootCmd()
	// Use --help on a subcommand so RunE doesn't actually execute the
	// real command body (which would touch the user's filesystem). We
	// only care that cobra parses --json into the shared package
	// variable as part of resolving the subcommand.
	root.SetArgs([]string{"list", "--json", "--help"})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !jsonout.Enabled() {
		t.Fatal("jsonout.Enabled() returned false after `list --json` invocation")
	}
}

// TestRootCmdRegistersAllSubcommands guards against a regression where
// a future refactor of newRootCmd accidentally drops a subcommand.
// All existing subcommands must still be reachable after routing was
// added (ROUTING-004: explicit subcommand bypasses hub/wizard).
func TestRootCmdRegistersAllSubcommands(t *testing.T) {
	root := newRootCmd()
	expected := []string{"bootstrap", "list", "get", "sync", "add", "publish", "remove", "update"}
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
	if !strings.Contains(out, "skills-registry") {
		t.Fatalf("help output missing program name:\n%s", out)
	}
	// The long description's hint about subcommands should make it
	// into the help text — cheap sanity check that we wired Long.
	if !strings.Contains(out, "skills-registry list") {
		t.Fatalf("help output missing subcommand example:\n%s", out)
	}
}

// TestRootCmdHelpSubcommandDoesNotTriggerRouting mirrors the previous
// test for `skills-registry help` (cobra's auto-injected help command).
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
	if !strings.Contains(out, "skills-registry") {
		t.Fatalf("help subcommand output missing program name:\n%s", out)
	}
}

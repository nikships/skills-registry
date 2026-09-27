package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/jsonout"
)

// This file pins the unified CLI error contract (cli-tui-5 + cli-tui-6):
//
//   - Runtime failures print no usage text and exactly one `Error:` line;
//     usage appears only for actual misuse (wrong arg count, unknown
//     command).
//   - Under --json, every failure mode emits exactly one parseable
//     {"error": ...} object on stdout — including usage/arg failures,
//     which never reach a RunE — plus the single `Error:` line on stderr.
//
// All tables drive run(), the same entrypoint main uses, so they cover the
// full parse → execute → report pipeline rather than individual helpers.

// runCLI executes argv through run with captured streams. The global --json
// flag state is reset first because jsonout.Enabled is process-global:
// parsing --json in one case would otherwise leak into the next, since a
// later parse without the flag never flips it back. Callers save/restore
// the ambient value once around the whole table.
func runCLI(t *testing.T, argv []string) (code int, jsonOut, cobraOut, errOut string) {
	t.Helper()
	jsonout.SetEnabled(false)
	jsonBuf := captureJSONOut(t)
	var outBuf, errBuf bytes.Buffer
	code = run(argv, &outBuf, &errBuf)
	return code, jsonBuf.String(), outBuf.String(), errBuf.String()
}

// saveJSONFlag snapshots the ambient --json state for the duration of the
// calling test.
func saveJSONFlag(t *testing.T) {
	t.Helper()
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
}

// isolateNoConfig points config resolution at empty dirs so config.Load
// fails with ErrMissing. Every command that needs the registry hits this
// before touching the network, which makes it the standard deterministic
// runtime error for the tables below.
func isolateNoConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	// The env var wins over the file, so a machine that exports it would
	// otherwise load a real registry here.
	t.Setenv("SKILLS_REGISTRY", "")
}

// isolateFailingIndex points the public skill index at a 503 so discover
// fails without reaching the network.
func isolateFailingIndex(t *testing.T) {
	t.Helper()
	serveDiscover(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}

// isolateNoGHAuth installs a `gh` shim that always fails, so bootstrap's
// auth check errors deterministically. GH_BIN takes precedence over PATH
// and the absolute fallback paths, so no real binary can leak in.
func isolateNoGHAuth(t *testing.T) {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}
	t.Setenv("GH_BIN", stub)
}

// requireSingleJSONError asserts raw is exactly one {"error": ...} object
// with a non-empty message. json.Unmarshal rejects trailing data, so a
// double-printed envelope fails here.
func requireSingleJSONError(t *testing.T, raw string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("stdout must be a single parseable JSON object, got %q: %v", raw, err)
	}
	msg, _ := payload["error"].(string)
	if msg == "" {
		t.Fatalf("missing non-empty error field in %q", raw)
	}
	return msg
}

// requireSingleErrorLine asserts stderr carries the error exactly once.
func requireSingleErrorLine(t *testing.T, errOut string) {
	t.Helper()
	if n := strings.Count(errOut, "Error:"); n != 1 {
		t.Errorf("stderr must carry exactly one Error: line, got %d:\n%s", n, errOut)
	}
}

// TestRunRuntimeErrorsOmitUsage covers every subcommand's runtime failure in
// human mode: no usage dump, no JSON, and one Error line. Each case forces
// its error without network access (missing config, failing index shim,
// failing gh shim, or an unwritable update target).
func TestRunRuntimeErrorsOmitUsage(t *testing.T) {
	saveJSONFlag(t)
	badBin := filepath.Join(t.TempDir(), "no-such-dir", "bin")
	cases := []struct {
		name  string
		argv  []string
		setup func(*testing.T)
	}{
		{"list", []string{"list", "--plain"}, isolateNoConfig},
		{"search", []string{"search", "pdf"}, isolateNoConfig},
		{"get", []string{"get", "demo"}, isolateNoConfig},
		{"sync", []string{"sync"}, isolateNoConfig},
		{"add", []string{"add", "./source"}, isolateNoConfig},
		{"publish", []string{"publish", "./skill"}, isolateNoConfig},
		{"remove", []string{"remove", "demo"}, isolateNoConfig},
		{"discover", []string{"discover", "pdf"}, isolateFailingIndex},
		{"bootstrap", []string{"bootstrap"}, isolateNoGHAuth},
		{"update", []string{"update", "--version", "v9.9.9", "--bin", badBin}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			code, jsonOut, cobraOut, errOut := runCLI(t, tc.argv)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (argv %q)", code, tc.argv)
			}
			if jsonOut != "" {
				t.Errorf("human mode must print no JSON, got %q", jsonOut)
			}
			if strings.Contains(cobraOut, "Usage:") || strings.Contains(errOut, "Usage:") {
				t.Errorf("a runtime failure must not dump usage:\nstdout:\n%s\nstderr:\n%s", cobraOut, errOut)
			}
			requireSingleErrorLine(t, errOut)
		})
	}
}

// TestRunJSONUsageErrorsEmitEnvelope covers usage-level failures under
// --json: wrong arg counts and unknown commands, which return before any
// RunE runs. Stdout must hold exactly the {"error"} object (no usage text
// for a parser to choke on) and stderr the single Error line.
func TestRunJSONUsageErrorsEmitEnvelope(t *testing.T) {
	saveJSONFlag(t)
	cases := []struct {
		name string
		argv []string
	}{
		{"search missing arg", []string{"search", "--json"}},
		{"get missing arg", []string{"get", "--json"}},
		{"publish missing arg", []string{"publish", "--json"}},
		{"remove missing arg", []string{"remove", "--json"}},
		{"add missing arg", []string{"add", "--json"}},
		{"discover missing arg", []string{"discover", "--json"}},
		{"update extra arg", []string{"update", "bogus", "--json"}},
		{"unknown command", []string{"frobnicate", "--json"}},
		{"json before subcommand", []string{"--json", "search"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No setup: validation fails before any config or network use.
			code, jsonOut, cobraOut, errOut := runCLI(t, tc.argv)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (argv %q)", code, tc.argv)
			}
			requireSingleJSONError(t, jsonOut)
			if strings.Contains(cobraOut, "Usage:") || strings.Contains(errOut, "Usage:") {
				t.Errorf("a --json failure must not dump usage:\nstdout:\n%s\nstderr:\n%s", cobraOut, errOut)
			}
			requireSingleErrorLine(t, errOut)
		})
	}
}

// TestRunJSONRuntimeErrorsEmitSingleEnvelope covers every subcommand's
// runtime failure under --json. Each RunE prints its own envelope and
// returns a marked error, so the root handler must not print a second
// one; requireSingleJSONError fails the test if it does. Bootstrap has
// no --json branch of its own, so its envelope comes from the root
// handler — pinned here to the same shape.
func TestRunJSONRuntimeErrorsEmitSingleEnvelope(t *testing.T) {
	saveJSONFlag(t)
	badBin := filepath.Join(t.TempDir(), "no-such-dir", "bin")
	cases := []struct {
		name  string
		argv  []string
		setup func(*testing.T)
	}{
		{"list", []string{"list", "--json"}, isolateNoConfig},
		{"search", []string{"search", "pdf", "--json"}, isolateNoConfig},
		{"get", []string{"get", "demo", "--json"}, isolateNoConfig},
		{"sync", []string{"sync", "--json"}, isolateNoConfig},
		{"add", []string{"add", "./source", "--json"}, isolateNoConfig},
		{"publish", []string{"publish", "./skill", "--json"}, isolateNoConfig},
		{"remove", []string{"remove", "demo", "--json"}, isolateNoConfig},
		{"discover", []string{"discover", "pdf", "--json"}, isolateFailingIndex},
		{"bootstrap", []string{"bootstrap", "--json"}, isolateNoGHAuth},
		{"update", []string{"update", "--json", "--version", "v9.9.9", "--bin", badBin}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			code, jsonOut, cobraOut, errOut := runCLI(t, tc.argv)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (argv %q)", code, tc.argv)
			}
			requireSingleJSONError(t, jsonOut)
			if strings.Contains(cobraOut, "Usage:") || strings.Contains(errOut, "Usage:") {
				t.Errorf("a --json failure must not dump usage:\nstdout:\n%s\nstderr:\n%s", cobraOut, errOut)
			}
			requireSingleErrorLine(t, errOut)
		})
	}
}

// TestRunHumanUsageErrorKeepsUsage pins the other half of the contract:
// genuine misuse still shows usage in human mode — only the duplicated
// Error line is gone.
func TestRunHumanUsageErrorKeepsUsage(t *testing.T) {
	saveJSONFlag(t)
	for _, argv := range [][]string{{"search"}, {"frobnicate"}} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, jsonOut, cobraOut, errOut := runCLI(t, argv)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (argv %q)", code, argv)
			}
			if jsonOut != "" {
				t.Errorf("human mode must print no JSON, got %q", jsonOut)
			}
			if !strings.Contains(cobraOut, "Usage:") {
				t.Errorf("misuse must still show usage:\nstdout:\n%s\nstderr:\n%s", cobraOut, errOut)
			}
			requireSingleErrorLine(t, errOut)
		})
	}
}

// TestWithJSONSilence pins the Args wrapper behind applyJSONArgsContract:
// validation failures silence cobra only when --json is on, successes
// never do, and the original error passes through untouched.
func TestWithJSONSilence(t *testing.T) {
	saveJSONFlag(t)
	wantErr := errors.New("nope")
	fail := func(*cobra.Command, []string) error { return wantErr }
	pass := func(*cobra.Command, []string) error { return nil }

	t.Run("failure with json silences", func(t *testing.T) {
		jsonout.SetEnabled(true)
		cmd := &cobra.Command{}
		if err := withJSONSilence(fail)(cmd, nil); !errors.Is(err, wantErr) {
			t.Fatalf("wrapped validator returned %v, want %v", err, wantErr)
		}
		if !cmd.SilenceUsage || !cmd.SilenceErrors {
			t.Errorf("SilenceUsage=%v SilenceErrors=%v, want both true",
				cmd.SilenceUsage, cmd.SilenceErrors)
		}
	})

	t.Run("failure without json untouched", func(t *testing.T) {
		jsonout.SetEnabled(false)
		cmd := &cobra.Command{}
		if err := withJSONSilence(fail)(cmd, nil); !errors.Is(err, wantErr) {
			t.Fatalf("wrapped validator returned %v, want %v", err, wantErr)
		}
		if cmd.SilenceUsage || cmd.SilenceErrors {
			t.Errorf("SilenceUsage=%v SilenceErrors=%v, want both false",
				cmd.SilenceUsage, cmd.SilenceErrors)
		}
	})

	t.Run("success with json untouched", func(t *testing.T) {
		jsonout.SetEnabled(true)
		cmd := &cobra.Command{}
		if err := withJSONSilence(pass)(cmd, nil); err != nil {
			t.Fatalf("wrapped validator returned %v, want nil", err)
		}
		if cmd.SilenceUsage || cmd.SilenceErrors {
			t.Errorf("SilenceUsage=%v SilenceErrors=%v, want both false",
				cmd.SilenceUsage, cmd.SilenceErrors)
		}
	})
}

// TestApplyJSONArgsContractWiring pins the wiring: validators that exist
// keep validating (the wrapper only adds the silence side effect), and
// commands without a validator are left alone.
func TestApplyJSONArgsContractWiring(t *testing.T) {
	root := newRootCmd()
	search, _, err := root.Find([]string{"search"})
	if err != nil {
		t.Fatalf("Find(search): %v", err)
	}
	if search.Args == nil {
		t.Fatal("search must keep an Args validator")
	}
	if err := search.Args(search, nil); err == nil {
		t.Error("search with no query must still fail validation")
	}
	if err := search.Args(search, []string{"pdf"}); err != nil {
		t.Errorf("search pdf should validate: %v", err)
	}
	list, _, err := root.Find([]string{"list"})
	if err != nil {
		t.Fatalf("Find(list): %v", err)
	}
	if list.Args != nil {
		t.Error("list has no Args validator; the contract must leave it nil")
	}
}

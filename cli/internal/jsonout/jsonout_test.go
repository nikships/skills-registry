package jsonout

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// captureStdout swaps the package's stdout writer with a buffer for the
// duration of the test, restoring the original on cleanup. This is the
// shared scaffolding used by Print / PrintError tests so we can read
// what would have been emitted to the real os.Stdout.
func captureStdout(t *testing.T) *bytes.Buffer {
	t.Helper()
	orig := stdout
	buf := &bytes.Buffer{}
	stdout = buf
	t.Cleanup(func() { stdout = orig })
	return buf
}

// resetEnabled forces Enabled() back to false for the duration of the
// test. The package state is otherwise shared across tests, so binding
// flags in one test would leak into another without this.
func resetEnabled(t *testing.T) {
	t.Helper()
	prev := enabled
	enabled = false
	t.Cleanup(func() { enabled = prev })
}

// TestPrintMarshalsValueAsCompactJSON verifies the happy path: a small
// struct round-trips through Print and back, ending in a single newline
// so consumers like `jq` see one line per emitted object.
func TestPrintMarshalsValueAsCompactJSON(t *testing.T) {
	buf := captureStdout(t)
	in := map[string]any{"slug": "demo", "name": "Demo"}
	if err := Print(in); err != nil {
		t.Fatalf("Print returned error: %v", err)
	}
	got := strings.TrimRight(buf.String(), "\n")
	var roundtrip map[string]any
	if err := json.Unmarshal([]byte(got), &roundtrip); err != nil {
		t.Fatalf("output is not valid JSON: %q (%v)", got, err)
	}
	if roundtrip["slug"] != "demo" || roundtrip["name"] != "Demo" {
		t.Fatalf("roundtrip mismatch: %v", roundtrip)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("Print should end output with a newline; got %q", buf.String())
	}
}

// TestPrintReturnsMarshalError exercises the error return: an
// unmarshallable value (a channel) must surface json.Marshal's error to
// the caller instead of swallowing it.
func TestPrintReturnsMarshalError(t *testing.T) {
	captureStdout(t)
	err := Print(make(chan int))
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}

// TestPrintErrorEmitsValidJSON verifies the structured error path: any
// error produces a single JSON object with the `error` field set to the
// error's text.
func TestPrintErrorEmitsValidJSON(t *testing.T) {
	buf := captureStdout(t)
	PrintError(errors.New("boom"))
	got := strings.TrimRight(buf.String(), "\n")
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("PrintError output is not valid JSON: %q (%v)", got, err)
	}
	if payload.Error != "boom" {
		t.Fatalf("payload.Error = %q, want %q", payload.Error, "boom")
	}
}

// TestPrintErrorWithNilUsesEmptyString covers the defensive nil branch:
// callers in an already-failed path may not always have a non-nil error
// to forward.
func TestPrintErrorWithNilUsesEmptyString(t *testing.T) {
	buf := captureStdout(t)
	PrintError(nil)
	got := strings.TrimRight(buf.String(), "\n")
	want := `{"error":""}`
	if got != want {
		t.Fatalf("PrintError(nil) = %q, want %q", got, want)
	}
}

// TestEnabledDefaultsFalse pins down the zero value: before BindFlag
// fires (or any test calls SetEnabled), Enabled() must return false so
// subcommands default to their interactive behavior.
func TestEnabledDefaultsFalse(t *testing.T) {
	resetEnabled(t)
	if Enabled() {
		t.Fatal("Enabled() defaulted to true; expected false")
	}
}

// TestSetEnabled exercises the test-hook setter. Production callers
// rely on BindFlag, but this is the contract the helpers expose for
// driving them in unit tests.
func TestSetEnabled(t *testing.T) {
	resetEnabled(t)
	SetEnabled(true)
	if !Enabled() {
		t.Fatal("SetEnabled(true) did not affect Enabled()")
	}
	SetEnabled(false)
	if Enabled() {
		t.Fatal("SetEnabled(false) did not affect Enabled()")
	}
}

// TestBindFlagOnRootAffectsSubcommand pins down the central inheritance
// contract: --json is bound on the root cobra command, but cobra's
// persistent-flag propagation makes it observable from any subcommand.
// This is what allows callers like `skill-registry list --json` to flip
// Enabled() to true without each subcommand re-declaring the flag.
func TestBindFlagOnRootAffectsSubcommand(t *testing.T) {
	resetEnabled(t)
	root := &cobra.Command{Use: "tester"}
	BindFlag(root)
	var sawEnabled bool
	sub := &cobra.Command{
		Use: "child",
		RunE: func(*cobra.Command, []string) error {
			sawEnabled = Enabled()
			return nil
		},
	}
	root.AddCommand(sub)
	root.SetArgs([]string{"child", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !sawEnabled {
		t.Fatal("Enabled() returned false inside subcommand even though --json was passed")
	}
}

// TestBindFlagAbsentFromCmdLineLeavesDisabled is the negative half of
// the inheritance test: a subcommand invocation without --json must
// leave Enabled() at false. Together with the positive test above,
// this guards against a future refactor that accidentally hard-codes
// enabled=true or flips the default.
func TestBindFlagAbsentFromCmdLineLeavesDisabled(t *testing.T) {
	resetEnabled(t)
	root := &cobra.Command{Use: "tester"}
	BindFlag(root)
	var sawEnabled bool
	sub := &cobra.Command{
		Use: "child",
		RunE: func(*cobra.Command, []string) error {
			sawEnabled = Enabled()
			return nil
		},
	}
	root.AddCommand(sub)
	root.SetArgs([]string{"child"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if sawEnabled {
		t.Fatal("Enabled() returned true without --json on argv")
	}
}

// TestBindFlagRegistersPersistentFlag is a structural check: after
// BindFlag returns, the root command's PersistentFlags set must
// actually contain --json. Without this, cobra would silently ignore
// the flag rather than propagating it to subcommands.
func TestBindFlagRegistersPersistentFlag(t *testing.T) {
	resetEnabled(t)
	root := &cobra.Command{Use: "tester"}
	BindFlag(root)
	if f := root.PersistentFlags().Lookup(FlagName); f == nil {
		t.Fatalf("BindFlag did not register persistent flag %q", FlagName)
	}
}

// TestSwapWriterRedirectsOutput verifies that SwapWriter is wired up
// correctly: after the swap, every Print / PrintError call lands in the
// supplied writer instead of os.Stdout, and the previous writer is
// returned so tests can restore the default on cleanup.
func TestSwapWriterRedirectsOutput(t *testing.T) {
	resetEnabled(t)
	target := &bytes.Buffer{}
	prev := SwapWriter(target)
	t.Cleanup(func() { SwapWriter(prev) })

	if err := Print(map[string]string{"slug": "demo"}); err != nil {
		t.Fatalf("Print: %v", err)
	}
	got := strings.TrimSpace(target.String())
	if got != `{"slug":"demo"}` {
		t.Fatalf("captured output = %q, want {\"slug\":\"demo\"}", got)
	}
}

// TestSwapWriterReturnsPriorWriter pins down the round-trip property:
// the value SwapWriter returns must match the writer that was active
// just before the call, so tests can chain `defer SwapWriter(prev)`
// patterns without losing the original os.Stdout.
func TestSwapWriterReturnsPriorWriter(t *testing.T) {
	first := &bytes.Buffer{}
	originalPrev := SwapWriter(first)
	t.Cleanup(func() { SwapWriter(originalPrev) })

	second := &bytes.Buffer{}
	gotPrev := SwapWriter(second)
	if gotPrev != first {
		t.Fatalf("SwapWriter did not return the prior writer (first=%p, got=%p)", first, gotPrev)
	}
	// Restore so the test cleanup sees a coherent stack.
	SwapWriter(first)
}

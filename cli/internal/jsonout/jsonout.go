// Package jsonout owns the persistent --json bool flag bound to the
// root cobra command and provides the small set of helpers subcommands
// use to emit structured output when that flag is set.
//
// Subcommands are expected to early-branch on jsonout.Enabled() and
// either:
//
//   - call jsonout.Print(struct{...}{...}) on success, or
//   - call jsonout.PrintError(err) and return a non-zero exit code on
//     failure.
//
// Wiring the flag at the root makes it inherited by every subcommand
// via cobra's PersistentFlags propagation, so a single BindFlag call
// is enough to make --json available everywhere.
package jsonout

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// FlagName is the persistent flag's long form. Exposed as a constant so
// tests, docs, and any future tooling can reference the canonical name
// without hard-coding the string.
const FlagName = "json"

// flagDescription is the help text printed for `--json` in cobra usage.
const flagDescription = "Emit machine-readable JSON to stdout and suppress all TUI/interactive output."

// enabled mirrors the value cobra parses out of --json. Stored at
// package scope so subcommands can read it via Enabled() without
// threading a dependency through every call site.
var enabled bool

// stdout is the sink for Print and PrintError. Kept as a package
// variable (rather than always writing to os.Stdout directly) so tests
// can substitute a buffer when verifying output.
var stdout io.Writer = os.Stdout

// BindFlag attaches the persistent --json flag to cmd. Call once on
// the root cobra command before subcommands run; cobra propagates
// persistent flags down to every subcommand at parse time, so a single
// binding makes --json universally available.
func BindFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&enabled, FlagName, false, flagDescription)
}

// Enabled reports whether --json was supplied on the command line.
// Subcommands should check this early and branch into a non-interactive
// code path (no TUI, no prompts, JSON-only output).
func Enabled() bool {
	return enabled
}

// SetEnabled overrides the parsed flag state. Tests use this to drive
// the helpers without spinning up a cobra command; production callers
// should rely on BindFlag + cobra flag parsing.
func SetEnabled(v bool) {
	enabled = v
}

// SwapWriter replaces the package-level stdout writer and returns the
// previous one so tests can capture emitted JSON and then restore the
// real os.Stdout on cleanup. Test-only; production callers must leave
// the default (os.Stdout) in place.
//
// We expose this on the public surface rather than reading
// `os.Stdout` dynamically inside Print/PrintError because the
// dynamic-lookup variant would mask bugs in subcommand wiring (a test
// could subvert capture by re-pointing os.Stdout mid-call without ever
// touching the helper).
func SwapWriter(w io.Writer) io.Writer {
	prev := stdout
	stdout = w
	return prev
}

// Print marshals v as compact JSON and writes the result to stdout
// followed by a single newline. Returns any marshal or write error so
// the caller can surface it through its normal error path.
func Print(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(buf))
	return err
}

// PrintError writes {"error": "..."} to stdout. The error text comes
// from err.Error(); an empty string is used when err is nil. Never
// returns an error: callers are already in a failure path and another
// layer of "write failed" plumbing would just obscure the real cause.
func PrintError(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	body, mErr := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: msg})
	if mErr != nil {
		// Marshalling a single string field shouldn't realistically
		// fail, but make sure the consumer still gets parseable JSON
		// if it ever does.
		fmt.Fprintln(stdout, `{"error":"failed to encode error message"}`)
		return
	}
	fmt.Fprintln(stdout, string(body))
}

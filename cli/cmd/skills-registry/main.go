// skills-registry — TUI manager for a GitHub-backed skill registry.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/jsonout"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes argv against a fresh command tree and returns the process exit
// code. Split from main so tests can drive the full parse → execute → report
// pipeline in-process. Cobra's usage and error streams go to stdout/stderr;
// --json envelopes go through jsonout's writer (os.Stdout in production,
// swapped out by tests), so they stay on stdout whatever cobra does.
func run(argv []string, stdout, stderr io.Writer) int {
	cleanupOldBinaries()
	root := newRootCmd()
	root.SetArgs(argv)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		// Usage/arg failures under --json never reach a RunE, so no
		// {"error"} envelope was printed yet; emit it here so the stdout
		// contract holds for every failure mode. Runtime --json failures
		// already printed theirs (see jsonout.PrintErrorHandled) and carry
		// the mark, so this never double-prints.
		if jsonout.Enabled() && !jsonout.AlreadyReported(err) {
			jsonout.PrintError(err)
		}
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

// newRootCmd assembles the cobra command tree. A bare `skills-registry`
// invocation (no subcommand) is dispatched via RunE → runRoot, which
// routes between the onboarding wizard, the dashboard hub, and a plain
// help dump based on (a) whether a registry is already configured and
// (b) whether stdout is attached to a terminal.
//
// Subcommands (list/get/sync/add/publish/bootstrap) are dispatched by
// cobra by name before RunE runs, so they bypass routing entirely.
// `--help` is intercepted by cobra before RunE as well, so it always
// shows usage regardless of first-run state.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "skills-registry",
		Short: "Manage a GitHub-backed personal skill registry",
		Long: `skills-registry is a TUI for your personal skill registry repository.

Running "skills-registry" with no subcommand drops you into the right place:
  - First-time users (no config yet)      → onboarding wizard
  - Returning users (config exists)       → dashboard hub
  - Non-interactive shells (stdout piped) → this usage text

Day-to-day, use:
  skills-registry list                     browse + durably install into agent dot-folders
  skills-registry search <query>           fuzzy-search your registry (top 10 matches; query is required)
  skills-registry discover <query>         search the public skill index for third-party skills to import
  skills-registry get <slug>               temporary fetch into ~/.cache/skills-registry/skills/<slug>/
  skills-registry sync                     push local skills missing from the registry
  skills-registry add <source>             clone a source, multi-select what to publish + install
  skills-registry publish <path>           publish a single local skill folder
  skills-registry remove <slug>            delete a skill from the registry + local copies
  skills-registry update                   self-update the installed CLI
  skills-registry bootstrap                explicit (re-)run of the bootstrap flow`,
		Version: version,
		Args:    cobra.NoArgs,
		RunE:    runRoot,
		// SilenceErrors quiets cobra's own "Error:" line for every failure,
		// including argument-count and unknown-command errors that return
		// before any RunE runs; run prints the error exactly once. Usage
		// still prints for those (SilenceUsage stays false) unless --json
		// silenced it via applyJSONArgsContract.
		SilenceErrors: true,
	}

	// Bind the persistent --json flag on the root so every subcommand
	// inherits it. Subcommands honor it via jsonout.Enabled() and emit
	// structured output instead of TUI/interactive prompts.
	jsonout.BindFlag(root)

	root.AddCommand(
		newBootstrapCmd(),
		newListCmd(),
		newSearchCmd(),
		newDiscoverCmd(),
		newGetCmd(),
		newSyncCmd(),
		newAddCmd(),
		newPublishCmd(),
		newRemoveCmd(),
		newUpdateCmd(),
	)
	applyJSONArgsContract(root)

	return root
}

// applyJSONArgsContract wraps the Args validators of root and every
// subcommand so usage-level failures (wrong arg count, unknown command)
// under --json suppress cobra's usage dump; run then emits the {"error"}
// envelope to stdout instead. Human invocations are untouched: validation
// still fails with the same error and cobra still prints usage. Flags are
// parsed before Args validation, so jsonout.Enabled() is reliable here for
// every path this covers (unknown commands included — root's NoArgs check
// is what reports them, after parsing).
func applyJSONArgsContract(root *cobra.Command) {
	if root.Args != nil {
		root.Args = withJSONSilence(root.Args)
	}
	for _, c := range root.Commands() {
		if c.Args != nil {
			c.Args = withJSONSilence(c.Args)
		}
	}
}

// withJSONSilence wraps an Args validator so a validation failure under
// --json silences cobra's usage and error output. ExecuteC consults the
// silence flags after execute() returns, so setting them here (before the
// error propagates) takes effect even though no RunE runs.
func withJSONSilence(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil && jsonout.Enabled() {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		}
		return err
	}
}

// runRoot is the bare-command handler. It only runs when no subcommand
// (and no help flag) was supplied.
func runRoot(cmd *cobra.Command, _ []string) error {
	// A malformed config or a failed TUI is not a misuse of the command, so
	// neither the usage block nor cobra's own error line belongs in the
	// output; main prints the error once. Root arg validation still shows
	// usage because it runs before RunE.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	_, loadErr := config.Load()
	switch bareRouteDecision(isTerminal(), jsonout.Enabled(), loadErr) {
	case bareRouteHelp:
		return cmd.Help()
	case bareRouteWizard:
		return runWizard(cmd.Context())
	case bareRouteHub:
		runAutoUpdate(cmd.Context(), os.Stderr)
		return runHub(cmd.Context())
	case bareRouteError:
		return loadErr
	}
	return nil
}

// bareRoute enumerates the four resolutions a bare `skills-registry`
// invocation can land on.
type bareRoute int

const (
	// bareRouteHelp prints the usage text without starting any TUI.
	// Triggered when stdout is not a terminal (e.g. piped, redirected,
	// or running under CI), so we can't render a Bubble Tea program.
	bareRouteHelp bareRoute = iota

	// bareRouteWizard launches the first-run onboarding wizard, used
	// when config.Load() returns ErrMissing.
	bareRouteWizard

	// bareRouteHub launches the dashboard for returning users, used
	// when config.Load() succeeds.
	bareRouteHub

	// bareRouteError surfaces a malformed-config error (anything other
	// than ErrMissing or nil) to the caller so the user can see what's
	// wrong with their registry.toml.
	bareRouteError
)

// bareRouteDecision is the pure decision function backing runRoot.
// Extracted so the routing matrix is unit-testable without touching the
// filesystem, network, or os.Stdout.
//
// The order matters: a non-TTY environment OR an explicit --json
// invocation short-circuits to help even when no config exists. In
// both cases we can't (or shouldn't) render a TUI — non-TTY because
// the terminal can't display it, --json because the caller has asked
// for machine-readable output. Help is the safest non-TUI default for
// F1.4; later milestones may swap in a JSON status payload when
// jsonMode is set.
func bareRouteDecision(isTTY bool, jsonMode bool, loadErr error) bareRoute {
	switch {
	case !isTTY || jsonMode:
		return bareRouteHelp
	case errors.Is(loadErr, config.ErrMissing):
		return bareRouteWizard
	case loadErr != nil:
		return bareRouteError
	default:
		return bareRouteHub
	}
}

func runAutoUpdate(ctx context.Context, stderr io.Writer) {
	if !autoUpdateEnabled() {
		return
	}
	fmt.Fprintln(stderr, "checking for skills-registry updates...")
	res, err := updateRunner(ctx, updateOpts{})
	if err != nil {
		fmt.Fprintf(stderr, "warning: auto-update failed: %v\n", err)
		return
	}
	if res.Updated {
		fmt.Fprintln(stderr, res.Message)
	}
}

func autoUpdateEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SKILLS_REGISTRY_AUTO_UPDATE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

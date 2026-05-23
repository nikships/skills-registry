// skill-registry — TUI manager for a GitHub-backed skill registry.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
)

var version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// newRootCmd assembles the cobra command tree. A bare `skill-registry`
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
		Use:   "skill-registry",
		Short: "Manage a GitHub-backed personal skill registry",
		Long: `skill-registry is a TUI for your personal skill registry repository.

Running "skill-registry" with no subcommand drops you into the right place:
  - First-time users (no config yet)      → onboarding wizard
  - Returning users (config exists)       → dashboard hub
  - Non-interactive shells (stdout piped) → this usage text

Day-to-day, use:
  skill-registry list                     fuzzy-filterable list of every skill
  skill-registry get <slug>               download a skill to ./.agents/skills/<slug>/
  skill-registry sync                     push local skills missing from the registry
  skill-registry add <source>             clone a source, multi-select what to publish
  skill-registry publish <path>           publish a single local skill folder
  skill-registry bootstrap                explicit (re-)run of the bootstrap flow`,
		Version: version,
		Args:    cobra.NoArgs,
		RunE:    runRoot,
	}

	root.AddCommand(
		newBootstrapCmd(),
		newListCmd(),
		newGetCmd(),
		newSyncCmd(),
		newAddCmd(),
		newPublishCmd(),
	)

	return root
}

// runRoot is the bare-command handler. It only runs when no subcommand
// (and no help flag) was supplied.
func runRoot(cmd *cobra.Command, _ []string) error {
	_, loadErr := config.Load()
	switch bareRouteDecision(isTerminal(), loadErr) {
	case bareRouteHelp:
		return cmd.Help()
	case bareRouteWizard:
		return runWizard(cmd.Context())
	case bareRouteHub:
		return runHub(cmd.Context())
	case bareRouteError:
		return loadErr
	}
	return nil
}

// bareRoute enumerates the four resolutions a bare `skill-registry`
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
// The order matters: a non-TTY environment short-circuits to help even
// when no config exists, because we can't render a TUI either way and
// help is a more useful default than dumping a half-broken wizard into
// a pipe.
func bareRouteDecision(isTTY bool, loadErr error) bareRoute {
	switch {
	case !isTTY:
		return bareRouteHelp
	case errors.Is(loadErr, config.ErrMissing):
		return bareRouteWizard
	case loadErr != nil:
		return bareRouteError
	default:
		return bareRouteHub
	}
}

// runWizard is the first-run onboarding entry point. F2.1 will replace
// this with a full Bubble Tea alt-screen wizard; until then we shell
// into the existing `bootstrap` flow so users without a config still
// land on a working setup path.
func runWizard(ctx context.Context) error {
	return runBootstrap(ctx, bootstrapOpts{})
}

// runHub is the returning-user dashboard entry point. F3.1 will replace
// this with a full Bubble Tea alt-screen hub; until then we shell into
// the existing `list` TUI so the bare command still does something
// useful when a registry is already configured.
func runHub(ctx context.Context) error {
	return runList(ctx, "", false)
}

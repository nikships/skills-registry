package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// runWizard launches the F2.1 onboarding wizard in alt-screen mode.
//
// F2.2 / F2.3 will plug per-step business logic (scan, repo create,
// push, agent install, MCP wire-up) directly into tui.WizardModel.
// Until then the wizard renders a glamorous frame and, when the user
// finishes the placeholder flow, falls through to runBootstrap so a
// fresh `skill-registry` invocation still ends with a working
// configuration. Cancelling the wizard short-circuits the fall-through
// — there's no point pushing a user through CLI prompts after they
// just dismissed the same flow's TUI front-end.
func runWizard(ctx context.Context) error {
	model := tui.NewWizard(ctx)
	out, err := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	).Run()
	if err != nil {
		return fmt.Errorf("run wizard: %w", err)
	}
	final, ok := out.(tui.WizardModel)
	if !ok {
		return fmt.Errorf("wizard returned unexpected model %T", out)
	}
	if final.Cancelled() {
		fmt.Println("Onboarding cancelled.")
		return nil
	}
	// F2.2 / F2.3 replace this with real in-wizard business logic.
	// Until then defer to the existing bootstrap flow so first-run
	// users still end up with a populated registry + agent SKILL.md.
	return runBootstrap(ctx, bootstrapOpts{})
}

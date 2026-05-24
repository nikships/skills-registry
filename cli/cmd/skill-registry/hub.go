package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// runHub launches the alt-screen dashboard for returning users.
//
// F3.1 owns the frame (sparkle header + responsive card grid + footer).
// F3.2 will branch on tui.HubModel.Selection() and re-launch the hub
// after each card action. Until then a selected card prints a one-line
// "wiring lands in F3.2" notice so the user gets immediate feedback and
// the launcher exit path stays exercised.
func runHub(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	loader := hubCountLoader(cfg.Repo, cfg.DefaultBranch)
	model := tui.NewHub(ctx, cfg.Repo, loader)
	out, err := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	).Run()
	if err != nil {
		return fmt.Errorf("run hub: %w", err)
	}
	final, ok := out.(tui.HubModel)
	if !ok {
		return fmt.Errorf("hub returned unexpected model %T", out)
	}
	return finishHub(final)
}

// hubCountLoader returns a closure that lists the registry and reports the
// skill count back to the hub model. Constructed once per launch — the
// registry client is cheap to build but we'd rather not allocate one per
// async tick.
func hubCountLoader(repo, branch string) tui.HubCountLoader {
	return func(ctx context.Context) (int, error) {
		client, err := registry.New(repo, branch)
		if err != nil {
			return 0, err
		}
		summaries, err := client.List(ctx)
		if err != nil {
			return 0, err
		}
		return len(summaries), nil
	}
}

// finishHub handles the post-quit handoff. Quit-without-selection exits
// silently; a selection prints a one-line placeholder until F3.2 lands
// the real per-action wiring.
func finishHub(final tui.HubModel) error {
	if final.Quit() {
		return nil
	}
	sel := final.Selection()
	if sel == "" {
		return nil
	}
	fmt.Printf("Selected: %s (wiring lands in F3.2)\n", sel)
	return nil
}

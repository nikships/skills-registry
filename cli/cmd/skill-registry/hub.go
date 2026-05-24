package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// runHub launches one long-lived alt-screen Bubble Tea program. Every hub
// action runs as an embedded flow so the terminal never drops back to
// scrollback between actions.
func runHub(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	program := tui.NewHubProgram(ctx, buildHubDeps(ctx, cfg))
	if _, err := tea.NewProgram(
		program,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	).Run(); err != nil {
		return fmt.Errorf("run hub: %w", err)
	}
	return nil
}

// hubToast is kept as the compact toast shape used by older unit tests and by
// errToast. The embedded HubProgram now carries flow results via tui.flowExitMsg.
type hubToast struct {
	text  string
	ok    bool
	fatal error
}

// settingsSaver returns the SettingsSaver closure wired to config.Save.
// Kept out of NewSettings so the TUI package stays decoupled from
// internal/config.
func settingsSaver() tui.SettingsSaver {
	return func(repo, branch string) (string, error) {
		return config.Save(config.Config{Repo: repo, DefaultBranch: branch})
	}
}

// errToast formats an action failure as a one-line red toast. A
// context.Canceled (e.g. user hit ctrl+c inside a sub-action) is demoted to a
// neutral "cancelled" caption so the dashboard doesn't scream about a clean
// user-initiated exit.
func errToast(action string, err error) hubToast {
	if errors.Is(err, context.Canceled) {
		return hubToast{text: fmt.Sprintf("%s · cancelled", action), ok: true}
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " · ")
	return hubToast{text: fmt.Sprintf("✗ %s: %s", action, msg), ok: false}
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

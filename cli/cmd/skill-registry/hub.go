package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anand-92/skills-registry/cli/internal/cache"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// runHub is the F3.2 dispatch loop: launch the alt-screen dashboard,
// run the action the user picks, capture the result as a toast, and
// re-launch with that toast seeded into the next frame. The loop
// terminates only when the user explicitly quits the hub (q / esc /
// ctrl+c) or a launcher-level error makes continuing impossible.
func runHub(ctx context.Context) error {
	var pending hubToast
	for {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		model := tui.NewHub(ctx, cfg.Repo, hubCountLoader(cfg.Repo, cfg.DefaultBranch))
		if pending.text != "" {
			model = model.WithToast(pending.text, pending.ok)
		}
		final, err := launchHubProgram(ctx, model)
		if err != nil {
			return err
		}
		if final.Quit() {
			return nil
		}
		action := final.Selection()
		if action == "" {
			return nil
		}
		pending = dispatchHubAction(ctx, action)
		if pending.fatal != nil {
			return pending.fatal
		}
	}
}

// hubToast carries one action's result back into the next hub frame.
// `fatal` short-circuits the loop when continuing would be pointless
// (e.g. the registry config was deleted mid-session); per-action
// failures land as red toasts instead so the user can retry.
type hubToast struct {
	text  string
	ok    bool
	fatal error
}

// launchHubProgram runs a single iteration of the alt-screen hub and
// returns the post-quit model so the caller can read Selection() / Quit().
func launchHubProgram(ctx context.Context, model tui.HubModel) (tui.HubModel, error) {
	out, err := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	).Run()
	if err != nil {
		return tui.HubModel{}, fmt.Errorf("run hub: %w", err)
	}
	final, ok := out.(tui.HubModel)
	if !ok {
		return tui.HubModel{}, fmt.Errorf("hub returned unexpected model %T", out)
	}
	return final, nil
}

// dispatchHubAction is the per-action switch. Each branch runs the
// corresponding subcommand handler inline (the hub's alt-screen has
// already been released, so prompts and progress prints behave like a
// normal terminal session) and returns the toast the next iteration
// should display.
func dispatchHubAction(ctx context.Context, action string) hubToast {
	switch action {
	case tui.HubActionBrowse:
		return runBrowseFromHub(ctx)
	case tui.HubActionSync:
		return runSyncFromHub(ctx)
	case tui.HubActionAdd:
		return runAddFromHub(ctx)
	case tui.HubActionPublish:
		return runPublishFromHub(ctx)
	case tui.HubActionRemove:
		return runRemoveFromHub(ctx)
	case tui.HubActionSettings:
		return runSettingsFromHub(ctx)
	}
	return hubToast{text: fmt.Sprintf("✗ unknown action: %s", action), ok: false}
}

// runSettingsFromHub launches the F3.3 Settings alt-screen sub-TUI.
// The model owns its own alt-screen lifecycle (matching the
// Browse/list pattern from runBrowseFromHub), so the hub's alt-screen
// is released cleanly before this one starts. On exit, the toast
// surfaces either the saved-at path or any error the user encountered.
func runSettingsFromHub(ctx context.Context) hubToast {
	cfg, err := config.Load()
	if err != nil {
		return errToast("settings", err)
	}
	mcpBin, _ := locateMCPBinary()
	model := tui.NewSettings(
		cfg.Repo, cfg.DefaultBranch,
		cache.CacheRoot(),
		mcpBin,
		settingsSaver(),
	)
	out, err := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	).Run()
	if err != nil {
		return errToast("settings", err)
	}
	final, ok := out.(tui.SettingsModel)
	if !ok {
		return errToast("settings", fmt.Errorf("settings returned unexpected model %T", out))
	}
	if err := final.SaveError(); err != nil {
		return errToast("settings", err)
	}
	if final.SavedPath() != "" {
		return hubToast{text: fmt.Sprintf("✓ settings saved → %s", final.SavedPath()), ok: true}
	}
	return hubToast{text: "settings · closed", ok: true}
}

// settingsSaver returns the SettingsSaver closure wired to config.Save.
// Kept out of NewSettings so the TUI package stays decoupled from
// internal/config.
func settingsSaver() tui.SettingsSaver {
	return func(repo, branch string) (string, error) {
		return config.Save(config.Config{Repo: repo, DefaultBranch: branch})
	}
}

// runBrowseFromHub launches the existing list TUI as its own alt-screen
// program. The list owns its own quit handling — pressing q in the list
// just returns here, where we surface a neutral "closed" toast.
func runBrowseFromHub(ctx context.Context) hubToast {
	if err := runList(ctx, "", false); err != nil {
		return errToast("browse", err)
	}
	return hubToast{text: "✓ browse · closed", ok: true}
}

// runSyncFromHub runs the sync subcommand's interactive flow. The
// multi-select + confirm prompts inside runSync are tea.Programs without
// alt-screen, so they render as a brief inline UI between hub sessions.
func runSyncFromHub(ctx context.Context) hubToast {
	if err := runSync(ctx, false, false); err != nil {
		return errToast("sync", err)
	}
	return hubToast{text: "✓ sync complete", ok: true}
}

// runAddFromHub prompts for the source (path / owner/repo / git URL),
// then delegates to runAdd. A cancelled prompt collapses to a neutral
// toast so the user lands back on the hub without an error.
func runAddFromHub(ctx context.Context) hubToast {
	source, cancelled, err := promptHubLine(
		"Add skills from a source",
		"owner/repo, git URL, or local path",
		"esc to cancel · enter to continue",
	)
	if err != nil {
		return errToast("add", err)
	}
	if cancelled || source == "" {
		return hubToast{text: "add · cancelled", ok: true}
	}
	if err := runAdd(ctx, source, false, false); err != nil {
		return errToast("add", err)
	}
	return hubToast{text: fmt.Sprintf("✓ added from %s", source), ok: true}
}

// runRemoveFromHub prompts for the slug then delegates to runRemove.
// The interactive confirmation lives inside runRemove (yes=false,
// quietMode=false), so we don't need a second prompt here. A
// user-initiated abort at the confirm step lands as a neutral
// "cancelled" toast rather than an error.
func runRemoveFromHub(ctx context.Context) hubToast {
	slug, cancelled, err := promptHubLine(
		"Remove a skill",
		"slug to delete (e.g. code-review)",
		"esc to cancel · enter to continue",
	)
	if err != nil {
		return errToast("remove", err)
	}
	if cancelled || slug == "" {
		return hubToast{text: "remove · cancelled", ok: true}
	}
	report, err := runRemove(ctx, slug, false, false)
	if err != nil {
		return errToast("remove", err)
	}
	if report == nil {
		return hubToast{text: "remove · cancelled", ok: true}
	}
	return hubToast{
		text: fmt.Sprintf("✓ removed %s", removeSummaryLine(*report)),
		ok:   true,
	}
}

// runPublishFromHub prompts for the local skill folder path then
// delegates to runPublish. Mirrors runAddFromHub's cancellation rules.
func runPublishFromHub(ctx context.Context) hubToast {
	path, cancelled, err := promptHubLine(
		"Publish a local skill",
		"path to skill folder (contains SKILL.md)",
		"esc to cancel · enter to publish",
	)
	if err != nil {
		return errToast("publish", err)
	}
	if cancelled || path == "" {
		return hubToast{text: "publish · cancelled", ok: true}
	}
	if err := runPublish(ctx, path, ""); err != nil {
		return errToast("publish", err)
	}
	return hubToast{text: fmt.Sprintf("✓ published %s", path), ok: true}
}

// promptHubLine runs a one-shot tui.InputModel outside any alt-screen
// so the prompt appears inline between hub sessions. Returns the
// trimmed value, a `cancelled` flag (esc / ctrl+c), or an error from
// the bubble tea program itself.
func promptHubLine(title, placeholder, help string) (string, bool, error) {
	model := tui.NewInput(title, "", placeholder, "")
	model.Help = help
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return "", false, err
	}
	final, ok := out.(tui.InputModel)
	if !ok {
		return "", false, fmt.Errorf("input returned unexpected model %T", out)
	}
	if final.Cancelled() {
		return "", true, nil
	}
	return strings.TrimSpace(final.Value()), false, nil
}

// errToast formats an action failure as a one-line red toast. A
// context.Canceled (e.g. user hit ctrl+c inside the sub-action) is
// demoted to a neutral "cancelled" caption so the dashboard doesn't
// scream about a clean user-initiated exit. Multi-line errors get
// flattened to a bullet-separated row so the toast never wraps and
// pushes the footer off-screen.
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

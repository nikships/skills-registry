package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/bootstrap"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// runWizard launches the onboarding wizard in alt-screen mode. The
// wizard owns the entire bootstrap flow end-to-end (scan → repo →
// push → agents → cleanup → MCP snippet → done). The legacy
// `bootstrap` subcommand is still available for headless / scripted
// invocations.
func runWizard(ctx context.Context) error {
	gh, err := registry.FindGH()
	if err != nil {
		return err
	}
	if err := registry.EnsureAuthed(ctx, gh); err != nil {
		return err
	}
	// Fail-fast before the wizard renders if the bulk push won't be
	// possible. The Go bootstrap path uses a single `git push` to avoid
	// GitHub's secondary rate limit; without `git` we'd lose 30+ steps
	// of context to a late failure.
	if err := requireGitForBootstrap(); err != nil {
		return err
	}

	deps := buildWizardDeps(gh)
	model := tui.NewWizard(ctx).WithDeps(deps)
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
	return finishWizard(final)
}

// finishWizard handles the post-wizard hand-off. Cancelled runs exit
// cleanly; completed runs print a short success caption. The hub launcher
// (F3.x) reads `Completed()` to decide whether to open the dashboard;
// until then we just close the alt-screen and let the user re-invoke
// `skills-registry` if they want the hub.
func finishWizard(final tui.WizardModel) error {
	if final.Cancelled() {
		fmt.Println("Onboarding cancelled.")
		return nil
	}
	if !final.Completed() {
		// Defensive — shouldn't happen because the only non-cancel exit
		// is Done step's enter, which sets Completed()=true.
		return nil
	}
	fmt.Printf("\n%s onboarding complete — your registry %s is live.\n",
		tui.OkStyle.Render("✓"), tui.TitleStyle.Render(final.Repo()))
	fmt.Printf("  · %d skill(s) pushed · %d agent folder(s) installed\n",
		final.Pushed(), final.AgentsInstalled())
	fmt.Println("\nRun `skills-registry` any time to open the hub.")
	return nil
}

// buildWizardDeps wires the real scan / create-repo / save-config /
// push / agent-install / cleanup / MCP-install callbacks. Each closure
// captures the resolved `gh` path and the caller's home + cwd so the
// wizard model doesn't need to know about any of that. The wizard's own
// ctx is threaded into each callback via the `c` parameter at call time.
func buildWizardDeps(gh string) tui.WizardDeps {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	dotDirs := dotDirsFromAgents()
	return tui.WizardDeps{
		Scan: func(_ context.Context) ([]scan.Skill, error) {
			sources := scan.DiscoverSources(home, cwd, nil, dotDirs)
			return scan.Discover(sources)
		},
		CreateRepo: func(c context.Context, name, visibility string) (string, error) {
			return wizardCreateRepo(c, gh, name, visibility)
		},
		SaveConfig: func(repo string) error {
			_, err := config.Save(config.Config{Repo: repo, DefaultBranch: "main"})
			return err
		},
		Push: func(c context.Context, repo string, skills []scan.Skill,
			onProgress func(done, total int), onStatus func(msg string)) (int, error) {
			return wizardPushSkills(c, gh, repo, skills, onProgress, onStatus)
		},
		AgentChoices: wizardAgentChoices,
		InstallAgents: func(_ context.Context, repo string, picked []any) ([]string, error) {
			return wizardInstallAgents(home, cwd, repo, picked)
		},
		LoadCleanup: func(c context.Context, _ string, skills []scan.Skill) []tui.WizardCleanupEntry {
			return wizardLoadCleanup(c, gh, home, cwd, dotDirs, skills)
		},
		DeleteCleanup: wizardDeleteCleanup,
		MCPSnippet:    bootstrap.MCPJSONSnippet,
	}
}

// wizardCreateRepo resolves the owner from the authenticated `gh` session
// and creates the repo (or reuses an existing one with the same name).
// Returns "owner/name" or an error. Mirrors the create-or-reuse path in
// runBootstrap so a half-completed onboarding can be safely re-run.
func wizardCreateRepo(ctx context.Context, gh, name, visibility string) (string, error) {
	owner, err := lookupGitHubOwner(ctx, gh)
	if err != nil {
		return "", err
	}
	full := name
	if !strings.Contains(name, "/") {
		full = owner + "/" + name
	}
	probe, err := registry.New(full, "main")
	if err != nil {
		return "", err
	}
	probe.GH = gh
	if exists, _ := probe.Exists(ctx); exists {
		// Reuse the existing repo. The follow-up push will fill in any
		// missing skills.
		return full, nil
	}
	description := "Personal skill registry — managed via skills-registry."
	created, err := probe.CreateRepo(ctx, name, visibility, description)
	if err != nil {
		// `gh repo create` says "already exists" when the owner has
		// previously created the same name; treat that as reuse.
		if strings.Contains(err.Error(), "already exists") {
			return full, nil
		}
		return "", err
	}
	if created == "" {
		created = full
	}
	return created, nil
}

// wizardPushSkills computes the delta against the remote registry,
// materializes every file, and runs PushTreeViaGit with the supplied
// progress / status callbacks plugged in. Returns the number of skills
// uploaded.
func wizardPushSkills(ctx context.Context, gh, repo string, skills []scan.Skill,
	onProgress func(done, total int), onStatus func(msg string)) (int, error) {
	client, err := registry.New(repo, "main")
	if err != nil {
		return 0, err
	}
	client.GH = gh
	missing, err := wizardPushMissing(ctx, client, skills, onStatus)
	if err != nil {
		return 0, err
	}
	if len(missing) == 0 {
		return 0, nil
	}
	files, err := wizardCollectFiles(missing)
	if err != nil {
		return 0, err
	}
	if onStatus != nil {
		onStatus(fmt.Sprintf("uploading %d skill(s) (%d files)…", len(missing), len(files)))
	}
	client.OnProgress = onProgress
	client.OnStatus = onStatus
	defer func() {
		client.OnProgress = nil
		client.OnStatus = nil
	}()
	commit := fmt.Sprintf("init: import %d skill(s)", len(missing))
	if err := client.PushTreeViaGit(ctx, files, commit); err != nil {
		return 0, err
	}
	return len(missing), nil
}

// wizardPushMissing returns the subset of `skills` that isn't already in
// the registry. Surfaces an "already in sync" status when the local set
// matches what's on GitHub.
func wizardPushMissing(ctx context.Context, client *registry.Client, skills []scan.Skill,
	onStatus func(msg string)) ([]scan.Skill, error) {
	existing, err := client.Slugs(ctx)
	if err != nil {
		// Brand-new repo with no commits yet returns a 404 or 409;
		// treat those as an empty registry rather than failing the push.
		// Any other error (auth, network) should propagate so the user
		// sees a meaningful message instead of silent data loss.
		errMsg := err.Error()
		if strings.Contains(errMsg, "404") || strings.Contains(errMsg, "409") {
			existing = map[string]struct{}{}
		} else {
			return nil, fmt.Errorf("list registry slugs: %w", err)
		}
	}
	missing := scan.DedupeAgainst(skills, existing)
	if len(missing) == 0 && onStatus != nil {
		onStatus("registry already in sync — nothing to upload.")
	}
	return missing, nil
}

// wizardCollectFiles materializes every file under each skill folder into
// a `<slug>/<rel>` keyed map, the format PushTreeViaGit expects.
func wizardCollectFiles(skills []scan.Skill) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, sk := range skills {
		if err := walkSkillIntoFiles(sk, files); err != nil {
			return nil, fmt.Errorf("read %s: %w", sk.Slug, err)
		}
	}
	return files, nil
}

// wizardAgentChoices is the live dep for the wizard's agent multi-select.
// It mirrors the (formerly inline) selectAgents bootstrap helper: every
// known agent target becomes a row, with Universal agents locked at the
// top and a small "popular" set default-checked.
func wizardAgentChoices() []tui.WizardAgent {
	all := agents.All()
	defaults := map[string]struct{}{
		"Claude Code": {},
		"Factory":     {},
		"Cursor":      {},
		"Codex CLI":   {},
	}
	out := make([]tui.WizardAgent, 0, len(all))
	for _, t := range all {
		_, def := defaults[t.Display]
		out = append(out, tui.WizardAgent{
			Display: t.Display,
			Hint:    t.DotDir + "/skills",
			Locked:  t.Universal,
			Default: def,
			Value:   t,
		})
	}
	return out
}

// wizardInstallAgents converts the wizard's opaque `picked` values back
// into the agents.Target type bootstrap.InstallSkillMd expects.
func wizardInstallAgents(home, cwd, repo string, picked []any) ([]string, error) {
	targets := make([]agents.Target, 0, len(picked))
	for _, v := range picked {
		t, ok := v.(agents.Target)
		if !ok {
			return nil, fmt.Errorf("wizard agent value %T is not agents.Target", v)
		}
		targets = append(targets, t)
	}
	return bootstrap.InstallSkillMd(home, cwd, repo, targets)
}

// wizardLoadCleanup runs the same scan.EntriesForCleanup the legacy
// promptDeleteLocal flow uses. We rebuild the source list (cheap; the
// scan is cached at the OS level) so the wizard model doesn't need to
// thread sources through every step.
func wizardLoadCleanup(ctx context.Context, gh, home, cwd string, dotDirs []string,
	skills []scan.Skill) []tui.WizardCleanupEntry {
	sources := scan.DiscoverSources(home, cwd, nil, dotDirs)
	slugs := wizardRegistrySlugs(ctx, gh, skills)
	entries := scan.EntriesForCleanup(sources, slugs)
	out := make([]tui.WizardCleanupEntry, 0, len(entries))
	for _, en := range entries {
		out = append(out, tui.WizardCleanupEntry{
			Path:      en.Path,
			Source:    en.Source,
			IsSymlink: en.IsSymlink,
		})
	}
	return out
}

// wizardRegistrySlugs returns the set of slug names currently in the
// registry. Falls back to the locally-pushed slugs when the registry
// listing fails, matching the bootstrap CLI's recovery path.
func wizardRegistrySlugs(ctx context.Context, gh string, skills []scan.Skill) map[string]struct{} {
	cfg, err := config.Load()
	if err == nil && cfg.Repo != "" {
		client, cerr := registry.New(cfg.Repo, cfg.DefaultBranch)
		if cerr == nil {
			client.GH = gh
			if slugs, lerr := client.Slugs(ctx); lerr == nil {
				return slugs
			}
		}
	}
	// Fallback: trust the local push set so the wizard still surfaces
	// cleanup candidates even when the registry listing is unavailable.
	out := map[string]struct{}{}
	for _, s := range skills {
		out[s.Slug] = struct{}{}
	}
	return out
}

// wizardDeleteCleanup is the live dep for step 6's delete goroutine.
// Symlinks and real directories use the same os.RemoveAll call — both
// behave correctly (no follow + recursive removal, respectively).
func wizardDeleteCleanup(entries []tui.WizardCleanupEntry) (int, int) {
	var deleted, failed int
	for _, en := range entries {
		if err := os.RemoveAll(en.Path); err != nil {
			failed++
			continue
		}
		deleted++
	}
	return deleted, failed
}

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/bootstrap"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

func newBootstrapCmd() *cobra.Command {
	var (
		repoFlag    string
		visFlag     string
		noAgents    bool
		nonInteractive bool
	)
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create the registry repo, push local skills, and install agent docs",
		Long: `Run by "skills-registry init" — but safe to re-run.

If a registry config already exists, the repo-creation step is skipped and
you go straight to the agent multi-select.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBootstrap(cmd.Context(), bootstrapOpts{
				Repo:            repoFlag,
				Visibility:      visFlag,
				NoAgents:        noAgents,
				NonInteractive:  nonInteractive,
			})
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Skip the repo-name prompt and use this slug (owner/name).")
	cmd.Flags().StringVar(&visFlag, "visibility", "", "Skip the visibility prompt (public|private).")
	cmd.Flags().BoolVar(&noAgents, "no-agents", false, "Don't install the SKILL.md into any agent dot-folders.")
	cmd.Flags().BoolVar(&nonInteractive, "yes", false, "Accept defaults; useful for scripting.")
	return cmd
}

type bootstrapOpts struct {
	Repo           string
	Visibility     string
	NoAgents       bool
	NonInteractive bool
}

func runBootstrap(ctx context.Context, opts bootstrapOpts) error {
	gh, err := registry.FindGH()
	if err != nil {
		return err
	}
	if err := registry.EnsureAuthed(ctx, gh); err != nil {
		return err
	}

	cfg, _ := config.Load()
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	// 1. Scan local skills
	dotDirs := dotDirsFromAgents()
	sources := scan.DiscoverSources(home, cwd, nil, dotDirs)
	localSkills, err := scan.Discover(sources)
	if err != nil {
		return fmt.Errorf("scan local skills: %w", err)
	}
	fmt.Println(tui.TitleStyle.Render("skill-registry — bootstrap"))
	fmt.Printf("\nFound %s local skill(s) in %d source folder(s).\n",
		tui.OkStyle.Render(fmt.Sprintf("%d", len(localSkills))), len(sources))
	for _, s := range sources {
		fmt.Printf("  · %s\n", s.Label)
	}

	// 2. Create / reuse repo
	if cfg.Repo == "" {
		repo, branch, err := promptAndCreateRepo(ctx, gh, opts, localSkills)
		if err != nil {
			return err
		}
		cfg = config.Config{Repo: repo, DefaultBranch: branch}
		if _, err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("\n%s saved registry config: %s\n", tui.OkStyle.Render("✓"), tui.TitleStyle.Render(repo))
		fmt.Printf("  %s %s\n", tui.HintStyle.Render("→"), repoURL(repo))
	} else {
		fmt.Printf("\n%s reusing existing registry: %s\n", tui.OkStyle.Render("✓"), tui.TitleStyle.Render(cfg.Repo))
		fmt.Printf("  %s %s\n", tui.HintStyle.Render("→"), repoURL(cfg.Repo))
	}

	// 3. Push local skills to the registry (only on first run)
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}
	client.GH = gh
	pushedCount, err := pushLocalSkills(ctx, client, localSkills)
	if err != nil {
		return fmt.Errorf("push local skills: %w", err)
	}
	if pushedCount > 0 {
		fmt.Printf("%s pushed %d skill(s) to %s.\n", tui.OkStyle.Render("✓"), pushedCount, cfg.Repo)
	} else {
		fmt.Printf("\n%s no new skills to push.\n", tui.OkStyle.Render("·"))
	}

	// 4. Multi-select agent install targets
	if opts.NoAgents {
		fmt.Println("\nSkipping agent install (--no-agents).")
	} else {
		picked, err := selectAgents(opts.NonInteractive)
		if err != nil {
			return err
		}
		paths, err := bootstrap.InstallSkillMd(home, cwd, cfg.Repo, picked)
		if err != nil {
			return err
		}
		fmt.Printf("\n%s installed skill-registry/SKILL.md into %d agent folder(s):\n",
			tui.OkStyle.Render("✓"), len(paths))
		for _, p := range paths {
			fmt.Println("  ·", p)
		}
	}

	// 5. Offer to delete the now-redundant local skill folders.
	if !opts.NonInteractive {
		if err := promptDeleteLocal(localSkills); err != nil {
			return err
		}
	}

	// 6. Print MCP JSON snippet
	mcpBin, _ := locateMCPBinary()
	fmt.Println("\n" + tui.TitleStyle.Render("Wire it up:"))
	fmt.Println()
	fmt.Println(tui.SubtitleStyle.Render("Claude Code / Claude Desktop / Cursor / VS Code (mcp.json):"))
	fmt.Println(bootstrap.MCPJSONSnippet(mcpBin))
	fmt.Println()
	fmt.Println(tui.SubtitleStyle.Render("Codex (~/.codex/config.toml):"))
	fmt.Println(bootstrap.CodexTOMLSnippet(mcpBin))

	fmt.Println()
	fmt.Printf("%s Your registry is live: %s\n",
		tui.OkStyle.Render("✓"), repoURL(cfg.Repo))
	fmt.Println("\nDone.")
	return nil
}

// repoURL returns the canonical https URL for a repo slug like "owner/name".
func repoURL(repo string) string {
	return "https://github.com/" + repo
}

// promptDeleteLocal offers the user a y/n to delete the local skill folders
// that were just imported into the registry. Local skills are now dead weight
// — every agent that scans dot-folders has to read and parse them on every
// session, which bloats context windows and degrades performance.
func promptDeleteLocal(localSkills []scan.Skill) error {
	if len(localSkills) == 0 {
		return nil
	}
	// Group skills by source folder for the summary.
	bySource := map[string]int{}
	for _, s := range localSkills {
		bySource[s.Source]++
	}
	sources := make([]string, 0, len(bySource))
	for src := range bySource {
		sources = append(sources, src)
	}
	sort.Strings(sources)

	fmt.Println()
	fmt.Println(tui.TitleStyle.Render("Clean up local copies?"))
	fmt.Println(tui.HintStyle.Render(
		"Now that your skills live in the registry, the local copies are dead weight."))
	fmt.Println(tui.HintStyle.Render(
		"Every coding agent re-reads them on each session, bloating context and degrading performance."))
	fmt.Println()
	for _, src := range sources {
		fmt.Printf("  · %s (%d skill(s))\n", src, bySource[src])
	}
	fmt.Println()

	choices := []tui.Choice{
		{Value: "yes", Label: "Yes, delete", Hint: "Recommended — cleaner context"},
		{Value: "no", Label: "No, keep them", Hint: "I'll handle it manually"},
	}
	model := tui.NewChoice("Delete local skill folders?",
		fmt.Sprintf("This removes %d skill folder(s) from disk. Nothing in the registry is touched.", len(localSkills)),
		choices)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return err
	}
	final := out.(tui.ChoiceModel)
	// Cancel (Esc) and explicit "no" both mean keep.
	if final.Cancelled() || final.Value() == nil || final.Value().(string) != "yes" {
		fmt.Printf("\n%s kept local skills in place.\n", tui.HintStyle.Render("·"))
		return nil
	}

	var deleted, failed int
	for _, s := range localSkills {
		if err := os.RemoveAll(s.Folder); err != nil {
			fmt.Printf("  %s could not remove %s: %v\n",
				tui.ErrorStyle.Render("!"), s.Folder, err)
			failed++
			continue
		}
		deleted++
	}
	fmt.Printf("\n%s removed %d local skill folder(s).", tui.OkStyle.Render("✓"), deleted)
	if failed > 0 {
		fmt.Printf(" (%d failed)", failed)
	}
	fmt.Println()
	return nil
}

// selectAgents returns the agent targets the user wants to install into.
func selectAgents(nonInteractive bool) ([]agents.Target, error) {
	all := agents.All()
	if nonInteractive {
		var locked []agents.Target
		for _, t := range all {
			if t.Universal {
				locked = append(locked, t)
			}
		}
		return locked, nil
	}
	items := make([]tui.MultiSelectItem, 0, len(all))
	for _, t := range all {
		items = append(items, tui.MultiSelectItem{
			Value:  t,
			Label:  t.Display,
			Hint:   t.DotDir + "/skills",
			Locked: t.Universal,
		})
	}
	// Default-check a few common agents.
	defaults := map[string]struct{}{
		"Claude Code": {},
		"Factory":     {},
		"Cursor":      {},
		"Codex CLI":   {},
	}
	var defaultValues []any
	for _, t := range all {
		if _, ok := defaults[t.Display]; ok {
			defaultValues = append(defaultValues, t)
		}
	}
	model := tui.NewMultiSelect("Install skill-registry SKILL.md into which agents?", items, defaultValues, false)
	program := tea.NewProgram(model)
	result, err := program.Run()
	if err != nil {
		return nil, err
	}
	final := result.(tui.MultiSelectModel)
	if final.Cancelled() {
		return nil, fmt.Errorf("cancelled")
	}
	var out []agents.Target
	for _, v := range final.SelectedValues() {
		out = append(out, v.(agents.Target))
	}
	return out, nil
}

func dotDirsFromAgents() []string {
	all := agents.All()
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, t.DotDir)
	}
	return out
}

func locateMCPBinary() (string, error) {
	// The init script installs skill-registry-mcp via `uv tool install`; it
	// ends up at ~/.local/bin/skill-registry-mcp on most setups.
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "skill-registry-mcp"),
		"/opt/homebrew/bin/skill-registry-mcp",
		"/usr/local/bin/skill-registry-mcp",
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "skill-registry-mcp", fmt.Errorf("skill-registry-mcp not found on disk; using PATH lookup")
}

func promptAndCreateRepo(ctx context.Context, gh string, opts bootstrapOpts, localSkills []scan.Skill) (string, string, error) {
	repoSlug := strings.TrimSpace(opts.Repo)
	if repoSlug == "" {
		nameModel := tui.NewInput(
			"Registry repo name",
			"What should we name your skill registry repo on GitHub?",
			"skill-registry",
			"skill-registry",
		)
		nameModel.Help = "Enter just the name (no `owner/` prefix). Created on your authenticated user account."
		out, err := tea.NewProgram(nameModel).Run()
		if err != nil {
			return "", "", err
		}
		final := out.(tui.InputModel)
		if final.Cancelled() {
			return "", "", fmt.Errorf("cancelled")
		}
		name := final.Value()
		if name == "" {
			name = "skill-registry"
		}
		repoSlug = name
	}

	visibility := opts.Visibility
	if visibility == "" {
		choices := []tui.Choice{
			{Value: "private", Label: "Private", Hint: "Only you can see and clone (recommended)"},
			{Value: "public", Label: "Public", Hint: "Visible to everyone"},
		}
		model := tui.NewChoice("Visibility", "Should your registry be public or private?", choices)
		out, err := tea.NewProgram(model).Run()
		if err != nil {
			return "", "", err
		}
		final := out.(tui.ChoiceModel)
		if final.Cancelled() || final.Value() == nil {
			return "", "", fmt.Errorf("cancelled")
		}
		visibility = final.Value().(string)
	}

	owner, err := lookupGitHubOwner(ctx, gh)
	if err != nil {
		return "", "", err
	}

	// If the slug already contains "owner/", trust it.
	repo := repoSlug
	if !strings.Contains(repo, "/") {
		repo = owner + "/" + repo
	}

	description := fmt.Sprintf("Personal skill registry (%d skills) — managed via skill-registry.", len(localSkills))
	tempClient, err := registry.New(repo, "main")
	if err != nil {
		return "", "", err
	}
	tempClient.GH = gh
	fullRepo, err := tempClient.CreateRepo(ctx, repoSlug, visibility, description)
	if err != nil {
		// If the repo already exists on the user's account, allow reuse.
		if strings.Contains(err.Error(), "already exists") {
			fmt.Printf("%s repo %s already exists; reusing.\n", tui.HintStyle.Render("·"), repo)
			return repo, "main", nil
		}
		return "", "", err
	}
	if fullRepo == "" {
		fullRepo = repo
	}
	return fullRepo, "main", nil
}

func lookupGitHubOwner(ctx context.Context, gh string) (string, error) {
	client, err := registry.New("placeholder/placeholder", "main")
	if err != nil {
		return "", err
	}
	client.GH = gh
	var u struct {
		Login string `json:"login"`
	}
	if err := client.GetJSON(ctx, "user", &u); err != nil {
		return "", err
	}
	if u.Login == "" {
		return "", fmt.Errorf("could not determine GitHub login")
	}
	return u.Login, nil
}

func pushLocalSkills(ctx context.Context, client *registry.Client, local []scan.Skill) (int, error) {
	if len(local) == 0 {
		return 0, nil
	}
	// What's already in the registry?
	existing, err := client.Slugs(ctx)
	if err != nil {
		// Brand-new repo — assume empty.
		existing = map[string]struct{}{}
	}
	missing := scan.DedupeAgainst(local, existing)
	if len(missing) == 0 {
		return 0, nil
	}

	// Aggregate all files for one batched commit.
	files := map[string][]byte{}
	for _, sk := range missing {
		if err := walkSkillIntoFiles(sk, files); err != nil {
			return 0, err
		}
	}

	fmt.Printf("\n%s uploading %d skill(s) (%d files) to %s\n",
		tui.HintStyle.Render("·"), len(missing), len(files), client.Repo)
	fmt.Printf("  %s\n",
		tui.HintStyle.Render("large registries can take a few minutes — uploading in parallel…"))

	client.OnProgress = renderProgress(os.Stderr)
	defer func() { client.OnProgress = nil }()

	_, err = client.PushTree(ctx, files, fmt.Sprintf("init: import %d skill(s)", len(missing)))
	if err != nil {
		return 0, err
	}
	return len(missing), nil
}

// renderProgress returns an OnProgress callback that overwrites a single
// "uploaded X/N files" line on the given writer (typically stderr).
func renderProgress(w io.Writer) func(done, total int) {
	var lastWidth int
	return func(done, total int) {
		line := fmt.Sprintf("  uploaded %d/%d files", done, total)
		// Pad to clear any leftover characters from a shorter previous line.
		if pad := lastWidth - len(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		lastWidth = len(line)
		fmt.Fprintf(w, "\r%s", line)
		if done >= total {
			fmt.Fprintln(w)
		}
	}
}

func walkSkillIntoFiles(s scan.Skill, dst map[string][]byte) error {
	return filepathWalk(s.Folder, func(rel string, content []byte) {
		dst[s.Slug+"/"+rel] = content
	})
}

// filepathWalk reads every file under root (skipping hidden + __pycache__) and
// invokes cb with the relative path and content.
func filepathWalk(root string, cb func(rel string, content []byte)) error {
	return walkDirSkipHidden(root, cb)
}

func walkDirSkipHidden(root string, cb func(rel string, content []byte)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "__pycache__" {
			continue
		}
		full := filepath.Join(root, name)
		if e.IsDir() {
			sub, err := os.ReadDir(full)
			if err != nil {
				continue
			}
			for _, child := range sub {
				if err := walkSubdir(root, full, child, cb); err != nil {
					return err
				}
			}
			continue
		}
		body, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		cb(name, body)
	}
	return nil
}

func walkSubdir(root, dir string, child os.DirEntry, cb func(rel string, content []byte)) error {
	name := child.Name()
	if strings.HasPrefix(name, ".") || name == "__pycache__" {
		return nil
	}
	full := filepath.Join(dir, name)
	if child.IsDir() {
		entries, err := os.ReadDir(full)
		if err != nil {
			return nil
		}
		for _, sub := range entries {
			if err := walkSubdir(root, full, sub, cb); err != nil {
				return err
			}
		}
		return nil
	}
	body, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, full)
	cb(filepath.ToSlash(rel), body)
	return nil
}

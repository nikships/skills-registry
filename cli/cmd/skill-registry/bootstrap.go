package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
		repoFlag       string
		visFlag        string
		noAgents       bool
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
				Repo:           repoFlag,
				Visibility:     visFlag,
				NoAgents:       noAgents,
				NonInteractive: nonInteractive,
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
	// The initial push uses `git` (single push instead of N blob POSTs, which
	// trip GitHub's secondary rate limit on big registries). Fail-fast here
	// so the user isn't pushed through repo creation only to hit a missing
	// dependency at upload time.
	if err := requireGitForBootstrap(); err != nil {
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

	// 2. Create / reuse repo.
	cfg, err = resolveRegistry(ctx, cfg, gh, opts, localSkills)
	if err != nil {
		return err
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

	registrySlugs := buildRegistrySlugSet(ctx, client, localSkills)

	// 4. Multi-select agent install targets
	if err := installAgentDocs(home, cwd, cfg.Repo, opts); err != nil {
		return err
	}

	// 5. Offer to delete the now-redundant local skill folders.
	if !opts.NonInteractive {
		if err := promptDeleteLocal(sources, registrySlugs); err != nil {
			return err
		}
	}

	// 6. Print MCP JSON snippet
	printWireUpSnippet(cfg.Repo)
	return nil
}

// resolveRegistry checks if the config already has a valid repo, or prompts
// to create one. Returns the updated config.
func resolveRegistry(ctx context.Context, cfg config.Config, gh string, opts bootstrapOpts, localSkills []scan.Skill) (config.Config, error) {
	needsCreate := cfg.Repo == ""
	defaultName := ""
	if !needsCreate {
		probe, err := registry.New(cfg.Repo, cfg.DefaultBranch)
		if err != nil {
			return cfg, err
		}
		probe.GH = gh
		exists, err := probe.Exists(ctx)
		if err != nil {
			return cfg, fmt.Errorf("check registry %s: %w", cfg.Repo, err)
		}
		if exists {
			fmt.Printf("\n%s reusing existing registry: %s\n", tui.OkStyle.Render("✓"), tui.TitleStyle.Render(cfg.Repo))
			fmt.Printf("  %s %s\n", tui.HintStyle.Render("→"), repoURL(cfg.Repo))
		} else {
			fmt.Printf("\n%s configured registry %s no longer exists on GitHub.\n",
				tui.ErrorStyle.Render("!"), tui.TitleStyle.Render(cfg.Repo))
			fmt.Printf("  %s config still points at it: %s\n",
				tui.HintStyle.Render("·"), config.Path())
			recreate, err := promptRecreateRepo(opts.NonInteractive)
			if err != nil {
				return cfg, err
			}
			if !recreate {
				return cfg, fmt.Errorf("aborted; recreate %s on GitHub or edit/delete %s and re-run",
					cfg.Repo, config.Path())
			}
			defaultName = cfg.Name()
			needsCreate = true
		}
	}
	if needsCreate {
		repo, branch, err := promptAndCreateRepo(ctx, gh, opts, localSkills, defaultName)
		if err != nil {
			return cfg, err
		}
		cfg = config.Config{Repo: repo, DefaultBranch: branch}
		if _, err := config.Save(cfg); err != nil {
			return cfg, fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("\n%s saved registry config: %s\n", tui.OkStyle.Render("✓"), tui.TitleStyle.Render(repo))
		fmt.Printf("  %s %s\n", tui.HintStyle.Render("→"), repoURL(repo))
	}
	return cfg, nil
}

// buildRegistrySlugSet returns the post-push slug set for cleanup. Falls back
// to localSkills if the listing call fails.
func buildRegistrySlugSet(ctx context.Context, client *registry.Client, localSkills []scan.Skill) map[string]struct{} {
	registrySlugs, err := client.Slugs(ctx)
	if err != nil {
		fmt.Printf("\n%s could not list registry slugs (%v); cleanup scope falls back to just-pushed skills.\n",
			tui.HintStyle.Render("!"), err)
		registrySlugs = map[string]struct{}{}
		for _, s := range localSkills {
			registrySlugs[s.Slug] = struct{}{}
		}
	}
	return registrySlugs
}

// installAgentDocs runs the agent multi-select and installs SKILL.md into
// the chosen dot-folders.
func installAgentDocs(home, cwd, repo string, opts bootstrapOpts) error {
	if opts.NoAgents {
		fmt.Println("\nSkipping agent install (--no-agents).")
		return nil
	}
	picked, err := selectAgents(opts.NonInteractive)
	if err != nil {
		return err
	}
	paths, err := bootstrap.InstallSkillMd(home, cwd, repo, picked)
	if err != nil {
		return err
	}
	fmt.Printf("\n%s installed skill-registry/SKILL.md into %d agent folder(s):\n",
		tui.OkStyle.Render("✓"), len(paths))
	for _, p := range paths {
		fmt.Println("  ·", p)
	}
	return nil
}

// printWireUpSnippet prints the hosted-MCP JSON snippet and a note about
// Codex. The CLI never installs or boots an MCP server; the user just
// pastes this into their client config.
func printWireUpSnippet(repo string) {
	fmt.Println("\n" + tui.TitleStyle.Render("Wire it up:"))
	fmt.Println()
	fmt.Println(tui.SubtitleStyle.Render("Claude Code / Claude Desktop / Cursor / VS Code (mcp.json):"))
	fmt.Println(bootstrap.MCPJSONSnippet())
	fmt.Println()
	fmt.Println(tui.HintStyle.Render("· Codex requires stdio MCP — the hosted server doesn't support that yet."))
	fmt.Println()
	fmt.Printf("%s Your registry is live: %s\n",
		tui.OkStyle.Render("✓"), repoURL(repo))
	fmt.Println("\nDone.")
}

// repoURL returns the canonical https URL for a repo slug like "owner/name".
func repoURL(repo string) string {
	return "https://github.com/" + repo
}

// promptDeleteLocal offers the user a y/n to delete every dot-folder copy of
// a registered skill — real folders AND symlinks. Local skills are now dead
// weight: every agent that scans dot-folders re-reads and re-parses them on
// every session, bloating context and degrading performance.
//
// Earlier versions iterated `[]scan.Skill` (slug-deduped by Discover), which
// meant only ONE source's copy of each slug got deleted per run. With ~5
// agents installing identical copies (e.g. .codex, .copilot, .cursor, .factory,
// .gemini) the user had to run `init` five times to clean a single slug.
// Now we sweep every source for every known slug in one pass.
func promptDeleteLocal(sources []scan.Source, registrySlugs map[string]struct{}) error {
	entries := scan.EntriesForCleanup(sources, registrySlugs)
	if len(entries) == 0 {
		return nil
	}

	bySource := map[string]int{}
	symlinksBySource := map[string]int{}
	for _, en := range entries {
		bySource[en.Source]++
		if en.IsSymlink {
			symlinksBySource[en.Source]++
		}
	}
	srcLabels := make([]string, 0, len(bySource))
	for k := range bySource {
		srcLabels = append(srcLabels, k)
	}
	sort.Strings(srcLabels)

	fmt.Println()
	fmt.Println(tui.TitleStyle.Render("Clean up local copies?"))
	fmt.Println(tui.HintStyle.Render(
		"Now that your skills live in the registry, the local copies are dead weight."))
	fmt.Println(tui.HintStyle.Render(
		"Every coding agent re-reads them on each session, bloating context and degrading performance."))
	fmt.Println()
	for _, src := range srcLabels {
		line := fmt.Sprintf("  · %s (%d entry(ies))", src, bySource[src])
		if n := symlinksBySource[src]; n > 0 {
			line += fmt.Sprintf(" — %d symlink(s)", n)
		}
		fmt.Println(line)
	}
	fmt.Println()

	choices := []tui.Choice{
		{Value: "yes", Label: "Yes, delete", Hint: "Recommended — cleaner context"},
		{Value: "no", Label: "No, keep them", Hint: "I'll handle it manually"},
	}
	model := tui.NewChoice("Delete local skill folders?",
		fmt.Sprintf("This removes %d entry(ies) across %d source folder(s). Nothing in the registry is touched.",
			len(entries), len(srcLabels)),
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
	for _, en := range entries {
		// os.RemoveAll unlinks a symlink (it doesn't follow into the target),
		// and recursively removes a real directory. Both are what we want.
		if err := os.RemoveAll(en.Path); err != nil {
			fmt.Printf("  %s could not remove %s: %v\n",
				tui.ErrorStyle.Render("!"), en.Path, err)
			failed++
			continue
		}
		deleted++
	}
	fmt.Printf("\n%s removed %d local entry(ies).", tui.OkStyle.Render("✓"), deleted)
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

// promptRecreateRepo asks the user whether to re-create a registry repo that
// the saved config still references. Returns true on "yes" (or in
// nonInteractive/--yes mode). Returns false on explicit "no" or Esc.
func promptRecreateRepo(nonInteractive bool) (bool, error) {
	if nonInteractive {
		return true, nil
	}
	choices := []tui.Choice{
		{Value: "yes", Label: "Yes, recreate it", Hint: "Same name by default — you can edit it next"},
		{Value: "no", Label: "No, abort", Hint: "I'll restore the repo or edit the config myself"},
	}
	model := tui.NewChoice(
		"Registry repo missing",
		"The configured registry no longer exists on GitHub. Recreate it now?",
		choices,
	)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return false, err
	}
	final := out.(tui.ChoiceModel)
	if final.Cancelled() || final.Value() == nil {
		return false, nil
	}
	return final.Value().(string) == "yes", nil
}

func promptAndCreateRepo(ctx context.Context, gh string, opts bootstrapOpts, localSkills []scan.Skill, defaultName string) (string, string, error) {
	repoSlug, err := promptRepoName(opts, defaultName)
	if err != nil {
		return "", "", err
	}

	visibility, err := promptVisibility(opts)
	if err != nil {
		return "", "", err
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

// promptRepoName asks the user for a repository name, or returns the
// pre-configured value from opts.Repo.
func promptRepoName(opts bootstrapOpts, defaultName string) (string, error) {
	repoSlug := strings.TrimSpace(opts.Repo)
	if repoSlug != "" {
		return repoSlug, nil
	}
	seed := defaultName
	if seed == "" {
		seed = "skill-registry"
	}
	nameModel := tui.NewInput(
		"Registry repo name",
		"What should we name your skill registry repo on GitHub?",
		seed,
		seed,
	)
	nameModel.Help = "Enter just the name (no `owner/` prefix). Created on your authenticated user account."
	out, err := tea.NewProgram(nameModel).Run()
	if err != nil {
		return "", err
	}
	final := out.(tui.InputModel)
	if final.Cancelled() {
		return "", fmt.Errorf("cancelled")
	}
	name := final.Value()
	if name == "" {
		name = "skill-registry"
	}
	return name, nil
}

// promptVisibility asks the user for repo visibility, or returns the
// pre-configured value from opts.Visibility.
func promptVisibility(opts bootstrapOpts) (string, error) {
	if opts.Visibility != "" {
		switch opts.Visibility {
		case "public", "private":
			return opts.Visibility, nil
		default:
			return "", fmt.Errorf("invalid --visibility %q: must be \"public\" or \"private\"", opts.Visibility)
		}
	}
	choices := []tui.Choice{
		{Value: "private", Label: "Private", Hint: "Only you can see and clone (recommended)"},
		{Value: "public", Label: "Public", Hint: "Visible to everyone"},
	}
	model := tui.NewChoice("Visibility", "Should your registry be public or private?", choices)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return "", err
	}
	final := out.(tui.ChoiceModel)
	if final.Cancelled() || final.Value() == nil {
		return "", fmt.Errorf("cancelled")
	}
	return final.Value().(string), nil
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

// requireGitForBootstrap is the upfront probe for the bulk-push dependency.
// Returns a clean install hint when `git` is missing so the user doesn't
// discover this after they've already named a repo and clicked through
// prompts.
func requireGitForBootstrap() error {
	if _, err := exec.LookPath("git"); err == nil {
		return nil
	}
	return fmt.Errorf(
		"git not found on PATH. The bootstrap step pushes all your skills in a " +
			"single `git push` to avoid GitHub's per-API rate limit.\n" +
			"Install git from https://git-scm.com/downloads " +
			"(macOS: `brew install git`, Linux: `apt install git` / `dnf install git`) and re-run.")
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

	fmt.Printf("\n%s uploading %d skill(s) (%d files) to %s via git push\n",
		tui.HintStyle.Render("·"), len(missing), len(files), client.Repo)

	client.OnProgress = renderProgress(os.Stderr)
	client.OnStatus = func(msg string) { fmt.Fprintf(os.Stderr, "  %s\n", msg) }
	defer func() {
		client.OnProgress = nil
		client.OnStatus = nil
	}()

	if err := client.PushTreeViaGit(ctx, files, fmt.Sprintf("init: import %d skill(s)", len(missing))); err != nil {
		return 0, err
	}
	return len(missing), nil
}

// renderProgress returns an OnProgress callback that overwrites a single
// "prepared X/N files" line on the given writer (typically stderr). The
// final `git push` happens after the callback finishes; that step is
// surfaced separately via OnStatus so the message only fires when an
// actual push is about to happen.
func renderProgress(w io.Writer) func(done, total int) {
	var lastWidth int
	return func(done, total int) {
		line := fmt.Sprintf("  prepared %d/%d files", done, total)
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
	return walkDirSkipHidden(s.Folder, func(rel string, content []byte) {
		dst[s.Slug+"/"+rel] = content
	})
}

// walkDirSkipHidden reads every file under root (skipping hidden +
// __pycache__) and invokes cb with the relative path and content.
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

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/cache"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// removeLocationRegistry / Cache / Dotfolders are the canonical string
// IDs emitted in the `--json` payload's `removed_from` array. Keeping
// them as constants prevents callers (tests, hub wiring) from drifting
// onto slightly-different spellings.
const (
	removeLocationRegistry  = "registry"
	removeLocationCache     = "cache"
	removeLocationDotFolder = "dotfolders"
)

// removeResult is the structured payload emitted when `--json` is set.
// Field order mirrors the validation contract's JSON-005 example so
// downstream `jq` consumers see a stable shape across releases.
type removeResult struct {
	Slug        string   `json:"slug"`
	RemovedFrom []string `json:"removed_from"`
	SHA         string   `json:"sha,omitempty"`
	Repo        string   `json:"repo,omitempty"`
}

// removeReport is the in-memory view of what was actually deleted on
// the local filesystem. The cobra wrapper translates this into either
// a JSON payload (when jsonout.Enabled()) or a series of human-readable
// status lines (default).
type removeReport struct {
	Slug              string
	Repo              string
	CommitSHA         string
	CacheCleared      bool
	DotFoldersCleared int
	DotFolderPaths    []string
}

func newRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <slug>",
		Short: "Delete a skill from the registry, local cache, and agent dot-folders",
		Long: `Removes a skill end-to-end:

  1. The <slug>/ subtree is deleted from the GitHub registry repo via
     the Git Data API (single atomic commit).
  2. The Python MCP server's local cache (~/.cache/skills-mcp/skills/<slug>/
     and <slug>.meta.json) is wiped so the next get_skill call re-fetches.
  3. Every known AI tool dot-folder (~/.claude/skills, ~/.factory/skills,
     etc. — see agents.All()) is scanned for a matching <slug>/ subdir
     and removed.

Interactive runs surface a confirmation prompt before any destructive
action. Use --yes to skip the prompt (required for scripted invocations
that don't pipe --json), or --json to opt into structured stdout output
(which implies --yes — JSON callers never get a TUI prompt).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemoveCmd(cmd.Context(), args[0], yes || shouldAutoYes())
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	return cmd
}

// runRemoveCmd is the cobra wrapper around runRemove. It bridges the
// shared core to the two output modes: human-readable status lines for
// interactive use, and a structured JSON payload (with PrintError +
// os.Exit(1) on failure) for `--json`. The split keeps runRemove free
// of presentation concerns so the hub launcher can reuse it.
func runRemoveCmd(ctx context.Context, slug string, yes bool) error {
	jsonMode := jsonout.Enabled()
	report, err := runRemove(ctx, slug, yes, jsonMode)
	if err != nil {
		if jsonMode {
			jsonout.PrintError(err)
			os.Exit(1)
		}
		return err
	}
	if report == nil {
		// Aborted at confirmation; nothing destructive ran.
		return nil
	}
	if jsonMode {
		return jsonout.Print(reportToJSON(*report))
	}
	printRemoveSummary(*report)
	return nil
}

// runRemove is the shared core invoked by both the standalone `remove`
// subcommand and the hub's Remove card. Returns a nil *removeReport
// when the user aborts at the confirmation step.
//
// Skips the interactive confirm when either:
//   - yes is true (explicit `--yes`), or
//   - quietMode is true (JSON or non-TTY invocation — neither can
//     usefully render a Bubble Tea prompt).
func runRemove(ctx context.Context, slug string, yes, quietMode bool) (*removeReport, error) {
	cfg, client, err := loadRegistryForRemove()
	if err != nil {
		return nil, err
	}
	canonSlug := scan.Slugify(slug)
	if err := assertRemoteSlugExists(ctx, client, canonSlug, cfg.Repo); err != nil {
		return nil, err
	}
	if !yes && !quietMode {
		ok, err := confirmRemove(canonSlug, cfg.Repo)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}
	report := &removeReport{Slug: canonSlug, Repo: cfg.Repo}
	sha, err := client.Delete(ctx, canonSlug)
	if err != nil {
		return nil, fmt.Errorf("delete %s from %s: %w", canonSlug, cfg.Repo, err)
	}
	report.CommitSHA = sha
	report.CacheCleared = removeFromCache(canonSlug)
	report.DotFolderPaths = removeFromDotFolders(canonSlug)
	report.DotFoldersCleared = len(report.DotFolderPaths)
	return report, nil
}

// loadRegistryForRemove resolves the active config and constructs the
// `gh`-backed registry client. Extracted so runRemove stays under the
// cyclomatic complexity ceiling and tests can stub the client via a
// shared helper.
func loadRegistryForRemove() (config.Config, *registry.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, client, nil
}

// assertRemoteSlugExists is the source-of-truth gate for the exit-1
// "not found" branch. Probing via Slugs() rather than letting Delete
// surface ErrSlugNotFound keeps the confirmation prompt from spinning
// up only to be torn down moments later, and gives the user a single
// clean error message instead of a generic API failure.
func assertRemoteSlugExists(ctx context.Context, client *registry.Client, slug, repo string) error {
	slugs, err := client.Slugs(ctx)
	if err != nil {
		return fmt.Errorf("list registry slugs in %s: %w", repo, err)
	}
	if _, ok := slugs[slug]; !ok {
		return fmt.Errorf("slug %q not found in registry %s", slug, repo)
	}
	return nil
}

// confirmRemove renders a yes/no prompt before the destructive action.
// "Cancel" is the default highlight (cursor starts on Yes; the user
// must press enter to commit) — Choice doesn't offer per-row default
// cursor placement, so we list Cancel second and rely on the impossible-
// to-fat-finger esc fallback to give the user an obvious safety net.
func confirmRemove(slug, repo string) (bool, error) {
	choices := []tui.Choice{
		{Value: "no", Label: "Cancel", Hint: "Make no changes"},
		{Value: "yes", Label: "Yes, remove it", Hint: "Deletes the slug everywhere"},
	}
	model := tui.NewChoice(
		fmt.Sprintf("Remove %s from %s?", slug, repo),
		"This deletes the skill from the GitHub registry, local cache, and every agent dot-folder.",
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

// removeFromCache wipes the Python MCP server's per-slug cache. The
// directory and the sibling `<slug>.meta.json` file are both removed
// when present. Returns true if any of the two existed prior to the
// call — used to populate the `removed_from` JSON array.
func removeFromCache(slug string) bool {
	root := cache.CacheRoot()
	skillDir := filepath.Join(root, slug)
	metaFile := filepath.Join(root, slug+".meta.json")
	removed := false
	if _, err := os.Stat(skillDir); err == nil {
		if rmErr := os.RemoveAll(skillDir); rmErr == nil {
			removed = true
		}
	}
	if _, err := os.Stat(metaFile); err == nil {
		if rmErr := os.Remove(metaFile); rmErr == nil {
			removed = true
		}
	}
	return removed
}

// removeFromDotFolders sweeps every agent dot-folder (~/.claude/skills,
// .agents/skills under cwd, etc.) and removes any direct child whose
// name matches the slug — literally or via Slugify so hyphenated folder
// names ("agp-9-upgrade") match canonical slugs ("agp_9_upgrade").
//
// Symlinks are removed without following (os.RemoveAll unlinks the
// symlink itself). Real directories are removed recursively. Returns
// the absolute paths actually deleted so the CLI surface can both
// count and log them.
func removeFromDotFolders(slug string) []string {
	home, homeErr := os.UserHomeDir()
	cwd, cwdErr := os.Getwd()
	if homeErr != nil || cwdErr != nil {
		fmt.Fprintf(os.Stderr, "warning: skipping dot-folder cleanup (home=%v, cwd=%v)\n", homeErr, cwdErr)
		return nil
	}
	return removeFromDotFoldersAt(slug, home, cwd)
}

// removeFromDotFoldersAt is the testable core of removeFromDotFolders.
// It accepts explicit home/cwd so callers (and tests) don't depend on
// the process-wide os.UserHomeDir / os.Getwd.
func removeFromDotFoldersAt(slug, home, cwd string) []string {
	var deleted []string
	for _, target := range agents.All() {
		dir := target.SkillsDir(home, cwd)
		paths := matchSlugChildren(dir, slug)
		for _, p := range paths {
			if err := os.RemoveAll(p); err == nil {
				deleted = append(deleted, p)
			}
		}
	}
	sort.Strings(deleted)
	return deleted
}

// matchSlugChildren returns every direct child of `parent` whose name
// matches `slug` literally or via Slugify. Returns an empty slice when
// parent doesn't exist or is unreadable — both are normal in a fresh
// install where most agent dot-folders are absent.
func matchSlugChildren(parent, slug string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == slug || scan.Slugify(name) == slug {
			out = append(out, filepath.Join(parent, name))
		}
	}
	return out
}

// reportToJSON converts the internal report into the externally-visible
// JSON shape. The `removed_from` array is built fresh each call so the
// caller doesn't accidentally append into a shared slice.
func reportToJSON(r removeReport) removeResult {
	locations := []string{removeLocationRegistry}
	if r.CacheCleared {
		locations = append(locations, removeLocationCache)
	}
	if r.DotFoldersCleared > 0 {
		locations = append(locations, removeLocationDotFolder)
	}
	return removeResult{
		Slug:        r.Slug,
		Repo:        r.Repo,
		SHA:         r.CommitSHA,
		RemovedFrom: locations,
	}
}

// printRemoveSummary writes the human-readable success block. Mirrors
// the publish.go output style (✓ headline + indented bullet detail) so
// the visual language stays consistent across destructive operations.
func printRemoveSummary(r removeReport) {
	fmt.Println(
		tui.OkStyle.Render("✓"),
		"removed",
		r.Slug,
		"from",
		r.Repo+"@"+shortSHA(r.CommitSHA),
	)
	fmt.Println("  · registry: deleted")
	if r.CacheCleared {
		fmt.Println("  · cache: cleared")
	}
	if r.DotFoldersCleared > 0 {
		fmt.Printf("  · agent folders: %d cleared\n", r.DotFoldersCleared)
		for _, p := range r.DotFolderPaths {
			fmt.Println("    ·", tui.HintStyle.Render(p))
		}
	}
}

// removeSummaryLine is the single-sentence form of printRemoveSummary
// used inside the hub's toast row. The toast can't wrap multiple lines,
// so we compress the report into "<slug> · registry · cache · 2
// dotfolders" form.
func removeSummaryLine(r removeReport) string {
	parts := []string{r.Slug, "registry"}
	if r.CacheCleared {
		parts = append(parts, "cache")
	}
	if r.DotFoldersCleared > 0 {
		parts = append(parts, fmt.Sprintf("%d dotfolders", r.DotFoldersCleared))
	}
	return strings.Join(parts, " · ")
}

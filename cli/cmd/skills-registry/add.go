package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// addJSONResult is the payload emitted by `add --json [--yes]`.
// Mirrors syncJSONResult so an agent driving both commands sees a
// consistent {pushed, skipped} shape. `skipped` carries slugs that
// were discovered inside the source but already exist in the registry
// (the safe "no-op" path) so the consumer can decide whether to flag
// drift.
type addJSONResult struct {
	Pushed  []string `json:"pushed"`
	Skipped []string `json:"skipped"`
}

var (
	ghShorthandRe      = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	windowsDrivePathRe = regexp.MustCompile(`^[A-Za-z]:`)
)

func newAddCmd() *cobra.Command {
	var (
		yes bool
		all bool
	)
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills from an external source (path, owner/repo, or git URL) to the registry",
		Long: `Clones (or uses) the source, discovers every SKILL.md inside it, lets
you multi-select what to publish, and pushes the selected skills to your
GitHub registry repo — not your local folder.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonout.Enabled() {
				cmd.SilenceErrors = true
				return runAddJSON(cmd.Context(), args[0])
			}
			return runAdd(cmd.Context(), args[0], yes || shouldAutoYes(), all)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	cmd.Flags().BoolVar(&all, "all", false, "Publish every skill found in the source.")
	return cmd
}

// runAddJSON is the --json code path: skips the multi-select prompt,
// publishes every SKILL.md found in the resolved source that isn't
// already in the registry, and emits {pushed, skipped}. Failures
// surface as {"error": "..."} + a non-zero exit.
func runAddJSON(ctx context.Context, source string) error {
	cfg, err := config.Load()
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	dir, cleanup, err := resolveSource(ctx, source)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	defer cleanup()
	skills, err := scan.Discover([]scan.Source{{Path: dir, Label: source}})
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	if len(skills) == 0 {
		err := fmt.Errorf("no SKILL.md files found under %s", source)
		jsonout.PrintError(err)
		return err
	}
	existing, err := client.Slugs(ctx)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	var pushed, skipped []string
	safeSource := redactSourceUserInfo(source)
	for _, sk := range skills {
		if _, dup := existing[sk.Slug]; dup {
			skipped = append(skipped, sk.Slug)
			continue
		}
		files := map[string][]byte{}
		if err := walkSkillIntoFiles(sk, files); err != nil {
			jsonout.PrintError(err)
			return err
		}
		bySlug := rekeyBySlug(sk.Slug, files)
		msg := fmt.Sprintf("add: %s (from %s)", sk.Slug, safeSource)
		if _, err := client.Publish(ctx, sk.Slug, bySlug, msg); err != nil {
			err = fmt.Errorf("publish %s: %w", sk.Slug, err)
			jsonout.PrintError(err)
			return err
		}
		pushed = append(pushed, sk.Slug)
	}
	if pushed == nil {
		pushed = []string{}
	}
	if skipped == nil {
		skipped = []string{}
	}
	return jsonout.Print(addJSONResult{Pushed: pushed, Skipped: skipped})
}

func runAdd(ctx context.Context, source string, yes, all bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}

	dir, cleanup, err := resolveSource(ctx, source)
	if err != nil {
		return err
	}
	defer cleanup()

	skills, err := scan.Discover([]scan.Source{{Path: dir, Label: source}})
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		return fmt.Errorf("no SKILL.md files found under %s", source)
	}

	picked, err := selectSkillsForAdd(skills, yes, all, source, cfg.Repo)
	if err != nil {
		return err
	}
	if picked == nil {
		return nil
	}
	if len(picked) == 0 {
		fmt.Println("Nothing selected.")
		return nil
	}

	safeSource := redactSourceUserInfo(source)
	return publishSkills(ctx, client, picked, func(slug string) string {
		return fmt.Sprintf("add: %s (from %s)", slug, safeSource)
	})
}

// selectSkillsForAdd handles the interactive multi-select and confirmation
// for add. Returns nil with no error when the user cancels or selects nothing.
func selectSkillsForAdd(skills []scan.Skill, yes, all bool, source, repo string) ([]scan.Skill, error) {
	if all {
		return skills, nil
	}
	picked, err := promptAddSelection(skills)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled") {
			return nil, nil
		}
		return nil, err
	}
	if len(picked) == 0 {
		return []scan.Skill{}, nil
	}
	if !yes {
		ok, err := confirmPush(fmt.Sprintf(
			"Publish %d skill(s) from %s to %s?", len(picked), source, repo))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}
	return picked, nil
}

func promptAddSelection(skills []scan.Skill) ([]scan.Skill, error) {
	items := make([]tui.MultiSelectItem, 0, len(skills))
	for _, s := range skills {
		items = append(items, tui.MultiSelectItem{
			Value: s,
			Label: s.Name,
			Hint:  s.Slug,
		})
	}
	model := tui.NewMultiSelect("Select skills to publish", items, nil, true)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return nil, err
	}
	final := out.(tui.MultiSelectModel)
	if final.Cancelled() {
		return nil, fmt.Errorf("cancelled")
	}
	var picked []scan.Skill
	for _, v := range final.SelectedValues() {
		picked = append(picked, v.(scan.Skill))
	}
	return picked, nil
}

func resolveSource(ctx context.Context, source string) (string, func(), error) {
	return resolveSourceWithNotice(ctx, source, !jsonout.Enabled())
}

func resolveSourceQuiet(ctx context.Context, source string) (string, func(), error) {
	return resolveSourceWithNotice(ctx, source, false)
}

func resolveSourceWithNotice(ctx context.Context, source string, announce bool) (string, func(), error) {
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "~") {
		path, err := validateLocalSourcePath(source)
		if err != nil {
			return "", noopCleanup, err
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", noopCleanup, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return "", noopCleanup, fmt.Errorf("not a directory: %s", source)
		}
		return abs, noopCleanup, nil
	}

	url := source
	if ghShorthandRe.MatchString(source) {
		url = "https://github.com/" + source + ".git"
	}
	tmp, err := os.MkdirTemp("", "skills-registry-add-")
	if err != nil {
		return "", noopCleanup, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if announce {
		fmt.Println(tui.HintStyle.Render("cloning " + url + " …"))
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--single-branch", url, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", noopCleanup, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}
	return tmp, cleanup, nil
}

func validateLocalSourcePath(source string) (string, error) {
	path, err := url.PathUnescape(source)
	if err != nil {
		return "", fmt.Errorf("invalid source path encoding: %w", err)
	}
	lowerSource := strings.ToLower(source)
	switch {
	case strings.Contains(path, `\`) || strings.Contains(lowerSource, "%5c"):
		return "", fmt.Errorf("invalid source path: backslashes are not allowed")
	case strings.Contains(lowerSource, "%2f"):
		return "", fmt.Errorf("invalid source path: encoded separators are not allowed")
	case strings.HasPrefix(path, "~"):
		return "", fmt.Errorf("invalid source path: tilde expansion is not allowed")
	case filepath.IsAbs(path) || windowsDrivePathRe.MatchString(path):
		return "", fmt.Errorf("invalid source path: absolute paths are not allowed")
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid source path: traversal is not allowed")
		}
	}
	return path, nil
}

func redactSourceUserInfo(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || parsed == nil || parsed.User == nil || parsed.Scheme == "" {
		return source
	}
	parsed.User = nil
	return parsed.String()
}

func noopCleanup() {}

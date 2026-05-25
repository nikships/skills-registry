package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// publishJSONResult is the payload emitted by `publish --json`. Field
// order matches the JSON-003 contract ({slug, sha, url}) so a
// `jq '.url'` consumer can immediately open the resulting GitHub tree
// view. SHA carries the new commit's full hash, not the 7-char short
// form printed for humans — agents downstream often want the canonical
// identifier.
type publishJSONResult struct {
	Slug string `json:"slug"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

func newPublishCmd() *cobra.Command {
	var nameOverride string
	cmd := &cobra.Command{
		Use:   "publish <path>",
		Short: "Publish a single local skill folder to the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonout.Enabled() {
				return runPublishJSON(cmd.Context(), args[0], nameOverride)
			}
			return runPublish(cmd.Context(), args[0], nameOverride)
		},
	}
	cmd.Flags().StringVar(&nameOverride, "name", "", "Override the skill name (default: read from SKILL.md).")
	return cmd
}

func runPublish(ctx context.Context, path, nameOverride string) error {
	res, err := doPublish(ctx, path, nameOverride)
	if err != nil {
		return err
	}
	fmt.Println(tui.OkStyle.Render("✓"), "published", res.slug, "to", res.repo+"@"+shortSHA(res.sha))
	fmt.Printf("  view: %s\n", res.url)
	return nil
}

// runPublishJSON is the --json code path: publishes the skill folder
// and emits {slug, sha, url}. Errors land as {"error": "..."} +
// os.Exit(1). The branch is taken before doPublish so even setup-time
// failures (missing config, bad path) surface as JSON.
func runPublishJSON(ctx context.Context, path, nameOverride string) error {
	res, err := doPublish(ctx, path, nameOverride)
	if err != nil {
		jsonout.PrintError(err)
		os.Exit(1)
	}
	return jsonout.Print(publishJSONResult{
		Slug: res.slug,
		SHA:  res.sha,
		URL:  res.url,
	})
}

// publishOutcome carries the resolved metadata for one publish call.
// Extracted so runPublish (human-readable output) and runPublishJSON
// (structured output) share a single business-logic implementation.
type publishOutcome struct {
	slug string
	repo string
	sha  string
	url  string
}

// doPublish is the shared core: resolves config + client, validates
// the on-disk skill folder, gathers files, and pushes to the registry.
// Returns a publishOutcome so both presentation layers can render the
// same data their own way.
func doPublish(ctx context.Context, path, nameOverride string) (publishOutcome, error) {
	cfg, err := config.Load()
	if err != nil {
		return publishOutcome{}, err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return publishOutcome{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return publishOutcome{}, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return publishOutcome{}, fmt.Errorf("not a directory: %s", path)
	}
	if _, err := os.Stat(filepath.Join(abs, scan.MainFileName)); err != nil {
		return publishOutcome{}, fmt.Errorf("missing SKILL.md in %s", path)
	}
	body, err := os.ReadFile(filepath.Join(abs, scan.MainFileName))
	if err != nil {
		return publishOutcome{}, err
	}
	name := nameOverride
	if name == "" {
		name, _ = readNameFromFrontmatter(string(body))
		if name == "" {
			name = filepath.Base(abs)
		}
	}
	slug := scan.Slugify(name)
	files := map[string][]byte{}
	if err := collectFiles(abs, "", files); err != nil {
		return publishOutcome{}, err
	}
	if len(files) == 0 {
		return publishOutcome{}, fmt.Errorf("nothing to publish: %s appears empty", abs)
	}
	sha, err := client.Publish(ctx, slug, files, fmt.Sprintf("publish: %s", slug))
	if err != nil {
		return publishOutcome{}, err
	}
	return publishOutcome{
		slug: slug,
		repo: cfg.Repo,
		sha:  sha,
		url:  fmt.Sprintf("https://github.com/%s/tree/%s/%s", cfg.Repo, shortSHA(sha), slug),
	}, nil
}

// maxFileBytes is the per-file size cap, matching the Python
// SKILLS_MAX_FILE_BYTES default (2 MiB). Files exceeding this
// limit are skipped with a warning to prevent accidental upload
// of large binaries.
const maxFileBytes = 2 * 1024 * 1024

func collectFiles(root, prefix string, out map[string][]byte) error {
	entries, err := os.ReadDir(filepath.Join(root, prefix))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "__pycache__" {
			continue
		}
		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}
		if e.IsDir() {
			if err := collectFiles(root, rel, out); err != nil {
				return err
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes {
			fmt.Fprintf(os.Stderr, "warning: skipping %s (%d bytes > %d byte limit)\n", rel, info.Size(), maxFileBytes)
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		out[rel] = body
	}
	return nil
}

func readNameFromFrontmatter(text string) (string, string) {
	if !strings.HasPrefix(text, "---") {
		return "", ""
	}
	lines := strings.Split(text, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", ""
	}
	var name, desc string
	for _, raw := range lines[1:end] {
		if !strings.Contains(raw, ":") {
			continue
		}
		k, v, _ := strings.Cut(raw, ":")
		key := strings.TrimSpace(k)
		val := strings.Trim(strings.TrimSpace(v), "'\"")
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	return name, desc
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

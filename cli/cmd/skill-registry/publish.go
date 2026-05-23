package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

func newPublishCmd() *cobra.Command {
	var nameOverride string
	cmd := &cobra.Command{
		Use:   "publish <path>",
		Short: "Publish a single local skill folder to the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(cmd.Context(), args[0], nameOverride)
		},
	}
	cmd.Flags().StringVar(&nameOverride, "name", "", "Override the skill name (default: read from SKILL.md).")
	return cmd
}

func runPublish(ctx context.Context, path, nameOverride string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	if _, err := os.Stat(filepath.Join(abs, scan.MainFileName)); err != nil {
		return fmt.Errorf("missing SKILL.md in %s", path)
	}

	body, err := os.ReadFile(filepath.Join(abs, scan.MainFileName))
	if err != nil {
		return err
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
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("nothing to publish: %s appears empty", abs)
	}

	sha, err := client.Publish(ctx, slug, files, fmt.Sprintf("publish: %s", slug))
	if err != nil {
		return err
	}
	fmt.Println(tui.OkStyle.Render("✓"), "published", slug, "to", cfg.Repo+"@"+shortSHA(sha))
	fmt.Printf("  view: https://github.com/%s/tree/%s/%s\n", cfg.Repo, shortSHA(sha), slug)
	return nil
}

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

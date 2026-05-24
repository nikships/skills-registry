package main

import (
	"context"
	"fmt"
	"os"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/cache"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

func buildHubDeps(ctx context.Context, cfg config.Config) tui.HubDeps {
	return tui.HubDeps{
		Repo:  cfg.Repo,
		Count: hubCountLoader(cfg.Repo, cfg.DefaultBranch),
		Manage: tui.ManageFlowDeps{
			Rows:     manageRows(ctx, cfg),
			Download: manageDownloader(cfg),
			Delete:   manageDeleter,
		},
		Settings: buildSettingsDeps(cfg),
		Add:      buildAddFlowDeps(cfg),
		Publish:  buildPublishFlowDeps(),
		Sync:     buildSyncFlowDeps(cfg),
	}
}

func buildSettingsDeps(cfg config.Config) tui.SettingsFlowDeps {
	mcpBin, _ := locateMCPBinary()
	return tui.SettingsFlowDeps{
		Repo:      cfg.Repo,
		Branch:    cfg.DefaultBranch,
		CacheRoot: cache.CacheRoot(),
		MCPBinary: mcpBin,
		Save:      settingsSaver(),
	}
}

func manageRows(ctx context.Context, cfg config.Config) tui.RowLoader {
	return func() ([]tui.SkillRow, error) {
		client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
		if err != nil {
			return nil, err
		}
		summaries, err := client.List(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]tui.SkillRow, 0, len(summaries))
		for _, s := range summaries {
			rows = append(rows, tui.SkillRow{Slug: s.Slug, Name: s.Name, Desc: s.Description})
		}
		return rows, nil
	}
}

func manageDownloader(cfg config.Config) tui.Downloader {
	return func(downloadCtx context.Context, slug string) (string, string, error) {
		client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
		if err != nil {
			return "", "", err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("resolve current working directory: %w", err)
		}
		return DownloadSkill(downloadCtx, client, slug, "", cwd)
	}
}

func manageDeleter(deleteCtx context.Context, slug string) (string, error) {
	report, err := runRemove(deleteCtx, slug, true, true)
	if err != nil {
		return "", err
	}
	if report == nil {
		return "", fmt.Errorf("remove %s cancelled", slug)
	}
	return report.CommitSHA, nil
}

func buildAddFlowDeps(cfg config.Config) tui.AddFlowDeps {
	return tui.AddFlowDeps{
		Resolve: func(ctx context.Context, source string) (string, func(), error) {
			return resolveSourceQuiet(ctx, source)
		},
		Discover: func(dir, label string) ([]scan.Skill, error) {
			return scan.Discover([]scan.Source{{Path: dir, Label: label}})
		},
		Slugs: func(ctx context.Context) (map[string]struct{}, error) {
			client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
			if err != nil {
				return nil, err
			}
			return client.Slugs(ctx)
		},
		Files: filesForSkill,
		Publish: func(ctx context.Context, slug string, files map[string][]byte, msg string) (string, error) {
			client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
			if err != nil {
				return "", err
			}
			return client.Publish(ctx, slug, files, msg)
		},
	}
}

func buildPublishFlowDeps() tui.PublishFlowDeps {
	return tui.PublishFlowDeps{
		Publish: func(ctx context.Context, path string) (tui.PublishFlowResult, error) {
			out, err := doPublish(ctx, path, "")
			if err != nil {
				return tui.PublishFlowResult{}, err
			}
			return tui.PublishFlowResult{
				Slug: out.slug,
				Repo: out.repo,
				SHA:  out.sha,
				URL:  out.url,
			}, nil
		},
	}
}

func buildSyncFlowDeps(cfg config.Config) tui.SyncFlowDeps {
	return tui.SyncFlowDeps{
		Discover: func(context.Context) ([]scan.Skill, error) {
			return discoverLocalSkills()
		},
		Slugs: func(ctx context.Context) (map[string]struct{}, error) {
			client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
			if err != nil {
				return nil, err
			}
			return client.Slugs(ctx)
		},
		Files: filesForSkill,
		Publish: func(ctx context.Context, slug string, files map[string][]byte, msg string) (string, error) {
			client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
			if err != nil {
				return "", err
			}
			return client.Publish(ctx, slug, files, msg)
		},
	}
}

func discoverLocalSkills() ([]scan.Skill, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve current working directory: %w", err)
	}
	dotDirs := make([]string, 0, len(agents.All()))
	for _, a := range agents.All() {
		dotDirs = append(dotDirs, a.DotDir)
	}
	return scan.Discover(scan.DiscoverSources(home, cwd, nil, dotDirs))
}

func filesForSkill(sk scan.Skill) (map[string][]byte, error) {
	files := map[string][]byte{}
	if err := walkSkillIntoFiles(sk, files); err != nil {
		return nil, err
	}
	return rekeyBySlug(sk.Slug, files), nil
}

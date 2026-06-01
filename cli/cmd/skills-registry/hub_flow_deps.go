package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anand-92/skills-registry/cli/internal/bootstrap"
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
			Rows:           manageRows(ctx, cfg),
			Install:        manageInstaller(cfg),
			InstallTargets: installPickerTargets,
			Delete:         manageDeleter,
		},
		Settings: buildSettingsDeps(cfg),
		Add:      buildAddFlowDeps(cfg),
		Publish:  buildPublishFlowDeps(),
		Sync:     buildSyncFlowDeps(cfg),
		Purge:    buildPurgeFlowDeps(),
	}
}

func buildSettingsDeps(cfg config.Config) tui.SettingsFlowDeps {
	return tui.SettingsFlowDeps{
		Repo:      cfg.Repo,
		Branch:    cfg.DefaultBranch,
		CacheRoot: cache.CacheRoot(),
		HostedMCP: bootstrap.HostedMCPURL,
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

// manageInstaller bridges tui.Installer to installSkillIntoTargets so
// the manage TUI's Enter-to-install pulls a skill once and copies it
// into every user-selected agent dot-folder. The registry client is
// rebuilt per invocation for the same scoping reasons as
// registrySlugsFn.
func manageInstaller(cfg config.Config) tui.Installer {
	return func(installCtx context.Context, slug string, values []any) ([]string, error) {
		client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
		if err != nil {
			return nil, err
		}
		targets, err := installAnyValuesToTargets(values)
		if err != nil {
			return nil, err
		}
		return installSkillIntoTargets(installCtx, client, slug, targets)
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
		Slugs:          registrySlugsFn(cfg),
		Files:          filesForSkill,
		Publish:        registryPublishFn(cfg),
		InstallTargets: installPickerTargets,
		Install:        manageInstaller(cfg),
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
		Slugs:   registrySlugsFn(cfg),
		Files:   filesForSkill,
		Publish: registryPublishFn(cfg),
	}
}

// registrySlugsFn returns the standard "list registry slugs" lambda
// every hub flow uses to discover what's already published. A fresh
// registry.Client is built per call so the underlying gh credential
// cache (and any future per-call config) stays scoped to the request.
func registrySlugsFn(cfg config.Config) func(context.Context) (map[string]struct{}, error) {
	return func(ctx context.Context) (map[string]struct{}, error) {
		client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
		if err != nil {
			return nil, err
		}
		return client.Slugs(ctx)
	}
}

// registryPublishFn returns the standard "publish a skill" lambda
// every hub flow that writes to the registry uses. The client is
// rebuilt per call for the same scoping reasons as registrySlugsFn.
func registryPublishFn(cfg config.Config) func(context.Context, string, map[string][]byte, string) (string, error) {
	return func(ctx context.Context, slug string, files map[string][]byte, msg string) (string, error) {
		client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
		if err != nil {
			return "", err
		}
		return client.Publish(ctx, slug, files, msg)
	}
}

func buildPurgeFlowDeps() tui.PurgeFlowDeps {
	return tui.PurgeFlowDeps{
		Discover: func(context.Context) ([]scan.Skill, error) {
			skills, err := discoverLocalSkills()
			if err != nil {
				return nil, err
			}
			return filterMetaSkill(skills), nil
		},
		Delete: purgeLocalSkills,
	}
}

// filterMetaSkill strips the bootstrapped `skills-registry` meta-skill
// (written by bootstrap.InstallSkillMd into every agent dot-folder) so
// the Purge flow never wipes the agent's gateway back into the registry.
// The wizard's post-publish cleanup (scan.EntriesForCleanup) makes the
// same carve-out; this keeps the two flows consistent.
func filterMetaSkill(skills []scan.Skill) []scan.Skill {
	out := make([]scan.Skill, 0, len(skills))
	for _, sk := range skills {
		if isMetaSkill(sk) {
			continue
		}
		out = append(out, sk)
	}
	return out
}

// isMetaSkill reports whether sk is the bootstrapped `skills-registry`
// meta-skill. scan.Skill.Folder is always the folder containing SKILL.md,
// so a single basename compare is sufficient — no need to also probe for
// the SKILL.md sibling, scan.Discover already guarantees it.
func isMetaSkill(sk scan.Skill) bool {
	return filepath.Base(sk.Folder) == "skills-registry"
}

// localSkillSources resolves the discover-source list ($HOME + $CWD
// across every known dot-folder). Shared by discoverLocalSkills and
// purgeAllowedRoots so the home/cwd/DiscoverSources resolution lives in
// one place.
func localSkillSources() ([]scan.Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve current working directory: %w", err)
	}
	return scan.DiscoverSources(home, cwd, nil, dotDirsFromAgents()), nil
}

func discoverLocalSkills() ([]scan.Skill, error) {
	sources, err := localSkillSources()
	if err != nil {
		return nil, err
	}
	return scan.Discover(sources)
}

// purgeLocalSkills removes each supplied skill folder with os.RemoveAll.
// Best-effort: a single failure does not abort the loop; the returned
// (deleted, failed) counts let the caller toast a partial-failure
// summary. We require the folder to sit inside a known dot-folder
// (resolved via DiscoverSources) before touching it so a bad caller
// can't pass an arbitrary path.
//
// The context is checked at the top of each iteration so a force-quit
// (ctrl+c) from the TUI halts the delete loop between folders rather
// than running it to completion in the background.
func purgeLocalSkills(ctx context.Context, skills []scan.Skill) (int, int, error) {
	if len(skills) == 0 {
		return 0, 0, nil
	}
	allowed, err := purgeAllowedRoots()
	if err != nil {
		return 0, 0, err
	}
	var deleted, failed int
	for _, sk := range skills {
		if err := ctx.Err(); err != nil {
			return deleted, failed, err
		}
		// Defense in depth: even if a caller bypasses the Discover-side
		// filter (filterMetaSkill), never wipe the bootstrapped
		// `skills-registry` meta-skill. Silent skip — neither deleted
		// nor failed — because Purge's contract is "never touch it".
		if isMetaSkill(sk) {
			continue
		}
		if !pathUnderAnyRoot(sk.Folder, allowed) {
			failed++
			continue
		}
		if err := os.RemoveAll(sk.Folder); err != nil {
			failed++
			continue
		}
		deleted++
	}
	return deleted, failed, nil
}

// purgeAllowedRoots returns the absolute paths of every known dot-folder
// `<dot>/skills` directory under $HOME and $CWD. Skill folders outside
// these roots are refused — the purge action is intentionally scoped to
// what discoverLocalSkills surfaces.
func purgeAllowedRoots() ([]string, error) {
	sources, err := localSkillSources()
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(sources))
	for _, s := range sources {
		roots = append(roots, s.Path)
	}
	return roots, nil
}

// pathUnderAnyRoot reports whether folder is contained in any of the
// supplied root directories. Both sides are resolved to absolute paths
// before comparing with filepath.Rel so traversals (".."), absolute
// drift, and the root-itself case are rejected — only proper
// descendants match. The root case matters: if a skill's Folder ever
// resolves to the root itself (e.g. `~/.claude/skills`), `os.RemoveAll`
// on it would wipe every sibling skill under that root.
func pathUnderAnyRoot(folder string, roots []string) bool {
	absFolder, err := filepath.Abs(folder)
	if err != nil {
		return false
	}
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, absFolder)
		if err != nil {
			continue
		}
		// rel == "." means folder IS the root — refuse. The HasPrefix
		// check catches "../sibling" traversals; IsAbs catches the
		// case where filepath.Rel returns an absolute path because the
		// two paths share no common base (e.g. different volumes on
		// Windows).
		if rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func filesForSkill(sk scan.Skill) (map[string][]byte, error) {
	files := map[string][]byte{}
	if err := walkSkillIntoFiles(sk, files); err != nil {
		return nil, err
	}
	return rekeyBySlug(sk.Slug, files), nil
}

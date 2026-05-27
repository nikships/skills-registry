// Package main — durable local install of registry skills.
//
// `skills-registry get` writes into the global cache (~/.cache/skills-mcp/…)
// for temporary, agent-driven fetches. List / Manage / Add all share a
// different installer: pull the skill once into a temp directory, then
// copy it into each user-selected agent dot-folder, then delete the
// temp directory. The cache is never touched.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// installPickerTargets returns the rows shown by the install agent
// picker. `.agents/skills` (the universal project-local folder) is
// Locked=true so it's always installed; the small "popular" set is
// Default=true so first-time users get a sensible pre-checked state.
// The opaque Value is the agents.Target itself so the cmd-side
// installer can resolve SkillsDir() without re-parsing the display.
func installPickerTargets() []tui.InstallTarget {
	all := agents.All()
	out := make([]tui.InstallTarget, 0, len(all))
	for _, t := range all {
		_, popular := popularAgentDisplays[t.Display]
		out = append(out, tui.InstallTarget{
			Display: t.Display,
			Hint:    t.DotDir + "/skills",
			Locked:  t.Universal,
			Default: popular,
			Value:   t,
		})
	}
	return out
}

// universalInstallTargets returns the locked-universal agent targets
// (currently just `.agents/skills`). Used by non-interactive paths
// (`add --json`) so the durable install always lands somewhere even
// without a prompt. Matches the plan's "Durable user installs always
// include .agents/skills" assumption.
func universalInstallTargets() []agents.Target {
	out := make([]agents.Target, 0, 1)
	for _, t := range agents.All() {
		if t.Universal {
			out = append(out, t)
		}
	}
	return out
}

// installSkillIntoTargets fetches a registry skill into a temporary
// directory, copies its contents into each target's
// `<dotdir>/skills/<slug>/`, then removes the temp directory. Returns
// the absolute paths actually written (one per target) so the caller
// can surface them in the toast / JSON payload. The cache root is
// never written to — that's the whole point of the split from `get`.
func installSkillIntoTargets(ctx context.Context, client *registry.Client, slug string, targets []agents.Target) ([]string, error) {
	if len(targets) == 0 {
		return nil, errors.New("install requires at least one target")
	}
	canon := scan.Slugify(slug)

	scratch, err := os.MkdirTemp("", "skills-registry-install-")
	if err != nil {
		return nil, fmt.Errorf("create scratch dir: %w", err)
	}
	// Best-effort cleanup. We intentionally do NOT propagate the
	// remove error — the install paths have already been written and
	// the user shouldn't see a noisy failure for a temp-dir cleanup.
	defer func() { _ = os.RemoveAll(scratch) }()

	source := filepath.Join(scratch, canon)
	if err := os.MkdirAll(source, 0o755); err != nil {
		return nil, fmt.Errorf("create scratch %s: %w", source, err)
	}
	if err := client.Get(ctx, canon, source); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", canon, err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve cwd: %w", err)
	}

	written := make([]string, 0, len(targets))
	for _, t := range targets {
		dest := filepath.Join(t.SkillsDir(home, cwd), canon)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", dest, err)
		}
		if err := copyTree(source, dest); err != nil {
			return nil, fmt.Errorf("copy into %s: %w", dest, err)
		}
		written = append(written, dest)
	}
	return written, nil
}

// copyTree walks src and recreates the tree under dst. Existing files
// are overwritten — re-installing a skill should refresh the local
// copy, not leave the previous version lingering.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed: %s", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFileForInstall(path, target)
	})
}

// copyFileForInstall is a small replace-existing copy that preserves
// the source file's mode. Used by copyTree for each leaf entry. Named
// distinctly from update.go:copyFile so the two single-purpose helpers
// can stay isolated without one taking on responsibilities of the other.
func copyFileForInstall(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not allowed: %s", src)
	}
	srcFD, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFD.Close()
	dstFD, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer dstFD.Close()
	if _, err := io.Copy(dstFD, srcFD); err != nil {
		return err
	}
	return dstFD.Close()
}

// installAnyValuesToTargets converts the opaque []any returned by
// InstallPickerModel.SelectedValues() back into []agents.Target.
// Lives in the same file as installSkillIntoTargets so the conversion
// stays next to its only caller path.
func installAnyValuesToTargets(values []any) ([]agents.Target, error) {
	out := make([]agents.Target, 0, len(values))
	for _, v := range values {
		t, ok := v.(agents.Target)
		if !ok {
			return nil, fmt.Errorf("install picker value %T is not agents.Target", v)
		}
		out = append(out, t)
	}
	return out, nil
}

package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

const defaultUpdateRepo = "anand-92/skills-registry"

type updateRunnerFunc func(context.Context, updateOpts) (updateResult, error)

var updateRunner updateRunnerFunc = performUpdate

type updateOpts struct {
	repo    string
	version string
	binPath string
	force   bool
	dryRun  bool
	tarball string
}

type updateResult struct {
	Updated bool   `json:"updated"`
	Version string `json:"version"`
	Asset   string `json:"asset"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func newUpdateCmd() *cobra.Command {
	opts := updateOpts{repo: defaultUpdateRepo, version: "latest"}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the skills-registry CLI to the latest release",
		Long: `Downloads the matching skills-registry release asset and replaces the current binary.

By default this installs the latest GitHub Release from anand-92/skills-registry.
Use --version to pin a tag (for example v0.5.1), or --bin to update a specific binary path.
Set SKILLS_REGISTRY_AUTO_UPDATE=1 to update automatically before opening the hub.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.repo, "repo", opts.repo, "GitHub repository to download from (owner/repo).")
	cmd.Flags().StringVar(&opts.version, "version", opts.version, "Release tag to install, or latest.")
	cmd.Flags().StringVar(&opts.binPath, "bin", "", "Path to binary to replace (default: current executable).")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Reinstall even when the current version matches the target release.")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the planned update without downloading or replacing anything.")
	return cmd
}

func runUpdate(ctx context.Context, opts updateOpts) error {
	res, err := performUpdate(ctx, opts)
	if err != nil {
		if jsonout.Enabled() {
			jsonout.PrintError(err)
		}
		return err
	}
	if jsonout.Enabled() {
		return jsonout.Print(res)
	}
	if res.Updated {
		fmt.Println(tui.OkStyle.Render("✓"), res.Message)
		return nil
	}
	fmt.Println(tui.HintStyle.Render("·"), res.Message)
	return nil
}

func performUpdate(ctx context.Context, opts updateOpts) (updateResult, error) {
	if opts.repo == "" {
		opts.repo = defaultUpdateRepo
	}
	if opts.version == "" {
		opts.version = "latest"
	}
	asset, err := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return updateResult{}, err
	}
	binPath, err := updateTargetPath(opts.binPath)
	if err != nil {
		return updateResult{}, err
	}
	gh, err := registry.FindGH()
	if err != nil && opts.tarball == "" {
		return updateResult{}, err
	}
	targetVersion := opts.version
	if targetVersion == "latest" && gh != "" {
		tag, tagErr := latestReleaseTag(ctx, gh, opts.repo)
		if tagErr != nil {
			return updateResult{}, tagErr
		}
		targetVersion = tag
		opts.version = targetVersion
	}
	res := updateResult{
		Version: targetVersion,
		Asset:   asset,
		Path:    binPath,
	}
	if !opts.force && versionMatches(version, targetVersion) {
		res.Message = fmt.Sprintf("already on %s", targetVersion)
		return res, nil
	}
	if opts.dryRun {
		res.Message = fmt.Sprintf("would install %s from %s to %s", targetVersion, opts.repo, binPath)
		return res, nil
	}
	if err := installUpdate(ctx, gh, opts, asset, binPath); err != nil {
		return updateResult{}, err
	}
	res.Updated = true
	res.Message = fmt.Sprintf("updated skills-registry to %s → %s", targetVersion, binPath)
	return res, nil
}

func updateAssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("unsupported OS for update: %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported architecture for update: %s", goarch)
	}
	return fmt.Sprintf("skills-registry_%s_%s.tar.gz", goos, goarch), nil
}

func updateTargetPath(binPath string) (string, error) {
	if binPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve current executable: %w", err)
		}
		binPath = exe
	}
	abs, err := filepath.Abs(binPath)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect update target: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return abs, nil
	}
	target, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve update target symlink: %w", err)
	}
	return target, nil
}

func latestReleaseTag(ctx context.Context, gh, repo string) (string, error) {
	cmd := exec.CommandContext(ctx, gh, "release", "view", "--repo", repo, "--json", "tagName")
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return "", fmt.Errorf("resolve latest release: %w: %s", err, detail)
		}
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	var payload struct {
		TagName string `json:"tagName"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("parse latest release: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("latest release for %s did not include a tag", repo)
	}
	return payload.TagName, nil
}

func versionMatches(current, target string) bool {
	if current == "" || current == "dev" || target == "" || target == "latest" {
		return false
	}
	return strings.TrimPrefix(current, "v") == strings.TrimPrefix(target, "v")
}

func installUpdate(ctx context.Context, gh string, opts updateOpts, asset, binPath string) error {
	dir := filepath.Dir(binPath)
	tmpDir, err := os.MkdirTemp(dir, ".skills-registry-update-")
	if err != nil {
		return fmt.Errorf("create update temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarball := filepath.Join(tmpDir, asset)
	if opts.tarball != "" {
		if err := copyFile(opts.tarball, tarball, 0o644); err != nil {
			return err
		}
	} else if err := downloadUpdateAsset(ctx, gh, opts.repo, opts.version, asset, tmpDir); err != nil {
		return err
	}

	extracted := filepath.Join(tmpDir, "skills-registry")
	if err := extractUpdateBinary(tarball, extracted); err != nil {
		return err
	}
	if err := os.Chmod(extracted, 0o755); err != nil {
		return fmt.Errorf("mark binary executable: %w", err)
	}
	if err := os.Rename(extracted, binPath); err != nil {
		return fmt.Errorf("replace %s: %w", binPath, err)
	}
	return nil
}

func downloadUpdateAsset(ctx context.Context, gh, repo, version, asset, dir string) error {
	args := []string{"release", "download"}
	if version != "latest" {
		args = append(args, version)
	}
	args = append(args, "--repo", repo, "--pattern", asset, "--dir", dir, "--clobber")
	cmd := exec.CommandContext(ctx, gh, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("download release asset %s: %s", asset, strings.TrimSpace(string(out)))
	}
	return nil
}

func extractUpdateBinary(tarball, dest string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return fmt.Errorf("open update tarball: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read update tarball: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read update archive: %w", err)
		}
		if filepath.Base(hdr.Name) != "skills-registry" || hdr.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("write extracted binary: %w", err)
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("extract update binary: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close update binary: %w", closeErr)
		}
		return nil
	}
	return fmt.Errorf("binary skills-registry not found in update archive")
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s: %w", src, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", dst, closeErr)
	}
	return nil
}

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// Defaults mirror install.sh so the in-binary updater and the curl|sh
// installer always pull from the same release surface.
const (
	defaultUpdateRepo       = "anand-92/skills-registry"
	defaultGitHubAPIBaseURL = "https://api.github.com"
	defaultReleaseBaseURL   = "https://github.com"
	updateHTTPTimeout       = 60 * time.Second
	updateUserAgent         = "skills-registry-update"
)

type updateRunnerFunc func(context.Context, updateOpts) (updateResult, error)

// updateRunner is the indirection autoUpdate uses; tests swap it out.
var updateRunner updateRunnerFunc = performUpdate

// updateOpts is the input to performUpdate.
//
// The first six fields are bound to user-visible cobra flags. The trio
// at the bottom (apiBaseURL / releaseBaseURL / httpClient) are pure
// test-injection seams — they are deliberately *not* bound to flags so
// the public CLI surface keeps mirroring install.sh exactly. Tests
// override them to swap api.github.com / github.com for an httptest
// server.
type updateOpts struct {
	repo    string
	version string
	binPath string
	force   bool
	dryRun  bool
	tarball string

	apiBaseURL     string
	releaseBaseURL string
	httpClient     *http.Client
}

// updateResult is what performUpdate returns and what `--json` emits.
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
		Long: `Downloads the matching skills-registry release tarball from GitHub Releases
and replaces the current binary in place. Mirrors install.sh — no gh
dependency, just a straight HTTPS GET against the public release URL.

By default this installs the latest release from anand-92/skills-registry.
Use --version to pin a tag (for example v0.5.1), or --bin to update a
specific binary path. Set SKILLS_REGISTRY_AUTO_UPDATE=1 to opportunistically
update right before opening the hub.`,
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

// performUpdate is the headless entry point. It is exercised directly
// by unit tests and indirectly by runUpdate / runAutoUpdate.
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
	client := opts.httpClient
	if client == nil {
		client = &http.Client{Timeout: updateHTTPTimeout}
	}
	if opts.version == "latest" && opts.tarball == "" {
		tag, err := latestReleaseTag(ctx, client, updateAPIBase(opts), opts.repo)
		if err != nil {
			return updateResult{}, err
		}
		opts.version = tag
	}
	res := updateResult{
		Version: opts.version,
		Asset:   asset,
		Path:    binPath,
	}
	if !opts.force && versionMatches(version, opts.version) {
		res.Message = fmt.Sprintf("already on %s", opts.version)
		return res, nil
	}
	if opts.dryRun {
		res.Message = fmt.Sprintf("would install %s from %s to %s", opts.version, opts.repo, binPath)
		return res, nil
	}
	if err := installUpdate(ctx, client, opts, asset, binPath); err != nil {
		return updateResult{}, err
	}
	res.Updated = true
	res.Message = fmt.Sprintf("updated skills-registry to %s → %s", opts.version, binPath)
	return res, nil
}

// updateAssetName mirrors install.sh's detect_os/detect_arch +
// asset-name composition. All six combinations the release workflow
// publishes are accepted (darwin/linux/windows × amd64/arm64).
func updateAssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux", "windows":
	default:
		return "", fmt.Errorf("unsupported OS for update: %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported architecture for update: %s", goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("skills-registry_%s_%s%s", goos, goarch, ext), nil
}

// updateTargetPath resolves the file we're going to replace. Defaults
// to the current executable. Symlinks are followed so we replace the
// real binary, not the link — that way `brew`/`asdf`-style symlinked
// installs keep working.
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

func updateAPIBase(opts updateOpts) string {
	if opts.apiBaseURL != "" {
		return opts.apiBaseURL
	}
	return defaultGitHubAPIBaseURL
}

func updateReleaseBase(opts updateOpts) string {
	if opts.releaseBaseURL != "" {
		return opts.releaseBaseURL
	}
	return defaultReleaseBaseURL
}

// latestReleaseTag hits the GitHub REST API to discover the tag of the
// newest published release for repo. Returns the tag (e.g. "v0.7.0").
func latestReleaseTag(ctx context.Context, client *http.Client, apiBase, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(apiBase, "/"), repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build latest release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("resolve latest release: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parse latest release: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("latest release for %s did not include a tag", repo)
	}
	return payload.TagName, nil
}

// versionMatches is the "should we skip this update" comparator.
// "dev" never matches (those are local builds), "latest" never matches
// (we only get here after resolving it to a real tag, but defend
// anyway), and "v" prefixes are normalized.
func versionMatches(current, target string) bool {
	if current == "" || current == "dev" || target == "" || target == "latest" {
		return false
	}
	return strings.TrimPrefix(current, "v") == strings.TrimPrefix(target, "v")
}

// installUpdate downloads (or copies, if opts.tarball is set), extracts
// the binary, and atomically swaps it in via os.Rename.
//
// The temp dir lives next to binPath so the final Rename stays on the
// same filesystem — cross-FS renames fail on Linux, and a Rename to a
// running binary is the one POSIX-portable way to replace a live exe.
func installUpdate(ctx context.Context, client *http.Client, opts updateOpts, asset, binPath string) error {
	tmpDir, err := os.MkdirTemp(filepath.Dir(binPath), ".skills-registry-update-")
	if err != nil {
		return fmt.Errorf("create update temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarball := filepath.Join(tmpDir, asset)
	if opts.tarball != "" {
		err = copyFile(opts.tarball, tarball)
	} else {
		err = downloadUpdateAsset(ctx, client, updateReleaseBase(opts), opts.repo, opts.version, asset, tarball)
	}
	if err != nil {
		return err
	}

	extracted := filepath.Join(tmpDir, "skills-registry")
	if err := extractUpdateBinary(tarball, extracted); err != nil {
		return err
	}
	if err := os.Chmod(extracted, 0o755); err != nil {
		return fmt.Errorf("mark binary executable: %w", err)
	}
	if err := replaceBinary(extracted, binPath, runtime.GOOS); err != nil {
		return err
	}
	return nil
}

// replaceBinary swaps src in for dst atomically. On Windows it first
// rotates the existing dst to dst+".old" because Windows does not allow
// overwriting a running executable. The .old file is removed best-effort;
// if it is still locked it will be cleaned up on the next process start.
func replaceBinary(src, dst, goos string) error {
	if goos == "windows" {
		oldPath := dst + ".old"
		// Remove any stale .old left behind by a previous update.
		_ = os.Remove(oldPath)

		if err := os.Rename(dst, oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate old binary: %w", err)
		}
		if err := os.Rename(src, dst); err != nil {
			// Try to restore the old binary so the user isn't left broken.
			_ = os.Rename(oldPath, dst)
			return fmt.Errorf("replace %s: %w", dst, err)
		}
		// Best-effort: the old binary may still be locked by the
		// running process. Failure here is not fatal.
		_ = os.Remove(oldPath)
		return nil
	}

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	return nil
}

// cleanupOldBinaries removes stale <binary>.old files left behind by
// Windows self-updates. Called once at process start before any work
// begins so we don't litter the install directory.
func cleanupOldBinaries() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}

// downloadUpdateAsset writes the release tarball at the given URL to
// dest. URL composition mirrors install.sh:build_url exactly so the
// two paths stay in lockstep.
func downloadUpdateAsset(ctx context.Context, client *http.Client, releaseBase, repo, version, asset, dest string) error {
	base := strings.TrimRight(releaseBase, "/")
	var url string
	if version == "" || version == "latest" {
		url = fmt.Sprintf("%s/%s/releases/latest/download/%s", base, repo, asset)
	} else {
		url = fmt.Sprintf("%s/%s/releases/download/%s/%s", base, repo, version, asset)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", updateUserAgent)
	// Identity encoding keeps the on-the-wire bytes equal to the file
	// bytes — we don't want net/http to transparently gunzip the .tar.gz.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("download %s: %s: %s", asset, resp.Status, strings.TrimSpace(string(body)))
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	// `defer out.Close()` is the safety net: if any future return path is
	// added before the explicit Close below, the descriptor still gets
	// released. A double-close on an already-closed *os.File returns an
	// error which we drop, since the explicit close above has already
	// surfaced any flush failure to the caller.
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dest, err)
	}
	return nil
}

// extractUpdateBinary extracts the binary from an archive (tar.gz or zip).
// It looks for "skills-registry" (POSIX) or "skills-registry.exe" (Windows).
// The first matching regular file wins; everything else is skipped.
func extractUpdateBinary(archivePath, dest string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractUpdateBinaryZip(archivePath, dest)
	}
	return extractUpdateBinaryTarGz(archivePath, dest)
}

func extractUpdateBinaryTarGz(tarball, dest string) error {
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

func extractUpdateBinaryZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open update zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if (base != "skills-registry" && base != "skills-registry.exe") || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return fmt.Errorf("write extracted binary: %w", err)
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rcCloseErr := rc.Close()
		if copyErr != nil {
			return fmt.Errorf("extract update binary: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close update binary: %w", closeErr)
		}
		if rcCloseErr != nil {
			return fmt.Errorf("close zip entry: %w", rcCloseErr)
		}
		return nil
	}
	return fmt.Errorf("binary skills-registry not found in update archive")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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

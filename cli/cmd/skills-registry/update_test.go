package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAutoUpdateEnabled(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY_AUTO_UPDATE", "yes")
	if !autoUpdateEnabled() {
		t.Fatal("expected auto update to be enabled")
	}
	t.Setenv("SKILLS_REGISTRY_AUTO_UPDATE", "")
	if autoUpdateEnabled() {
		t.Fatal("expected auto update to be disabled by default")
	}
}

func TestRunAutoUpdateUsesRunnerAndSwallowsErrors(t *testing.T) {
	t.Setenv("SKILLS_REGISTRY_AUTO_UPDATE", "1")
	orig := updateRunner
	t.Cleanup(func() { updateRunner = orig })
	called := false
	updateRunner = func(context.Context, updateOpts) (updateResult, error) {
		called = true
		return updateResult{}, errors.New("boom")
	}
	var stderr bytes.Buffer
	runAutoUpdate(context.Background(), &stderr)
	if !called {
		t.Fatal("expected auto update runner to be called")
	}
	if !strings.Contains(stderr.String(), "warning: auto-update failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUpdateAssetNameSupported(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "skills-registry_darwin_arm64.tar.gz"},
		{"linux", "amd64", "skills-registry_linux_amd64.tar.gz"},
		{"windows", "amd64", "skills-registry_windows_amd64.zip"},
		{"windows", "arm64", "skills-registry_windows_arm64.zip"},
	}
	for _, c := range cases {
		got, err := updateAssetName(c.goos, c.goarch)
		if err != nil {
			t.Fatalf("updateAssetName(%q,%q) returned err: %v", c.goos, c.goarch, err)
		}
		if got != c.want {
			t.Fatalf("updateAssetName(%q,%q) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestUpdateAssetNameRejectsUnsupported(t *testing.T) {
	if _, err := updateAssetName("freebsd", "amd64"); err == nil {
		t.Fatal("expected unsupported OS error")
	}
	if _, err := updateAssetName("linux", "386"); err == nil {
		t.Fatal("expected unsupported arch error")
	}
}

func TestUpdateTargetPathResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-skills-registry")
	link := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	got, err := updateTargetPath(link)
	if err != nil {
		t.Fatalf("updateTargetPath: %v", err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if got != want {
		t.Fatalf("target path = %q, want %q", got, want)
	}
}

func TestVersionMatches(t *testing.T) {
	if !versionMatches("0.5.1", "v0.5.1") {
		t.Fatal("expected version to match tag with v prefix")
	}
	if versionMatches("dev", "v0.5.1") {
		t.Fatal("dev should never be treated as up to date")
	}
	if versionMatches("v0.5.1", "latest") {
		t.Fatal("'latest' is unresolved and must never match")
	}
	if versionMatches("", "v0.5.1") {
		t.Fatal("empty current version must never match")
	}
}

func TestPerformUpdateDryRunPinnedVersion(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "skills-registry")
	res, err := performUpdate(context.Background(), updateOpts{
		version: "v9.9.9",
		binPath: bin,
		dryRun:  true,
	})
	if err != nil {
		t.Fatalf("performUpdate dry run: %v", err)
	}
	if res.Updated {
		t.Fatal("dry run should not report Updated=true")
	}
	if res.Version != "v9.9.9" || res.Path != bin {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
	if !strings.Contains(res.Message, "would install") {
		t.Fatalf("dry-run message = %q", res.Message)
	}
}

func TestPerformUpdateFromLocalTarball(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatalf("write old bin: %v", err)
	}

	asset, err := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("updateAssetName: %v", err)
	}
	archivePath := filepath.Join(dir, "update"+filepath.Ext(asset))
	if strings.HasSuffix(asset, ".zip") {
		writeUpdateZip(t, archivePath, "skills-registry.exe", "new")
	} else {
		writeUpdateTarball(t, archivePath, "new")
	}

	res, err := performUpdate(context.Background(), updateOpts{
		version: "v9.9.9",
		binPath: bin,
		tarball: archivePath,
		force:   true,
	})
	if err != nil {
		t.Fatalf("performUpdate local tarball: %v", err)
	}
	if !res.Updated {
		t.Fatal("expected Updated=true")
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read updated bin: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("updated binary body = %q", body)
	}
}

// TestLatestReleaseTagViaHTTP exercises the GitHub API JSON path.
func TestLatestReleaseTagViaHTTP(t *testing.T) {
	wantTag := "v1.2.3"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/nikships/skills-registry/releases/latest" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); ua != updateUserAgent {
			t.Errorf("missing User-Agent: %q", ua)
		}
		if ac := r.Header.Get("Accept"); !strings.Contains(ac, "github") {
			t.Errorf("expected GitHub Accept header, got %q", ac)
		}
		_, _ = io.WriteString(w, fmt.Sprintf(`{"tag_name":%q}`, wantTag))
	}))
	t.Cleanup(srv.Close)

	got, err := latestReleaseTag(context.Background(), srv.Client(), srv.URL, "nikships/skills-registry")
	if err != nil {
		t.Fatalf("latestReleaseTag: %v", err)
	}
	if got != wantTag {
		t.Fatalf("tag = %q, want %q", got, wantTag)
	}
}

func TestLatestReleaseTagHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	_, err := latestReleaseTag(context.Background(), srv.Client(), srv.URL, "nikships/skills-registry")
	if err == nil {
		t.Fatal("expected error on non-200 response")
	}
	if !strings.Contains(err.Error(), "resolve latest release") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLatestReleaseTagEmptyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":""}`)
	}))
	t.Cleanup(srv.Close)

	_, err := latestReleaseTag(context.Background(), srv.Client(), srv.URL, "nikships/skills-registry")
	if err == nil {
		t.Fatal("expected error when tag_name is empty")
	}
	if !strings.Contains(err.Error(), "did not include a tag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLatestReleaseTagInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{not-json")
	}))
	t.Cleanup(srv.Close)

	_, err := latestReleaseTag(context.Background(), srv.Client(), srv.URL, "nikships/skills-registry")
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
	if !strings.Contains(err.Error(), "parse latest release") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDownloadUpdateAssetPinnedVersion verifies the URL path matches
// the format install.sh uses for pinned releases.
func TestDownloadUpdateAssetPinnedVersion(t *testing.T) {
	body := []byte("tarball-bytes")
	wantPath := "/nikships/skills-registry/releases/download/v1.2.3/skills-registry_darwin_arm64.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path %q (want %q)", r.URL.Path, wantPath)
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", r.Header.Get("Accept-Encoding"))
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "skills-registry_darwin_arm64.tar.gz")
	if err := downloadUpdateAsset(
		context.Background(), srv.Client(), srv.URL,
		"nikships/skills-registry", "v1.2.3",
		"skills-registry_darwin_arm64.tar.gz", dest,
	); err != nil {
		t.Fatalf("downloadUpdateAsset: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded body = %q, want %q", got, body)
	}
}

// TestDownloadUpdateAssetLatestPath verifies the latest-release URL
// uses /releases/latest/download/<asset> (matches install.sh).
func TestDownloadUpdateAssetLatestPath(t *testing.T) {
	wantPath := "/nikships/skills-registry/releases/latest/download/skills-registry_linux_amd64.tar.gz"
	hit := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path %q (want %q)", r.URL.Path, wantPath)
		}
		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "asset.tar.gz")
	if err := downloadUpdateAsset(
		context.Background(), srv.Client(), srv.URL,
		"nikships/skills-registry", "latest",
		"skills-registry_linux_amd64.tar.gz", dest,
	); err != nil {
		t.Fatalf("downloadUpdateAsset: %v", err)
	}
	if !hit.Load() {
		t.Fatal("expected the server to be hit")
	}
}

func TestDownloadUpdateAssetNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "asset.tar.gz")
	err := downloadUpdateAsset(
		context.Background(), srv.Client(), srv.URL,
		"nikships/skills-registry", "v9.9.9",
		"skills-registry_linux_amd64.tar.gz", dest,
	)
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Failed downloads must not leave a partial file behind. (We never
	// open dest on the error path.)
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected dest to be absent on error, stat err = %v", statErr)
	}
}

// TestPerformUpdateEndToEndViaHTTP wires up both endpoints behind an
// httptest server and exercises performUpdate the way the real CLI
// would: API call → tag resolve → tarball download → in-place swap.
func TestPerformUpdateEndToEndViaHTTP(t *testing.T) {
	// updateAssetName only knows darwin/linux/windows × amd64/arm64;
	// on a non-release GOOS/GOARCH (e.g. freebsd, 386) it returns an error
	// and the test would fail through no fault of the updater. Skip those
	// builds so `go test ./...` stays green on any host architecture.
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skipf("update only supports darwin/linux/windows, running on %s", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("update only supports amd64/arm64, running on %s", runtime.GOARCH)
	}
	asset, err := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("updateAssetName: %v", err)
	}
	wantTag := "v0.99.0"

	dir := t.TempDir()
	bin := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(bin, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("seed old bin: %v", err)
	}

	apiCalls := atomic.Int32{}
	dlCalls := atomic.Int32{}
	var apiSrv, releaseSrv *httptest.Server

	apiSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"tag_name":%q}`, wantTag))
	}))
	t.Cleanup(apiSrv.Close)

	releaseSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dlCalls.Add(1)
		wantPath := "/nikships/skills-registry/releases/download/" + wantTag + "/" + asset
		if r.URL.Path != wantPath {
			t.Errorf("download path = %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if strings.HasSuffix(asset, ".zip") {
			writeZipToWriter(t, w, "skills-registry.exe", "fresh binary contents")
		} else {
			writeTarballToWriter(t, w, "fresh binary contents")
		}
	}))
	t.Cleanup(releaseSrv.Close)

	res, err := performUpdate(context.Background(), updateOpts{
		repo:           "nikships/skills-registry",
		version:        "latest",
		binPath:        bin,
		force:          true,
		apiBaseURL:     apiSrv.URL,
		releaseBaseURL: releaseSrv.URL,
		httpClient:     apiSrv.Client(),
	})
	if err != nil {
		t.Fatalf("performUpdate: %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected Updated=true, got %+v", res)
	}
	if res.Version != wantTag {
		t.Fatalf("Version = %q, want %q", res.Version, wantTag)
	}
	if res.Asset != asset {
		t.Fatalf("Asset = %q, want %q", res.Asset, asset)
	}
	if res.Path != bin {
		t.Fatalf("Path = %q, want %q", res.Path, bin)
	}
	if apiCalls.Load() != 1 {
		t.Fatalf("API hit count = %d, want 1", apiCalls.Load())
	}
	if dlCalls.Load() != 1 {
		t.Fatalf("download hit count = %d, want 1", dlCalls.Load())
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read updated bin: %v", err)
	}
	if string(body) != "fresh binary contents" {
		t.Fatalf("binary body = %q, want %q", body, "fresh binary contents")
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat updated bin: %v", err)
	}
	// Windows doesn't have Unix permission bits; skip the executable check there.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("updated binary is not executable: mode=%v", info.Mode())
	}
}

// TestPerformUpdateSkipsWhenAlreadyLatest pins the no-op path: when the
// resolved tag matches the linked-in version and --force is not set,
// performUpdate must return Updated=false without writing anything.
func TestPerformUpdateSkipsWhenAlreadyLatest(t *testing.T) {
	origVersion := version
	version = "v9.9.9"
	t.Cleanup(func() { version = origVersion })

	dir := t.TempDir()
	bin := filepath.Join(dir, "skills-registry")
	body := []byte("untouched")
	if err := os.WriteFile(bin, body, 0o755); err != nil {
		t.Fatalf("seed bin: %v", err)
	}

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v9.9.9"}`)
	}))
	t.Cleanup(apiSrv.Close)

	releaseHits := atomic.Int32{}
	releaseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releaseHits.Add(1)
	}))
	t.Cleanup(releaseSrv.Close)

	res, err := performUpdate(context.Background(), updateOpts{
		version:        "latest",
		binPath:        bin,
		apiBaseURL:     apiSrv.URL,
		releaseBaseURL: releaseSrv.URL,
		httpClient:     apiSrv.Client(),
	})
	if err != nil {
		t.Fatalf("performUpdate: %v", err)
	}
	if res.Updated {
		t.Fatal("expected Updated=false when versions match")
	}
	if !strings.Contains(res.Message, "already on") {
		t.Fatalf("message = %q", res.Message)
	}
	if releaseHits.Load() != 0 {
		t.Fatalf("release endpoint hit %d times, want 0", releaseHits.Load())
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("binary mutated: got %q, want %q", got, body)
	}
}

// TestPerformUpdateDryRunResolvesLatest verifies dry-run still resolves
// the latest tag (so the printed message names a real version) but
// never touches the release endpoint.
func TestPerformUpdateDryRunResolvesLatest(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "skills-registry")

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v1.0.0"}`)
	}))
	t.Cleanup(apiSrv.Close)

	releaseHits := atomic.Int32{}
	releaseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releaseHits.Add(1)
	}))
	t.Cleanup(releaseSrv.Close)

	res, err := performUpdate(context.Background(), updateOpts{
		version:        "latest",
		binPath:        bin,
		dryRun:         true,
		force:          true,
		apiBaseURL:     apiSrv.URL,
		releaseBaseURL: releaseSrv.URL,
		httpClient:     apiSrv.Client(),
	})
	if err != nil {
		t.Fatalf("performUpdate: %v", err)
	}
	if res.Updated {
		t.Fatal("dry run must not report Updated=true")
	}
	if res.Version != "v1.0.0" {
		t.Fatalf("Version = %q, want v1.0.0", res.Version)
	}
	if !strings.Contains(res.Message, "would install v1.0.0") {
		t.Fatalf("message = %q", res.Message)
	}
	if releaseHits.Load() != 0 {
		t.Fatalf("dry run hit release endpoint %d times, want 0", releaseHits.Load())
	}
}

func TestNewUpdateCmdFlagWiring(t *testing.T) {
	cmd := newUpdateCmd()
	if cmd.Use != "update" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	for _, name := range []string{"repo", "version", "bin", "force", "dry-run"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("missing flag --%s", name)
		}
	}
}

// writeUpdateTarball writes a single-file gzipped tarball at path with
// the given binary body. Used by the local-tarball tests.
func writeUpdateTarball(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tarball: %v", err)
	}
	defer f.Close()
	writeTarballToWriter(t, f, body)
}

// writeTarballToWriter is the shared helper backing both
// writeUpdateTarball (file-on-disk path) and the httptest handlers
// that need to stream a fixture tarball as their response body.
func writeTarballToWriter(t *testing.T, w io.Writer, body string) {
	t.Helper()
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "skills-registry",
		Mode:     0o755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}

// writeUpdateZip writes a single-file zip at path with the given binary body.
func writeUpdateZip(t *testing.T, path, entryName, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	writeZipToWriter(t, f, entryName, body)
}

// writeZipToWriter streams a fixture zip for httptest handlers.
func writeZipToWriter(t *testing.T, w io.Writer, entryName, body string) {
	t.Helper()
	zw := zip.NewWriter(w)
	h := &zip.FileHeader{
		Name:   entryName,
		Method: zip.Deflate,
	}
	h.SetMode(0o755)
	fw, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := fw.Write([]byte(body)); err != nil {
		t.Fatalf("write zip body: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestExtractUpdateBinaryZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "update.zip")
	writeUpdateZip(t, zipPath, "skills-registry.exe", "win binary")

	dest := filepath.Join(dir, "skills-registry.exe")
	if err := extractUpdateBinary(zipPath, dest); err != nil {
		t.Fatalf("extractUpdateBinary(zip): %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(body) != "win binary" {
		t.Fatalf("extracted body = %q, want %q", body, "win binary")
	}
}

func TestReplaceBinaryPOSIX(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(dst, []byte("old"), 0o755); err != nil {
		t.Fatalf("write dst: %v", err)
	}
	src := filepath.Join(dir, "new")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := replaceBinary(src, dst, "linux"); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("dst = %q, want new", body)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("src should be gone, stat err = %v", err)
	}
}

func TestReplaceBinaryWindowsRotatesOld(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "skills-registry.exe")
	if err := os.WriteFile(dst, []byte("old"), 0o755); err != nil {
		t.Fatalf("write dst: %v", err)
	}
	src := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := replaceBinary(src, dst, "windows"); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("dst = %q, want new", body)
	}
	// The backup uses a unique .old.<nanos> suffix.
	matches, err := filepath.Glob(dst + ".old.*")
	if err != nil {
		t.Fatalf("glob .old.*: %v", err)
	}
	if len(matches) == 0 {
		// The backup may already have been removed (if the test runner
		// is Windows and the file isn't locked). That's acceptable.
		return
	}
	oldBody, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(oldBody) != "old" {
		t.Fatalf("backup = %q, want old", oldBody)
	}
}

func TestReplaceBinaryWindowsRestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "skills-registry.exe")
	if err := os.WriteFile(dst, []byte("old"), 0o755); err != nil {
		t.Fatalf("write dst: %v", err)
	}
	// Use a non-existent src so Rename(src, dst) reliably fails on
	// every platform, then verify the old binary is restored.
	src := filepath.Join(dir, "does-not-exist")
	if err := replaceBinary(src, dst, "windows"); err == nil {
		t.Fatal("expected replaceBinary to fail when src does not exist")
	}
	// dst must still contain the old binary.
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst after failed replace: %v", err)
	}
	if string(body) != "old" {
		t.Fatalf("dst = %q after failed replace, want old", body)
	}
}

func TestReplaceBinaryWindowsCreatesNewWhenMissing(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "skills-registry.exe")
	src := filepath.Join(dir, "skills-registry")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := replaceBinary(src, dst, "windows"); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("dst = %q, want new", body)
	}
}

func TestCleanupOldBinaries(t *testing.T) {
	dir := t.TempDir()
	// Exercise the glob cleanup path that cleanupOldBinaries uses.
	base := filepath.Join(dir, "skills-registry.exe")
	for _, name := range []string{"skills-registry.exe.old", "skills-registry.exe.old.123456", "skills-registry.exe.old.789012"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stale"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	matches, err := filepath.Glob(base + ".old*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 stale files, got %d", len(matches))
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			t.Fatalf("remove %s: %v", m, err)
		}
	}
	left, _ := filepath.Glob(base + ".old*")
	if len(left) != 0 {
		t.Fatalf("expected 0 stale files after cleanup, got %d", len(left))
	}
}

func TestExtractUpdateBinaryZipSkipsDirs(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "update.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	// add a directory entry
	_, err = zw.Create("skills-registry.exe/")
	if err != nil {
		t.Fatalf("create dir entry: %v", err)
	}
	// add the real binary
	h := &zip.FileHeader{Name: "skills-registry.exe", Method: zip.Deflate}
	h.SetMode(0o755)
	fw, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatalf("create real entry: %v", err)
	}
	fw.Write([]byte("real"))
	zw.Close()
	f.Close()

	dest := filepath.Join(dir, "out.exe")
	if err := extractUpdateBinary(zipPath, dest); err != nil {
		t.Fatalf("extractUpdateBinary: %v", err)
	}
	body, _ := os.ReadFile(dest)
	if string(body) != "real" {
		t.Fatalf("got %q, want real", body)
	}
}

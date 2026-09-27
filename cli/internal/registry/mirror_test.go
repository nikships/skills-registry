package registry

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seedBareWithFiles initializes a bare git remote, then seeds it via a
// sidecar working clone with the given path→content map. Returns the
// absolute path of the bare repo (suitable for use as Client.HTTPSURL).
func seedBareWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	remote := initBareRemote(t)
	if len(files) == 0 {
		return remote
	}
	seed := t.TempDir()
	work := filepath.Join(seed, "seed")
	runGitInTest(t, seed, "clone", remote, "seed")
	runGitInTest(t, work, "config", "user.name", "seed")
	runGitInTest(t, work, "config", "user.email", "seed@example.com")
	for rel, content := range files {
		full := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	runGitInTest(t, work, "add", "-A")
	runGitInTest(t, work, "commit", "-m", "seed")
	runGitInTest(t, work, "push", "-u", "origin", "main")
	return remote
}

// pushUpdate writes (or overwrites) files into the bare remote via a
// fresh sidecar clone. Used by the incremental-fetch test to simulate a
// remote-side change between two List() calls.
func pushUpdate(t *testing.T, remote string, files map[string]string) {
	t.Helper()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "side")
	runGitInTest(t, tmp, "clone", remote, "side")
	runGitInTest(t, work, "config", "user.name", "side")
	runGitInTest(t, work, "config", "user.email", "side@example.com")
	for rel, content := range files {
		full := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	runGitInTest(t, work, "add", "-A")
	runGitInTest(t, work, "commit", "-m", "update")
	runGitInTest(t, work, "push", "origin", "main")
}

// failingGH returns the path of a "gh" shim that exits non-zero with an
// "unexpected" message on every invocation. Used to assert the mirror
// path never falls back to gh api.
func failingGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	script := "#!/bin/sh\necho 'unexpected gh call' >&2\nexit 99\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write failing gh: %v", err)
	}
	return bin
}

// TestMirrorListSkipsGHAPI verifies that List reads from the local
// clone — the gh stub fails loudly on any call, so a successful List
// proves zero `gh api` round-trips happened.
func TestMirrorListSkipsGHAPI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := seedBareWithFiles(t, map[string]string{
		"alpha/SKILL.md": "---\nname: Alpha\ndescription: first skill\n---\n",
		"beta/SKILL.md":  "---\nname: Beta\ndescription: second skill\n---\n",
	})
	c := &Client{
		GH:            failingGH(t),
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		MirrorRoot:    t.TempDir(),
	}
	summaries, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d: %+v", len(summaries), summaries)
	}
	if summaries[0].Slug != "alpha" || summaries[1].Slug != "beta" {
		t.Fatalf("expected slug order [alpha, beta], got [%s, %s]",
			summaries[0].Slug, summaries[1].Slug)
	}
	if summaries[0].Name != "Alpha" || summaries[0].Description != "first skill" {
		t.Fatalf("alpha parsed wrong: %+v", summaries[0])
	}
	if summaries[1].Description != "second skill" {
		t.Fatalf("beta description wrong: %+v", summaries[1])
	}
}

// TestMirrorListIncrementalFetch verifies that a subsequent List call
// picks up remote-side changes via `git fetch` + `git reset --hard`,
// without re-cloning from scratch (the same mirror dir is reused).
func TestMirrorListIncrementalFetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := seedBareWithFiles(t, map[string]string{
		"alpha/SKILL.md": "---\nname: Alpha\ndescription: first version\n---\n",
	})
	mirror := t.TempDir()
	c := &Client{
		GH:            failingGH(t),
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		MirrorRoot:    mirror,
	}
	first, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List #1: %v", err)
	}
	if len(first) != 1 || first[0].Description != "first version" {
		t.Fatalf("first List wrong: %+v", first)
	}

	pushUpdate(t, remote, map[string]string{
		"alpha/SKILL.md": "---\nname: Alpha\ndescription: second version\n---\n",
	})

	second, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List #2: %v", err)
	}
	if len(second) != 1 || second[0].Description != "second version" {
		t.Fatalf("incremental fetch did not pick up update: %+v", second)
	}
	// .git dir should still be the same shallow clone — verify it survived.
	if _, err := os.Stat(filepath.Join(mirror, ".git")); err != nil {
		t.Fatalf("mirror .git missing after incremental fetch: %v", err)
	}
}

// TestMirrorGetCopiesNestedFiles verifies Get walks the mirror tree and
// reproduces nested files at the right relative path. Two files
// (SKILL.md + scripts/helper.py) make sure the recursion fires.
func TestMirrorGetCopiesNestedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := seedBareWithFiles(t, map[string]string{
		"gamma/SKILL.md":          "---\nname: Gamma\ndescription: g\n---\n",
		"gamma/scripts/helper.py": "print('hi')\n",
	})
	c := &Client{
		GH:            failingGH(t),
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		MirrorRoot:    t.TempDir(),
	}
	dest := t.TempDir()
	if err := c.Get(context.Background(), "gamma", dest); err != nil {
		t.Fatalf("Get: %v", err)
	}
	skill, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatalf("missing SKILL.md: %v", err)
	}
	if !strings.Contains(string(skill), "name: Gamma") {
		t.Fatalf("SKILL.md content wrong: %q", skill)
	}
	helper, err := os.ReadFile(filepath.Join(dest, "scripts", "helper.py"))
	if err != nil {
		t.Fatalf("missing scripts/helper.py: %v", err)
	}
	if string(helper) != "print('hi')\n" {
		t.Fatalf("helper.py content wrong: %q", helper)
	}
}

// TestMirrorEmptyRepoReturnsZeroSkills verifies that an upstream with
// no commits (brand-new `gh repo create` output) makes List return an
// empty slice without erroring and without firing the gh stub.
func TestMirrorEmptyRepoReturnsZeroSkills(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := initBareRemote(t) // no commits, no main branch
	c := &Client{
		GH:            failingGH(t),
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		MirrorRoot:    t.TempDir(),
	}
	summaries, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List on empty repo: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected zero summaries for empty repo, got %+v", summaries)
	}
}

// TestMirrorDisableFallsBackToAPI verifies that SKILLS_MIRROR_DISABLE
// short-circuits the mirror entirely, sending all traffic back through
// `gh api`. We seed a stub that records being hit; the test fails if
// the mirror path is taken (the stub never fires).
func TestMirrorDisableFallsBackToAPI(t *testing.T) {
	t.Setenv("SKILLS_MIRROR_DISABLE", "1")
	bin, _ := stubGH(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "fallback-skill", "type": "dir", "sha": "tree-1"},
			},
		},
		{
			"key": "GET repos/x/y/contents/fallback-skill/SKILL.md",
			"body": map[string]any{
				"encoding": "base64",
				// "---\nname: Fallback\ndescription: via api\n---\n"
				"content": "LS0tCm5hbWU6IEZhbGxiYWNrCmRlc2NyaXB0aW9uOiB2aWEgYXBpCi0tLQo=",
			},
		},
	})
	c := &Client{
		GH:            bin,
		Repo:          "x/y",
		DefaultBranch: "main",
		// MirrorRoot intentionally left set to keep this test hermetic
		// even if the test process somehow ignores the env var.
		MirrorRoot: t.TempDir(),
	}
	summaries, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Slug != "fallback-skill" {
		t.Fatalf("expected single fallback-skill summary, got %+v", summaries)
	}
	if summaries[0].Description != "via api" {
		t.Fatalf("expected description 'via api', got %q", summaries[0].Description)
	}
}

// TestMirrorGetMissingSlugReturnsNotFound verifies that a slug absent
// from the (freshly fast-forwarded) mirror fails loudly with
// ErrSlugNotFound instead of succeeding with an empty dest. The mirror
// miss is authoritative — the gh stub fails any API fallback attempt.
func TestMirrorGetMissingSlugReturnsNotFound(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := seedBareWithFiles(t, map[string]string{
		"present/SKILL.md": "---\nname: Present\n---\n",
	})
	c := &Client{
		GH:            failingGH(t),
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		MirrorRoot:    t.TempDir(),
	}
	dest := filepath.Join(t.TempDir(), "absent")
	if err := c.Get(context.Background(), "absent", dest); !errors.Is(err, ErrSlugNotFound) {
		t.Fatalf("Get(absent) = %v, want ErrSlugNotFound", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("Get(absent) must not create dest, stat = %v", err)
	}
}

// TestMirrorSlugsMatchesList sanity-checks that Slugs returns exactly
// the set of top-level dirs that List enumerates. Cheap because both
// hit the same mirror clone.
func TestMirrorSlugsMatchesList(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := seedBareWithFiles(t, map[string]string{
		"alpha/SKILL.md": "---\nname: Alpha\n---\n",
		"beta/SKILL.md":  "---\nname: Beta\n---\n",
	})
	c := &Client{
		GH:            failingGH(t),
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		MirrorRoot:    t.TempDir(),
	}
	slugs, err := c.Slugs(context.Background())
	if err != nil {
		t.Fatalf("Slugs: %v", err)
	}
	if len(slugs) != 2 {
		t.Fatalf("expected 2 slugs, got %v", slugs)
	}
	if _, ok := slugs["alpha"]; !ok {
		t.Fatalf("missing alpha in slugs: %v", slugs)
	}
	if _, ok := slugs["beta"]; !ok {
		t.Fatalf("missing beta in slugs: %v", slugs)
	}
}

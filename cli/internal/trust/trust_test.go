package trust

import "testing"

// TestAssessClassifiesEverySourceShape is the core table: each source shape
// `add` accepts, and whether it goes through the import gate.
func TestAssessClassifiesEverySourceShape(t *testing.T) {
	opts := Options{Owners: []string{"nikships"}}
	cases := []struct {
		name          string
		source        string
		opts          Options
		wantOrigin    Origin
		wantUntrusted bool
		wantOwner     string
	}{
		{
			name:       "relative local path",
			source:     "./skills/pdf",
			opts:       opts,
			wantOrigin: OriginLocalPath,
		},
		{
			name:       "absolute local path",
			source:     "/Users/nik/skills/pdf",
			opts:       opts,
			wantOrigin: OriginLocalPath,
		},
		{
			name:       "parent-relative local path",
			source:     "../skills",
			opts:       opts,
			wantOrigin: OriginLocalPath,
		},
		{
			name:       "home-relative local path",
			source:     "~/skills",
			opts:       opts,
			wantOrigin: OriginLocalPath,
		},
		{
			name:       "own repo shorthand",
			source:     "nikships/skills-registry",
			opts:       opts,
			wantOrigin: OriginOwnRepo,
			wantOwner:  "nikships",
		},
		{
			name:       "own repo shorthand with different case",
			source:     "NikShips/skills-registry",
			opts:       opts,
			wantOrigin: OriginOwnRepo,
			wantOwner:  "NikShips",
		},
		{
			name:          "third-party shorthand",
			source:        "Xquik-dev/tweetclaw",
			opts:          opts,
			wantOrigin:    OriginPublicRepo,
			wantUntrusted: true,
			wantOwner:     "Xquik-dev",
		},
		{
			name:       "own repo tree URL with a folder",
			source:     "https://github.com/nikships/skills-registry/tree/main/skills/pdf",
			opts:       opts,
			wantOrigin: OriginOwnRepo,
			wantOwner:  "nikships",
		},
		{
			name:          "third-party tree URL",
			source:        "https://github.com/owner/repo/tree/main/skills/pdf",
			opts:          opts,
			wantOrigin:    OriginPublicRepo,
			wantUntrusted: true,
			wantOwner:     "owner",
		},
		{
			name:          "third-party blob URL",
			source:        "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize",
			opts:          opts,
			wantOrigin:    OriginPublicRepo,
			wantUntrusted: true,
			wantOwner:     "openclaw",
		},
		{
			name:          "third-party bare repo URL",
			source:        "https://github.com/openclaw/openclaw",
			opts:          opts,
			wantOrigin:    OriginPublicRepo,
			wantUntrusted: true,
			wantOwner:     "openclaw",
		},
		{
			name:          "non-github git URL",
			source:        "https://gitlab.com/owner/repo.git",
			opts:          opts,
			wantOrigin:    OriginRemoteGit,
			wantUntrusted: true,
		},
		{
			name:          "ssh git remote",
			source:        "git@github.com:owner/repo.git",
			opts:          opts,
			wantOrigin:    OriginRemoteGit,
			wantUntrusted: true,
		},
		{
			// A user with no configured owner must not get a trusted verdict
			// by default: an empty owner list fails safe.
			name:          "own-looking repo with no configured owners",
			source:        "nikships/skills-registry",
			opts:          Options{},
			wantOrigin:    OriginPublicRepo,
			wantUntrusted: true,
			wantOwner:     "nikships",
		},
		{
			// A Discover pick is untrusted even when the URL happens to point
			// at the user's own repository: it was not the user who chose it.
			name:          "discover pick under the user's own owner",
			source:        "https://github.com/nikships/skills-registry/blob/abc/skills/pdf",
			opts:          Options{Owners: []string{"nikships"}, FromDiscover: true},
			wantOrigin:    OriginDiscover,
			wantUntrusted: true,
			wantOwner:     "nikships",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Assess(tc.source, tc.opts)
			if got.Origin != tc.wantOrigin {
				t.Errorf("Origin = %q, want %q", got.Origin, tc.wantOrigin)
			}
			if got.Untrusted != tc.wantUntrusted {
				t.Errorf("Untrusted = %v, want %v", got.Untrusted, tc.wantUntrusted)
			}
			if got.Owner != tc.wantOwner {
				t.Errorf("Owner = %q, want %q", got.Owner, tc.wantOwner)
			}
			if got.Source != tc.source {
				t.Errorf("Source = %q, want it echoed unchanged", got.Source)
			}
			if got.Reason == "" {
				t.Error("Reason is empty; every verdict must be explainable")
			}
			if got.Untrusted != got.Origin.Untrusted() {
				t.Errorf("Untrusted (%v) disagrees with Origin.Untrusted() (%v)", got.Untrusted, got.Origin.Untrusted())
			}
		})
	}
}

func TestParseOwnerRepo(t *testing.T) {
	cases := []struct {
		source string
		owner  string
		repo   string
		ok     bool
	}{
		{"owner/repo", "owner", "repo", true},
		{"owner/repo.git", "owner", "repo", true},
		{"Owner-1/repo_2.x", "Owner-1", "repo_2.x", true},
		{"  owner/repo  ", "owner", "repo", true},
		{"owner", "", "", false},
		{"owner/repo/extra", "", "", false},
		{"./owner/repo", "", "", false},
		{"https://github.com/owner/repo", "", "", false},
		{"git@github.com:owner/repo.git", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		owner, repo, ok := ParseOwnerRepo(tc.source)
		if ok != tc.ok || owner != tc.owner || repo != tc.repo {
			t.Errorf("ParseOwnerRepo(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.source, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}

func TestIsLocalPath(t *testing.T) {
	local := []string{"./x", "/x", "../x", "~/x", "  ./x"}
	remote := []string{"owner/repo", "https://github.com/o/r", "git@github.com:o/r.git", "x"}
	for _, s := range local {
		if !IsLocalPath(s) {
			t.Errorf("IsLocalPath(%q) = false, want true", s)
		}
	}
	for _, s := range remote {
		if IsLocalPath(s) {
			t.Errorf("IsLocalPath(%q) = true, want false", s)
		}
	}
}

func TestOriginUntrusted(t *testing.T) {
	trusted := []Origin{OriginLocalPath, OriginOwnRepo}
	untrusted := []Origin{OriginPublicRepo, OriginRemoteGit, OriginDiscover, Origin("something-new")}
	for _, o := range trusted {
		if o.Untrusted() {
			t.Errorf("%q.Untrusted() = true, want false", o)
		}
	}
	for _, o := range untrusted {
		if !o.Untrusted() {
			t.Errorf("%q.Untrusted() = false, want true", o)
		}
	}
}

// TestAssessBlankOwnerIsNeverTrusted covers the degenerate case where the
// configured registry repo has no owner: a blank entry must not make a blank
// URL owner "the user's own".
func TestAssessBlankOwnerIsNeverTrusted(t *testing.T) {
	got := Assess("https://github.com/openclaw/openclaw/tree/main/skills/x", Options{Owners: []string{"", "  "}})
	if !got.Untrusted {
		t.Fatalf("a blank configured owner produced a trusted verdict: %+v", got)
	}
}

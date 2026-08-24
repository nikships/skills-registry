package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// skillNetBody is a trimmed copy of a real SkillNet response, keeping the
// nesting (`evaluation.<score>.level`) and the fields this package ignores
// (`stars`, per-score `reason`, `cost_awareness`) so the mapping is exercised
// against the shape the live index actually returns.
const skillNetBody = `{
  "data": [
    {
      "skill_name": "summarize",
      "skill_description": "Summarize or transcribe URLs, videos, articles, and PDFs.",
      "author": "openclaw",
      "stars": 372728,
      "skill_url": "https://github.com/openclaw/openclaw/blob/1300b2263027f1f9a72a2e5fbde2fd272536461f/skills/summarize",
      "category": "AIGC",
      "evaluation": {
        "safety": {"level": "Good", "reason": "benign"},
        "completeness": {"level": "Good", "reason": "thorough"},
        "executability": {"level": "Average", "reason": "mostly clear"},
        "cost_awareness": {"level": "Good", "reason": "cheap"}
      }
    },
    {
      "skill_name": "nano-pdf",
      "skill_description": "Edit PDFs with natural-language instructions.",
      "author": "clawdbot",
      "stars": 230194,
      "skill_url": "https://github.com/clawdbot/clawdbot/blob/02aeff8/skills/nano-pdf",
      "category": "Productivity",
      "evaluation": {
        "safety": {"level": "Poor", "reason": "writes files"}
      }
    }
  ],
  "meta": {"query": "pdf", "mode": "keyword", "total": 41692},
  "success": true
}`

// newTestClient serves handler and returns a Client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL + "/v1/search"}
}

func TestSearchKeywordModeSendsExpectedParams(t *testing.T) {
	var got struct {
		path   string
		params map[string]string
	}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.params = map[string]string{}
		for k, v := range r.URL.Query() {
			got.params[k] = v[0]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(skillNetBody))
	})

	resp, err := client.Search(context.Background(), Query{Text: "pdf", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.path != "/v1/search" {
		t.Errorf("path = %q, want /v1/search", got.path)
	}
	for key, want := range map[string]string{"q": "pdf", "mode": "keyword", "limit": "10"} {
		if got.params[key] != want {
			t.Errorf("param %s = %q, want %q", key, got.params[key], want)
		}
	}
	if _, ok := got.params["category"]; ok {
		t.Errorf("category must be omitted when unset, got %q", got.params["category"])
	}
	if resp.Source != SourceSkillNet {
		t.Errorf("source = %q, want %q", resp.Source, SourceSkillNet)
	}
	if resp.Query != "pdf" || resp.Mode != "keyword" {
		t.Errorf("echoed query/mode = %q/%q, want pdf/keyword", resp.Query, resp.Mode)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(resp.Results))
	}
	first := resp.Results[0]
	if first.Name != "summarize" || first.Author != "openclaw" || first.Category != "AIGC" {
		t.Errorf("row[0] identity mismatch: %+v", first)
	}
	if first.Safety != "Good" || first.Completeness != "Good" || first.Executability != "Average" {
		t.Errorf("row[0] scores = %q/%q/%q, want Good/Good/Average",
			first.Safety, first.Completeness, first.Executability)
	}
	if !strings.HasPrefix(first.SkillURL, "https://github.com/openclaw/openclaw/blob/") {
		t.Errorf("row[0] skill_url = %q, want the github blob URL `add` accepts", first.SkillURL)
	}
}

// TestSearchAbsentScoresStayEmpty pins the "unscored is not safe" rule: a row
// whose evaluation omits completeness and executability must leave those
// fields empty rather than defaulting to a passing grade.
func TestSearchAbsentScoresStayEmpty(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(skillNetBody))
	})
	resp, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	second := resp.Results[1]
	if second.Safety != "Poor" {
		t.Errorf("safety = %q, want Poor", second.Safety)
	}
	if second.Completeness != "" || second.Executability != "" {
		t.Errorf("absent scores = %q/%q, want empty (unscored, never assumed good)",
			second.Completeness, second.Executability)
	}
}

func TestSearchVectorModeSendsModeVector(t *testing.T) {
	var mode, category string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mode = r.URL.Query().Get("mode")
		category = r.URL.Query().Get("category")
		_, _ = w.Write([]byte(`{"data":[],"success":true}`))
	})
	resp, err := client.Search(context.Background(), Query{
		Text:     "pdf",
		Mode:     ModeVector,
		Category: "Productivity",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != "vector" {
		t.Errorf("mode = %q, want vector", mode)
	}
	if category != "Productivity" {
		t.Errorf("category = %q, want Productivity", category)
	}
	if resp.Mode != "vector" {
		t.Errorf("echoed mode = %q, want vector", resp.Mode)
	}
}

// TestSearchNeverSendsCredentials is the security assertion behind the plain
// HTTP transport: the request must carry no token, no GitHub auth, and no
// cookie, so a plaintext hop leaks nothing but the query.
func TestSearchNeverSendsCredentials(t *testing.T) {
	forbidden := []string{
		"Authorization",
		"Proxy-Authorization",
		"Cookie",
		"X-Github-Token",
		"X-Api-Key",
		"Private-Token",
	}
	var seen http.Header
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		_, _ = w.Write([]byte(skillNetBody))
	})
	t.Setenv("GH_TOKEN", "ghp_should_never_be_forwarded")
	t.Setenv("GITHUB_TOKEN", "ghp_should_never_be_forwarded")

	if _, err := client.Search(context.Background(), Query{Text: "pdf"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range forbidden {
		if v := seen.Get(h); v != "" {
			t.Errorf("request carried %s: %q — credentials must never reach the index", h, v)
		}
	}
	for name, values := range seen {
		for _, v := range values {
			if strings.Contains(v, "ghp_") {
				t.Errorf("header %s leaked a GitHub token: %q", name, v)
			}
		}
	}
	if ua := seen.Get("User-Agent"); ua != userAgent {
		t.Errorf("User-Agent = %q, want %q", ua, userAgent)
	}
}

// TestSearchOmitsQueryCredentials guards the other half of the transport
// promise: no token ends up in the URL either.
func TestSearchOmitsQueryCredentials(t *testing.T) {
	var raw string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	if _, err := client.Search(context.Background(), Query{Text: "pdf"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, key := range []string{"token", "access_token", "api_key", "authorization"} {
		if strings.Contains(strings.ToLower(raw), key) {
			t.Errorf("query %q contains credential parameter %q", raw, key)
		}
	}
}

func TestSearchEmptyResultsAreNonNilSlice(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0},"success":true}`))
	})
	resp, err := client.Search(context.Background(), Query{Text: "nothing-matches-this"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Results == nil {
		t.Fatal("results must be an empty slice, never nil (encodes as [] not null)")
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"source":"skillnet","query":"nothing-matches-this","mode":"keyword","results":[]}`
	if string(body) != want {
		t.Fatalf("payload = %s, want %s", body, want)
	}
}

// TestSearchNullDataIsEmpty covers `"data": null`, which an index error path
// can emit instead of an empty array.
func TestSearchNullDataIsEmpty(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"success":true}`))
	})
	resp, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 || resp.Results == nil {
		t.Fatalf("results = %#v, want empty non-nil slice", resp.Results)
	}
}

func TestSearchTimeoutFailsClosed(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(skillNetBody))
	})
	client.HTTPClient = &http.Client{Timeout: 20 * time.Millisecond}

	resp, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err == nil {
		t.Fatal("a timed-out request must return an error, not partial results")
	}
	if len(resp.Results) != 0 {
		t.Errorf("failed search returned %d results, want none", len(resp.Results))
	}
}

func TestSearchNonJSONBodyFailsClosed(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>gateway</body></html>"))
	})
	_, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err == nil {
		t.Fatal("a non-JSON body must return an error")
	}
	if !strings.Contains(err.Error(), "non-JSON") {
		t.Errorf("error = %v, want it to name the non-JSON response", err)
	}
}

func TestSearchServerErrorFailsClosed(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	})
	_, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err == nil {
		t.Fatal("HTTP 502 must return an error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %v, want it to name the status code", err)
	}
}

func TestSearchUnreachableHostFailsClosed(t *testing.T) {
	// Bind, capture the URL, then close so the port is refused.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL + "/v1/search"
	srv.Close()

	client := &Client{BaseURL: dead}
	if _, err := client.Search(context.Background(), Query{Text: "pdf"}); err == nil {
		t.Fatal("an unreachable index must return an error")
	}
}

func TestSearchDedupesIdenticalRows(t *testing.T) {
	body := `{"data":[
	  {"skill_name":"pdf","skill_url":"https://github.com/a/b/blob/sha/skills/pdf","author":"a"},
	  {"skill_name":"pdf","skill_url":"https://github.com/a/b/blob/sha/skills/pdf","author":"a"},
	  {"skill_name":"pdf","skill_url":"https://github.com/c/d/blob/sha/skills/pdf","author":"c"},
	  {"skill_name":"other","skill_url":"https://github.com/a/b/blob/sha/skills/pdf","author":"a"}
	]}`
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	resp, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("results = %d, want 3 (only the exact (name, skill_url) clone is dropped): %+v",
			len(resp.Results), resp.Results)
	}
	// Order must follow the index's own ranking, first occurrence wins.
	if resp.Results[0].Author != "a" || resp.Results[1].Author != "c" || resp.Results[2].Name != "other" {
		t.Errorf("dedup reordered results: %+v", resp.Results)
	}
}

// TestSearchDropsIdentitylessRows keeps a malformed row out of the table
// rather than rendering a blank line a user cannot act on.
func TestSearchDropsIdentitylessRows(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"author":"ghost"},{"skill_name":"real","skill_url":"u"}]}`))
	})
	resp, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Name != "real" {
		t.Fatalf("results = %+v, want only the row with an identity", resp.Results)
	}
}

func TestSearchLimitIsCappedAndDefaulted(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  string
	}{
		{"zero defaults", 0, "10"},
		{"negative defaults", -5, "10"},
		{"under the cap passes through", 25, "25"},
		{"at the cap passes through", 50, "50"},
		{"over the cap is clamped", 500, "50"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("limit")
				_, _ = w.Write([]byte(`{"data":[]}`))
			})
			if _, err := client.Search(context.Background(), Query{Text: "pdf", Limit: tc.limit}); err != nil {
				t.Fatalf("Search: %v", err)
			}
			if got != tc.want {
				t.Errorf("limit = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	called := false
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	if _, err := client.Search(context.Background(), Query{Text: "   "}); err == nil {
		t.Fatal("a blank query must be rejected before any request is made")
	}
	if called {
		t.Error("a blank query must not reach the index")
	}
}

func TestParseMode(t *testing.T) {
	cases := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"", ModeKeyword, false},
		{"keyword", ModeKeyword, false},
		{"KEYWORD", ModeKeyword, false},
		{" vector ", ModeVector, false},
		{"semantic", "", true},
	}
	for _, tc := range cases {
		got, err := ParseMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q) = %q, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBaseURLPrefersEnvOverride(t *testing.T) {
	t.Setenv(BaseURLEnv, "")
	if got := BaseURL(); got != DefaultBaseURL {
		t.Errorf("BaseURL() = %q, want %q", got, DefaultBaseURL)
	}
	t.Setenv(BaseURLEnv, " http://127.0.0.1:9/v1/search ")
	if got := BaseURL(); got != "http://127.0.0.1:9/v1/search" {
		t.Errorf("BaseURL() = %q, want the trimmed override", got)
	}
	if got := New().BaseURL; got != "http://127.0.0.1:9/v1/search" {
		t.Errorf("New().BaseURL = %q, want the override", got)
	}
}

// TestNewFallsBackToDefaultEndpoint documents that the shipped default is the
// public SkillNet endpoint over plain HTTP (its certificate does not match the
// host), and that no credential is attached to compensate.
func TestNewFallsBackToDefaultEndpoint(t *testing.T) {
	t.Setenv(BaseURLEnv, "")
	if got := New().BaseURL; got != DefaultBaseURL {
		t.Fatalf("New().BaseURL = %q, want %q", got, DefaultBaseURL)
	}
	if !strings.HasPrefix(DefaultBaseURL, "http://") {
		t.Fatalf("DefaultBaseURL = %q; the index is plain HTTP by necessity", DefaultBaseURL)
	}
}

// TestSearchPreservesEndpointQueryParams lets a mirror or test server pin its
// own parameters without them being dropped.
func TestSearchPreservesEndpointQueryParams(t *testing.T) {
	var params map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params = r.URL.Query()
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	client := &Client{BaseURL: srv.URL + "/v1/search?tenant=mirror"}
	if _, err := client.Search(context.Background(), Query{Text: "pdf"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := params["tenant"]; len(got) != 1 || got[0] != "mirror" {
		t.Errorf("tenant param = %v, want [mirror]", got)
	}
	if got := params["q"]; len(got) != 1 || got[0] != "pdf" {
		t.Errorf("q param = %v, want [pdf]", got)
	}
}

func TestSearchInvalidBaseURLFailsClosed(t *testing.T) {
	client := &Client{BaseURL: "http://[::1"}
	if _, err := client.Search(context.Background(), Query{Text: "pdf"}); err == nil {
		t.Fatal("a malformed endpoint must return an error")
	}
}

// TestResultFieldOrder pins the published JSON contract the TUI, the hub, the
// gateway skill, and the macOS app all read.
func TestResultFieldOrder(t *testing.T) {
	body, err := json.Marshal(Response{
		Source: SourceSkillNet,
		Query:  "pdf",
		Mode:   "keyword",
		Results: []Result{{
			Name:          "summarize",
			Description:   "d",
			Author:        "openclaw",
			Category:      "AIGC",
			SkillURL:      "https://github.com/openclaw/openclaw/blob/sha/skills/summarize",
			Safety:        "Good",
			Completeness:  "Good",
			Executability: "Good",
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"source":"skillnet","query":"pdf","mode":"keyword","results":[` +
		`{"name":"summarize","description":"d","author":"openclaw","category":"AIGC",` +
		`"skill_url":"https://github.com/openclaw/openclaw/blob/sha/skills/summarize",` +
		`"safety":"Good","completeness":"Good","executability":"Good"}]}`
	if string(body) != want {
		t.Fatalf("payload =\n%s\nwant\n%s", body, want)
	}
}

// TestSearchDropsStarsAndProse pins the deliberate omissions: repository star
// counts are not a signal about an individual skill, and the index's score
// prose is not part of this contract.
func TestSearchDropsStarsAndProse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(skillNetBody))
	})
	resp, err := client.Search(context.Background(), Query{Text: "pdf"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, dropped := range []string{"stars", "372728", "reason", "cost_awareness", "maintainability"} {
		if strings.Contains(string(body), dropped) {
			t.Errorf("payload leaked %q from the index blob: %s", dropped, body)
		}
	}
}

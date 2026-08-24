package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/discover"
	"github.com/nikships/skills-registry/cli/internal/jsonout"
)

// discoverBody is a two-row index response covering both the fully-graded and
// the partially-graded case.
const discoverBody = `{
  "data": [
    {
      "skill_name": "summarize",
      "skill_description": "Summarize URLs, videos, articles, and PDFs.",
      "author": "openclaw",
      "stars": 372728,
      "skill_url": "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize",
      "category": "AIGC",
      "evaluation": {
        "safety": {"level": "Good"},
        "completeness": {"level": "Good"},
        "executability": {"level": "Good"}
      }
    },
    {
      "skill_name": "nano-pdf",
      "skill_description": "Edit PDFs with natural-language instructions.",
      "author": "clawdbot",
      "stars": 230194,
      "skill_url": "https://github.com/clawdbot/clawdbot/blob/02aeff8/skills/nano-pdf",
      "category": "Productivity",
      "evaluation": {}
    }
  ],
  "success": true
}`

// serveDiscover points SKILLS_DISCOVER_URL at an httptest server for the
// duration of the test, so no request ever reaches the live index. It returns
// the requests the handler saw.
func serveDiscover(t *testing.T, handler http.HandlerFunc) *[]*http.Request {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clone := r.Clone(r.Context())
		seen = append(seen, clone)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(discover.BaseURLEnv, srv.URL+"/v1/search")
	return &seen
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// enableJSON flips the persistent --json flag for one test.
func enableJSON(t *testing.T) {
	t.Helper()
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)
}

// TestRunDiscoverJSONEmitsPublishedContract pins the payload the Discover TUI,
// the hub card, the gateway skill, and the macOS app all consume.
func TestRunDiscoverJSONEmitsPublishedContract(t *testing.T) {
	enableJSON(t)
	seen := serveDiscover(t, jsonHandler(discoverBody))
	buf := captureJSONOut(t)

	err := runDiscoverJSON(context.Background(), discover.Query{Text: "pdf", Limit: 10})
	if err != nil {
		t.Fatalf("runDiscoverJSON: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if strings.Contains(out, "\n") {
		t.Fatalf("output must be single-line, got %q", out)
	}
	var payload discover.Response
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if payload.Source != "skillnet" || payload.Query != "pdf" || payload.Mode != "keyword" {
		t.Errorf("envelope = %+v, want source=skillnet query=pdf mode=keyword", payload)
	}
	if len(payload.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(payload.Results))
	}
	if payload.Results[0].SkillURL != "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize" {
		t.Errorf("skill_url = %q, want the github blob URL `add` accepts", payload.Results[0].SkillURL)
	}
	// An ungraded row keeps empty scores; downstream renders that as
	// "unscored", never as safe.
	if payload.Results[1].Safety != "" {
		t.Errorf("ungraded safety = %q, want empty", payload.Results[1].Safety)
	}
	// Stars are dropped on purpose: they are the host repo's, not the skill's.
	if strings.Contains(out, "372728") || strings.Contains(out, "stars") {
		t.Errorf("payload leaked repository stars: %s", out)
	}
	if len(*seen) != 1 {
		t.Fatalf("expected exactly one index request, got %d", len(*seen))
	}
	params := (*seen)[0].URL.Query()
	if params.Get("q") != "pdf" || params.Get("mode") != "keyword" || params.Get("limit") != "10" {
		t.Errorf("request params = %v, want q=pdf mode=keyword limit=10", params)
	}
}

func TestRunDiscoverJSONVectorModeAndCategory(t *testing.T) {
	enableJSON(t)
	seen := serveDiscover(t, jsonHandler(`{"data":[],"success":true}`))
	buf := captureJSONOut(t)

	err := runDiscoverJSON(context.Background(), discover.Query{
		Text:     "summarize a video",
		Mode:     discover.ModeVector,
		Category: "Productivity",
	})
	if err != nil {
		t.Fatalf("runDiscoverJSON: %v", err)
	}
	params := (*seen)[0].URL.Query()
	if params.Get("mode") != "vector" {
		t.Errorf("mode = %q, want vector", params.Get("mode"))
	}
	if params.Get("category") != "Productivity" {
		t.Errorf("category = %q, want Productivity", params.Get("category"))
	}
	want := `{"source":"skillnet","query":"summarize a video","mode":"vector","results":[]}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("payload = %s, want %s", got, want)
	}
}

// TestRunDiscoverJSONSendsNoCredentials is the transport guarantee behind
// using plain HTTP: even with GitHub tokens in the environment, the request
// carries no credential header.
func TestRunDiscoverJSONSendsNoCredentials(t *testing.T) {
	enableJSON(t)
	t.Setenv("GH_TOKEN", "ghp_never_forward_this")
	t.Setenv("GITHUB_TOKEN", "ghp_never_forward_this")
	seen := serveDiscover(t, jsonHandler(discoverBody))
	captureJSONOut(t)

	if err := runDiscoverJSON(context.Background(), discover.Query{Text: "pdf"}); err != nil {
		t.Fatalf("runDiscoverJSON: %v", err)
	}
	headers := (*seen)[0].Header
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-Github-Token", "X-Api-Key"} {
		if v := headers.Get(h); v != "" {
			t.Errorf("request carried %s = %q; the index must never receive credentials", h, v)
		}
	}
	for name, values := range headers {
		for _, v := range values {
			if strings.Contains(v, "ghp_") {
				t.Errorf("header %s leaked a token: %q", name, v)
			}
		}
	}
	if raw := (*seen)[0].URL.RawQuery; strings.Contains(raw, "ghp_") || strings.Contains(strings.ToLower(raw), "token") {
		t.Errorf("query string leaked a credential: %q", raw)
	}
}

func TestRunDiscoverJSONCapsLimit(t *testing.T) {
	enableJSON(t)
	seen := serveDiscover(t, jsonHandler(`{"data":[]}`))
	captureJSONOut(t)

	if err := runDiscoverJSON(context.Background(), discover.Query{Text: "pdf", Limit: 5000}); err != nil {
		t.Fatalf("runDiscoverJSON: %v", err)
	}
	if got := (*seen)[0].URL.Query().Get("limit"); got != "50" {
		t.Errorf("limit = %q, want 50 (the cap)", got)
	}
}

func TestRunDiscoverJSONDedupesClones(t *testing.T) {
	enableJSON(t)
	body := `{"data":[
	  {"skill_name":"pdf","skill_url":"https://github.com/a/b/blob/sha/skills/pdf","author":"a"},
	  {"skill_name":"pdf","skill_url":"https://github.com/a/b/blob/sha/skills/pdf","author":"a"},
	  {"skill_name":"pdf","skill_url":"https://github.com/c/d/blob/sha/skills/pdf","author":"c"}
	]}`
	serveDiscover(t, jsonHandler(body))
	buf := captureJSONOut(t)

	if err := runDiscoverJSON(context.Background(), discover.Query{Text: "pdf"}); err != nil {
		t.Fatalf("runDiscoverJSON: %v", err)
	}
	var payload discover.Response
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(payload.Results) != 2 {
		t.Fatalf("results = %d, want 2 after dedup: %+v", len(payload.Results), payload.Results)
	}
}

// TestRunDiscoverJSONFailsClosed covers every remote failure mode: the JSON
// path must emit {"error": …} and return a non-nil error (which main turns
// into exit 1) with no results object.
func TestRunDiscoverJSONFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"http 500", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}},
		{"http 502", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}},
		{"non-JSON body", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>gateway timeout</html>"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enableJSON(t)
			serveDiscover(t, tc.handler)
			buf := captureJSONOut(t)

			err := runDiscoverJSON(context.Background(), discover.Query{Text: "pdf"})
			if err == nil {
				t.Fatal("expected a non-nil error so the process exits non-zero")
			}
			out := strings.TrimSpace(buf.String())
			var payload struct {
				Error   string `json:"error"`
				Results []any  `json:"results"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("error output must be valid JSON, got %q", out)
			}
			if payload.Error == "" {
				t.Errorf("error field empty in %q", out)
			}
			if payload.Results != nil {
				t.Errorf("failure payload must carry no results, got %q", out)
			}
		})
	}
}

// TestRunDiscoverJSONUnreachableFailsClosed exercises the "index is down"
// path against a closed port rather than a served error.
func TestRunDiscoverJSONUnreachableFailsClosed(t *testing.T) {
	enableJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL + "/v1/search"
	srv.Close()
	t.Setenv(discover.BaseURLEnv, dead)
	buf := captureJSONOut(t)

	if err := runDiscoverJSON(context.Background(), discover.Query{Text: "pdf"}); err == nil {
		t.Fatal("an unreachable index must return an error")
	}
	if !strings.Contains(buf.String(), `"error"`) {
		t.Errorf("expected an {\"error\": …} payload, got %q", buf.String())
	}
}

// TestRunDiscoverHumanErrorPointsAtAdd pins the human-readable failure: it
// must tell the user they can still import a URL directly.
func TestRunDiscoverHumanErrorPointsAtAdd(t *testing.T) {
	serveDiscover(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	var out bytes.Buffer

	err := runDiscover(context.Background(), &out, discover.Query{Text: "pdf"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "skills-registry add <github-url>") {
		t.Errorf("error must point the user at `add`, got: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a failed search must print no table, got %q", out.String())
	}
}

func TestRunDiscoverHumanRendersTable(t *testing.T) {
	serveDiscover(t, jsonHandler(discoverBody))
	var out bytes.Buffer

	if err := runDiscover(context.Background(), &out, discover.Query{Text: "pdf"}); err != nil {
		t.Fatalf("runDiscover: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"2 results",
		`for "pdf" (keyword mode)`,
		"NAME", "CATEGORY", "SAFETY", "AUTHOR", "URL",
		"summarize", "AIGC", "Good", "openclaw",
		"https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize",
		"nano-pdf", "Productivity", "clawdbot",
		"https://github.com/clawdbot/clawdbot/blob/02aeff8/skills/nano-pdf",
		"skills-registry add <URL>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// An ungraded skill must read as unscored, never as a blank cell that
	// looks like a pass.
	if !strings.Contains(got, unscoredLabel) {
		t.Errorf("ungraded row must render %q:\n%s", unscoredLabel, got)
	}
	// Repository stars are never surfaced.
	if strings.Contains(got, "372728") || strings.Contains(strings.ToUpper(got), "STARS") {
		t.Errorf("output must not headline repository stars:\n%s", got)
	}
	// The URL column must survive intact for copy-paste into `add`.
	if strings.Contains(got, "…/skills") {
		t.Errorf("URLs must never be truncated:\n%s", got)
	}
}

func TestRunDiscoverHumanEmptyResults(t *testing.T) {
	serveDiscover(t, jsonHandler(`{"data":[],"success":true}`))
	var out bytes.Buffer

	if err := runDiscover(context.Background(), &out, discover.Query{Text: "nothing-here"}); err != nil {
		t.Fatalf("runDiscover: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "0 results") {
		t.Errorf("expected a zero-result headline:\n%s", got)
	}
	if !strings.Contains(got, `Nothing in the index matched "nothing-here"`) {
		t.Errorf("expected an explicit empty-state line:\n%s", got)
	}
	if !strings.Contains(got, "--mode vector") {
		t.Errorf("empty keyword search should suggest vector mode:\n%s", got)
	}
	if strings.Contains(got, "NAME") {
		t.Errorf("no table header should print with zero results:\n%s", got)
	}
}

// TestRunDiscoverHumanEmptyVectorOmitsVectorHint avoids suggesting the mode
// the user already used.
func TestRunDiscoverHumanEmptyVectorOmitsVectorHint(t *testing.T) {
	serveDiscover(t, jsonHandler(`{"data":[]}`))
	var out bytes.Buffer

	err := runDiscover(context.Background(), &out, discover.Query{Text: "x", Mode: discover.ModeVector})
	if err != nil {
		t.Fatalf("runDiscover: %v", err)
	}
	if strings.Contains(out.String(), "--mode vector") {
		t.Errorf("vector search must not suggest vector mode again:\n%s", out.String())
	}
}

func TestRunDiscoverRejectsUnknownMode(t *testing.T) {
	seen := serveDiscover(t, jsonHandler(discoverBody))
	var out bytes.Buffer

	err := runDiscover(context.Background(), &out, discover.Query{Text: "pdf", Mode: "semantic"})
	if err == nil {
		t.Fatal("an unknown --mode must be rejected")
	}
	if !strings.Contains(err.Error(), "semantic") {
		t.Errorf("error should name the bad mode: %v", err)
	}
	if len(*seen) != 0 {
		t.Errorf("an invalid mode must not reach the index, saw %d requests", len(*seen))
	}
}

// TestNewDiscoverCmdSurface pins the command's flags, argument count, and the
// facts the Long help must state.
func TestNewDiscoverCmdSurface(t *testing.T) {
	cmd := newDiscoverCmd()
	if cmd.Name() != "discover" {
		t.Fatalf("name = %q, want discover", cmd.Name())
	}
	if cmd.Long == "" {
		t.Fatal("discover needs a Long help string")
	}
	for _, want := range []string{
		"plain HTTP",
		"no credentials",
		discover.BaseURLEnv,
		"--mode vector",
		unscoredLabel,
	} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long help must mention %q:\n%s", want, cmd.Long)
		}
	}
	for name, want := range map[string]string{
		"mode":     "keyword",
		"category": "",
		"limit":    "10",
	} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("missing --%s flag", name)
			continue
		}
		if f.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, f.DefValue, want)
		}
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("discover with no query must fail argument validation")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("discover takes exactly one query argument")
	}
	if err := cmd.Args(cmd, []string{"pdf"}); err != nil {
		t.Errorf("discover pdf should validate: %v", err)
	}
}

// TestDiscoverCmdFailureOmitsUsageBlock guards the failure ergonomics: an
// unreachable index is not a misuse of the command, so the multi-line usage
// dump and cobra's duplicate error line must stay out of the output. Only the
// argument-count check (which runs before RunE) should print usage.
func TestDiscoverCmdFailureOmitsUsageBlock(t *testing.T) {
	serveDiscover(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	var out bytes.Buffer
	cmd := newDiscoverCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"pdf"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected a non-nil error from a 503")
	}
	if got := out.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Flags:") {
		t.Errorf("a remote failure must not dump usage:\n%s", got)
	}
	if !cmd.SilenceUsage || !cmd.SilenceErrors {
		t.Errorf("SilenceUsage=%v SilenceErrors=%v, want both true so main prints the error once",
			cmd.SilenceUsage, cmd.SilenceErrors)
	}
}

// TestDiscoverCmdJSONFailureKeepsStdoutParseable pins the machine-readable
// failure path end-to-end: stdout must hold exactly the {"error": …} object,
// with no usage text mixed in for a consumer's parser to choke on.
func TestDiscoverCmdJSONFailureKeepsStdoutParseable(t *testing.T) {
	enableJSON(t)
	serveDiscover(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	stdout := captureJSONOut(t)
	var cobraOut bytes.Buffer
	cmd := newDiscoverCmd()
	cmd.SetOut(&cobraOut)
	cmd.SetErr(&cobraOut)
	cmd.SetArgs([]string{"pdf"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected a non-nil error so the process exits 1")
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout must be a single parseable JSON object, got %q", stdout.String())
	}
	if payload["error"] == "" || payload["error"] == nil {
		t.Errorf("missing error field in %q", stdout.String())
	}
	if strings.Contains(cobraOut.String(), "Usage:") {
		t.Errorf("no usage block should print on a remote failure:\n%s", cobraOut.String())
	}
}

// TestRootRegistersDiscover guards the wiring: the command must be reachable
// from the root and listed in its help.
func TestRootRegistersDiscover(t *testing.T) {
	root := newRootCmd()
	found, _, err := root.Find([]string{"discover"})
	if err != nil {
		t.Fatalf("Find(discover): %v", err)
	}
	if found.Name() != "discover" {
		t.Fatalf("root does not resolve `discover`, got %q", found.Name())
	}
	if !strings.Contains(root.Long, "discover") {
		t.Error("root Long help should list discover next to search")
	}
}

func TestScoreLabelNamesMissingGrades(t *testing.T) {
	for in, want := range map[string]string{
		"Good":    "Good",
		"Average": "Average",
		"Poor":    "Poor",
		"":        unscoredLabel,
		"   ":     unscoredLabel,
	} {
		if got := scoreLabel(in); got != want {
			t.Errorf("scoreLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClipSlicesRunesNotBytes(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"truncate-me-please", 10, "truncate-…"},
		{"日本語のスキル名です", 5, "日本語の…"},
		{"abc", 1, "a"},
	}
	for _, tc := range cases {
		if got := clip(tc.in, tc.width); got != tc.want {
			t.Errorf("clip(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

func TestPadCountsRunes(t *testing.T) {
	if got := pad("日本", 4); got != "日本  " {
		t.Errorf("pad = %q, want two trailing spaces", got)
	}
	if got := pad("toolong", 3); got != "toolong" {
		t.Errorf("pad must never truncate, got %q", got)
	}
}

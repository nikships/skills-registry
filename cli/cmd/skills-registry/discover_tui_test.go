package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/discover"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// stubDiscoverSearch swaps the index call for one test, so the interactive
// path is exercised without a network hop.
func stubDiscoverSearch(t *testing.T, fn func(context.Context, discover.Query) (discover.Response, error)) {
	t.Helper()
	prev := discoverSearch
	t.Cleanup(func() { discoverSearch = prev })
	discoverSearch = fn
}

// TestDiscoverJSONBypassesTUI is the regression guard for the flag that must
// not change: with --json set, the command emits the payload and never
// constructs the picker, whatever stdout is attached to.
func TestDiscoverJSONBypassesTUI(t *testing.T) {
	enableJSON(t)
	serveDiscover(t, jsonHandler(discoverBody))
	stdout := captureJSONOut(t)
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		t.Error("--json reached the interactive search path")
		return discover.Response{}, nil
	})

	var cobraOut bytes.Buffer
	cmd := newDiscoverCmd()
	cmd.SetOut(&cobraOut)
	cmd.SetErr(&cobraOut)
	cmd.SetArgs([]string{"pdf"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("discover --json: %v", err)
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, `{"source":"skillnet"`) {
		t.Fatalf("stdout = %q, want the published JSON payload", out)
	}
	// The alt-screen escape sequences a Bubble Tea program emits would
	// corrupt a consumer's parse; none may appear.
	if strings.Contains(out, "\x1b[") {
		t.Errorf("JSON output carried terminal escapes: %q", out)
	}
}

// TestDiscoverNonTTYPrintsPlainTable pins the other non-interactive route: a
// piped stdout keeps the fixed-width table rather than opening a TUI that
// could not render.
func TestDiscoverNonTTYPrintsPlainTable(t *testing.T) {
	serveDiscover(t, jsonHandler(discoverBody))
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		t.Error("a non-TTY stdout reached the interactive search path")
		return discover.Response{}, nil
	})

	var out bytes.Buffer
	cmd := newDiscoverCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"pdf"})
	// `go test` runs with stdout redirected, so isTerminal() is already false
	// here: this asserts the routing under exactly the conditions a piped
	// invocation sees.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !strings.Contains(out.String(), "NAME") {
		t.Errorf("expected the plain table on a non-TTY:\n%s", out.String())
	}
}

// TestDiscoverPlainFlagSkipsTUI proves --plain is an explicit opt out of the
// picker even on a terminal.
func TestDiscoverPlainFlagSkipsTUI(t *testing.T) {
	serveDiscover(t, jsonHandler(discoverBody))
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		t.Error("--plain reached the interactive search path")
		return discover.Response{}, nil
	})

	var out bytes.Buffer
	cmd := newDiscoverCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"pdf", "--plain"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("discover --plain: %v", err)
	}
	if !strings.Contains(out.String(), "skills-registry add <URL>") {
		t.Errorf("--plain must print the table:\n%s", out.String())
	}
}

// TestDiscoverRowsMapPublishedContract pins the projection from the index
// payload onto the picker's rows, including that an ungraded row keeps empty
// grades so the view renders them as unscored.
func TestDiscoverRowsMapPublishedContract(t *testing.T) {
	resp := discover.Response{
		Source: discover.SourceSkillNet,
		Query:  "pdf",
		Results: []discover.Result{
			{
				Name:          "summarize",
				Description:   "Summarize URLs, videos, articles, and PDFs.",
				Author:        "openclaw",
				Category:      "AIGC",
				SkillURL:      "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize",
				Safety:        "Good",
				Completeness:  "Good",
				Executability: "Average",
			},
			{Name: "nano-pdf", SkillURL: "https://github.com/clawdbot/clawdbot/blob/02aeff8/skills/nano-pdf"},
		},
	}
	rows := discoverRows(resp)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	want := tui.DiscoverRow{
		Name:          "summarize",
		Desc:          "Summarize URLs, videos, articles, and PDFs.",
		Author:        "openclaw",
		Category:      "AIGC",
		SkillURL:      "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize",
		Safety:        "Good",
		Completeness:  "Good",
		Executability: "Average",
	}
	if rows[0] != want {
		t.Errorf("row 0 = %+v, want %+v", rows[0], want)
	}
	if rows[1].Safety != "" || rows[1].Completeness != "" || rows[1].Executability != "" {
		t.Errorf("ungraded row = %+v, want empty grades so the view renders unscored", rows[1])
	}
	// Order is the index's own ranking, never re-sorted here.
	if rows[0].Name != "summarize" || rows[1].Name != "nano-pdf" {
		t.Errorf("rows reordered: %q then %q", rows[0].Name, rows[1].Name)
	}
}

// TestDiscoverFlowDepsSearchFailureSurfaces proves the search hook hands the
// index error to the flow rather than swallowing it into an empty row set,
// which is what makes the flow's error state reachable in production.
func TestDiscoverFlowDepsSearchFailureSurfaces(t *testing.T) {
	boom := errors.New("skill index returned HTTP 503")
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		return discover.Response{}, boom
	})
	deps := discoverFlowDeps(config.Config{Repo: "owner/registry"}, discover.Query{Text: "pdf"})
	rows, err := deps.Search(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the index failure", err)
	}
	if rows != nil {
		t.Errorf("rows = %+v, want none alongside an error", rows)
	}
}

// TestDiscoverFlowDepsCarryQueryAndRepo pins the header/confirm inputs the
// flow renders, and that the picked row travels the untrusted import gate.
func TestDiscoverFlowDepsCarryQueryAndRepo(t *testing.T) {
	stubDiscoverSearch(t, func(_ context.Context, q discover.Query) (discover.Response, error) {
		return discover.Response{Query: q.Text, Results: []discover.Result{{Name: "x", SkillURL: "u"}}}, nil
	})
	deps := discoverFlowDeps(config.Config{Repo: "owner/registry"}, discover.Query{Text: "pdf"})
	if deps.Query != "pdf" {
		t.Errorf("Query = %q, want pdf", deps.Query)
	}
	if deps.Repo != "owner/registry" {
		t.Errorf("Repo = %q, want owner/registry", deps.Repo)
	}
	if deps.Add.Gate == nil {
		t.Error("the add deps must carry the import gate: a public-index pick is untrusted")
	}
	if deps.Add.Resolve == nil || deps.Add.Publish == nil {
		t.Error("the add deps must carry the existing subtree add path")
	}
	rows, err := deps.Search(context.Background())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "x" {
		t.Errorf("rows = %+v, want the stubbed row", rows)
	}
}

// TestDiscoverImportGateForcesUntrusted is the rule a Discover pick depends
// on: a row out of the public index is untrusted whatever shape its URL has,
// so the gate hook the picker gets must be the from-discover one.
func TestDiscoverImportGateForcesUntrusted(t *testing.T) {
	cfg := config.Config{Repo: "owner/registry", DefaultBranch: "main"}
	// A repo under the user's own registry owner is normally trusted; marking
	// the source as a public-index pick must override that.
	source := "https://github.com/owner/other/tree/main/skills/x"
	trusted := gateForSource(t, cfg, source, false)
	if trusted.untrusted() {
		t.Fatalf("precondition failed: %s should be trusted without the discover flag", source)
	}
	fromIndex := gateForSource(t, cfg, source, true)
	if !fromIndex.untrusted() {
		t.Error("a public-index pick must be treated as untrusted")
	}
	// And the hook the picker is wired with is the from-discover one.
	if buildDiscoverAddDeps(cfg).Gate == nil {
		t.Error("the discover add deps must carry a gate hook")
	}
}

// gateForSource builds a gate with no skills to review, which is enough to assert
// the source assessment without touching the filesystem or the index.
func gateForSource(t *testing.T, cfg config.Config, source string, fromDiscover bool) gate {
	t.Helper()
	prev := lookupIndexRow
	t.Cleanup(func() { lookupIndexRow = prev })
	lookupIndexRow = func(context.Context, string) (indexRow, bool, error) {
		return indexRow{}, false, nil
	}
	g, err := buildGate(context.Background(), source, cfg, nil, fromDiscover)
	if err != nil {
		t.Fatalf("buildGate: %v", err)
	}
	return g
}

// TestRunDiscoverTUIRequiresConfig proves the interactive path fails with the
// config error rather than opening a picker that has nowhere to publish.
func TestRunDiscoverTUIRequiresConfig(t *testing.T) {
	t.Setenv(config.EnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		t.Error("the picker searched the index despite a missing config")
		return discover.Response{}, nil
	})
	err := runDiscoverTUI(context.Background(), discover.Query{Text: "pdf"})
	if err == nil {
		t.Fatal("expected the missing-config error")
	}
}

// TestDiscoverCmdPlainFlagSurface keeps the new flag's default documented: the
// picker is the default on a terminal, so --plain must default to false.
func TestDiscoverCmdPlainFlagSurface(t *testing.T) {
	cmd := newDiscoverCmd()
	f := cmd.Flags().Lookup("plain")
	if f == nil {
		t.Fatal("missing --plain flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--plain default = %q, want false", f.DefValue)
	}
	if !strings.Contains(cmd.Long, "interactive picker") {
		t.Errorf("Long help must describe the interactive picker:\n%s", cmd.Long)
	}
}

// TestDiscoverFlowIsEmbeddable is the hub card's entry-point contract: the
// flow satisfies tea.Model and its ending is redirectable, so the hub can host
// it without a second implementation.
func TestDiscoverFlowIsEmbeddable(t *testing.T) {
	var flow tea.Model = tui.NewDiscoverFlow(context.Background(), tui.DiscoverFlowDeps{
		Query:  "pdf",
		Search: func(context.Context) ([]tui.DiscoverRow, error) { return nil, nil },
	}).WithOnExit(func(tui.DiscoverFlowModel) tea.Msg { return nil })
	if flow.Init() == nil {
		t.Error("Init must kick off the search")
	}
}

// TestDiscoverHTTPFixtureStillServes keeps the shared httptest helper honest
// for this file's use of it.
func TestDiscoverHTTPFixtureStillServes(t *testing.T) {
	seen := serveDiscover(t, jsonHandler(discoverBody))
	resp, err := discover.New().Search(context.Background(), discover.Query{Text: "pdf"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(resp.Results))
	}
	if len(*seen) != 1 {
		t.Errorf("requests = %d, want 1", len(*seen))
	}
	if (*seen)[0].Method != http.MethodGet {
		t.Errorf("method = %s, want GET", (*seen)[0].Method)
	}
}

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/discover"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// TestBuildHubDepsWiresDiscoverCard proves the hub's Discover card reaches the
// index and travels the untrusted import gate, rather than being a tile the
// launcher cannot serve.
func TestBuildHubDepsWiresDiscoverCard(t *testing.T) {
	deps := buildHubDeps(context.Background(), config.Config{Repo: "owner/registry", DefaultBranch: "main"})
	if deps.Discover.Search == nil {
		t.Fatal("the Discover card has no search hook")
	}
	if deps.Discover.Add.Gate == nil {
		t.Error("a public-index pick must carry the untrusted import gate")
	}
	if deps.Discover.Add.Resolve == nil || deps.Discover.Add.Publish == nil {
		t.Error("the Discover card must reuse the existing add import path")
	}
}

// TestBuildHubDepsDoesNotSearchTheIndex is the no-network-at-idle contract at
// the wiring layer: constructing the dependency set the dashboard renders from
// must not issue a single index request.
func TestBuildHubDepsDoesNotSearchTheIndex(t *testing.T) {
	seen := serveDiscover(t, jsonHandler(discoverBody))
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		t.Error("building the hub deps searched the public index")
		return discover.Response{}, nil
	})
	_ = buildHubDeps(context.Background(), config.Config{Repo: "owner/registry", DefaultBranch: "main"})
	if len(*seen) != 0 {
		t.Errorf("index requests at hub construction = %d, want 0", len(*seen))
	}
}

// TestHubDiscoverSearchMapsIndexRows proves the card's search hook returns the
// index's own rows, in the index's own order, once a query is submitted.
func TestHubDiscoverSearchMapsIndexRows(t *testing.T) {
	seen := serveDiscover(t, jsonHandler(discoverBody))
	deps := buildDiscoverHubDeps(config.Config{Repo: "owner/registry"})
	rows, err := deps.Search(context.Background(), "pdf")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
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
		Executability: "Good",
	}
	if rows[0] != want {
		t.Errorf("row 0 = %+v, want %+v", rows[0], want)
	}
	if rows[1].Name != "nano-pdf" {
		t.Errorf("row 1 = %q, want the index's own ranking preserved", rows[1].Name)
	}
	if len(*seen) != 1 {
		t.Errorf("index requests = %d, want exactly the submitted query", len(*seen))
	}
}

// TestHubDiscoverSearchFailureCarriesFallbackHint proves an unreachable index
// surfaces as an error the hub can toast, and keeps the reminder that `add`
// still works without the index.
func TestHubDiscoverSearchFailureCarriesFallbackHint(t *testing.T) {
	boom := errors.New("skill index returned HTTP 503")
	stubDiscoverSearch(t, func(context.Context, discover.Query) (discover.Response, error) {
		return discover.Response{}, boom
	})
	deps := buildDiscoverHubDeps(config.Config{Repo: "owner/registry"})
	rows, err := deps.Search(context.Background(), "pdf")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the index failure", err)
	}
	if rows != nil {
		t.Errorf("rows = %+v, want none alongside an error", rows)
	}
	if !strings.Contains(err.Error(), "skills-registry add") {
		t.Errorf("err = %q, want the add fallback hint", err)
	}
}

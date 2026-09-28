package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/registry"
)

// TestListSearchGetHelpSurface pins the discoverability contract from
// cli-tui-14: list/search/get each carry Long + Example help covering
// the TUI keys, the search top-10 cap, and the empty-query behavior.
func TestListSearchGetHelpSurface(t *testing.T) {
	cases := []struct {
		name  string
		build func() *cobra.Command
		long  []string
	}{
		{
			name:  "list",
			build: newListCmd,
			long: []string{
				`"/"`,     // filter key
				"enter",   // install key
				`"d"`,     // remove key
				`"?"`,     // help key
				"--query", // filter flag
				"--plain", // non-TUI mode
				"--json",  // script mode
			},
		},
		{
			name:  "search",
			build: newSearchCmd,
			long: []string{
				"top 10", // the cap
				"[]",     // empty-query behavior
				`"list"`, // the exhaustive alternative
				"--json",
			},
		},
		{
			name:  "get",
			build: newGetCmd,
			long: []string{
				"cache",    // one-shot destination
				"--dest",   // override
				`"list"`,   // durable-install alternative
				`"search"`, // slug discovery
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.build()
			if cmd.Long == "" {
				t.Fatalf("%s needs a Long help string", tc.name)
			}
			if cmd.Example == "" {
				t.Fatalf("%s needs an Example help string", tc.name)
			}
			for _, want := range tc.long {
				if !strings.Contains(cmd.Long, want) {
					t.Errorf("%s Long help must mention %q:\n%s", tc.name, want, cmd.Long)
				}
			}
			if !strings.Contains(cmd.Example, "skills-registry "+tc.name) {
				t.Errorf("%s Example must show an invocation:\n%s", tc.name, cmd.Example)
			}
		})
	}
}

// TestPrintPlainSearchHeaderSaysTop10 pins the cli-tui-14 fix: the human
// search header names the top-10 cap so readers don't mistake it for an
// exhaustive listing.
func TestPrintPlainSearchHeaderSaysTop10(t *testing.T) {
	out := captureStdout(t, func() {
		printPlainSearch("x/y", []registry.Summary{
			{Slug: "demo", Name: "Demo", Description: "A demo skill"},
		})
	})
	if !strings.Contains(out, "top 10") {
		t.Errorf("search header must say \"top 10\":\n%s", out)
	}
	if !strings.Contains(out, "demo") {
		t.Errorf("search table must still list the match:\n%s", out)
	}
}

// captureStdout redirects os.Stdout into a pipe while fn runs and
// returns everything it wrote. printPlainSearch writes via fmt.Printf
// (not the jsonout writer), so the jsonout capture helper can't see it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}

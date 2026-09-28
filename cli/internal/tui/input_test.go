package tui

import (
	"strings"
	"testing"
)

// TestNewInputShowsFullPlaceholder is the cli-tui-3 regression test for the
// shared prompt: bubbles sizes the placeholder buffer as Width+1 runes, so
// an unset Width rendered only the first character of the hint. Every
// NewInput caller (add, publish, discover-hub, bootstrap) inherits this fix,
// so pin each production placeholder plus a short one.
func TestNewInputShowsFullPlaceholder(t *testing.T) {
	placeholders := []string{
		"owner/repo, git URL, or local path",
		"path to folder containing SKILL.md",
		"pdf, summarize a youtube video, …",
		"skills-registry",
	}
	for _, ph := range placeholders {
		m := NewInput("Title", "Prompt", ph, "")
		v := stripANSI(m.View())
		if !strings.Contains(v, "> "+ph) {
			t.Errorf("NewInput(%q) clips the placeholder:\n%s", ph, v)
		}
	}
}

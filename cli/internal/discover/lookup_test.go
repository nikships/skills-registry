package discover

import (
	"context"
	"net/http"
	"testing"
)

func TestSkillKeyIgnoresTheRevision(t *testing.T) {
	// The index links a skill at whatever commit it last saw, so two URLs for
	// the same folder at different revisions must reduce to the same key.
	a, okA := SkillKey("https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize")
	b, okB := SkillKey("https://github.com/OpenClaw/OpenClaw/tree/main/skills/summarize")
	if !okA || !okB {
		t.Fatalf("SkillKey failed: %v %v", okA, okB)
	}
	if a != b {
		t.Fatalf("SkillKey = %q and %q, want the same key for the same folder", a, b)
	}
	if a != "openclaw/openclaw/skills/summarize" {
		t.Fatalf("SkillKey = %q, want owner/repo/path", a)
	}
}

func TestSkillKeyRejectsNonFolderURLs(t *testing.T) {
	for _, in := range []string{
		"https://github.com/openclaw/openclaw",
		"https://github.com/openclaw/openclaw/tree/main",
		"https://gitlab.com/o/r/tree/main/skills/x",
		"owner/repo",
		"",
	} {
		if _, ok := SkillKey(in); ok {
			t.Errorf("SkillKey(%q) succeeded, want a rejection", in)
		}
	}
}

// TestLookupFindsTheMatchingRow proves a URL the user pasted resolves to the
// index's row for the same folder even when the revisions differ.
func TestLookupFindsTheMatchingRow(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "summarize" {
			t.Errorf("query = %q, want the folder name", got)
		}
		_, _ = w.Write([]byte(skillNetBody))
	})
	got, ok, err := client.Lookup(context.Background(),
		"https://github.com/openclaw/openclaw/tree/main/skills/summarize")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("Lookup found nothing for an indexed folder")
	}
	if got.Safety != "Good" || got.Executability != "Average" {
		t.Fatalf("Lookup = %+v, want the index's own grades", got)
	}
}

// TestLookupMissIsNotAnError is the unscored path: the index simply has no row,
// which the caller renders as unscored rather than as a failure.
func TestLookupMissIsNotAnError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(skillNetBody))
	})
	_, ok, err := client.Lookup(context.Background(),
		"https://github.com/someone/else/tree/main/skills/other")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatal("Lookup matched a folder the index does not carry")
	}
}

// TestLookupIgnoresANearMissWithADifferentPath guards the key comparison: a
// same-named skill in another repository is not this skill.
func TestLookupIgnoresANearMissWithADifferentPath(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(skillNetBody))
	})
	_, ok, err := client.Lookup(context.Background(),
		"https://github.com/openclaw/openclaw/tree/main/other/summarize")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatal("Lookup matched on the name alone, ignoring the folder path")
	}
}

func TestLookupSkipsNonFolderURLsWithoutASearch(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("Lookup issued a search for a URL it cannot key")
	})
	_, ok, err := client.Lookup(context.Background(), "https://github.com/openclaw/openclaw")
	if err != nil || ok {
		t.Fatalf("Lookup = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestLookupPropagatesSearchFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, ok, err := client.Lookup(context.Background(),
		"https://github.com/openclaw/openclaw/tree/main/skills/summarize")
	if err == nil {
		t.Fatal("Lookup swallowed a transport failure")
	}
	if ok {
		t.Error("Lookup reported a hit alongside an error")
	}
}

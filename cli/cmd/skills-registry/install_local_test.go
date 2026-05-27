package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// TestInstallPickerTargetsLocksUniversal pins the spec invariant:
// every Universal=true target (today only `.agents`) appears in the
// picker as Locked=true. A regression here would let a user accidentally
// untoggle the always-on installer, leaving newly fetched skills with
// no canonical local copy.
func TestInstallPickerTargetsLocksUniversal(t *testing.T) {
	rows := installPickerTargets()
	if len(rows) == 0 {
		t.Fatal("installPickerTargets returned no rows")
	}
	var locked []string
	var dotAgents tui.InstallTarget
	for _, r := range rows {
		if r.Locked {
			locked = append(locked, r.Display)
		}
		if t2, ok := r.Value.(agents.Target); ok && t2.Universal {
			dotAgents = r
		}
	}
	if len(locked) == 0 {
		t.Fatalf("no locked rows; expected at least the universal target")
	}
	if dotAgents.Value == nil {
		t.Fatalf("did not find the universal .agents row in picker output")
	}
	if !dotAgents.Locked {
		t.Errorf("universal target Locked=false, want true")
	}
	if dotAgents.Hint == "" {
		t.Errorf("universal target Hint is empty, want '<dotdir>/skills'")
	}
}

// TestInstallPickerTargetsPreselectsPopular asserts the small popular
// set is pre-checked by Default=true so the first-time user picks up
// Claude/Cursor/etc. automatically.
func TestInstallPickerTargetsPreselectsPopular(t *testing.T) {
	rows := installPickerTargets()
	defaulted := map[string]bool{}
	for _, r := range rows {
		if r.Default {
			defaulted[r.Display] = true
		}
	}
	for name := range popularAgentDisplays {
		if !defaulted[name] {
			t.Errorf("expected popular agent %q to be Default=true", name)
		}
	}
}

// TestUniversalInstallTargetsReturnsAtLeastOne pins the non-interactive
// fallback used by `add --json`: at least one Universal=true target is
// always present so the JSON path never lands with zero install
// destinations.
func TestUniversalInstallTargetsReturnsAtLeastOne(t *testing.T) {
	out := universalInstallTargets()
	if len(out) == 0 {
		t.Fatal("universalInstallTargets returned empty slice")
	}
	for _, t2 := range out {
		if !t2.Universal {
			t.Errorf("non-universal target %q in universalInstallTargets output", t2.Display)
		}
	}
}

// TestInstallAnyValuesToTargetsRejectsForeignTypes covers the
// defensive cast inside the bridge from the embedded picker's opaque
// []any back to []agents.Target. A foreign type must surface a clean
// error rather than silently dropping rows.
func TestInstallAnyValuesToTargetsRejectsForeignTypes(t *testing.T) {
	universal := universalInstallTargets()
	values := []any{universal[0], "not-a-target"}
	if _, err := installAnyValuesToTargets(values); err == nil {
		t.Fatal("installAnyValuesToTargets accepted a foreign type")
	}
}

// TestCopyTreeRoundtripsFiles is the local-install regression test:
// copyTree should reproduce every file under src under dst, preserving
// content. Used by installSkillIntoTargets when copying the temp fetch
// dir into each agent dot-folder.
func TestCopyTreeRoundtripsFiles(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "scripts", "go.sh"), []byte("#!/bin/sh"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	for _, rel := range []string{"SKILL.md", "scripts/go.sh"} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("missing copied file %s: %v", rel, err)
		}
		if len(got) == 0 {
			t.Errorf("copied file %s is empty", rel)
		}
	}
}

// TestCopyTreePropagatesSourceError ensures the walk surfaces a
// filesystem error from the source instead of swallowing it. We pass
// a non-existent src so WalkDir's first call fails.
func TestCopyTreePropagatesSourceError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "does-not-exist")
	dst := t.TempDir()
	if err := copyTree(src, dst); err == nil {
		t.Fatal("copyTree on missing src returned nil error")
	} else if !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) {
		// Some filesystems wrap differently; we accept any error so
		// long as it's not nil.
		t.Logf("error type %T: %v", err, err)
	}
}

// TestCopyTreeRejectsSymlink verifies that copyTree fails if any entry
// is a symbolic link.
func TestCopyTreeRejectsSymlink(t *testing.T) {
	src := t.TempDir()
	target := filepath.Join(src, "target")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	sym := filepath.Join(src, "symlink")
	err := os.Symlink(target, sym)
	if err != nil {
		// On Windows, if we don't have symlink privileges, we skip the test.
		t.Skip("skipping symlink test; symlink creation failed (likely due to Windows privileges):", err)
	}

	dst := t.TempDir()
	if err := copyTree(src, dst); err == nil {
		t.Fatal("copyTree succeeded but should have rejected symlink")
	}
}

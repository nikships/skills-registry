package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// stubDownloader returns a Downloader that records every slug it sees and
// returns the configured dest/err.
type stubDownloader struct {
	calls []string
	dest  string
	err   error
}

func (s *stubDownloader) fn() Downloader {
	return func(_ context.Context, slug string) (string, string, error) {
		s.calls = append(s.calls, slug)
		return s.dest, "", s.err
	}
}

// readyModel returns a ListModel in stateReady with two rows loaded.
func readyModel(t *testing.T, dl Downloader) ListModel {
	t.Helper()
	loader := func() ([]SkillRow, error) {
		return []SkillRow{
			{Slug: "foo_skill", Name: "Foo", Desc: "first"},
			{Slug: "bar_skill", Name: "Bar", Desc: "second"},
		}, nil
	}
	m := NewList(context.Background(), "owner/repo", loader, dl)
	// Skip the loader by injecting the rowsLoadedMsg directly.
	got, _ := m.Update(rowsLoadedMsg{rows: []SkillRow{
		{Slug: "foo_skill", Name: "Foo", Desc: "first"},
		{Slug: "bar_skill", Name: "Bar", Desc: "second"},
	}})
	return got.(ListModel)
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func TestEnter_TriggersDownload(t *testing.T) {
	stub := &stubDownloader{dest: "/tmp/.agents/skills/foo_skill"}
	m := readyModel(t, stub.fn())

	got, cmd := m.Update(enterKey())
	mm := got.(ListModel)

	if mm.rowState["foo_skill"] != StatusDownloading {
		t.Fatalf("rowState[foo_skill] = %v, want StatusDownloading", mm.rowState["foo_skill"])
	}
	if mm.inflight != 1 {
		t.Fatalf("inflight = %d, want 1", mm.inflight)
	}
	if cmd == nil {
		t.Fatal("Update returned nil cmd; expected a download tea.Cmd")
	}

	// Executing the command should invoke the downloader and yield a
	// downloadDoneMsg{} with the resolved dest.
	msg := cmd()
	// The cmd is a tea.Batch — drain it until we find the downloadDoneMsg.
	done, ok := drainForDone(msg)
	if !ok {
		t.Fatal("did not get downloadDoneMsg from cmd output")
	}
	if done.slug != "foo_skill" {
		t.Fatalf("done.slug = %q, want foo_skill", done.slug)
	}
	if done.dest != stub.dest {
		t.Fatalf("done.dest = %q, want %q", done.dest, stub.dest)
	}
	if got := len(stub.calls); got != 1 || stub.calls[0] != "foo_skill" {
		t.Fatalf("downloader calls = %v, want [foo_skill]", stub.calls)
	}
}

func TestEnter_IgnoresDoublePressWhileDownloading(t *testing.T) {
	stub := &stubDownloader{dest: "/tmp/.agents/skills/foo_skill"}
	m := readyModel(t, stub.fn())

	// First enter starts the download.
	got, _ := m.Update(enterKey())
	mm := got.(ListModel)

	// Second enter on the same row must be a no-op.
	got2, cmd2 := mm.Update(enterKey())
	mm2 := got2.(ListModel)

	if mm2.inflight != 1 {
		t.Fatalf("inflight after double-press = %d, want 1", mm2.inflight)
	}
	if cmd2 != nil {
		t.Fatalf("expected nil cmd on double-press, got %T", cmd2)
	}
}

func TestDownloadDoneMsg_Success(t *testing.T) {
	stub := &stubDownloader{dest: "/tmp/.agents/skills/foo_skill"}
	m := readyModel(t, stub.fn())
	// Pretend the download is already in flight.
	m.rowState["foo_skill"] = StatusDownloading
	m.inflight = 1

	got, _ := m.Update(downloadDoneMsg{
		slug: "foo_skill",
		dest: "/tmp/.agents/skills/foo_skill",
	})
	mm := got.(ListModel)

	if mm.rowState["foo_skill"] != StatusDone {
		t.Fatalf("rowState[foo_skill] = %v, want StatusDone", mm.rowState["foo_skill"])
	}
	if mm.inflight != 0 {
		t.Fatalf("inflight = %d, want 0", mm.inflight)
	}
	if !mm.toastOK {
		t.Fatal("toastOK = false, want true on success")
	}
	if !strings.Contains(mm.toast, "Foo") || !strings.Contains(mm.toast, "/tmp/.agents/skills/foo_skill") {
		t.Fatalf("toast = %q, want it to mention Foo and dest path", mm.toast)
	}
	if mm.rowDest["foo_skill"] != "/tmp/.agents/skills/foo_skill" {
		t.Fatalf("rowDest[foo_skill] = %q, want dest path", mm.rowDest["foo_skill"])
	}
}

func TestDownloadDoneMsg_Error(t *testing.T) {
	stub := &stubDownloader{err: errors.New("boom")}
	m := readyModel(t, stub.fn())
	m.rowState["foo_skill"] = StatusDownloading
	m.inflight = 1

	got, _ := m.Update(downloadDoneMsg{
		slug: "foo_skill",
		err:  errors.New("boom"),
	})
	mm := got.(ListModel)

	if mm.rowState["foo_skill"] != StatusErr {
		t.Fatalf("rowState[foo_skill] = %v, want StatusErr", mm.rowState["foo_skill"])
	}
	if mm.inflight != 0 {
		t.Fatalf("inflight = %d, want 0", mm.inflight)
	}
	if mm.toastOK {
		t.Fatal("toastOK = true, want false on error")
	}
	if !strings.Contains(mm.toast, "Foo") || !strings.Contains(mm.toast, "boom") {
		t.Fatalf("toast = %q, want it to mention Foo and boom", mm.toast)
	}
}

func TestEnter_NoOpWithoutDownloader(t *testing.T) {
	m := readyModel(t, nil)
	got, cmd := m.Update(enterKey())
	mm := got.(ListModel)
	if mm.inflight != 0 {
		t.Fatalf("inflight = %d, want 0 (no downloader)", mm.inflight)
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd)
	}
	if _, ok := mm.rowState["foo_skill"]; ok {
		t.Fatal("rowState entry should not be created when downloader is nil")
	}
}

// drainForDone walks a tea.Msg that may be a Batch / sequence wrapper and
// returns the first downloadDoneMsg found.
func drainForDone(msg tea.Msg) (downloadDoneMsg, bool) {
	switch v := msg.(type) {
	case downloadDoneMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if d, ok := drainForDone(c()); ok {
				return d, true
			}
		}
	}
	return downloadDoneMsg{}, false
}

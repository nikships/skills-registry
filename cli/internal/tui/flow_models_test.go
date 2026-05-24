package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPublishFlowDoneEmitsSuccessToast(t *testing.T) {
	m := NewPublishFlow(context.Background(), PublishFlowDeps{})
	got, cmd := m.Update(publishFlowDoneMsg{result: PublishFlowResult{
		Slug: "demo",
		Repo: "owner/repo",
		SHA:  "abcdef123456",
	}})
	if cmd == nil {
		t.Fatal("publish done returned nil cmd")
	}
	if _, ok := got.(PublishFlowModel); !ok {
		t.Fatalf("model = %T, want PublishFlowModel", got)
	}
	msg, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want flowExitMsg", cmd())
	}
	if !msg.ok || !strings.Contains(msg.toast, "published demo") {
		t.Fatalf("flowExitMsg = %+v", msg)
	}
}

func TestSyncFlowLoadedNoMissingExitsInSync(t *testing.T) {
	m := NewSyncFlow(context.Background(), "owner/repo", SyncFlowDeps{})
	_, cmd := m.Update(syncLoadedMsg{})
	if cmd == nil {
		t.Fatal("sync loaded returned nil cmd")
	}
	msg := cmd().(flowExitMsg)
	if !msg.ok || !strings.Contains(msg.toast, "already in sync") {
		t.Fatalf("flowExitMsg = %+v", msg)
	}
}

func TestAddFlowLoadedErrorRunsCleanupAndExits(t *testing.T) {
	cleaned := false
	m := NewAddFlow(context.Background(), "owner/repo", AddFlowDeps{})
	_, cmd := m.Update(addLoadedMsg{
		cleanup: func() { cleaned = true },
		err:     context.Canceled,
	})
	if cmd == nil {
		t.Fatal("add loaded error returned nil cmd")
	}
	if !cleaned {
		t.Fatal("cleanup was not called")
	}
	msg := cmd().(flowExitMsg)
	if msg.ok || !strings.Contains(msg.toast, "add") {
		t.Fatalf("flowExitMsg = %+v", msg)
	}
}

func TestAddFlowEscCancelsWithoutTeaQuit(t *testing.T) {
	m := NewAddFlow(context.Background(), "owner/repo", AddFlowDeps{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc returned nil cmd")
	}
	if _, ok := cmd().(flowExitMsg); !ok {
		t.Fatalf("esc cmd returned %T, want flowExitMsg", cmd())
	}
}

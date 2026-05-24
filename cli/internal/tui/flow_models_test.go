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

func TestAddFlowRejectsUnsafeLocalSource(t *testing.T) {
	m := NewAddFlow(context.Background(), "owner/repo", AddFlowDeps{})
	m.source.Input.SetValue("../outside")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(AddFlowModel)
	if cmd != nil {
		t.Fatalf("unsafe source returned cmd %T, want nil", cmd)
	}
	if mm.state != addStateSource {
		t.Fatalf("state = %v, want addStateSource", mm.state)
	}
	if mm.source.err == nil || !strings.Contains(mm.source.err.Error(), "traversal") {
		t.Fatalf("source err = %v, want traversal error", mm.source.err)
	}
}

func TestPublishFlowRejectsUnsafePath(t *testing.T) {
	m := NewPublishFlow(context.Background(), PublishFlowDeps{})
	m.path.Input.SetValue("/tmp/skill")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(PublishFlowModel)
	if cmd != nil {
		t.Fatalf("unsafe path returned cmd %T, want nil", cmd)
	}
	if mm.state != publishStatePath {
		t.Fatalf("state = %v, want publishStatePath", mm.state)
	}
	if mm.path.err == nil || !strings.Contains(mm.path.err.Error(), "absolute") {
		t.Fatalf("path err = %v, want absolute error", mm.path.err)
	}
}

func TestAddFlowRedactsCredentialedSourceInPersistentLabel(t *testing.T) {
	m := NewAddFlow(context.Background(), "owner/repo", AddFlowDeps{})
	m.source.Input.SetValue("https://user@example.com/org/repo.git")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(AddFlowModel)
	if cmd == nil {
		t.Fatal("safe remote source should start loading")
	}
	if strings.Contains(mm.sourceText, "user@") {
		t.Fatalf("sourceText retained credentials: %q", mm.sourceText)
	}
	if mm.sourceText != "https://example.com/org/repo.git" {
		t.Fatalf("sourceText = %q, want redacted URL", mm.sourceText)
	}
}

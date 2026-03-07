package session

import (
	"testing"
	"time"

	"github.com/Kocoro-lab/shan/internal/client"
)

func TestManager_ResumeLatest_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	sess, err := m.ResumeLatest()
	if err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}
	if sess != nil {
		t.Error("expected nil session for empty directory")
	}
}

func TestManager_ResumeLatest_FindsMostRecent(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Save two sessions with different UpdatedAt times
	old := &Session{
		ID:        "old-session",
		Title:     "Old",
		UpdatedAt: time.Now().Add(-1 * time.Hour),
		Messages: []client.Message{
			{Role: "user", Content: client.NewTextContent("old message")},
		},
	}
	store.Save(old)

	recent := &Session{
		ID:        "recent-session",
		Title:     "Recent",
		UpdatedAt: time.Now(),
		Messages: []client.Message{
			{Role: "user", Content: client.NewTextContent("recent message")},
			{Role: "assistant", Content: client.NewTextContent("recent reply")},
		},
	}
	store.Save(recent)

	m := NewManager(dir)
	sess, err := m.ResumeLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("expected a session, got nil")
	}
	if sess.ID != "recent-session" {
		t.Errorf("expected most recent session 'recent-session', got %q", sess.ID)
	}
	if len(sess.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(sess.Messages))
	}

	// Should be set as current
	if m.Current() == nil || m.Current().ID != "recent-session" {
		t.Error("ResumeLatest should set the session as current")
	}
}

func TestManager_ResumeLatest_SingleSession(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Save(&Session{
		ID:    "only-one",
		Title: "Only session",
		Messages: []client.Message{
			{Role: "user", Content: client.NewTextContent("hello")},
		},
	})

	m := NewManager(dir)
	sess, err := m.ResumeLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "only-one" {
		t.Errorf("expected 'only-one', got %q", sess.ID)
	}
}

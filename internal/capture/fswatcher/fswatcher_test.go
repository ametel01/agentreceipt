package fswatcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
)

func TestClassifierFlagsSensitiveAndDependencyPaths(t *testing.T) {
	t.Parallel()

	classifier := NewClassifier(config.Default())
	tests := []struct {
		path       string
		sensitive  bool
		dependency bool
	}{
		{path: ".env", sensitive: true},
		{path: "src/auth/session.go", sensitive: true},
		{path: "go.mod", dependency: true},
		{path: "cmd/root.go"},
	}
	for _, test := range tests {
		got := classifier.Classify(test.path)
		if got.Sensitive != test.sensitive || got.Dependency != test.dependency {
			t.Fatalf("Classify(%q) = %+v, want sensitive=%v dependency=%v", test.path, got, test.sensitive, test.dependency)
		}
	}
}

func TestWatcherEmitsCanonicalEventsAndChangedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionID := "ar_ses_fs_test"
	watcher, err := New(root, sessionID, NewClassifier(config.Default()), 20*time.Millisecond)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("TOKEN=test\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	event := waitForEvent(t, watcher.Events())
	if event.SessionID != sessionID || event.Source != Source || event.Type != "fs.change" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if event.Payload["path"] != ".env" || event.Payload["sensitive"] != true {
		t.Fatalf("unexpected event payload: %+v", event.Payload)
	}

	changed := watcher.ChangedFiles()
	if len(changed) != 1 {
		t.Fatalf("len(changed) = %d, want 1: %+v", len(changed), changed)
	}
	if changed[0].Path != ".env" || !changed[0].Sensitive {
		t.Fatalf("unexpected changed file summary: %+v", changed[0])
	}
}

func TestWatcherFlagsDependencyChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	watcher, err := New(root, "ar_ses_dep_test", NewClassifier(config.Default()), 20*time.Millisecond)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	event := waitForEvent(t, watcher.Events())
	if event.Payload["dependency"] != true {
		t.Fatalf("dependency flag missing from event: %+v", event.Payload)
	}
	changed := watcher.ChangedFiles()
	if len(changed) != 1 || !changed[0].Dependency {
		t.Fatalf("dependency flag missing from changed files: %+v", changed)
	}
}

func waitForEvent(t *testing.T, events <-chan model.Event) model.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fs event")
		return model.Event{}
	}
}

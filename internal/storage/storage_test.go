package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLayoutUsesCanonicalPaths(t *testing.T) {
	t.Parallel()

	layout, err := NewLayout("/repo", "ar_ses_123")
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot() error = %v", err)
	}
	repo := filepath.Join(root, ReposDir, RepositoryKey("/repo"))
	if got, want := layout.EventsJSONL, filepath.Join(repo, SessionsDir, "ar_ses_123", EventsFile); got != want {
		t.Fatalf("EventsJSONL = %q, want %q", got, want)
	}
	if got, want := layout.StateJSON, filepath.Join(repo, SessionsDir, "ar_ses_123", StateFile); got != want {
		t.Fatalf("StateJSON = %q, want %q", got, want)
	}
	if got, want := layout.ProviderCodexTraces, filepath.Join(repo, SessionsDir, "ar_ses_123", ProviderDir, ProviderCodexDir, TracesDir); got != want {
		t.Fatalf("ProviderCodexTraces = %q, want %q", got, want)
	}
}

func TestNewLayoutRejectsUnsafeSessionID(t *testing.T) {
	t.Parallel()

	for _, sessionID := range []string{"", "123", "ar_ses_../x", "ar_ses_/tmp/x"} {
		if _, err := NewLayout("/repo", sessionID); err == nil {
			t.Fatalf("NewLayout() returned nil error for unsafe session ID %q", sessionID)
		}
	}
}

func TestNewLayoutRejectsEmptyRepoRoot(t *testing.T) {
	t.Parallel()

	if _, err := NewLayout("", "ar_ses_test"); err == nil {
		t.Fatal("NewLayout() returned nil error for empty repo root")
	}
}

func TestEnsureSessionLayoutCreatesReservedDirectories(t *testing.T) {
	t.Parallel()

	layout, err := NewLayout(t.TempDir(), "ar_ses_test")
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	defer func() {
		_ = os.RemoveAll(layout.Repo)
	}()
	if err := EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	for _, dir := range []string{layout.Session, layout.Diffs, layout.ProviderCodexTraces, layout.ProviderClaude, layout.Blobs, layout.Signatures} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %q: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", dir)
		}
	}
}

func TestEnsureSessionLayoutReportsDirectoryCreationErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	err := EnsureSessionLayout(Layout{
		Root:     root,
		Sessions: filepath.Join(blocked, SessionsDir),
	})
	if err == nil {
		t.Fatal("EnsureSessionLayout() returned nil error for blocked directory")
	}
	if !strings.Contains(err.Error(), "create directory") {
		t.Fatalf("EnsureSessionLayout() error = %q, want directory creation context", err.Error())
	}
}

func TestManifestArtifactsAreSessionRelative(t *testing.T) {
	t.Parallel()

	layout, err := NewLayout("/repo", "ar_ses_test")
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	artifacts := ManifestArtifacts(layout)
	if artifacts.EventsJSONL != EventsFile {
		t.Fatalf("EventsJSONL = %q, want %q", artifacts.EventsJSONL, EventsFile)
	}
	if artifacts.CodexTraceDir != "provider/codex/traces" {
		t.Fatalf("CodexTraceDir = %q, want provider/codex/traces", artifacts.CodexTraceDir)
	}
}

func TestRepositoryPathUsesGlobalHome(t *testing.T) {
	t.Parallel()

	path, err := RepositoryPath("/repo")
	if err != nil {
		t.Fatalf("RepositoryPath() error = %v", err)
	}
	if filepath.Dir(path) != filepath.Join(os.TempDir(), "agentreceipt-test", ReposDir) {
		t.Fatalf("RepositoryPath() = %q, want test global home under tempdir", path)
	}
}

func TestDefaultRootUsesEnvironmentOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnv, home)
	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot() error = %v", err)
	}
	if root != home {
		t.Fatalf("DefaultRoot() = %q, want %q", root, home)
	}
	sessions, err := SessionsPath("/repo")
	if err != nil {
		t.Fatalf("SessionsPath() error = %v", err)
	}
	if got, want := sessions, filepath.Join(home, ReposDir, RepositoryKey("/repo"), SessionsDir); got != want {
		t.Fatalf("SessionsPath() = %q, want %q", got, want)
	}
}

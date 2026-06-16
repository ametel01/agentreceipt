package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLayoutUsesCanonicalPaths(t *testing.T) {
	t.Parallel()

	layout, err := NewLayout("/repo", "ar_ses_123")
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if got, want := layout.EventsJSONL, filepath.Join("/repo", RootDir, SessionsDir, "ar_ses_123", EventsFile); got != want {
		t.Fatalf("EventsJSONL = %q, want %q", got, want)
	}
	if got, want := layout.StateJSON, filepath.Join("/repo", RootDir, SessionsDir, "ar_ses_123", StateFile); got != want {
		t.Fatalf("StateJSON = %q, want %q", got, want)
	}
	if got, want := layout.ProviderCodexTraces, filepath.Join("/repo", RootDir, SessionsDir, "ar_ses_123", ProviderDir, ProviderCodexDir, TracesDir); got != want {
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

package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/storage"
)

func TestStartStatusLiveStopLifecycle(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.State != model.SessionStateActive || state.EventCount != 1 {
		t.Fatalf("unexpected start state: %+v", state)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if _, err := os.Stat(layout.StateJSON); err != nil {
		t.Fatalf("state file was not written: %v", err)
	}
	if _, err := os.Stat(layout.ManifestJSON); err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}

	status, ok, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !ok || status.SessionID != state.SessionID || status.State != model.SessionStateActive {
		t.Fatalf("unexpected status ok=%v state=%+v", ok, status)
	}
	events, err := manager.Live(context.Background(), 10)
	if err != nil {
		t.Fatalf("Live() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != "git.snapshot" {
		t.Fatalf("unexpected live events: %+v", events)
	}

	appendSessionFile(t, filepath.Join(repo, "README.md"), "changed\n")
	finalized, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped || finalized.State != model.SessionStateFinalized {
		t.Fatalf("unexpected finalized state stopped=%v state=%+v", stopped, finalized)
	}
	if finalized.EventCount != 3 || finalized.CaptureSources.CodexLogs != "missing" {
		t.Fatalf("unexpected finalized metadata: %+v", finalized)
	}
	if len(finalized.Warnings) != 1 || finalized.Warnings[0].Code != "codex_events_missing" {
		t.Fatalf("expected zero Codex warning: %+v", finalized.Warnings)
	}
	if _, err := os.Stat(layout.FinalPatch); err != nil {
		t.Fatalf("final patch was not written: %v", err)
	}
	storedEvents, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if _, err := eventlog.Replay(storedEvents); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	manifest := readManifest(t, layout.ManifestJSON)
	if manifest.State != model.SessionStateFinalized || manifest.EventCount != 3 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	_, ok, err = manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after stop error = %v", err)
	}
	if ok {
		t.Fatal("Status() still found active session after stop")
	}
}

func TestStopWithoutActiveSessionIsIdempotent(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	_, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if stopped {
		t.Fatal("Stop() reported stopped=true with no active session")
	}
}

func TestStopTwiceIsIdempotent(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	if _, err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, stopped, err := manager.Stop(context.Background()); err != nil || !stopped {
		t.Fatalf("first Stop() stopped=%v err=%v", stopped, err)
	}
	if _, stopped, err := manager.Stop(context.Background()); err != nil || stopped {
		t.Fatalf("second Stop() stopped=%v err=%v", stopped, err)
	}
}

func TestLiveWithoutActiveSessionReturnsNoEvents(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	events, err := manager.Live(context.Background(), 10)
	if err != nil {
		t.Fatalf("Live() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("Live() len = %d, want 0", len(events))
	}
}

func TestStatusClearsStaleActivePointer(t *testing.T) {
	repo := newSessionGitRepo(t)
	if err := writeActiveSession(repo, "ar_ses_missing"); err != nil {
		t.Fatalf("writeActiveSession() error = %v", err)
	}
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	_, ok, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if ok {
		t.Fatal("Status() found session for stale active pointer")
	}
	if _, ok, err := readActiveSession(repo); err != nil || ok {
		t.Fatalf("active pointer was not cleared ok=%v err=%v", ok, err)
	}
}

func TestStatusRejectsCorruptActivePointer(t *testing.T) {
	repo := newSessionGitRepo(t)
	rootDir := filepath.Join(repo, storage.RootDir)
	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		t.Fatalf("mkdir root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, storage.ActiveSessionFile), []byte("../bad\n"), 0o600); err != nil {
		t.Fatalf("write corrupt active pointer: %v", err)
	}
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	if _, _, err := manager.Status(context.Background()); err == nil {
		t.Fatal("Status() returned nil error for corrupt active pointer")
	}
}

func TestStartRejectsConcurrentActiveSession(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	if _, err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := manager.Start(context.Background()); err == nil {
		t.Fatal("Start() returned nil error for concurrent active session")
	}
}

func TestFormatStatusSortsReasonsAndHandlesEmptyReasons(t *testing.T) {
	state := State{
		SessionID:  "ar_ses_test",
		State:      model.SessionStateActive,
		EventCount: 2,
		RiskSummary: RiskSummary{
			Level:   model.RiskMedium,
			Reasons: []string{"z reason", "a reason"},
		},
		CaptureSources: CaptureSources{Git: "active", Filesystem: "ready", CodexLogs: "missing"},
	}
	output := FormatStatus(state)
	if !strings.Contains(output, "Reasons: a reason, z reason") {
		t.Fatalf("FormatStatus() did not sort reasons: %q", output)
	}

	state.RiskSummary.Reasons = nil
	output = FormatStatus(state)
	if !strings.Contains(output, "Reasons: none") {
		t.Fatalf("FormatStatus() did not render empty reasons: %q", output)
	}
}

func TestBuildFilesystemWatcher(t *testing.T) {
	watcher, err := BuildFilesystemWatcher(t.TempDir(), "ar_ses_test", config.Default())
	if err != nil {
		t.Fatalf("BuildFilesystemWatcher() error = %v", err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSessionHelpers(t *testing.T) {
	warning := model.Warning{Code: "same", Message: "message"}
	warnings := appendWarning([]model.Warning{warning}, warning)
	if len(warnings) != 1 {
		t.Fatalf("appendWarning duplicated existing warning: %+v", warnings)
	}
	if got := repoPathOrCWD("/tmp/repo"); got != "/tmp/repo" {
		t.Fatalf("repoPathOrCWD explicit = %q", got)
	}
	if got := (Manager{}).now(); got.IsZero() {
		t.Fatal("default now returned zero time")
	}
}

func readManifest(t *testing.T, path string) model.Manifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest model.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	return manifest
}

func newSessionGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runSessionGit(t, repo, "init")
	runSessionGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runSessionGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runSessionGit(t, repo, "add", "README.md")
	runSessionGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runSessionGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func appendSessionFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 16, 8, 0, 0, 0, time.UTC)
}

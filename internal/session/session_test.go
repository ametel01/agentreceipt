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

	"github.com/ametel01/agentreceipt/internal/capture/instructions"
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
	if finalized.EventCount < 3 || finalized.CaptureSources.CodexLogs != "missing" {
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
	if manifest.State != model.SessionStateFinalized || manifest.EventCount != finalized.EventCount {
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

func TestStartCapturesInstructionFilesAtSessionStart(t *testing.T) {
	repo := newSessionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# Session Instructions\n"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Claude Instructions\n"), 0o600); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(state.Warnings) != 0 {
		t.Fatalf("start warnings = %+v", state.Warnings)
	}

	events, err := manager.Live(context.Background(), 10)
	if err != nil {
		t.Fatalf("Live() error = %v", err)
	}
	instructionPaths := make(map[string]bool, 2)
	hasGitSnapshot := false
	for _, event := range events {
		if event.Source == "git_monitor" && event.Type == "git.snapshot" {
			hasGitSnapshot = true
			continue
		}
		if event.Source == instructions.Source && event.Type == instructions.TypeInstructionFile {
			path, ok := event.Payload["path"].(string)
			if ok {
				instructionPaths[path] = true
			}
		}
	}
	if !hasGitSnapshot {
		t.Fatalf("expected git snapshot event in start events: %+v", events)
	}
	if !instructionPaths["AGENTS.md"] || !instructionPaths["CLAUDE.md"] {
		t.Fatalf("instruction events missing: %+v", events)
	}
	if len(events) != 3 {
		t.Fatalf("start event count = %d, want 3", len(events))
	}

	finalized, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped || finalized.SessionID != state.SessionID {
		t.Fatalf("stop session = %+v", finalized)
	}
}

func TestStartCapturesInstructionFileWarningsWhenInstructionIsNotRegular(t *testing.T) {
	repo := newSessionGitRepo(t)
	if err := os.Mkdir(filepath.Join(repo, "AGENTS.md"), 0o750); err != nil {
		t.Fatalf("mkdir AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Claude Instructions\n"), 0o600); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !hasSessionWarning(state.Warnings, "instruction_capture.non_regular_agents") {
		t.Fatalf("expected non-regular instruction warning in start state: %+v", state.Warnings)
	}
	state, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped {
		t.Fatal("Stop() stopped=false")
	}
	if !hasSessionWarning(state.Warnings, "instruction_capture.non_regular_agents") {
		t.Fatalf("warning was not persisted to final state: %+v", state.Warnings)
	}
}

func TestListSessionsForRepository(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	summaries, err := manager.List(context.Background())
	if err != nil {
		t.Fatalf("List() empty error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("List() empty = %+v, want none", summaries)
	}

	active, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	summaries, err = manager.List(context.Background())
	if err != nil {
		t.Fatalf("List() active error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("List() active len = %d, want 1: %+v", len(summaries), summaries)
	}
	if got := summaries[0]; got.SessionID != active.SessionID || got.State != model.SessionStateActive || !got.Active || got.EventCount != active.EventCount {
		t.Fatalf("unexpected active summary: %+v state=%+v", got, active)
	}

	finalized, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped {
		t.Fatal("Stop() stopped=false")
	}
	sessionsPath, err := storage.SessionsPath(repo)
	if err != nil {
		t.Fatalf("SessionsPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sessionsPath, "not-a-session"), 0o750); err != nil {
		t.Fatalf("mkdir invalid session: %v", err)
	}
	layoutWithoutState, err := storage.NewLayout(repo, "ar_ses_without_state")
	if err != nil {
		t.Fatalf("NewLayout() without state error = %v", err)
	}
	if err := os.MkdirAll(layoutWithoutState.Session, 0o750); err != nil {
		t.Fatalf("mkdir session without state: %v", err)
	}

	summaries, err = manager.List(context.Background())
	if err != nil {
		t.Fatalf("List() finalized error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("List() finalized len = %d, want 1: %+v", len(summaries), summaries)
	}
	if got := summaries[0]; got.SessionID != finalized.SessionID || got.State != model.SessionStateFinalized || got.Active || got.EventCount != finalized.EventCount || got.Warnings != len(finalized.Warnings) {
		t.Fatalf("unexpected finalized summary: %+v state=%+v", got, finalized)
	}
}

func TestAppendProviderEventsPreventsMissingCodexWarning(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_codex_test",
		SessionID: state.SessionID,
		Timestamp: fixedNow(),
		Source:    "codex_session_log",
		Type:      "provider.command",
		Provider:  "codex",
		CWD:       repo,
		Payload:   map[string]any{"command": "go test ./..."},
	}
	state, ok, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil)
	if err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if !ok || state.CaptureSources.CodexLogs != "imported" {
		t.Fatalf("unexpected appended state ok=%v state=%+v", ok, state)
	}
	finalized, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped {
		t.Fatal("Stop() stopped=false")
	}
	if finalized.CaptureSources.CodexLogs != "imported" {
		t.Fatalf("CodexLogs = %q, want imported", finalized.CaptureSources.CodexLogs)
	}
	for _, warning := range finalized.Warnings {
		if warning.Code == "codex_events_missing" {
			t.Fatalf("unexpected missing Codex warning: %+v", finalized.Warnings)
		}
	}
}

func TestAppendClaudeProviderEventsPreventsMissingProviderWarning(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_claude_test",
		SessionID: state.SessionID,
		Timestamp: fixedNow(),
		Source:    "claude_hook",
		Type:      "provider.command",
		Provider:  "claude",
		CWD:       repo,
		Payload:   map[string]any{"tool_call": map[string]any{"command": "go test ./..."}},
	}
	state, ok, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil)
	if err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if !ok || state.CaptureSources.CodexLogs != "imported" {
		t.Fatalf("unexpected appended state ok=%v state=%+v", ok, state)
	}
	finalized, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped {
		t.Fatal("Stop() stopped=false")
	}
	if finalized.CaptureSources.CodexLogs != "imported" {
		t.Fatalf("CodexLogs = %q, want imported", finalized.CaptureSources.CodexLogs)
	}
	if hasSessionWarning(finalized.Warnings, "codex_events_missing") {
		t.Fatalf("unexpected missing provider warning: %+v", finalized.Warnings)
	}
}

func TestAppendProviderWarningsOnlyDoesNotClaimCodexImported(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	warningEvent := model.Event{
		EventID:   "evt_codex_warning_only",
		SessionID: state.SessionID,
		Timestamp: fixedNow(),
		Source:    "codex_session_log",
		Type:      "provider.parse_warning",
		Provider:  "codex",
		CWD:       repo,
		Payload: map[string]any{
			"code":    "malformed_json",
			"message": "bad line",
		},
	}
	state, ok, err := manager.AppendProviderEvents(context.Background(), []model.Event{warningEvent}, []model.Warning{{
		Code:    "codex_malformed_json",
		Message: "bad line",
	}})
	if err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if !ok || state.CaptureSources.CodexLogs != "not_observed" {
		t.Fatalf("warnings-only import should not claim Codex logs imported ok=%v state=%+v", ok, state)
	}
	if len(state.Warnings) != 1 || state.Warnings[0].Code != "codex_malformed_json" {
		t.Fatalf("expected parse warning to be retained: %+v", state.Warnings)
	}
	finalized, stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped {
		t.Fatal("Stop() stopped=false")
	}
	if finalized.CaptureSources.Git != "finalized" || finalized.CaptureSources.Filesystem != "stopped" || finalized.CaptureSources.CodexLogs != "missing" {
		t.Fatalf("unexpected finalized capture sources: %+v", finalized.CaptureSources)
	}
	if !hasSessionWarning(finalized.Warnings, "codex_malformed_json") || !hasSessionWarning(finalized.Warnings, "codex_events_missing") {
		t.Fatalf("expected parse and missing-Codex warnings: %+v", finalized.Warnings)
	}
}

func TestLockedAppendersRejectFinalizedSession(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	sessionID := "ar_ses_finalized_append"
	layout := writeStoredSessionForState(t, repo, sessionID, model.SessionStateFinalized)
	if err := writeActiveSession(repo, sessionID); err != nil {
		t.Fatalf("writeActiveSession() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_codex_after_finalize",
		SessionID: sessionID,
		Timestamp: fixedNow(),
		Source:    "codex_session_log",
		Type:      "provider.command",
		Provider:  "codex",
		CWD:       repo,
		Payload:   map[string]any{"command": "go test ./..."},
	}
	if _, _, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil); err == nil || !strings.Contains(err.Error(), "session is not active") {
		t.Fatalf("AppendProviderEvents() err = %v, want inactive session error", err)
	}
	if _, _, err := manager.Mark(context.Background(), "after finalize", filepath.Join(t.TempDir(), "keys")); err == nil || !strings.Contains(err.Error(), "session is not active") {
		t.Fatalf("Mark() err = %v, want inactive session error", err)
	}
	if _, _, err := manager.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "session is not active") {
		t.Fatalf("Stop() err = %v, want inactive session error", err)
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want only initial event", len(events))
	}
}

func TestSessionCapturesFilesystemChanges(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}

	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.CaptureSources.Git != "active" || state.CaptureSources.Filesystem != "active" || state.CaptureSources.CodexLogs != "not_observed" {
		t.Fatalf("start should report active git/filesystem and no Codex import: %+v", state)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	event := waitForSessionEvent(t, layout.EventsJSONL, func(event model.Event) bool {
		return event.Source == "fs_watcher" && event.Type == "fs.change"
	})
	if event.Payload["path"] != "go.mod" {
		t.Fatalf("fs path = %v, want go.mod: %+v", event.Payload["path"], event)
	}
	if event.Payload["dependency"] != true {
		t.Fatalf("dependency classification missing: %+v", event.Payload)
	}
	if _, ok := event.Payload["sensitive"].(bool); !ok {
		t.Fatalf("sensitive classification missing: %+v", event.Payload)
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if _, err := eventlog.Replay(events); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if _, stopped, err := manager.Stop(context.Background()); err != nil || !stopped {
		t.Fatalf("Stop() stopped=%v err=%v", stopped, err)
	}
}

func TestRunFilesystemWatcherStopsOnMarker(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_direct_watcher"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	writer, err := eventlog.NewWriter(layout.EventsJSONL, "", 1)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	appended, err := writer.Append(model.Event{
		EventID:   "evt_git_start_direct",
		SessionID: sessionID,
		Timestamp: fixedNow(),
		Source:    "git_monitor",
		Type:      "git.snapshot",
		CWD:       repo,
		Payload:   map[string]any{"phase": "start"},
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	state := State{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		RepoRoot:      repo,
		State:         model.SessionStateActive,
		PID:           os.Getpid(),
		StartedAt:     fixedNow(),
		UpdatedAt:     fixedNow(),
		EventCount:    appended.Seq,
		ChainHash:     appended.EventHash,
		CaptureSources: CaptureSources{
			Git:        "active",
			Filesystem: "active",
			CodexLogs:  "not_observed",
		},
		RiskSummary: RiskSummary{Level: model.RiskInfo},
	}
	if err := writeState(layout, state); err != nil {
		t.Fatalf("writeState() error = %v", err)
	}
	if err := writeManifest(layout, model.NewManifest(sessionID, fixedNow(), storage.ManifestArtifacts(layout))); err != nil {
		t.Fatalf("writeManifest() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunFilesystemWatcher(ctx, FilesystemWatcherOptions{
			RepoRoot:  repo,
			SessionID: sessionID,
			Config:    config.Default(),
			Nonce:     testWatcherNonce(),
		})
	}()
	if err := waitForFilesystemWatcherReady(ctx, layout); err != nil {
		t.Fatalf("waitForFilesystemWatcherReady() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("TOKEN=test\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	event := waitForSessionEvent(t, layout.EventsJSONL, func(event model.Event) bool {
		return event.Source == "fs_watcher" && event.Type == "fs.change"
	})
	if event.Payload["path"] != ".env" || event.Payload["sensitive"] != true {
		t.Fatalf("unexpected fs event payload: %+v", event.Payload)
	}
	if err := os.WriteFile(layout.FilesystemWatcherStopPath, []byte("stop\n"), 0o600); err != nil {
		t.Fatalf("write stop marker: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RunFilesystemWatcher() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RunFilesystemWatcher to stop")
	}
	if _, err := os.Stat(layout.FilesystemWatcherDonePath); err != nil {
		t.Fatalf("done marker was not written: %v", err)
	}
}

func TestStartRejectsDisabledFilesystemCapture(t *testing.T) {
	repo := newSessionGitRepo(t)
	cfg := config.Default()
	cfg.Capture.Filesystem = false
	manager := Manager{RepoPath: repo, Config: cfg, Now: fixedNow}

	if _, err := manager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "filesystem capture is disabled") {
		t.Fatalf("Start() err = %v, want filesystem disabled", err)
	}
	if _, ok, err := manager.Status(context.Background()); err != nil || ok {
		t.Fatalf("Status() after disabled start ok=%v err=%v", ok, err)
	}
}

func TestStartFilesystemWatcherProductionLaunchHonorsContext(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_launch_timeout"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	originalArg0 := os.Args[0]
	os.Args[0] = "agentreceipt"
	defer func() {
		os.Args[0] = originalArg0
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _, err = startFilesystemWatcher(ctx, State{RepoRoot: repo, SessionID: sessionID}, layout, config.Default())
	if err == nil {
		t.Fatal("startFilesystemWatcher() returned nil error for test-binary sidecar timeout")
	}
}

func TestStopFilesystemWatcherAcceptsDoneMarker(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_stop_done"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	if err := os.WriteFile(layout.FilesystemWatcherDonePath, []byte("done\n"), 0o600); err != nil {
		t.Fatalf("write done marker: %v", err)
	}
	state := State{
		SessionID:            sessionID,
		RepoRoot:             repo,
		FilesystemWatcherPID: os.Getpid(),
		CaptureSources:       CaptureSources{Filesystem: "active"},
	}

	if err := stopFilesystemWatcher(context.Background(), state, layout); err != nil {
		t.Fatalf("stopFilesystemWatcher() error = %v", err)
	}
	if _, err := os.Stat(layout.FilesystemWatcherStopPath); err != nil {
		t.Fatalf("stop marker was not written: %v", err)
	}
}

func TestStopFilesystemWatcherNoopAndRepoPathFallback(t *testing.T) {
	repo := newSessionGitRepo(t)
	layout, err := storage.NewLayout(repo, "ar_ses_watcher_noop")
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := stopFilesystemWatcher(context.Background(), State{}, layout); err != nil {
		t.Fatalf("stopFilesystemWatcher() noop error = %v", err)
	}
	if got := repoPathOrCWD(""); got == "" {
		t.Fatal("repoPathOrCWD empty returned empty path")
	}
}

func TestStopFilesystemWatcherRejectsMismatchedIdentityBeforeSignal(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_stop_mismatch"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	state := State{
		SessionID:              sessionID,
		RepoRoot:               repo,
		FilesystemWatcherPID:   12345,
		FilesystemWatcherNonce: testWatcherNonce(),
		CaptureSources:         CaptureSources{Filesystem: "active"},
	}
	if err := writeFilesystemWatcherIdentity(layout, filesystemWatcherIdentity{
		SessionID: sessionID,
		Nonce:     strings.Repeat("b", 32),
		PID:       state.FilesystemWatcherPID,
	}); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	called := false
	withFilesystemWatcherStopTestSeams(t, func(pid int) (filesystemWatcherProcess, error) {
		called = true
		return &fakeFilesystemWatcherProcess{}, nil
	})

	err = stopFilesystemWatcher(context.Background(), state, layout)
	if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("stopFilesystemWatcher() err = %v, want identity mismatch", err)
	}
	if called {
		t.Fatal("fallback signaled process despite mismatched identity")
	}
}

func TestStopFilesystemWatcherRequiresIdentityBeforeSignal(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_stop_missing_identity"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	state := State{
		SessionID:              sessionID,
		RepoRoot:               repo,
		FilesystemWatcherPID:   12345,
		FilesystemWatcherNonce: testWatcherNonce(),
		CaptureSources:         CaptureSources{Filesystem: "active"},
	}
	called := false
	withFilesystemWatcherStopTestSeams(t, func(pid int) (filesystemWatcherProcess, error) {
		called = true
		return &fakeFilesystemWatcherProcess{}, nil
	})

	err = stopFilesystemWatcher(context.Background(), state, layout)
	if err == nil || !strings.Contains(err.Error(), "identity cannot be verified") {
		t.Fatalf("stopFilesystemWatcher() err = %v, want identity verification error", err)
	}
	if called {
		t.Fatal("fallback signaled process despite missing identity")
	}
}

func TestStopFilesystemWatcherAcceptsVerifiedStaleProcess(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_stop_stale_process"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	state := State{
		SessionID:              sessionID,
		RepoRoot:               repo,
		FilesystemWatcherPID:   12345,
		FilesystemWatcherNonce: testWatcherNonce(),
		CaptureSources:         CaptureSources{Filesystem: "active"},
	}
	if err := writeFilesystemWatcherIdentity(layout, filesystemWatcherIdentity{
		SessionID: sessionID,
		Nonce:     state.FilesystemWatcherNonce,
		PID:       state.FilesystemWatcherPID,
	}); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	process := &fakeFilesystemWatcherProcess{signalErr: os.ErrProcessDone}
	withFilesystemWatcherStopTestSeams(t, func(pid int) (filesystemWatcherProcess, error) {
		if pid != state.FilesystemWatcherPID {
			t.Fatalf("pid = %d, want %d", pid, state.FilesystemWatcherPID)
		}
		return process, nil
	})

	if err := stopFilesystemWatcher(context.Background(), state, layout); err != nil {
		t.Fatalf("stopFilesystemWatcher() error = %v", err)
	}
	if process.signals != 1 || process.kills != 0 {
		t.Fatalf("fallback signals=%d kills=%d, want 1/0", process.signals, process.kills)
	}
	if _, err := os.Stat(layout.FilesystemWatcherStopPath); err != nil {
		t.Fatalf("stop marker was not written: %v", err)
	}
}

func TestStopFilesystemWatcherSignalsVerifiedIdentityOnFallback(t *testing.T) {
	repo := newSessionGitRepo(t)
	sessionID := "ar_ses_stop_verified_identity"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	state := State{
		SessionID:              sessionID,
		RepoRoot:               repo,
		FilesystemWatcherPID:   12345,
		FilesystemWatcherNonce: testWatcherNonce(),
		CaptureSources:         CaptureSources{Filesystem: "active"},
	}
	if err := writeFilesystemWatcherIdentity(layout, filesystemWatcherIdentity{
		SessionID: sessionID,
		Nonce:     state.FilesystemWatcherNonce,
		PID:       state.FilesystemWatcherPID,
	}); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	process := &fakeFilesystemWatcherProcess{}
	withFilesystemWatcherStopTestSeams(t, func(pid int) (filesystemWatcherProcess, error) {
		if pid != state.FilesystemWatcherPID {
			t.Fatalf("pid = %d, want %d", pid, state.FilesystemWatcherPID)
		}
		return process, nil
	})

	err = stopFilesystemWatcher(context.Background(), state, layout)
	if err == nil || !strings.Contains(err.Error(), "did not stop cleanly") {
		t.Fatalf("stopFilesystemWatcher() err = %v, want fallback failure", err)
	}
	if process.signals != 1 || process.kills != 1 {
		t.Fatalf("fallback signals=%d kills=%d, want 1/1", process.signals, process.kills)
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

func TestMarkWritesSignedManualMarker(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	started, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	state, ok, err := manager.Mark(context.Background(), "manual review complete", filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatalf("Mark() error = %v", err)
	}
	if !ok || state.EventCount != 2 || state.CaptureSources.CodexLogs != "not_observed" {
		t.Fatalf("unexpected mark state ok=%v state=%+v", ok, state)
	}
	layout, err := storage.NewLayout(repo, started.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	last := events[len(events)-1]
	if last.Source != "manual_marker" || last.Type != "manual.marker" {
		t.Fatalf("unexpected marker event: %+v", last)
	}
	if last.Payload["message"] != "manual review complete" || last.Payload["signature_algorithm"] != "ed25519" || last.Payload["signature"] == "" {
		t.Fatalf("marker payload missing signed context: %+v", last.Payload)
	}
}

func TestMarkWithoutActiveSessionReturnsFalse(t *testing.T) {
	repo := newSessionGitRepo(t)
	manager := Manager{RepoPath: repo, Config: config.Default(), Now: fixedNow}
	if _, ok, err := manager.Mark(context.Background(), "manual review", filepath.Join(t.TempDir(), "keys")); err != nil || ok {
		t.Fatalf("Mark() without active session ok=%v err=%v", ok, err)
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
	rootDir, err := storage.RepositoryPath(repo)
	if err != nil {
		t.Fatalf("RepositoryPath() error = %v", err)
	}
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

func hasSessionWarning(warnings []model.Warning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}

	return false
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

func writeStoredSessionForState(t *testing.T, repo string, sessionID string, stateValue model.SessionState) storage.Layout {
	t.Helper()
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	writer, err := eventlog.NewWriter(layout.EventsJSONL, "", 1)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	appended, err := writer.Append(model.Event{
		EventID:   "evt_git_start_" + sessionID,
		SessionID: sessionID,
		Timestamp: fixedNow(),
		Source:    "git_monitor",
		Type:      "git.snapshot",
		CWD:       repo,
		Payload:   map[string]any{"phase": "start"},
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	state := State{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		RepoRoot:      repo,
		State:         stateValue,
		PID:           os.Getpid(),
		StartedAt:     fixedNow(),
		UpdatedAt:     fixedNow(),
		EventCount:    appended.Seq,
		ChainHash:     appended.EventHash,
		CaptureSources: CaptureSources{
			Git:        "active",
			Filesystem: "stopped",
			CodexLogs:  "not_observed",
		},
		RiskSummary: RiskSummary{Level: model.RiskInfo},
	}
	if err := writeState(layout, state); err != nil {
		t.Fatalf("writeState() error = %v", err)
	}
	manifest := model.NewManifest(sessionID, fixedNow(), storage.ManifestArtifacts(layout))
	manifest.State = stateValue
	manifest.EventCount = appended.Seq
	if err := writeManifest(layout, manifest); err != nil {
		t.Fatalf("writeManifest() error = %v", err)
	}

	return layout
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

type fakeFilesystemWatcherProcess struct {
	signals   int
	kills     int
	signalErr error
	killErr   error
}

func (p *fakeFilesystemWatcherProcess) Signal(os.Signal) error {
	p.signals++

	return p.signalErr
}

func (p *fakeFilesystemWatcherProcess) Kill() error {
	p.kills++

	return p.killErr
}

func testWatcherNonce() string {
	return strings.Repeat("a", 32)
}

func withFilesystemWatcherStopTestSeams(t *testing.T, find func(int) (filesystemWatcherProcess, error)) {
	t.Helper()
	originalFind := findFilesystemWatcherProcess
	originalDelay := filesystemWatcherFallbackDelay
	originalTimeout := filesystemWatcherStopAckTimeout
	originalPoll := filesystemWatcherStopPollInterval
	findFilesystemWatcherProcess = find
	filesystemWatcherFallbackDelay = 0
	filesystemWatcherStopAckTimeout = time.Millisecond
	filesystemWatcherStopPollInterval = time.Millisecond
	t.Cleanup(func() {
		findFilesystemWatcherProcess = originalFind
		filesystemWatcherFallbackDelay = originalDelay
		filesystemWatcherStopAckTimeout = originalTimeout
		filesystemWatcherStopPollInterval = originalPoll
	})
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

func waitForSessionEvent(t *testing.T, eventsPath string, match func(model.Event) bool) model.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, err := eventlog.ReadFile(eventsPath)
		if err == nil {
			for _, event := range events {
				if match(event) {
					return event
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	events, _ := eventlog.ReadFile(eventsPath)
	t.Fatalf("timed out waiting for matching event in %+v", events)
	return model.Event{}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 16, 8, 0, 0, 0, time.UTC)
}

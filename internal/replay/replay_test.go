package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/ametel01/agentreceipt/internal/providerevidence"
	"github.com/ametel01/agentreceipt/internal/receipt"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
)

func TestBuildProducesVerifiedReplayWithPairedCommandAndChangedFiles(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_success",
						"command": "go test ./...",
					},
					"risk_signals": []any{},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":          "call_success",
					"command":          "go test ./...",
					"status":           "success",
					"exit_code":        0,
					"stdout_truncated": true,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "main.go",
					"action":     "modify",
					"sensitive":  true,
					"dependency": false,
				},
			},
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() {\n}\n"), 0o600); err != nil {
				t.Fatalf("write modified file: %v", err)
			}
		},
	)

	report, err := Build(context.Background(), Options{
		RepoPath:    repo,
		SessionID:   sessionID,
		GeneratedAt: replayNow(),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if !report.Verification.Valid {
		t.Fatalf("report verification valid = %t", report.Verification.Valid)
	}
	if report.Source.SessionState != model.SessionStateFinalized {
		t.Fatalf("session state = %q, want finalized", report.Source.SessionState)
	}
	if len(report.Commands) != 1 {
		t.Fatalf("commands = %+v", report.Commands)
	}
	if report.Commands[0].Command != "go test ./..." || report.Commands[0].Status != "success" {
		t.Fatalf("command = %+v", report.Commands[0])
	}
	if report.Summary.ChangedFileCount != len(report.Files) {
		t.Fatalf("changed file count mismatch: summary=%d files=%d", report.Summary.ChangedFileCount, len(report.Files))
	}
	if len(report.Files) == 0 || report.Files[0].Path != "main.go" || !report.Files[0].InFinalPatch {
		t.Fatalf("files = %+v", report.Files)
	}

	artifact := firstArtifact(report.Artifacts, filepath.Join("diffs", storage.FinalPatchFile))
	if artifact == nil {
		t.Fatalf("artifacts = %+v", report.Artifacts)
	}
	if !strings.HasSuffix(filepath.ToSlash(artifact.Path), filepath.ToSlash(filepath.Join(storage.DiffsDir, storage.FinalPatchFile))) {
		t.Fatalf("artifact path = %q", artifact.Path)
	}
}

func TestBuildRecordsFailedCommandExitCodeAndOutputSummary(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_failed",
						"command": "rm -rf /tmp",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":          "call_failed",
					"command":          "rm -rf /tmp",
					"status":           "failed",
					"exit_code":        7,
					"failed_reason":    "command failed",
					"stdout":           "nothing to see here",
					"stdout_truncated": true,
				},
			},
		},
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.Commands) != 1 || report.Commands[0].Status != "failed" || report.Commands[0].ExitCode == nil || *report.Commands[0].ExitCode != 7 {
		t.Fatalf("command = %+v", report.Commands)
	}
	if !strings.Contains(report.Commands[0].OutputSummary, "failed:") {
		t.Fatalf("output summary = %q", report.Commands[0].OutputSummary)
	}
}

func TestBuildIncludesUnpairedCommandResultGap(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"command": "git status --short",
					"status":  "failed",
				},
			},
		},
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.Commands) != 1 || report.Commands[0].Command != "git status --short" {
		t.Fatalf("commands = %+v", report.Commands)
	}
	if !containsGap(report.Gaps, "Unpaired command result for command") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
}

func TestBuildRedactsSecretsFromCommandOutputsAndRiskMessages(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_secret",
						"command": "cat .env",
					},
					"risk_signals": []any{
						map[string]any{
							"level":      string(model.RiskHigh),
							"signal":     "secret_access",
							"command":    "cat .env",
							"details":    "token=sk-really-secret-token",
							"confidence": string(model.ConfidenceHigh),
						},
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":         "call_secret",
					"command":         "cat .env",
					"status":          "failed",
					"failed_reason":   "access denied for token=sk-really-secret-token",
					"stderr_or_error": "authorization=Bearer sk-super-secret",
				},
			},
		},
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(report.Commands) != 1 {
		t.Fatalf("commands = %+v", report.Commands)
	}
	if strings.Contains(report.Commands[0].OutputSummary, "sk-really-secret-token") {
		t.Fatalf("command output summary leaks secret: %q", report.Commands[0].OutputSummary)
	}
	if !strings.Contains(report.Commands[0].OutputSummary, "[REDACTED]") {
		t.Fatalf("command output summary not redacted: %q", report.Commands[0].OutputSummary)
	}
	if len(report.Risks) == 0 {
		t.Fatalf("risks = %v", report.Risks)
	}
	if hasSecretLeak(report.Risks, "sk-really-secret-token") {
		t.Fatalf("risk message leaks secret: %q", report.Risks)
	}
	if len(report.Commands[0].RiskSignals) == 0 || strings.Contains(report.Commands[0].RiskSignals[0].Message, "sk-really-secret-token") {
		t.Fatalf("command risk signal not redacted: %+v", report.Commands[0].RiskSignals)
	}
	for _, signal := range report.Commands[0].RiskSignals {
		if !strings.Contains(signal.Message, "[REDACTED]") {
			t.Fatalf("command risk signal not redacted: %+v", signal)
		}
	}
}

func TestBuildCapturesMissingEvidenceGaps(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "main.go",
					"action":     "modify",
					"sensitive":  false,
					"dependency": false,
				},
			},
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc FooChanged() {}\n"), 0o600); err != nil {
				t.Fatalf("write modified file: %v", err)
			}
		},
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !containsGap(report.Gaps, "No provider tool events were observed.") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
	if !containsGap(report.Gaps, "No test command detected.") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
	if !containsGap(report.Gaps, "No lint command detected.") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
}

func TestBuildMarksInvalidWhenArtifactsAreTampered(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	if err := os.WriteFile(layout.FinalPatch, []byte("tampered patch\n"), 0o600); err != nil {
		t.Fatalf("tamper final patch: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Verification.Valid {
		t.Fatalf("expected invalid verification, got valid; report=%+v", report.Verification)
	}
	if !containsGap(report.Gaps, "Final patch hash verification failed") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
}

func TestBuildIncludesGapWhenSessionNotFinalized(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	state, err := readSessionStateForTest(layout.StateJSON)
	if err != nil {
		t.Fatalf("read session state: %v", err)
	}
	state.State = model.SessionStateActive
	if err := writeSessionStateForTest(layout.StateJSON, state); err != nil {
		t.Fatalf("write session state: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !containsGap(report.Gaps, "Session is not finalized") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
}

func TestBuildSortsTimelineAndCommandEvidenceRefs(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_b",
						"command": "echo beta",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_a",
						"command": "go test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id": "call_a",
					"status":  "success",
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id": "call_b",
					"status":  "success",
				},
			},
		},
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(report.Commands) != 2 {
		t.Fatalf("commands = %+v", report.Commands)
	}
	if !strings.Contains(report.Commands[0].EvidenceRefs[0], "seq=2") {
		t.Fatalf("commands evidence refs not deterministic: %+v", report.Commands)
	}
	if !strings.Contains(report.Commands[1].EvidenceRefs[0], "seq=3") {
		t.Fatalf("commands evidence refs not deterministic: %+v", report.Commands)
	}
	if report.Commands[0].Command != "echo beta" {
		t.Fatalf("command order = %+v", report.Commands)
	}

	for index := 1; index < len(report.Timeline); index++ {
		if report.Timeline[index-1].Seq >= report.Timeline[index].Seq {
			t.Fatalf("timeline not sorted: %+v", report.Timeline)
		}
	}
}

func TestWriteBundleCopiesArtifactsAndHashes(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	tracePath := filepath.Join(layout.ProviderCodexTraces, "test.trace.jsonl")
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o750); err != nil {
		t.Fatalf("mkdir traces: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("trace payload\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	bundleDir := t.TempDir()

	_, err := WriteBundle(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
		BundleDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}

	for _, path := range []string{
		"replay.json",
		storage.EventsFile,
		storage.ReceiptJSONFile,
		storage.ManifestFile,
		filepath.Join(storage.DiffsDir, storage.FinalPatchFile),
		filepath.Join(storage.ProviderDir, storage.ProviderCodexDir, storage.TracesDir, "test.trace.jsonl"),
	} {
		if _, err := os.Stat(filepath.Join(bundleDir, path)); err != nil {
			t.Fatalf("bundle path %q missing: %v", path, err)
		}
	}

	bundlePayload, err := os.ReadFile(filepath.Join(bundleDir, "replay.json"))
	if err != nil {
		t.Fatalf("read bundle replay.json: %v", err)
	}
	var bundledReport Report
	if err := json.Unmarshal(bundlePayload, &bundledReport); err != nil {
		t.Fatalf("unmarshal bundled report: %v", err)
	}
	if bundledReport.SessionID != sessionID {
		t.Fatalf("bundled session id = %q", bundledReport.SessionID)
	}
	if len(bundledReport.Artifacts) == 0 {
		t.Fatalf("bundled report has no artifacts")
	}
	for _, artifact := range bundledReport.Artifacts {
		if artifact.Path == "replay.json" {
			if artifact.Hash == "" {
				t.Fatalf("replay artifact hash should be present")
			}
			continue
		}
		sourcePath := filepath.Join(bundleDir, filepath.FromSlash(artifact.Path))
		if _, err := os.Stat(sourcePath); err != nil {
			t.Fatalf("artifact path %q not found: %v", sourcePath, err)
		}
		if artifact.Hash != replayFileHash(sourcePath) {
			t.Fatalf("artifact hash mismatch for %q: %q != %q", artifact.Path, artifact.Hash, replayFileHash(sourcePath))
		}
	}
}

func TestWriteBundleFailsForMissingRequiredArtifacts(t *testing.T) {
	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	if err := os.Remove(layout.ManifestJSON); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove manifest: %v", err)
	}
	bundleDir := t.TempDir()

	if _, err := WriteBundle(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
		BundleDir: bundleDir,
	}); err == nil {
		t.Fatal("WriteBundle() returned nil error with missing required artifact")
	}
}

func TestWriteBundleSkipsMissingOptionalTraces(t *testing.T) {
	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	if err := os.RemoveAll(layout.ProviderCodexTraces); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove traces: %v", err)
	}
	bundleDir := t.TempDir()

	report, err := WriteBundle(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
		BundleDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	if firstArtifact(report.Artifacts, "replay.json") == nil {
		t.Fatal("replay artifact missing")
	}
	if _, err := os.Stat(filepath.Join(bundleDir, storage.ProviderDir, storage.ProviderCodexDir, storage.TracesDir)); err == nil {
		entries, readErr := os.ReadDir(filepath.Join(bundleDir, storage.ProviderDir, storage.ProviderCodexDir, storage.TracesDir))
		if readErr != nil {
			t.Fatalf("read copied traces dir: %v", readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("copied traces directory should be empty when no source traces exist: %d", len(entries))
		}
	}
}

func TestWriteBundleExcludesRawProviderLogs(t *testing.T) {
	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	if err := os.MkdirAll(filepath.Dir(layout.CodexImportedSession), 0o750); err != nil {
		t.Fatalf("make provider dir: %v", err)
	}
	if err := os.WriteFile(layout.CodexImportedSession, []byte("raw-session-log\n"), 0o600); err != nil {
		t.Fatalf("write raw codex log: %v", err)
	}
	bundleDir := t.TempDir()

	if _, err := WriteBundle(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
		BundleDir: bundleDir,
	}); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundleDir, storage.ProviderDir, storage.ProviderCodexDir, storage.CodexImportedSession)); err == nil {
		t.Fatal("raw provider imported-session.jsonl should not be included in bundle")
	}
}

func finalizedReplaySession(t *testing.T, events []model.Event, beforeStop func(string)) (repo string, sessionID string, layout storage.Layout) {
	t.Helper()

	repo = newReplayGitRepo(t)
	manager := session.Manager{
		RepoPath: repo,
		Config:   config.Default(),
		Now:      replayNow,
	}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	layout, err = storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	if beforeStop != nil {
		beforeStop(repo)
	}
	if len(events) > 0 {
		normalized := make([]model.Event, 0, len(events))
		for index, event := range events {
			event.SessionID = state.SessionID
			event.CWD = repo
			if event.Timestamp.IsZero() {
				event.Timestamp = replayNow().Add(time.Duration(index+1) * time.Second)
			}
			if event.Source == "" {
				event.Source = providerevidence.SourceCodex
			}
			normalized = append(normalized, event)
		}
		if _, err := eventlog.AppendBatch(layout.EventsJSONL, normalized); err != nil {
			t.Fatalf("AppendBatch() error = %v", err)
		}
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := receipt.Finalize(context.Background(), receipt.Options{
		RepoPath:    repo,
		SessionID:   state.SessionID,
		KeyDir:      t.TempDir(),
		GeneratedAt: replayNow(),
	}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	sessionID = state.SessionID
	return repo, sessionID, layout
}

func newReplayGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runReplayGit(t, repo, "init")
	runReplayGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runReplayGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc Foo() {}\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runReplayGit(t, repo, "add", "main.go", "README.md")
	runReplayGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runReplayGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func readSessionStateForTest(path string) (session.State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return session.State{}, err
	}
	var state session.State
	if err := json.Unmarshal(data, &state); err != nil {
		return session.State{}, err
	}

	return state, nil
}

func writeSessionStateForTest(path string, state session.State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func hasSecretLeak(reasons []Risk, secret string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason.Message, secret) {
			return true
		}
	}

	return false
}

func replayFileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func firstArtifact(artifacts []Artifact, name string) *Artifact {
	for _, artifact := range artifacts {
		if artifact.Name == name {
			return &artifact
		}
	}

	return nil
}

func containsGap(gaps []string, needle string) bool {
	for _, gap := range gaps {
		if strings.Contains(gap, needle) {
			return true
		}
	}

	return false
}

func replayNow() time.Time {
	return time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
}

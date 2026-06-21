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

func TestBuildClassifiesWorkspaceChangesFromCleanStart(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		nil,
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() { return 1 }\n"), 0o600); err != nil {
				t.Fatalf("write modified main.go: %v", err)
			}
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

	if len(report.WorkspaceChange.PreExistingDirtyFiles) != 0 {
		t.Fatalf("pre-existing dirty files = %#v", report.WorkspaceChange.PreExistingDirtyFiles)
	}
	if len(report.WorkspaceChange.AgentModifiedCleanFiles) != 1 || report.WorkspaceChange.AgentModifiedCleanFiles[0] != "main.go" {
		t.Fatalf("agent_modified_clean_files = %#v", report.WorkspaceChange.AgentModifiedCleanFiles)
	}
	if len(report.WorkspaceChange.AgentCreatedChanges) != 0 || len(report.WorkspaceChange.AgentTouchedPreExistingFiles) != 0 {
		t.Fatalf("unexpected workspace changes: %+v", report.WorkspaceChange)
	}
	if !report.WorkspaceChange.FinalDiffMatchesWorkspace {
		t.Fatalf("expected final patch to match workspace diff")
	}
}

func TestBuildSeparatesDirtyStartAndAgentIntroducedFiles(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "scratch.txt"), []byte("scratch\n"), 0o600); err != nil {
				t.Fatalf("write pre-existing scratch file: %v", err)
			}
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() { return 2 }\n"), 0o600); err != nil {
				t.Fatalf("modify main.go: %v", err)
			}
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

	if !containsString(report.WorkspaceChange.PreExistingDirtyFiles, "scratch.txt") {
		t.Fatalf("pre-existing dirty files = %#v", report.WorkspaceChange.PreExistingDirtyFiles)
	}
	if len(report.WorkspaceChange.AgentTouchedPreExistingFiles) != 0 {
		t.Fatalf("agent_touched_pre_existing_files should be empty: %#v", report.WorkspaceChange.AgentTouchedPreExistingFiles)
	}
	if len(report.WorkspaceChange.AgentModifiedCleanFiles) != 1 || report.WorkspaceChange.AgentModifiedCleanFiles[0] != "main.go" {
		t.Fatalf("agent_modified_clean_files = %#v", report.WorkspaceChange.AgentModifiedCleanFiles)
	}
}

func TestBuildClassifiesAgentTouchedPreExistingFiles(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() { return 3 }\n"), 0o600); err != nil {
				t.Fatalf("modify main.go before start: %v", err)
			}
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() { return 4 }\n"), 0o600); err != nil {
				t.Fatalf("modify main.go during session: %v", err)
			}
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

	if !containsString(report.WorkspaceChange.PreExistingDirtyFiles, "main.go") {
		t.Fatalf("pre-existing dirty files = %#v", report.WorkspaceChange.PreExistingDirtyFiles)
	}
	if !containsString(report.WorkspaceChange.AgentTouchedPreExistingFiles, "main.go") {
		t.Fatalf("agent_touched_pre_existing_files = %#v", report.WorkspaceChange.AgentTouchedPreExistingFiles)
	}
	if len(report.WorkspaceChange.AgentModifiedCleanFiles) != 0 {
		t.Fatalf("agent_modified_clean_files should be empty: %#v", report.WorkspaceChange.AgentModifiedCleanFiles)
	}
}

func TestBuildClassifiesUntrackedPreExistingFileAsContext(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "new.txt"), []byte("temp\n"), 0o600); err != nil {
				t.Fatalf("write untracked file: %v", err)
			}
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "new.txt"), []byte("temp\nupdated\n"), 0o600); err != nil {
				t.Fatalf("modify untracked file: %v", err)
			}
			runReplayGit(t, repoRoot, "add", "new.txt")
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

	if !containsString(report.WorkspaceChange.PreExistingDirtyFiles, "new.txt") {
		t.Fatalf("pre-existing dirty files = %#v", report.WorkspaceChange.PreExistingDirtyFiles)
	}
	if !containsString(report.WorkspaceChange.AgentTouchedPreExistingFiles, "new.txt") {
		t.Fatalf("agent_touched_pre_existing_files = %#v", report.WorkspaceChange.AgentTouchedPreExistingFiles)
	}
}

func TestBuildDetectsFinalPatchWorkspaceMismatch(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		nil,
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() { return 6 }\n"), 0o600); err != nil {
				t.Fatalf("modify main.go: %v", err)
			}
		},
		nil,
	)

	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc Foo() { return 7 }\n"), 0o600); err != nil {
		t.Fatalf("mutate workspace after session: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if report.WorkspaceChange.FinalDiffMatchesWorkspace {
		t.Fatalf("final patch should not match workspace after post-session edit")
	}

	focus := BuildFocusReport(report)
	if focus.Verdict != focusVerdictBlock {
		t.Fatalf("focus verdict = %q, want %q", focus.Verdict, focusVerdictBlock)
	}
	hasDiffMismatch := false
	for _, task := range focus.ReviewTasks {
		if task.Kind == "diff_mismatch" && task.Priority == focusTaskPriorityP0 {
			hasDiffMismatch = true
			break
		}
	}
	if !hasDiffMismatch {
		t.Fatalf("expected diff_mismatch task for workspace diff mismatch")
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

func TestBuildIncludesInstructionFileMetadataFromCaptureEvents(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "AGENTS.md"), []byte("# Team Rules\n- follow instructions\n"), 0o600); err != nil {
				t.Fatalf("write AGENTS.md: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, "CLAUDE.md"), []byte("# Claude Instructions\n- do good things\n"), 0o600); err != nil {
				t.Fatalf("write CLAUDE.md: %v", err)
			}
		},
		nil,
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.InstructionFiles) != 2 {
		t.Fatalf("instruction_files = %#v", report.InstructionFiles)
	}
	byPath := make(map[string]struct {
		Hash  string
		Size  int64
		MTime string
	}, len(report.InstructionFiles))
	for _, entry := range report.InstructionFiles {
		if entry.Hash == "" || entry.Size <= 0 || entry.MTime == "" {
			t.Fatalf("instruction entry invalid: %#v", entry)
		}
		if len(entry.Summary) == 0 {
			t.Fatalf("instruction summary empty for %s", entry.Path)
		}
		byPath[entry.Path] = struct {
			Hash  string
			Size  int64
			MTime string
		}{Hash: entry.Hash, Size: entry.Size, MTime: entry.MTime}
	}
	if _, ok := byPath["AGENTS.md"]; !ok {
		t.Fatalf("missing AGENTS.md metadata: %#v", report.InstructionFiles)
	}
	if _, ok := byPath["CLAUDE.md"]; !ok {
		t.Fatalf("missing CLAUDE.md metadata: %#v", report.InstructionFiles)
	}
}

func TestBuildMapsInstructionCaptureWarningsToReplayGaps(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySessionWithSetup(
		t,
		func(repoRoot string) {
			if err := os.Mkdir(filepath.Join(repoRoot, "AGENTS.md"), 0o750); err != nil {
				t.Fatalf("mkdir AGENTS.md: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, "CLAUDE.md"), []byte("# Claude Instructions\n"), 0o600); err != nil {
				t.Fatalf("write CLAUDE.md: %v", err)
			}
		},
		nil,
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.InstructionFiles) != 1 || report.InstructionFiles[0].Path != "CLAUDE.md" {
		t.Fatalf("unexpected instruction metadata: %#v", report.InstructionFiles)
	}
	if !containsGap(report.Gaps, "Instruction file is not a regular file: AGENTS.md") {
		t.Fatalf("expected warning gap from unreadable instruction: %v", report.Gaps)
	}
	if !containsGap(report.Gaps, "No provider tool events were observed.") {
		t.Fatalf("expected provider-missing warning gap: %v", report.Gaps)
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

func TestBuildRedactsCommandOutputWithoutReplayingRisk(t *testing.T) {
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
	if len(report.Risks) != 0 {
		t.Fatalf("risks = %v", report.Risks)
	}
	if !report.Privacy.RedactionApplied {
		t.Fatalf("privacy redaction_applied = false: %+v", report.Privacy)
	}
	if !report.Privacy.SensitiveContentDetected {
		t.Fatalf("privacy sensitive_content_detected = false: %+v", report.Privacy)
	}
	if report.Privacy.RawProviderLogsExposed {
		t.Fatalf("privacy raw_provider_logs_exposed = true: %+v", report.Privacy)
	}
	if report.Privacy.OutputCaps.MaxOutputSummaryRunes != maxOutputSummaryRunes || report.Privacy.OutputCaps.MaxFailedCommandSummaryRunes != maxOutputSummaryRunes {
		t.Fatalf("privacy output caps = %+v", report.Privacy.OutputCaps)
	}
	if len(report.Privacy.RedactionPatterns) == 0 {
		t.Fatalf("privacy redaction_patterns = %+v", report.Privacy.RedactionPatterns)
	}
	if !containsString(report.Privacy.RedactedFields, "commands.output_summary") || !containsString(report.Privacy.RedactedFields, "failed_command_details.failed_reason") || !containsString(report.Privacy.RedactedFields, "failed_command_details.stderr_or_error_summary") {
		t.Fatalf("privacy redacted_fields = %+v", report.Privacy.RedactedFields)
	}
	if got := findClaim(report.Claims, "verification.verdict"); got == nil || got.Status == "" || got.Confidence == "" {
		t.Fatalf("verification.verdict claim = %+v", got)
	}
	if got := findClaim(report.Claims, "privacy.redaction"); got == nil || got.Status == "" || got.Confidence == "" {
		t.Fatalf("privacy.redaction claim = %+v", got)
	}
	if got := findClaim(report.Claims, "outcome"); got == nil || got.Status != outcomeStatusFailed {
		t.Fatalf("outcome claim = %+v", got)
	}
}

func TestBuildDoesNotExposeProviderRiskSignalRawField(t *testing.T) {
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
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(raw), "\"risk_signals\"") {
		t.Fatalf("replay report should not expose raw risk_signals in JSON: %s", raw)
	}
	if report.Privacy.RawProviderLogsExposed {
		t.Fatalf("privacy raw_provider_logs_exposed = true: %+v", report.Privacy)
	}
}

func TestBuildClassifiesCompletedOutcomeWithPassingGates(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_read",
						"command": "cat main.go",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_read",
					"command":   "cat main.go",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_test",
						"command": "go test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_test",
					"command":   "go test ./...",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_lint",
						"command": "npm run lint",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_lint",
					"command":   "npm run lint",
					"status":    "success",
					"exit_code": 0,
				},
			},
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
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() {}\n"), 0o600); err != nil {
				t.Fatalf("write main.go: %v", err)
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

	if report.Outcome.Status != outcomeStatusCompleted {
		t.Fatalf("outcome = %+v", report.Outcome)
	}
	if got := findClaim(report.Claims, "outcome"); got == nil || got.Status != outcomeStatusCompleted {
		t.Fatalf("outcome claim = %+v", got)
	}
}

func TestBuildClassifiesCompletedWithGapsAndFailedOutcomes(t *testing.T) {
	t.Parallel()

	t.Run("completed_with_gaps", func(t *testing.T) {
		t.Parallel()
		repo, sessionID, _ := finalizedReplaySession(
			t,
			[]model.Event{
				{
					Source: providerevidence.SourceCodex,
					Type:   providerevidence.TypeCommand,
					Payload: map[string]any{
						"tool_call": map[string]any{
							"call_id": "call_test",
							"command": "go test ./...",
						},
					},
				},
				{
					Source: providerevidence.SourceCodex,
					Type:   providerevidence.TypeCommandResult,
					Payload: map[string]any{
						"call_id":   "call_test",
						"command":   "go test ./...",
						"status":    "success",
						"exit_code": 0,
					},
				},
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
				if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Foo() {}\n"), 0o600); err != nil {
					t.Fatalf("write main.go: %v", err)
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
		if report.Outcome.Status != outcomeStatusCompletedWithGaps {
			t.Fatalf("outcome = %+v", report.Outcome)
		}
		if got := findClaim(report.Claims, "outcome"); got == nil || got.Status != outcomeStatusCompletedWithGaps {
			t.Fatalf("outcome claim = %+v", got)
		}
	})

	t.Run("failed_and_abandoned", func(t *testing.T) {
		t.Parallel()
		repo, sessionID, layout := finalizedReplaySession(
			t,
			[]model.Event{
				{
					Source: providerevidence.SourceCodex,
					Type:   providerevidence.TypeCommand,
					Payload: map[string]any{
						"tool_call": map[string]any{
							"call_id": "call_failed",
							"command": "go test ./...",
						},
					},
				},
				{
					Source: providerevidence.SourceCodex,
					Type:   providerevidence.TypeCommandResult,
					Payload: map[string]any{
						"call_id":   "call_failed",
						"command":   "go test ./...",
						"status":    "failed",
						"exit_code": 1,
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
		if report.Outcome.Status != outcomeStatusFailed {
			t.Fatalf("failed outcome = %+v", report.Outcome)
		}
		if got := findClaim(report.Claims, "outcome"); got == nil || got.Status != outcomeStatusFailed {
			t.Fatalf("outcome claim = %+v", got)
		}

		state, err := readSessionStateForTest(layout.StateJSON)
		if err != nil {
			t.Fatalf("read session state: %v", err)
		}
		state.State = model.SessionStateActive
		if err := writeSessionStateForTest(layout.StateJSON, state); err != nil {
			t.Fatalf("write session state: %v", err)
		}

		abandonedReport, err := Build(context.Background(), Options{
			RepoPath:  repo,
			SessionID: sessionID,
		})
		if err != nil {
			t.Fatalf("Build() abandoned error = %v", err)
		}
		if abandonedReport.Outcome.Status != outcomeStatusAbandoned {
			t.Fatalf("abandoned outcome = %+v", abandonedReport.Outcome)
		}
	})
}

func TestBuildIncludesEvaluatorSignalsForAttemptsResultsAndFileSignals(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeEvent,
				Payload: map[string]any{
					"payload_type": "token_count",
					"token_usage": map[string]any{
						"total_tokens": 88,
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeEvent,
				Payload: map[string]any{
					"payload_type": "token_count",
					"raw": map[string]any{
						"payload": map[string]any{
							"info": map[string]any{
								"last_token_usage": map[string]any{
									"total_tokens": 12,
								},
							},
						},
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_test",
						"command": "go test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_test",
					"command":   "go test ./...",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_cat",
						"command": "cat main.go",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":          "call_cat",
					"command":          "cat main.go",
					"status":           "success",
					"stdout":           "ok",
					"stdout_truncated": false,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_edit",
						"command": "cat > main.go",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_edit",
					"command":   "cat > main.go",
					"status":    "failed",
					"exit_code": 2,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_rm",
						"command": "rm -rf /tmp",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id": "call_rm",
					"command": "rm -rf /tmp",
					"status":  "success",
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_fetch",
						"command": "curl https://example.com",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_fetch",
					"command":   "curl https://example.com",
					"status":    "failed",
					"exit_code": 1,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_make_test",
						"command": "make test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_make_test",
					"command":   "make test ./...",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"command": "git commit -m \"oops\"",
					"status":  "failed",
				},
			},
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
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "main_test.go",
					"action":     "modify",
					"sensitive":  false,
					"dependency": false,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "docs/guide.md",
					"action":     "modify",
					"sensitive":  false,
					"dependency": false,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "go.mod",
					"action":     "modify",
					"sensitive":  false,
					"dependency": true,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       ".env.local",
					"action":     "modify",
					"sensitive":  true,
					"dependency": false,
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

	signals := report.EvaluatorSignals
	if signals.TestCommandCount != 2 {
		t.Fatalf("test_command_count = %d, want 2", signals.TestCommandCount)
	}
	if signals.TotalTokens != 100 {
		t.Fatalf("total_tokens = %d, want 100", signals.TotalTokens)
	}
	if signals.LintCommandCount != 0 {
		t.Fatalf("lint_command_count = %d, want 0", signals.LintCommandCount)
	}
	if signals.TypecheckCommandCount != 0 {
		t.Fatalf("typecheck_command_count = %d, want 0", signals.TypecheckCommandCount)
	}
	if signals.ReadCommandCount != 1 {
		t.Fatalf("read_command_count = %d, want 1", signals.ReadCommandCount)
	}
	if signals.EditCommandCount != 1 {
		t.Fatalf("edit_command_count = %d, want 1", signals.EditCommandCount)
	}
	if signals.FailedCommandStreak != 1 {
		t.Fatalf("failed_command_streak = %d, want 1", signals.FailedCommandStreak)
	}
	if signals.SameFileEditCount != 2 {
		t.Fatalf("same_file_edit_count = %d, want 2", signals.SameFileEditCount)
	}
	if signals.ReadToEditRatio != 0.5 {
		t.Fatalf("read_to_edit_ratio = %f, want 0.5", signals.ReadToEditRatio)
	}
	if !signals.ValidationAfterLastEdit {
		t.Fatalf("validation_after_last_edit = %v, want true", signals.ValidationAfterLastEdit)
	}
	if signals.LastEditTime == "" || signals.LastValidationTime == "" {
		t.Fatalf("validation timing signals missing: last_edit_time=%q last_validation_time=%q", signals.LastEditTime, signals.LastValidationTime)
	}
	if signals.LastValidationTime < signals.LastEditTime {
		t.Fatalf("last_validation_time %q should be >= last_edit_time %q", signals.LastValidationTime, signals.LastEditTime)
	}
	if signals.WriteCommandCount != 1 {
		t.Fatalf("write_command_count = %d, want 1", signals.WriteCommandCount)
	}
	if signals.FailedCommandCount != 3 {
		t.Fatalf("failed_command_count = %d, want 3", signals.FailedCommandCount)
	}
	if signals.NetworkCommandCount != 1 {
		t.Fatalf("network_command_count = %d, want 1", signals.NetworkCommandCount)
	}
	if signals.DestructiveCommandCount != 1 {
		t.Fatalf("destructive_command_count = %d, want 1", signals.DestructiveCommandCount)
	}
	if signals.GitMutationCommandCount != 1 {
		t.Fatalf("git_mutation_command_count = %d, want 1", signals.GitMutationCommandCount)
	}
	if signals.CommitCount != 1 {
		t.Fatalf("commit_count = %d, want 1", signals.CommitCount)
	}
	if signals.DependencyFileChangeCount != 1 {
		t.Fatalf("dependency_file_change_count = %d, want 1", signals.DependencyFileChangeCount)
	}
	if signals.SensitiveFileChangeCount != 1 {
		t.Fatalf("sensitive_file_change_count = %d, want 1", signals.SensitiveFileChangeCount)
	}
	if signals.ChangedTestFileCount != 1 {
		t.Fatalf("changed_test_file_count = %d, want 1", signals.ChangedTestFileCount)
	}
	if signals.ChangedDocFileCount != 1 {
		t.Fatalf("changed_doc_file_count = %d, want 1", signals.ChangedDocFileCount)
	}
	if signals.ChangedProductionFileCount != 3 {
		t.Fatalf("changed_production_file_count = %d, want 3", signals.ChangedProductionFileCount)
	}
}

func TestBuildEvaluatorSignalsHandlesNoCommandEvidence(t *testing.T) {
	t.Parallel()

	signals := buildEvaluatorSignals(nil, nil, nil)
	if signals.TotalTokens != 0 {
		t.Fatalf("total_tokens = %d, want 0", signals.TotalTokens)
	}
	if signals.FailedCommandStreak != 0 {
		t.Fatalf("failed_command_streak = %d, want 0", signals.FailedCommandStreak)
	}
	if signals.SameFileEditCount != 0 {
		t.Fatalf("same_file_edit_count = %d, want 0", signals.SameFileEditCount)
	}
	if signals.ReadToEditRatio != 0 {
		t.Fatalf("read_to_edit_ratio = %f, want 0", signals.ReadToEditRatio)
	}
	if signals.ValidationAfterLastEdit {
		t.Fatalf("validation_after_last_edit = %v, want false", signals.ValidationAfterLastEdit)
	}
	if signals.LastEditTime != "" || signals.LastValidationTime != "" {
		t.Fatalf("unexpected validation timestamps: last_edit_time=%q last_validation_time=%q", signals.LastEditTime, signals.LastValidationTime)
	}
}

func TestBuildEvaluatorSignalsCountsFailedCommandStreak(t *testing.T) {
	t.Parallel()

	signals := buildEvaluatorSignals([]Command{
		{Command: "go test ./...", Status: "failed"},
		{Command: "make test", Status: "failed"},
		{Command: "cat main.go", Status: "success"},
		{Command: "go test ./internal/replay", Status: "failed"},
	}, nil, nil)
	if signals.FailedCommandStreak != 2 {
		t.Fatalf("failed_command_streak = %d, want 2", signals.FailedCommandStreak)
	}
}

func TestBuildIncludesEvidenceIndexCoverageAndStableOrder(t *testing.T) {
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
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_success",
					"command":   "go test ./...",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_secret",
						"command": "echo sk-secret-abc123",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_secret",
					"command":   "echo sk-secret-abc123",
					"status":    "success",
					"exit_code": 0,
				},
			},
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
		nil,
	)

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	entries := report.EvidenceIndex
	if len(entries) == 0 {
		t.Fatalf("evidence_index is empty")
	}

	entryByRef := map[string]EvidenceEntry{}
	for _, entry := range entries {
		entryByRef[entry.Ref] = entry
	}

	for index := 1; index < len(entries); index++ {
		if entries[index-1].Ref > entries[index].Ref {
			t.Fatalf("evidence_index not sorted: %#v", entries)
		}
	}

	if len(entryByRef) != len(entries) {
		t.Fatalf("evidence_index has duplicate refs: len=%d unique=%d", len(entries), len(entryByRef))
	}

	if got := entryByRef["events.jsonl#seq=1"]; got.Ref == "" {
		t.Fatalf("missing events.jsonl#seq=1 event artifact entry: %#v", got)
	}
	if got := entryByRef["commands/0001"]; got.Ref == "" || got.Type != "command" {
		t.Fatalf("missing commands/0001 entry: %#v", got)
	}
	if got := entryByRef["files/main.go"]; got.Ref == "" || got.Type != "file" {
		t.Fatalf("missing files/main.go entry: %#v", got)
	}
	if got := entryByRef["commands/0002"]; got.Ref == "" || !got.Redacted {
		t.Fatalf("missing redacted command entry for secret command: %#v", got)
	}
	eventsJSONLArtifact := false
	for _, entry := range entries {
		if filepath.Base(entry.Ref) == storage.EventsFile {
			if entry.Type != "artifact" {
				t.Fatalf("events.jsonl artifact entry should be type artifact: %#v", entry)
			}
			eventsJSONLArtifact = true
			break
		}
	}
	if !eventsJSONLArtifact {
		t.Fatalf("missing events.jsonl artifact entry in evidence_index")
	}
	if got := entryByRef["diffs/final.patch"]; got.Ref == "" || got.Type != "artifact" {
		t.Fatalf("missing diffs/final.patch artifact entry: %#v", got)
	}
}

func TestBuildIncludesQualityGatesForVerifyCommand(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_verify",
						"command": "make verify",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_verify",
					"command":   "make verify",
					"status":    "success",
					"exit_code": 0,
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

	if report.QualityGates.Verify.Status != qualityGateStatusPassed {
		t.Fatalf("verify gate status = %q", report.QualityGates.Verify.Status)
	}
	if report.QualityGates.Verify.LastExitCode == nil || *report.QualityGates.Verify.LastExitCode != 0 {
		t.Fatalf("verify gate last_exit_code = %+v", report.QualityGates.Verify.LastExitCode)
	}
	if report.QualityGates.Tests.Status != qualityGateStatusPassed {
		t.Fatalf("tests gate status = %q", report.QualityGates.Tests.Status)
	}
	if !containsEvidencePrefix(report.QualityGates.Verify.EvidenceRefs, "events.jsonl#seq=") {
		t.Fatalf("verify gate evidence refs = %+v", report.QualityGates.Verify.EvidenceRefs)
	}
	if len(report.QualityGates.Verify.Commands) == 0 || report.QualityGates.Verify.Commands[0] != "make verify" {
		t.Fatalf("verify gate commands = %+v", report.QualityGates.Verify.Commands)
	}
}

func TestBuildIncludesFailedCommandDetailsForFailedTestCommand(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_failed_test",
						"command": "go test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":          "call_failed_test",
					"command":          "go test ./...",
					"status":           "failed",
					"exit_code":        1,
					"failed_reason":    "command failed due to api_key=sk-REDACTED",
					"stdout":           "go test output: secret token=sk-super-secret",
					"stdout_truncated": true,
					"stderr_or_error":  "authorization=Bearer another-secret-token",
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

	if report.QualityGates.Tests.Status != qualityGateStatusFailed {
		t.Fatalf("tests gate status = %q", report.QualityGates.Tests.Status)
	}
	if report.QualityGates.Tests.LastExitCode == nil || *report.QualityGates.Tests.LastExitCode != 1 {
		t.Fatalf("tests gate last_exit_code = %+v", report.QualityGates.Tests.LastExitCode)
	}
	if len(report.FailedCommandDetails) != 1 {
		t.Fatalf("failed_command_details = %+v", report.FailedCommandDetails)
	}

	detail := report.FailedCommandDetails[0]
	if detail.ExitCode == nil || *detail.ExitCode != 1 {
		t.Fatalf("failed detail exit_code = %+v", detail.ExitCode)
	}
	if detail.Cwd == "" {
		t.Fatalf("failed detail missing cwd")
	}
	if detail.Time == "" {
		t.Fatalf("failed detail missing time")
	}
	if detail.FailedReason == "" {
		t.Fatalf("missing failed_reason: %+v", detail)
	}
	if detail.OutputTruncated != true {
		t.Fatalf("expected output_truncated=true, got %+v", detail.OutputTruncated)
	}
	if detail.StdoutSummary == "" {
		t.Fatalf("missing stdout_summary: %+v", detail)
	}
	if !strings.Contains(detail.StderrOrErrorSummary, "[REDACTED]") {
		t.Fatalf("expected redacted stderr summary, got %q", detail.StderrOrErrorSummary)
	}
	if !strings.Contains(detail.StdoutSummary, "[REDACTED]") {
		t.Fatalf("expected redacted stdout summary, got %q", detail.StdoutSummary)
	}
	if !strings.Contains(detail.FailedReason, "[REDACTED]") {
		t.Fatalf("expected redacted failed_reason, got %q", detail.FailedReason)
	}
}

func TestBuildMarksMissingQualityGateSignals(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_test",
						"command": "go test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id": "call_test",
					"command": "go test ./...",
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

	if report.QualityGates.Lint.Status != qualityGateStatusNotRun {
		t.Fatalf("lint gate status = %q", report.QualityGates.Lint.Status)
	}
	if report.QualityGates.Typecheck.Status != qualityGateStatusNotRun {
		t.Fatalf("typecheck gate status = %q", report.QualityGates.Typecheck.Status)
	}
	if report.QualityGates.Coverage.Status != qualityGateStatusNotRun {
		t.Fatalf("coverage gate status = %q", report.QualityGates.Coverage.Status)
	}
	if report.QualityGates.Tests.Status != qualityGateStatusPassed {
		t.Fatalf("tests gate status = %q", report.QualityGates.Tests.Status)
	}
}

func TestBuildIncludesPolicyChecksAndReviewFocus(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(
		t,
		[]model.Event{
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_read",
						"command": "cat main.go",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_read",
					"command":   "cat main.go",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_edit",
						"command": "apply_patch",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_edit",
					"command":   "apply_patch",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_net",
						"command": "curl https://example.com",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_net",
					"command":   "curl https://example.com",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_rm",
						"command": "rm -rf /tmp/cache",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_rm",
					"command":   "rm -rf /tmp/cache",
					"status":    "failed",
					"exit_code": 1,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_test",
						"command": "go test ./...",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_test",
					"command":   "go test ./...",
					"status":    "success",
					"exit_code": 0,
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommand,
				Payload: map[string]any{
					"tool_call": map[string]any{
						"call_id": "call_commit",
						"command": "git commit -m \"update\"",
					},
				},
			},
			{
				Source: providerevidence.SourceCodex,
				Type:   providerevidence.TypeCommandResult,
				Payload: map[string]any{
					"call_id":   "call_commit",
					"command":   "git commit -m \"update\"",
					"status":    "success",
					"exit_code": 0,
				},
			},
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
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "go.mod",
					"action":     "modify",
					"sensitive":  false,
					"dependency": true,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       ".github/workflows/ci.yml",
					"action":     "modify",
					"sensitive":  false,
					"dependency": false,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       ".env.local",
					"action":     "modify",
					"sensitive":  true,
					"dependency": false,
				},
			},
			{
				Source: "fs_watcher",
				Type:   "fs.change",
				Payload: map[string]any{
					"path":       "generated/widget.gen.go",
					"action":     "modify",
					"sensitive":  false,
					"dependency": false,
				},
			},
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Changed() {}\n"), 0o600); err != nil {
				t.Fatalf("write main.go: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/project\n"), 0o600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(repoRoot, ".github", "workflows"), 0o750); err != nil {
				t.Fatalf("mkdir workflows: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"), []byte("name: CI\n"), 0o600); err != nil {
				t.Fatalf("write ci.yml: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, ".env.local"), []byte("TOKEN=secret\n"), 0o600); err != nil {
				t.Fatalf("write env: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(repoRoot, "generated"), 0o750); err != nil {
				t.Fatalf("mkdir generated: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, "generated", "widget.gen.go"), []byte("package generated\n"), 0o600); err != nil {
				t.Fatalf("write generated file: %v", err)
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

	if got, ok := findPolicyCheck(report.PolicyChecks, "target_file_read_before_edit"); !ok || got.Status != policyCheckStatusPass {
		t.Fatalf("target_file_read_before_edit = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "related_context_read_before_edit"); !ok || got.Status != policyCheckStatusPass {
		t.Fatalf("related_context_read_before_edit = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "tests_run_after_code_changes"); !ok || got.Status != policyCheckStatusPass {
		t.Fatalf("tests_run_after_code_changes = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "lint_run_after_code_changes"); !ok || got.Status != policyCheckStatusUnknown {
		t.Fatalf("lint_run_after_code_changes = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "typecheck_run_when_applicable"); !ok || got.Status != policyCheckStatusNotApplicable {
		t.Fatalf("typecheck_run_when_applicable = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "destructive_command_used"); !ok || got.Status != policyCheckStatusFail {
		t.Fatalf("destructive_command_used = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "network_command_used"); !ok || got.Status != policyCheckStatusFail {
		t.Fatalf("network_command_used = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "dependency_file_changed"); !ok || got.Status != policyCheckStatusWarn {
		t.Fatalf("dependency_file_changed = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "sensitive_file_changed"); !ok || got.Status != policyCheckStatusWarn {
		t.Fatalf("sensitive_file_changed = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "ci_or_security_file_changed"); !ok || got.Status != policyCheckStatusWarn {
		t.Fatalf("ci_or_security_file_changed = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "generated_file_changed"); !ok || got.Status != policyCheckStatusWarn {
		t.Fatalf("generated_file_changed = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "commit_created"); !ok || got.Status != policyCheckStatusPass {
		t.Fatalf("commit_created = %+v", got)
	}
	if len(report.ReviewFocus) == 0 {
		t.Fatal("review_focus is empty")
	}
	if !containsReviewFocus(report.ReviewFocus, "Production code changed without test file changes.") {
		t.Fatalf("review_focus = %+v", report.ReviewFocus)
	}
	if !containsReviewFocus(report.ReviewFocus, "Failed command: rm -rf /tmp/cache") {
		t.Fatalf("review_focus = %+v", report.ReviewFocus)
	}
	if !containsReviewFocus(report.ReviewFocus, "No command evidence was available to determine whether lint ran after code changes.") {
		t.Fatalf("review_focus = %+v", report.ReviewFocus)
	}
	if !containsPolicyEvidenceRefs(report.PolicyChecks) {
		t.Fatalf("policy checks missing evidence refs: %+v", report.PolicyChecks)
	}
}

func TestBuildDistinguishesUnknownPolicyChecksFromFailures(t *testing.T) {
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
			if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc Changed() {}\n"), 0o600); err != nil {
				t.Fatalf("write main.go: %v", err)
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

	if got, ok := findPolicyCheck(report.PolicyChecks, "tests_run_after_code_changes"); !ok || got.Status != policyCheckStatusUnknown {
		t.Fatalf("tests_run_after_code_changes = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "network_command_used"); !ok || got.Status != policyCheckStatusUnknown {
		t.Fatalf("network_command_used = %+v", got)
	}
	if got, ok := findPolicyCheck(report.PolicyChecks, "commit_created"); !ok || got.Status != policyCheckStatusNotApplicable {
		t.Fatalf("commit_created = %+v", got)
	}
	if !containsReviewFocus(report.ReviewFocus, "No provider tool events were observed.") {
		t.Fatalf("review_focus = %+v", report.ReviewFocus)
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
	if containsGap(report.Gaps, "No test command detected.") {
		t.Fatalf("review-only gap present unexpectedly: %+v", report.Gaps)
	}
	if containsGap(report.Gaps, "No lint command detected.") {
		t.Fatalf("review-only gap present unexpectedly: %+v", report.Gaps)
	}
	if containsGap(report.Gaps, "No typecheck command detected for TypeScript changes.") {
		t.Fatalf("review-only gap present unexpectedly: %+v", report.Gaps)
	}
}

func TestBuildIncludesFinalPatchFilesWithoutFsEvents(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	if err := os.WriteFile(layout.FinalPatch, []byte("diff --git a/main.go b/main.go\nindex 111..222 100644\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"), 0o600); err != nil {
		t.Fatalf("write final patch: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("files = %+v", report.Files)
	}
	if report.Summary.ChangedFileCount != len(report.Files) {
		t.Fatalf("changed file count mismatch: %d != %d", report.Summary.ChangedFileCount, len(report.Files))
	}
	file := report.Files[0]
	if file.Path != "main.go" {
		t.Fatalf("file path = %q", file.Path)
	}
	if !file.InFinalPatch {
		t.Fatalf("file InFinalPatch = false: %+v", file)
	}
	if !containsEvidenceRef(file.EvidenceRefs, "diffs/final.patch") {
		t.Fatalf("file evidence refs = %+v", file.EvidenceRefs)
	}
}

func TestBuildMergesFinalPatchAndFilesystemEvidence(t *testing.T) {
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
					"path":       "README.md",
					"action":     "modify",
					"dependency": false,
					"sensitive":  false,
				},
			},
		},
		func(repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("updated\n"), 0o600); err != nil {
				t.Fatalf("write readme: %v", err)
			}
		},
	)
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := os.WriteFile(layout.FinalPatch, []byte("diff --git a/README.md b/README.md\nindex 111..222 100644\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n\ndiff --git a/main.go b/main.go\nindex 222..333 100644\nnew file mode 100644\n--- /dev/null\n+++ b/main.go\n"), 0o600); err != nil {
		t.Fatalf("write final patch: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.Files) != 2 {
		t.Fatalf("files = %+v", report.Files)
	}
	if report.Summary.ChangedFileCount != len(report.Files) {
		t.Fatalf("changed file count mismatch: summary=%d files=%d", report.Summary.ChangedFileCount, len(report.Files))
	}
	readme := findFile(report.Files, "README.md")
	if readme == nil {
		t.Fatalf("missing README.md in files: %+v", report.Files)
	}
	if !containsEvidencePrefix(readme.EvidenceRefs, "events.jsonl#seq=") {
		t.Fatalf("README evidence refs = %+v", readme.EvidenceRefs)
	}
	if !containsEvidenceRef(readme.EvidenceRefs, "diffs/final.patch") {
		t.Fatalf("README evidence refs = %+v", readme.EvidenceRefs)
	}
}

func TestBuildMarksInvalidWhenArtifactsAreTampered(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		tamper    func(t *testing.T, repoRoot string, sessionID string, layout storage.Layout)
		expectGap string
		check     func(*testing.T, Verification)
	}{
		{
			name: "events",
			tamper: func(t *testing.T, repoRoot string, sessionID string, layout storage.Layout) {
				t.Helper()
				if err := os.WriteFile(layout.EventsJSONL, []byte("not-valid-jsonl\n"), 0o600); err != nil {
					t.Fatalf("tamper events: %v", err)
				}
			},
			expectGap: "Event chain verification failed",
			check: func(t *testing.T, verification Verification) {
				if verification.EventChainValid {
					t.Fatalf("event chain should be invalid: %+v", verification)
				}
				if !verification.FinalPatchHashValid || !verification.ManifestHashValid || !verification.ReceiptHashValid || !verification.SignatureValid {
					t.Fatalf("unexpected component validity for event-tamper case: %+v", verification)
				}
			},
		},
		{
			name: "receipt",
			tamper: func(t *testing.T, repoRoot string, sessionID string, layout storage.Layout) {
				t.Helper()
				decoded, err := receipt.Read(layout)
				if err != nil {
					t.Fatalf("read receipt: %v", err)
				}
				decoded.Verification.ReceiptHash = "sha256:tampered"
				data, err := json.MarshalIndent(decoded, "", "  ")
				if err != nil {
					t.Fatalf("marshal receipt: %v", err)
				}
				if err := os.WriteFile(layout.ReceiptJSON, append(data, '\n'), 0o600); err != nil {
					t.Fatalf("write receipt: %v", err)
				}
			},
			expectGap: "Signature verification failed",
			check: func(t *testing.T, verification Verification) {
				if verification.SignatureValid {
					t.Fatalf("signature should be invalid: %+v", verification)
				}
				if verification.ReceiptHashValid {
					t.Fatalf("receipt hash should be invalid after tampering: %+v", verification)
				}
				if !verification.EventChainValid || !verification.ManifestHashValid || !verification.FinalPatchHashValid {
					t.Fatalf("unexpected component validity for receipt-tamper case: %+v", verification)
				}
			},
		},
		{
			name: "manifest",
			tamper: func(t *testing.T, repoRoot string, sessionID string, layout storage.Layout) {
				t.Helper()
				if err := os.WriteFile(layout.ManifestJSON, []byte("tampered manifest\n"), 0o600); err != nil {
					t.Fatalf("tamper manifest: %v", err)
				}
			},
			expectGap: "Manifest hash verification failed",
			check: func(t *testing.T, verification Verification) {
				if verification.ManifestHashValid {
					t.Fatalf("manifest hash should be invalid: %+v", verification)
				}
				if !verification.EventChainValid || !verification.FinalPatchHashValid || !verification.ReceiptHashValid || !verification.SignatureValid {
					t.Fatalf("unexpected component validity for manifest-tamper case: %+v", verification)
				}
			},
		},
		{
			name: "final patch",
			tamper: func(t *testing.T, repoRoot string, sessionID string, layout storage.Layout) {
				t.Helper()
				if err := os.WriteFile(layout.FinalPatch, []byte("tampered patch\n"), 0o600); err != nil {
					t.Fatalf("tamper final patch: %v", err)
				}
			},
			expectGap: "Final patch hash verification failed",
			check: func(t *testing.T, verification Verification) {
				if verification.FinalPatchHashValid {
					t.Fatalf("final patch hash should be invalid: %+v", verification)
				}
				if !verification.EventChainValid || !verification.ManifestHashValid || !verification.ReceiptHashValid || !verification.SignatureValid {
					t.Fatalf("unexpected component validity for final patch-tamper case: %+v", verification)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
			tc.tamper(t, repo, sessionID, layout)

			report, err := Build(context.Background(), Options{
				RepoPath:  repo,
				SessionID: sessionID,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if report.Verification.Valid {
				t.Fatalf("expected invalid verification, got valid: %s", tc.name)
			}
			if !containsGap(report.Gaps, tc.expectGap) {
				t.Fatalf("gaps for %s = %+v", tc.name, report.Gaps)
			}
			if tc.check != nil {
				tc.check(t, report.Verification)
			}
		})
	}
}

func TestBuildVerificationProvidesComponentFlagsAndSignatureErrorCodes(t *testing.T) {
	t.Parallel()

	repo, sessionID, _ := finalizedReplaySession(t, nil, nil)
	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !report.Verification.Valid {
		t.Fatalf("expected valid verification for baseline session: %+v", report.Verification)
	}
	if !report.Verification.IntegrityValid {
		t.Fatalf("expected integrity_valid=true for baseline session: %+v", report.Verification)
	}
	if !report.Verification.AuthenticityValid {
		t.Fatalf("expected authenticity_valid=true for baseline session: %+v", report.Verification)
	}
	if report.Verification.AuthenticityStatus != authenticityStatusAuthentic {
		t.Fatalf("unexpected authenticity_status: %q", report.Verification.AuthenticityStatus)
	}
	if report.Verification.TrustStatus != trustStatusNotConfigured {
		t.Fatalf("expected not-configured trust_status for baseline session: %q", report.Verification.TrustStatus)
	}
	if report.Verification.SignerTrusted {
		t.Fatalf("expected signer_trusted=false when trust policy is not configured: %+v", report.Verification)
	}
	if !report.Verification.PolicyValid {
		t.Fatalf("expected policy_valid=true when trust policy is not configured: %+v", report.Verification)
	}
	if report.Verification.OverallVerdict != verificationVerdictPassed {
		t.Fatalf("expected overall_verdict=%q, got %q", verificationVerdictPassed, report.Verification.OverallVerdict)
	}
	if report.Verification.OverallReason == "" {
		t.Fatalf("overall_reason should be populated for valid session: %q", report.Verification.OverallReason)
	}
	if !report.Verification.EventChainValid || !report.Verification.FinalPatchHashValid || !report.Verification.ManifestHashValid || !report.Verification.ReceiptHashValid || !report.Verification.SignatureValid {
		t.Fatalf("expected all verification components to be valid: %+v", report.Verification)
	}
	for _, name := range []string{"event_chain", "final_patch_hash", "manifest_hash", "receipt_hash", "signature"} {
		check, ok := verificationComponent(report.Verification.ComponentResults, name)
		if !ok {
			t.Fatalf("missing component result for %s", name)
		}
		if !check.Valid || check.Reason != "" {
			t.Fatalf("expected component result %s to be valid with empty reason, got: %#v", name, check)
		}
	}
	if report.Verification.SignatureError != "" {
		t.Fatalf("signature error should be empty for valid verification: %+v", report.Verification.SignatureError)
	}
	if report.Verification.SignatureErrorCode != "" {
		t.Fatalf("signature error code should be empty for valid verification: %+v", report.Verification.SignatureErrorCode)
	}
}

func TestBuildVerificationTrustPolicyMatchesSignedSigner(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	decoded, err := readReceiptForReplayTest(layout.ReceiptJSON)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	trustedSignerKeyIDs := []string{decoded.Verification.SignerKeyID}

	report, err := Build(context.Background(), Options{
		RepoPath:            repo,
		SessionID:           sessionID,
		TrustedSignerKeyIDs: trustedSignerKeyIDs,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !report.Verification.AuthenticityValid {
		t.Fatalf("expected authenticity_valid=true for trusted signer test: %+v", report.Verification)
	}
	if !report.Verification.SignerTrusted {
		t.Fatalf("expected signer_trusted=true for configured signer: %+v", report.Verification)
	}
	if report.Verification.TrustStatus != trustStatusTrusted {
		t.Fatalf("expected trust_status=%q, got %q", trustStatusTrusted, report.Verification.TrustStatus)
	}
	if !report.Verification.PolicyValid {
		t.Fatalf("expected policy_valid=true for configured valid policy: %+v", report.Verification)
	}
	if report.Verification.OverallVerdict != verificationVerdictPassed {
		t.Fatalf("expected overall_verdict=%q, got %q", verificationVerdictPassed, report.Verification.OverallVerdict)
	}
}

func TestBuildVerificationTrustPolicyRejectsUntrustedSigner(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	decoded, err := readReceiptForReplayTest(layout.ReceiptJSON)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if decoded.Verification.SignerKeyID == "" {
		t.Fatal("receipt signer_key_id should be present for trust policy test")
	}
	if decoded.Verification.SignerKeyID == "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatal("test fixture signer key id should not match untrusted test policy")
	}

	report, err := Build(context.Background(), Options{
		RepoPath:            repo,
		SessionID:           sessionID,
		TrustedSignerKeyIDs: []string{"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !report.Verification.AuthenticityValid {
		t.Fatalf("expected authenticity_valid=true despite untrusted policy: %+v", report.Verification)
	}
	if report.Verification.SignerTrusted {
		t.Fatalf("expected signer_trusted=false for untrusted signer: %+v", report.Verification)
	}
	if report.Verification.TrustStatus != trustStatusNotTrusted {
		t.Fatalf("expected trust_status=%q, got %q", trustStatusNotTrusted, report.Verification.TrustStatus)
	}
	if !report.Verification.PolicyValid {
		t.Fatalf("expected policy_valid=true for valid policy list: %+v", report.Verification)
	}
	if report.Verification.OverallVerdict != verificationVerdictUntrusted {
		t.Fatalf("expected overall_verdict=%q for untrusted signer, got %q", verificationVerdictUntrusted, report.Verification.OverallVerdict)
	}
	if report.Verification.OverallReason != "signer is not trusted" {
		t.Fatalf("expected overall_reason='signer is not trusted', got %q", report.Verification.OverallReason)
	}
}

func readReceiptForReplayTest(path string) (model.Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Receipt{}, err
	}
	var decoded model.Receipt
	if err := json.Unmarshal(data, &decoded); err != nil {
		return model.Receipt{}, err
	}

	return decoded, nil
}

func recomputeReceiptHashForTest(t *testing.T, receiptData model.Receipt) string {
	t.Helper()
	receiptData.Verification.ReceiptHash = ""
	receiptData.Verification.Signature = ""
	serialized, err := model.MarshalCanonical(receiptData)
	if err != nil {
		t.Fatalf("marshal canonical receipt: %v", err)
	}
	sum := sha256.Sum256(serialized)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestBuildVerificationReportsLegacyEmbeddedSignerError(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	decoded, err := readReceiptForReplayTest(layout.ReceiptJSON)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	decoded.Verification.SignerPublicKey = ""
	decoded.Verification.SignerKeyID = ""
	decoded.Verification.ReceiptHash = recomputeReceiptHashForTest(t, decoded)
	data, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	if err := os.WriteFile(layout.ReceiptJSON, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Verification.Valid {
		t.Fatalf("expected invalid verification for legacy signer missing embedded metadata: %+v", report.Verification)
	}
	if report.Verification.SignatureValid {
		t.Fatalf("signature should be invalid for legacy signer case: %+v", report.Verification)
	}
	if report.Verification.SignatureErrorCode != "legacy_missing_embedded_signer" {
		t.Fatalf("expected legacy_missing_embedded_signer code, got %q: %+v", report.Verification.SignatureErrorCode, report.Verification)
	}
	if !report.Verification.IntegrityValid {
		t.Fatalf("expected integrity_valid=true for missing embedded signer: %+v", report.Verification)
	}
	if report.Verification.AuthenticityStatus != authenticityStatusUnverifiable {
		t.Fatalf("expected authenticity_status=unverifiable for missing embedded signer, got %q", report.Verification.AuthenticityStatus)
	}
	if report.Verification.OverallVerdict != verificationVerdictIntegrityOnly {
		t.Fatalf("expected overall_verdict=%q for legacy signer: %q", verificationVerdictIntegrityOnly, report.Verification.OverallVerdict)
	}
	if check, ok := verificationComponent(report.Verification.ComponentResults, "signature"); !ok || check.Valid || check.Reason == "" {
		t.Fatalf("expected invalid signature component result with reason: %#v", check)
	}
	if !containsGap(report.Gaps, "Signature verification failed") {
		t.Fatalf("missing signature-gap warning: %+v", report.Gaps)
	}
}

func TestBuildVerificationReportsSignatureMismatchWithIntactHashes(t *testing.T) {
	t.Parallel()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	decoded, err := readReceiptForReplayTest(layout.ReceiptJSON)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	decoded.Verification.Signature = "bad_signature_mismatch"
	decoded.Verification.ReceiptHash = recomputeReceiptHashForTest(t, decoded)
	mutatedJSON, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	if err := os.WriteFile(layout.ReceiptJSON, append(mutatedJSON, '\n'), 0o600); err != nil {
		t.Fatalf("write tampered receipt: %v", err)
	}
	if err := os.WriteFile(layout.ReceiptSignature, []byte(decoded.Verification.Signature), 0o600); err != nil {
		t.Fatalf("tamper receipt signature file: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !report.Verification.IntegrityValid {
		t.Fatalf("expected integrity_valid=true for signature-only mismatch: %+v", report.Verification)
	}
	if report.Verification.SignatureValid {
		t.Fatalf("signature should be invalid when receipt.sig is tampered: %+v", report.Verification)
	}
	if report.Verification.OverallVerdict != verificationVerdictUntrusted {
		t.Fatalf("expected overall_verdict=%q for signature mismatch, got %q", verificationVerdictUntrusted, report.Verification.OverallVerdict)
	}
	if report.Verification.SignatureErrorCode != "signature_verification_error" {
		t.Fatalf("expected signature_verification_error, got %q: %+v", report.Verification.SignatureErrorCode, report.Verification)
	}
	if check, ok := verificationComponent(report.Verification.ComponentResults, "signature"); !ok || check.Valid || check.Reason == "" {
		t.Fatalf("expected invalid signature component result with reason: %#v", check)
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
	if len(bundledReport.EvidenceIndex) == 0 {
		t.Fatalf("bundled report has no evidence_index")
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
	return finalizedReplaySessionWithSetup(t, nil, beforeStop, events)
}

func finalizedReplaySessionWithSetup(t *testing.T, beforeStart func(string), beforeStop func(string), events []model.Event) (repo string, sessionID string, layout storage.Layout) {
	t.Helper()

	repo = newReplayGitRepo(t)
	if beforeStart != nil {
		beforeStart(repo)
	}
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

func containsEvidenceRef(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}

	return false
}

func verificationComponent(checks []VerificationCheck, name string) (VerificationCheck, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}

	return VerificationCheck{}, false
}

func containsEvidencePrefix(values []string, needle string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, needle) {
			return true
		}
	}

	return false
}

func replayNow() time.Time {
	return time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
}

func findFile(files []File, path string) *File {
	for index := range files {
		if files[index].Path == path {
			return &files[index]
		}
	}

	return nil
}

func containsReviewFocus(items []ReviewFocusItem, needle string) bool {
	for _, item := range items {
		if strings.Contains(item.Message, needle) {
			return true
		}
	}

	return false
}

func containsPolicyEvidenceRefs(checks []PolicyCheck) bool {
	for _, check := range checks {
		if len(check.EvidenceRefs) > 0 {
			return true
		}
	}

	return false
}

func findClaim(claims []Claim, name string) *Claim {
	for index := range claims {
		if claims[index].Name == name {
			return &claims[index]
		}
	}

	return nil
}

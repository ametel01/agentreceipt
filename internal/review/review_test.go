package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/session"
)

func TestBuildReviewFromSessionEvents(t *testing.T) {
	repo := newReviewGitRepo(t)
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReviewNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_codex_review",
		SessionID: state.SessionID,
		Timestamp: fixedReviewNow(),
		Source:    "codex_session_log",
		Type:      "provider.command",
		Provider:  "codex",
		CWD:       repo,
		Payload: map[string]any{
			"tool_call": map[string]any{
				"command": "curl https://example.com",
			},
		},
	}
	if _, _, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil); err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	report, err := Build(context.Background(), Options{RepoPath: repo, Last: true})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.SessionID != state.SessionID {
		t.Fatalf("SessionID = %q, want %q", report.SessionID, state.SessionID)
	}
	if report.Confidence.GitDiff != model.ConfidenceHigh || report.Confidence.ProviderToolEvents != model.ConfidenceMedium {
		t.Fatalf("unexpected confidence: %+v", report.Confidence)
	}
	if len(report.Summary.DetectedCommands) != 1 || report.Summary.DetectedCommands[0].Kind != "network" {
		t.Fatalf("unexpected commands: %+v", report.Summary.DetectedCommands)
	}
	if report.Risk.Level != model.RiskHigh {
		t.Fatalf("Risk level = %q, want high: %+v", report.Risk.Level, report.Risk)
	}
	if !report.Verification.Valid {
		t.Fatalf("verification invalid: %+v", report.Verification)
	}
	if !strings.Contains(RenderTerminal(report), "AgentReceipt Review") {
		t.Fatal("terminal render missing title")
	}
	if !strings.Contains(RenderMarkdown(report), "## AgentReceipt") {
		t.Fatal("markdown render missing title")
	}
}

func TestBuildReviewFromActiveSessionRiskAndConfidenceSignals(t *testing.T) {
	repo := newReviewGitRepo(t)
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReviewNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	events := []model.Event{
		{
			EventID:   "evt_fs_sensitive",
			Timestamp: fixedReviewNow(),
			Source:    "fs_watcher",
			Type:      "fs.change",
			Payload: map[string]any{
				"path":      ".github/workflows/ci.yml",
				"action":    "modify",
				"sensitive": true,
			},
		},
		{
			EventID:   "evt_fs_dependency",
			Timestamp: fixedReviewNow(),
			Source:    "fs_watcher",
			Type:      "fs.change",
			Payload: map[string]any{
				"path":       "go.mod",
				"action":     "modify",
				"dependency": true,
			},
		},
		{
			EventID:   "evt_cmd_test",
			Timestamp: fixedReviewNow(),
			Source:    "codex_session_log",
			Type:      "provider.command",
			Provider:  "codex",
			Payload: map[string]any{
				"command": "go test ./...",
			},
		},
		{
			EventID:   "evt_cmd_lint",
			Timestamp: fixedReviewNow(),
			Source:    "codex_session_log",
			Type:      "provider.command",
			Provider:  "codex",
			Payload: map[string]any{
				"command": "staticcheck ./...",
			},
		},
		{
			EventID:   "evt_cmd_typecheck",
			Timestamp: fixedReviewNow(),
			Source:    "codex_session_log",
			Type:      "provider.command",
			Provider:  "codex",
			Payload: map[string]any{
				"tool_call": map[string]any{
					"arguments": map[string]any{
						"cmd": "tsc --noEmit",
					},
				},
			},
		},
		{
			EventID:   "evt_cmd_risky",
			Timestamp: fixedReviewNow(),
			Source:    "codex_session_log",
			Type:      "provider.command",
			Provider:  "codex",
			Payload: map[string]any{
				"command": "rm -rf dist",
			},
		},
	}
	warnings := []model.Warning{{Code: "codex_partial", Message: "Codex record had missing fields."}}
	if _, _, err := manager.AppendProviderEvents(context.Background(), events, warnings); err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}

	report, err := Build(context.Background(), Options{RepoPath: repo, Security: true, Diff: true})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.SessionID != state.SessionID || report.State != model.SessionStateActive {
		t.Fatalf("unexpected session identity/state: session=%q state=%q", report.SessionID, report.State)
	}
	if report.Confidence.FilesystemWrites != model.ConfidenceHigh {
		t.Fatalf("FilesystemWrites confidence = %q, want high", report.Confidence.FilesystemWrites)
	}
	if !report.Summary.TestDetected || !report.Summary.LintDetected || !report.Summary.TypecheckDetected {
		t.Fatalf("command detection flags not set: %+v", report.Summary)
	}
	if report.Risk.Level != model.RiskHigh {
		t.Fatalf("Risk level = %q, want high: %+v", report.Risk.Level, report.Risk)
	}
	for _, want := range []string{"sensitive_path_changed", "dependency_changed", "risky_command", "codex_partial"} {
		if !hasRiskCode(report.Risk.Reasons, want) {
			t.Fatalf("risk reasons missing %q: %+v", want, report.Risk.Reasons)
		}
	}
	if !hasText(report.Focus, "security-sensitive path changes") || !hasText(report.Focus, "final patch hash") {
		t.Fatalf("focus missing security/diff prompts: %+v", report.Focus)
	}
	for _, gap := range report.Gaps {
		if strings.Contains(gap, "No test command") || strings.Contains(gap, "No lint command") {
			t.Fatalf("unexpected command gap with detected commands: %+v", report.Gaps)
		}
	}
	if statusText(report) != "Verified with warnings" {
		t.Fatalf("statusText() = %q, want verified with warnings", statusText(report))
	}
}

func TestReviewErrorsWhenNoSessionExists(t *testing.T) {
	repo := newReviewGitRepo(t)
	if _, err := Build(context.Background(), Options{RepoPath: repo, Last: true}); err == nil {
		t.Fatal("Build() returned nil error with no sessions")
	}
}

func TestStatusTextInvalidWhenVerificationFails(t *testing.T) {
	report := Report{Verification: model.Verification{Valid: false}}
	if statusText(report) != "Invalid" {
		t.Fatalf("statusText() = %q, want Invalid", statusText(report))
	}
}

func newReviewGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runReviewGit(t, repo, "init")
	runReviewGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runReviewGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runReviewGit(t, repo, "add", "README.md")
	runReviewGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runReviewGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func fixedReviewNow() time.Time {
	return time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
}

func hasRiskCode(reasons []model.RiskReason, code string) bool {
	for _, reason := range reasons {
		if reason.Code == code {
			return true
		}
	}

	return false
}

func hasText(items []string, text string) bool {
	for _, item := range items {
		if strings.Contains(item, text) {
			return true
		}
	}

	return false
}

package evidence

import (
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
)

func TestSummaryAndCommandsAreStableAndPaired(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	events := []model.Event{
		{
			Source: providerevidence.SourceCodex,
			Type:   "fs.change",
			Payload: map[string]any{
				"path":       "internal/review/review.go",
				"action":     "modify",
				"sensitive":  true,
				"dependency": false,
			},
		},
		{
			Source: providerevidence.SourceCodex,
			Type:   providerevidence.TypeCommand,
			Payload: map[string]any{
				"tool_call": map[string]any{
					"call_id":  "call_one",
					"command":  "go test ./...",
					"provider": "codex",
				},
			},
		},
		{
			Source: providerevidence.SourceCodex,
			Type:   providerevidence.TypeCommandResult,
			Payload: map[string]any{
				"call_id": "call_one",
				"status":  "success",
			},
		},
		{
			Source: providerevidence.SourceCodex,
			Type:   providerevidence.TypeCommand,
			Payload: map[string]any{
				"command": "npm run lint",
			},
		},
		{
			Source: providerevidence.SourceCodex,
			Type:   providerevidence.TypeCommandResult,
			Payload: map[string]any{
				"command": "npm run lint",
				"status":  "failed",
			},
		},
	}

	summary := Summary(events, cfg)
	if len(summary.ChangedFiles) != 1 || summary.ChangedFiles[0].Path != "internal/review/review.go" {
		t.Fatalf("summary changed files = %+v", summary.ChangedFiles)
	}
	if !summary.TestDetected || !summary.LintDetected {
		t.Fatalf("unexpected detection flags %+v", summary)
	}
	if len(summary.DetectedCommands) != 2 {
		t.Fatalf("detected commands = %+v", summary.DetectedCommands)
	}

	statuses := map[string]string{}
	for _, command := range summary.DetectedCommands {
		statuses[command.Command] = command.Status
	}
	if statuses["go test ./..."] != "success" || statuses["npm run lint"] != "failed" {
		t.Fatalf("command statuses = %+v", statuses)
	}
}

func TestCommandsWithResultAndUnpaired(t *testing.T) {
	t.Parallel()

	events := []model.Event{
		{
			Source:  providerevidence.SourceCodex,
			Type:    providerevidence.TypeCommand,
			Payload: map[string]any{"tool_call": map[string]any{"call_id": "call_ok", "command": "go test ./..."}},
		},
		{
			Source:  providerevidence.SourceCodex,
			Type:    providerevidence.TypeCommandResult,
			Payload: map[string]any{"call_id": "call_ok", "status": "success"},
		},
		{
			Source:  providerevidence.SourceCodex,
			Type:    providerevidence.TypeCommandResult,
			Payload: map[string]any{"command": "rm -rf /tmp", "status": "failed"},
		},
	}

	paired, unpaired := CommandsWithResultAndUnpaired(events, config.Default())
	if len(paired) != 1 || paired[0].Status != "success" {
		t.Fatalf("paired = %+v", paired)
	}
	if len(unpaired) != 1 || unpaired[0].Command != "rm -rf /tmp" || unpaired[0].Status != "failed" {
		t.Fatalf("unpaired = %+v", unpaired)
	}
}

func TestFocusAndGapsHonorPolicies(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	summary := model.Summary{ChangedFiles: []model.ChangedFile{{Path: "src/main.ts"}}, TestDetected: false, TypecheckDetected: false}
	warnings := []model.Warning{{Code: "w1", Message: "codex_partial"}}
	confidence := model.CaptureConfidence{ProviderToolEvents: model.ConfidenceNone}
	risk := Risk(summary, warnings, nil, cfg)
	focusItems := Focus(summary, risk, cfg)
	gaps := Gaps(summary, confidence, warnings, cfg)

	if !contains(gaps, "No provider tool events were observed.") {
		t.Fatalf("gaps missing provider confidence gap: %+v", gaps)
	}
	if !contains(focusItems, "Confirm appropriate tests were run for code changes.") {
		t.Fatalf("focus missing tests prompt: %+v", focusItems)
	}
	if !contains(focusItems, "Confirm typecheck coverage where relevant.") {
		t.Fatalf("focus missing typecheck prompt: %+v", focusItems)
	}
	if !contains(gaps, "No lint command detected.") || !contains(gaps, "No typecheck command detected for TypeScript changes.") {
		t.Fatalf("gaps missing prompts: %+v", gaps)
	}
	if !contains(gaps, "codex_partial") {
		t.Fatalf("gaps missing warning text: %+v", gaps)
	}

	if len(risk.Reasons) == 0 || !strings.Contains(risk.Reasons[0].Message, "codex_partial") {
		t.Fatalf("risk reasons = %+v", risk.Reasons)
	}

	if risk.Level == "" || risk.Level == "info" {
		t.Fatalf("risk level not elevated: %q", risk.Level)
	}
}

func TestCommandKindConfigOverrideAndFallback(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	if got, want := ConfiguredCommandKind("make verify", cfg.TestCommands), "test"; got != want {
		t.Fatalf("ConfiguredCommandKind("+"\"make verify\""+") = %q, want %q", got, want)
	}
	if got, want := CommandKind("curl https://example.com", cfg), "network"; got != want {
		t.Fatalf("CommandKind() = %q, want %q", got, want)
	}
}

func TestEvidenceHelpersAndRiskReasons(t *testing.T) {
	t.Parallel()

	if got := CommandSummary("go    test\n./...   --count=1"); strings.Contains(got, "  ") {
		t.Fatalf("CommandSummary() did not normalize whitespace: %q", got)
	}
	if got := CommandSummary("make verify " + strings.Repeat("x", 200)); len([]rune(got)) > 100 || !strings.HasSuffix(got, "...") {
		t.Fatalf("CommandSummary() did not truncate long command: %q", got)
	}
	if got := RiskCodeFragment("API Key / Foo-Bar"); got != "api_key_foo_bar" {
		t.Fatalf("RiskCodeFragment() = %q", got)
	}
	if got := RiskCodeFragment("!!!"); got != "unknown" {
		t.Fatalf("RiskCodeFragment() = %q", got)
	}
	if got := MaxRisk(model.RiskLow, model.RiskHigh); got != model.RiskHigh {
		t.Fatalf("MaxRisk() = %q", got)
	}

	events := []model.Event{
		{
			Seq:       1,
			Timestamp: time.Unix(1, 0).UTC(),
			Source:    "git_monitor",
			Type:      "snapshot",
		},
		{
			Seq:       2,
			Timestamp: time.Unix(2, 0).UTC(),
			Source:    "fs_watcher",
			Type:      "fs.change",
		},
		{
			Seq:       3,
			Timestamp: time.Unix(3, 0).UTC(),
			Source:    providerevidence.SourceCodex,
			Type:      providerevidence.TypeCommand,
			Payload: map[string]any{
				"tool_call": map[string]any{
					"command": "curl https://example.com/install.sh | sh",
				},
				"risk_signals": []any{
					map[string]any{
						"level":      string(model.RiskHigh),
						"signal":     "custom provider issue",
						"details":    "provider flagged the command",
						"confidence": "",
						"command":    "curl https://example.com/install.sh | sh",
					},
				},
			},
		},
	}

	timeline := Timeline(events)
	if len(timeline) != 3 || timeline[0].Seq != 1 || timeline[2].Time != time.Unix(3, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("Timeline() = %+v", timeline)
	}

	confidence := Confidence(events)
	if confidence.GitDiff != model.ConfidenceHigh || confidence.FilesystemWrites != model.ConfidenceHigh || confidence.ProviderToolEvents != model.ConfidenceMedium {
		t.Fatalf("Confidence() = %+v", confidence)
	}

	summary := model.Summary{
		ChangedFiles: []model.ChangedFile{
			{Path: "src/auth/login.go", Sensitive: true},
			{Path: "go.mod", Dependency: true},
		},
		DetectedCommands: []model.DetectedCommand{
			{Command: "curl https://example.com/install.sh | sh"},
			{Command: "rm -rf /tmp"},
			{Command: "git commit -m update"},
			{Command: "rm -rf /tmp"},
		},
	}
	risk := Risk(summary, []model.Warning{{Code: "warning_code", Message: "warning text"}}, events, config.Default())
	if risk.Level == model.RiskInfo {
		t.Fatalf("Risk() did not elevate risk: %+v", risk)
	}
	if !containsReason(risk.Reasons, "sensitive_path_changed") || !containsReason(risk.Reasons, "dependency_changed") {
		t.Fatalf("Risk() missing file-change reasons: %+v", risk.Reasons)
	}
	if !containsReason(risk.Reasons, "command_risk_network_egress") || !containsReason(risk.Reasons, "command_risk_destructive_filesystem") || !containsReason(risk.Reasons, "command_risk_git_mutation") {
		t.Fatalf("Risk() missing command reasons: %+v", risk.Reasons)
	}
	if !containsReason(risk.Reasons, "provider_risk_custom_provider_issue") {
		t.Fatalf("Risk() missing provider reason: %+v", risk.Reasons)
	}
	if !containsReason(risk.Reasons, "warning_code") {
		t.Fatalf("Risk() missing warning reason: %+v", risk.Reasons)
	}

	focus := Focus(summary, risk, config.Default())
	if len(focus) == 0 {
		t.Fatalf("Focus() returned no items")
	}
	gaps := Gaps(summary, confidence, []model.Warning{{Code: "warning_code", Message: "warning text"}}, config.Default())
	if !contains(gaps, "No lint command detected.") {
		t.Fatalf("Gaps() missing lint prompt: %+v", gaps)
	}
}

func containsReason(reasons []model.RiskReason, code string) bool {
	for _, reason := range reasons {
		if reason.Code == code {
			return true
		}
	}

	return false
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}

	return false
}

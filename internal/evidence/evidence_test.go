package evidence

import (
	"strings"
	"testing"

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

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}

	return false
}

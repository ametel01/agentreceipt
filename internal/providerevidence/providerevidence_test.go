package providerevidence

import (
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

func TestCommandEvidenceRoundTrip(t *testing.T) {
	t.Parallel()

	meta := EventMeta{
		EventID:   "evt_provider_1",
		SessionID: "ar_ses_test",
		Timestamp: time.Date(2026, 6, 18, 4, 0, 0, 0, time.UTC),
		Source:    SourceCodex,
		Provider:  ProviderCodex,
		CWD:       "/repo",
	}
	commandEvent := NewCommandEvent(meta, ToolCall{
		CallID:  "call_1",
		Tool:    "exec_command",
		Command: "curl https://example.com",
		Arguments: map[string]any{
			"cmd": "curl https://example.com",
		},
	}, []RiskSignal{{
		Level:      model.RiskHigh,
		Signal:     "network_egress",
		Command:    "curl https://example.com",
		Details:    "command can send data to an external host",
		Confidence: model.ConfidenceHigh,
	}}, map[string]any{"line_no": 7})

	attempt, ok := CommandAttemptFromEvent(commandEvent)
	if !ok {
		t.Fatalf("CommandAttemptFromEvent() ok=false for %+v", commandEvent)
	}
	if attempt.Command != "curl https://example.com" || attempt.CallID != "call_1" || attempt.Source != SourceCodex || attempt.Provider != ProviderCodex {
		t.Fatalf("attempt = %+v", attempt)
	}
	signals := RiskSignalsFromEvent(commandEvent)
	if len(signals) != 1 || signals[0].Signal != "network_egress" || signals[0].Confidence != model.ConfidenceHigh {
		t.Fatalf("signals = %+v", signals)
	}

	resultEvent := NewCommandResultEvent(meta, CommandResult{
		CallID: "call_1",
		Status: "failed",
	}, map[string]any{"line_no": 8})
	result, ok := CommandResultFromEvent(resultEvent)
	if !ok {
		t.Fatalf("CommandResultFromEvent() ok=false for %+v", resultEvent)
	}
	if result.CallID != "call_1" || result.Status != "failed" {
		t.Fatalf("result = %+v", result)
	}
}

func TestProviderLabelAndTokenTotal(t *testing.T) {
	t.Parallel()

	events := []model.Event{
		{
			Source:   SourceCodex,
			Type:     TypeEvent,
			Provider: ProviderCodex,
			Payload: map[string]any{
				"payload_type": "token_count",
				"token_usage":  map[string]any{"total_tokens": 42},
			},
		},
		{
			Source:   SourceClaude,
			Type:     TypeCommand,
			Provider: ProviderClaude,
			Payload:  map[string]any{"tool_call": map[string]any{"command": "go test ./..."}},
		},
	}

	if label := ProviderLabel(events); label != "Codex CLI + Claude Code" {
		t.Fatalf("ProviderLabel() = %q", label)
	}
	total, ok := TokenTotal(events[0])
	if !ok || total != 42 {
		t.Fatalf("TokenTotal() = %d, %v", total, ok)
	}
	if _, ok := TokenTotal(events[1]); ok {
		t.Fatal("TokenTotal() ok=true for non-token event")
	}
}

func TestConstructorsAndReadersHandleFallbackShapes(t *testing.T) {
	t.Parallel()

	meta := EventMeta{EventID: "evt_provider_2", SessionID: "ar_ses_test", Source: SourceClaude, Provider: ProviderClaude}
	toolEvent := NewToolEvent(meta, ToolCall{CallID: "call_tool", Tool: "Edit"}, map[string]any{"category": "file_edit"})
	if !IsToolEvidenceEvent(toolEvent) || !IsProviderEvidenceSource(toolEvent) {
		t.Fatalf("tool event was not recognized as Provider Evidence: %+v", toolEvent)
	}
	if _, ok := CommandAttemptFromEvent(toolEvent); ok {
		t.Fatalf("tool event produced a command attempt: %+v", toolEvent)
	}
	warningEvent := NewParseWarningEvent(meta, 3, "malformed_json", "bad line")
	if IsToolEvidenceEvent(warningEvent) {
		t.Fatalf("parse warning was treated as tool evidence: %+v", warningEvent)
	}
	providerEvent := NewProviderEvent(meta, map[string]any{"payload_type": "opaque"})
	if providerEvent.Type != TypeEvent || providerEvent.Payload["payload_type"] != "opaque" {
		t.Fatalf("provider event = %+v", providerEvent)
	}

	legacyAttempt := model.Event{
		Source: SourceCodex,
		Type:   TypeCommand,
		Payload: map[string]any{
			"tool_call": map[string]any{
				"call_id": "call_legacy",
				"arguments": map[string]any{
					"command": "go test ./...",
				},
			},
		},
	}
	attempt, ok := CommandAttemptFromEvent(legacyAttempt)
	if !ok || attempt.Command != "go test ./..." || attempt.CallID != "call_legacy" || attempt.Provider != ProviderCodex {
		t.Fatalf("legacy attempt = %+v ok=%v", attempt, ok)
	}

	legacyResult := model.Event{Type: TypeCommandResult, Payload: map[string]any{"call_id": "call_legacy", "status": "success"}}
	result, ok := CommandResultFromEvent(legacyResult)
	if !ok || result.CallID != "call_legacy" || result.Status != "success" {
		t.Fatalf("legacy result = %+v ok=%v", result, ok)
	}
	if _, ok := CommandResultFromEvent(model.Event{Type: TypeCommandResult, Payload: map[string]any{"status": "ok"}}); ok {
		t.Fatal("invalid command status produced a command result")
	}
}

func TestRiskSignalsAndTokenFallbacks(t *testing.T) {
	t.Parallel()

	event := model.Event{
		Source: SourceCodex,
		Type:   TypeCommand,
		Payload: map[string]any{
			"tool_call": map[string]any{"command": "cat .env"},
			"risk_signals": []any{
				map[string]any{
					"level":      "medium",
					"signal":     "secret_access",
					"details":    "credential material read",
					"confidence": "high",
					"line_no":    float64(12),
				},
				map[string]any{"level": "info", "signal": "ignored"},
				"not a signal",
			},
		},
	}
	signals := RiskSignalsFromEvent(event)
	if len(signals) != 1 || signals[0].Command != "cat .env" || signals[0].LineNumber != 12 || signals[0].Level != model.RiskMedium {
		t.Fatalf("signals = %+v", signals)
	}

	rawTokenEvent := model.Event{
		Source: SourceCodex,
		Type:   TypeEvent,
		Payload: map[string]any{
			"payload_type": "token_count",
			"raw": map[string]any{
				"payload": map[string]any{
					"info": map[string]any{
						"last_token_usage": map[string]any{"total_tokens": float64(88)},
					},
				},
			},
		},
	}
	if total, ok := TokenTotal(rawTokenEvent); !ok || total != 88 {
		t.Fatalf("raw TokenTotal() = %d, %v", total, ok)
	}

	for _, status := range []string{"success", "failed", "unknown"} {
		if got := NormalizeCommandStatus(status); got != status {
			t.Fatalf("NormalizeCommandStatus(%q) = %q", status, got)
		}
	}
	if got := NormalizeCommandStatus("ok"); got != "" {
		t.Fatalf("NormalizeCommandStatus(ok) = %q", got)
	}
}

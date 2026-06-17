package claude

import (
	"strings"
	"testing"
)

func TestNormalizeCommandAttempt(t *testing.T) {
	t.Parallel()

	result := NormalizeJSON([]byte(`{"type":"pre_tool_use","timestamp":"2026-06-17T00:00:00Z","payload":{"tool":"Bash","call_id":"call_1","arguments":{"cmd":"go test ./...","token":"sk-testsecret"}}}`), ParseOptions{SessionID: "ar_ses_test", CWD: "/repo"})
	if result.EventCount != 1 || result.CommandCount != 1 || result.WarningCount != 0 {
		t.Fatalf("result counts = %+v", result)
	}
	event := result.Events[0]
	if event.Type != "provider.command" || event.Provider != "claude" || event.Source != Source {
		t.Fatalf("event identity = %+v", event)
	}
	toolCall := event.Payload["tool_call"].(map[string]any)
	if toolCall["command"] != "go test ./..." || toolCall["call_id"] != "call_1" || toolCall["tool"] != "Bash" {
		t.Fatalf("tool_call = %+v", toolCall)
	}
	arguments := toolCall["arguments"].(map[string]any)
	if arguments["token"] != "[REDACTED]" {
		t.Fatalf("arguments were not redacted: %+v", arguments)
	}
}

func TestNormalizeCommandResultOmitsRawOutputByDefault(t *testing.T) {
	t.Parallel()

	result := NormalizeJSON([]byte(`{"type":"post_tool_use","payload":{"tool":"Bash","call_id":"call_1","exit_code":7,"stdout":"very secret output","failed_reason":"command failed"}}`), ParseOptions{SessionID: "ar_ses_test"})
	if result.EventCount != 1 || result.CommandCount != 1 {
		t.Fatalf("result counts = %+v", result)
	}
	commandResult := result.Events[0].Payload["command_result"].(map[string]any)
	if commandResult["status"] != "failed" || commandResult["exit_code"] != 7 {
		t.Fatalf("command_result status = %+v", commandResult)
	}
	if _, ok := commandResult["stdout"]; ok {
		t.Fatalf("stdout retained by default: %+v", commandResult)
	}
	if commandResult["failed_reason"] != "command failed" {
		t.Fatalf("failed_reason = %+v", commandResult)
	}
}

func TestNormalizeOpaqueEventOmitsPromptTextByDefault(t *testing.T) {
	t.Parallel()

	result := NormalizeJSON([]byte(`{"type":"user_prompt","payload":{"content":"please change auth","tool":"Prompt"}}`), ParseOptions{SessionID: "ar_ses_test"})
	if result.EventCount != 1 {
		t.Fatalf("result counts = %+v", result)
	}
	event := result.Events[0]
	if event.Type != "provider.event" || event.Payload["category"] != "opaque" {
		t.Fatalf("event = %+v", event)
	}
	rendered := event.Payload["tool_call"].(map[string]any)
	if strings.Contains(rendered["tool"].(string), "please change auth") {
		t.Fatalf("prompt text leaked into tool_call: %+v", rendered)
	}
	if _, ok := event.Payload["content"]; ok {
		t.Fatalf("prompt content retained: %+v", event.Payload)
	}
}

func TestNormalizeMalformedJSONWarnsWithoutEvents(t *testing.T) {
	t.Parallel()

	result := NormalizeJSON([]byte(`{malformed`), ParseOptions{SessionID: "ar_ses_test"})
	if result.EventCount != 0 || result.WarningCount != 1 {
		t.Fatalf("result counts = %+v", result)
	}
	if result.Warnings[0].Code != "claude_malformed_json" {
		t.Fatalf("warning = %+v", result.Warnings[0])
	}
}

func TestNormalizeStoresRedactedTruncatedOutputWhenOptedIn(t *testing.T) {
	t.Parallel()

	result := NormalizeJSON([]byte(`{"type":"tool_result","payload":{"call_id":"call_1","exit_code":0,"stdout":"bearer abcdefghijklmnopqrstuvwxyz"}}`), ParseOptions{
		SessionID:           "ar_ses_test",
		MaxOutputBytes:      16,
		StoreRawToolOutputs: true,
	})
	commandResult := result.Events[0].Payload["command_result"].(map[string]any)
	stdout := commandResult["stdout"].(string)
	if strings.Contains(stdout, "abcdefghijklmnopqrstuvwxyz") || len(stdout) > 16 {
		t.Fatalf("stdout was not redacted/truncated: %q", stdout)
	}
	if commandResult["stdout_truncated"] != true {
		t.Fatalf("stdout_truncated = %+v", commandResult)
	}
}

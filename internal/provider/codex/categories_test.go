package codex

import (
	"strings"
	"testing"
)

func TestCategorizeRecordMapsStableCodexCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        map[string]any
		category   LogCategory
		family     LogFamily
		renderable bool
		auditOnly  bool
		tool       string
	}{
		{
			name:       "session meta",
			raw:        map[string]any{"type": "session_meta", "cwd": "/repo"},
			category:   CategorySessionMeta,
			family:     LogFamilyContext,
			auditOnly:  true,
			renderable: false,
		},
		{
			name:       "turn context",
			raw:        map[string]any{"type": "turn_context", "cwd": "/repo"},
			category:   CategoryTurnContext,
			family:     LogFamilyContext,
			auditOnly:  true,
			renderable: false,
		},
		{
			name:       "task started",
			raw:        map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "turn_1"}},
			category:   CategoryTaskStarted,
			family:     LogFamilyConversation,
			renderable: true,
		},
		{
			name:       "user message",
			raw:        map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "run tests"}},
			category:   CategoryUserMessage,
			family:     LogFamilyConversation,
			renderable: true,
		},
		{
			name:       "agent message",
			raw:        map[string]any{"type": "event_msg", "payload": map[string]any{"type": "agent_message", "phase": "commentary", "message": "running tests"}},
			category:   CategoryAgentMessage,
			family:     LogFamilyConversation,
			renderable: true,
		},
		{
			name:       "exec command call",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "exec_command", "call_id": "call_1"}},
			category:   CategoryExecCommandCall,
			family:     LogFamilyTool,
			renderable: true,
			tool:       "exec_command",
		},
		{
			name:       "non-shell function call",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "update_plan", "call_id": "call_2"}},
			category:   CategoryFunctionCall,
			family:     LogFamilyTool,
			renderable: true,
			tool:       "update_plan",
		},
		{
			name:       "function output",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "ok"}},
			category:   CategoryFunctionCallOutput,
			family:     LogFamilyTool,
			renderable: true,
		},
		{
			name:       "apply patch call",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "call_id": "call_3"}},
			category:   CategoryApplyPatchCall,
			family:     LogFamilyTool,
			renderable: true,
			tool:       "apply_patch",
		},
		{
			name:       "custom tool output",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call_output", "call_id": "call_3", "output": "ok"}},
			category:   CategoryCustomToolCallOutput,
			family:     LogFamilyTool,
			renderable: true,
		},
		{
			name:       "patch apply end",
			raw:        map[string]any{"type": "event_msg", "payload": map[string]any{"type": "patch_apply_end", "call_id": "call_3", "success": true}},
			category:   CategoryPatchApplyEnd,
			family:     LogFamilyTool,
			renderable: true,
		},
		{
			name:       "token count",
			raw:        map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count"}},
			category:   CategoryTokenCount,
			family:     LogFamilyTelemetry,
			auditOnly:  true,
			renderable: false,
		},
		{
			name:       "reasoning",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "reasoning", "encrypted_content": "secret"}},
			category:   CategoryReasoning,
			family:     LogFamilyTelemetry,
			auditOnly:  true,
			renderable: false,
		},
		{
			name:       "unknown",
			raw:        map[string]any{"type": "response_item", "payload": map[string]any{"type": "future_event"}},
			category:   CategoryUnknown,
			family:     LogFamilyUnknown,
			renderable: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			info := CategorizeRecord(test.raw)
			if info.Category != test.category || info.Family != test.family || info.Renderable != test.renderable || info.AuditOnly != test.auditOnly || info.Tool != test.tool {
				t.Fatalf("CategorizeRecord() = %+v", info)
			}
		})
	}
}

func TestParseJSONLStoresTimelineCategory(t *testing.T) {
	t.Parallel()

	result := ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"go test ./..."}}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":3,"reasoning_output_tokens":1,"total_tokens":13}}}}`,
	}, "\n")), ParseOptions{SessionID: "ar_ses_test"})
	if len(result.Timeline) != 2 {
		t.Fatalf("Timeline length = %d, want 2", len(result.Timeline))
	}
	got := result.Timeline[0]
	if got.Category != CategoryExecCommandCall || got.Family != LogFamilyTool {
		t.Fatalf("timeline category = %q family = %q", got.Category, got.Family)
	}
	if result.Timeline[1].Category != CategoryTokenCount || result.Timeline[1].Family != LogFamilyTelemetry {
		t.Fatalf("token timeline category = %q family = %q", result.Timeline[1].Category, result.Timeline[1].Family)
	}
	if len(result.TokenUsages) != 1 {
		t.Fatalf("TokenUsages length = %d, want 1", len(result.TokenUsages))
	}
	usage := result.TokenUsages[0]
	if usage.InputTokens != 10 || usage.CachedInputTokens != 2 || usage.OutputTokens != 3 || usage.ReasoningOutputTokens != 1 || usage.TotalTokens != 13 {
		t.Fatalf("TokenUsage = %+v", usage)
	}
}

func TestKnownLogCategoriesIncludesUnknown(t *testing.T) {
	t.Parallel()

	var foundUnknown bool
	for _, category := range KnownLogCategories() {
		if category == CategoryUnknown {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatal("KnownLogCategories() did not include CategoryUnknown")
	}
}

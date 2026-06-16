package codex

type LogFamily string

const (
	LogFamilyConversation LogFamily = "conversation"
	LogFamilyTool         LogFamily = "tool"
	LogFamilyTelemetry    LogFamily = "telemetry"
	LogFamilyContext      LogFamily = "context"
	LogFamilyUnknown      LogFamily = "unknown"
)

type LogCategory string

const (
	CategorySessionMeta          LogCategory = "session_meta"
	CategoryTurnContext          LogCategory = "turn_context"
	CategoryCompacted            LogCategory = "compacted"
	CategoryContextCompacted     LogCategory = "context_compacted"
	CategoryTaskStarted          LogCategory = "task_started"
	CategoryTaskComplete         LogCategory = "task_complete"
	CategoryTurnAborted          LogCategory = "turn_aborted"
	CategoryUserMessage          LogCategory = "user_message"
	CategoryAgentMessage         LogCategory = "agent_message"
	CategoryModelMessage         LogCategory = "model_message"
	CategoryReasoning            LogCategory = "reasoning"
	CategoryTokenCount           LogCategory = "token_count"
	CategoryExecCommandCall      LogCategory = "exec_command_call"
	CategoryFunctionCall         LogCategory = "function_call"
	CategoryFunctionCallOutput   LogCategory = "function_call_output"
	CategoryApplyPatchCall       LogCategory = "apply_patch_call"
	CategoryCustomToolCall       LogCategory = "custom_tool_call"
	CategoryCustomToolCallOutput LogCategory = "custom_tool_call_output"
	CategoryPatchApplyEnd        LogCategory = "patch_apply_end"
	CategoryUnknown              LogCategory = "unknown"
)

type CategoryInfo struct {
	Category    LogCategory `json:"category"`
	Family      LogFamily   `json:"family"`
	TopLevel    string      `json:"top_level,omitempty"`
	PayloadType string      `json:"payload_type,omitempty"`
	Tool        string      `json:"tool,omitempty"`
	Renderable  bool        `json:"renderable"`
	AuditOnly   bool        `json:"audit_only"`
}

func CategorizeRecord(raw map[string]any) CategoryInfo {
	topLevel := stringField(raw, "type")
	payload := mapField(raw, "payload")
	if payload == nil {
		payload = raw
	}
	payloadType := stringField(payload, "type")
	tool := firstString(payload, "name", "tool")
	category := categoryFor(topLevel, payloadType, tool)
	info := CategoryInfo{
		Category:    category,
		Family:      familyFor(category),
		TopLevel:    topLevel,
		PayloadType: payloadType,
		Tool:        tool,
	}
	info.Renderable = isRenderable(category)
	info.AuditOnly = isAuditOnly(category)

	return info
}

func KnownLogCategories() []LogCategory {
	return []LogCategory{
		CategorySessionMeta,
		CategoryTurnContext,
		CategoryCompacted,
		CategoryContextCompacted,
		CategoryTaskStarted,
		CategoryTaskComplete,
		CategoryTurnAborted,
		CategoryUserMessage,
		CategoryAgentMessage,
		CategoryModelMessage,
		CategoryReasoning,
		CategoryTokenCount,
		CategoryExecCommandCall,
		CategoryFunctionCall,
		CategoryFunctionCallOutput,
		CategoryApplyPatchCall,
		CategoryCustomToolCall,
		CategoryCustomToolCallOutput,
		CategoryPatchApplyEnd,
		CategoryUnknown,
	}
}

func categoryFor(topLevel string, payloadType string, tool string) LogCategory {
	switch topLevel {
	case "session_meta":
		return CategorySessionMeta
	case "turn_context":
		return CategoryTurnContext
	case "compacted":
		return CategoryCompacted
	}
	switch payloadType {
	case "context_compacted":
		return CategoryContextCompacted
	case "task_started":
		return CategoryTaskStarted
	case "task_complete":
		return CategoryTaskComplete
	case "turn_aborted":
		return CategoryTurnAborted
	case "user_message":
		return CategoryUserMessage
	case "agent_message":
		return CategoryAgentMessage
	case "message":
		return CategoryModelMessage
	case "reasoning":
		return CategoryReasoning
	case "token_count":
		return CategoryTokenCount
	case "function_call":
		if tool == "exec_command" {
			return CategoryExecCommandCall
		}

		return CategoryFunctionCall
	case "function_call_output":
		return CategoryFunctionCallOutput
	case "custom_tool_call":
		if tool == "apply_patch" {
			return CategoryApplyPatchCall
		}

		return CategoryCustomToolCall
	case "custom_tool_call_output":
		return CategoryCustomToolCallOutput
	case "patch_apply_end":
		return CategoryPatchApplyEnd
	default:
		return CategoryUnknown
	}
}

func familyFor(category LogCategory) LogFamily {
	switch category {
	case CategoryTaskStarted, CategoryTaskComplete, CategoryTurnAborted, CategoryUserMessage, CategoryAgentMessage:
		return LogFamilyConversation
	case CategoryExecCommandCall, CategoryFunctionCall, CategoryFunctionCallOutput, CategoryApplyPatchCall, CategoryCustomToolCall, CategoryCustomToolCallOutput, CategoryPatchApplyEnd:
		return LogFamilyTool
	case CategoryTokenCount, CategoryReasoning:
		return LogFamilyTelemetry
	case CategorySessionMeta, CategoryTurnContext, CategoryCompacted, CategoryContextCompacted, CategoryModelMessage:
		return LogFamilyContext
	default:
		return LogFamilyUnknown
	}
}

func isRenderable(category LogCategory) bool {
	switch category {
	case CategoryTaskStarted, CategoryTaskComplete, CategoryTurnAborted, CategoryUserMessage, CategoryAgentMessage,
		CategoryExecCommandCall, CategoryFunctionCall, CategoryFunctionCallOutput, CategoryApplyPatchCall, CategoryCustomToolCall, CategoryCustomToolCallOutput, CategoryPatchApplyEnd:
		return true
	default:
		return false
	}
}

func isAuditOnly(category LogCategory) bool {
	switch category {
	case CategorySessionMeta, CategoryTurnContext, CategoryCompacted, CategoryContextCompacted, CategoryModelMessage, CategoryReasoning, CategoryTokenCount:
		return true
	default:
		return false
	}
}

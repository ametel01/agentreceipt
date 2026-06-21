package providerevidence

import (
	"encoding/json"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

const (
	SourceCodex  = "codex_session_log"
	SourceClaude = "claude_hook"

	ProviderCodex  = "codex"
	ProviderClaude = "claude"

	TypeCommand       = "provider.command"
	TypeCommandResult = "provider.command_result"
	TypeEvent         = "provider.event"
	TypeParseWarning  = "provider.parse_warning"
)

type EventMeta struct {
	EventID   string
	SessionID string
	Timestamp time.Time
	Source    string
	Provider  string
	CWD       string
}

type ToolCall struct {
	SessionID  string         `json:"session_id,omitempty"`
	LineNumber int            `json:"line_no,omitempty"`
	Time       string         `json:"ts,omitempty"`
	TurnID     string         `json:"turn_id,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	ToolType   string         `json:"tool_type,omitempty"`
	CallID     string         `json:"call_id,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Command    string         `json:"command,omitempty"`
	Source     string         `json:"source,omitempty"`
}

type CommandAttempt struct {
	Command  string
	CallID   string
	Source   string
	Provider string
}

type CommandResult struct {
	SessionID       string `json:"session_id,omitempty"`
	LineNumber      int    `json:"line_no,omitempty"`
	CallID          string `json:"call_id,omitempty"`
	TurnID          string `json:"turn_id,omitempty"`
	Tool            string `json:"tool,omitempty"`
	Time            string `json:"ts,omitempty"`
	Command         string `json:"command,omitempty"`
	Status          string `json:"status"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	Stdout          string `json:"stdout,omitempty"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrOrError   string `json:"stderr_or_error,omitempty"`
	FailedReason    string `json:"failed_reason,omitempty"`
	Source          string `json:"source,omitempty"`
}

type RiskSignal struct {
	SessionID  string           `json:"session_id,omitempty"`
	Level      model.RiskLevel  `json:"level"`
	Signal     string           `json:"signal"`
	Category   string           `json:"category,omitempty"`
	Command    string           `json:"command,omitempty"`
	Details    string           `json:"details"`
	LineNumber int              `json:"line_no,omitempty"`
	Confidence model.Confidence `json:"confidence"`
}

func NewCommandEvent(meta EventMeta, toolCall ToolCall, risks []RiskSignal, fields map[string]any) model.Event {
	payload := clonePayload(fields)
	payload["tool_call"] = payloadMap(toolCall)
	if len(risks) > 0 {
		payload["risk_signals"] = risks
	}

	return newEvent(meta, TypeCommand, payload)
}

func NewToolEvent(meta EventMeta, toolCall ToolCall, fields map[string]any) model.Event {
	payload := clonePayload(fields)
	payload["tool_call"] = payloadMap(toolCall)

	return newEvent(meta, TypeEvent, payload)
}

func NewCommandResultEvent(meta EventMeta, result CommandResult, fields map[string]any) model.Event {
	payload := clonePayload(fields)
	payload["command_result"] = payloadMap(result)

	return newEvent(meta, TypeCommandResult, payload)
}

func NewProviderEvent(meta EventMeta, fields map[string]any) model.Event {
	return newEvent(meta, TypeEvent, clonePayload(fields))
}

func NewParseWarningEvent(meta EventMeta, lineNumber int, code string, message string) model.Event {
	return newEvent(meta, TypeParseWarning, map[string]any{
		"line_no": lineNumber,
		"code":    code,
		"message": message,
	})
}

func IsProviderEvidenceSource(event model.Event) bool {
	return knownProvider(event.Provider) || event.Source == SourceCodex || event.Source == SourceClaude
}

func IsToolEvidenceEvent(event model.Event) bool {
	switch event.Type {
	case TypeCommand, TypeCommandResult, TypeEvent:
		return true
	default:
		return false
	}
}

func ProviderLabel(events []model.Event) string {
	providers := map[string]bool{}
	for _, event := range events {
		if !IsToolEvidenceEvent(event) {
			continue
		}
		switch {
		case knownProvider(event.Provider):
			providers[event.Provider] = true
		case event.Source == SourceCodex:
			providers[ProviderCodex] = true
		case event.Source == SourceClaude:
			providers[ProviderClaude] = true
		}
	}
	switch {
	case providers[ProviderCodex] && providers[ProviderClaude]:
		return "Codex CLI + Claude Code"
	case providers[ProviderClaude]:
		return "Claude Code"
	case providers[ProviderCodex]:
		return "Codex CLI"
	default:
		return "unknown"
	}
}

func CommandAttemptFromEvent(event model.Event) (CommandAttempt, bool) {
	if event.Type != TypeCommand {
		return CommandAttempt{}, false
	}
	command := commandFromPayload(event.Payload)
	if command == "" {
		return CommandAttempt{}, false
	}

	return CommandAttempt{
		Command:  command,
		CallID:   callIDFromPayload(event.Payload),
		Source:   event.Source,
		Provider: providerFromEvent(event),
	}, true
}

func CommandResultFromEvent(event model.Event) (CommandResult, bool) {
	if event.Type != TypeCommandResult {
		return CommandResult{}, false
	}
	payload := mapPayload(event.Payload, "command_result")
	if payload == nil {
		payload = event.Payload
	}
	status := NormalizeCommandStatus(stringPayload(payload, "status"))
	if status == "" {
		return CommandResult{}, false
	}
	result := CommandResult{
		CallID:          firstString(payload, "call_id"),
		Command:         stringPayload(payload, "command"),
		Status:          status,
		Source:          event.Source,
		Stdout:          stringPayload(payload, "stdout"),
		StdoutTruncated: boolPayload(payload, "stdout_truncated"),
		FailedReason:    firstString(payload, "failed_reason", "reason", "error"),
		StderrOrError:   firstString(payload, "stderr_or_error", "stderr"),
	}
	if result.CallID == "" {
		result.CallID = callIDFromPayload(event.Payload)
	}
	if exitCode, ok := intPayload(payload, "exit_code"); ok {
		result.ExitCode = &exitCode
	}

	return result, true
}

func RiskSignalsFromEvent(event model.Event) []RiskSignal {
	if event.Type != TypeCommand {
		return nil
	}
	fallbackCommand := commandFromPayload(event.Payload)
	rawSignals := arrayPayload(event.Payload, "risk_signals")
	signals := make([]RiskSignal, 0, len(rawSignals))
	for _, raw := range rawSignals {
		signal := riskSignalFromRaw(raw, fallbackCommand)
		if riskRank(signal.Level) == 0 {
			continue
		}
		if signal.Confidence == "" {
			signal.Confidence = model.ConfidenceMedium
		}
		signals = append(signals, signal)
	}

	return signals
}

func TokenTotal(event model.Event) (int, bool) {
	if !IsProviderEvidenceSource(event) {
		return 0, false
	}
	if stringPayload(event.Payload, "payload_type") != "token_count" {
		return 0, false
	}
	if usage := mapPayload(event.Payload, "token_usage"); usage != nil {
		return intPayload(usage, "total_tokens")
	}
	raw := mapPayload(event.Payload, "raw")
	payload := mapPayload(raw, "payload")
	if payload == nil {
		payload = raw
	}
	info := mapPayload(payload, "info")
	usage := mapPayload(info, "last_token_usage")

	return intPayload(usage, "total_tokens")
}

func TokenSessionTotal(event model.Event) (int, bool) {
	if !IsProviderEvidenceSource(event) {
		return 0, false
	}
	if stringPayload(event.Payload, "payload_type") != "token_count" {
		return 0, false
	}
	if usage := mapPayload(event.Payload, "token_usage"); usage != nil {
		if total, ok := intPayload(usage, "session_total_tokens"); ok {
			return total, true
		}
	}
	raw := mapPayload(event.Payload, "raw")
	payload := mapPayload(raw, "payload")
	if payload == nil {
		payload = raw
	}
	info := mapPayload(payload, "info")
	usage := mapPayload(info, "total_token_usage")

	return intPayload(usage, "total_tokens")
}

func NormalizeCommandStatus(status string) string {
	switch status {
	case "success", "failed", "unknown":
		return status
	default:
		return ""
	}
}

func newEvent(meta EventMeta, eventType string, payload map[string]any) model.Event {
	return model.Event{
		EventID:   meta.EventID,
		SessionID: meta.SessionID,
		Timestamp: meta.Timestamp,
		Source:    meta.Source,
		Type:      eventType,
		Provider:  meta.Provider,
		CWD:       meta.CWD,
		Payload:   payload,
	}
}

func clonePayload(fields map[string]any) map[string]any {
	payload := make(map[string]any, len(fields))
	for key, value := range fields {
		payload[key] = value
	}

	return payload
}

func payloadMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return map[string]any{}
	}

	return payload
}

func providerFromEvent(event model.Event) string {
	if knownProvider(event.Provider) {
		return event.Provider
	}
	switch event.Source {
	case SourceCodex:
		return ProviderCodex
	case SourceClaude:
		return ProviderClaude
	default:
		return "unknown"
	}
}

func knownProvider(provider string) bool {
	return provider != "" && provider != "unknown"
}

func commandFromPayload(payload map[string]any) string {
	toolCall := mapPayload(payload, "tool_call")
	if toolCall == nil {
		return stringPayload(payload, "command")
	}
	if command := stringPayload(toolCall, "command"); command != "" {
		return command
	}
	if command := stringPayload(mapPayload(toolCall, "arguments"), "cmd"); command != "" {
		return command
	}

	return stringPayload(mapPayload(toolCall, "arguments"), "command")
}

func callIDFromPayload(payload map[string]any) string {
	if callID := stringPayload(payload, "call_id"); callID != "" {
		return callID
	}

	return stringPayload(mapPayload(payload, "tool_call"), "call_id")
}

func riskSignalFromRaw(raw any, fallbackCommand string) RiskSignal {
	if signal, ok := raw.(RiskSignal); ok {
		if signal.Command == "" {
			signal.Command = fallbackCommand
		}
		return signal
	}
	payload, ok := raw.(map[string]any)
	if !ok {
		payload = payloadMap(raw)
	}
	signal := RiskSignal{
		Level:      model.RiskLevel(stringPayload(payload, "level")),
		Signal:     stringPayload(payload, "signal"),
		Category:   stringPayload(payload, "category"),
		Command:    stringPayload(payload, "command"),
		Details:    stringPayload(payload, "details"),
		Confidence: model.Confidence(stringPayload(payload, "confidence")),
	}
	if signal.Command == "" {
		signal.Command = fallbackCommand
	}
	if lineNumber, ok := intPayload(payload, "line_no"); ok {
		signal.LineNumber = lineNumber
	}

	return signal
}

func riskRank(level model.RiskLevel) int {
	switch level {
	case model.RiskCritical:
		return 4
	case model.RiskHigh:
		return 3
	case model.RiskMedium:
		return 2
	case model.RiskLow:
		return 1
	default:
		return 0
	}
}

func stringPayload(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}

	return ""
}

func mapPayload(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	if value, ok := payload[key].(map[string]any); ok {
		return value
	}
	if value := payloadMap(payload[key]); len(value) > 0 {
		return value
	}

	return nil
}

func arrayPayload(payload map[string]any, key string) []any {
	switch value := payload[key].(type) {
	case []any:
		return value
	case []RiskSignal:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func intPayload(payload map[string]any, key string) (int, bool) {
	switch value := payload[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, false
		}

		return int(parsed), true
	default:
		return 0, false
	}
}

func boolPayload(payload map[string]any, key string) bool {
	if value, ok := payload[key].(bool); ok {
		return value
	}

	return false
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringPayload(payload, key); value != "" {
			return value
		}
	}

	return ""
}

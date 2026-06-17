package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

const (
	Source          = "claude_hook"
	DefaultMaxBytes = 2000
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)(authorization|token|api_key)=\S+`),
}

type ParseOptions struct {
	SessionID           string
	CWD                 string
	MaxOutputBytes      int
	RedactSecrets       bool
	RedactSecretsSet    bool
	StorePrompts        bool
	StoreRawToolOutputs bool
}

type ParseResult struct {
	EventCount   int             `json:"event_count"`
	CommandCount int             `json:"command_count"`
	WarningCount int             `json:"warning_count"`
	Events       []model.Event   `json:"-"`
	Warnings     []model.Warning `json:"warnings"`
}

type parsePrivacy struct {
	maxBytes            int
	redactSecrets       bool
	storePrompts        bool
	storeRawToolOutputs bool
}

func ParseReader(reader io.Reader, options ParseOptions) ParseResult {
	data, err := io.ReadAll(reader)
	if err != nil {
		result := ParseResult{}
		result.addWarning("claude_read_failed", err.Error())

		return result.finish()
	}

	return NormalizeJSON(data, options)
}

func NormalizeJSON(data []byte, options ParseOptions) ParseResult {
	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		result := ParseResult{}
		result.addWarning("claude_malformed_json", err.Error())

		return result.finish()
	}
	result := ParseResult{}
	result.consumeRecord(options, raw)

	return result.finish()
}

func (r *ParseResult) consumeRecord(options ParseOptions, raw map[string]any) {
	privacy := privacyFromOptions(options)
	payload := mapField(raw, "payload")
	if payload == nil {
		payload = raw
	}
	recordType := firstString(raw, "type", "event", "hook_event")
	payloadType := firstString(payload, "type", "event", "hook_event")
	rawType := recordType
	if rawType == "" {
		rawType = payloadType
	}
	ts := firstString(raw, "timestamp", "ts", "time")
	if ts == "" {
		ts = firstString(payload, "timestamp", "ts", "time")
	}
	arguments := argumentsMap(payload["arguments"])
	callID := firstString(payload, "call_id", "tool_call_id", "id")
	if callID == "" {
		callID = firstString(arguments, "call_id", "tool_call_id", "id")
	}
	tool := firstString(payload, "tool", "tool_name", "name")
	command := firstString(payload, "command", "cmd")
	if command == "" {
		command = firstString(arguments, "command", "cmd")
	}
	command = redact(command, privacy)
	if isResultRecord(rawType, payloadType) {
		r.consumeCommandResult(options, payload, ts, rawType, callID, tool, command, privacy)
		return
	}
	if command != "" {
		r.consumeCommandAttempt(options, payload, ts, rawType, callID, tool, command, arguments, privacy)
		return
	}
	r.consumeProviderEvent(options, ts, rawType, callID, tool, arguments, categoryFor(rawType, payloadType), privacy)
}

func (r *ParseResult) consumeCommandAttempt(options ParseOptions, payload map[string]any, ts string, rawType string, callID string, tool string, command string, arguments map[string]any, privacy parsePrivacy) {
	toolCall := map[string]any{
		"call_id":   callID,
		"tool":      tool,
		"command":   command,
		"arguments": redactMap(arguments, privacy),
	}
	r.Events = append(r.Events, model.Event{
		EventID:   fmt.Sprintf("evt_claude_%d", len(r.Events)+1),
		SessionID: options.SessionID,
		Timestamp: parseTime(ts),
		Source:    Source,
		Type:      "provider.command",
		Provider:  "claude",
		CWD:       options.CWD,
		Payload: map[string]any{
			"raw_type":  rawType,
			"tool_call": toolCall,
		},
	})
	r.CommandCount++
	if callID == "" {
		r.addWarning("claude_missing_call_id", "Claude command hook record is missing call_id.")
	}
	if tool == "" {
		r.addWarning("claude_missing_tool_name", "Claude command hook record is missing tool name.")
	}
	_ = payload
}

func (r *ParseResult) consumeCommandResult(options ParseOptions, payload map[string]any, ts string, rawType string, callID string, tool string, command string, privacy parsePrivacy) {
	rawOutput := firstString(payload, "output", "stdout", "content")
	status := firstString(payload, "status")
	exitCode, hasExitCode := intValue(payload["exit_code"])
	if status == "" {
		status = statusFromExitCode(hasExitCode, exitCode)
	}
	failedReason := firstString(payload, "failed_reason", "reason", "error")
	truncated := privacy.maxBytes > 0 && len(rawOutput) > privacy.maxBytes
	storedOutput := ""
	if privacy.storeRawToolOutputs {
		storedOutput = redact(rawOutput, privacy)
	}
	commandResult := map[string]any{
		"call_id":          callID,
		"tool":             tool,
		"command":          command,
		"status":           status,
		"stdout_truncated": truncated,
	}
	if hasExitCode {
		commandResult["exit_code"] = exitCode
	}
	if failedReason != "" {
		commandResult["failed_reason"] = redact(failedReason, privacy)
	}
	if storedOutput != "" {
		commandResult["stdout"] = storedOutput
	}
	r.Events = append(r.Events, model.Event{
		EventID:   fmt.Sprintf("evt_claude_%d", len(r.Events)+1),
		SessionID: options.SessionID,
		Timestamp: parseTime(ts),
		Source:    Source,
		Type:      "provider.command_result",
		Provider:  "claude",
		CWD:       options.CWD,
		Payload: map[string]any{
			"raw_type":       rawType,
			"command_result": commandResult,
		},
	})
	r.CommandCount++
	if callID == "" {
		r.addWarning("claude_missing_call_id", "Claude command-result hook record is missing call_id.")
	}
}

func (r *ParseResult) consumeProviderEvent(options ParseOptions, ts string, rawType string, callID string, tool string, arguments map[string]any, category string, privacy parsePrivacy) {
	toolCall := map[string]any{
		"call_id": callID,
		"tool":    tool,
	}
	if len(arguments) > 0 {
		toolCall["arguments"] = redactMap(arguments, privacy)
	}
	payload := map[string]any{
		"raw_type":  rawType,
		"category":  category,
		"tool_call": toolCall,
	}
	if privacy.storePrompts && isPromptRecord(rawType) {
		payload["prompt_retained"] = true
	}
	r.Events = append(r.Events, model.Event{
		EventID:   fmt.Sprintf("evt_claude_%d", len(r.Events)+1),
		SessionID: options.SessionID,
		Timestamp: parseTime(ts),
		Source:    Source,
		Type:      "provider.event",
		Provider:  "claude",
		CWD:       options.CWD,
		Payload:   payload,
	})
}

func (r *ParseResult) addWarning(code string, message string) {
	r.Warnings = append(r.Warnings, model.Warning{Code: code, Message: message})
}

func (r ParseResult) finish() ParseResult {
	r.EventCount = len(r.Events)
	r.WarningCount = len(r.Warnings)

	return r
}

func privacyFromOptions(options ParseOptions) parsePrivacy {
	maxBytes := options.MaxOutputBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	redactSecrets := true
	if options.RedactSecretsSet {
		redactSecrets = options.RedactSecrets
	}

	return parsePrivacy{
		maxBytes:            maxBytes,
		redactSecrets:       redactSecrets,
		storePrompts:        options.StorePrompts,
		storeRawToolOutputs: options.StoreRawToolOutputs,
	}
}

func isResultRecord(recordType string, payloadType string) bool {
	combined := strings.ToLower(recordType + " " + payloadType)
	for _, marker := range []string{"result", "output", "post_tool_use", "tool_result"} {
		if strings.Contains(combined, marker) {
			return true
		}
	}

	return false
}

func isPromptRecord(recordType string) bool {
	recordType = strings.ToLower(recordType)
	for _, marker := range []string{"prompt", "user_message", "assistant_message", "transcript"} {
		if strings.Contains(recordType, marker) {
			return true
		}
	}

	return false
}

func categoryFor(recordType string, payloadType string) string {
	combined := strings.ToLower(recordType + " " + payloadType)
	switch {
	case strings.Contains(combined, "permission"):
		return "permission"
	case strings.Contains(combined, "file") || strings.Contains(combined, "edit"):
		return "file_edit"
	case strings.Contains(combined, "hook"):
		return "hook_lifecycle"
	default:
		return "opaque"
	}
}

func statusFromExitCode(hasExitCode bool, exitCode int) string {
	if !hasExitCode {
		return "unknown"
	}
	if exitCode == 0 {
		return "success"
	}

	return "failed"
}

func argumentsMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		var decoded map[string]any
		if err := json.Unmarshal([]byte(typed), &decoded); err == nil {
			return decoded
		}
	}

	return map[string]any{}
}

func mapField(raw map[string]any, key string) map[string]any {
	if value, ok := raw[key].(map[string]any); ok {
		return value
	}

	return nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(raw, key); value != "" {
			return value
		}
	}

	return ""
}

func stringField(raw map[string]any, key string) string {
	switch value := raw[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func intValue(value any) (int, bool) {
	switch value := value.(type) {
	case float64:
		return int(value), true
	case int:
		return value, true
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

func parseTime(value string) time.Time {
	if value == "" {
		return time.Now().UTC()
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}

	return time.Now().UTC()
}

func redact(value string, privacy parsePrivacy) string {
	redacted := value
	if privacy.redactSecrets {
		for _, pattern := range secretPatterns {
			redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
		}
	}
	if privacy.maxBytes > 0 && len(redacted) > privacy.maxBytes {
		return redacted[:privacy.maxBytes]
	}

	return redacted
}

func redactMap(raw map[string]any, privacy parsePrivacy) map[string]any {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			out[key] = redact(typed, privacy)
		case map[string]any:
			out[key] = redactMap(typed, privacy)
		case []any:
			out[key] = redactSlice(typed, privacy)
		default:
			out[key] = value
		}
	}

	return out
}

func redactSlice(raw []any, privacy parsePrivacy) []any {
	out := make([]any, 0, len(raw))
	for _, value := range raw {
		switch typed := value.(type) {
		case string:
			out = append(out, redact(typed, privacy))
		case map[string]any:
			out = append(out, redactMap(typed, privacy))
		case []any:
			out = append(out, redactSlice(typed, privacy))
		default:
			out = append(out, value)
		}
	}

	return out
}

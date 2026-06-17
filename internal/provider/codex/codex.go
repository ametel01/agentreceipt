package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/commandrisk"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
	"github.com/ametel01/agentreceipt/internal/storage"
)

const (
	Source          = "codex_session_log"
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
	SourcePath          string
	MaxOutputBytes      int
	LineOffset          int
	RedactSecrets       bool
	RedactSecretsSet    bool
	StorePrompts        bool
	StoreRawToolOutputs bool
}

type parsePrivacy struct {
	maxBytes            int
	redactSecrets       bool
	storePrompts        bool
	storeRawToolOutputs bool
}

type ParseResult struct {
	SourcePath       string             `json:"source_path"`
	LineCount        int                `json:"line_count"`
	EventCount       int                `json:"event_count"`
	ToolCallCount    int                `json:"tool_call_count"`
	CommandCount     int                `json:"command_count"`
	ErrorCount       int                `json:"error_count"`
	WarningCount     int                `json:"warning_count"`
	Events           []model.Event      `json:"-"`
	Timeline         []TimelineRecord   `json:"timeline"`
	ToolCalls        []ToolCall         `json:"tool_calls"`
	Commands         []CommandEvent     `json:"commands"`
	TokenUsages      []TokenUsageEvent  `json:"token_usages"`
	ExecutionErrors  []ExecutionError   `json:"execution_errors"`
	RiskSignals      []RiskSignal       `json:"risk_signals"`
	SourceConfidence []ConfidenceRecord `json:"source_confidence"`
	Warnings         []ParseWarning     `json:"warnings"`
}

type TimelineRecord struct {
	Index    int            `json:"i"`
	Time     string         `json:"ts,omitempty"`
	Type     string         `json:"type"`
	Subtype  string         `json:"subtype,omitempty"`
	Category LogCategory    `json:"category"`
	Family   LogFamily      `json:"family"`
	TurnID   string         `json:"turn_id,omitempty"`
	Summary  string         `json:"summary"`
	Raw      map[string]any `json:"raw,omitempty"`
}

type ToolCall struct {
	SessionID  string         `json:"session_id"`
	LineNumber int            `json:"line_no,omitempty"`
	Time       string         `json:"ts,omitempty"`
	TurnID     string         `json:"turn_id,omitempty"`
	Tool       string         `json:"tool"`
	ToolType   string         `json:"tool_type"`
	CallID     string         `json:"call_id,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Command    string         `json:"command,omitempty"`
	Source     string         `json:"source"`
}

type CommandEvent struct {
	SessionID       string `json:"session_id"`
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
	Source          string `json:"source"`
}

type ExecutionError struct {
	SessionID  string `json:"session_id"`
	CallID     string `json:"call_id,omitempty"`
	ErrorClass string `json:"error_class"`
	Message    string `json:"message"`
	Severity   string `json:"severity"`
	Time       string `json:"ts,omitempty"`
}

type TokenUsageEvent struct {
	SessionID             string `json:"session_id"`
	LineNumber            int    `json:"line_no,omitempty"`
	TurnID                string `json:"turn_id,omitempty"`
	InputTokens           int    `json:"input_tokens"`
	CachedInputTokens     int    `json:"cached_input_tokens"`
	OutputTokens          int    `json:"output_tokens"`
	ReasoningOutputTokens int    `json:"reasoning_output_tokens"`
	TotalTokens           int    `json:"total_tokens"`
	Source                string `json:"source"`
}

type RiskSignal = providerevidence.RiskSignal

type ConfidenceRecord struct {
	Source     string           `json:"source"`
	Confidence model.Confidence `json:"confidence"`
	Reason     string           `json:"reason"`
}

type ParseWarning struct {
	LineNumber int    `json:"line_no,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type InspectResult struct {
	CodexHome  string         `json:"codex_home"`
	Candidates []Candidate    `json:"candidates"`
	Warnings   []ParseWarning `json:"warnings"`
}

type Candidate struct {
	Path    string    `json:"path"`
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
}

func ParseFile(path string, options ParseOptions) (ParseResult, error) {
	root, name, err := openRootForPath(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.Open(name)
	if err != nil {
		return ParseResult{}, fmt.Errorf("open Codex JSONL: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	options.SourcePath = path

	return ParseJSONL(file, options), nil
}

func ParseJSONL(reader io.Reader, options ParseOptions) ParseResult {
	if options.MaxOutputBytes <= 0 {
		options.MaxOutputBytes = DefaultMaxBytes
	}
	result := ParseResult{
		SourcePath: options.SourcePath,
		LineCount:  options.LineOffset,
		SourceConfidence: []ConfidenceRecord{{
			Source:     "session_jsonl",
			Confidence: model.ConfidenceHigh,
			Reason:     "structured Codex session JSONL parsed",
		}},
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		result.LineCount++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			result.addWarning(result.LineCount, "malformed_json", err.Error())
			result.Events = append(result.Events, warningEvent(options, result.LineCount, "malformed_json", err.Error()))
			continue
		}
		result.consumeRecord(options, raw)
	}
	if err := scanner.Err(); err != nil {
		result.addWarning(0, "read_error", err.Error())
	}
	result.EventCount = len(result.Events)
	result.WarningCount = len(result.Warnings)
	result.ToolCallCount = len(result.ToolCalls)
	result.CommandCount = len(result.Commands)
	result.ErrorCount = len(result.ExecutionErrors)
	if result.EventCount == 0 {
		result.SourceConfidence = append(result.SourceConfidence, ConfidenceRecord{
			Source:     "session_jsonl",
			Confidence: model.ConfidenceNone,
			Reason:     "no provider events extracted",
		})
	}

	return result
}

func (r *ParseResult) consumeRecord(options ParseOptions, raw map[string]any) {
	privacy := privacyFromOptions(options)
	recordType := stringField(raw, "type")
	payload := mapField(raw, "payload")
	if payload == nil {
		payload = raw
	}
	payloadType := stringField(payload, "type")
	ts := firstString(raw, "timestamp", "ts", "time")
	turnID := firstString(raw, "turn_id", "turnID")
	callID := firstString(payload, "call_id", "callID")
	summary := summarize(recordType, payloadType, payload, privacy)
	category := CategorizeRecord(raw)
	r.Timeline = append(r.Timeline, TimelineRecord{
		Index:    r.LineCount,
		Time:     ts,
		Type:     recordType,
		Subtype:  payloadType,
		Category: category.Category,
		Family:   category.Family,
		TurnID:   turnID,
		Summary:  summary,
	})

	switch payloadType {
	case "function_call", "custom_tool_call":
		r.consumeToolCall(options, raw, payload, ts, turnID, callID)
	case "function_call_output", "custom_tool_call_output":
		r.consumeCommandOutput(options, payload, ts, turnID, callID)
	case "token_count":
		r.consumeTokenCount(options, payload, turnID)
		r.Events = append(r.Events, providerEvent(options, r.LineCount, ts, recordType, payloadType, raw))
	default:
		r.Events = append(r.Events, providerEvent(options, r.LineCount, ts, recordType, payloadType, raw))
	}
}

func (r *ParseResult) consumeToolCall(options ParseOptions, raw map[string]any, payload map[string]any, ts string, turnID string, callID string) {
	privacy := privacyFromOptions(options)
	tool := firstString(payload, "name", "tool")
	args := argumentsMap(payload["arguments"])
	command := firstString(args, "cmd", "command")
	toolCall := ToolCall{
		SessionID:  options.SessionID,
		LineNumber: r.LineCount,
		Time:       ts,
		TurnID:     turnID,
		Tool:       tool,
		ToolType:   stringField(payload, "type"),
		CallID:     callID,
		Arguments:  redactMap(args, privacy),
		Command:    redact(command, privacy),
		Source:     "session_jsonl",
	}
	r.ToolCalls = append(r.ToolCalls, toolCall)
	var riskSignals []RiskSignal
	if command != "" {
		r.Commands = append(r.Commands, CommandEvent{
			SessionID:  options.SessionID,
			LineNumber: r.LineCount,
			CallID:     callID,
			TurnID:     turnID,
			Tool:       tool,
			Time:       ts,
			Command:    redact(command, privacy),
			Status:     "unknown",
			Source:     "session_jsonl",
		})
		riskSignals = r.addRiskSignals(options, command)
	}
	meta := providerevidence.EventMeta{
		EventID:   fmt.Sprintf("evt_codex_%d", r.LineCount),
		SessionID: options.SessionID,
		Timestamp: parseTime(ts),
		Source:    Source,
		Provider:  "codex",
		CWD:       options.CWD,
	}
	fields := map[string]any{
		"line_no":  r.LineCount,
		"raw_type": stringField(raw, "type"),
	}
	providerToolCall := providerevidence.ToolCall{
		SessionID:  toolCall.SessionID,
		LineNumber: toolCall.LineNumber,
		Time:       toolCall.Time,
		TurnID:     toolCall.TurnID,
		Tool:       toolCall.Tool,
		ToolType:   toolCall.ToolType,
		CallID:     toolCall.CallID,
		Arguments:  toolCall.Arguments,
		Command:    toolCall.Command,
		Source:     toolCall.Source,
	}
	if command != "" {
		r.Events = append(r.Events, providerevidence.NewCommandEvent(meta, providerToolCall, riskSignals, fields))
	} else {
		r.Events = append(r.Events, providerevidence.NewToolEvent(meta, providerToolCall, fields))
	}
	if tool == "" {
		r.addWarning(r.LineCount, "missing_tool_name", "function call record is missing tool name")
	}
}

func (r *ParseResult) consumeCommandOutput(options ParseOptions, payload map[string]any, ts string, turnID string, callID string) {
	privacy := privacyFromOptions(options)
	rawOutput := firstString(payload, "output", "content")
	status, exitCode, failedReason := commandStatus(rawOutput)
	output := redact(rawOutput, privacy)
	truncated := len(rawOutput) > privacy.maxBytes
	if !privacy.storeRawToolOutputs {
		output = ""
	}
	commandEvent := CommandEvent{
		SessionID:       options.SessionID,
		LineNumber:      r.LineCount,
		CallID:          callID,
		TurnID:          turnID,
		Tool:            firstString(payload, "name", "tool"),
		Time:            ts,
		Status:          status,
		ExitCode:        exitCode,
		Stdout:          output,
		StdoutTruncated: truncated,
		FailedReason:    failedReason,
		Source:          "session_jsonl",
	}
	r.Commands = append(r.Commands, commandEvent)
	if status == "failed" {
		r.ExecutionErrors = append(r.ExecutionErrors, ExecutionError{
			SessionID:  options.SessionID,
			CallID:     callID,
			ErrorClass: "exec_failed",
			Message:    failedReason,
			Severity:   "medium",
			Time:       ts,
		})
	}
	r.Events = append(r.Events, providerevidence.NewCommandResultEvent(providerevidence.EventMeta{
		EventID:   fmt.Sprintf("evt_codex_%d", r.LineCount),
		SessionID: options.SessionID,
		Timestamp: parseTime(ts),
		Source:    Source,
		Provider:  "codex",
		CWD:       options.CWD,
	}, providerevidence.CommandResult{
		SessionID:       commandEvent.SessionID,
		LineNumber:      commandEvent.LineNumber,
		CallID:          commandEvent.CallID,
		TurnID:          commandEvent.TurnID,
		Tool:            commandEvent.Tool,
		Time:            commandEvent.Time,
		Status:          commandEvent.Status,
		ExitCode:        commandEvent.ExitCode,
		Stdout:          commandEvent.Stdout,
		StdoutTruncated: commandEvent.StdoutTruncated,
		FailedReason:    commandEvent.FailedReason,
		Source:          commandEvent.Source,
	}, map[string]any{"line_no": r.LineCount}))
}

func (r *ParseResult) consumeTokenCount(options ParseOptions, payload map[string]any, turnID string) {
	info := mapField(payload, "info")
	if info == nil {
		return
	}
	usage := mapField(info, "last_token_usage")
	if usage == nil {
		return
	}
	r.TokenUsages = append(r.TokenUsages, TokenUsageEvent{
		SessionID:             options.SessionID,
		LineNumber:            r.LineCount,
		TurnID:                turnID,
		InputTokens:           intField(usage, "input_tokens"),
		CachedInputTokens:     intField(usage, "cached_input_tokens"),
		OutputTokens:          intField(usage, "output_tokens"),
		ReasoningOutputTokens: intField(usage, "reasoning_output_tokens"),
		TotalTokens:           intField(usage, "total_tokens"),
		Source:                "session_jsonl",
	})
}

func (r *ParseResult) addRiskSignals(options ParseOptions, command string) []RiskSignal {
	privacy := privacyFromOptions(options)
	signals := make([]RiskSignal, 0)
	for _, classification := range commandrisk.Classify(command) {
		signal := RiskSignal{
			SessionID:  options.SessionID,
			Level:      classification.Level,
			Signal:     classification.Signal,
			Category:   classification.Category,
			Command:    redact(command, privacy),
			Details:    classification.Reason,
			LineNumber: r.LineCount,
			Confidence: model.ConfidenceHigh,
		}
		signals = append(signals, signal)
		r.RiskSignals = append(r.RiskSignals, signal)
	}

	return signals
}

func (r *ParseResult) addWarning(lineNumber int, code string, message string) {
	r.Warnings = append(r.Warnings, ParseWarning{LineNumber: lineNumber, Code: code, Message: message})
}

func WriteTraces(layout storage.Layout, result ParseResult) error {
	if err := os.MkdirAll(layout.ProviderCodexTraces, 0o750); err != nil {
		return fmt.Errorf("create Codex trace directory: %w", err)
	}
	if err := os.MkdirAll(layout.ProviderCodex, 0o750); err != nil {
		return fmt.Errorf("create Codex provider directory: %w", err)
	}
	if err := writeJSON(layout.ProviderCodex, storage.ParseReportFile, result); err != nil {
		return err
	}
	writes := []struct {
		name string
		data any
	}{
		{name: "timeline.ndjson", data: result.Timeline},
		{name: "tool-calls.ndjson", data: result.ToolCalls},
		{name: "command-events.ndjson", data: result.Commands},
		{name: "errors.ndjson", data: result.ExecutionErrors},
		{name: "risk-signals.ndjson", data: result.RiskSignals},
		{name: "source-confidence.ndjson", data: result.SourceConfidence},
		{name: "session-summary.ndjson", data: []any{map[string]any{"event_count": result.EventCount, "warning_count": result.WarningCount, "command_count": result.CommandCount}}},
	}
	for _, write := range writes {
		if err := writeNDJSON(layout.ProviderCodexTraces, write.name, write.data); err != nil {
			return err
		}
	}

	return nil
}

func Inspect(home string) InspectResult {
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return InspectResult{Warnings: []ParseWarning{{Code: "home_unavailable", Message: err.Error()}}}
		}
		home = filepath.Join(userHome, ".codex")
	}
	result := InspectResult{CodexHome: home}
	for _, subdir := range []string{"sessions", "archived_sessions"} {
		dir := filepath.Join(home, subdir)
		root, err := os.OpenRoot(dir)
		if err != nil {
			continue
		}
		defer func() {
			_ = root.Close()
		}()
		_ = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry == nil || entry.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			info, statErr := entry.Info()
			if statErr != nil {
				return nil
			}
			result.Candidates = append(result.Candidates, Candidate{Path: filepath.Join(dir, path), ModTime: info.ModTime(), Size: info.Size()})

			return nil
		})
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].ModTime.After(result.Candidates[j].ModTime)
	})
	if len(result.Candidates) == 0 {
		result.Warnings = append(result.Warnings, ParseWarning{
			Code:    "codex_logs_missing",
			Message: "No Codex session JSONL files were found.",
		})
	}

	return result
}

func providerEvent(options ParseOptions, lineNumber int, ts string, recordType string, payloadType string, raw map[string]any) model.Event {
	privacy := privacyFromOptions(options)
	payload := map[string]any{
		"line_no":      lineNumber,
		"record_type":  recordType,
		"payload_type": payloadType,
	}
	if shouldStoreRawProviderRecord(payloadType, privacy) {
		payload["raw"] = redactMap(raw, privacy)
	}
	if payloadType == "token_count" {
		if tokenUsage := tokenUsagePayload(raw); len(tokenUsage) > 0 {
			payload["token_usage"] = tokenUsage
		}
	}

	return providerevidence.NewProviderEvent(providerevidence.EventMeta{
		EventID:   fmt.Sprintf("evt_codex_%d", lineNumber),
		SessionID: options.SessionID,
		Timestamp: parseTime(ts),
		Source:    Source,
		Provider:  "codex",
		CWD:       options.CWD,
	}, payload)
}

func tokenUsagePayload(raw map[string]any) map[string]any {
	payload := mapField(raw, "payload")
	if payload == nil {
		payload = raw
	}
	info := mapField(payload, "info")
	usage := mapField(info, "last_token_usage")
	if usage == nil {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens"} {
		if total, ok := intValue(usage[key]); ok {
			out[key] = total
		}
	}

	return out
}

func warningEvent(options ParseOptions, lineNumber int, code string, message string) model.Event {
	return providerevidence.NewParseWarningEvent(providerevidence.EventMeta{
		EventID:   fmt.Sprintf("evt_codex_warning_%d", lineNumber),
		SessionID: options.SessionID,
		Timestamp: time.Now().UTC(),
		Source:    Source,
		Provider:  "codex",
		CWD:       options.CWD,
	}, lineNumber, code, message)
}

func summarize(recordType string, payloadType string, payload map[string]any, privacy parsePrivacy) string {
	if command := firstString(argumentsMap(payload["arguments"]), "cmd", "command"); command != "" {
		return "command: " + redact(command, privacy)
	}
	if payloadType != "" {
		return payloadType
	}
	if recordType != "" {
		return recordType
	}

	return "unknown Codex record"
}

func commandStatus(output string) (string, *int, string) {
	if output == "" {
		return "unknown", nil, ""
	}
	exitPattern := regexp.MustCompile(`(?:Exit code:|Process exited with code)\s*([0-9]+)`)
	if match := exitPattern.FindStringSubmatch(output); len(match) == 2 {
		code, _ := strconv.Atoi(match[1])
		if code != 0 {
			return "failed", &code, "non-zero exit code"
		}

		return "success", &code, ""
	}
	if strings.Contains(output, "failed for") {
		return "failed", nil, "tool output contained failure marker"
	}

	return "success", nil, ""
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
	default:
		return ""
	}
}

func intField(raw map[string]any, key string) int {
	value, _ := intValue(raw[key])

	return value
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

func shouldStoreRawProviderRecord(payloadType string, privacy parsePrivacy) bool {
	switch payloadType {
	case "user_message", "agent_message", "assistant", "assistant_message", "developer", "developer_message", "system", "system_message", "reasoning":
		return privacy.storePrompts
	case "function_call_output", "custom_tool_call_output":
		return privacy.storeRawToolOutputs
	default:
		return false
	}
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

func writeJSON(dir string, name string, value any) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return root.WriteFile(name, data, 0o600)
}

func writeNDJSON(dir string, name string, value any) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := marshalNDJSON(value)
	if err != nil {
		return err
	}

	return root.WriteFile(name, data, 0o600)
}

func marshalNDJSON(value any) ([]byte, error) {
	items, ok := value.([]any)
	if !ok {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var generic []any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil, err
		}
		items = generic
	}
	var builder strings.Builder
	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		builder.Write(line)
		builder.WriteByte('\n')
	}

	return []byte(builder.String()), nil
}

func openRootForPath(path string) (*os.Root, string, error) {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	name := filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) {
		return nil, "", errors.New("path does not name a file")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", err
	}

	return root, name, nil
}

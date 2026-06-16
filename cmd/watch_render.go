package cmd

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/rs/zerolog"
)

const (
	watchFieldProvider    = "provider"
	watchFieldFamily      = "family"
	watchFieldCategory    = "category"
	watchFieldStatus      = "status"
	watchFieldTokens      = "tokens"
	watchFieldTotalTokens = "total_tokens"
	watchFieldExitCode    = "exit_code"
	watchFieldSourcePath  = "source_path"
	watchFieldReason      = "reason"
	watchFieldRiskLevel   = "risk_level"
	watchFieldRiskSignal  = "risk_signal"
	watchFieldRiskReason  = "risk_reason"
	watchFieldTool        = "tool"
	watchFieldCommand     = "command"
)

type WatchEvent struct {
	Provider    string
	Family      codex.LogFamily
	Category    codex.LogCategory
	Status      string
	Message     string
	Tokens      int
	TotalTokens int
	ExitCode    *int
	SourcePath  string
	Reason      string
	RiskLevel   model.RiskLevel
	RiskSignal  string
	RiskReason  string
	Tool        string
	Command     string
}

type codexWatchRenderer struct {
	logger         zerolog.Logger
	color          bool
	calls          map[string]codex.ToolCall
	risks          map[string][]codex.RiskSignal
	pendingActions []string
	lastTokenTotal int
	hasTokenTotal  bool
}

func newCodexWatchRenderer(out io.Writer) *codexWatchRenderer {
	return newCodexWatchRendererWithColor(out, false)
}

func newCodexWatchRendererWithColor(out io.Writer, color bool) *codexWatchRenderer {
	return &codexWatchRenderer{
		// zerolog is intentionally limited to streaming watch events. Review,
		// receipt, verify, and Markdown output are report renderers, not logs.
		logger: zerolog.New(newWatchConsoleWriter(out, color)).Level(zerolog.InfoLevel),
		color:  color,
		calls:  map[string]codex.ToolCall{},
		risks:  map[string][]codex.RiskSignal{},
	}
}

func (r *codexWatchRenderer) SeedTokenTotal(total int) {
	if total <= 0 {
		return
	}
	r.lastTokenTotal = total
	r.hasTokenTotal = true
}

func (r *codexWatchRenderer) Print(result codex.ParseResult) error {
	for _, event := range r.Events(result) {
		if err := r.PrintEvent(event); err != nil {
			return err
		}
	}

	return nil
}

func (r *codexWatchRenderer) Events(result codex.ParseResult) []WatchEvent {
	events := []WatchEvent{}
	toolCalls := toolCallsByLine(result.ToolCalls)
	commands := commandsByLine(result.Commands)
	tokenUsages := tokenUsagesByLine(result.TokenUsages)
	riskSignals := riskSignalsByLine(result.RiskSignals)
	for _, record := range result.Timeline {
		switch record.Category {
		case codex.CategoryExecCommandCall, codex.CategoryFunctionCall, codex.CategoryApplyPatchCall, codex.CategoryCustomToolCall:
			for _, toolCall := range toolCalls[record.Index] {
				r.recordToolCall(toolCall, riskSignals[record.Index])
			}
		case codex.CategoryFunctionCallOutput, codex.CategoryCustomToolCallOutput:
			for _, command := range commands[record.Index] {
				event, ok := r.commandEvent(command, record, result.SourcePath)
				if ok {
					events = append(events, event)
					if detail, ok := highRiskDetailEvent(event); ok {
						events = append(events, detail)
					}
				}
			}
		case codex.CategoryTokenCount:
			for _, usage := range tokenUsages[record.Index] {
				event, ok := r.tokenEvent(usage, record, result.SourcePath)
				if ok {
					events = append(events, event)
				}
			}
		}
	}
	for _, warning := range result.Warnings {
		events = append(events, warningEvent(warning, result.SourcePath))
	}

	return events
}

func (r *codexWatchRenderer) recordToolCall(toolCall codex.ToolCall, risks []codex.RiskSignal) {
	if toolCall.CallID != "" {
		r.calls[toolCall.CallID] = toolCall
		if len(risks) > 0 {
			r.risks[toolCall.CallID] = append([]codex.RiskSignal(nil), risks...)
		}
	}
}

func (r *codexWatchRenderer) commandEvent(command codex.CommandEvent, record codex.TimelineRecord, sourcePath string) (WatchEvent, bool) {
	if command.Status == "unknown" && command.Command != "" {
		return WatchEvent{}, false
	}
	toolCall := r.calls[command.CallID]
	subject := resultSubject(command, toolCall)
	label := resultLabel(command)
	risk, riskPresent := topRiskSignal(r.risks[command.CallID])
	r.pendingActions = append(r.pendingActions, subject)

	event := WatchEvent{
		Provider:   "codex",
		Family:     record.Family,
		Category:   record.Category,
		Status:     label,
		Message:    subject,
		ExitCode:   command.ExitCode,
		SourcePath: sourcePath,
		Tool:       emptyDefault(command.Tool, toolCall.Tool),
		Command:    emptyDefault(command.Command, toolCall.Command),
	}
	if riskPresent {
		event.RiskLevel = risk.Level
		event.RiskSignal = risk.Signal
		event.RiskReason = risk.Details
	}

	return event, true
}

func (r *codexWatchRenderer) tokenEvent(usage codex.TokenUsageEvent, record codex.TimelineRecord, sourcePath string) (WatchEvent, bool) {
	if len(r.pendingActions) == 0 {
		return WatchEvent{}, false
	}

	detail := ""
	switch len(r.pendingActions) {
	case 1:
		detail = "after " + truncate(r.pendingActions[0], 80)
	default:
		detail = fmt.Sprintf("after %d actions", len(r.pendingActions))
	}
	r.pendingActions = nil

	totalTokens := usage.TotalTokens
	tokens := totalTokens
	if r.hasTokenTotal && totalTokens >= r.lastTokenTotal {
		tokens = totalTokens - r.lastTokenTotal
	}
	r.lastTokenTotal = totalTokens
	r.hasTokenTotal = true

	return WatchEvent{
		Provider:    "codex",
		Family:      record.Family,
		Category:    record.Category,
		Status:      "tokens",
		Message:     detail,
		Tokens:      tokens,
		TotalTokens: totalTokens,
		SourcePath:  sourcePath,
	}, true
}

func warningEvent(warning codex.ParseWarning, sourcePath string) WatchEvent {
	return watchWarningEvent(warning.Code, warning.Message, sourcePath)
}

func watchFileEvent(sourcePath string, reason string) WatchEvent {
	return WatchEvent{
		Provider:   "codex",
		Family:     codex.LogFamilyContext,
		Category:   codex.CategorySessionMeta,
		Status:     "watch",
		Message:    fmt.Sprintf("%s (%s)", filepath.Base(sourcePath), reason),
		SourcePath: sourcePath,
		Reason:     reason,
	}
}

func highRiskDetailEvent(event WatchEvent) (WatchEvent, bool) {
	if event.RiskLevel != model.RiskHigh || event.RiskSignal == "" {
		return WatchEvent{}, false
	}

	return WatchEvent{
		Provider:   event.Provider,
		Family:     event.Family,
		Category:   event.Category,
		Status:     "risk",
		Message:    event.RiskSignal + ": " + event.RiskReason,
		SourcePath: event.SourcePath,
		Reason:     event.RiskSignal,
		RiskLevel:  event.RiskLevel,
		RiskSignal: event.RiskSignal,
		RiskReason: event.RiskReason,
		Tool:       event.Tool,
		Command:    event.Command,
	}, true
}

func watchWarningEvent(code string, message string, sourcePath string) WatchEvent {
	return WatchEvent{
		Provider:   "codex",
		Family:     codex.LogFamilyUnknown,
		Category:   codex.CategoryUnknown,
		Status:     "warn",
		Message:    code + ": " + message,
		SourcePath: sourcePath,
		Reason:     code,
	}
}

func (r *codexWatchRenderer) PrintEvent(event WatchEvent) error {
	logEvent := r.logger.Info().
		Str(watchFieldProvider, event.Provider).
		Str(watchFieldFamily, string(event.Family)).
		Str(watchFieldCategory, string(event.Category)).
		Str(watchFieldStatus, event.Status).
		Str(watchFieldSourcePath, event.SourcePath).
		Str(watchFieldReason, event.Reason).
		Str(watchFieldRiskLevel, string(event.RiskLevel)).
		Str(watchFieldRiskSignal, event.RiskSignal).
		Str(watchFieldRiskReason, event.RiskReason).
		Str(watchFieldTool, event.Tool).
		Str(watchFieldCommand, event.Command)
	if event.Tokens > 0 {
		logEvent = logEvent.Int(watchFieldTokens, event.Tokens)
	}
	if event.TotalTokens > 0 {
		logEvent = logEvent.Int(watchFieldTotalTokens, event.TotalTokens)
	}
	if event.ExitCode != nil {
		logEvent = logEvent.Int(watchFieldExitCode, *event.ExitCode)
	}
	logEvent.Msg(renderWatchMessage(event, r.color))

	return nil
}

func renderWatchMessage(event WatchEvent, color bool) string {
	value := event.Message
	if event.Status == "tokens" {
		value = fmt.Sprintf("%d", event.Tokens)
		if event.TotalTokens > 0 {
			value += fmt.Sprintf(" (%d session)", event.TotalTokens)
		}
		if event.Message != "" {
			value += " " + event.Message
		}
	}
	if event.ExitCode != nil {
		suffix := fmt.Sprintf(" (exit %d)", *event.ExitCode)
		value = truncate(value, 240-len(suffix)) + suffix
	}
	value = truncate(value, 240)
	if color {
		value = colorizeWatchMessage(event, value)
	} else if event.RiskLevel != "" && event.Status != "risk" {
		value = watchRiskBadge(event.RiskLevel, false) + " " + value
	}

	return value
}

func newWatchConsoleWriter(out io.Writer, color bool) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:        out,
		NoColor:    !color,
		PartsOrder: []string{watchFieldProvider, watchFieldStatus, zerolog.MessageFieldName},
		FieldsExclude: []string{
			watchFieldProvider,
			watchFieldFamily,
			watchFieldCategory,
			watchFieldStatus,
			watchFieldTokens,
			watchFieldTotalTokens,
			watchFieldExitCode,
			watchFieldSourcePath,
			watchFieldReason,
			watchFieldRiskLevel,
			watchFieldRiskSignal,
			watchFieldRiskReason,
			watchFieldTool,
			watchFieldCommand,
		},
		FormatMessage: func(value any) string {
			return truncate(fmt.Sprint(value), 240)
		},
		FormatPartValueByName: func(value any, name string) string {
			return formatWatchPartValue(value, name, color)
		},
	}
}

func formatWatchPartValue(value any, name string, color bool) string {
	switch name {
	case watchFieldProvider:
		return watchColorize(fmt.Sprintf("%-6s", fmt.Sprint(value)), watchColorDimGray, color)
	case watchFieldStatus:
		status := fmt.Sprint(value)
		return watchColorize(fmt.Sprintf("%-7s", status), watchStatusColor(status), color)
	default:
		return fmt.Sprint(value)
	}
}

const (
	watchColorBlue       = "34"
	watchColorCyan       = "36"
	watchColorDimGray    = "2;37"
	watchColorGreen      = "32"
	watchColorMagenta    = "35"
	watchColorRed        = "31"
	watchColorRedBold    = "1;31"
	watchColorWhiteBold  = "1;37"
	watchColorYellow     = "33"
	watchColorYellowBold = "1;33"
)

func watchStatusColor(status string) string {
	switch status {
	case "ok":
		return watchColorGreen
	case "fail":
		return watchColorRed
	case "warn":
		return watchColorYellowBold
	case "risk":
		return watchColorRedBold
	case "tokens":
		return watchColorYellow
	case "watch":
		return watchColorCyan
	default:
		return watchColorDimGray
	}
}

func colorizeWatchMessage(event WatchEvent, message string) string {
	if event.RiskLevel != "" && event.Status != "risk" {
		return watchRiskBadge(event.RiskLevel, true) + " " + colorizeWatchMessageBody(event, message)
	}

	return colorizeWatchMessageBody(event, message)
}

func colorizeWatchMessageBody(event WatchEvent, message string) string {
	switch event.Status {
	case "watch":
		return watchColorize(message, watchColorCyan, true)
	case "tokens":
		return watchColorize(message, watchColorYellow, true)
	case "warn":
		return watchColorize(message, watchColorYellowBold, true)
	case "risk":
		return watchColorize(message, riskColor(event.RiskLevel), true)
	case "fail":
		return watchColorize(message, watchColorRed, true)
	case "ok":
		switch {
		case event.Tool == "apply_patch" || hasWatchPrefix(event.Message, "edit "):
			return watchColorize(message, watchColorMagenta, true)
		case event.Command != "" || hasWatchPrefix(event.Message, "run "):
			return watchColorize(message, watchColorBlue, true)
		default:
			return watchColorize(message, watchColorGreen, true)
		}
	default:
		switch event.Family {
		case codex.LogFamilyConversation:
			return watchColorize(message, watchColorWhiteBold, true)
		case codex.LogFamilyTool:
			return watchColorize(message, watchColorBlue, true)
		case codex.LogFamilyTelemetry:
			return watchColorize(message, watchColorYellow, true)
		case codex.LogFamilyContext:
			return watchColorize(message, watchColorCyan, true)
		default:
			return watchColorize(message, watchColorDimGray, true)
		}
	}
}

func watchRiskBadge(level model.RiskLevel, color bool) string {
	label := ""
	switch level {
	case model.RiskHigh:
		label = "[HIGH]"
	case model.RiskMedium:
		label = "[MED]"
	case model.RiskLow:
		label = "[low]"
	default:
		return ""
	}

	return watchColorize(label, riskColor(level), color)
}

func riskColor(level model.RiskLevel) string {
	switch level {
	case model.RiskHigh:
		return watchColorRedBold
	case model.RiskMedium:
		return watchColorYellowBold
	case model.RiskLow:
		return watchColorDimGray
	default:
		return watchColorDimGray
	}
}

func hasWatchPrefix(value string, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func watchColorize(value string, code string, enabled bool) string {
	if !enabled || value == "" {
		return value
	}

	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func resultSubject(command codex.CommandEvent, toolCall codex.ToolCall) string {
	if command.Command != "" {
		return "run " + command.Command
	}
	if toolCall.Command != "" {
		return "run " + toolCall.Command
	}
	if toolCall.Tool == "apply_patch" {
		return "edit apply_patch"
	}
	if toolCall.Tool != "" {
		return "tool " + toolCall.Tool
	}
	if command.Tool == "apply_patch" {
		return "edit apply_patch"
	}
	if command.Tool != "" {
		return "tool " + command.Tool
	}

	return emptyDefault(command.CallID, "unknown")
}

func resultLabel(command codex.CommandEvent) string {
	switch command.Status {
	case "success":
		return "ok"
	case "failed":
		return "fail"
	default:
		return "result"
	}
}

func toolCallsByLine(calls []codex.ToolCall) map[int][]codex.ToolCall {
	grouped := map[int][]codex.ToolCall{}
	for _, call := range calls {
		grouped[call.LineNumber] = append(grouped[call.LineNumber], call)
	}

	return grouped
}

func commandsByLine(commands []codex.CommandEvent) map[int][]codex.CommandEvent {
	grouped := map[int][]codex.CommandEvent{}
	for _, command := range commands {
		grouped[command.LineNumber] = append(grouped[command.LineNumber], command)
	}

	return grouped
}

func tokenUsagesByLine(usages []codex.TokenUsageEvent) map[int][]codex.TokenUsageEvent {
	grouped := map[int][]codex.TokenUsageEvent{}
	for _, usage := range usages {
		grouped[usage.LineNumber] = append(grouped[usage.LineNumber], usage)
	}

	return grouped
}

func riskSignalsByLine(signals []codex.RiskSignal) map[int][]codex.RiskSignal {
	grouped := map[int][]codex.RiskSignal{}
	for _, signal := range signals {
		grouped[signal.LineNumber] = append(grouped[signal.LineNumber], signal)
	}

	return grouped
}

func topRiskSignal(signals []codex.RiskSignal) (codex.RiskSignal, bool) {
	if len(signals) == 0 {
		return codex.RiskSignal{}, false
	}
	top := signals[0]
	for _, signal := range signals[1:] {
		if watchRiskRank(signal.Level) > watchRiskRank(top.Level) {
			top = signal
		}
	}

	return top, true
}

func watchRiskRank(level model.RiskLevel) int {
	switch level {
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

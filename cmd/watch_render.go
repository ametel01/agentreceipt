package cmd

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/rs/zerolog"
)

const (
	watchFieldProvider   = "provider"
	watchFieldFamily     = "family"
	watchFieldCategory   = "category"
	watchFieldStatus     = "status"
	watchFieldTokens     = "tokens"
	watchFieldExitCode   = "exit_code"
	watchFieldSourcePath = "source_path"
	watchFieldReason     = "reason"
	watchFieldTool       = "tool"
	watchFieldCommand    = "command"
)

type WatchEvent struct {
	Provider   string
	Family     codex.LogFamily
	Category   codex.LogCategory
	Status     string
	Message    string
	Tokens     int
	ExitCode   *int
	SourcePath string
	Reason     string
	Tool       string
	Command    string
}

type codexWatchRenderer struct {
	logger         zerolog.Logger
	calls          map[string]codex.ToolCall
	pendingActions []string
}

func newCodexWatchRenderer(out io.Writer) *codexWatchRenderer {
	return &codexWatchRenderer{
		logger: zerolog.New(newWatchConsoleWriter(out, false)).Level(zerolog.InfoLevel),
		calls:  map[string]codex.ToolCall{},
	}
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
	for _, record := range result.Timeline {
		switch record.Category {
		case codex.CategoryExecCommandCall, codex.CategoryFunctionCall, codex.CategoryApplyPatchCall, codex.CategoryCustomToolCall:
			for _, toolCall := range toolCalls[record.Index] {
				r.recordToolCall(toolCall)
			}
		case codex.CategoryFunctionCallOutput, codex.CategoryCustomToolCallOutput:
			for _, command := range commands[record.Index] {
				event, ok := r.commandEvent(command, record, result.SourcePath)
				if ok {
					events = append(events, event)
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

func (r *codexWatchRenderer) recordToolCall(toolCall codex.ToolCall) {
	if toolCall.CallID != "" {
		r.calls[toolCall.CallID] = toolCall
	}
}

func (r *codexWatchRenderer) commandEvent(command codex.CommandEvent, record codex.TimelineRecord, sourcePath string) (WatchEvent, bool) {
	if command.Status == "unknown" && command.Command != "" {
		return WatchEvent{}, false
	}
	toolCall := r.calls[command.CallID]
	subject := resultSubject(command, toolCall)
	label := resultLabel(command)
	r.pendingActions = append(r.pendingActions, subject)

	return WatchEvent{
		Provider:   "codex",
		Family:     record.Family,
		Category:   record.Category,
		Status:     label,
		Message:    subject,
		ExitCode:   command.ExitCode,
		SourcePath: sourcePath,
		Tool:       emptyDefault(command.Tool, toolCall.Tool),
		Command:    emptyDefault(command.Command, toolCall.Command),
	}, true
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

	return WatchEvent{
		Provider:   "codex",
		Family:     record.Family,
		Category:   record.Category,
		Status:     "tokens",
		Message:    detail,
		Tokens:     usage.TotalTokens,
		SourcePath: sourcePath,
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
		Str(watchFieldTool, event.Tool).
		Str(watchFieldCommand, event.Command)
	if event.Tokens > 0 {
		logEvent = logEvent.Int(watchFieldTokens, event.Tokens)
	}
	if event.ExitCode != nil {
		logEvent = logEvent.Int(watchFieldExitCode, *event.ExitCode)
	}
	logEvent.Msg(renderWatchMessage(event))

	return nil
}

func renderWatchMessage(event WatchEvent) string {
	value := event.Message
	if event.Status == "tokens" {
		value = fmt.Sprintf("%d", event.Tokens)
		if event.Message != "" {
			value += " " + event.Message
		}
	}
	if event.ExitCode != nil {
		suffix := fmt.Sprintf(" (exit %d)", *event.ExitCode)
		value = truncate(value, 240-len(suffix)) + suffix
	}

	return truncate(value, 240)
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
			watchFieldExitCode,
			watchFieldSourcePath,
			watchFieldReason,
			watchFieldTool,
			watchFieldCommand,
		},
		FormatMessage: func(value any) string {
			return truncate(fmt.Sprint(value), 240)
		},
		FormatPartValueByName: formatWatchPartValue,
	}
}

func formatWatchPartValue(value any, name string) string {
	switch name {
	case watchFieldProvider:
		return fmt.Sprintf("%-6s", fmt.Sprint(value))
	case watchFieldStatus:
		return fmt.Sprintf("%-7s", fmt.Sprint(value))
	default:
		return fmt.Sprint(value)
	}
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

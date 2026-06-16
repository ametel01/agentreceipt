package cmd

import (
	"fmt"
	"io"

	"github.com/ametel01/agentreceipt/internal/provider/codex"
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
	out            io.Writer
	calls          map[string]codex.ToolCall
	pendingActions []string
}

func newCodexWatchRenderer(out io.Writer) *codexWatchRenderer {
	return &codexWatchRenderer{
		out:   out,
		calls: map[string]codex.ToolCall{},
	}
}

func (r *codexWatchRenderer) Print(result codex.ParseResult) error {
	for _, event := range r.Events(result) {
		if err := r.printEvent(event); err != nil {
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
	return WatchEvent{
		Provider:   "codex",
		Family:     codex.LogFamilyUnknown,
		Category:   codex.CategoryUnknown,
		Status:     "warn",
		Message:    warning.Code + ": " + warning.Message,
		SourcePath: sourcePath,
		Reason:     warning.Code,
	}
}

func (r *codexWatchRenderer) printEvent(event WatchEvent) error {
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

	return writeLiveLine(r.out, event.Status, value)
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

func writeLiveLine(out io.Writer, label string, value string) error {
	_, err := fmt.Fprintf(out, "[codex] %-6s %s\n", label, truncate(value, 240))

	return err
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

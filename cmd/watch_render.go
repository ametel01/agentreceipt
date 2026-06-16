package cmd

import (
	"fmt"
	"io"

	"github.com/ametel01/agentreceipt/internal/provider/codex"
)

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
	toolCalls := toolCallsByLine(result.ToolCalls)
	commands := commandsByLine(result.Commands)
	tokenUsages := tokenUsagesByLine(result.TokenUsages)
	for _, record := range result.Timeline {
		switch record.Category {
		case codex.CategoryExecCommandCall, codex.CategoryFunctionCall, codex.CategoryApplyPatchCall, codex.CategoryCustomToolCall:
			for _, toolCall := range toolCalls[record.Index] {
				if err := r.printToolCall(toolCall, record.Category); err != nil {
					return err
				}
			}
		case codex.CategoryFunctionCallOutput, codex.CategoryCustomToolCallOutput:
			for _, command := range commands[record.Index] {
				if err := r.printCommandResult(command); err != nil {
					return err
				}
			}
		case codex.CategoryTokenCount:
			for _, usage := range tokenUsages[record.Index] {
				if err := r.printTokenUsage(usage); err != nil {
					return err
				}
			}
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(r.out, "[codex] warn   %s: %s\n", warning.Code, warning.Message); err != nil {
			return err
		}
	}

	return nil
}

func (r *codexWatchRenderer) printToolCall(toolCall codex.ToolCall, category codex.LogCategory) error {
	if toolCall.CallID != "" {
		r.calls[toolCall.CallID] = toolCall
	}

	return nil
}

func (r *codexWatchRenderer) printCommandResult(command codex.CommandEvent) error {
	if command.Status == "unknown" && command.Command != "" {
		return nil
	}
	subject := resultSubject(command, r.calls[command.CallID])
	label := resultLabel(command)
	suffix := ""
	if command.ExitCode != nil {
		suffix = fmt.Sprintf(" (exit %d)", *command.ExitCode)
	}

	if suffix != "" {
		subject = truncate(subject, 240-len(suffix))
	}

	if err := writeLiveLine(r.out, label, subject+suffix); err != nil {
		return err
	}
	r.pendingActions = append(r.pendingActions, subject)

	return nil
}

func (r *codexWatchRenderer) printTokenUsage(usage codex.TokenUsageEvent) error {
	if len(r.pendingActions) == 0 {
		return nil
	}

	detail := fmt.Sprintf("%d", usage.TotalTokens)
	switch len(r.pendingActions) {
	case 1:
		detail += " after " + truncate(r.pendingActions[0], 80)
	default:
		detail += fmt.Sprintf(" after %d actions", len(r.pendingActions))
	}
	r.pendingActions = nil

	return writeLiveLine(r.out, "tokens", detail)
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

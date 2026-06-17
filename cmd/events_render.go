package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

func printEventsJSON(out io.Writer, events []model.Event) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(events)
}

func printEventsJSONL(out io.Writer, events []model.Event) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}

	return nil
}

func renderEventsTerminal(events []model.Event, color bool) string {
	var builder strings.Builder
	title := fmt.Sprintf("AgentReceipt Events (%d)", len(events))
	builder.WriteString(watchColorize(title, watchColorWhiteBold, color))
	builder.WriteString("\n\n")
	for index, event := range events {
		if index > 0 {
			builder.WriteString("\n")
		}
		renderEventTerminal(&builder, event, color)
	}

	return builder.String()
}

func renderEventTerminal(builder *strings.Builder, event model.Event, color bool) {
	sequence := watchColorize(fmt.Sprintf("#%d", event.Seq), watchColorDimGray, color)
	eventType := watchColorize(event.Type, eventTypeColor(event.Type), color)
	source := watchColorize(event.Source, watchColorCyan, color)
	timestamp := watchColorize(event.Timestamp.UTC().Format(time.RFC3339Nano), watchColorDimGray, color)
	fmt.Fprintf(builder, "%s %s  %s  %s\n", sequence, eventType, source, timestamp)
	if detail := eventDetail(event, color); detail != "" {
		fmt.Fprintf(builder, "  %s\n", detail)
	}
	if event.Provider != "" {
		fmt.Fprintf(builder, "  provider: %s\n", watchColorize(event.Provider, watchColorDimGray, color))
	}
	if event.CWD != "" {
		fmt.Fprintf(builder, "  cwd: %s\n", watchColorize(event.CWD, watchColorDimGray, color))
	}
	if len(event.Payload) > 0 {
		fmt.Fprintf(builder, "  %s\n%s\n", watchColorize("payload:", watchColorDimGray, color), prettyEventPayload(event.Payload))
	}
	if event.PrevHash != "" || event.EventHash != "" {
		builder.WriteString("  ")
		builder.WriteString(watchColorize("chain:", watchColorDimGray, color))
		if event.PrevHash != "" {
			fmt.Fprintf(builder, " prev=%s", watchColorize(shortHash(event.PrevHash), watchColorDimGray, color))
		}
		if event.EventHash != "" {
			fmt.Fprintf(builder, " event=%s", watchColorize(shortHash(event.EventHash), watchColorDimGray, color))
		}
		builder.WriteString("\n")
	}
}

func eventDetail(event model.Event, color bool) string {
	switch event.Type {
	case "fs.change":
		action := payloadString(event.Payload, "action")
		path := payloadString(event.Payload, "path")
		op := payloadString(event.Payload, "op")
		detail := strings.TrimSpace(action + " " + path)
		if detail == "" {
			return ""
		}
		actionColor := eventActionColor(action)
		rendered := watchColorize(action, actionColor, color)
		if path != "" {
			rendered += " " + watchColorize(path, watchColorWhiteBold, color)
		}
		if op != "" {
			rendered += " " + watchColorize("("+op+")", watchColorDimGray, color)
		}
		return rendered
	case "provider.command":
		command := payloadString(event.Payload, "command")
		if command == "" {
			command = nestedPayloadString(event.Payload, "tool_call", "command")
		}
		if command != "" {
			return watchColorize("run ", watchColorDimGray, color) + watchColorize(command, watchColorBlue, color)
		}
	case "provider.command_result":
		status := payloadString(event.Payload, "status")
		exitCode := payloadString(event.Payload, "exit_code")
		if status == "" {
			return ""
		}
		detail := watchColorize(status, eventStatusColor(status), color)
		if exitCode != "" {
			detail += " " + watchColorize("(exit "+exitCode+")", watchColorDimGray, color)
		}
		return detail
	case "manual.marker":
		message := payloadString(event.Payload, "message")
		if message != "" {
			return watchColorize(message, watchColorWhiteBold, color)
		}
	case "git.snapshot":
		phase := payloadString(event.Payload, "phase")
		if phase != "" {
			return watchColorize("phase ", watchColorDimGray, color) + watchColorize(phase, watchColorCyan, color)
		}
	case "receipt.finalize":
		status := payloadString(event.Payload, "status")
		if status != "" {
			return watchColorize(status, watchColorGreen, color)
		}
	}

	return ""
}

func prettyEventPayload(payload map[string]any) string {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "  " + fmt.Sprint(payload)
	}

	return indentLines(string(data), "  ")
}

func indentLines(value string, prefix string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}

	return strings.Join(lines, "\n")
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func nestedPayloadString(payload map[string]any, key string, nestedKey string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return ""
	}

	return payloadString(nested, nestedKey)
}

func shortHash(value string) string {
	if len(value) <= 19 {
		return value
	}
	if strings.HasPrefix(value, "sha256:") && len(value) > len("sha256:")+12 {
		return value[:len("sha256:")+12]
	}

	return value[:19]
}

func eventTypeColor(eventType string) string {
	switch eventType {
	case "fs.change":
		return watchColorMagenta
	case "git.snapshot":
		return watchColorCyan
	case "provider.command":
		return watchColorBlue
	case "provider.command_result":
		return watchColorGreen
	case "manual.marker":
		return watchColorYellow
	case "receipt.finalize":
		return watchColorWhiteBold
	default:
		return watchColorDimGray
	}
}

func eventActionColor(action string) string {
	switch action {
	case "create":
		return watchColorGreen
	case "modify":
		return watchColorBlue
	case "delete":
		return watchColorRed
	case "rename":
		return watchColorYellow
	default:
		return watchColorDimGray
	}
}

func eventStatusColor(status string) string {
	switch status {
	case "success", "ok":
		return watchColorGreen
	case "failed", "failure", "fail":
		return watchColorRed
	default:
		return watchColorYellow
	}
}

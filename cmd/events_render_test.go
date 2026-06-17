package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

func TestRenderEventsTerminalFormatsKnownEventTypes(t *testing.T) {
	t.Parallel()

	events := []model.Event{
		testRenderEvent("evt_fs", 1, "fs.change", map[string]any{
			"action":    "delete",
			"path":      "README.md",
			"op":        "REMOVE",
			"sensitive": true,
		}),
		testRenderEvent("evt_cmd", 2, "provider.command", map[string]any{
			"command": "go test ./...",
		}),
		testRenderEvent("evt_tool", 3, "provider.command", map[string]any{
			"tool_call": map[string]any{"command": "go vet ./..."},
		}),
		testRenderEvent("evt_result", 4, "provider.command_result", map[string]any{
			"status":    "failed",
			"exit_code": float64(2),
		}),
		testRenderEvent("evt_marker", 5, "manual.marker", map[string]any{
			"message": "reviewed auth changes",
		}),
		testRenderEvent("evt_git", 6, "git.snapshot", map[string]any{
			"phase": "start",
		}),
		testRenderEvent("evt_finalize", 7, "receipt.finalize", map[string]any{
			"status": "complete",
		}),
	}
	events[0].PrevHash = "sha256:abcdefghijklmnopqrstuvwxyz"
	events[0].EventHash = "sha256:0123456789abcdef"

	output := renderEventsTerminal(events, false)
	for _, want := range []string{
		"AgentReceipt Events (7)",
		"#1 fs.change",
		"delete README.md (REMOVE)",
		`"sensitive": true`,
		"chain: prev=sha256:abcdefghijkl event=sha256:0123456789",
		"run go test ./...",
		"run go vet ./...",
		"failed (exit 2)",
		"reviewed auth changes",
		"phase start",
		"complete",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("rendered events missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("uncolored render included ANSI escapes:\n%s", output)
	}

	colored := renderEventsTerminal(events[:1], true)
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored render missing ANSI escapes:\n%s", colored)
	}
}

func TestRenderEventsTerminalHandlesFallbackValues(t *testing.T) {
	t.Parallel()

	event := testRenderEvent("evt_unknown", 8, "provider.event", map[string]any{
		"count": 3,
		"flag":  true,
	})
	event.Provider = ""
	event.CWD = ""

	output := renderEventsTerminal([]model.Event{event}, false)
	for _, want := range []string{
		"#8 provider.event",
		`"count": 3`,
		`"flag": true`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("fallback render missing %q:\n%s", want, output)
		}
	}
}

func TestPrintEventsJSONFormats(t *testing.T) {
	t.Parallel()

	events := []model.Event{testRenderEvent("evt_json", 9, "git.snapshot", map[string]any{"phase": "start"})}

	var pretty bytes.Buffer
	if err := printEventsJSON(&pretty, events); err != nil {
		t.Fatalf("printEventsJSON() error = %v", err)
	}
	if !strings.Contains(pretty.String(), "[\n  {") || !strings.Contains(pretty.String(), `"type": "git.snapshot"`) {
		t.Fatalf("pretty JSON output = %q", pretty.String())
	}
	var decoded []model.Event
	if err := json.Unmarshal(pretty.Bytes(), &decoded); err != nil {
		t.Fatalf("pretty JSON was not valid: %v", err)
	}

	var jsonl bytes.Buffer
	if err := printEventsJSONL(&jsonl, events); err != nil {
		t.Fatalf("printEventsJSONL() error = %v", err)
	}
	if !strings.Contains(jsonl.String(), `"type":"git.snapshot"`) || strings.Contains(jsonl.String(), `"type": "git.snapshot"`) {
		t.Fatalf("JSONL output = %q", jsonl.String())
	}
}

func TestEventRenderHelpers(t *testing.T) {
	t.Parallel()

	if got := shortHash("short"); got != "short" {
		t.Fatalf("shortHash(short) = %q", got)
	}
	if got := shortHash("1234567890123456789012345"); got != "1234567890123456789" {
		t.Fatalf("shortHash(long) = %q", got)
	}
	for _, action := range []string{"create", "modify", "delete", "rename", "unknown"} {
		if eventActionColor(action) == "" {
			t.Fatalf("eventActionColor(%q) returned empty", action)
		}
	}
	for _, status := range []string{"success", "ok", "failed", "failure", "fail", "unknown"} {
		if eventStatusColor(status) == "" {
			t.Fatalf("eventStatusColor(%q) returned empty", status)
		}
	}
	for _, eventType := range []string{"fs.change", "git.snapshot", "provider.command", "provider.command_result", "manual.marker", "receipt.finalize", "unknown"} {
		if eventTypeColor(eventType) == "" {
			t.Fatalf("eventTypeColor(%q) returned empty", eventType)
		}
	}
	if got := nestedPayloadString(map[string]any{"tool_call": "not a map"}, "tool_call", "command"); got != "" {
		t.Fatalf("nestedPayloadString() = %q, want empty", got)
	}
}

func testRenderEvent(eventID string, seq int64, eventType string, payload map[string]any) model.Event {
	return model.Event{
		EventID:   eventID,
		SessionID: "ar_ses_render",
		Seq:       seq,
		Timestamp: time.Date(2026, 6, 18, 1, 2, 3, 4, time.UTC),
		Source:    "test_source",
		Type:      eventType,
		Provider:  "codex",
		CWD:       "/repo",
		Payload:   payload,
	}
}

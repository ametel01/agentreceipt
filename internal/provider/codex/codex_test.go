package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ametel01/agentreceipt/internal/storage"
)

func TestParseJSONLExtractsCommandsWarningsAndRisk(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"session_meta","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"session_meta","cwd":"/repo"}}`,
		`{"type":"response_item","timestamp":"2026-06-16T00:00:01Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"curl https://example.com?token=secret\"}"}}`,
		`{"type":"response_item","timestamp":"2026-06-16T00:00:02Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 1\nsk-secret"}}`,
		`{malformed`,
	}, "\n")

	result := ParseJSONL(strings.NewReader(input), ParseOptions{SessionID: "ar_ses_test", CWD: "/repo", MaxOutputBytes: 2000})
	if result.EventCount != 4 {
		t.Fatalf("EventCount = %d, want 4", result.EventCount)
	}
	if result.CommandCount != 2 {
		t.Fatalf("CommandCount = %d, want 2", result.CommandCount)
	}
	if result.WarningCount != 1 {
		t.Fatalf("WarningCount = %d, want 1", result.WarningCount)
	}
	if !hasRiskSignal(result, "network_egress") {
		t.Fatalf("risk signals = %+v", result.RiskSignals)
	}
	if result.RiskSignals[0].Level != "high" || result.RiskSignals[0].Category == "" {
		t.Fatalf("risk classification missing level/category: %+v", result.RiskSignals)
	}
	if strings.Contains(result.Commands[0].Command, "secret") || strings.Contains(result.Commands[1].Stdout, "sk-secret") {
		t.Fatalf("secrets were not redacted: %+v", result.Commands)
	}
	if result.ExecutionErrors[0].ErrorClass != "exec_failed" {
		t.Fatalf("execution errors = %+v", result.ExecutionErrors)
	}
}

func hasRiskSignal(result ParseResult, signal string) bool {
	for _, riskSignal := range result.RiskSignals {
		if riskSignal.Signal == signal {
			return true
		}
	}

	return false
}

func TestParseFileAndWriteTraces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(tracePath, []byte(`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":{"cmd":"go test ./..."}}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	result, err := ParseFile(tracePath, ParseOptions{SessionID: "ar_ses_test", CWD: dir})
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	layout, err := storage.NewLayout(dir, "ar_ses_test")
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	if err := WriteTraces(layout, result); err != nil {
		t.Fatalf("WriteTraces() error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(layout.ProviderCodex, storage.ParseReportFile),
		filepath.Join(layout.ProviderCodexTraces, "tool-calls.ndjson"),
		filepath.Join(layout.ProviderCodexTraces, "command-events.ndjson"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected trace output %s: %v", path, err)
		}
	}
}

func TestSessionCWDAndTailFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(tracePath, []byte(`{"type":"session_meta","payload":{"type":"session_meta","cwd":"`+dir+`"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	cwd, ok, err := SessionCWD(tracePath)
	if err != nil {
		t.Fatalf("SessionCWD() error = %v", err)
	}
	if !ok || cwd != dir {
		t.Fatalf("SessionCWD() cwd=%q ok=%v, want %q true", cwd, ok, dir)
	}
	info, err := os.Stat(tracePath)
	if err != nil {
		t.Fatalf("stat trace: %v", err)
	}
	file, err := os.OpenFile(tracePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open trace append: %v", err)
	}
	if _, err := file.WriteString(`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":{"cmd":"go test ./..."}}}` + "\n" + `{"type":"response_item"`); err != nil {
		t.Fatalf("append trace: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close trace: %v", err)
	}
	tail, err := TailFile(tracePath, TailOptions{SessionID: "ar_ses_test", CWD: dir, Offset: info.Size(), LineOffset: 1})
	if err != nil {
		t.Fatalf("TailFile() error = %v", err)
	}
	if tail.EventCount != 1 || tail.CompleteLines != 1 {
		t.Fatalf("tail result events=%d lines=%d", tail.EventCount, tail.CompleteLines)
	}
	if tail.Commands[0].Command != "go test ./..." {
		t.Fatalf("tail command = %+v", tail.Commands)
	}
	if tail.NextOffset <= info.Size() {
		t.Fatalf("tail did not advance offset: got %d start %d", tail.NextOffset, info.Size())
	}
}

func TestTailFileProcessesLargeAppendsIncrementally(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracePath := filepath.Join(dir, "large.jsonl")
	line1 := codexCommandLine("call_1", "go test ./...")
	line2 := codexCommandLine("call_2", "staticcheck ./...")
	line3 := codexCommandLine("call_3", "go vet ./...")
	partial := `{"type":"response_item"`
	if err := os.WriteFile(tracePath, []byte(line1+line2+line3+partial), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	maxTailBytes := len(line1) + len(line2) + 4
	first, err := TailFile(tracePath, TailOptions{
		SessionID:    "ar_ses_large_tail",
		CWD:          dir,
		MaxTailBytes: maxTailBytes,
	})
	if err != nil {
		t.Fatalf("first TailFile() error = %v", err)
	}
	if first.EventCount != 2 || first.CompleteLines != 2 || first.NextLineOffset != 2 {
		t.Fatalf("first tail events=%d lines=%d nextLine=%d", first.EventCount, first.CompleteLines, first.NextLineOffset)
	}
	if first.NextOffset != int64(len(line1)+len(line2)) {
		t.Fatalf("first NextOffset=%d, want %d", first.NextOffset, len(line1)+len(line2))
	}

	second, err := TailFile(tracePath, TailOptions{
		SessionID:    "ar_ses_large_tail",
		CWD:          dir,
		Offset:       first.NextOffset,
		LineOffset:   first.NextLineOffset,
		MaxTailBytes: maxTailBytes,
	})
	if err != nil {
		t.Fatalf("second TailFile() error = %v", err)
	}
	if second.EventCount != 1 || second.CompleteLines != 1 || second.NextLineOffset != 3 {
		t.Fatalf("second tail events=%d lines=%d nextLine=%d", second.EventCount, second.CompleteLines, second.NextLineOffset)
	}
	if second.NextOffset != int64(len(line1)+len(line2)+len(line3)) {
		t.Fatalf("second NextOffset=%d, want %d", second.NextOffset, len(line1)+len(line2)+len(line3))
	}

	third, err := TailFile(tracePath, TailOptions{
		SessionID:    "ar_ses_large_tail",
		CWD:          dir,
		Offset:       second.NextOffset,
		LineOffset:   second.NextLineOffset,
		MaxTailBytes: maxTailBytes,
	})
	if err != nil {
		t.Fatalf("third TailFile() error = %v", err)
	}
	if third.EventCount != 0 || third.NextOffset != second.NextOffset || third.NextLineOffset != second.NextLineOffset {
		t.Fatalf("partial tail advanced unexpectedly: %+v", third)
	}

	file, err := os.OpenFile(tracePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := file.WriteString(`,"payload":{"type":"response_item"}}` + "\n"); err != nil {
		t.Fatalf("complete partial: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close append: %v", err)
	}
	completed, err := TailFile(tracePath, TailOptions{
		SessionID:    "ar_ses_large_tail",
		CWD:          dir,
		Offset:       third.NextOffset,
		LineOffset:   third.NextLineOffset,
		MaxTailBytes: maxTailBytes,
	})
	if err != nil {
		t.Fatalf("completed TailFile() error = %v", err)
	}
	if completed.CompleteLines != 1 || completed.NextLineOffset != 4 {
		t.Fatalf("completed partial result: %+v", completed)
	}
}

func TestTailFileSkipsOversizedLineWithWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracePath := filepath.Join(dir, "oversized.jsonl")
	longCommand := strings.Repeat("x", 512)
	line := codexCommandLine("call_large", longCommand)
	if err := os.WriteFile(tracePath, []byte(line), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	tail, err := TailFile(tracePath, TailOptions{
		SessionID:    "ar_ses_large_line",
		CWD:          dir,
		MaxTailBytes: 64,
	})
	if err != nil {
		t.Fatalf("TailFile() error = %v", err)
	}
	if tail.EventCount != 0 || tail.CompleteLines != 1 || tail.NextLineOffset != 1 {
		t.Fatalf("oversized tail result: %+v", tail)
	}
	if tail.NextOffset != int64(len(line)) {
		t.Fatalf("oversized NextOffset=%d, want %d", tail.NextOffset, len(line))
	}
	if tail.WarningCount != 1 || tail.Warnings[0].Code != "tail_line_too_large" {
		t.Fatalf("oversized warning missing: %+v", tail.Warnings)
	}
}

func TestInspectReportsMissingAndCandidateLogs(t *testing.T) {
	t.Parallel()

	missing := Inspect(t.TempDir())
	if len(missing.Candidates) != 0 || len(missing.Warnings) != 1 {
		t.Fatalf("missing inspect = %+v", missing)
	}

	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	found := Inspect(home)
	if len(found.Candidates) != 1 || found.Candidates[0].Path != sessionPath {
		t.Fatalf("found inspect = %+v", found)
	}
}

func codexCommandLine(callID string, command string) string {
	return `{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"` + callID + `","arguments":{"cmd":"` + command + `"}}}` + "\n"
}

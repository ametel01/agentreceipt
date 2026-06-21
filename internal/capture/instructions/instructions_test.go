package instructions

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/ametel01/agentreceipt/internal/model"
)

func TestCaptureInstructionFilesCollectsAGENTSAndCLAUDEMetadata(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# Team Rules\n- Use deterministic events\n- Run tests before commit\n"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Claude Instructions\n- Be conservative with risk\n"), 0o600); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	events, warnings, err := CaptureInstructionFiles(repo, "session-1")
	if err != nil {
		t.Fatalf("CaptureInstructionFiles() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	byPath := map[string]model.Event{}
	for _, event := range events {
		if event.Source != Source || event.Type != TypeInstructionFile {
			t.Fatalf("unexpected event identity: %+v", event)
		}
		if got, want := event.CWD, repo; got != want {
			t.Fatalf("event cwd = %q, want %q", got, want)
		}
		path := event.Payload["path"].(string)
		byPath[path] = event
	}
	if len(byPath) != 2 {
		t.Fatalf("events paths = %+v", byPath)
	}
	assertInstructionEvent(t, byPath["AGENTS.md"], filepath.Join(repo, "AGENTS.md"), "agents")
	assertInstructionEvent(t, byPath["CLAUDE.md"], filepath.Join(repo, "CLAUDE.md"), "claude")
}

func TestCaptureInstructionFilesSkipsMissingFiles(t *testing.T) {
	t.Parallel()

	events, warnings, err := CaptureInstructionFiles(t.TempDir(), "session-missing")
	if err != nil {
		t.Fatalf("CaptureInstructionFiles() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("len(events) = %d, want 0", len(events))
	}
	if len(warnings) != 0 {
		t.Fatalf("len(warnings) = %d, want 0", len(warnings))
	}
}

func TestCaptureInstructionFilesWarnsOnNonRegularInstructionFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, "AGENTS.md"), 0o750); err != nil {
		t.Fatalf("mkdir AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("No problem"), 0o600); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	events, warnings, err := CaptureInstructionFiles(repo, "session-warning")
	if err != nil {
		t.Fatalf("CaptureInstructionFiles() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1", len(warnings))
	}
	if warnings[0].Code != "instruction_capture.non_regular_agents" {
		t.Fatalf("warning code = %q, want %q", warnings[0].Code, "instruction_capture.non_regular_agents")
	}
}

func TestSummarizeInstructionFileContentIsDeterministicAndBounded(t *testing.T) {
	t.Parallel()

	content := []byte("# Heading\n\n- Rule A should always apply.\n\nRun only vetted tests.\n")
	first := summarizeInstructionFileContent(content)
	second := summarizeInstructionFileContent(content)

	if len(first) == 0 || len(first) > 3 {
		t.Fatalf("summary len=%d, want in [1,3]", len(first))
	}
	for _, summary := range first {
		if utf8.RuneCountInString(summary) > 120 {
			t.Fatalf("summary line too long: %q", summary)
		}
	}
	if len(second) != len(first) {
		t.Fatalf("determinism violated: first=%#v second=%#v", first, second)
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("determinism violated: first=%#v second=%#v", first, second)
		}
	}
}

func TestSummarizeInstructionFileContentDropsLongLines(t *testing.T) {
	t.Parallel()

	long := make([]byte, 200)
	for index := range long {
		long[index] = 'a'
	}
	summary := summarizeInstructionFileContent([]byte("### " + string(long)))
	if len(summary) != 1 {
		t.Fatalf("summary len = %d, want 1", len(summary))
	}
	if len(summary[0]) > 126 {
		t.Fatalf("summary not truncated: %q", summary[0])
	}
}

func assertInstructionEvent(t *testing.T, event model.Event, filePath string, expectedTag string) {
	t.Helper()

	path := filePath
	if got := event.Payload["path"]; got != filepath.Base(filePath) {
		t.Fatalf("event path = %q, want %q", got, filepath.Base(filePath))
	}
	if got, ok := event.Payload["hash"].(string); !ok || got == "" {
		t.Fatalf("event hash = %v, want non-empty", got)
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		expect := "sha256:" + expectedHash(data)
		if got != expect {
			t.Fatalf("event hash = %q, want %q", got, expect)
		}
	}
	size, ok := event.Payload["size"].(int64)
	if !ok {
		sizeInt := event.Payload["size"].(int)
		size = int64(sizeInt)
	}
	stat, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat %q: %v", filePath, err)
	}
	if size != stat.Size() {
		t.Fatalf("event size = %d, want %d", size, stat.Size())
	}
	summary, ok := event.Payload["summary"].([]string)
	if !ok {
		t.Fatalf("event summary type = %T, want []string", event.Payload["summary"])
	}
	if len(summary) == 0 {
		t.Fatalf("event summary empty for %s", expectedTag)
	}
}

func expectedHash(data []byte) string {
	sum := sha256.Sum256(data)

	return fmt.Sprintf("%x", sum)
}

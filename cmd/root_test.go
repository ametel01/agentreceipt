package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
	"github.com/spf13/cobra"
)

func TestRootHelpListsCommandSurface(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("root help returned error: %v", err)
	}

	required := []string{
		"init",
		"install codex",
		"install claude",
		"start",
		"status",
		"live",
		"stop",
		"review",
		"verify",
		"export",
		"import codex-jsonl",
		"inspect codex",
		"mark",
		"pr comment",
		"--config",
		"--repo",
		"--quiet",
	}
	for _, want := range required {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q\nhelp:\n%s", want, stdout)
		}
	}
}

func TestCommandTreeContainsRequiredCommands(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	required := [][]string{
		{"init"},
		{"install", "codex"},
		{"install", "claude"},
		{"start"},
		{"status"},
		{"live"},
		{"stop"},
		{"review"},
		{"verify"},
		{"export"},
		{"import", "codex-jsonl"},
		{"inspect", "codex"},
		{"mark"},
		{"pr", "comment"},
		{"version"},
	}
	for _, path := range required {
		if found, _, err := root.Find(path); err != nil || found == nil {
			t.Fatalf("command %q not found: %v", strings.Join(path, " "), err)
		}
	}
}

func TestReviewModeFlags(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	review, _, err := root.Find([]string{"review"})
	if err != nil {
		t.Fatalf("find review command: %v", err)
	}

	for _, name := range []string{"last", "session", "security", "diff", "json", "md", "pr"} {
		if review.Flags().Lookup(name) == nil {
			t.Fatalf("review flag %q is not registered", name)
		}
	}
}

func TestStartWatchFlags(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	start, _, err := root.Find([]string{"start"})
	if err != nil {
		t.Fatalf("find start command: %v", err)
	}

	for _, name := range []string{"watch", "codex-home", "watch-interval", "watch-duration", "watch-existing"} {
		if start.Flags().Lookup(name) == nil {
			t.Fatalf("start flag %q is not registered", name)
		}
	}
}

func TestStartWatchOptionsValidation(t *testing.T) {
	t.Parallel()

	cmd := newStartCommand()
	if err := cmd.Flags().Set("watch-interval", "0"); err != nil {
		t.Fatalf("set watch-interval: %v", err)
	}
	if _, err := watchOptionsFromStartCommand(cmd); err == nil || !strings.Contains(err.Error(), "watch-interval") {
		t.Fatalf("expected interval validation error, got %v", err)
	}

	cmd = newStartCommand()
	if err := cmd.Flags().Set("watch-duration", "-1s"); err != nil {
		t.Fatalf("set watch-duration: %v", err)
	}
	if _, err := watchOptionsFromStartCommand(cmd); err == nil || !strings.Contains(err.Error(), "watch-duration") {
		t.Fatalf("expected duration validation error, got %v", err)
	}

	cmd = newStartCommand()
	if err := cmd.Flags().Set("watch-interval", "250ms"); err != nil {
		t.Fatalf("set watch-interval: %v", err)
	}
	if err := cmd.Flags().Set("watch-duration", "1s"); err != nil {
		t.Fatalf("set watch-duration: %v", err)
	}
	if err := cmd.Flags().Set("watch-existing", "true"); err != nil {
		t.Fatalf("set watch-existing: %v", err)
	}
	options, err := watchOptionsFromStartCommand(cmd)
	if err != nil {
		t.Fatalf("watchOptionsFromStartCommand() error = %v", err)
	}
	if options.Interval != 250*time.Millisecond || options.Duration != time.Second || !options.IncludeExisting {
		t.Fatalf("options = %+v", options)
	}
}

func TestCodexCandidateMatchesRepoAndNewLogs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	otherRepo := t.TempDir()
	dir := t.TempDir()
	matchingTrace := filepath.Join(dir, "matching.jsonl")
	if err := os.WriteFile(matchingTrace, []byte(`{"type":"session_meta","payload":{"type":"session_meta","cwd":"`+repo+`"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write matching trace: %v", err)
	}
	matches, reason := codexCandidateMatches(codex.Candidate{Path: matchingTrace, ModTime: time.Now()}, repo, time.Now())
	if !matches || !strings.Contains(reason, "cwd") {
		t.Fatalf("expected cwd match, got matches=%v reason=%q", matches, reason)
	}

	otherTrace := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(otherTrace, []byte(`{"type":"session_meta","payload":{"type":"session_meta","cwd":"`+otherRepo+`"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write other trace: %v", err)
	}
	matches, _ = codexCandidateMatches(codex.Candidate{Path: otherTrace, ModTime: time.Now()}, repo, time.Now())
	if matches {
		t.Fatal("candidate from another cwd matched repo")
	}

	noMetadataTrace := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(noMetadataTrace, []byte(`{"type":"response_item"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write new trace: %v", err)
	}
	matches, reason = codexCandidateMatches(codex.Candidate{Path: noMetadataTrace, ModTime: time.Now()}, repo, time.Now().Add(-time.Second))
	if !matches || !strings.Contains(reason, "new log") {
		t.Fatalf("expected new log match, got matches=%v reason=%q", matches, reason)
	}
	matches, _ = codexCandidateMatches(codex.Candidate{Path: noMetadataTrace, ModTime: time.Now().Add(-time.Minute)}, repo, time.Now())
	if matches {
		t.Fatal("old candidate without cwd metadata matched repo")
	}
}

func TestPrintCodexLiveResultFormatsToolsResultsAndWarnings(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	exitCode := 7
	longCommand := strings.Repeat("x", 300)
	result := codex.ParseResult{
		ToolCalls: []codex.ToolCall{
			{Tool: "read_file"},
			{Tool: "exec_command", Command: longCommand},
			{Command: "go test ./..."},
		},
		Commands: []codex.CommandEvent{
			{Command: "go test ./...", Status: "unknown"},
			{CallID: "call_1", Status: "failed", ExitCode: &exitCode},
			{Status: "success"},
		},
		Warnings: []codex.ParseWarning{{Code: "malformed_json", Message: "bad record"}},
	}
	if err := printCodexLiveResult(cmd, result); err != nil {
		t.Fatalf("printCodexLiveResult() error = %v", err)
	}
	output := stdout.String()
	for _, want := range []string{
		"[codex] tool read_file",
		"[codex] tool exec_command",
		"[codex] tool unknown cmd=\"go test ./...\"",
		"[codex] result call_1 status=failed exit=7",
		"[codex] result unknown status=success",
		"[codex] warning malformed_json: bad record",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, longCommand) {
		t.Fatalf("long command was not truncated: %q", output)
	}
}

func TestWatchCodexReportsMissingLogsOnce(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	err := watchCodex(context.Background(), cmd, session.Manager{}, session.State{RepoRoot: t.TempDir(), SessionID: "ar_ses_test"}, startWatchOptions{
		CodexHome: t.TempDir(),
		Interval:  1 * time.Millisecond,
		Duration:  3 * time.Millisecond,
	})
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("watchCodex() error = %v", err)
	}
	output := stdout.String()
	if got := strings.Count(output, "warning codex_logs_missing"); got != 1 {
		t.Fatalf("missing-log warning count = %d, output:\n%s", got, output)
	}
}

func TestInstallClaudeIsDeferredNoOp(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "install", "claude")
	if err != nil {
		t.Fatalf("install claude returned error: %v", err)
	}
	if !strings.Contains(stdout, "deferred in the Codex-first MVP") {
		t.Fatalf("install claude output did not explain deferred status: %q", stdout)
	}
}

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "version")
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	if got, want := strings.TrimSpace(stdout), "agentreceipt test-version"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestScaffoldCommandsPrintPlannedBehavior(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"install", "codex"},
	} {
		stdout, _, err := executeCommand(t, args...)
		if err != nil {
			t.Fatalf("%q returned error: %v", strings.Join(args, " "), err)
		}
		if !strings.Contains(stdout, scaffoldMessage) {
			t.Fatalf("%q output missing scaffold message: %q", strings.Join(args, " "), stdout)
		}
	}
}

func TestInitCommandCreatesConfigPolicyStorageAndKeys(t *testing.T) {
	repo := newCommandGitRepo(t)
	homeDir := filepath.Join(t.TempDir(), "home")
	keyDir := filepath.Join(t.TempDir(), "keys")
	t.Setenv("AGENTRECEIPT_HOME", homeDir)
	t.Setenv("AGENTRECEIPT_KEY_DIR", keyDir)

	stdout, _, err := executeCommand(t, "--repo", repo, "init")
	if err != nil {
		t.Fatalf("init returned error: %v", err)
	}
	if !strings.Contains(stdout, "Initialized global AgentReceipt storage") {
		t.Fatalf("init output = %q", stdout)
	}
	for _, path := range []string{
		homeDir,
		filepath.Join(keyDir, "default.ed25519"),
		filepath.Join(keyDir, "default.pub"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected init artifact %s: %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(repo, ".agentreceipt.yml"), filepath.Join(repo, ".agentreceipt")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("init polluted repo path %s: %v", path, err)
		}
	}
}

func TestLifecycleCommandsUsePersistedSessionState(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)

	stdout, _, err := executeCommand(t, "--repo", repo, "start")
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	if !strings.Contains(stdout, "Started AgentReceipt session ar_ses_") {
		t.Fatalf("start output = %q", stdout)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(stdout, "State: active") || !strings.Contains(stdout, "Events: 1") {
		t.Fatalf("status output did not reflect active session: %q", stdout)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "live", "--limit", "1")
	if err != nil {
		t.Fatalf("live returned error: %v", err)
	}
	if !strings.Contains(stdout, `"type":"git.snapshot"`) {
		t.Fatalf("live output missing git snapshot: %q", stdout)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "stop")
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "Finalized AgentReceipt session ar_ses_") {
		t.Fatalf("stop output = %q", stdout)
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(stdout, "Finalized AgentReceipt session "))
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	for _, path := range []string{layout.ReceiptJSON, layout.ReceiptMarkdown, layout.ReviewMarkdown, layout.ReceiptSignature} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status after stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "No active AgentReceipt session.") {
		t.Fatalf("status after stop output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "verify")
	if err != nil {
		t.Fatalf("verify returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Receipt valid.") || !strings.Contains(stdout, "Signature: valid") {
		t.Fatalf("verify output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "export", "--json")
	if err != nil {
		t.Fatalf("export json returned error: %v", err)
	}
	if !strings.Contains(stdout, `"signature_algorithm": "ed25519"`) {
		t.Fatalf("export json output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "export", "--pr")
	if err != nil {
		t.Fatalf("export pr returned error: %v", err)
	}
	if !strings.Contains(stdout, "## AgentReceipt") {
		t.Fatalf("export pr output = %q", stdout)
	}
}

func TestImportCodexJSONLCommand(t *testing.T) {
	t.Parallel()

	if _, _, err := executeCommand(t, "import", "codex-jsonl"); err == nil {
		t.Fatal("import codex-jsonl without a path returned nil error")
	}
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(tracePath, []byte(`{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	stdout, _, err := executeCommand(t, "import", "codex-jsonl", tracePath)
	if err != nil {
		t.Fatalf("import codex-jsonl returned error: %v", err)
	}
	if !strings.Contains(stdout, "events=1") || !strings.Contains(stdout, "active_session=false") {
		t.Fatalf("import output = %q", stdout)
	}
}

func TestImportCodexJSONLActiveSession(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	if _, _, err := executeCommand(t, "--repo", repo, "start"); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	trace := strings.Join([]string{
		`{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{malformed`,
	}, "\n")
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	stdout, _, err := executeCommand(t, "--repo", repo, "import", "codex-jsonl", tracePath)
	if err != nil {
		t.Fatalf("active import returned error: %v", err)
	}
	if !strings.Contains(stdout, "active_session=true") || !strings.Contains(stdout, "warnings=1") {
		t.Fatalf("active import output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(stdout, "- codex_logs: imported") {
		t.Fatalf("status did not show imported Codex logs: %q", stdout)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "review", "--last", "--json")
	if err != nil {
		t.Fatalf("review json returned error: %v", err)
	}
	if !strings.Contains(stdout, `"session_id"`) || !strings.Contains(stdout, `"risk"`) {
		t.Fatalf("review json output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "review", "--last", "--pr")
	if err != nil {
		t.Fatalf("review pr returned error: %v", err)
	}
	if !strings.Contains(stdout, "## AgentReceipt") || !strings.Contains(stdout, "Capture confidence:") {
		t.Fatalf("review pr output = %q", stdout)
	}
}

func TestStartWatchImportsMatchingCodexLog(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "17")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	tracePath := filepath.Join(sessionDir, "rollout-test.jsonl")
	trace := strings.Join([]string{
		`{"type":"session_meta","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"session_meta","cwd":"` + repo + `"}}`,
		`{"type":"response_item","timestamp":"2026-06-17T00:00:01Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{"type":"response_item","timestamp":"2026-06-17T00:00:02Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	stdout, _, err := executeCommand(t, "--repo", repo, "start", "--watch", "--codex-home", home, "--watch-existing", "--watch-interval", "1ms", "--watch-duration", "5ms")
	if err != nil {
		t.Fatalf("start --watch returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Watching Codex logs") || !strings.Contains(stdout, "[codex] tool exec_command") || !strings.Contains(stdout, `cmd="go test ./..."`) {
		t.Fatalf("watch output missing live command details: %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(stdout, "- codex_logs: imported") {
		t.Fatalf("status did not show imported Codex logs: %q", stdout)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestInspectCodexCommand(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "16")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "rollout-test.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	stdout, _, err := executeCommand(t, "inspect", "codex", "--home", home, "--last")
	if err != nil {
		t.Fatalf("inspect codex returned error: %v", err)
	}
	if !strings.Contains(stdout, "Candidates: 1") || !strings.Contains(stdout, sessionPath) {
		t.Fatalf("inspect output = %q", stdout)
	}
}

func TestInspectCodexCommandReportsMissingLogs(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "inspect", "codex", "--home", t.TempDir())
	if err != nil {
		t.Fatalf("inspect codex returned error: %v", err)
	}
	if !strings.Contains(stdout, "Candidates: 0") || !strings.Contains(stdout, "warning[codex_logs_missing]") {
		t.Fatalf("missing inspect output = %q", stdout)
	}
}

func TestMarkCommandRequiresMessage(t *testing.T) {
	t.Parallel()

	if _, _, err := executeCommand(t, "mark"); err == nil {
		t.Fatal("mark without a message returned nil error")
	}
}

func TestMarkCommandWritesSignedManualMarker(t *testing.T) {
	repo := newCommandGitRepo(t)
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	stdout, _, err := executeCommand(t, "--repo", repo, "start")
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(stdout, "Started AgentReceipt session "))

	stdout, _, err = executeCommand(t, "--repo", repo, "mark", "reviewed", "auth")
	if err != nil {
		t.Fatalf("mark returned error: %v", err)
	}
	if !strings.Contains(stdout, "reviewed auth") {
		t.Fatalf("mark output missing joined message: %q", stdout)
	}
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	last := events[len(events)-1]
	if last.Source != "manual_marker" || last.Type != "manual.marker" {
		t.Fatalf("last event is not manual marker: %+v", last)
	}
	if last.Payload["message"] != "reviewed auth" || last.Payload["signature"] == "" {
		t.Fatalf("marker payload missing message/signature: %+v", last.Payload)
	}
}

func TestMarkCommandRequiresActiveSession(t *testing.T) {
	repo := newCommandGitRepo(t)
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	if _, _, err := executeCommand(t, "--repo", repo, "mark", "reviewed"); err == nil {
		t.Fatal("mark without active session returned nil error")
	}
}

func TestPRCommentRequiresGitHubCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, _, err := executeCommand(t, "pr", "comment"); err == nil || !strings.Contains(err.Error(), "GitHub CLI gh is required") {
		t.Fatalf("pr comment error = %v", err)
	}
}

func TestPRCommentReportsMissingCurrentPR(t *testing.T) {
	repo := newCommandGitRepo(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	binDir := t.TempDir()
	ghPath := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\necho no pull request >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(gitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, _, err := executeCommand(t, "--repo", repo, "pr", "comment"); err == nil || !strings.Contains(err.Error(), "no current pull request detected") {
		t.Fatalf("pr comment error = %v", err)
	}
}

func TestPRCommentPostsGeneratedMarkdownWithGitHubCLI(t *testing.T) {
	repo := newCommandGitRepo(t)
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	if _, _, err := executeCommand(t, "--repo", repo, "start"); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")
	ghScript := `#!/bin/sh
if [ "$1 $2" = "pr view" ]; then
  echo '{"number":1}'
  exit 0
fi
if [ "$1 $2 $3 $4" = "pr comment --body-file -" ]; then
  body="$(cat)"
  test -n "$body" || exit 2
  printf '%s' "$body" | grep -q "## AgentReceipt" || exit 3
  echo commented >> "` + logPath + `"
  exit 0
fi
exit 4
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(ghScript), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(gitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	stdout, _, err := executeCommand(t, "--repo", repo, "pr", "comment")
	if err != nil {
		t.Fatalf("pr comment returned error: %v", err)
	}
	if !strings.Contains(stdout, "Posted AgentReceipt PR comment.") {
		t.Fatalf("pr comment output = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agentreceipt")); !os.IsNotExist(err) {
		t.Fatalf("pr comment polluted repo storage: %v", err)
	}
	if data, err := os.ReadFile(logPath); err != nil || !strings.Contains(string(data), "commented") {
		t.Fatalf("fake gh was not invoked data=%q err=%v", data, err)
	}
}

func executeCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := NewRootCommand("test-version")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()

	return stdout.String(), stderr.String(), err
}

func newCommandGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runCommandGit(t, repo, "init")
	runCommandGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runCommandGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runCommandGit(t, repo, "add", "README.md")
	runCommandGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runCommandGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

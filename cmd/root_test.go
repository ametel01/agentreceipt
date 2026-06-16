package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/storage"
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
	keyDir := filepath.Join(t.TempDir(), "keys")
	t.Setenv("AGENTRECEIPT_KEY_DIR", keyDir)

	stdout, _, err := executeCommand(t, "--repo", repo, "init")
	if err != nil {
		t.Fatalf("init returned error: %v", err)
	}
	if !strings.Contains(stdout, "Initialized AgentReceipt") {
		t.Fatalf("init output = %q", stdout)
	}
	for _, path := range []string{
		filepath.Join(repo, ".agentreceipt.yml"),
		filepath.Join(repo, ".agentreceipt", "policy.yml"),
		filepath.Join(keyDir, "default.ed25519"),
		filepath.Join(keyDir, "default.pub"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected init artifact %s: %v", path, err)
		}
	}
	if info, err := os.Stat(filepath.Join(repo, ".agentreceipt", "sessions")); err != nil || !info.IsDir() {
		t.Fatalf("expected sessions directory info=%v err=%v", info, err)
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
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(gitPath))
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
if [ "$1 $2 $3 $4" = "pr comment --body-file .agentreceipt/pr-comment.md" ]; then
  test -s "$4" || exit 2
  grep -q "## AgentReceipt" "$4" || exit 3
  echo commented >> "` + logPath + `"
  exit 0
fi
exit 4
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(ghScript), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(gitPath))
	stdout, _, err := executeCommand(t, "--repo", repo, "pr", "comment")
	if err != nil {
		t.Fatalf("pr comment returned error: %v", err)
	}
	if !strings.Contains(stdout, "Posted AgentReceipt PR comment.") {
		t.Fatalf("pr comment output = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(repo, prCommentFile)); !os.IsNotExist(err) {
		t.Fatalf("temporary PR comment file was not removed: %v", err)
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

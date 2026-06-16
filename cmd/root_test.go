package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		{"init"},
		{"review", "--last"},
		{"verify"},
		{"export", "--md"},
		{"inspect", "codex", "--last"},
		{"pr", "comment"},
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

func TestLifecycleCommandsUsePersistedSessionState(t *testing.T) {
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

	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status after stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "No active AgentReceipt session.") {
		t.Fatalf("status after stop output = %q", stdout)
	}
}

func TestImportCodexJSONLCommand(t *testing.T) {
	t.Parallel()

	if _, _, err := executeCommand(t, "import", "codex-jsonl"); err == nil {
		t.Fatal("import codex-jsonl without a path returned nil error")
	}
	stdout, _, err := executeCommand(t, "import", "codex-jsonl", "trace.jsonl")
	if err != nil {
		t.Fatalf("import codex-jsonl returned error: %v", err)
	}
	if !strings.Contains(stdout, "trace.jsonl") {
		t.Fatalf("import output missing path: %q", stdout)
	}
}

func TestMarkCommandRequiresMessage(t *testing.T) {
	t.Parallel()

	if _, _, err := executeCommand(t, "mark"); err == nil {
		t.Fatal("mark without a message returned nil error")
	}
	stdout, _, err := executeCommand(t, "mark", "reviewed", "auth")
	if err != nil {
		t.Fatalf("mark returned error: %v", err)
	}
	if !strings.Contains(stdout, "reviewed auth") {
		t.Fatalf("mark output missing joined message: %q", stdout)
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

package cmd

import (
	"bytes"
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

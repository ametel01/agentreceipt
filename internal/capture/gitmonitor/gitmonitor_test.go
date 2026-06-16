package gitmonitor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ametel01/agentreceipt/internal/storage"
)

func TestCaptureStartAndFinalSnapshots(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	sessionID := "ar_ses_git_test"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	monitor, err := New(context.Background(), repo, sessionID, layout)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	start, events, err := monitor.CaptureStart(context.Background())
	if err != nil {
		t.Fatalf("CaptureStart() error = %v", err)
	}
	if start.Phase != "start" || len(events) != 1 || events[0].Type != "git.snapshot" {
		t.Fatalf("unexpected start capture: snapshot=%+v events=%+v", start, events)
	}
	if _, err := os.Stat(filepath.Join(layout.Diffs, "000001.patch")); err != nil {
		t.Fatalf("start patch was not written: %v", err)
	}

	appendFile(t, filepath.Join(repo, "README.md"), "\nchanged\n")
	finalSnapshot, finalEvents, err := monitor.CaptureFinal(context.Background())
	if err != nil {
		t.Fatalf("CaptureFinal() error = %v", err)
	}
	if finalSnapshot.Phase != "final" || !finalSnapshot.Dirty || finalSnapshot.PatchHash == start.PatchHash {
		t.Fatalf("unexpected final snapshot: %+v", finalSnapshot)
	}
	if len(finalEvents) != 1 || finalEvents[0].Payload["patch_hash"] != finalSnapshot.PatchHash {
		t.Fatalf("unexpected final events: %+v", finalEvents)
	}
	if _, err := os.Stat(layout.FinalPatch); err != nil {
		t.Fatalf("final patch was not written: %v", err)
	}
}

func TestDiffMismatchDetectsChangesAfterFinalSnapshot(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	sessionID := "ar_ses_git_mismatch"
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		t.Fatalf("EnsureSessionLayout() error = %v", err)
	}
	monitor, err := New(context.Background(), repo, sessionID, layout)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	appendFile(t, filepath.Join(repo, "README.md"), "\nfirst change\n")
	finalSnapshot, _, err := monitor.CaptureFinal(context.Background())
	if err != nil {
		t.Fatalf("CaptureFinal() error = %v", err)
	}

	mismatched, err := monitor.DiffMismatched(context.Background(), finalSnapshot.PatchHash)
	if err != nil {
		t.Fatalf("DiffMismatched() before external change error = %v", err)
	}
	if mismatched {
		t.Fatal("DiffMismatched() reported mismatch before external change")
	}

	appendFile(t, filepath.Join(repo, "README.md"), "\nexternal change\n")
	mismatched, err = monitor.DiffMismatched(context.Background(), finalSnapshot.PatchHash)
	if err != nil {
		t.Fatalf("DiffMismatched() after external change error = %v", err)
	}
	if !mismatched {
		t.Fatal("DiffMismatched() did not detect external change")
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()

	entries := parseStatus(" M README.md\n?? new.txt\n")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Code != "M" || entries[0].Path != "README.md" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Code != "??" || entries[1].Path != "new.txt" {
		t.Fatalf("unexpected second entry: %+v", entries[1])
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README fixture: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func appendFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

package gitmonitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/storage"
)

const Source = "git_monitor"

type Monitor struct {
	sessionID string
	repoRoot  string
	layout    storage.Layout
}

type Snapshot struct {
	SessionID        string
	Phase            string
	RepoRoot         string
	Branch           string
	Head             string
	Dirty            bool
	Status           []StatusEntry
	StagedDiffHash   string
	UnstagedDiffHash string
	PatchPath        string
	PatchHash        string
	CapturedAt       time.Time
}

type StatusEntry struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

func New(ctx context.Context, repoPath string, sessionID string, layout storage.Layout) (*Monitor, error) {
	if err := storage.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	if repoPath == "" {
		return nil, errors.New("repo path is required")
	}
	repoRoot, err := DiscoverRoot(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	if layout.Session == "" {
		return nil, errors.New("storage layout is required")
	}

	return &Monitor{sessionID: sessionID, repoRoot: repoRoot, layout: layout}, nil
}

func DiscoverRoot(ctx context.Context, repoPath string) (string, error) {
	root, err := gitToplevel(ctx, repoPath)
	if err != nil {
		return "", err
	}
	repoRoot := strings.TrimSpace(root)
	if repoRoot == "" {
		return "", errors.New("git toplevel is empty")
	}

	return repoRoot, nil
}

func (m *Monitor) CaptureStart(ctx context.Context) (Snapshot, []model.Event, error) {
	return m.capture(ctx, "start", filepath.Join(m.layout.Diffs, "000001.patch"))
}

func (m *Monitor) CaptureFinal(ctx context.Context) (Snapshot, []model.Event, error) {
	return m.capture(ctx, "final", m.layout.FinalPatch)
}

func (m *Monitor) CurrentDiffHash(ctx context.Context) (string, error) {
	patch, err := m.combinedPatch(ctx)
	if err != nil {
		return "", err
	}

	return hashBytes([]byte(patch)), nil
}

func (m *Monitor) DiffMismatched(ctx context.Context, recordedHash string) (bool, error) {
	currentHash, err := m.CurrentDiffHash(ctx)
	if err != nil {
		return false, err
	}

	return currentHash != recordedHash, nil
}

func (m *Monitor) capture(ctx context.Context, phase string, patchPath string) (Snapshot, []model.Event, error) {
	now := time.Now().UTC()
	branch, err := m.branch(ctx)
	if err != nil {
		return Snapshot{}, nil, err
	}
	head, err := m.head(ctx)
	if err != nil {
		return Snapshot{}, nil, err
	}
	statusText, err := gitStatus(ctx, m.repoRoot)
	if err != nil {
		return Snapshot{}, nil, err
	}
	stagedDiff, err := gitStagedDiff(ctx, m.repoRoot)
	if err != nil {
		return Snapshot{}, nil, err
	}
	unstagedDiff, err := gitUnstagedDiff(ctx, m.repoRoot)
	if err != nil {
		return Snapshot{}, nil, err
	}
	patch, err := m.combinedPatch(ctx)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if err := writePatch(patchPath, []byte(patch)); err != nil {
		return Snapshot{}, nil, err
	}
	snapshot := Snapshot{
		SessionID:        m.sessionID,
		Phase:            phase,
		RepoRoot:         m.repoRoot,
		Branch:           branch,
		Head:             head,
		Dirty:            strings.TrimSpace(statusText) != "",
		Status:           parseStatus(statusText),
		StagedDiffHash:   hashBytes([]byte(stagedDiff)),
		UnstagedDiffHash: hashBytes([]byte(unstagedDiff)),
		PatchPath:        patchPath,
		PatchHash:        hashBytes([]byte(patch)),
		CapturedAt:       now,
	}

	return snapshot, []model.Event{snapshotEvent(snapshot)}, nil
}

func (m *Monitor) branch(ctx context.Context) (string, error) {
	branch, err := gitBranch(ctx, m.repoRoot)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(branch), nil
}

func (m *Monitor) head(ctx context.Context) (string, error) {
	head, err := gitHead(ctx, m.repoRoot)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(head), nil
}

func (m *Monitor) combinedPatch(ctx context.Context) (string, error) {
	patch, err := gitHeadDiff(ctx, m.repoRoot)
	if err != nil {
		return "", err
	}

	return patch, nil
}

func snapshotEvent(snapshot Snapshot) model.Event {
	return model.Event{
		EventID:   "evt_git_" + snapshot.Phase + "_" + strconv.FormatInt(snapshot.CapturedAt.UnixNano(), 36),
		SessionID: snapshot.SessionID,
		Timestamp: snapshot.CapturedAt,
		Source:    Source,
		Type:      "git.snapshot",
		CWD:       snapshot.RepoRoot,
		Payload: map[string]any{
			"phase":              snapshot.Phase,
			"repo_root":          snapshot.RepoRoot,
			"branch":             snapshot.Branch,
			"head":               snapshot.Head,
			"dirty":              snapshot.Dirty,
			"status":             snapshot.Status,
			"staged_diff_hash":   snapshot.StagedDiffHash,
			"unstaged_diff_hash": snapshot.UnstagedDiffHash,
			"patch_path":         snapshot.PatchPath,
			"patch_hash":         snapshot.PatchHash,
		},
	}
}

func parseStatus(status string) []StatusEntry {
	lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
	entries := make([]StatusEntry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		code := strings.TrimSpace(line[:min(2, len(line))])
		path := ""
		if len(line) > 3 {
			path = line[3:]
		}
		entries = append(entries, StatusEntry{Code: code, Path: path})
	}

	return entries
}

func writePatch(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create diff directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open diff root: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	if err := root.WriteFile(filepath.Base(path), data, 0o600); err != nil {
		return fmt.Errorf("write diff patch: %w", err)
	}

	return nil
}

func gitToplevel(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir

	return commandOutput(cmd, "git rev-parse --show-toplevel")
}

func gitBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir

	return commandOutput(cmd, "git rev-parse --abbrev-ref HEAD")
}

func gitHead(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir

	return commandOutput(cmd, "git rev-parse HEAD")
}

func gitStatus(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	cmd.Dir = dir

	return commandOutput(cmd, "git status --porcelain=v1")
}

func gitStagedDiff(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--binary")
	cmd.Dir = dir

	return commandOutput(cmd, "git diff --cached --binary")
}

func gitUnstagedDiff(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--binary")
	cmd.Dir = dir

	return commandOutput(cmd, "git diff --binary")
}

func gitHeadDiff(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--binary", "HEAD")
	cmd.Dir = dir

	return commandOutput(cmd, "git diff --binary HEAD")
}

func commandOutput(cmd *exec.Cmd, description string) (string, error) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", description, err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)

	return "sha256:" + hex.EncodeToString(sum[:])
}

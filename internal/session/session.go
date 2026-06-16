package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/fswatcher"
	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/storage"
)

type Manager struct {
	RepoPath string
	Config   config.Config
	Now      func() time.Time
}

type State struct {
	SchemaVersion  int                `json:"schema_version"`
	SessionID      string             `json:"session_id"`
	RepoRoot       string             `json:"repo_root"`
	State          model.SessionState `json:"state"`
	PID            int                `json:"pid"`
	StartedAt      time.Time          `json:"started_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	EventCount     int64              `json:"event_count"`
	ChainHash      string             `json:"chain_hash"`
	FinalDiffHash  string             `json:"final_diff_hash,omitempty"`
	CaptureSources CaptureSources     `json:"capture_sources"`
	RiskSummary    RiskSummary        `json:"risk_summary"`
	Warnings       []model.Warning    `json:"warnings,omitempty"`
}

type CaptureSources struct {
	Git        string `json:"git"`
	Filesystem string `json:"filesystem"`
	CodexLogs  string `json:"codex_logs"`
}

type RiskSummary struct {
	Level   model.RiskLevel `json:"level"`
	Reasons []string        `json:"reasons"`
}

func (m Manager) Start(ctx context.Context) (State, error) {
	repoRoot, err := gitmonitor.DiscoverRoot(ctx, repoPathOrCWD(m.RepoPath))
	if err != nil {
		return State{}, err
	}
	if active, ok, err := m.activeSession(repoRoot); err != nil {
		return State{}, err
	} else if ok {
		return State{}, fmt.Errorf("active session already exists: %s", active.SessionID)
	}
	sessionID, err := NewID()
	if err != nil {
		return State{}, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return State{}, err
	}
	if err := storage.EnsureSessionLayout(layout); err != nil {
		return State{}, err
	}
	now := m.now()
	manifest := model.NewManifest(sessionID, now, storage.ManifestArtifacts(layout))
	manifest.State = model.SessionStateActive

	writer, err := eventlog.NewWriter(layout.EventsJSONL, "", 1)
	if err != nil {
		return State{}, err
	}
	defer func() {
		_ = writer.Close()
	}()
	monitor, err := gitmonitor.New(ctx, repoRoot, sessionID, layout)
	if err != nil {
		return State{}, err
	}
	fsWatcher, err := BuildFilesystemWatcher(repoRoot, sessionID, m.Config)
	if err != nil {
		return State{}, err
	}
	if err := fsWatcher.Close(); err != nil {
		return State{}, err
	}
	_, gitEvents, err := monitor.CaptureStart(ctx)
	if err != nil {
		return State{}, err
	}
	var chainHash string
	var eventCount int64
	for _, event := range gitEvents {
		appended, err := writer.Append(event)
		if err != nil {
			return State{}, err
		}
		chainHash = appended.EventHash
		eventCount = appended.Seq
	}
	state := State{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		RepoRoot:      repoRoot,
		State:         model.SessionStateActive,
		PID:           os.Getpid(),
		StartedAt:     now,
		UpdatedAt:     now,
		EventCount:    eventCount,
		ChainHash:     chainHash,
		CaptureSources: CaptureSources{
			Git:        "active",
			Filesystem: "ready",
			CodexLogs:  "not_observed",
		},
		RiskSummary: RiskSummary{Level: model.RiskInfo},
	}
	manifest.EventCount = eventCount
	if err := writeManifest(layout, manifest); err != nil {
		return State{}, err
	}
	if err := writeState(layout, state); err != nil {
		return State{}, err
	}
	if err := writeActiveSession(repoRoot, sessionID); err != nil {
		return State{}, err
	}

	return state, nil
}

func (m Manager) Status(ctx context.Context) (State, bool, error) {
	repoRoot, err := gitmonitor.DiscoverRoot(ctx, repoPathOrCWD(m.RepoPath))
	if err != nil {
		return State{}, false, err
	}

	return m.activeSession(repoRoot)
}

func (m Manager) Live(ctx context.Context, limit int) ([]model.Event, error) {
	state, ok, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	layout, err := storage.NewLayout(state.RepoRoot, state.SessionID)
	if err != nil {
		return nil, err
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit >= len(events) {
		return events, nil
	}

	return events[len(events)-limit:], nil
}

func (m Manager) Stop(ctx context.Context) (State, bool, error) {
	state, ok, err := m.Status(ctx)
	if err != nil {
		return State{}, false, err
	}
	if !ok {
		return State{}, false, nil
	}
	layout, err := storage.NewLayout(state.RepoRoot, state.SessionID)
	if err != nil {
		return State{}, false, err
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		return State{}, false, err
	}
	prevHash, err := eventlog.Replay(events)
	if err != nil {
		return State{}, false, err
	}
	writer, err := eventlog.NewWriter(layout.EventsJSONL, prevHash, int64(len(events)+1))
	if err != nil {
		return State{}, false, err
	}
	defer func() {
		_ = writer.Close()
	}()
	monitor, err := gitmonitor.New(ctx, state.RepoRoot, state.SessionID, layout)
	if err != nil {
		return State{}, false, err
	}
	finalSnapshot, gitEvents, err := monitor.CaptureFinal(ctx)
	if err != nil {
		return State{}, false, err
	}
	var appended model.Event
	for _, event := range gitEvents {
		appended, err = writer.Append(event)
		if err != nil {
			return State{}, false, err
		}
	}
	warning := model.Warning{
		Code:    "codex_events_missing",
		Message: "No Codex provider events were observed; provider evidence remains unavailable for this session.",
	}
	finalize := model.Event{
		EventID:   fmt.Sprintf("evt_finalize_%d", m.now().UnixNano()),
		SessionID: state.SessionID,
		Timestamp: m.now(),
		Source:    "receipt_finalizer",
		Type:      "receipt.finalize",
		CWD:       state.RepoRoot,
		Payload: map[string]any{
			"final_diff_hash":      finalSnapshot.PatchHash,
			"codex_events_present": false,
			"warning":              warning,
		},
	}
	appended, err = writer.Append(finalize)
	if err != nil {
		return State{}, false, err
	}
	now := m.now()
	state.State = model.SessionStateFinalized
	state.UpdatedAt = now
	state.EventCount = appended.Seq
	state.ChainHash = appended.EventHash
	state.FinalDiffHash = finalSnapshot.PatchHash
	state.CaptureSources.Git = "finalized"
	state.CaptureSources.Filesystem = "stopped"
	state.CaptureSources.CodexLogs = "missing"
	state.RiskSummary = RiskSummary{
		Level:   model.RiskLow,
		Reasons: []string{"No Codex provider events were observed."},
	}
	state.Warnings = appendWarning(state.Warnings, warning)
	manifest := model.NewManifest(state.SessionID, state.StartedAt, storage.ManifestArtifacts(layout))
	manifest.State = model.SessionStateFinalized
	manifest.UpdatedAt = now
	manifest.EventCount = state.EventCount
	manifest.Warnings = state.Warnings
	if err := writeManifest(layout, manifest); err != nil {
		return State{}, false, err
	}
	if err := writeState(layout, state); err != nil {
		return State{}, false, err
	}
	if err := clearActiveSession(state.RepoRoot); err != nil {
		return State{}, false, err
	}

	return state, true, nil
}

func (m Manager) activeSession(repoRoot string) (State, bool, error) {
	sessionID, ok, err := readActiveSession(repoRoot)
	if err != nil || !ok {
		return State{}, ok, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return State{}, false, err
	}
	state, err := readState(layout)
	if errors.Is(err, os.ErrNotExist) {
		_ = clearActiveSession(repoRoot)
		return State{}, false, nil
	}

	return state, err == nil, err
}

func NewID() (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}

	return fmt.Sprintf("ar_ses_%d_%s", time.Now().UTC().UnixNano(), hex.EncodeToString(random[:])), nil
}

func FormatStatus(state State) string {
	reasons := append([]string(nil), state.RiskSummary.Reasons...)
	sort.Strings(reasons)
	if len(reasons) == 0 {
		reasons = []string{"none"}
	}

	return fmt.Sprintf(`Session: %s
State: %s
Events: %d
Risk: %s
Capture:
- git: %s
- filesystem: %s
- codex_logs: %s
Warnings: %d
Reasons: %s
`, state.SessionID, state.State, state.EventCount, state.RiskSummary.Level, state.CaptureSources.Git, state.CaptureSources.Filesystem, state.CaptureSources.CodexLogs, len(state.Warnings), strings.Join(reasons, ", "))
}

func writeManifest(layout storage.Layout, manifest model.Manifest) error {
	return writeSessionJSON(layout.Session, storage.ManifestFile, manifest)
}

func writeState(layout storage.Layout, state State) error {
	return writeSessionJSON(layout.Session, storage.StateFile, state)
}

func readState(layout storage.Layout) (State, error) {
	root, err := os.OpenRoot(layout.Session)
	if err != nil {
		return State{}, err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := root.ReadFile(storage.StateFile)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode session state: %w", err)
	}

	return state, nil
}

func writeSessionJSON(rootPath string, name string, value any) error {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	data = append(data, '\n')
	if err := root.WriteFile(name, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}

func writeActiveSession(repoRoot string, sessionID string) error {
	rootDir := filepath.Join(repoRoot, storage.RootDir)
	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()

	return root.WriteFile(storage.ActiveSessionFile, []byte(sessionID+"\n"), 0o600)
}

func readActiveSession(repoRoot string) (string, bool, error) {
	root, err := os.OpenRoot(filepath.Join(repoRoot, storage.RootDir))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := root.ReadFile(storage.ActiveSessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	sessionID := strings.TrimSpace(string(data))
	if sessionID == "" {
		return "", false, nil
	}
	if err := storage.ValidateSessionID(sessionID); err != nil {
		return "", false, err
	}

	return sessionID, true, nil
}

func clearActiveSession(repoRoot string) error {
	root, err := os.OpenRoot(filepath.Join(repoRoot, storage.RootDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()
	err = root.Remove(storage.ActiveSessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}

func appendWarning(warnings []model.Warning, warning model.Warning) []model.Warning {
	for _, existing := range warnings {
		if existing.Code == warning.Code {
			return warnings
		}
	}

	return append(warnings, warning)
}

func repoPathOrCWD(path string) string {
	if path != "" {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	return cwd
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}

	return time.Now().UTC()
}

func BuildFilesystemWatcher(repoRoot string, sessionID string, cfg config.Config) (*fswatcher.Watcher, error) {
	return fswatcher.New(repoRoot, sessionID, fswatcher.NewClassifier(cfg), 50*time.Millisecond)
}

package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/fswatcher"
	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
	"github.com/ametel01/agentreceipt/internal/signing"
	"github.com/ametel01/agentreceipt/internal/storage"
)

type Manager struct {
	RepoPath string
	Config   config.Config
	Now      func() time.Time
}

type State struct {
	SchemaVersion          int                `json:"schema_version"`
	SessionID              string             `json:"session_id"`
	RepoRoot               string             `json:"repo_root"`
	State                  model.SessionState `json:"state"`
	PID                    int                `json:"pid"`
	FilesystemWatcherPID   int                `json:"filesystem_watcher_pid,omitempty"`
	FilesystemWatcherNonce string             `json:"filesystem_watcher_nonce,omitempty"`
	StartedAt              time.Time          `json:"started_at"`
	UpdatedAt              time.Time          `json:"updated_at"`
	EventCount             int64              `json:"event_count"`
	ChainHash              string             `json:"chain_hash"`
	FinalDiffHash          string             `json:"final_diff_hash,omitempty"`
	CaptureSources         CaptureSources     `json:"capture_sources"`
	RiskSummary            RiskSummary        `json:"risk_summary"`
	Warnings               []model.Warning    `json:"warnings,omitempty"`
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

type SessionSummary struct {
	SessionID  string             `json:"session_id"`
	State      model.SessionState `json:"state"`
	Active     bool               `json:"active"`
	StartedAt  time.Time          `json:"started_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
	EventCount int64              `json:"event_count"`
	Warnings   int                `json:"warnings"`
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
	appendResult, err := eventlog.AppendBatch(layout.EventsJSONL, gitEvents)
	if err != nil {
		return State{}, err
	}
	state := State{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		RepoRoot:      repoRoot,
		State:         model.SessionStateActive,
		PID:           os.Getpid(),
		StartedAt:     now,
		UpdatedAt:     now,
		EventCount:    appendResult.EventCount,
		ChainHash:     appendResult.ChainHash,
		CaptureSources: CaptureSources{
			Git:        "active",
			Filesystem: "starting",
			CodexLogs:  "not_observed",
		},
		RiskSummary: RiskSummary{Level: model.RiskInfo},
	}
	manifest.EventCount = appendResult.EventCount
	if err := writeManifest(layout, manifest); err != nil {
		return State{}, err
	}
	if err := writeState(layout, state); err != nil {
		return State{}, err
	}
	if err := writeActiveSession(repoRoot, sessionID); err != nil {
		return State{}, err
	}
	watcherPID, watcherNonce, err := startFilesystemWatcher(ctx, state, layout, m.Config)
	if err != nil {
		_ = clearActiveSession(repoRoot)
		return State{}, err
	}
	state.FilesystemWatcherPID = watcherPID
	state.FilesystemWatcherNonce = watcherNonce
	state.CaptureSources.Filesystem = "active"
	state.UpdatedAt = m.now()
	manifest.UpdatedAt = state.UpdatedAt
	if err := writeManifest(layout, manifest); err != nil {
		return State{}, err
	}
	if err := writeState(layout, state); err != nil {
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

func (m Manager) List(ctx context.Context) ([]SessionSummary, error) {
	repoRoot, err := gitmonitor.DiscoverRoot(ctx, repoPathOrCWD(m.RepoPath))
	if err != nil {
		return nil, err
	}
	sessionsPath, err := storage.SessionsPath(repoRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(sessionsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	activeID, active, err := readActiveSession(repoRoot)
	if err != nil {
		return nil, err
	}
	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		if err := storage.ValidateSessionID(sessionID); err != nil {
			continue
		}
		layout, err := storage.NewLayout(repoRoot, sessionID)
		if err != nil {
			return nil, err
		}
		state, err := readState(layout)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, SessionSummary{
			SessionID:  state.SessionID,
			State:      state.State,
			Active:     active && state.SessionID == activeID,
			StartedAt:  state.StartedAt,
			UpdatedAt:  state.UpdatedAt,
			EventCount: state.EventCount,
			Warnings:   len(state.Warnings),
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].SessionID > summaries[j].SessionID
		}

		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
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
	if err := stopFilesystemWatcher(ctx, state, layout); err != nil {
		return State{}, false, err
	}
	if _, err := eventlog.AppendTransaction(layout.EventsJSONL, func(tx *eventlog.AppendTx) error {
		currentState, err := readState(layout)
		if err != nil {
			return err
		}
		if currentState.State != model.SessionStateActive {
			return fmt.Errorf("session is not active: %s", currentState.State)
		}
		state = currentState
		events := tx.Snapshot().Events
		providerPresent := providerEventsPresent(events)
		codexPresent := codexEventsPresent(events)
		monitor, err := gitmonitor.New(ctx, state.RepoRoot, state.SessionID, layout)
		if err != nil {
			return err
		}
		finalSnapshot, gitEvents, err := monitor.CaptureFinal(ctx)
		if err != nil {
			return err
		}
		finalize := model.Event{
			EventID:   fmt.Sprintf("evt_finalize_%d", m.now().UnixNano()),
			SessionID: state.SessionID,
			Timestamp: m.now(),
			Source:    "receipt_finalizer",
			Type:      "receipt.finalize",
			CWD:       state.RepoRoot,
			Payload: map[string]any{
				"provider_events_present": providerPresent,
				"final_diff_hash":         finalSnapshot.PatchHash,
				"codex_events_present":    codexPresent,
			},
		}
		eventsToAppend := append(append([]model.Event(nil), gitEvents...), finalize)
		appendResult, err := tx.AppendAll(eventsToAppend)
		if err != nil {
			return err
		}
		now := m.now()
		state.State = model.SessionStateFinalized
		state.UpdatedAt = now
		state.EventCount = appendResult.EventCount
		state.ChainHash = appendResult.ChainHash
		state.FinalDiffHash = finalSnapshot.PatchHash
		state.CaptureSources.Git = "finalized"
		state.CaptureSources.Filesystem = "stopped"
		state.CaptureSources.CodexLogs = "imported"
		state.RiskSummary = RiskSummary{Level: model.RiskInfo}
		if !providerPresent {
			warning := model.Warning{
				Code:    "codex_events_missing",
				Message: "No provider tool events were observed; provider evidence remains unavailable for this session.",
			}
			state.CaptureSources.CodexLogs = "missing"
			state.RiskSummary = RiskSummary{
				Level:   model.RiskLow,
				Reasons: []string{"No provider tool events were observed."},
			}
			state.Warnings = appendWarning(state.Warnings, warning)
		}
		manifest := model.NewManifest(state.SessionID, state.StartedAt, storage.ManifestArtifacts(layout))
		manifest.State = model.SessionStateFinalized
		manifest.UpdatedAt = now
		manifest.EventCount = state.EventCount
		manifest.Warnings = state.Warnings
		if err := writeManifest(layout, manifest); err != nil {
			return err
		}
		if err := writeState(layout, state); err != nil {
			return err
		}

		return clearActiveSession(state.RepoRoot)
	}); err != nil {
		return State{}, false, err
	}

	return state, true, nil
}

func (m Manager) AppendProviderEvents(ctx context.Context, providerEvents []model.Event, warnings []model.Warning) (State, bool, error) {
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
	if _, err := eventlog.AppendTransaction(layout.EventsJSONL, func(tx *eventlog.AppendTx) error {
		currentState, err := readState(layout)
		if err != nil {
			return err
		}
		if currentState.State != model.SessionStateActive {
			return fmt.Errorf("session is not active: %s", currentState.State)
		}
		state = currentState
		normalizedProviderEvents := make([]model.Event, 0, len(providerEvents))
		for _, providerEvent := range providerEvents {
			normalized := providerEvent
			normalized.SessionID = state.SessionID
			if normalized.CWD == "" {
				normalized.CWD = state.RepoRoot
			}
			normalizedProviderEvents = append(normalizedProviderEvents, normalized)
		}
		appendResult, err := tx.AppendAll(normalizedProviderEvents)
		if err != nil {
			return err
		}
		now := m.now()
		state.UpdatedAt = now
		state.EventCount = appendResult.EventCount
		state.ChainHash = appendResult.ChainHash
		if providerEventsPresent(normalizedProviderEvents) {
			state.CaptureSources.CodexLogs = "imported"
		}
		for _, warning := range warnings {
			state.Warnings = appendWarning(state.Warnings, warning)
		}
		manifest := model.NewManifest(state.SessionID, state.StartedAt, storage.ManifestArtifacts(layout))
		manifest.State = state.State
		manifest.UpdatedAt = now
		manifest.EventCount = state.EventCount
		manifest.Warnings = state.Warnings
		if err := writeManifest(layout, manifest); err != nil {
			return err
		}

		return writeState(layout, state)
	}); err != nil {
		return State{}, false, err
	}

	return state, true, nil
}

func (m Manager) Mark(ctx context.Context, message string, keyDir string) (State, bool, error) {
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
	keypair, err := signing.LoadOrCreateDefault(keyDir)
	if err != nil {
		return State{}, false, err
	}
	now := m.now()
	payloadForSignature := struct {
		SessionID string    `json:"session_id"`
		RepoRoot  string    `json:"repo_root"`
		Message   string    `json:"message"`
		Timestamp time.Time `json:"timestamp"`
	}{
		SessionID: state.SessionID,
		RepoRoot:  state.RepoRoot,
		Message:   message,
		Timestamp: now,
	}
	signaturePayload, err := model.MarshalCanonical(payloadForSignature)
	if err != nil {
		return State{}, false, err
	}
	marker := model.Event{
		EventID:   fmt.Sprintf("evt_manual_%d", now.UnixNano()),
		SessionID: state.SessionID,
		Timestamp: now,
		Source:    "manual_marker",
		Type:      "manual.marker",
		CWD:       state.RepoRoot,
		Payload: map[string]any{
			"message":             message,
			"signature_algorithm": "ed25519",
			"signature":           signing.Sign(keypair.PrivateKey, signaturePayload),
			"public_key":          keypair.Public,
		},
	}
	if _, err := eventlog.AppendTransaction(layout.EventsJSONL, func(tx *eventlog.AppendTx) error {
		currentState, err := readState(layout)
		if err != nil {
			return err
		}
		if currentState.State != model.SessionStateActive {
			return fmt.Errorf("session is not active: %s", currentState.State)
		}
		state = currentState
		appendResult, err := tx.AppendAll([]model.Event{marker})
		if err != nil {
			return err
		}
		state.UpdatedAt = now
		state.EventCount = appendResult.EventCount
		state.ChainHash = appendResult.ChainHash
		manifest := model.NewManifest(state.SessionID, state.StartedAt, storage.ManifestArtifacts(layout))
		manifest.State = state.State
		manifest.UpdatedAt = now
		manifest.EventCount = state.EventCount
		manifest.Warnings = state.Warnings
		if err := writeManifest(layout, manifest); err != nil {
			return err
		}

		return writeState(layout, state)
	}); err != nil {
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
	rootDir, err := storage.RepositoryPath(repoRoot)
	if err != nil {
		return err
	}
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
	rootDir, err := storage.RepositoryPath(repoRoot)
	if err != nil {
		return "", false, err
	}
	root, err := os.OpenRoot(rootDir)
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
	rootDir, err := storage.RepositoryPath(repoRoot)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
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

func codexEventsPresent(events []model.Event) bool {
	for _, event := range events {
		if isCodexProviderEvidenceEvent(event) {
			return true
		}
	}

	return false
}

func providerEventsPresent(events []model.Event) bool {
	for _, event := range events {
		if isProviderEvidenceEvent(event) {
			return true
		}
	}

	return false
}

func isCodexProviderEvidenceEvent(event model.Event) bool {
	if event.Provider != providerevidence.ProviderCodex && event.Source != providerevidence.SourceCodex {
		return false
	}

	return providerevidence.IsToolEvidenceEvent(event)
}

func isProviderEvidenceEvent(event model.Event) bool {
	return providerevidence.IsProviderEvidenceSource(event) && providerevidence.IsToolEvidenceEvent(event)
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

type FilesystemWatcherOptions struct {
	RepoRoot  string
	SessionID string
	Config    config.Config
	Nonce     string
}

type filesystemWatcherIdentity struct {
	SessionID string `json:"session_id"`
	Nonce     string `json:"nonce"`
	PID       int    `json:"pid"`
}

type inProcessWatcher struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var inProcessWatchers sync.Map

type filesystemWatcherProcess interface {
	Signal(os.Signal) error
	Kill() error
}

var findFilesystemWatcherProcess = func(pid int) (filesystemWatcherProcess, error) {
	return os.FindProcess(pid)
}

var filesystemWatcherFallbackDelay = 100 * time.Millisecond
var filesystemWatcherStopAckTimeout = 2 * time.Second
var filesystemWatcherStopPollInterval = 25 * time.Millisecond

func RunFilesystemWatcher(ctx context.Context, options FilesystemWatcherOptions) error {
	if err := storage.ValidateSessionID(options.SessionID); err != nil {
		return err
	}
	if !validWatcherNonce(options.Nonce) {
		return errors.New("filesystem watcher nonce is required")
	}
	layout, err := storage.NewLayout(options.RepoRoot, options.SessionID)
	if err != nil {
		return err
	}
	runCtx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()
	runCtx, stopPolling := context.WithCancel(runCtx)
	defer stopPolling()
	go cancelWhenStopFileAppears(runCtx, cancel, layout.FilesystemWatcherStopPath)
	if err := os.Remove(layout.FilesystemWatcherDonePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove watcher done marker: %w", err)
	}
	if err := os.Remove(layout.FilesystemWatcherStopPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove watcher stop marker: %w", err)
	}
	defer func() {
		_ = os.Remove(layout.FilesystemWatcherPIDPath)
		_ = os.Remove(filesystemWatcherIdentityPath(layout))
		_ = os.WriteFile(layout.FilesystemWatcherDonePath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
	}()
	watcher, err := BuildFilesystemWatcher(options.RepoRoot, options.SessionID, options.Config)
	if err != nil {
		return err
	}
	defer func() {
		_ = watcher.Close()
	}()
	if err := watcher.Start(runCtx); err != nil {
		return err
	}
	if err := writeFilesystemWatcherIdentity(layout, filesystemWatcherIdentity{
		SessionID: options.SessionID,
		Nonce:     options.Nonce,
		PID:       os.Getpid(),
	}); err != nil {
		return err
	}
	if err := os.WriteFile(layout.FilesystemWatcherPIDPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("write filesystem watcher pid: %w", err)
	}
	for event := range watcher.Events() {
		if err := appendFilesystemEvent(layout, event); err != nil {
			return err
		}
	}

	return nil
}

func startFilesystemWatcher(ctx context.Context, state State, layout storage.Layout, cfg config.Config) (int, string, error) {
	if !cfg.Capture.Filesystem {
		return 0, "", errors.New("filesystem capture is disabled")
	}
	nonce, err := newWatcherNonce()
	if err != nil {
		return 0, "", err
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		runCtx, cancel := context.WithCancel(context.Background())
		handle := inProcessWatcher{cancel: cancel, done: make(chan struct{})}
		key := filesystemWatcherKey(layout)
		inProcessWatchers.Store(key, handle)
		go func() {
			defer close(handle.done)
			defer inProcessWatchers.Delete(key)
			_ = RunFilesystemWatcher(runCtx, FilesystemWatcherOptions{
				RepoRoot:  state.RepoRoot,
				SessionID: state.SessionID,
				Config:    cfg,
				Nonce:     nonce,
			})
		}()

		return 0, nonce, waitForFilesystemWatcherReady(ctx, layout)
	}
	executable, err := os.Executable()
	if err != nil {
		return 0, "", fmt.Errorf("resolve current executable: %w", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return 0, "", fmt.Errorf("marshal filesystem watcher config: %w", err)
	}
	// #nosec G204 -- launches this AgentReceipt executable with validated session args for the local watcher sidecar.
	command := exec.CommandContext(ctx, executable,
		"--repo", state.RepoRoot,
		"__internal-fswatcher",
		"--session", state.SessionID,
		"--watcher-nonce", nonce,
		"--config-json", string(configJSON),
	)
	if err := command.Start(); err != nil {
		return 0, "", fmt.Errorf("start filesystem watcher: %w", err)
	}
	if err := waitForFilesystemWatcherReady(ctx, layout); err != nil {
		return 0, "", err
	}

	return command.Process.Pid, nonce, nil
}

func stopFilesystemWatcher(ctx context.Context, state State, layout storage.Layout) error {
	if handle, ok := inProcessWatchers.Load(filesystemWatcherKey(layout)); ok {
		watcher := handle.(inProcessWatcher)
		watcher.cancel()
		select {
		case <-watcher.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("timed out stopping filesystem watcher")
		}
	}
	if state.CaptureSources.Filesystem != "active" && state.FilesystemWatcherPID == 0 {
		return nil
	}
	if err := os.WriteFile(layout.FilesystemWatcherStopPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write filesystem watcher stop marker: %w", err)
	}
	deadline := time.Now().Add(filesystemWatcherStopAckTimeout)
	for {
		if _, err := os.Stat(layout.FilesystemWatcherDonePath); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(filesystemWatcherStopPollInterval):
		}
	}
	if state.FilesystemWatcherPID <= 0 {
		return errors.New("filesystem watcher did not acknowledge stop")
	}
	if err := validateFilesystemWatcherIdentity(layout, filesystemWatcherIdentity{
		SessionID: state.SessionID,
		Nonce:     state.FilesystemWatcherNonce,
		PID:       state.FilesystemWatcherPID,
	}); err != nil {
		return err
	}
	process, err := findFilesystemWatcherProcess(state.FilesystemWatcherPID)
	if err != nil {
		return fmt.Errorf("find filesystem watcher process: %w", err)
	}
	_ = process.Signal(os.Interrupt)
	time.Sleep(filesystemWatcherFallbackDelay)
	if _, err := os.Stat(layout.FilesystemWatcherDonePath); err == nil {
		return nil
	}
	_ = process.Kill()

	return errors.New("filesystem watcher did not stop cleanly")
}

func appendFilesystemEvent(layout storage.Layout, event model.Event) error {
	_, err := eventlog.AppendTransaction(layout.EventsJSONL, func(tx *eventlog.AppendTx) error {
		appendResult, err := tx.AppendAll([]model.Event{event})
		if err != nil {
			return err
		}
		state, err := readState(layout)
		if err != nil {
			return err
		}
		state.EventCount = appendResult.EventCount
		state.ChainHash = appendResult.ChainHash
		state.UpdatedAt = appendResult.LastEvent.Timestamp
		state.CaptureSources.Filesystem = "active"
		manifest := model.NewManifest(state.SessionID, state.StartedAt, storage.ManifestArtifacts(layout))
		manifest.State = state.State
		manifest.UpdatedAt = state.UpdatedAt
		manifest.EventCount = state.EventCount
		manifest.Warnings = state.Warnings
		if err := writeManifest(layout, manifest); err != nil {
			return err
		}

		return writeState(layout, state)
	})

	return err
}

func cancelWhenStopFileAppears(ctx context.Context, cancel context.CancelFunc, path string) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				cancel()
				return
			}
		}
	}
}

func filesystemWatcherKey(layout storage.Layout) string {
	return filepath.Clean(layout.Session)
}

func filesystemWatcherIdentityPath(layout storage.Layout) string {
	return filepath.Join(layout.Session, "fswatcher.identity.json")
}

func newWatcherNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate filesystem watcher nonce: %w", err)
	}

	return hex.EncodeToString(raw[:]), nil
}

func validWatcherNonce(nonce string) bool {
	if len(nonce) != 32 {
		return false
	}
	_, err := hex.DecodeString(nonce)

	return err == nil
}

func writeFilesystemWatcherIdentity(layout storage.Layout, identity filesystemWatcherIdentity) error {
	if !validWatcherNonce(identity.Nonce) {
		return errors.New("filesystem watcher nonce is required")
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("marshal filesystem watcher identity: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(filesystemWatcherIdentityPath(layout), data, 0o600)
}

func readFilesystemWatcherIdentity(layout storage.Layout) (filesystemWatcherIdentity, error) {
	data, err := os.ReadFile(filesystemWatcherIdentityPath(layout))
	if err != nil {
		return filesystemWatcherIdentity{}, fmt.Errorf("read filesystem watcher identity: %w", err)
	}
	var identity filesystemWatcherIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return filesystemWatcherIdentity{}, fmt.Errorf("decode filesystem watcher identity: %w", err)
	}

	return identity, nil
}

func validateFilesystemWatcherIdentity(layout storage.Layout, expected filesystemWatcherIdentity) error {
	if !validWatcherNonce(expected.Nonce) {
		return errors.New("filesystem watcher identity cannot be verified: missing watcher nonce")
	}
	actual, err := readFilesystemWatcherIdentity(layout)
	if err != nil {
		return fmt.Errorf("filesystem watcher identity cannot be verified: %w", err)
	}
	if actual.SessionID != expected.SessionID || actual.Nonce != expected.Nonce || actual.PID != expected.PID {
		return errors.New("filesystem watcher identity mismatch; refusing to signal recorded PID")
	}

	return nil
}

func waitForFilesystemWatcherReady(ctx context.Context, layout storage.Layout) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(layout.FilesystemWatcherPIDPath); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for filesystem watcher startup")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func BuildFilesystemWatcher(repoRoot string, sessionID string, cfg config.Config) (*fswatcher.Watcher, error) {
	return fswatcher.New(repoRoot, sessionID, fswatcher.NewClassifier(cfg), 50*time.Millisecond)
}

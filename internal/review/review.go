package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
)

type Options struct {
	RepoPath  string
	SessionID string
	Last      bool
	Security  bool
	Diff      bool
}

type Report struct {
	SchemaVersion int                     `json:"schema_version"`
	SessionID     string                  `json:"session_id"`
	GeneratedAt   time.Time               `json:"generated_at"`
	State         model.SessionState      `json:"state"`
	Provider      string                  `json:"provider"`
	Summary       model.Summary           `json:"summary"`
	Confidence    model.CaptureConfidence `json:"confidence"`
	Risk          model.Risk              `json:"risk"`
	Verification  model.Verification      `json:"verification"`
	Focus         []string                `json:"focus"`
	Gaps          []string                `json:"gaps"`
	Warnings      []model.Warning         `json:"warnings,omitempty"`
	Timeline      []TimelineItem          `json:"timeline"`
	EventsByType  map[string]int          `json:"events_by_type"`
}

type TimelineItem struct {
	Seq    int64  `json:"seq"`
	Time   string `json:"time"`
	Source string `json:"source"`
	Type   string `json:"type"`
}

var commandKindPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{kind: "test", pattern: regexp.MustCompile(`\b(go test|npm test|npm run test|pnpm test|yarn test|pytest|cargo test|make test)\b`)},
	{kind: "lint", pattern: regexp.MustCompile(`\b(lint|golangci-lint|staticcheck|go vet)\b`)},
	{kind: "typecheck", pattern: regexp.MustCompile(`\b(typecheck|tsc --noEmit|pyright)\b`)},
	{kind: "network", pattern: regexp.MustCompile(`\b(curl|wget|ssh|nc|aws|gcloud)\b`)},
	{kind: "destructive", pattern: regexp.MustCompile(`\b(rm|dd|mkfs|shutdown|reboot)\b`)},
}

func Build(ctx context.Context, options Options) (Report, error) {
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return Report{}, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return Report{}, err
	}
	state, err := readState(layout)
	if err != nil {
		return Report{}, err
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		return Report{}, err
	}
	chainHash, replayErr := eventlog.Replay(events)
	report := Report{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		GeneratedAt:   time.Now().UTC(),
		State:         state.State,
		Provider:      "Codex CLI",
		Warnings:      state.Warnings,
		EventsByType:  make(map[string]int),
	}
	report.Summary = summarize(events)
	report.Confidence = confidence(events)
	report.Risk = risk(report.Summary, state.Warnings)
	report.Focus = focus(report.Summary, report.Risk)
	report.Gaps = gaps(report.Summary, report.Confidence, state.Warnings)
	report.Timeline = timeline(events)
	for _, event := range events {
		report.EventsByType[event.Type]++
	}
	report.Verification = model.Verification{
		EventChainHash: chainHash,
		DiffHash:       state.FinalDiffHash,
		Valid:          replayErr == nil,
	}
	if replayErr != nil {
		report.Risk.Level = maxRisk(report.Risk.Level, model.RiskHigh)
		report.Risk.Reasons = append(report.Risk.Reasons, model.RiskReason{
			Code:       "event_chain_invalid",
			Message:    replayErr.Error(),
			Level:      model.RiskHigh,
			Confidence: model.ConfidenceHigh,
		})
		report.Gaps = append(report.Gaps, "Event chain replay failed.")
	}
	if options.Security {
		report.Focus = append(report.Focus, "Review security-sensitive path changes and provider risk signals first.")
	}
	if options.Diff {
		report.Focus = append(report.Focus, "Compare final patch hash against reviewer-visible diff.")
	}

	return report, nil
}

func RenderTerminal(report Report) string {
	var builder strings.Builder
	builder.WriteString("AgentReceipt Review\n\n")
	fmt.Fprintf(&builder, "Session: %s\n", report.SessionID)
	fmt.Fprintf(&builder, "Provider: %s\n", report.Provider)
	fmt.Fprintf(&builder, "State: %s\n", report.State)
	fmt.Fprintf(&builder, "Risk: %s\n", report.Risk.Level)
	fmt.Fprintf(&builder, "Commands detected: %d\n", len(report.Summary.DetectedCommands))
	fmt.Fprintf(&builder, "Files changed: %d\n", len(report.Summary.ChangedFiles))
	builder.WriteString("\nCapture confidence:\n")
	fmt.Fprintf(&builder, "- Git diff: %s\n", report.Confidence.GitDiff)
	fmt.Fprintf(&builder, "- Filesystem writes: %s\n", report.Confidence.FilesystemWrites)
	fmt.Fprintf(&builder, "- Provider tool events: %s\n", report.Confidence.ProviderToolEvents)
	builder.WriteString("\nWarnings:\n")
	if len(report.Warnings) == 0 {
		builder.WriteString("- none\n")
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&builder, "- %s: %s\n", warning.Code, warning.Message)
	}
	builder.WriteString("\nReviewer focus:\n")
	for _, item := range report.Focus {
		fmt.Fprintf(&builder, "- %s\n", item)
	}

	return builder.String()
}

func RenderMarkdown(report Report) string {
	var builder strings.Builder
	builder.WriteString("## AgentReceipt\n\n")
	fmt.Fprintf(&builder, "Status: %s\n\n", statusText(report))
	builder.WriteString("Session:\n")
	fmt.Fprintf(&builder, "- Provider: %s\n", report.Provider)
	fmt.Fprintf(&builder, "- Session: `%s`\n", report.SessionID)
	fmt.Fprintf(&builder, "- Files changed: %d\n", len(report.Summary.ChangedFiles))
	fmt.Fprintf(&builder, "- Tool events: %d\n", report.EventsByType["provider.command"]+report.EventsByType["provider.event"])
	fmt.Fprintf(&builder, "- Commands detected: %d\n", len(report.Summary.DetectedCommands))
	fmt.Fprintf(&builder, "- Tests detected: %t\n\n", report.Summary.TestDetected)
	builder.WriteString("Risk:\n")
	for _, reason := range report.Risk.Reasons {
		fmt.Fprintf(&builder, "- %s: %s\n", reason.Level, reason.Message)
	}
	if len(report.Risk.Reasons) == 0 {
		builder.WriteString("- none\n")
	}
	builder.WriteString("\nCapture confidence:\n")
	fmt.Fprintf(&builder, "- Git diff: %s\n", report.Confidence.GitDiff)
	fmt.Fprintf(&builder, "- Filesystem writes: %s\n", report.Confidence.FilesystemWrites)
	fmt.Fprintf(&builder, "- Provider tool events: %s\n\n", report.Confidence.ProviderToolEvents)
	builder.WriteString("Reviewer focus:\n")
	for _, item := range report.Focus {
		fmt.Fprintf(&builder, "- %s\n", item)
	}

	return builder.String()
}

func resolveSession(ctx context.Context, options Options) (string, string, error) {
	repoRoot, err := gitmonitor.DiscoverRoot(ctx, repoPathOrCWD(options.RepoPath))
	if err != nil {
		return "", "", err
	}
	if options.SessionID != "" {
		return repoRoot, options.SessionID, nil
	}
	if !options.Last {
		manager := session.Manager{RepoPath: repoRoot}
		if state, ok, err := manager.Status(ctx); err != nil {
			return "", "", err
		} else if ok {
			return repoRoot, state.SessionID, nil
		}
	}
	sessionID, err := latestSession(repoRoot)
	if err != nil {
		return "", "", err
	}

	return repoRoot, sessionID, nil
}

func latestSession(repoRoot string) (string, error) {
	root, err := os.OpenRoot(filepath.Join(repoRoot, storage.RootDir, storage.SessionsDir))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = root.Close()
	}()
	var latest string
	var latestTime time.Time
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil || !entry.IsDir() || path == "." {
			return nil
		}
		if storage.ValidateSessionID(entry.Name()) != nil {
			return fs.SkipDir
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return fs.SkipDir
		}
		if latest == "" || info.ModTime().After(latestTime) {
			latest = entry.Name()
			latestTime = info.ModTime()
		}

		return fs.SkipDir
	})
	if err != nil {
		return "", err
	}
	if latest == "" {
		return "", errors.New("no AgentReceipt sessions found")
	}

	return latest, nil
}

func summarize(events []model.Event) model.Summary {
	changedByPath := make(map[string]model.ChangedFile)
	commands := make([]model.DetectedCommand, 0)
	for _, event := range events {
		if event.Type == "fs.change" {
			path := stringPayload(event.Payload, "path")
			if path != "" {
				changedByPath[path] = model.ChangedFile{
					Path:       path,
					Action:     stringPayload(event.Payload, "action"),
					Sensitive:  boolPayload(event.Payload, "sensitive"),
					Dependency: boolPayload(event.Payload, "dependency"),
				}
			}
		}
		if event.Type == "provider.command" {
			command := commandFromPayload(event.Payload)
			if command != "" {
				kind := commandKind(command)
				commands = append(commands, model.DetectedCommand{
					Command:    command,
					Kind:       kind,
					Status:     "unknown",
					Source:     event.Source,
					Confidence: model.ConfidenceMedium,
				})
			}
		}
	}
	changedFiles := make([]model.ChangedFile, 0, len(changedByPath))
	for _, changed := range changedByPath {
		changedFiles = append(changedFiles, changed)
	}
	sort.Slice(changedFiles, func(i, j int) bool {
		return changedFiles[i].Path < changedFiles[j].Path
	})
	summary := model.Summary{ChangedFiles: changedFiles, DetectedCommands: commands}
	for _, command := range commands {
		switch command.Kind {
		case "test":
			summary.TestDetected = true
		case "lint":
			summary.LintDetected = true
		case "typecheck":
			summary.TypecheckDetected = true
		}
	}

	return summary
}

func confidence(events []model.Event) model.CaptureConfidence {
	confidence := model.CaptureConfidence{
		GitDiff:            model.ConfidenceNone,
		FilesystemWrites:   model.ConfidenceNone,
		ProviderToolEvents: model.ConfidenceNone,
		FileReads:          model.ConfidenceNone,
		NetworkCalls:       model.ConfidenceLow,
	}
	for _, event := range events {
		switch event.Source {
		case "git_monitor":
			confidence.GitDiff = model.ConfidenceHigh
		case "fs_watcher":
			confidence.FilesystemWrites = model.ConfidenceHigh
		case "codex_session_log":
			confidence.ProviderToolEvents = model.ConfidenceMedium
		}
	}

	return confidence
}

func risk(summary model.Summary, warnings []model.Warning) model.Risk {
	result := model.Risk{Level: model.RiskInfo}
	for _, changed := range summary.ChangedFiles {
		if changed.Sensitive {
			result.Level = maxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "sensitive_path_changed",
				Message:    "Sensitive path changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
		if changed.Dependency {
			result.Level = maxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "dependency_changed",
				Message:    "Dependency file changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
	}
	for _, command := range summary.DetectedCommands {
		if command.Kind == "network" || command.Kind == "destructive" {
			result.Level = maxRisk(result.Level, model.RiskHigh)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "risky_command",
				Message:    "Risky command detected: " + command.Command,
				Level:      model.RiskHigh,
				Confidence: command.Confidence,
			})
		}
	}
	for _, warning := range warnings {
		result.Level = maxRisk(result.Level, model.RiskLow)
		result.Reasons = append(result.Reasons, model.RiskReason{
			Code:       warning.Code,
			Message:    warning.Message,
			Level:      model.RiskLow,
			Confidence: model.ConfidenceMedium,
		})
	}

	return result
}

func focus(summary model.Summary, risk model.Risk) []string {
	items := make([]string, 0)
	if len(risk.Reasons) == 0 {
		items = append(items, "Review the final diff against the generated receipt.")
	}
	for _, reason := range risk.Reasons {
		items = append(items, reason.Message)
	}
	if !summary.TestDetected {
		items = append(items, "Confirm appropriate tests were run for code changes.")
	}
	if !summary.TypecheckDetected {
		items = append(items, "Confirm typecheck coverage where relevant.")
	}

	return items
}

func gaps(summary model.Summary, confidence model.CaptureConfidence, warnings []model.Warning) []string {
	gaps := make([]string, 0)
	if confidence.ProviderToolEvents == model.ConfidenceNone {
		gaps = append(gaps, "No provider tool events were observed.")
	}
	if !summary.TestDetected {
		gaps = append(gaps, "No test command detected.")
	}
	if !summary.LintDetected {
		gaps = append(gaps, "No lint command detected.")
	}
	for _, warning := range warnings {
		gaps = append(gaps, warning.Message)
	}

	return gaps
}

func timeline(events []model.Event) []TimelineItem {
	items := make([]TimelineItem, 0, len(events))
	for _, event := range events {
		items = append(items, TimelineItem{
			Seq:    event.Seq,
			Time:   event.Timestamp.Format(time.RFC3339),
			Source: event.Source,
			Type:   event.Type,
		})
	}

	return items
}

func commandKind(command string) string {
	for _, matcher := range commandKindPatterns {
		if matcher.pattern.MatchString(command) {
			return matcher.kind
		}
	}

	return "command"
}

func commandFromPayload(payload map[string]any) string {
	toolCall, ok := payload["tool_call"].(map[string]any)
	if !ok {
		return stringPayload(payload, "command")
	}
	if command := stringPayload(toolCall, "command"); command != "" {
		return command
	}

	return stringPayload(mapPayload(toolCall, "arguments"), "cmd")
}

func readState(layout storage.Layout) (session.State, error) {
	root, err := os.OpenRoot(layout.Session)
	if err != nil {
		return session.State{}, err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := root.ReadFile(storage.StateFile)
	if err != nil {
		return session.State{}, err
	}
	var state session.State
	if err := json.Unmarshal(data, &state); err != nil {
		return session.State{}, err
	}

	return state, nil
}

func stringPayload(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}

	return ""
}

func boolPayload(payload map[string]any, key string) bool {
	if value, ok := payload[key].(bool); ok {
		return value
	}

	return false
}

func mapPayload(payload map[string]any, key string) map[string]any {
	if value, ok := payload[key].(map[string]any); ok {
		return value
	}

	return map[string]any{}
}

func maxRisk(left model.RiskLevel, right model.RiskLevel) model.RiskLevel {
	if riskRank(right) > riskRank(left) {
		return right
	}

	return left
}

func riskRank(level model.RiskLevel) int {
	switch level {
	case model.RiskLow:
		return 1
	case model.RiskMedium:
		return 2
	case model.RiskHigh:
		return 3
	case model.RiskCritical:
		return 4
	default:
		return 0
	}
}

func statusText(report Report) string {
	if report.Verification.Valid && len(report.Warnings) == 0 {
		return "Verified"
	}
	if report.Verification.Valid {
		return "Verified with warnings"
	}

	return "Invalid"
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

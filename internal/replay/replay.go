package replay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/buildinfo"
	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/evidence"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
	"github.com/ametel01/agentreceipt/internal/receipt"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
)

const replayKind = "agentreceipt.session_replay"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|authorization|token)\s*[:=]\S+`),
}

const maxOutputSummaryRunes = 120

// Options controls replay report building.
type Options struct {
	RepoPath    string
	SessionID   string
	GeneratedAt time.Time
	BundleDir   string
}

// Report is the verifier-facing replay payload.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Kind          string         `json:"kind"`
	SessionID     string         `json:"session_id"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Source        Source         `json:"source"`
	Verification  Verification   `json:"verification"`
	Summary       Summary        `json:"summary"`
	Timeline      []TimelineItem `json:"timeline"`
	Commands      []Command      `json:"commands"`
	Files         []File         `json:"files"`
	Risks         []Risk         `json:"risks"`
	Gaps          []string       `json:"gaps"`
	VerifierTasks []string       `json:"verifier_tasks"`
	Artifacts     []Artifact     `json:"artifacts"`
}

type Source struct {
	AgentReceiptVersion string             `json:"agentreceipt_version"`
	RepoRoot            string             `json:"repo_root"`
	SessionState        model.SessionState `json:"session_state"`
}

type Verification struct {
	Valid          bool   `json:"valid"`
	EventChainHash string `json:"event_chain_hash"`
	DiffHash       string `json:"diff_hash"`
	ManifestHash   string `json:"manifest_hash"`
	ReceiptHash    string `json:"receipt_hash"`
	SignatureValid bool   `json:"signature_valid"`
	SignedBy       string `json:"signed_by"`
}

type Summary struct {
	Provider          string          `json:"provider"`
	DurationSeconds   int64           `json:"duration_seconds"`
	CommandCount      int             `json:"command_count"`
	ChangedFileCount  int             `json:"changed_file_count"`
	TestDetected      bool            `json:"test_detected"`
	LintDetected      bool            `json:"lint_detected"`
	TypecheckDetected bool            `json:"typecheck_detected"`
	FinalRisk         model.RiskLevel `json:"final_risk"`
}

type TimelineItem struct {
	Seq          int64            `json:"seq"`
	Time         string           `json:"time"`
	Source       string           `json:"source"`
	Type         string           `json:"type"`
	Confidence   model.Confidence `json:"confidence"`
	EvidenceRefs []string         `json:"evidence_refs"`
}

type Command struct {
	Command         string           `json:"command"`
	Kind            string           `json:"kind"`
	Status          string           `json:"status"`
	ExitCode        *int             `json:"exit_code,omitempty"`
	RiskSignals     []RiskSignal     `json:"risk_signals"`
	OutputSummary   string           `json:"output_summary"`
	StdoutTruncated bool             `json:"stdout_truncated"`
	Confidence      model.Confidence `json:"confidence"`
	EvidenceRefs    []string         `json:"evidence_refs"`
}

type RiskSignal struct {
	Code       string           `json:"code"`
	Level      model.RiskLevel  `json:"level"`
	Confidence model.Confidence `json:"confidence"`
	Message    string           `json:"message,omitempty"`
}

type File struct {
	Path         string   `json:"path"`
	Action       string   `json:"action"`
	Sensitive    bool     `json:"sensitive"`
	Dependency   bool     `json:"dependency"`
	InFinalPatch bool     `json:"in_final_patch"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type Risk struct {
	Code         string           `json:"code"`
	Level        model.RiskLevel  `json:"level"`
	Confidence   model.Confidence `json:"confidence"`
	Message      string           `json:"message"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
}

type Artifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Hash   string `json:"hash"`
	Exists bool   `json:"exists"`
}

type commandAttempt struct {
	command     string
	rawCommand  string
	kind        string
	status      string
	callID      string
	seq         int64
	riskSignals []RiskSignal
}

type commandResultMeta struct {
	command         string
	commandSummary  string
	status          string
	callID          string
	exitCode        *int
	stdout          string
	stdoutTruncated bool
	failedReason    string
	stderrOrError   string
	seq             int64
}

// Build constructs a replay report for one session.
func Build(ctx context.Context, options Options) (Report, error) {
	if options.SessionID == "" {
		return Report{}, errors.New("session ID is required")
	}

	repoRoot, err := gitRoot(ctx, options.RepoPath)
	if err != nil {
		return Report{}, err
	}

	layout, err := storage.NewLayout(repoRoot, options.SessionID)
	if err != nil {
		return Report{}, err
	}

	state, stateErr := readSessionState(layout.StateJSON)
	events, eventsErr := eventlog.ReadFile(layout.EventsJSONL)
	if eventsErr != nil {
		events = nil
	}

	gaps := make([]string, 0)
	if eventsErr != nil {
		gaps = append(gaps, "Unable to read events.jsonl: "+eventsErr.Error())
	}
	if stateErr != nil {
		state = session.State{SessionID: options.SessionID, RepoRoot: repoRoot}
		gaps = append(gaps, "Unable to read session state: "+stateErr.Error())
	}

	cfg := config.Default()
	evidenceSummary := evidence.Summary(events, cfg)
	evidenceConfidence := evidence.Confidence(events)
	evidenceRisk := evidence.Risk(evidenceSummary, state.Warnings, events, cfg)
	gaps = append(gaps, evidence.Gaps(evidenceSummary, evidenceConfidence, state.Warnings, cfg)...)

	finalPatch, finalPatchErr := readText(layout.FinalPatch)
	if finalPatchErr != nil {
		gaps = append(gaps, "Unable to read final patch: "+finalPatchErr.Error())
	}

	commands, commandGaps := buildCommands(events, cfg)
	gaps = append(gaps, commandGaps...)

	files := buildFiles(evidenceSummary.ChangedFiles, finalPatch, events)
	timeline := buildTimeline(events, evidenceConfidence)
	risks := buildRisks(evidenceRisk.Reasons, commands, files)

	verification, verifyWarnings := buildVerification(layout, eventsErr == nil)
	gaps = append(gaps, verifyWarnings...)

	if state.State != model.SessionStateFinalized {
		gaps = append(gaps, "Session is not finalized (state="+string(state.State)+"). Evidence may be incomplete.")
	}

	artifacts := buildArtifacts(layout)
	tasks := buildVerifierTasks(evidenceRisk, evidenceSummary, commands, files, gaps)

	report := Report{
		SchemaVersion: model.SchemaVersion,
		Kind:          replayKind,
		SessionID:     options.SessionID,
		GeneratedAt:   generatedAt(options.GeneratedAt),
		Source: Source{
			AgentReceiptVersion: buildinfo.Version(),
			RepoRoot:            layout.RepoRoot,
			SessionState:        state.State,
		},
		Verification: verification,
		Summary: Summary{
			Provider:          providerevidence.ProviderLabel(events),
			DurationSeconds:   sessionDuration(state.StartedAt, events),
			CommandCount:      len(commands),
			ChangedFileCount:  len(evidenceSummary.ChangedFiles),
			TestDetected:      evidenceSummary.TestDetected,
			LintDetected:      evidenceSummary.LintDetected,
			TypecheckDetected: evidenceSummary.TypecheckDetected,
			FinalRisk:         evidenceRisk.Level,
		},
		Timeline:      timeline,
		Commands:      commands,
		Files:         files,
		Risks:         risks,
		Gaps:          uniqueSorted(gaps),
		VerifierTasks: uniqueSorted(tasks),
		Artifacts:     artifacts,
	}

	return report, nil
}

// WriteBundle writes a portable replay bundle at options.BundleDir and returns
// the report written to replay.json.
func WriteBundle(ctx context.Context, options Options) (Report, error) {
	if strings.TrimSpace(options.BundleDir) == "" {
		return Report{}, errors.New("bundle path is required")
	}
	if options.SessionID == "" {
		return Report{}, errors.New("session ID is required")
	}

	repoRoot, err := gitRoot(ctx, options.RepoPath)
	if err != nil {
		return Report{}, err
	}
	layout, err := storage.NewLayout(repoRoot, options.SessionID)
	if err != nil {
		return Report{}, err
	}

	report, err := Build(ctx, options)
	if err != nil {
		return Report{}, err
	}

	if err := os.MkdirAll(options.BundleDir, 0o750); err != nil {
		return Report{}, fmt.Errorf("create bundle directory: %w", err)
	}

	artifacts := make([]Artifact, 0, 8)
	requiredArtifacts := []struct {
		source string
		rel    string
		name   string
	}{
		{source: layout.EventsJSONL, rel: storage.EventsFile, name: storage.EventsFile},
		{source: layout.ReceiptJSON, rel: storage.ReceiptJSONFile, name: storage.ReceiptJSONFile},
		{source: layout.ManifestJSON, rel: storage.ManifestFile, name: storage.ManifestFile},
		{source: layout.FinalPatch, rel: filepath.Join(storage.DiffsDir, storage.FinalPatchFile), name: storage.FinalPatchFile},
	}
	for _, artifactInfo := range requiredArtifacts {
		artifactPath := filepath.Join(options.BundleDir, artifactInfo.rel)
		artifact, err := copyArtifactFile(artifactInfo.source, artifactPath, artifactInfo.name, artifactInfo.rel)
		if err != nil {
			return Report{}, err
		}
		artifacts = append(artifacts, artifact)
	}

	traceArtifacts, err := copyCodexTraces(layout.ProviderCodexTraces, options.BundleDir)
	if err != nil {
		return Report{}, err
	}
	artifacts = append(artifacts, traceArtifacts...)

	report.Artifacts = sortArtifacts(artifacts)

	replayPayloadWithoutReplay, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return Report{}, err
	}
	replayArtifact := Artifact{
		Name:   "replay.json",
		Path:   "replay.json",
		Hash:   "sha256:" + hashHex(replayPayloadWithoutReplay),
		Exists: true,
	}
	replayReport := report
	replayReport.Artifacts = append(append([]Artifact(nil), report.Artifacts...), replayArtifact)
	replayReport.Artifacts = sortArtifacts(replayReport.Artifacts)
	replayPayload, err := json.MarshalIndent(replayReport, "", "  ")
	if err != nil {
		return Report{}, err
	}
	replayPath := filepath.Join(options.BundleDir, "replay.json")
	if err := writeAtomic(replayPath, append(replayPayload, '\n')); err != nil {
		return Report{}, err
	}

	return replayReport, nil
}

func buildCommands(events []model.Event, cfg config.Config) ([]Command, []string) {
	attempts := make([]commandAttempt, 0)
	results := make([]commandResultMeta, 0)
	for _, event := range events {
		if attempt, ok := providerevidence.CommandAttemptFromEvent(event); ok {
			attempts = append(attempts, commandAttempt{
				command:     evidence.CommandSummary(attempt.Command),
				rawCommand:  attempt.Command,
				kind:        evidence.CommandKind(attempt.Command, cfg),
				status:      "unknown",
				callID:      attempt.CallID,
				seq:         event.Seq,
				riskSignals: toRiskSignals(providerevidence.RiskSignalsFromEvent(event)),
			})
			continue
		}
		if result := commandResultFromEvent(event); result.command != "" || result.status != "" || result.callID != "" || result.exitCode != nil {
			if result.status == "" {
				continue
			}
			results = append(results, result)
		}
	}

	commands := make([]Command, 0, len(attempts)+len(results))
	commandGaps := make([]string, 0)
	usedResult := map[int64]bool{}

	for _, attempt := range attempts {
		result, found := pairResult(attempt, results, usedResult)
		status := "unknown"
		if found {
			status = normalizeStatus(result.status)
			usedResult[result.seq] = true
		} else {
			commandGaps = append(commandGaps, "Unknown command status for command: "+attempt.command+" ("+evidenceRef(attempt.seq)+")")
		}

		command := Command{
			Command:      redact(attempt.command),
			Kind:         attempt.kind,
			Status:       status,
			RiskSignals:  attempt.riskSignals,
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: []string{evidenceRef(attempt.seq)},
		}
		if found {
			command.ExitCode = cloneInt(result.exitCode)
			command.StdoutTruncated = result.stdoutTruncated
			command.OutputSummary = commandOutputSummary(result)
			command.EvidenceRefs = append(command.EvidenceRefs, evidenceRef(result.seq))
		}
		command.EvidenceRefs = uniqueSorted(command.EvidenceRefs)
		if command.Kind == "" {
			command.Kind = "command"
		}
		commands = append(commands, command)
	}

	for _, result := range results {
		if result.command == "" || usedResult[result.seq] {
			continue
		}
		if result.callID != "" {
			if paired, _, _ := pairingByCallID(results, result.callID, usedResult); paired {
				continue
			}
		}
		command := Command{
			Command:         redact(result.commandSummary),
			Kind:            evidence.CommandKind(result.command, cfg),
			Status:          normalizeStatus(result.status),
			ExitCode:        cloneInt(result.exitCode),
			OutputSummary:   commandOutputSummary(result),
			StdoutTruncated: result.stdoutTruncated,
			Confidence:      model.ConfidenceMedium,
			EvidenceRefs:    uniqueSorted([]string{evidenceRef(result.seq)}),
		}
		if command.Kind == "" {
			command.Kind = "command"
		}
		commands = append(commands, command)
		commandGaps = append(commandGaps, "Unpaired command result for command: "+command.Command+" ("+evidenceRef(result.seq)+")")
	}

	sort.SliceStable(commands, func(i, j int) bool {
		left := commandFirstEvidenceRef(commands[i])
		right := commandFirstEvidenceRef(commands[j])
		if left == right {
			return commands[i].Command < commands[j].Command
		}
		return left < right
	})

	return commands, commandGaps
}

func pairResult(attempt commandAttempt, results []commandResultMeta, used map[int64]bool) (commandResultMeta, bool) {
	if attempt.callID != "" {
		for index := len(results) - 1; index >= 0; index-- {
			result := results[index]
			if used[result.seq] {
				continue
			}
			if result.callID != "" && result.callID == attempt.callID {
				return result, true
			}
		}
	}
	if attempt.command != "" {
		for index := len(results) - 1; index >= 0; index-- {
			result := results[index]
			if used[result.seq] {
				continue
			}
			if result.commandSummary == attempt.command {
				return result, true
			}
		}
	}

	return commandResultMeta{}, false
}

func pairingByCallID(results []commandResultMeta, callID string, used map[int64]bool) (bool, commandResultMeta, int) {
	if callID == "" {
		return false, commandResultMeta{}, -1
	}
	for index := len(results) - 1; index >= 0; index-- {
		result := results[index]
		if used[result.seq] {
			continue
		}
		if result.callID == callID {
			used[result.seq] = true
			return true, result, index
		}
	}

	return false, commandResultMeta{}, -1
}

func commandResultFromEvent(event model.Event) commandResultMeta {
	result := commandResultMeta{}
	if parsed, ok := providerevidence.CommandResultFromEvent(event); ok {
		result = commandResultMeta{
			command:         parsed.Command,
			commandSummary:  evidence.CommandSummary(parsed.Command),
			status:          parsed.Status,
			callID:          parsed.CallID,
			exitCode:        cloneInt(parsed.ExitCode),
			stdout:          parsed.Stdout,
			stdoutTruncated: parsed.StdoutTruncated,
			failedReason:    parsed.FailedReason,
			stderrOrError:   parsed.StderrOrError,
			seq:             event.Seq,
		}
	}

	return result
}

func commandOutputSummary(result commandResultMeta) string {
	parts := make([]string, 0, 3)
	if result.failedReason != "" {
		parts = append(parts, "failed: "+redact(result.failedReason))
	}
	if result.stderrOrError != "" {
		parts = append(parts, "stderr: "+redact(result.stderrOrError))
	}
	if result.stdout != "" {
		parts = append(parts, "stdout: "+redact(result.stdout))
	}
	if len(parts) == 0 {
		return ""
	}

	return truncate(strings.Join(parts, "; "), maxOutputSummaryRunes)
}

func toRiskSignals(signals []providerevidence.RiskSignal) []RiskSignal {
	output := make([]RiskSignal, 0, len(signals))
	seen := map[string]bool{}
	for _, signal := range signals {
		if signal.Level == "" {
			continue
		}
		code := "provider_risk_" + evidence.RiskCodeFragment(signal.Signal)
		message := signal.Details
		if message == "" {
			message = "provider risk: " + signal.Signal
		}
		if signal.Command != "" {
			summary := evidence.CommandSummary(signal.Command)
			if message == "" {
				message = "provider risk in command: " + summary
			} else {
				message = message + " in command: " + summary
			}
		}
		message = redact(truncate(message, 120))
		finger := code + "\n" + message
		if seen[finger] {
			continue
		}
		seen[finger] = true
		confidence := signal.Confidence
		if confidence == "" {
			confidence = model.ConfidenceMedium
		}
		output = append(output, RiskSignal{
			Code:       code,
			Level:      signal.Level,
			Confidence: confidence,
			Message:    message,
		})
	}

	return output
}

func buildFiles(changed []model.ChangedFile, finalPatch string, events []model.Event) []File {
	fileRefs := map[string][]string{}
	for _, event := range events {
		if event.Type != "fs.change" {
			continue
		}
		path := stringFromEventPayload(event.Payload, "path")
		if path == "" {
			continue
		}
		path = filepath.ToSlash(path)
		fileRefs[path] = append(fileRefs[path], evidenceRef(event.Seq))
	}

	files := make([]File, 0, len(changed))
	for _, changedFile := range changed {
		path := filepath.ToSlash(changedFile.Path)
		files = append(files, File{
			Path:         path,
			Action:       changedFile.Action,
			Sensitive:    changedFile.Sensitive,
			Dependency:   changedFile.Dependency,
			InFinalPatch: patchContainsPath(finalPatch, path),
			EvidenceRefs: uniqueSorted(fileRefs[path]),
		})
	}

	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Path == files[j].Path {
			return files[i].Action < files[j].Action
		}
		return files[i].Path < files[j].Path
	})

	return files
}

func patchContainsPath(patch string, target string) bool {
	needle := filepath.ToSlash(target)
	if needle == "" {
		return false
	}
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		left := diffPath(parts[2])
		right := diffPath(parts[3])
		if left == needle || right == needle {
			return true
		}
	}
	return false
}

func diffPath(token string) string {
	t := strings.TrimSpace(token)
	if strings.HasPrefix(t, "a/") || strings.HasPrefix(t, "b/") {
		t = t[2:]
	}
	return strings.TrimPrefix(filepath.ToSlash(t), "./")
}

func buildTimeline(events []model.Event, confidence model.CaptureConfidence) []TimelineItem {
	timeline := make([]TimelineItem, 0, len(events))
	for _, event := range events {
		timeline = append(timeline, TimelineItem{
			Seq:          event.Seq,
			Time:         event.Timestamp.Format(time.RFC3339),
			Source:       event.Source,
			Type:         event.Type,
			Confidence:   timelineConfidence(event, confidence),
			EvidenceRefs: []string{evidenceRef(event.Seq)},
		})
	}

	sort.SliceStable(timeline, func(i, j int) bool {
		return timeline[i].Seq < timeline[j].Seq
	})

	return timeline
}

func timelineConfidence(event model.Event, confidence model.CaptureConfidence) model.Confidence {
	switch {
	case event.Source == "git_monitor":
		return confidence.GitDiff
	case event.Source == "fs_watcher":
		return confidence.FilesystemWrites
	case providerevidence.IsProviderEvidenceSource(event):
		return confidence.ProviderToolEvents
	default:
		return model.ConfidenceNone
	}
}

func buildRisks(reasons []model.RiskReason, commands []Command, files []File) []Risk {
	commandRefs := map[string][]string{}
	for _, command := range commands {
		if command.Command == "" {
			continue
		}
		commandRefs[command.Command] = append(commandRefs[command.Command], command.EvidenceRefs...)
		commandRefs[evidence.CommandSummary(command.Command)] = append(commandRefs[evidence.CommandSummary(command.Command)], command.EvidenceRefs...)
	}
	fileRefs := map[string][]string{}
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		fileRefs[file.Path] = append(fileRefs[file.Path], file.EvidenceRefs...)
	}

	risks := make([]Risk, 0, len(reasons))
	seen := map[string]bool{}
	for _, reason := range reasons {
		risk := Risk{
			Code:       reason.Code,
			Level:      reason.Level,
			Confidence: reason.Confidence,
			Message:    redact(reason.Message),
		}
		risk.EvidenceRefs = evidenceRefsFromRisk(risk, commandRefs, fileRefs)
		if risk.Confidence == "" {
			risk.Confidence = model.ConfidenceMedium
		}
		finger := risk.Code + "\x00" + risk.Message
		if seen[finger] {
			continue
		}
		seen[finger] = true
		risks = append(risks, risk)
	}

	sort.SliceStable(risks, func(i, j int) bool {
		if risks[i].Code == risks[j].Code {
			return risks[i].Message < risks[j].Message
		}
		return risks[i].Code < risks[j].Code
	})

	return risks
}

func evidenceRefsFromRisk(risk Risk, commandRefs map[string][]string, fileRefs map[string][]string) []string {
	if strings.Contains(risk.Code, "provider_risk_") || strings.HasPrefix(risk.Code, "command_risk_") {
		if refs, ok := refsFromCommandMessage(risk.Message, commandRefs); ok {
			return refs
		}
	}
	if path, ok := pathFromMessage(risk.Message); ok {
		if refs := fileRefs[path]; len(refs) > 0 {
			return uniqueSorted(refs)
		}
	}
	for key, refs := range commandRefs {
		if len(refs) == 0 {
			continue
		}
		if key == "" {
			continue
		}
		for _, candidate := range []string{risk.Code, risk.Message} {
			if strings.Contains(candidate, key) {
				return uniqueSorted(refs)
			}
		}
	}

	return nil
}

func refsFromCommandMessage(message string, commandRefs map[string][]string) ([]string, bool) {
	needle := ""
	if idx := strings.LastIndex(message, "in command:"); idx >= 0 {
		needle = strings.TrimSpace(message[idx+len("in command:"):])
	}
	if needle != "" {
		for key, refs := range commandRefs {
			if key == needle || strings.Contains(key, needle) || strings.Contains(needle, key) {
				return uniqueSorted(refs), true
			}
		}
	}
	return nil, false
}

func pathFromMessage(message string) (string, bool) {
	if strings.Contains(message, ": ") {
		parts := strings.SplitN(message, ": ", 2)
		candidate := strings.TrimSpace(parts[len(parts)-1])
		if candidate != "" {
			return filepath.ToSlash(candidate), true
		}
	}
	for _, marker := range []string{" in ", "; ", " path "} {
		if idx := strings.LastIndex(message, marker); idx >= 0 {
			candidate := strings.TrimSpace(message[idx+len(marker):])
			if candidate != "" {
				return filepath.ToSlash(candidate), true
			}
		}
	}
	return "", false
}

func buildVerifierTasks(
	evidenceRisk model.Risk,
	evidenceSummary model.Summary,
	commands []Command,
	files []File,
	gaps []string,
) []string {
	tasks := make([]string, 0)
	tasks = append(tasks, gaps...)
	tasks = append(tasks, evidence.Focus(evidenceSummary, evidenceRisk, config.Default())...)

	for _, command := range commands {
		switch command.Status {
		case "failed":
			tasks = append(tasks, "Inspect failed command: "+command.Command)
		case "unknown":
			tasks = append(tasks, "Confirm command result for: "+command.Command)
		}
	}

	for _, file := range files {
		if file.InFinalPatch && file.Sensitive {
			tasks = append(tasks, "Review sensitive path changes: "+file.Path)
		}
		if file.InFinalPatch && file.Dependency {
			tasks = append(tasks, "Review dependency file changes: "+file.Path)
		}
	}

	return tasks
}

func buildVerification(layout storage.Layout, _ bool) (Verification, []string) {
	verification := Verification{}
	verificationWarnings := make([]string, 0)

	if chainHash, err := eventHash(layout.EventsJSONL); err == nil {
		verification.EventChainHash = chainHash
	} else {
		verificationWarnings = append(verificationWarnings, "Event chain verification failed")
	}
	if diffHash, err := fileHash(layout.FinalPatch); err == nil {
		verification.DiffHash = diffHash
	} else {
		verificationWarnings = append(verificationWarnings, "Final patch hash verification failed")
	}
	if manifestHash, err := fileHash(layout.ManifestJSON); err == nil {
		verification.ManifestHash = manifestHash
	} else {
		verificationWarnings = append(verificationWarnings, "Manifest hash verification failed")
	}
	if receiptHash, err := fileHash(layout.ReceiptJSON); err == nil {
		if rec, err := receipt.Read(layout); err == nil && rec.Verification.ReceiptHash != "" {
			verification.ReceiptHash = rec.Verification.ReceiptHash
			if receiptHash == "" {
				verification.ReceiptHash = ""
			}
		} else {
			verification.ReceiptHash = receiptHash
			if err != nil {
				verificationWarnings = append(verificationWarnings, "Receipt hash read failed")
			}
		}
	} else {
		verificationWarnings = append(verificationWarnings, "Receipt hash verification failed")
	}

	result, err := receipt.VerifyBundle(layout.Session)
	if err != nil {
		verificationWarnings = append(verificationWarnings, err.Error())
		verification.Valid = false
		return verification, verificationWarnings
	}

	verification.Valid = result.Valid
	verification.SignatureValid = result.Signature
	verification.SignedBy = result.SignedBy
	if !result.EventChain {
		verificationWarnings = append(verificationWarnings, "Event chain verification failed")
	}
	if !result.FinalDiffHash {
		verificationWarnings = append(verificationWarnings, "Final patch hash verification failed")
	}
	if !result.ManifestHash {
		verificationWarnings = append(verificationWarnings, "Manifest hash verification failed")
	}
	if !result.ReceiptHash {
		verificationWarnings = append(verificationWarnings, "Receipt hash verification failed")
	}
	if !result.Signature {
		verificationWarnings = append(verificationWarnings, "Signature verification failed")
	}

	if verificationWarnings := uniqueSorted(verificationWarnings); len(verificationWarnings) > 0 {
		if !verification.Valid {
			return verification, verificationWarnings
		}
	}

	return verification, uniqueSorted(verificationWarnings)
}

func buildArtifacts(layout storage.Layout) []Artifact {
	return []Artifact{
		artifact(filepath.Base(layout.Session), "events.jsonl", layout.EventsJSONL),
		artifact(filepath.Base(layout.Session), "receipt.json", layout.ReceiptJSON),
		artifact(filepath.Base(layout.Session), "manifest.json", layout.ManifestJSON),
		artifact(filepath.Base(layout.Session), filepath.Join("diffs", "final.patch"), layout.FinalPatch),
	}
}

func copyArtifactFile(source, destination, name, path string) (Artifact, error) {
	root, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return Artifact{}, fmt.Errorf("open artifact directory %q: %w", filepath.Dir(source), err)
	}
	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(filepath.Base(source))
	if err != nil {
		return Artifact{}, fmt.Errorf("read artifact %q: %w", source, err)
	}
	if err := writeAtomic(destination, data); err != nil {
		return Artifact{}, fmt.Errorf("write artifact %q: %w", destination, err)
	}

	return Artifact{
		Name:   name,
		Path:   filepath.ToSlash(path),
		Hash:   "sha256:" + hashHex(data),
		Exists: true,
	}, nil
}

func copyCodexTraces(traceRoot, bundleRoot string) ([]Artifact, error) {
	_, err := os.Stat(traceRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("check codex trace directory: %w", err)
	}

	artifacts := make([]Artifact, 0)
	err = filepath.WalkDir(traceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(traceRoot, path)
		if err != nil {
			return fmt.Errorf("compute trace artifact path: %w", err)
		}
		rel := filepath.ToSlash(relative)
		destination := filepath.Join(bundleRoot, storage.ProviderDir, storage.ProviderCodexDir, storage.TracesDir, rel)
		artifactPath := filepath.ToSlash(filepath.Join(storage.ProviderDir, storage.ProviderCodexDir, storage.TracesDir, rel))
		artifact, err := copyArtifactFile(path, destination, artifactPath, artifactPath)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("copy codex traces: %w", err)
	}

	return sortArtifacts(artifacts), nil
}

func writeAtomic(destination string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".agentreceipt-")
	if err != nil {
		return fmt.Errorf("create temp file for %q: %w", destination, err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if _, err := io.Copy(temp, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("write temp file for %q: %w", destination, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temp file for %q: %w", destination, err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp file for %q: %w", destination, err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("write %q: %w", destination, err)
	}
	return nil
}

func sortArtifacts(artifacts []Artifact) []Artifact {
	sorted := append([]Artifact(nil), artifacts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Path == sorted[j].Path {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Path < sorted[j].Path
	})
	return sorted
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func artifact(sessionRoot string, name string, path string) Artifact {
	artifactPath := filepath.ToSlash(path)
	if rel, err := filepath.Rel(sessionRoot, path); err == nil {
		artifactPath = filepath.ToSlash(rel)
	}
	hash, exists := buildArtifactHash(path)
	return Artifact{Name: name, Path: artifactPath, Hash: hash, Exists: exists}
}

func buildArtifactHash(path string) (string, bool) {
	data, err := readText(path)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(data))

	return "sha256:" + hex.EncodeToString(sum[:]), true
}

func eventHash(path string) (string, error) {
	events, err := eventlog.ReadFile(path)
	if err != nil {
		return "", err
	}
	return eventlog.Replay(events)
}

func fileHash(path string) (string, error) {
	data, err := readRaw(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func readSessionState(path string) (session.State, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return session.State{}, err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return session.State{}, err
	}
	var state session.State
	if err := json.Unmarshal(data, &state); err != nil {
		return session.State{}, fmt.Errorf("decode session state: %w", err)
	}

	return state, nil
}

func readRaw(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}

func readText(path string) (string, error) {
	data, err := readRaw(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func gitRoot(ctx context.Context, repoPath string) (string, error) {
	if repoPath == "" {
		repoPath = "."
	}
	return gitmonitor.DiscoverRoot(ctx, repoPath)
}

func evidenceRef(seq int64) string {
	return fmt.Sprintf("events.jsonl#seq=%d", seq)
}

func commandFirstEvidenceRef(command Command) int64 {
	if len(command.EvidenceRefs) == 0 {
		return 0
	}
	smallest := int64(1<<63 - 1)
	for _, ref := range command.EvidenceRefs {
		if !strings.HasPrefix(ref, "events.jsonl#seq=") {
			continue
		}
		var seq int64
		if _, err := fmt.Sscanf(ref, "events.jsonl#seq=%d", &seq); err == nil && seq < smallest {
			smallest = seq
		}
	}
	if smallest == 1<<63-1 {
		return 0
	}
	return smallest
}

func normalizeStatus(status string) string {
	switch status {
	case "success", "failed", "unknown":
		return status
	default:
		return "unknown"
	}
}

func sessionDuration(startedAt time.Time, events []model.Event) int64 {
	if startedAt.IsZero() || len(events) == 0 {
		return 0
	}
	duration := int64(events[len(events)-1].Timestamp.Sub(startedAt).Seconds())
	if duration < 0 {
		return 0
	}
	return duration
}

func generatedAt(timestamp time.Time) time.Time {
	if timestamp.IsZero() {
		return time.Now().UTC()
	}
	return timestamp
}

func stringFromEventPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value := payload[key]
	txt, ok := value.(string)
	if !ok {
		return ""
	}
	return txt
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func truncate(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func redact(input string) string {
	value := input
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

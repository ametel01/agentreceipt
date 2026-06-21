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
	"github.com/ametel01/agentreceipt/internal/capture/fswatcher"
	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/capture/instructions"
	"github.com/ametel01/agentreceipt/internal/commandrisk"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/evidence"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
	"github.com/ametel01/agentreceipt/internal/receipt"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
	"github.com/ametel01/agentreceipt/internal/trust"
)

const replayKind = "agentreceipt.session_replay"
const finalPatchEvidenceRef = "diffs/final.patch"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|authorization|token)\s*[:=]\S+`),
}

const maxOutputSummaryRunes = 120

// Options controls replay report building.
type Options struct {
	RepoPath            string
	SessionID           string
	GeneratedAt         time.Time
	BundleDir           string
	TrustedSignerKeyIDs []string
}

// Report is the verifier-facing replay payload.
type Report struct {
	SchemaVersion        int                   `json:"schema_version"`
	Kind                 string                `json:"kind"`
	SessionID            string                `json:"session_id"`
	GeneratedAt          time.Time             `json:"generated_at"`
	Source               Source                `json:"source"`
	Verification         Verification          `json:"verification"`
	Summary              Summary               `json:"summary"`
	EvaluatorSignals     EvaluatorSignals      `json:"evaluator_signals"`
	QualityGates         QualityGates          `json:"quality_gates"`
	PatchSummary         PatchSummary          `json:"patch_summary"`
	PolicyChecks         []PolicyCheck         `json:"policy_checks"`
	ReviewFocus          []ReviewFocusItem     `json:"review_focus"`
	Privacy              PrivacyReport         `json:"privacy"`
	Claims               []Claim               `json:"claims"`
	Outcome              Outcome               `json:"outcome"`
	Timeline             []TimelineItem        `json:"timeline"`
	Commands             []Command             `json:"commands"`
	InstructionFiles     []InstructionFile     `json:"instruction_files"`
	Files                []File                `json:"files"`
	Risks                []Risk                `json:"risks"`
	FailedCommandDetails []FailedCommandDetail `json:"failed_command_details,omitempty"`
	Gaps                 []string              `json:"gaps"`
	VerifierTasks        []string              `json:"verifier_tasks"`
	Artifacts            []Artifact            `json:"artifacts"`
}

type Source struct {
	AgentReceiptVersion string             `json:"agentreceipt_version"`
	RepoRoot            string             `json:"repo_root"`
	SessionState        model.SessionState `json:"session_state"`
}

type Verification struct {
	Valid               bool   `json:"valid"`
	EventChainHash      string `json:"event_chain_hash"`
	DiffHash            string `json:"diff_hash"`
	ManifestHash        string `json:"manifest_hash"`
	ReceiptHash         string `json:"receipt_hash"`
	EventChainValid     bool   `json:"event_chain_valid"`
	FinalPatchHashValid bool   `json:"final_patch_hash_valid"`
	ManifestHashValid   bool   `json:"manifest_hash_valid"`
	ReceiptHashValid    bool   `json:"receipt_hash_valid"`
	SignatureValid      bool   `json:"signature_valid"`
	SignatureError      string `json:"signature_error,omitempty"`
	SignatureErrorCode  string `json:"signature_error_code,omitempty"`
	SignedBy            string `json:"signed_by"`

	IntegrityValid     bool                `json:"integrity_valid"`
	AuthenticityValid  bool                `json:"authenticity_valid"`
	AuthenticityStatus string              `json:"authenticity_status"`
	TrustStatus        string              `json:"trust_status"`
	SignerTrusted      bool                `json:"signer_trusted"`
	PolicyValid        bool                `json:"policy_valid"`
	OverallVerdict     string              `json:"overall_verdict"`
	OverallReason      string              `json:"overall_reason"`
	ComponentResults   []VerificationCheck `json:"component_results"`
}

type VerificationCheck struct {
	Name   string `json:"name"`
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

const (
	receiptErrLegacySignerMissing = "legacy_missing_embedded_signer"

	verificationVerdictIntegrityOnly = "integrity_only"
	verificationVerdictIntegrityFail = "integrity_failed"
	verificationVerdictUntrusted     = "untrusted_signer"
	verificationVerdictPolicyFailure = "policy_failed"
	verificationVerdictPassed        = "passed"

	authenticityStatusAuthentic    = "authenticated"
	authenticityStatusUnverifiable = "unverifiable"
	authenticityStatusFailed       = "failed"
	trustStatusTrusted             = "trusted"
	trustStatusNotTrusted          = "not_trusted"
	trustStatusNotConfigured       = "not_configured"
	trustStatusPolicyInvalid       = "policy_invalid"

	qualityGateStatusPassed  = "passed"
	qualityGateStatusFailed  = "failed"
	qualityGateStatusNotRun  = "not_run"
	qualityGateStatusUnknown = "unknown"

	policyCheckStatusPass          = "pass"
	policyCheckStatusFail          = "fail"
	policyCheckStatusWarn          = "warn"
	policyCheckStatusNotApplicable = "not_applicable"
	policyCheckStatusUnknown       = "unknown"
)

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

type EvaluatorSignals struct {
	ReadCommandCount           int `json:"read_command_count"`
	WriteCommandCount          int `json:"write_command_count"`
	EditCommandCount           int `json:"edit_command_count"`
	TestCommandCount           int `json:"test_command_count"`
	LintCommandCount           int `json:"lint_command_count"`
	TypecheckCommandCount      int `json:"typecheck_command_count"`
	FailedCommandCount         int `json:"failed_command_count"`
	NetworkCommandCount        int `json:"network_command_count"`
	DestructiveCommandCount    int `json:"destructive_command_count"`
	GitMutationCommandCount    int `json:"git_mutation_command_count"`
	DependencyFileChangeCount  int `json:"dependency_file_change_count"`
	SensitiveFileChangeCount   int `json:"sensitive_file_change_count"`
	CommitCount                int `json:"commit_count"`
	ChangedProductionFileCount int `json:"changed_production_file_count"`
	ChangedTestFileCount       int `json:"changed_test_file_count"`
	ChangedDocFileCount        int `json:"changed_doc_file_count"`
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
	OutputSummary   string           `json:"output_summary"`
	StdoutTruncated bool             `json:"stdout_truncated"`
	Confidence      model.Confidence `json:"confidence"`
	Cwd             string           `json:"cwd,omitempty"`
	Time            string           `json:"time,omitempty"`
	EvidenceRefs    []string         `json:"evidence_refs"`

	failedReason         string
	stderrOrErrorForFail string
	stdoutForFail        string
}

type File struct {
	Path         string   `json:"path"`
	Action       string   `json:"action"`
	Sensitive    bool     `json:"sensitive"`
	Dependency   bool     `json:"dependency"`
	InFinalPatch bool     `json:"in_final_patch"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type InstructionFile struct {
	Path    string   `json:"path"`
	Hash    string   `json:"hash"`
	Size    int64    `json:"size"`
	MTime   string   `json:"mtime"`
	Summary []string `json:"summary"`
}

type QualityGate struct {
	Status       string           `json:"status"`
	Commands     []string         `json:"commands"`
	EvidenceRefs []string         `json:"evidence_refs"`
	LastExitCode *int             `json:"last_exit_code,omitempty"`
	Confidence   model.Confidence `json:"confidence"`
}

type QualityGates struct {
	Format    QualityGate `json:"format"`
	Lint      QualityGate `json:"lint"`
	Tests     QualityGate `json:"tests"`
	RaceTests QualityGate `json:"race_tests"`
	Typecheck QualityGate `json:"typecheck"`
	Security  QualityGate `json:"security"`
	Coverage  QualityGate `json:"coverage"`
	Build     QualityGate `json:"build"`
	Smoke     QualityGate `json:"smoke"`
	Verify    QualityGate `json:"verify"`
}

type FailedCommandDetail struct {
	Cwd                  string           `json:"cwd"`
	Time                 string           `json:"time"`
	ExitCode             *int             `json:"exit_code,omitempty"`
	FailedReason         string           `json:"failed_reason"`
	StderrOrErrorSummary string           `json:"stderr_or_error_summary"`
	StdoutSummary        string           `json:"stdout_summary"`
	OutputTruncated      bool             `json:"output_truncated"`
	EvidenceRefs         []string         `json:"evidence_refs"`
	Confidence           model.Confidence `json:"confidence"`
}

type PolicyCheck struct {
	Name         string           `json:"name"`
	Status       string           `json:"status"`
	Message      string           `json:"message"`
	Confidence   model.Confidence `json:"confidence"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
}

type ReviewFocusItem struct {
	Message      string           `json:"message"`
	Confidence   model.Confidence `json:"confidence"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
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
	command string
	kind    string
	status  string
	callID  string
	seq     int64
	cwd     string
	time    string
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
	cwd             string
	time            string
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
	gaps = append(gaps, buildFactualGaps(state.Warnings, evidenceConfidence)...)

	finalPatch, finalPatchErr := readText(layout.FinalPatch)
	if finalPatchErr != nil {
		gaps = append(gaps, "Unable to read final patch: "+finalPatchErr.Error())
	}

	classifier := fswatcher.NewClassifier(cfg)
	patchSummary, patchGaps := buildPatchSummary(finalPatch, classifier)
	gaps = append(gaps, patchGaps...)

	commands, failedCommandDetails, commandGaps := buildCommands(events, cfg)
	gaps = append(gaps, commandGaps...)

	files := buildFiles(classifier, evidenceSummary.ChangedFiles, finalPatch, events)
	instructionFiles := buildInstructionFiles(events)
	timeline := buildTimeline(events, evidenceConfidence)

	verification, verifyWarnings := buildVerification(layout, eventsErr == nil, options)
	gaps = append(gaps, verifyWarnings...)

	if state.State != model.SessionStateFinalized {
		gaps = append(gaps, "Session is not finalized (state="+string(state.State)+"). Evidence may be incomplete.")
	}

	artifacts := buildArtifacts(layout)
	qualityGates := buildQualityGates(commands)
	policyChecks := buildPolicyChecks(commands, files, patchSummary, qualityGates)
	reviewFocus := buildReviewFocus(gaps, qualityGates, patchSummary, policyChecks, commands, files)
	privacyReport := buildPrivacyReport(commands, failedCommandDetails, files)

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
			ChangedFileCount:  len(files),
			TestDetected:      evidenceSummary.TestDetected,
			LintDetected:      evidenceSummary.LintDetected,
			TypecheckDetected: evidenceSummary.TypecheckDetected,
			FinalRisk:         model.RiskInfo,
		},
		EvaluatorSignals:     buildEvaluatorSignals(commands, files),
		QualityGates:         qualityGates,
		PatchSummary:         patchSummary,
		PolicyChecks:         policyChecks,
		ReviewFocus:          reviewFocus,
		Privacy:              privacyReport,
		FailedCommandDetails: failedCommandDetails,
		Timeline:             timeline,
		Commands:             commands,
		Files:                files,
		Risks:                nil,
		Gaps:                 uniqueSorted(gaps),
		VerifierTasks:        uniqueSorted(gaps),
		Artifacts:            artifacts,
		InstructionFiles:     instructionFiles,
	}
	report.Outcome = buildOutcome(report)
	report.Claims = buildClaims(report)

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

func buildCommands(events []model.Event, cfg config.Config) ([]Command, []FailedCommandDetail, []string) {
	attempts := make([]commandAttempt, 0)
	results := make([]commandResultMeta, 0)
	for _, event := range events {
		if attempt, ok := providerevidence.CommandAttemptFromEvent(event); ok {
			attempts = append(attempts, commandAttempt{
				command: evidence.CommandSummary(attempt.Command),
				kind:    evidence.CommandKind(attempt.Command, cfg),
				status:  "unknown",
				callID:  attempt.CallID,
				seq:     event.Seq,
				cwd:     event.CWD,
				time:    event.Timestamp.Format(time.RFC3339),
			})
			continue
		}
		if result := commandResultFromEvent(event); result.command != "" || result.status != "" || result.callID != "" || result.exitCode != nil {
			if result.status == "" {
				continue
			}
			result.cwd = event.CWD
			result.time = event.Timestamp.Format(time.RFC3339)
			results = append(results, result)
		}
	}

	commands := make([]Command, 0, len(attempts)+len(results))
	failedCommandDetails := make([]FailedCommandDetail, 0)
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
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: []string{evidenceRef(attempt.seq)},
		}
		if found {
			command.ExitCode = cloneInt(result.exitCode)
			command.StdoutTruncated = result.stdoutTruncated
			command.OutputSummary = commandOutputSummary(result)
			command.failedReason = result.failedReason
			command.stderrOrErrorForFail = result.stderrOrError
			command.stdoutForFail = result.stdout
			command.EvidenceRefs = append(command.EvidenceRefs, evidenceRef(result.seq))
			command.Cwd = coalesceString(attempt.cwd, result.cwd)
			command.Time = coalesceString(attempt.time, result.time)
		} else {
			command.Cwd = attempt.cwd
			command.Time = attempt.time
		}
		command.EvidenceRefs = uniqueSorted(command.EvidenceRefs)
		if command.Kind == "" {
			command.Kind = "command"
		}
		if isCommandFailed(command) {
			failedCommandDetails = append(failedCommandDetails, buildFailedCommandDetail(command))
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
			Command:              redact(result.commandSummary),
			Kind:                 evidence.CommandKind(result.command, cfg),
			Status:               normalizeStatus(result.status),
			ExitCode:             cloneInt(result.exitCode),
			OutputSummary:        commandOutputSummary(result),
			StdoutTruncated:      result.stdoutTruncated,
			Confidence:           model.ConfidenceMedium,
			EvidenceRefs:         uniqueSorted([]string{evidenceRef(result.seq)}),
			failedReason:         result.failedReason,
			stderrOrErrorForFail: result.stderrOrError,
			stdoutForFail:        result.stdout,
			Cwd:                  result.cwd,
			Time:                 result.time,
		}
		if isCommandFailed(command) {
			failedCommandDetails = append(failedCommandDetails, buildFailedCommandDetail(command))
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

	return commands, failedCommandDetails, uniqueSorted(commandGaps)
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func buildFailedCommandDetail(command Command) FailedCommandDetail {
	return FailedCommandDetail{
		Cwd:                  command.Cwd,
		Time:                 command.Time,
		ExitCode:             cloneInt(command.ExitCode),
		FailedReason:         redact(command.failedReason),
		StderrOrErrorSummary: redact(truncate(command.stderrOrErrorForFail, maxOutputSummaryRunes)),
		StdoutSummary:        redact(truncate(command.stdoutForFail, maxOutputSummaryRunes)),
		OutputTruncated:      command.StdoutTruncated,
		EvidenceRefs:         append([]string(nil), command.EvidenceRefs...),
		Confidence:           command.Confidence,
	}
}

func buildEvaluatorSignals(commands []Command, files []File) EvaluatorSignals {
	evaluatorSignals := EvaluatorSignals{}
	for _, command := range commands {
		if isReadCommand(command.Command) {
			evaluatorSignals.ReadCommandCount++
		}
		if isWriteCommand(command.Command) {
			evaluatorSignals.WriteCommandCount++
		}
		if isEditCommand(command.Command) {
			evaluatorSignals.EditCommandCount++
		}
		switch command.Kind {
		case "test":
			evaluatorSignals.TestCommandCount++
		case "lint":
			evaluatorSignals.LintCommandCount++
		case "typecheck":
			evaluatorSignals.TypecheckCommandCount++
		}
		if isCommandFailed(command) {
			evaluatorSignals.FailedCommandCount++
		}
		commandSignals := commandrisk.Classify(command.Command)
		if commandSignalsHas(commandSignals, "network_egress", "remote_code_execution", "cloud_or_deploy_mutation") {
			evaluatorSignals.NetworkCommandCount++
		}
		if commandSignalsHas(commandSignals, "destructive_filesystem", "destructive_git", "container_destructive", "find_delete", "mass_edit_or_overwrite") {
			evaluatorSignals.DestructiveCommandCount++
		}
		if commandSignalsHas(commandSignals, "git_mutation") {
			evaluatorSignals.GitMutationCommandCount++
		}
		if isCommitLikeCommand(command.Command) {
			evaluatorSignals.CommitCount++
		}
	}

	for _, file := range files {
		if file.Dependency {
			evaluatorSignals.DependencyFileChangeCount++
		}
		if file.Sensitive {
			evaluatorSignals.SensitiveFileChangeCount++
		}
		switch {
		case isTestFile(file.Path):
			evaluatorSignals.ChangedTestFileCount++
		case isDocFile(file.Path):
			evaluatorSignals.ChangedDocFileCount++
		default:
			evaluatorSignals.ChangedProductionFileCount++
		}
	}

	return evaluatorSignals
}

func buildQualityGates(commands []Command) QualityGates {
	qualityGates := QualityGates{
		Format:    QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Lint:      QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Tests:     QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		RaceTests: QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Typecheck: QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Security:  QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Coverage:  QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Build:     QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Smoke:     QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
		Verify:    QualityGate{Status: qualityGateStatusNotRun, Confidence: model.ConfidenceLow},
	}

	for _, command := range commands {
		commandText := command.Command
		for _, gate := range qualityGatesForCommand(commandText, command.Kind) {
			qualityGate := qualityGateRef(&qualityGates, gate)
			if qualityGate == nil {
				continue
			}
			qualityGate.Commands = append(qualityGate.Commands, commandText)
			qualityGate.EvidenceRefs = append(qualityGate.EvidenceRefs, command.EvidenceRefs...)
			qualityGate.Status = combineGateStatus(qualityGate.Status, gateStatusFromCommand(command))
			qualityGate.LastExitCode = gateLastExitCode(qualityGate.LastExitCode, command.ExitCode)
			qualityGate.Confidence = confidenceFromGateStatus(qualityGate.Status)
		}
	}

	qualityGates = qualityGateFinalize(qualityGates)

	return qualityGates
}

func qualityGateRef(gates *QualityGates, name string) *QualityGate {
	switch name {
	case "format":
		return &gates.Format
	case "lint":
		return &gates.Lint
	case "tests":
		return &gates.Tests
	case "race_tests":
		return &gates.RaceTests
	case "typecheck":
		return &gates.Typecheck
	case "security":
		return &gates.Security
	case "coverage":
		return &gates.Coverage
	case "build":
		return &gates.Build
	case "smoke":
		return &gates.Smoke
	case "verify":
		return &gates.Verify
	default:
		return nil
	}
}

func qualityGateFinalize(gates QualityGates) QualityGates {
	return QualityGates{
		Format:    finalizeGate(gates.Format),
		Lint:      finalizeGate(gates.Lint),
		Tests:     finalizeGate(gates.Tests),
		RaceTests: finalizeGate(gates.RaceTests),
		Typecheck: finalizeGate(gates.Typecheck),
		Security:  finalizeGate(gates.Security),
		Coverage:  finalizeGate(gates.Coverage),
		Build:     finalizeGate(gates.Build),
		Smoke:     finalizeGate(gates.Smoke),
		Verify:    finalizeGate(gates.Verify),
	}
}

func finalizeGate(gate QualityGate) QualityGate {
	gate.Commands = uniqueSorted(gate.Commands)
	gate.EvidenceRefs = uniqueSorted(gate.EvidenceRefs)
	gate.Confidence = confidenceFromGateStatus(gate.Status)

	return gate
}

func gateStatusFromCommand(command Command) string {
	if isCommandFailed(command) {
		return qualityGateStatusFailed
	}
	if command.Status == "success" {
		return qualityGateStatusPassed
	}

	if command.Status == "unknown" || command.ExitCode == nil {
		return qualityGateStatusUnknown
	}

	if *command.ExitCode == 0 {
		return qualityGateStatusPassed
	}

	return qualityGateStatusFailed
}

func combineGateStatus(current, next string) string {
	if next == qualityGateStatusFailed {
		return qualityGateStatusFailed
	}
	if current == qualityGateStatusNotRun {
		return next
	}
	if current == qualityGateStatusFailed {
		return qualityGateStatusFailed
	}
	if current == qualityGateStatusUnknown || next == qualityGateStatusUnknown {
		return qualityGateStatusUnknown
	}

	if next == qualityGateStatusPassed && current == qualityGateStatusNotRun {
		return qualityGateStatusPassed
	}

	return current
}

func confidenceFromGateStatus(status string) model.Confidence {
	switch status {
	case qualityGateStatusPassed:
		return model.ConfidenceMedium
	case qualityGateStatusFailed:
		return model.ConfidenceHigh
	case qualityGateStatusUnknown:
		return model.ConfidenceLowMedium
	case qualityGateStatusNotRun:
		return model.ConfidenceLow
	default:
		return model.ConfidenceLow
	}
}

func gateLastExitCode(current, candidate *int) *int {
	if candidate == nil {
		return current
	}

	return cloneInt(candidate)
}

func qualityGatesForCommand(command string, kind string) []string {
	command = strings.ToLower(strings.TrimSpace(redact(command)))
	gates := make([]string, 0)
	seen := map[string]bool{}
	appendGate := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		gates = append(gates, name)
	}

	if isFormatCommand(command) {
		appendGate("format")
	}
	if isSecurityCommand(command) {
		appendGate("security")
	}
	if isCoverageCommand(command) {
		appendGate("coverage")
	}
	if isBuildCommand(command) {
		appendGate("build")
	}
	if isSmokeCommand(command) {
		appendGate("smoke")
	}
	if isVerifyCommand(command) {
		appendGate("verify")
	}

	switch kind {
	case "lint":
		appendGate("lint")
	case "typecheck":
		appendGate("typecheck")
	case "test":
		appendGate("tests")
		if isRaceTestCommand(command) {
			appendGate("race_tests")
		}
		if isCoverageCommand(command) {
			appendGate("coverage")
		}
		if isVerifyCommand(command) {
			appendGate("verify")
		}
	default:
		if command == "make test" || command == "make verify" {
			appendGate("tests")
		}
	}

	return gates
}

func isFormatCommand(command string) bool {
	switch {
	case strings.Contains(command, "gofmt "), strings.Contains(command, " gofmt"):
		return true
	case strings.HasPrefix(command, "go fmt") || strings.Contains(command, " go fmt "):
		return true
	case strings.Contains(command, "prettier"):
		return true
	case strings.Contains(command, "rustfmt"):
		return true
	case strings.Contains(command, "gofumpt"):
		return true
	case strings.Contains(command, "black ") || strings.HasPrefix(command, "black"):
		return true
	case strings.Contains(command, "clang-format"):
		return true
	}

	return false
}

func isRaceTestCommand(command string) bool {
	return strings.Contains(command, " test -race") || strings.Contains(command, "-race")
}

func isCoverageCommand(command string) bool {
	switch {
	case strings.Contains(command, " -cover"):
		return true
	case strings.Contains(command, "coverage"):
		return true
	case strings.Contains(command, "coverprofile"):
		return true
	}

	return false
}

func isSecurityCommand(command string) bool {
	securityPatterns := []string{
		"gosec",
		"npm audit",
		"pnpm audit",
		"yarn audit",
		"snyk",
		"safety",
	}

	for _, pattern := range securityPatterns {
		if strings.Contains(command, pattern) {
			return true
		}
	}

	return false
}

func isBuildCommand(command string) bool {
	return strings.Contains(command, "go build") ||
		strings.Contains(command, "make build") ||
		strings.Contains(command, "npm run build") ||
		strings.Contains(command, "pnpm build") ||
		strings.Contains(command, "yarn build") ||
		strings.Contains(command, "cargo build")
}

func isSmokeCommand(command string) bool {
	return strings.Contains(command, "smoke")
}

func isVerifyCommand(command string) bool {
	return strings.HasPrefix(command, "make verify") || strings.HasPrefix(command, "agentreceipt verify")
}

func commandSignalsHas(signals []commandrisk.Classification, names ...string) bool {
	for _, signal := range signals {
		for _, target := range names {
			if signal.Signal == target {
				return true
			}
		}
	}

	return false
}

func isCommandFailed(command Command) bool {
	if command.Status == "failed" {
		return true
	}
	if command.ExitCode == nil {
		return false
	}

	return *command.ExitCode != 0
}

func isCommitLikeCommand(commandText string) bool {
	text := strings.TrimSpace(strings.ToLower(commandText))
	return strings.HasPrefix(text, "git commit ") ||
		text == "git commit" ||
		strings.HasPrefix(text, "git commit;") ||
		strings.Contains(text, "&& git commit")
}

func isReadCommand(commandText string) bool {
	commandText = strings.ToLower(strings.TrimSpace(commandText))
	switch {
	case commandText == "":
		return false
	case strings.HasPrefix(commandText, "cat >"):
		return false
	case strings.HasPrefix(commandText, "cat "):
		return true
	case strings.HasPrefix(commandText, "less "):
		return true
	case strings.HasPrefix(commandText, "more "):
		return true
	case strings.HasPrefix(commandText, "head "):
		return true
	case strings.HasPrefix(commandText, "tail "):
		return true
	case strings.HasPrefix(commandText, "grep "):
		return true
	case strings.HasPrefix(commandText, "rg "):
		return true
	case strings.HasPrefix(commandText, "find "):
		return true
	case strings.HasPrefix(commandText, "ls "):
		return true
	case strings.HasPrefix(commandText, "git diff"):
		return true
	case strings.HasPrefix(commandText, "git show"):
		return true
	case strings.HasPrefix(commandText, "git log"):
		return true
	case strings.HasPrefix(commandText, "git status"):
		return true
	}

	return false
}

func isWriteCommand(commandText string) bool {
	commandText = strings.TrimSpace(strings.ToLower(commandText))
	return strings.HasPrefix(commandText, "touch ") ||
		strings.HasPrefix(commandText, "mkdir ") ||
		strings.HasPrefix(commandText, "cp ") ||
		strings.HasPrefix(commandText, "mv ") ||
		strings.Contains(commandText, " > ") ||
		strings.Contains(commandText, ">>") ||
		strings.Contains(commandText, "tee ")
}

func isEditCommand(commandText string) bool {
	commandText = strings.ToLower(strings.TrimSpace(commandText))
	return strings.Contains(commandText, "sed -i") ||
		strings.Contains(commandText, "perl -pi") ||
		strings.Contains(commandText, "apply_patch") ||
		strings.Contains(commandText, "cat >")
}

func isTestFile(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	return strings.HasSuffix(path, "_test.go") ||
		strings.HasSuffix(path, "_test.ts") ||
		strings.HasSuffix(path, "_test.tsx") ||
		strings.HasSuffix(path, "_test.js") ||
		strings.HasSuffix(path, "_test.jsx") ||
		strings.HasSuffix(path, "_test.mjs") ||
		strings.HasSuffix(path, "_test.mts") ||
		strings.HasSuffix(path, "_test.cts")
}

func isDocFile(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	switch filepath.Ext(path) {
	case ".md", ".mdx", ".rst", ".txt", ".adoc":
		return true
	default:
		return false
	}
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

type patchFile struct {
	Path   string
	Action string
}

func buildFiles(classifier fswatcher.Classifier, changed []model.ChangedFile, finalPatch string, events []model.Event) []File {
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

	patchRefs := parseFinalPatchRefs(finalPatch)
	fileSummary := map[string]File{}
	for _, changedFile := range changed {
		path := filepath.ToSlash(changedFile.Path)
		fileSummary[path] = File{
			Path:         path,
			Action:       changedFile.Action,
			Sensitive:    changedFile.Sensitive,
			Dependency:   changedFile.Dependency,
			InFinalPatch: false,
			EvidenceRefs: uniqueSorted(fileRefs[path]),
		}
	}
	for _, file := range parseFinalPatchFiles(finalPatch) {
		existing, ok := fileSummary[file.Path]
		if !ok {
			classified := classifier.Classify(file.Path)
			existing = File{
				Path:       file.Path,
				Sensitive:  classified.Sensitive,
				Dependency: classified.Dependency,
			}
		}
		existing.Action = combineAction(existing.Action, file.Action)
		existing.InFinalPatch = true
		existing.EvidenceRefs = uniqueSorted(append(existing.EvidenceRefs, finalPatchEvidenceRef))
		fileSummary[file.Path] = existing
	}

	for _, file := range fileSummary {
		if _, patchRefIsPresent := patchRefs[file.Path]; patchRefIsPresent {
			file.InFinalPatch = true
		}
	}

	files := make([]File, 0, len(fileSummary))
	for _, file := range fileSummary {
		files = append(files, file)
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Path == files[j].Path {
			return files[i].Action < files[j].Action
		}
		return files[i].Path < files[j].Path
	})
	return files
}

func buildInstructionFiles(events []model.Event) []InstructionFile {
	instructionFiles := make([]InstructionFile, 0)
	for _, event := range events {
		if event.Source != instructions.Source || event.Type != instructions.TypeInstructionFile {
			continue
		}
		path := filepath.ToSlash(stringFromEventPayload(event.Payload, "path"))
		if path == "" {
			continue
		}
		instructionFiles = append(instructionFiles, InstructionFile{
			Path:    path,
			Hash:    stringFromEventPayload(event.Payload, "hash"),
			Size:    int64FromEventPayload(event.Payload, "size"),
			MTime:   stringFromEventPayload(event.Payload, "mtime"),
			Summary: stringSliceFromEventPayload(event.Payload, "summary"),
		})
	}

	sort.SliceStable(instructionFiles, func(i, j int) bool {
		if instructionFiles[i].Path == instructionFiles[j].Path {
			return instructionFiles[i].Hash < instructionFiles[j].Hash
		}
		return instructionFiles[i].Path < instructionFiles[j].Path
	})

	return instructionFiles
}

func parseFinalPatchRefs(patch string) map[string]bool {
	refs := map[string]bool{}
	for _, file := range parseFinalPatchFiles(patch) {
		refs[file.Path] = true
	}
	return refs
}

func parseFinalPatchFiles(patch string) []patchFile {
	lines := strings.Split(patch, "\n")
	files := make([]patchFile, 0)
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		left := diffPath(parts[2])
		right := diffPath(parts[3])
		action := "modify"
		for inner := index + 1; inner < len(lines); inner++ {
			next := strings.TrimSpace(lines[inner])
			if strings.HasPrefix(next, "diff --git ") {
				index = inner - 1
				break
			}
			switch {
			case strings.HasPrefix(next, "new file mode "):
				action = "add"
			case strings.HasPrefix(next, "deleted file mode "):
				action = "delete"
			case strings.HasPrefix(next, "rename from "):
				action = "rename"
				left = diffPath(strings.TrimPrefix(next, "rename from "))
			case strings.HasPrefix(next, "rename to "):
				action = "rename"
				right = diffPath(strings.TrimPrefix(next, "rename to "))
			}
		}
		switch action {
		case "delete":
			if left != "" {
				files = append(files, patchFile{Path: left, Action: action})
			}
		case "rename":
			if right != "" {
				files = append(files, patchFile{Path: right, Action: action})
			}
			if left != "" && left != right {
				files = append(files, patchFile{Path: left, Action: action})
			}
		default:
			if right != "" {
				files = append(files, patchFile{Path: right, Action: action})
				continue
			}
			if left != "" {
				files = append(files, patchFile{Path: left, Action: action})
			}
		}
	}
	return files
}

func combineAction(existing, next string) string {
	if next == "" || next == "modify" {
		return existing
	}
	if existing == "" || existing == "modify" {
		return next
	}
	if existing == next {
		return existing
	}
	return next
}

func diffPath(token string) string {
	t := strings.TrimSpace(token)
	if strings.HasPrefix(t, "a/") || strings.HasPrefix(t, "b/") {
		t = t[2:]
	}
	return strings.TrimPrefix(filepath.ToSlash(t), "./")
}

func buildFactualGaps(warnings []model.Warning, confidence model.CaptureConfidence) []string {
	gaps := make([]string, 0)
	if confidence.ProviderToolEvents == model.ConfidenceNone {
		gaps = append(gaps, "No provider tool events were observed.")
	}
	for _, warning := range warnings {
		if warning.Message != "" {
			gaps = append(gaps, warning.Message)
		}
	}

	return gaps
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

func buildVerification(layout storage.Layout, _ bool, options Options) (Verification, []string) {
	verification := Verification{}
	verificationWarnings := make([]string, 0)
	verification.TrustStatus = trustStatusNotConfigured
	verification.PolicyValid = true
	verification.SignerTrusted = false

	if chainHash, err := eventHash(layout.EventsJSONL); err == nil {
		verification.EventChainHash = chainHash
	} else {
		verificationWarnings = append(verificationWarnings, "Event chain verification failed")
		verificationWarnings = append(verificationWarnings, err.Error())
	}
	if diffHash, err := fileHash(layout.FinalPatch); err == nil {
		verification.DiffHash = diffHash
	} else {
		verificationWarnings = append(verificationWarnings, "Final patch hash verification failed")
		verificationWarnings = append(verificationWarnings, err.Error())
	}
	if manifestHash, err := fileHash(layout.ManifestJSON); err == nil {
		verification.ManifestHash = manifestHash
	} else {
		verificationWarnings = append(verificationWarnings, "Manifest hash verification failed")
		verificationWarnings = append(verificationWarnings, err.Error())
	}
	if receiptHash, err := fileHash(layout.ReceiptJSON); err == nil {
		if rec, err := receipt.Read(layout); err == nil && rec.Verification.ReceiptHash != "" {
			verification.ReceiptHash = rec.Verification.ReceiptHash
			if receiptHash == "" {
				verification.ReceiptHash = ""
			}
		} else if err != nil {
			verification.ReceiptHash = receiptHash
			verificationWarnings = append(verificationWarnings, "Receipt hash read failed")
			verificationWarnings = append(verificationWarnings, err.Error())
		}
	} else {
		verificationWarnings = append(verificationWarnings, "Receipt hash verification failed")
		verificationWarnings = append(verificationWarnings, err.Error())
	}

	verificationResult, err := receipt.VerifyBundle(layout.Session)
	if err != nil {
		verificationWarnings = append(verificationWarnings, err.Error())
	}

	verification.Valid = verificationResult.Valid
	verification.EventChainValid = verificationResult.EventChain
	verification.FinalPatchHashValid = verificationResult.FinalDiffHash
	verification.ManifestHashValid = verificationResult.ManifestHash
	verification.ReceiptHashValid = verificationResult.ReceiptHash
	verification.SignatureValid = verificationResult.Signature
	verification.SignatureError = verificationResult.SignatureError
	verification.SignatureErrorCode = verificationResult.SignatureErrorCode
	verification.SignedBy = verificationResult.SignedBy
	verification.IntegrityValid = verification.EventChainValid && verification.FinalPatchHashValid && verification.ManifestHashValid && verification.ReceiptHashValid
	verification.AuthenticityValid = verification.SignatureValid
	if verification.SignatureErrorCode == receiptErrLegacySignerMissing {
		verification.AuthenticityStatus = authenticityStatusUnverifiable
	} else if verificationResult.Signature {
		verification.AuthenticityStatus = authenticityStatusAuthentic
	} else {
		verification.AuthenticityStatus = authenticityStatusFailed
	}

	trustEvaluation := trust.EvaluateSignerTrust(verification.SignedBy, options.TrustedSignerKeyIDs)
	verification.PolicyValid = trustEvaluation.PolicyValid
	verification.TrustStatus = trustEvaluation.TrustStatus
	verification.SignerTrusted = trustEvaluation.SignerTrusted
	if !trustEvaluation.PolicyValid {
		verification.TrustStatus = trustStatusPolicyInvalid
	}

	if verification.SignatureErrorCode == receiptErrLegacySignerMissing {
		if verification.IntegrityValid {
			verification.OverallVerdict = verificationVerdictIntegrityOnly
			verification.OverallReason = "signature unverifiable due to missing embedded signer key material"
		} else {
			verification.OverallVerdict = verificationVerdictIntegrityFail
			verification.OverallReason = "integrity checks failed while signature key material is missing"
		}
	} else if !verification.EventChainValid || !verification.FinalPatchHashValid || !verification.ManifestHashValid || !verification.ReceiptHashValid {
		verification.OverallVerdict = verificationVerdictIntegrityFail
		verification.OverallReason = "integrity check failure"
	} else if !verification.SignatureValid {
		verification.OverallVerdict = verificationVerdictUntrusted
		verification.OverallReason = "signature verification failed"
	} else if !trustEvaluation.PolicyValid {
		verification.OverallVerdict = verificationVerdictPolicyFailure
		verification.OverallReason = "trust policy is invalid"
	} else if trustEvaluation.TrustStatus == trustStatusNotTrusted {
		verification.OverallVerdict = verificationVerdictUntrusted
		verification.OverallReason = "signer is not trusted"
	} else {
		verification.OverallVerdict = verificationVerdictPassed
		verification.OverallReason = "integrity checks and signature verification passed"
	}

	if verification.IntegrityValid && verification.SignatureValid {
		verification.Valid = true
	}

	for _, check := range []VerificationCheck{
		{Name: "event_chain", Valid: verification.EventChainValid, Reason: verificationResult.EventChainError},
		{Name: "final_patch_hash", Valid: verification.FinalPatchHashValid, Reason: verificationResult.FinalDiffHashError},
		{Name: "manifest_hash", Valid: verification.ManifestHashValid, Reason: verificationResult.ManifestHashError},
		{Name: "receipt_hash", Valid: verification.ReceiptHashValid, Reason: verificationResult.ReceiptHashError},
		{Name: "signature", Valid: verification.SignatureValid, Reason: verification.SignatureError},
	} {
		if !check.Valid && check.Reason == "" {
			switch check.Name {
			case "event_chain":
				check.Reason = "event chain mismatch"
			case "final_patch_hash":
				check.Reason = "final patch hash mismatch"
			case "manifest_hash":
				check.Reason = "manifest hash mismatch"
			case "receipt_hash":
				check.Reason = "receipt hash mismatch"
			case "signature":
				check.Reason = "signature verification failed"
			}
		}
		if check.Valid {
			check.Reason = ""
		}
		verification.ComponentResults = append(verification.ComponentResults, check)
		if !check.Valid {
			switch check.Name {
			case "event_chain":
				verificationWarnings = append(verificationWarnings, "Event chain verification failed")
			case "final_patch_hash":
				verificationWarnings = append(verificationWarnings, "Final patch hash verification failed")
			case "manifest_hash":
				verificationWarnings = append(verificationWarnings, "Manifest hash verification failed")
			case "receipt_hash":
				verificationWarnings = append(verificationWarnings, "Receipt hash verification failed")
			case "signature":
				verificationWarnings = append(verificationWarnings, "Signature verification failed")
			}
		}
	}

	verification.ComponentResults = uniqueComponentResults(verification.ComponentResults)

	if verificationWarnings := uniqueSorted(verificationWarnings); len(verificationWarnings) > 0 {
		return verification, verificationWarnings
	}

	return verification, uniqueSorted(verificationWarnings)
}

func uniqueComponentResults(checks []VerificationCheck) []VerificationCheck {
	seen := map[string]VerificationCheck{}
	for _, check := range checks {
		if check.Name == "" {
			continue
		}
		if prev, ok := seen[check.Name]; ok {
			if !check.Valid {
				seen[check.Name] = check
			}
			if check.Reason != "" {
				prev.Reason = check.Reason
				seen[check.Name] = prev
			}
			continue
		}
		seen[check.Name] = check
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]VerificationCheck, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
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

func int64FromEventPayload(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	value := payload[key]
	switch numeric := value.(type) {
	case int:
		return int64(numeric)
	case int64:
		return numeric
	case int32:
		return int64(numeric)
	case float64:
		return int64(numeric)
	case float32:
		return int64(numeric)
	case json.Number:
		parsed, err := numeric.Int64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func stringSliceFromEventPayload(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		s, ok := value.(string)
		if !ok || s == "" {
			continue
		}
		values = append(values, s)
	}

	return values
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

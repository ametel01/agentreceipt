package replay

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ametel01/agentreceipt/internal/model"
)

const focusKind = "agentreceipt.session_focus"

const (
	focusTopReasonLimit = 5
	focusTaskLimit      = 20
)

const (
	focusReasonBlock  = "block"
	focusReasonReview = "review"
)

const (
	focusTaskPriorityP0 = "P0"
	focusTaskPriorityP1 = "P1"
	focusTaskPriorityP2 = "P2"
	focusTaskPriorityP3 = "P3"
)

type FocusVerdict string

const (
	focusVerdictPass           FocusVerdict = "pass"
	focusVerdictReviewRequired FocusVerdict = "review_required"
	focusVerdictBlock          FocusVerdict = "block"
	focusVerdictUnverifiable   FocusVerdict = "unverifiable"
)

// FocusReport is a compact reviewer-agent output for a replay session.
type FocusReport struct {
	SchemaVersion    int                    `json:"schema_version"`
	Kind             string                 `json:"kind"`
	SessionID        string                 `json:"session_id"`
	GeneratedAt      time.Time              `json:"generated_at"`
	Verdict          FocusVerdict           `json:"verdict"`
	TopReasons       []string               `json:"top_reasons,omitempty"`
	ReviewTasks      []ReviewTask           `json:"review_tasks,omitempty"`
	ChangedFiles     []FocusChangedFile     `json:"changed_files,omitempty"`
	FailedGates      []FailedGate           `json:"failed_gates,omitempty"`
	WorkspaceChanges WorkspaceChangeSummary `json:"workspace_change_summary"`
	InstructionFiles []InstructionFile      `json:"instruction_files,omitempty"`
	EvidenceRefs     []string               `json:"evidence_refs,omitempty"`
}

type FocusChangedFile struct {
	Path                 string   `json:"path"`
	Action               string   `json:"action,omitempty"`
	Category             string   `json:"category,omitempty"`
	Sensitive            bool     `json:"sensitive,omitempty"`
	Dependency           bool     `json:"dependency,omitempty"`
	Symbols              []string `json:"symbols,omitempty"`
	ReadBeforeEdit       string   `json:"read_before_edit,omitempty"`
	RelatedContextRead   string   `json:"related_context_read,omitempty"`
	TestsRelated         []string `json:"tests_related,omitempty"`
	CommandsTouchingFile []string `json:"commands_touching_file,omitempty"`
	ReviewReasons        []string `json:"review_reasons,omitempty"`
	EvidenceRefs         []string `json:"evidence_refs,omitempty"`
}

type FailedGate struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type FocusReason struct {
	Message     string
	Refs        []string
	Confidence  model.Confidence
	Kind        string
	FromMissing bool
}

type ReviewTask struct {
	ID           string           `json:"id,omitempty"`
	Priority     string           `json:"priority"`
	Kind         string           `json:"kind"`
	Question     string           `json:"question"`
	Paths        []string         `json:"paths,omitempty"`
	Symbols      []string         `json:"symbols,omitempty"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
	Confidence   model.Confidence `json:"confidence,omitempty"`
	Source       string           `json:"source,omitempty"`
}

// BuildFocusReport builds a compact reviewer-agent focus report from an existing replay report.
func BuildFocusReport(replay Report) FocusReport {
	focus := FocusReport{
		SchemaVersion: replay.SchemaVersion,
		Kind:          focusKind,
		SessionID:     replay.SessionID,
		GeneratedAt:   replay.GeneratedAt,
	}

	focus.ChangedFiles = collectFocusChangedFiles(replay)
	focus.InstructionFiles = replay.InstructionFiles
	focus.FailedGates = collectFailedGates(replay.QualityGates)
	focus.WorkspaceChanges = replay.WorkspaceChange

	reasons, tasks := collectFocusReasonsAndTasks(replay, focus.FailedGates)
	focus.Verdict = determineFocusVerdict(replay, reasons)
	focus.TopReasons = capSortedStrings(focusReasonsToStrings(reasons), focusTopReasonLimit)

	for index := range tasks {
		tasks[index].ID = fmt.Sprintf("task_%03d", index+1)
	}
	focus.ReviewTasks = capSortedReviewTasks(tasks, focusTaskLimit)
	focus.EvidenceRefs = capSortedStrings(collectFocusEvidenceRefs(reasons, focus.ReviewTasks, focus.FailedGates), 200)

	return focus
}

func collectFocusReasonsAndTasks(replay Report, failedGates []FailedGate) ([]FocusReason, []ReviewTask) {
	reasons := make([]FocusReason, 0)
	tasks := make([]ReviewTask, 0)
	symbolsByPath := patchSymbolsByPath(replay.PatchSummary.ChangedFiles)
	replayFiles := replay.Files

	addReason := func(message string, refs []string, kind string, fromMissing bool) {
		if message == "" {
			return
		}
		reasons = append(reasons, FocusReason{
			Message:     message,
			Refs:        uniqueSorted(refs),
			Kind:        kind,
			FromMissing: fromMissing,
		})
	}

	addTask := func(kind string, priority string, question string, paths []string, symbols []string, refs []string, confidence model.Confidence, source string) {
		if question == "" {
			return
		}
		tasks = append(tasks, ReviewTask{
			Priority:     priority,
			Kind:         kind,
			Question:     question,
			Paths:        uniqueSorted(paths),
			Symbols:      uniqueSorted(symbols),
			EvidenceRefs: uniqueSorted(refs),
			Confidence:   confidence,
			Source:       source,
		})
	}

	if !replay.Verification.IntegrityValid {
		addReason("Integrity verification failed.", verificationEvidenceRefs(), focusReasonBlock, false)
		addTask(
			"integrity_failure",
			focusTaskPriorityP0,
			"Inspect integrity evidence before using this focus report.",
			nil,
			nil,
			verificationEvidenceRefs(),
			model.ConfidenceHigh,
			"verification",
		)
	}

	if replay.Verification.IntegrityValid && !replay.Verification.FinalPatchHashValid {
		addReason("Final patch mismatch detected.", []string{finalPatchEvidenceRef}, focusReasonBlock, false)
		addTask(
			"diff_mismatch",
			focusTaskPriorityP0,
			"Resolve final patch mismatch before review can continue.",
			nil,
			nil,
			[]string{finalPatchEvidenceRef},
			model.ConfidenceHigh,
			"verification",
		)
	}

	if len(replay.WorkspaceChange.PreExistingDirtyFiles) > 0 {
		addReason("Session started with pre-existing dirty files: "+strings.Join(replay.WorkspaceChange.PreExistingDirtyFiles, ", "), nil, focusReasonReview, true)
		addTask(
			"pre_existing_dirty",
			focusTaskPriorityP2,
			"Review pre-existing dirty files before evaluating agent changes.",
			replay.WorkspaceChange.PreExistingDirtyFiles,
			nil,
			nil,
			model.ConfidenceMedium,
			"workspace",
		)
	}

	if len(replay.WorkspaceChange.AgentTouchedPreExistingFiles) > 0 {
		addReason("Session modified pre-existing dirty files: "+strings.Join(replay.WorkspaceChange.AgentTouchedPreExistingFiles, ", "), nil, focusReasonReview, true)
		addTask(
			"pre_existing_touched",
			focusTaskPriorityP1,
			"Review edits to pre-existing dirty files.",
			replay.WorkspaceChange.AgentTouchedPreExistingFiles,
			nil,
			nil,
			model.ConfidenceHigh,
			"workspace",
		)
	}

	if workspaceSummaryHasComparableChanges(replay.WorkspaceChange) && !replay.WorkspaceChange.FinalDiffMatchesWorkspace {
		addReason("Final patch does not match the current workspace diff.", []string{finalPatchEvidenceRef}, focusReasonBlock, false)
		addTask(
			"diff_mismatch",
			focusTaskPriorityP0,
			"Resolve final patch mismatch against current workspace before review can continue.",
			nil,
			nil,
			[]string{finalPatchEvidenceRef},
			model.ConfidenceHigh,
			"verification",
		)
	}

	if workspaceSummaryHasComparableChanges(replay.WorkspaceChange) && !replay.WorkspaceChange.FinalDiffMatchesBranch {
		addReason("Final patch does not match current branch workspace diff.", nil, focusReasonReview, true)
		addTask(
			"branch_diff_mismatch",
			focusTaskPriorityP2,
			"Review whether the final patch aligns with current branch diff expectations.",
			nil,
			nil,
			[]string{finalPatchEvidenceRef},
			model.ConfidenceMedium,
			"verification",
		)
	}

	for _, gate := range failedGates {
		addReason("Quality gate "+gate.Name+" failed.", gate.EvidenceRefs, focusReasonBlock, false)
		addTask(
			"failed_gate",
			focusTaskPriorityP0,
			"Resolve failed quality gate: "+gate.Name+".",
			nil,
			nil,
			gate.EvidenceRefs,
			model.ConfidenceHigh,
			"quality_gate",
		)
	}

	if replay.Verification.IntegrityValid &&
		(replay.Verification.AuthenticityStatus == authenticityStatusUnverifiable || replay.Verification.TrustStatus == trustStatusNotTrusted) {
		addTask(
			"authenticity_unverifiable",
			focusTaskPriorityP1,
			"Inspect authenticity and trust evidence before using this focus report.",
			nil,
			nil,
			verificationEvidenceRefs(),
			model.ConfidenceMedium,
			"verification",
		)
	}

	for _, check := range replay.PolicyChecks {
		switch check.Status {
		case policyCheckStatusFail:
			addReason(check.Message, check.EvidenceRefs, focusReasonBlock, false)
			addTask(
				"failed_policy_check",
				focusTaskPriorityP0,
				check.Message,
				policyCheckPaths(check.Name, replay.Files),
				nil,
				check.EvidenceRefs,
				check.Confidence,
				"policy",
			)
		case policyCheckStatusWarn:
			addReason(check.Message, check.EvidenceRefs, focusReasonReview, true)
			addTask(
				"evidence_gap",
				policyCheckPriority(check.Name),
				check.Message,
				policyCheckPaths(check.Name, replay.Files),
				nil,
				check.EvidenceRefs,
				check.Confidence,
				"policy",
			)
		case policyCheckStatusUnknown:
			addReason(check.Message, check.EvidenceRefs, focusReasonReview, true)
			addTask(
				"evidence_gap",
				focusTaskPriorityP2,
				check.Message,
				policyCheckPaths(check.Name, replay.Files),
				nil,
				check.EvidenceRefs,
				check.Confidence,
				"policy",
			)
		}
	}

	if hasFailedCommands(replay.Commands) {
		failedRefs := failedCommandRefs(replay.Commands)
		if len(failedRefs) > 0 || len(replay.FailedCommandDetails) > 0 {
			addReason("Failed command execution was observed.", failedRefs, focusReasonBlock, false)
			addTask(
				"failed_command",
				focusTaskPriorityP0,
				"Review failed commands before approving the session.",
				nil,
				nil,
				failedRefs,
				model.ConfidenceHigh,
				"commands",
			)
		}
		for _, command := range replay.Commands {
			if command.Status != "failed" {
				continue
			}
			paths, symbols := commandRelatedEvidence(command.Command, replayFiles, symbolsByPath)
			addTask(
				"failed_command",
				focusTaskPriorityP0,
				"Failed command: "+command.Command,
				paths,
				symbols,
				command.EvidenceRefs,
				command.Confidence,
				"commands",
			)
		}
		for _, detail := range replay.FailedCommandDetails {
			question := strings.TrimSpace(detail.FailedReason)
			if question == "" {
				question = strings.TrimSpace(detail.StderrOrErrorSummary)
			}
			if question == "" {
				question = strings.TrimSpace(detail.StdoutSummary)
			}
			if question == "" {
				question = "Failed command details were observed."
			}
			addTask(
				"failed_command",
				focusTaskPriorityP0,
				"Review failed command detail: "+question,
				nil,
				nil,
				detail.EvidenceRefs,
				detail.Confidence,
				"commands",
			)
		}
	}

	for _, namedGate := range listQualityGates(replay.QualityGates) {
		if namedGate.status == qualityGateStatusNotRun || namedGate.status == qualityGateStatusUnknown {
			addReason("Quality gate "+namedGate.name+" was not run.", namedGate.gate.EvidenceRefs, focusReasonReview, true)
			addTask(
				"missing_gate",
				focusTaskPriorityP2,
				"Confirm quality gate "+namedGate.name+" or record explicit rationale.",
				nil,
				nil,
				namedGate.gate.EvidenceRefs,
				model.ConfidenceLow,
				"quality_gate",
			)
		}
	}

	if replay.PatchSummary.ProductionChangedWithoutTestsChanged {
		refs := patchSummaryCategoryEvidenceRefs(replay.PatchSummary, patchCategoryProduction, patchCategoryTest)
		addReason("Production code changed without test file changes.", refs, focusReasonReview, true)
		addTask(
			"missing_test",
			focusTaskPriorityP1,
			"Add or update tests for production code changes.",
			changedGoTestPaths(replay.PatchSummary),
			nil,
			refs,
			model.ConfidenceMedium,
			"patch",
		)
	}

	for _, file := range replay.PatchSummary.ChangedFiles {
		switch {
		case file.Dependency:
			addReason("Dependency file changed.", file.EvidenceRefs, focusReasonReview, true)
			addTask(
				"dependency_change",
				focusTaskPriorityP1,
				"Review dependency file change: "+file.Path,
				[]string{file.Path},
				file.Symbols,
				file.EvidenceRefs,
				model.ConfidenceMedium,
				"patch",
			)
		case file.Sensitive:
			addReason("Sensitive file changed.", file.EvidenceRefs, focusReasonReview, true)
			addTask(
				"sensitive_change",
				focusTaskPriorityP1,
				"Review sensitive file change: "+file.Path,
				[]string{file.Path},
				file.Symbols,
				file.EvidenceRefs,
				model.ConfidenceHigh,
				"patch",
			)
		case file.Category == patchCategoryGeneratedOrUnknown:
			addReason("Generated or unknown file changed.", file.EvidenceRefs, focusReasonReview, true)
			addTask(
				"generated_change",
				focusTaskPriorityP2,
				"Validate generated/unknown file change: "+file.Path,
				[]string{file.Path},
				file.Symbols,
				file.EvidenceRefs,
				model.ConfidenceMedium,
				"patch",
			)
		case file.Category == patchCategoryDependency:
			addReason("Dependency file changed.", file.EvidenceRefs, focusReasonReview, true)
			addTask(
				"dependency_change",
				focusTaskPriorityP1,
				"Review dependency file change: "+file.Path,
				[]string{file.Path},
				file.Symbols,
				file.EvidenceRefs,
				model.ConfidenceMedium,
				"patch",
			)
		case file.Category == patchCategoryDocs:
			addTask(
				"risky_file",
				focusTaskPriorityP3,
				"Review docs/config file change: "+file.Path,
				[]string{file.Path},
				file.Symbols,
				file.EvidenceRefs,
				model.ConfidenceLowMedium,
				"patch",
			)
		}
	}

	if replay.Source.SessionState != model.SessionStateFinalized {
		addReason("Session is not finalized ("+string(replay.Source.SessionState)+").", []string{"manifest.json"}, focusReasonReview, true)
		addTask(
			"evidence_gap",
			focusTaskPriorityP3,
			"Wait for a finalized session before deterministic review.",
			nil,
			nil,
			[]string{"manifest.json"},
			model.ConfidenceHigh,
			"session",
		)
	}

	for _, item := range replay.ReviewFocus {
		addTask(
			"evidence_gap",
			focusTaskPriorityP3,
			item.Message,
			nil,
			nil,
			item.EvidenceRefs,
			item.Confidence,
			"review_focus",
		)
	}

	reasons = dedupeFocusReasons(reasons)
	sort.SliceStable(reasons, func(i, j int) bool {
		return reasons[i].Message < reasons[j].Message
	})

	uniqueTasks := uniqueReviewTasks(tasks)
	sort.SliceStable(uniqueTasks, func(i, j int) bool {
		return compareFocusTasks(uniqueTasks[i], uniqueTasks[j])
	})

	return reasons, uniqueTasks
}

func workspaceSummaryHasComparableChanges(summary WorkspaceChangeSummary) bool {
	return len(summary.AgentCreatedChanges) > 0 ||
		len(summary.AgentModifiedCleanFiles) > 0 ||
		len(summary.AgentTouchedPreExistingFiles) > 0 ||
		len(summary.PreExistingDirtyFiles) > 0
}

func determineFocusVerdict(replay Report, reasons []FocusReason) FocusVerdict {
	for _, reason := range reasons {
		if reason.Kind == focusReasonBlock {
			return focusVerdictBlock
		}
	}

	if replay.Verification.IntegrityValid &&
		(replay.Verification.AuthenticityStatus == authenticityStatusUnverifiable ||
			replay.Verification.TrustStatus == trustStatusNotTrusted) {
		return focusVerdictUnverifiable
	}

	for _, reason := range reasons {
		if reason.Kind == focusReasonReview {
			return focusVerdictReviewRequired
		}
	}

	if replay.Source.SessionState == model.SessionStateFinalized &&
		replay.Verification.IntegrityValid &&
		replay.Verification.AuthenticityStatus == authenticityStatusAuthentic &&
		replay.Verification.TrustStatus == trustStatusTrusted &&
		allGatesPassed(replay.QualityGates) &&
		allPolicyChecksPass(replay.PolicyChecks) &&
		len(replay.Gaps) == 0 &&
		!hasFailedCommands(replay.Commands) {
		return focusVerdictPass
	}

	return focusVerdictReviewRequired
}

func allGatesPassed(gates QualityGates) bool {
	for _, named := range listQualityGates(gates) {
		if named.status != qualityGateStatusPassed {
			return false
		}
	}
	return true
}

func allPolicyChecksPass(checks []PolicyCheck) bool {
	for _, check := range checks {
		switch check.Status {
		case policyCheckStatusFail, policyCheckStatusWarn, policyCheckStatusUnknown:
			return false
		}
	}
	return true
}

type namedQualityGate struct {
	name   string
	status string
	gate   QualityGate
}

func listQualityGates(gates QualityGates) []namedQualityGate {
	return []namedQualityGate{
		{name: "format", status: gates.Format.Status, gate: gates.Format},
		{name: "lint", status: gates.Lint.Status, gate: gates.Lint},
		{name: "tests", status: gates.Tests.Status, gate: gates.Tests},
		{name: "race_tests", status: gates.RaceTests.Status, gate: gates.RaceTests},
		{name: "typecheck", status: gates.Typecheck.Status, gate: gates.Typecheck},
		{name: "security", status: gates.Security.Status, gate: gates.Security},
		{name: "coverage", status: gates.Coverage.Status, gate: gates.Coverage},
		{name: "build", status: gates.Build.Status, gate: gates.Build},
		{name: "smoke", status: gates.Smoke.Status, gate: gates.Smoke},
		{name: "verify", status: gates.Verify.Status, gate: gates.Verify},
	}
}

func collectFailedGates(gates QualityGates) []FailedGate {
	failed := make([]FailedGate, 0)
	for _, namedGate := range listQualityGates(gates) {
		if namedGate.status != qualityGateStatusFailed {
			continue
		}
		failed = append(failed, FailedGate{
			Name:         namedGate.name,
			Status:       namedGate.status,
			EvidenceRefs: uniqueSorted(append([]string(nil), namedGate.gate.EvidenceRefs...)),
		})
	}

	sort.SliceStable(failed, func(i, j int) bool {
		if failed[i].Name == failed[j].Name {
			return failed[i].Status < failed[j].Status
		}
		return failed[i].Name < failed[j].Name
	})

	return failed
}

func collectFocusChangedFiles(replay Report) []FocusChangedFile {
	source := replay.PatchSummary.ChangedFiles
	if len(source) == 0 {
		source = make([]PatchSummaryFile, 0, len(replay.Files))
		for _, file := range replay.Files {
			source = append(source, PatchSummaryFile{
				Path:         file.Path,
				Action:       file.Action,
				Category:     focusFileCategory(file),
				Sensitive:    file.Sensitive,
				Dependency:   file.Dependency,
				EvidenceRefs: file.EvidenceRefs,
			})
		}
	}
	if len(source) == 0 {
		return nil
	}

	filePaths := make([]string, 0, len(source))
	filesByPath := make(map[string]FocusChangedFile, len(source))
	for _, file := range source {
		if file.Path == "" {
			continue
		}
		filesByPath[file.Path] = FocusChangedFile{
			Path:         file.Path,
			Action:       file.Action,
			Category:     file.Category,
			Sensitive:    file.Sensitive,
			Dependency:   file.Dependency,
			Symbols:      uniqueSorted(file.Symbols),
			EvidenceRefs: uniqueSorted(file.EvidenceRefs),
		}
	}
	for path := range filesByPath {
		filePaths = append(filePaths, path)
	}

	policyChecksByName := policyChecksByName(replay.PolicyChecks)
	commandsByFile := focusCommandAssociations(replay.Commands, filePaths)
	evidence := commandEvidenceFromAssociations(commandsByFile)

	readBeforeEditStatus := policyCheckStatusForFile(filePaths, policyChecksByName["target_file_read_before_edit"])
	relatedContextStatus := policyCheckStatusForFile(filePaths, policyChecksByName["related_context_read_before_edit"])

	hasTestCommand := hasAnyTestCommand(replay.Commands)

	changed := make([]FocusChangedFile, 0, len(filesByPath))
	for path, file := range filesByPath {
		file.ReadBeforeEdit = readBeforeEditStatus[path]
		file.RelatedContextRead = relatedContextStatus[path]
		file.CommandsTouchingFile = uniqueSorted(commandsByPath(commandsByFile[path]))
		file.TestsRelated = uniqueSorted(commandsByPathTests(commandsByFile[path]))
		file.EvidenceRefs = uniqueSorted(append(file.EvidenceRefs, evidence[path]...))
		if check, ok := policyChecksByName["target_file_read_before_edit"]; ok {
			file.EvidenceRefs = uniqueSorted(append(file.EvidenceRefs, check.EvidenceRefs...))
		}
		if check, ok := policyChecksByName["related_context_read_before_edit"]; ok {
			file.EvidenceRefs = uniqueSorted(append(file.EvidenceRefs, check.EvidenceRefs...))
		}

		file.ReviewReasons = collectFocusFileReasons(
			file,
			readBeforeEditStatus[path],
			relatedContextStatus[path],
			commandsByFile[path],
			hasTestCommand,
			replay.PatchSummary,
			policyChecksByName,
		)

		changed = append(changed, file)
	}

	sort.SliceStable(changed, func(i, j int) bool {
		if changed[i].Path == changed[j].Path {
			return changed[i].Action < changed[j].Action
		}
		return changed[i].Path < changed[j].Path
	})
	return changed
}

type focusCommandAssociation struct {
	commands     []string
	commandRefs  []string
	testCommands []string
	failedFiles  []string
}

func focusCommandAssociations(commands []Command, filePaths []string) map[string]focusCommandAssociation {
	associations := make(map[string]focusCommandAssociation, len(filePaths))
	for _, path := range filePaths {
		associations[path] = focusCommandAssociation{}
	}

	for _, command := range commands {
		commandText := strings.TrimSpace(command.Command)
		for _, path := range filePaths {
			if !commandTouchesFilePath(commandText, path) {
				continue
			}
			association := associations[path]
			association.commands = append(association.commands, command.Command)
			association.commandRefs = append(association.commandRefs, command.EvidenceRefs...)
			if isLikelyTestCommand(command.Command) {
				association.testCommands = append(association.testCommands, command.Command)
			}
			if command.Status == "failed" {
				association.failedFiles = append(association.failedFiles, command.Command)
			}
			associations[path] = association
		}
	}

	return associations
}

func commandEvidenceFromAssociations(associations map[string]focusCommandAssociation) map[string][]string {
	evidence := make(map[string][]string, len(associations))
	for path, association := range associations {
		evidence[path] = uniqueSorted(append([]string(nil), association.commandRefs...))
	}
	return evidence
}

func commandsByPath(association focusCommandAssociation) []string {
	return uniqueSorted(append([]string(nil), association.commands...))
}

func commandsByPathTests(association focusCommandAssociation) []string {
	return uniqueSorted(append([]string(nil), association.testCommands...))
}

func hasAnyTestCommand(commands []Command) bool {
	for _, command := range commands {
		if isLikelyTestCommand(command.Command) {
			return true
		}
	}
	return false
}

func policyChecksByName(checks []PolicyCheck) map[string]PolicyCheck {
	m := make(map[string]PolicyCheck, len(checks))
	for _, check := range checks {
		if check.Name == "" {
			continue
		}
		m[check.Name] = check
	}
	return m
}

func policyCheckStatusForFile(paths []string, check PolicyCheck) map[string]string {
	statuses := make(map[string]string, len(paths))
	for _, path := range paths {
		statuses[path] = policyCheckStatusNotApplicable
	}
	if check.Name == "" {
		return statuses
	}
	for _, path := range paths {
		statuses[path] = check.Status
	}
	return statuses
}

func collectFocusFileReasons(
	file FocusChangedFile,
	readBeforeEditStatus string,
	relatedContextStatus string,
	association focusCommandAssociation,
	hasTestCommand bool,
	summary PatchSummary,
	policyChecksByName map[string]PolicyCheck,
) []string {
	reasons := make([]string, 0)

	switch {
	case file.Dependency || file.Category == patchCategoryDependency:
		reasons = append(reasons, "Dependency file changed.")
	case file.Category == patchCategoryGeneratedOrUnknown:
		reasons = append(reasons, "Generated or unknown file changed.")
	}

	if file.Sensitive {
		reasons = append(reasons, "Sensitive file changed.")
	}
	if isCIOrSecurityPatchFile(file.Path) {
		reasons = append(reasons, "CI or security file changed.")
	}
	if summary.ProductionChangedWithoutTestsChanged && file.Category == patchCategoryProduction {
		reasons = append(reasons, "Production code changed without test file changes.")
	}

	if readBeforeEditStatus == policyCheckStatusFail || readBeforeEditStatus == policyCheckStatusWarn || readBeforeEditStatus == policyCheckStatusUnknown {
		if check, ok := policyChecksByName["target_file_read_before_edit"]; ok && check.Message != "" {
			reasons = append(reasons, check.Message)
		}
	}
	if relatedContextStatus == policyCheckStatusFail || relatedContextStatus == policyCheckStatusWarn || relatedContextStatus == policyCheckStatusUnknown {
		if check, ok := policyChecksByName["related_context_read_before_edit"]; ok && check.Message != "" {
			reasons = append(reasons, check.Message)
		}
	}
	if len(association.failedFiles) > 0 {
		reasons = append(reasons, "A failed command touched this file.")
	}
	if file.Category == patchCategoryProduction && hasTestCommand && len(file.TestsRelated) == 0 {
		reasons = append(reasons, "No file-specific test command was identified for this file.")
	}

	return uniqueSorted(reasons)
}

func commandTouchesFilePath(commandText string, filePath string) bool {
	commandText = strings.ToLower(filepath.ToSlash(strings.TrimSpace(commandText)))
	filePath = strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	if commandText == "" || filePath == "" {
		return false
	}
	filePath = strings.TrimPrefix(filePath, "./")
	fileDir := strings.ToLower(filepath.ToSlash(filepath.Dir(filePath)))
	if fileDir == "." {
		fileDir = ""
	}
	fileBase := filepath.Base(filePath)

	for _, token := range commandPathTokens(commandText) {
		if commandPathTokenReferencesFile(token, filePath, fileDir, fileBase) {
			return true
		}
	}

	return false
}

func commandPathTokens(commandText string) []string {
	return strings.FieldsFunc(commandText, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case '&', ';', '|', '(', ')', '{', '}', '[', ']', '<', '>', '"', '\'', '`', ',':
			return true
		default:
			return false
		}
	})
}

func commandPathTokenReferencesFile(token string, filePath string, fileDir string, fileBase string) bool {
	token = strings.ToLower(filepath.ToSlash(strings.TrimSpace(token)))
	if token == "" {
		return false
	}
	token = strings.TrimPrefix(token, "./")
	if token == "" {
		return false
	}

	if token == filePath || token == fileBase {
		return true
	}
	if fileDir != "" && token == fileDir {
		return true
	}

	if token == "..." || token == "." || token == "./" || token == "../" {
		return false
	}
	if strings.HasSuffix(token, "/...") {
		prefix := strings.TrimSuffix(token, "/...")
		prefix = strings.TrimPrefix(prefix, "./")
		if prefix == "" || prefix == "." {
			return false
		}
		return pathHasPrefix(filePath, prefix) || pathHasPrefix(fileDir, prefix)
	}
	if !strings.Contains(token, "/") && !strings.Contains(token, ".") {
		return false
	}

	return pathHasPrefix(filePath, token) || pathHasPrefix(fileDir, token)
}

func pathHasPrefix(path string, prefix string) bool {
	if path == "" || prefix == "" {
		return false
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}

func isLikelyTestCommand(commandText string) bool {
	normalized := strings.ToLower(strings.TrimSpace(commandText))
	switch {
	case normalized == "":
		return false
	case strings.HasPrefix(normalized, "go test"):
		return true
	case strings.HasPrefix(normalized, "npm test"), strings.HasPrefix(normalized, "npm run test"):
		return true
	case strings.HasPrefix(normalized, "pnpm test"), strings.HasPrefix(normalized, "pnpm run test"):
		return true
	case strings.HasPrefix(normalized, "yarn test"), strings.HasPrefix(normalized, "yarn run test"):
		return true
	case strings.HasPrefix(normalized, "bun test"), strings.HasPrefix(normalized, "bun run test"):
		return true
	case strings.HasPrefix(normalized, "make test"):
		return true
	case strings.HasPrefix(normalized, "pytest"):
		return true
	case strings.HasPrefix(normalized, "cargo test"):
		return true
	default:
		return false
	}
}

func focusFileCategory(file File) string {
	switch {
	case file.Dependency:
		return patchCategoryDependency
	case isTestFile(file.Path):
		return patchCategoryTest
	case isDocFile(file.Path):
		return patchCategoryDocs
	case isPatchGeneratedPath(file.Path):
		return patchCategoryGeneratedOrUnknown
	default:
		return patchCategoryProduction
	}
}

func compareFocusTasks(a ReviewTask, b ReviewTask) bool {
	aRank := focusTaskPriorityRank(a.Priority)
	bRank := focusTaskPriorityRank(b.Priority)
	if aRank != bRank {
		return aRank < bRank
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	if a.Question != b.Question {
		return a.Question < b.Question
	}
	return strings.Join(a.EvidenceRefs, "|") < strings.Join(b.EvidenceRefs, "|")
}

func focusTaskPriorityRank(priority string) int {
	switch priority {
	case focusTaskPriorityP0:
		return 0
	case focusTaskPriorityP1:
		return 1
	case focusTaskPriorityP2:
		return 2
	case focusTaskPriorityP3:
		return 3
	default:
		return 4
	}
}

func patchSymbolsByPath(files []PatchSummaryFile) map[string][]string {
	symbolsByPath := map[string][]string{}
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		if len(file.Symbols) == 0 {
			continue
		}
		symbolsByPath[file.Path] = uniqueSorted(file.Symbols)
	}
	return symbolsByPath
}

func policyCheckPaths(name string, files []File) []string {
	switch name {
	case "dependency_file_changed":
		return filePathsMatching(files, func(file File) bool {
			return file.Dependency
		})
	case "sensitive_file_changed":
		return filePathsMatching(files, func(file File) bool {
			return file.Sensitive
		})
	case "ci_or_security_file_changed":
		return filePathsMatching(files, func(file File) bool {
			return isCIOrSecurityFile(file)
		})
	case "generated_file_changed":
		return filePathsMatching(files, func(file File) bool {
			return isPatchGeneratedPath(file.Path)
		})
	case "target_file_read_before_edit", "related_context_read_before_edit", "tests_run_after_code_changes", "lint_run_after_code_changes", "typecheck_run_when_applicable", "commit_created":
		return nil
	default:
		return nil
	}
}

func policyCheckPriority(name string) string {
	switch name {
	case "destructive_command_used", "network_command_used":
		return focusTaskPriorityP1
	case "dependency_file_changed", "sensitive_file_changed", "ci_or_security_file_changed", "generated_file_changed":
		return focusTaskPriorityP2
	default:
		return focusTaskPriorityP2
	}
}

func commandRelatedEvidence(commandText string, files []File, symbolsByPath map[string][]string) ([]string, []string) {
	normalized := strings.ToLower(filepath.ToSlash(strings.TrimSpace(commandText)))
	if normalized == "" {
		return nil, nil
	}

	paths := make([]string, 0)
	symbols := make([]string, 0)
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		path := strings.ToLower(filepath.ToSlash(file.Path))
		base := strings.ToLower(filepath.Base(path))
		if path != "" && strings.Contains(normalized, path) {
			paths = append(paths, file.Path)
			symbols = append(symbols, symbolsByPath[file.Path]...)
			continue
		}
		if base != "" && strings.Contains(normalized, base) {
			paths = append(paths, file.Path)
			symbols = append(symbols, symbolsByPath[file.Path]...)
		}
	}

	return uniqueSorted(paths), uniqueSorted(symbols)
}

func changedGoTestPaths(summary PatchSummary) []string {
	paths := make([]string, 0)
	for _, file := range summary.ChangedFiles {
		if file.Path == "" || file.Category != patchCategoryProduction {
			continue
		}
		paths = append(paths, file.Path)
	}
	return uniqueSorted(paths)
}

func filePathsMatching(files []File, predicate func(File) bool) []string {
	paths := make([]string, 0)
	for _, file := range files {
		if predicate(file) && file.Path != "" {
			paths = append(paths, file.Path)
		}
	}
	return uniqueSorted(paths)
}

func collectFocusEvidenceRefs(reasons []FocusReason, tasks []ReviewTask, gates []FailedGate) []string {
	refs := make([]string, 0)
	for _, reason := range reasons {
		refs = append(refs, reason.Refs...)
	}
	for _, task := range tasks {
		refs = append(refs, task.EvidenceRefs...)
	}
	for _, gate := range gates {
		refs = append(refs, gate.EvidenceRefs...)
	}
	return uniqueSorted(refs)
}

func dedupeFocusReasons(reasons []FocusReason) []FocusReason {
	seen := make(map[string]FocusReason)
	for _, reason := range reasons {
		existing, ok := seen[reason.Message]
		if !ok {
			seen[reason.Message] = reason
			continue
		}
		existing.Refs = uniqueSorted(append(existing.Refs, reason.Refs...))
		existing.FromMissing = existing.FromMissing || reason.FromMissing
		if existing.Confidence == "" || reason.Confidence > existing.Confidence {
			existing.Confidence = reason.Confidence
		}
		if existing.Kind == "" {
			existing.Kind = reason.Kind
		}
		if reason.Kind == focusReasonBlock {
			existing.Kind = focusReasonBlock
		}
		seen[reason.Message] = existing
	}

	ordered := make([]string, 0, len(seen))
	for key := range seen {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	out := make([]FocusReason, 0, len(ordered))
	for _, key := range ordered {
		out = append(out, seen[key])
	}
	return out
}

func focusReasonsToStrings(reasons []FocusReason) []string {
	reasonStrings := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reasonStrings = append(reasonStrings, reason.Message)
	}
	return reasonStrings
}

func uniqueReviewTasks(tasks []ReviewTask) []ReviewTask {
	seen := map[string]ReviewTask{}
	for _, task := range tasks {
		if task.Question == "" || task.Kind == "" {
			continue
		}
		key := task.Kind + "|" + task.Priority + "|" + task.Question
		if existing, ok := seen[key]; ok {
			existing.EvidenceRefs = uniqueSorted(append(existing.EvidenceRefs, task.EvidenceRefs...))
			existing.Paths = uniqueSorted(append(existing.Paths, task.Paths...))
			existing.Symbols = uniqueSorted(append(existing.Symbols, task.Symbols...))
			if existing.Confidence == "" {
				existing.Confidence = task.Confidence
			}
			if existing.Source == "" {
				existing.Source = task.Source
			}
			if existing.Priority == "" {
				existing.Priority = task.Priority
			}
			seen[key] = existing
			continue
		}
		task.EvidenceRefs = uniqueSorted(task.EvidenceRefs)
		task.Paths = uniqueSorted(task.Paths)
		task.Symbols = uniqueSorted(task.Symbols)
		seen[key] = task
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	unique := make([]ReviewTask, 0, len(seen))
	for _, key := range keys {
		unique = append(unique, seen[key])
	}
	for index := range unique {
		unique[index].ID = ""
	}
	return unique
}

func capSortedReviewTasks(tasks []ReviewTask, limit int) []ReviewTask {
	sort.SliceStable(tasks, func(i, j int) bool {
		return compareFocusTasks(tasks[i], tasks[j])
	})
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	for index := range tasks {
		tasks[index].ID = fmt.Sprintf("task_%03d", index+1)
	}
	return tasks
}

func capSortedStrings(values []string, limit int) []string {
	values = uniqueSorted(values)
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func patchSummaryCategoryEvidenceRefs(summary PatchSummary, categories ...string) []string {
	refs := make([]string, 0)
	wanted := map[string]struct{}{}
	for _, category := range categories {
		wanted[category] = struct{}{}
	}
	for _, file := range summary.ChangedFiles {
		if _, ok := wanted[file.Category]; ok {
			refs = append(refs, file.EvidenceRefs...)
		}
	}
	return uniqueSorted(refs)
}

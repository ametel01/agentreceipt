package replay

import (
	"fmt"
	"sort"
	"time"

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

type FocusVerdict string

const (
	focusVerdictPass           FocusVerdict = "pass"
	focusVerdictReviewRequired FocusVerdict = "review_required"
	focusVerdictBlock          FocusVerdict = "block"
	focusVerdictUnverifiable   FocusVerdict = "unverifiable"
)

// FocusReport is a compact reviewer-agent output for a replay session.
type FocusReport struct {
	SchemaVersion int                `json:"schema_version"`
	Kind          string             `json:"kind"`
	SessionID     string             `json:"session_id"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Verdict       FocusVerdict       `json:"verdict"`
	TopReasons    []string           `json:"top_reasons,omitempty"`
	ReviewTasks   []ReviewTask       `json:"review_tasks,omitempty"`
	ChangedFiles  []FocusChangedFile `json:"changed_files,omitempty"`
	FailedGates   []FailedGate       `json:"failed_gates,omitempty"`
	EvidenceRefs  []string           `json:"evidence_refs,omitempty"`
}

type FocusChangedFile struct {
	Path      string `json:"path"`
	Action    string `json:"action,omitempty"`
	Category  string `json:"category,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
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
	Kind         string           `json:"kind"`
	Question     string           `json:"question"`
	Paths        []string         `json:"paths,omitempty"`
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
	focus.FailedGates = collectFailedGates(replay.QualityGates)

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

	if !replay.Verification.IntegrityValid {
		addReason("Integrity verification failed.", verificationEvidenceRefs(), focusReasonBlock, false)
		tasks = append(tasks, ReviewTask{
			Kind:         "integrity_failure",
			Question:     "Inspect integrity evidence before using this focus report.",
			Confidence:   model.ConfidenceHigh,
			Source:       "verification",
			EvidenceRefs: verificationEvidenceRefs(),
		})
	}

	if replay.Verification.IntegrityValid && !replay.Verification.FinalPatchHashValid {
		addReason("Final patch mismatch detected.", []string{finalPatchEvidenceRef}, focusReasonBlock, false)
		tasks = append(tasks, ReviewTask{
			Kind:         "diff_mismatch",
			Question:     "Resolve final patch mismatch before review can continue.",
			Confidence:   model.ConfidenceHigh,
			Source:       "verification",
			EvidenceRefs: []string{finalPatchEvidenceRef},
		})
	}

	for _, gate := range failedGates {
		addReason("Quality gate "+gate.Name+" failed.", gate.EvidenceRefs, focusReasonBlock, false)
		tasks = append(tasks, ReviewTask{
			Kind:         "failed_gate",
			Question:     "Resolve failed quality gate: " + gate.Name + ".",
			EvidenceRefs: gate.EvidenceRefs,
			Confidence:   model.ConfidenceHigh,
			Source:       "quality_gate",
		})
	}

	for _, check := range replay.PolicyChecks {
		switch check.Status {
		case policyCheckStatusFail:
			addReason(check.Message, check.EvidenceRefs, focusReasonBlock, false)
			tasks = append(tasks, ReviewTask{
				Kind:         "failed_policy_check",
				Question:     check.Message,
				EvidenceRefs: check.EvidenceRefs,
				Confidence:   check.Confidence,
				Source:       "policy",
			})
		case policyCheckStatusWarn, policyCheckStatusUnknown:
			addReason(check.Message, check.EvidenceRefs, focusReasonReview, true)
			tasks = append(tasks, ReviewTask{
				Kind:         "policy_check",
				Question:     check.Message,
				EvidenceRefs: check.EvidenceRefs,
				Confidence:   check.Confidence,
				Source:       "policy",
			})
		}
	}

	if hasFailedCommands(replay.Commands) {
		failedRefs := failedCommandRefs(replay.Commands)
		if len(failedRefs) > 0 {
			addReason("Failed command execution was observed.", failedRefs, focusReasonBlock, false)
			tasks = append(tasks, ReviewTask{
				Kind:         "failed_command",
				Question:     "Review failed commands before approving the session.",
				EvidenceRefs: failedRefs,
				Confidence:   model.ConfidenceHigh,
				Source:       "commands",
			})
		}
	}

	for _, namedGate := range listQualityGates(replay.QualityGates) {
		if namedGate.status == qualityGateStatusNotRun || namedGate.status == qualityGateStatusUnknown {
			addReason("Quality gate "+namedGate.name+" was not run.", namedGate.gate.EvidenceRefs, focusReasonReview, true)
			tasks = append(tasks, ReviewTask{
				Kind:         "missing_gate",
				Question:     "Confirm quality gate " + namedGate.name + " or record explicit rationale.",
				EvidenceRefs: namedGate.gate.EvidenceRefs,
				Confidence:   model.ConfidenceLow,
				Source:       "quality_gate",
			})
		}
	}

	if replay.PatchSummary.ProductionChangedWithoutTestsChanged {
		refs := patchSummaryCategoryEvidenceRefs(replay.PatchSummary, patchCategoryProduction, patchCategoryTest)
		addReason("Production code changed without test file changes.", refs, focusReasonReview, true)
		tasks = append(tasks, ReviewTask{
			Kind:         "missing_test",
			Question:     "Add or update tests for production code changes.",
			Confidence:   model.ConfidenceMedium,
			Source:       "patch",
			EvidenceRefs: refs,
		})
	}

	for _, file := range replay.PatchSummary.ChangedFiles {
		switch {
		case file.Dependency:
			addReason("Dependency file changed.", file.EvidenceRefs, focusReasonReview, true)
			tasks = append(tasks, ReviewTask{
				Kind:         "risky_file",
				Question:     "Review dependency change: " + file.Path,
				Paths:        []string{file.Path},
				EvidenceRefs: file.EvidenceRefs,
				Confidence:   model.ConfidenceMedium,
				Source:       "patch",
			})
		case file.Sensitive:
			addReason("Sensitive file changed.", file.EvidenceRefs, focusReasonReview, true)
			tasks = append(tasks, ReviewTask{
				Kind:         "sensitive_file",
				Question:     "Review sensitive file change: " + file.Path,
				Paths:        []string{file.Path},
				EvidenceRefs: file.EvidenceRefs,
				Confidence:   model.ConfidenceHigh,
				Source:       "patch",
			})
		case file.Category == patchCategoryGeneratedOrUnknown:
			addReason("Generated or unknown file changed.", file.EvidenceRefs, focusReasonReview, true)
			tasks = append(tasks, ReviewTask{
				Kind:         "generated_file",
				Question:     "Validate generated/unknown file change: " + file.Path,
				Paths:        []string{file.Path},
				EvidenceRefs: file.EvidenceRefs,
				Confidence:   model.ConfidenceMedium,
				Source:       "patch",
			})
		case file.Category == patchCategoryDependency:
			addReason("Dependency file changed.", file.EvidenceRefs, focusReasonReview, true)
			tasks = append(tasks, ReviewTask{
				Kind:         "dependency_file",
				Question:     "Review dependency file change: " + file.Path,
				Paths:        []string{file.Path},
				EvidenceRefs: file.EvidenceRefs,
				Confidence:   model.ConfidenceMedium,
				Source:       "patch",
			})
		}
	}

	if replay.Source.SessionState != model.SessionStateFinalized {
		addReason("Session is not finalized ("+string(replay.Source.SessionState)+").", []string{"manifest.json"}, focusReasonReview, true)
		tasks = append(tasks, ReviewTask{
			Kind:         "session_state",
			Question:     "Wait for a finalized session before deterministic review.",
			EvidenceRefs: []string{"manifest.json"},
			Confidence:   model.ConfidenceHigh,
			Source:       "session",
		})
	}

	reasons = dedupeFocusReasons(reasons)
	sort.SliceStable(reasons, func(i, j int) bool {
		return reasons[i].Message < reasons[j].Message
	})

	uniqueTasks := uniqueReviewTasks(tasks)
	sort.SliceStable(uniqueTasks, func(i, j int) bool {
		if uniqueTasks[i].Kind == uniqueTasks[j].Kind {
			return uniqueTasks[i].Question < uniqueTasks[j].Question
		}
		return uniqueTasks[i].Kind < uniqueTasks[j].Kind
	})

	return reasons, uniqueTasks
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
	files := make([]FocusChangedFile, 0, max(len(replay.PatchSummary.ChangedFiles), len(replay.Files)))
	for _, file := range replay.PatchSummary.ChangedFiles {
		files = append(files, FocusChangedFile{
			Path:      file.Path,
			Action:    file.Action,
			Category:  file.Category,
			Sensitive: file.Sensitive,
		})
	}

	if len(files) == 0 {
		for _, file := range replay.Files {
			files = append(files, FocusChangedFile{
				Path:      file.Path,
				Action:    file.Action,
				Sensitive: file.Sensitive,
				Category:  focusFileCategory(file),
			})
		}
	}

	unique := make([]FocusChangedFile, 0, len(files))
	seen := map[string]FocusChangedFile{}
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		seen[file.Path] = file
	}
	for _, file := range seen {
		unique = append(unique, file)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].Path == unique[j].Path {
			return unique[i].Action < unique[j].Action
		}
		return unique[i].Path < unique[j].Path
	})

	return unique
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
		key := task.Kind + "|" + task.Question
		if existing, ok := seen[key]; ok {
			existing.EvidenceRefs = uniqueSorted(append(existing.EvidenceRefs, task.EvidenceRefs...))
			existing.Paths = uniqueSorted(append(existing.Paths, task.Paths...))
			if existing.Confidence == "" {
				existing.Confidence = task.Confidence
			}
			seen[key] = existing
			continue
		}
		task.EvidenceRefs = uniqueSorted(task.EvidenceRefs)
		task.Paths = uniqueSorted(task.Paths)
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
		if tasks[i].Kind == tasks[j].Kind {
			return tasks[i].Question < tasks[j].Question
		}
		return tasks[i].Kind < tasks[j].Kind
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

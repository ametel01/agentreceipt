package replay

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/ametel01/agentreceipt/internal/commandrisk"
	"github.com/ametel01/agentreceipt/internal/model"
)

func buildPolicyChecks(commands []Command, files []File, patchSummary PatchSummary, gates QualityGates) []PolicyCheck {
	checks := make([]PolicyCheck, 0, 11)
	hasCodeLikeChanges := hasPolicyCodeLikeChanges(files, patchSummary)
	hasTypeScriptChanges := hasPolicyTypeScriptChanges(files)
	readIndex := firstCommandIndex(commands, func(command Command) bool {
		return isReadCommand(command.Command)
	})
	editIndex := firstCommandIndex(commands, func(command Command) bool {
		return isWriteCommand(command.Command) || isEditCommand(command.Command)
	})

	checks = append(checks, buildReadBeforeEditPolicyCheck(commands, readIndex, editIndex, hasCodeLikeChanges))
	checks = append(checks, buildRelatedContextReadPolicyCheck(commands, files, readIndex, editIndex, hasCodeLikeChanges))
	checks = append(checks, buildGatePolicyCheck("tests_run_after_code_changes", "tests", gates.Tests, hasCodeLikeChanges, len(commands) == 0, "Test commands were not observed after code changes.", "Test commands failed after code changes."))
	checks = append(checks, buildGatePolicyCheck("lint_run_after_code_changes", "lint", gates.Lint, hasCodeLikeChanges, len(commands) == 0, "Lint commands were not observed after code changes.", "Lint commands failed after code changes."))
	checks = append(checks, buildTypecheckPolicyCheck(gates.Typecheck, hasTypeScriptChanges, len(commands) == 0))
	checks = append(checks, buildCommandSignalPolicyCheck(commands, "destructive_command_used", "Destructive commands were used.", "No command evidence was available to assess destructive command use.", []string{"destructive_filesystem", "destructive_git", "container_destructive", "find_delete", "mass_edit_or_overwrite"}))
	checks = append(checks, buildCommandSignalPolicyCheck(commands, "network_command_used", "Network commands were used.", "No command evidence was available to assess network command use.", []string{"network_egress", "remote_code_execution", "cloud_or_deploy_mutation"}))
	checks = append(checks, buildFileChangePolicyCheck(files, "dependency_file_changed", "Dependency file changed.", policyCheckStatusWarn, func(file File) bool { return file.Dependency }, "No dependency file changes were observed."))
	checks = append(checks, buildFileChangePolicyCheck(files, "sensitive_file_changed", "Sensitive file changed.", policyCheckStatusWarn, func(file File) bool { return file.Sensitive }, "No sensitive file changes were observed."))
	checks = append(checks, buildFileChangePolicyCheck(files, "ci_or_security_file_changed", "CI or security file changed.", policyCheckStatusWarn, isCIOrSecurityFile, "No CI or security file changes were observed."))
	checks = append(checks, buildFileChangePolicyCheck(files, "generated_file_changed", "Generated or unknown file changed.", policyCheckStatusWarn, isGeneratedPolicyFile, "No generated or unknown file changes were observed."))
	checks = append(checks, buildCommitPolicyCheck(commands, len(files) == 0))

	for index := range checks {
		if checks[index].EvidenceRefs != nil {
			checks[index].EvidenceRefs = uniqueSorted(checks[index].EvidenceRefs)
		}
	}

	return checks
}

func buildReviewFocus(gaps []string, gates QualityGates, patchSummary PatchSummary, policyChecks []PolicyCheck, commands []Command, files []File) []ReviewFocusItem {
	items := make([]ReviewFocusItem, 0)
	seen := map[string]bool{}
	add := func(message string, confidence model.Confidence, refs []string) {
		message = strings.TrimSpace(message)
		if message == "" || seen[message] {
			return
		}
		seen[message] = true
		items = append(items, ReviewFocusItem{
			Message:      message,
			Confidence:   confidence,
			EvidenceRefs: uniqueSorted(refs),
		})
	}

	for _, gap := range gaps {
		add(gap, model.ConfidenceLow, nil)
	}

	for _, gate := range []struct {
		Name string
		Gate QualityGate
	}{
		{Name: "format", Gate: gates.Format},
		{Name: "lint", Gate: gates.Lint},
		{Name: "tests", Gate: gates.Tests},
		{Name: "race_tests", Gate: gates.RaceTests},
		{Name: "typecheck", Gate: gates.Typecheck},
		{Name: "security", Gate: gates.Security},
		{Name: "coverage", Gate: gates.Coverage},
		{Name: "build", Gate: gates.Build},
		{Name: "smoke", Gate: gates.Smoke},
		{Name: "verify", Gate: gates.Verify},
	} {
		switch gate.Gate.Status {
		case qualityGateStatusFailed:
			add("Quality gate "+gate.Name+" failed.", model.ConfidenceHigh, gate.Gate.EvidenceRefs)
		case qualityGateStatusUnknown, qualityGateStatusNotRun:
			add("Quality gate "+gate.Name+" was not run.", model.ConfidenceLowMedium, gate.Gate.EvidenceRefs)
		}
	}

	if patchSummary.ProductionChangedWithoutTestsChanged {
		add("Production code changed without test file changes.", model.ConfidenceMedium, patchSummaryFileRefs(patchSummary, func(file PatchSummaryFile) bool {
			return file.Category == patchCategoryProduction || file.Category == patchCategoryTest
		}))
	}
	if refs := patchSummaryFileRefs(patchSummary, func(file PatchSummaryFile) bool { return file.Category == patchCategoryDependency }); len(refs) > 0 {
		add("Dependency files changed.", model.ConfidenceMedium, refs)
	}
	if refs := patchSummaryFileRefs(patchSummary, func(file PatchSummaryFile) bool { return file.Sensitive }); len(refs) > 0 {
		add("Sensitive files changed.", model.ConfidenceMedium, refs)
	}
	if refs := patchSummaryFileRefs(patchSummary, func(file PatchSummaryFile) bool { return isCIOrSecurityPatchFile(file.Path) }); len(refs) > 0 {
		add("CI or security files changed.", model.ConfidenceMedium, refs)
	}
	if refs := patchSummaryFileRefs(patchSummary, func(file PatchSummaryFile) bool { return file.Category == patchCategoryGeneratedOrUnknown }); len(refs) > 0 {
		add("Generated or unknown files changed.", model.ConfidenceMedium, refs)
	}

	for _, check := range policyChecks {
		switch check.Status {
		case policyCheckStatusWarn, policyCheckStatusFail, policyCheckStatusUnknown:
			add(check.Message, check.Confidence, check.EvidenceRefs)
		}
	}

	for _, command := range commands {
		if command.Status != "failed" {
			continue
		}
		message := "Failed command: " + command.Command
		if command.OutputSummary != "" {
			message += " (" + command.OutputSummary + ")"
		}
		add(message, command.Confidence, command.EvidenceRefs)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Message == items[j].Message {
			return strings.Join(items[i].EvidenceRefs, ",") < strings.Join(items[j].EvidenceRefs, ",")
		}
		return items[i].Message < items[j].Message
	})

	return items
}

func buildReadBeforeEditPolicyCheck(commands []Command, readIndex int, editIndex int, applicable bool) PolicyCheck {
	if !applicable {
		return PolicyCheck{
			Name:       "target_file_read_before_edit",
			Status:     policyCheckStatusNotApplicable,
			Message:    "No code-like changes were observed.",
			Confidence: model.ConfidenceLow,
		}
	}
	if editIndex < 0 {
		return PolicyCheck{
			Name:       "target_file_read_before_edit",
			Status:     policyCheckStatusNotApplicable,
			Message:    "No edit or write commands were observed.",
			Confidence: model.ConfidenceLow,
		}
	}
	if readIndex < 0 {
		return PolicyCheck{
			Name:         "target_file_read_before_edit",
			Status:       policyCheckStatusFail,
			Message:      "No read command was observed before edits.",
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: commandRefsAtIndex(commands, editIndex),
		}
	}
	if readIndex < editIndex {
		return PolicyCheck{
			Name:         "target_file_read_before_edit",
			Status:       policyCheckStatusPass,
			Message:      "Read commands were observed before edits.",
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: uniqueSorted(append(commandRefsAtIndex(commands, readIndex), commandRefsAtIndex(commands, editIndex)...)),
		}
	}

	return PolicyCheck{
		Name:         "target_file_read_before_edit",
		Status:       policyCheckStatusFail,
		Message:      "A read command was observed after edits began.",
		Confidence:   model.ConfidenceHigh,
		EvidenceRefs: uniqueSorted(append(commandRefsAtIndex(commands, readIndex), commandRefsAtIndex(commands, editIndex)...)),
	}
}

func buildRelatedContextReadPolicyCheck(commands []Command, files []File, readIndex int, editIndex int, applicable bool) PolicyCheck {
	if !applicable {
		return PolicyCheck{
			Name:       "related_context_read_before_edit",
			Status:     policyCheckStatusNotApplicable,
			Message:    "No code-like changes were observed.",
			Confidence: model.ConfidenceLow,
		}
	}
	if editIndex < 0 {
		return PolicyCheck{
			Name:       "related_context_read_before_edit",
			Status:     policyCheckStatusNotApplicable,
			Message:    "No edit or write commands were observed.",
			Confidence: model.ConfidenceLow,
		}
	}
	contextIndex := firstCommandIndex(commands, func(command Command) bool {
		return isReadCommand(command.Command) && commandReferencesChangedFile(command.Command, files)
	})
	if contextIndex >= 0 && contextIndex < editIndex {
		return PolicyCheck{
			Name:         "related_context_read_before_edit",
			Status:       policyCheckStatusPass,
			Message:      "Changed-file context was read before edits.",
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: uniqueSorted(append(commandRefsAtIndex(commands, contextIndex), commandRefsAtIndex(commands, editIndex)...)),
		}
	}
	if readIndex < 0 {
		return PolicyCheck{
			Name:         "related_context_read_before_edit",
			Status:       policyCheckStatusFail,
			Message:      "No read command was observed before edits.",
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: commandRefsAtIndex(commands, editIndex),
		}
	}
	if contextIndex >= 0 && contextIndex > editIndex {
		return PolicyCheck{
			Name:         "related_context_read_before_edit",
			Status:       policyCheckStatusFail,
			Message:      "Changed-file context was read after edits began.",
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: uniqueSorted(append(commandRefsAtIndex(commands, contextIndex), commandRefsAtIndex(commands, editIndex)...)),
		}
	}

	return PolicyCheck{
		Name:         "related_context_read_before_edit",
		Status:       policyCheckStatusWarn,
		Message:      "Read commands were observed, but none obviously referenced changed files.",
		Confidence:   model.ConfidenceMedium,
		EvidenceRefs: uniqueSorted(append(commandRefsAtIndex(commands, readIndex), commandRefsAtIndex(commands, editIndex)...)),
	}
}

func buildGatePolicyCheck(name string, gateName string, gate QualityGate, applicable bool, unknown bool, failMessage string, failedMessage string) PolicyCheck {
	if !applicable {
		return PolicyCheck{
			Name:       name,
			Status:     policyCheckStatusNotApplicable,
			Message:    "No code-like changes were observed.",
			Confidence: model.ConfidenceLow,
		}
	}
	switch gate.Status {
	case qualityGateStatusPassed:
		return PolicyCheck{
			Name:         name,
			Status:       policyCheckStatusPass,
			Message:      gateName + " commands were observed after code changes.",
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: gate.EvidenceRefs,
		}
	case qualityGateStatusFailed:
		return PolicyCheck{
			Name:         name,
			Status:       policyCheckStatusFail,
			Message:      failedMessage,
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: gate.EvidenceRefs,
		}
	case qualityGateStatusNotRun, qualityGateStatusUnknown:
		return PolicyCheck{
			Name:       name,
			Status:     policyCheckStatusUnknown,
			Message:    "No command evidence was available to determine whether " + gateName + " ran after code changes.",
			Confidence: model.ConfidenceLowMedium,
		}
	default:
		return PolicyCheck{
			Name:       name,
			Status:     policyCheckStatusUnknown,
			Message:    "No command evidence was available to determine whether " + gateName + " ran after code changes.",
			Confidence: model.ConfidenceLowMedium,
		}
	}
}

func buildTypecheckPolicyCheck(gate QualityGate, applicable bool, unknown bool) PolicyCheck {
	if !applicable {
		return PolicyCheck{
			Name:       "typecheck_run_when_applicable",
			Status:     policyCheckStatusNotApplicable,
			Message:    "No TypeScript changes were observed.",
			Confidence: model.ConfidenceLow,
		}
	}
	switch gate.Status {
	case qualityGateStatusPassed:
		return PolicyCheck{
			Name:         "typecheck_run_when_applicable",
			Status:       policyCheckStatusPass,
			Message:      "Typecheck commands were observed for TypeScript changes.",
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: gate.EvidenceRefs,
		}
	case qualityGateStatusFailed:
		return PolicyCheck{
			Name:         "typecheck_run_when_applicable",
			Status:       policyCheckStatusFail,
			Message:      "Typecheck commands failed for TypeScript changes.",
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: gate.EvidenceRefs,
		}
	case qualityGateStatusNotRun, qualityGateStatusUnknown:
		if unknown {
			return PolicyCheck{
				Name:       "typecheck_run_when_applicable",
				Status:     policyCheckStatusUnknown,
				Message:    "No command evidence was available to determine whether typecheck ran for TypeScript changes.",
				Confidence: model.ConfidenceLowMedium,
			}
		}
		return PolicyCheck{
			Name:         "typecheck_run_when_applicable",
			Status:       policyCheckStatusFail,
			Message:      "Typecheck commands were not observed for TypeScript changes.",
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: gate.EvidenceRefs,
		}
	default:
		return PolicyCheck{
			Name:       "typecheck_run_when_applicable",
			Status:     policyCheckStatusUnknown,
			Message:    "No command evidence was available to determine whether typecheck ran for TypeScript changes.",
			Confidence: model.ConfidenceLowMedium,
		}
	}
}

func buildCommandSignalPolicyCheck(commands []Command, name string, failMessage string, unknownMessage string, signals []string) PolicyCheck {
	refs := commandRefsMatching(commands, func(command Command) bool {
		return commandSignalsHas(commandrisk.Classify(command.Command), signals...)
	})
	if len(refs) > 0 {
		return PolicyCheck{
			Name:         name,
			Status:       policyCheckStatusFail,
			Message:      failMessage,
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: refs,
		}
	}
	if len(commands) == 0 {
		return PolicyCheck{
			Name:       name,
			Status:     policyCheckStatusUnknown,
			Message:    unknownMessage,
			Confidence: model.ConfidenceLowMedium,
		}
	}

	return PolicyCheck{
		Name:       name,
		Status:     policyCheckStatusPass,
		Message:    "No matching command evidence was observed.",
		Confidence: model.ConfidenceMedium,
	}
}

func buildFileChangePolicyCheck(files []File, name string, message string, status string, predicate func(File) bool, unknownMessage string) PolicyCheck {
	refs := fileRefsMatching(files, predicate)
	if len(refs) > 0 {
		return PolicyCheck{
			Name:         name,
			Status:       status,
			Message:      message,
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: refs,
		}
	}
	if len(files) == 0 {
		return PolicyCheck{
			Name:       name,
			Status:     policyCheckStatusUnknown,
			Message:    unknownMessage,
			Confidence: model.ConfidenceLowMedium,
		}
	}

	return PolicyCheck{
		Name:       name,
		Status:     policyCheckStatusPass,
		Message:    "No matching file changes were observed.",
		Confidence: model.ConfidenceMedium,
	}
}

func buildCommitPolicyCheck(commands []Command, unknownIfNoFiles bool) PolicyCheck {
	refs := commandRefsMatching(commands, func(command Command) bool {
		return isCommitLikeCommand(command.Command)
	})
	if len(refs) > 0 {
		return PolicyCheck{
			Name:         "commit_created",
			Status:       policyCheckStatusPass,
			Message:      "A commit command was observed.",
			Confidence:   model.ConfidenceMedium,
			EvidenceRefs: refs,
		}
	}
	if len(commands) == 0 || unknownIfNoFiles {
		return PolicyCheck{
			Name:       "commit_created",
			Status:     policyCheckStatusNotApplicable,
			Message:    "No commit command was observed.",
			Confidence: model.ConfidenceLow,
		}
	}

	return PolicyCheck{
		Name:       "commit_created",
		Status:     policyCheckStatusNotApplicable,
		Message:    "No commit command was observed.",
		Confidence: model.ConfidenceLow,
	}
}

func hasPolicyCodeLikeChanges(files []File, patchSummary PatchSummary) bool {
	if patchSummary.FileCounts.Production > 0 || patchSummary.FileCounts.Test > 0 || patchSummary.FileCounts.Config > 0 || patchSummary.FileCounts.Dependency > 0 || patchSummary.FileCounts.GeneratedOrUnknown > 0 {
		return true
	}
	for _, file := range files {
		if !isDocFile(file.Path) {
			return true
		}
	}

	return false
}

func hasPolicyTypeScriptChanges(files []File) bool {
	for _, file := range files {
		switch strings.ToLower(filepath.Ext(file.Path)) {
		case ".ts", ".tsx", ".mts", ".cts":
			return true
		}
	}

	return false
}

func firstCommandIndex(commands []Command, predicate func(Command) bool) int {
	for index, command := range commands {
		if predicate(command) {
			return index
		}
	}

	return -1
}

func commandRefsAtIndex(commands []Command, index int) []string {
	if index < 0 || index >= len(commands) {
		return nil
	}
	return append([]string(nil), commands[index].EvidenceRefs...)
}

func commandRefsMatching(commands []Command, predicate func(Command) bool) []string {
	refs := make([]string, 0)
	for _, command := range commands {
		if predicate(command) {
			refs = append(refs, command.EvidenceRefs...)
		}
	}

	return uniqueSorted(refs)
}

func fileRefsMatching(files []File, predicate func(File) bool) []string {
	refs := make([]string, 0)
	for _, file := range files {
		if predicate(file) {
			refs = append(refs, file.EvidenceRefs...)
		}
	}

	return uniqueSorted(refs)
}

func commandReferencesChangedFile(commandText string, files []File) bool {
	normalized := strings.ToLower(commandText)
	for _, file := range files {
		path := strings.ToLower(filepath.ToSlash(file.Path))
		base := strings.ToLower(filepath.Base(path))
		if path != "" && strings.Contains(normalized, path) {
			return true
		}
		if base != "" && strings.Contains(normalized, base) {
			return true
		}
	}

	return false
}

func isCIOrSecurityFile(file File) bool {
	return isCIOrSecurityPatchFile(file.Path)
}

func isCIOrSecurityPatchFile(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.HasPrefix(normalized, ".github/"):
		return true
	case strings.Contains(normalized, "/.github/"):
		return true
	case strings.Contains(normalized, "/security/"), strings.HasPrefix(normalized, "security/"):
		return true
	case strings.Contains(normalized, "/.circleci/"), strings.HasPrefix(normalized, ".circleci/"):
		return true
	case strings.Contains(normalized, "/.gitlab/"), strings.HasPrefix(normalized, ".gitlab/"):
		return true
	case strings.Contains(normalized, "/.azure/"), strings.HasPrefix(normalized, ".azure/"):
		return true
	case strings.Contains(normalized, "security"), strings.Contains(normalized, "ci"):
		return true
	default:
		return false
	}
}

func isGeneratedPolicyFile(file File) bool {
	return isPatchGeneratedPath(file.Path)
}

func patchSummaryFileRefs(summary PatchSummary, predicate func(PatchSummaryFile) bool) []string {
	refs := make([]string, 0)
	for _, file := range summary.ChangedFiles {
		if predicate(file) {
			refs = append(refs, file.EvidenceRefs...)
		}
	}

	return uniqueSorted(refs)
}

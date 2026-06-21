package replay

import (
	"strings"

	"github.com/ametel01/agentreceipt/internal/model"
)

type ReasonCode string

const (
	reasonCodeIntegrityFailure         ReasonCode = "integrity_failure"
	reasonCodeAuthenticityUnverifiable ReasonCode = "authenticity_unverifiable"
	reasonCodeFinalPatchMismatch       ReasonCode = "final_patch_mismatch"
	reasonCodeProviderCommandMissing   ReasonCode = "provider_command_events_missing"
	reasonCodeCommandEvidenceMissing   ReasonCode = "command_evidence_missing"
	reasonCodeSessionNotFinalized      ReasonCode = "session_not_finalized"
	reasonCodeFailedGate               ReasonCode = "failed_gate"
	reasonCodeFailedCommand            ReasonCode = "failed_command"
	reasonCodeMissingTests             ReasonCode = "missing_tests"
	reasonCodeDependencyChange         ReasonCode = "dependency_change"
	reasonCodeSensitiveChange          ReasonCode = "sensitive_change"
	reasonCodeGeneratedChange          ReasonCode = "generated_change"
	reasonCodeEvidenceGap              ReasonCode = "evidence_gap"
	reasonCodeReviewFocus              ReasonCode = "review_focus"
)

const (
	reviewabilityStatusReady        = "ready"
	reviewabilityStatusPartial      = "partial"
	reviewabilityStatusBlocked      = "blocked"
	reviewabilityStatusUnverifiable = "unverifiable"
)

const (
	processMeaningNoReviewRequired = "no_review_required"
	processMeaningReviewRequired   = "review_required"
	processMeaningBlocked          = "blocked"
	processMeaningUnverifiable     = "unverifiable"
	processMeaningEvidenceReady    = "evidence_ready"
)

// ProcessContract is the stable loop-facing contract for command behavior.
type ProcessContract struct {
	ExitCode  int    `json:"exit_code"`
	Meaning   string `json:"meaning"`
	Retryable bool   `json:"retryable"`
}

// Reviewability summarizes whether a report is ready for loop evaluation.
type Reviewability struct {
	Status                  string   `json:"status"`
	BlockingGaps            []string `json:"blocking_gaps,omitempty"`
	CanEvaluateIntegrity    bool     `json:"can_evaluate_integrity"`
	CanEvaluateCodeQuality  bool     `json:"can_evaluate_code_quality"`
	RequiresRerunValidation bool     `json:"requires_rerun_validation"`
	PrimaryBlocker          string   `json:"primary_blocker,omitempty"`
}

func buildProcessContractFromReviewability(reviewability Reviewability) ProcessContract {
	switch reviewability.Status {
	case reviewabilityStatusBlocked:
		return ProcessContract{
			ExitCode:  20,
			Meaning:   processMeaningBlocked,
			Retryable: reviewability.RequiresRerunValidation,
		}
	case reviewabilityStatusUnverifiable:
		return ProcessContract{
			ExitCode:  40,
			Meaning:   processMeaningUnverifiable,
			Retryable: false,
		}
	case reviewabilityStatusPartial:
		return ProcessContract{
			ExitCode:  10,
			Meaning:   processMeaningReviewRequired,
			Retryable: reviewability.RequiresRerunValidation,
		}
	default:
		return ProcessContract{
			ExitCode:  0,
			Meaning:   processMeaningNoReviewRequired,
			Retryable: false,
		}
	}
}

func buildProcessContractForReplay(report Report) ProcessContract {
	reviewability := buildReviewability(report)
	if reviewability.Status == reviewabilityStatusReady {
		return ProcessContract{
			ExitCode:  0,
			Meaning:   processMeaningEvidenceReady,
			Retryable: false,
		}
	}
	return buildProcessContractFromReviewability(reviewability)
}

func buildProcessContractForFocus(report Report, focus FocusReport) ProcessContract {
	if focus.Verdict == focusVerdictBlock {
		exitCode := 20
		meaning := processMeaningBlocked
		if focusContainsReasonCode(focus, reasonCodeFinalPatchMismatch) {
			exitCode = 50
			meaning = "workspace_diff_mismatch"
		}
		return ProcessContract{
			ExitCode:  exitCode,
			Meaning:   meaning,
			Retryable: false,
		}
	}
	if focus.Verdict == focusVerdictUnverifiable {
		return ProcessContract{
			ExitCode:  40,
			Meaning:   processMeaningUnverifiable,
			Retryable: false,
		}
	}
	if focus.Verdict == focusVerdictReviewRequired {
		return ProcessContract{
			ExitCode:  10,
			Meaning:   processMeaningReviewRequired,
			Retryable: true,
		}
	}
	if !report.Verification.IntegrityValid {
		return ProcessContract{
			ExitCode:  30,
			Meaning:   "integrity_failed",
			Retryable: false,
		}
	}
	return ProcessContract{
		ExitCode:  0,
		Meaning:   processMeaningNoReviewRequired,
		Retryable: false,
	}
}

func buildReviewability(report Report) Reviewability {
	reviewability := Reviewability{
		Status:                 reviewabilityStatusReady,
		CanEvaluateIntegrity:   true,
		CanEvaluateCodeQuality: report.Verification.IntegrityValid && report.Source.SessionState == model.SessionStateFinalized,
	}

	if !report.Verification.IntegrityValid {
		reviewability.Status = reviewabilityStatusBlocked
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeIntegrityFailure))
		reviewability.PrimaryBlocker = string(reasonCodeIntegrityFailure)
		reviewability.RequiresRerunValidation = true
		return uniqueReviewability(reviewability)
	}

	if report.Verification.AuthenticityStatus == authenticityStatusUnverifiable || report.Verification.TrustStatus == trustStatusNotTrusted {
		reviewability.Status = reviewabilityStatusUnverifiable
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeAuthenticityUnverifiable))
		reviewability.PrimaryBlocker = string(reasonCodeAuthenticityUnverifiable)
		return uniqueReviewability(reviewability)
	}

	if workspaceSummaryHasComparableChanges(report.WorkspaceChange) && !report.WorkspaceChange.FinalDiffMatchesWorkspace {
		reviewability.Status = reviewabilityStatusBlocked
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeFinalPatchMismatch))
		reviewability.PrimaryBlocker = string(reasonCodeFinalPatchMismatch)
		reviewability.RequiresRerunValidation = true
	}

	gaps := reviewabilityCodesFromStrings(report.Gaps)
	if len(gaps) > 0 {
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, gaps...)
	}
	if report.PatchSummary.ProductionChangedWithoutTestsChanged {
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeMissingTests))
	}
	for _, file := range report.PatchSummary.ChangedFiles {
		switch {
		case file.Dependency:
			reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeDependencyChange))
		case file.Sensitive:
			reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeSensitiveChange))
		case file.Category == patchCategoryGeneratedOrUnknown:
			reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeGeneratedChange))
		}
	}
	if len(report.ReviewFocus) > 0 {
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeReviewFocus))
	}
	if hasFailedCommands(report.Commands) {
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeFailedCommand))
		reviewability.RequiresRerunValidation = true
	}
	for _, gate := range listQualityGates(report.QualityGates) {
		if gate.status == qualityGateStatusFailed {
			reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeFailedGate)+":"+gate.name)
			reviewability.RequiresRerunValidation = true
		}
	}
	if report.Source.SessionState != model.SessionStateFinalized {
		reviewability.BlockingGaps = append(reviewability.BlockingGaps, string(reasonCodeSessionNotFinalized))
		reviewability.RequiresRerunValidation = true
	}

	switch {
	case reviewability.Status == reviewabilityStatusBlocked:
	case len(reviewability.BlockingGaps) > 0:
		reviewability.Status = reviewabilityStatusPartial
	case report.Verification.IntegrityValid &&
		report.Verification.AuthenticityStatus == authenticityStatusAuthentic &&
		report.Verification.TrustStatus == trustStatusTrusted &&
		allGatesPassed(report.QualityGates) &&
		allPolicyChecksPass(report.PolicyChecks) &&
		len(report.Gaps) == 0 &&
		!hasFailedCommands(report.Commands):
		reviewability.Status = reviewabilityStatusReady
	default:
		reviewability.Status = reviewabilityStatusPartial
	}

	reviewability.CanEvaluateCodeQuality = reviewability.Status == reviewabilityStatusReady && report.Source.SessionState == model.SessionStateFinalized
	reviewability.PrimaryBlocker = firstReviewabilityGap(reviewability.BlockingGaps)
	return uniqueReviewability(reviewability)
}

func uniqueReviewability(reviewability Reviewability) Reviewability {
	reviewability.BlockingGaps = uniqueSorted(reviewability.BlockingGaps)
	return reviewability
}

func reviewabilityCodesFromStrings(values []string) []string {
	codes := make([]string, 0, len(values))
	for _, value := range values {
		code := reviewabilityCodeFromString(value)
		if code == "" {
			continue
		}
		codes = append(codes, code)
	}
	return codes
}

func reviewabilityCodeFromString(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "":
		return ""
	case strings.Contains(normalized, "provider tool events"):
		return string(reasonCodeProviderCommandMissing)
	case strings.Contains(normalized, "command evidence was available"):
		return string(reasonCodeCommandEvidenceMissing)
	case strings.Contains(normalized, "session is not finalized"):
		return string(reasonCodeSessionNotFinalized)
	case strings.Contains(normalized, "final patch") && strings.Contains(normalized, "mismatch"):
		return string(reasonCodeFinalPatchMismatch)
	case strings.Contains(normalized, "integrity verification failed"):
		return string(reasonCodeIntegrityFailure)
	case strings.Contains(normalized, "authenticity") && strings.Contains(normalized, "unverifiable"):
		return string(reasonCodeAuthenticityUnverifiable)
	default:
		return normalizeReasonCode(normalized)
	}
}

func normalizeReasonCode(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return ""
	}
	var builder strings.Builder
	prevUnderscore := false
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				builder.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}

func firstReviewabilityGap(gaps []string) string {
	if len(gaps) == 0 {
		return ""
	}
	return gaps[0]
}

func focusContainsReasonCode(focus FocusReport, code ReasonCode) bool {
	for _, reason := range focus.TopReasons {
		if reviewabilityCodeFromString(reason) == string(code) {
			return true
		}
	}
	for _, task := range focus.ReviewTasks {
		if task.ReasonCode == string(code) {
			return true
		}
	}
	return false
}

func reviewTaskReasonCode(kind string, question string, source string) string {
	switch kind {
	case "integrity_failure":
		return string(reasonCodeIntegrityFailure)
	case "diff_mismatch":
		return string(reasonCodeFinalPatchMismatch)
	case "failed_gate":
		return string(reasonCodeFailedGate)
	case "failed_command":
		return string(reasonCodeFailedCommand)
	case "missing_test":
		return string(reasonCodeMissingTests)
	case "dependency_change":
		return string(reasonCodeDependencyChange)
	case "sensitive_change":
		return string(reasonCodeSensitiveChange)
	case "generated_change":
		return string(reasonCodeGeneratedChange)
	case "evidence_gap":
		return string(reasonCodeEvidenceGap)
	default:
		if source == "review_focus" {
			return string(reasonCodeReviewFocus)
		}
		if question != "" {
			return normalizeReasonCode(question)
		}
		return normalizeReasonCode(kind)
	}
}

package replay

import (
	"sort"
	"strings"

	"github.com/ametel01/agentreceipt/internal/model"
)

type PrivacyReport struct {
	RedactionApplied         bool       `json:"redaction_applied"`
	RedactedFields           []string   `json:"redacted_fields,omitempty"`
	RedactionPatterns        []string   `json:"redaction_patterns,omitempty"`
	OutputCaps               OutputCaps `json:"output_caps"`
	SensitiveContentDetected bool       `json:"sensitive_content_detected"`
	RawProviderLogsExposed   bool       `json:"raw_provider_logs_exposed"`
}

type OutputCaps struct {
	MaxOutputSummaryRunes        int `json:"max_output_summary_runes"`
	MaxFailedCommandSummaryRunes int `json:"max_failed_command_summary_runes"`
}

type Claim struct {
	Name         string           `json:"name"`
	Status       string           `json:"status"`
	Message      string           `json:"message"`
	Confidence   model.Confidence `json:"confidence"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
}

type Outcome struct {
	Status       string           `json:"status"`
	Reasons      []string         `json:"reasons"`
	Confidence   model.Confidence `json:"confidence"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
}

const (
	outcomeStatusCompleted         = "completed"
	outcomeStatusCompletedWithGaps = "completed_with_gaps"
	outcomeStatusFailed            = "failed"
	outcomeStatusAbandoned         = "abandoned"
	outcomeStatusCommitted         = "committed"
	outcomeStatusNeedsHumanReview  = "needs_human_review"
)

func buildPrivacyReport(commands []Command, failedDetails []FailedCommandDetail, files []File) PrivacyReport {
	report := PrivacyReport{
		OutputCaps: OutputCaps{
			MaxOutputSummaryRunes:        maxOutputSummaryRunes,
			MaxFailedCommandSummaryRunes: maxOutputSummaryRunes,
		},
		RedactionPatterns:      redactionPatternStrings(),
		RawProviderLogsExposed: false,
	}
	redactedFields := make([]string, 0)
	hasSensitiveContent := false

	for _, command := range commands {
		if strings.Contains(command.Command, "[REDACTED]") {
			redactedFields = append(redactedFields, "commands.command")
			hasSensitiveContent = true
		}
		if strings.Contains(command.OutputSummary, "[REDACTED]") {
			redactedFields = append(redactedFields, "commands.output_summary")
			hasSensitiveContent = true
		}
	}
	for _, detail := range failedDetails {
		if strings.Contains(detail.FailedReason, "[REDACTED]") {
			redactedFields = append(redactedFields, "failed_command_details.failed_reason")
			hasSensitiveContent = true
		}
		if strings.Contains(detail.StderrOrErrorSummary, "[REDACTED]") {
			redactedFields = append(redactedFields, "failed_command_details.stderr_or_error_summary")
			hasSensitiveContent = true
		}
		if strings.Contains(detail.StdoutSummary, "[REDACTED]") {
			redactedFields = append(redactedFields, "failed_command_details.stdout_summary")
			hasSensitiveContent = true
		}
	}
	for _, file := range files {
		if file.Sensitive {
			hasSensitiveContent = true
		}
	}

	report.RedactedFields = uniqueSorted(redactedFields)
	report.RedactionApplied = len(report.RedactedFields) > 0
	report.SensitiveContentDetected = hasSensitiveContent

	return report
}

func buildClaims(report Report) []Claim {
	claims := make([]Claim, 0, 24)
	add := func(name, status, message string, confidence model.Confidence, refs []string) {
		claims = append(claims, Claim{
			Name:         name,
			Status:       status,
			Message:      message,
			Confidence:   confidence,
			EvidenceRefs: uniqueSorted(refs),
		})
	}

	add(
		"verification.verdict",
		report.Verification.OverallVerdict,
		report.Verification.OverallReason,
		model.ConfidenceHigh,
		verificationEvidenceRefs(),
	)
	add(
		"verification.integrity",
		boolStatus(report.Verification.IntegrityValid, "pass", "fail"),
		verificationClaimMessage(report.Verification.IntegrityValid, "integrity"),
		verificationClaimConfidence(report.Verification.IntegrityValid),
		verificationEvidenceRefs(),
	)
	add(
		"verification.authenticity",
		report.Verification.AuthenticityStatus,
		verificationClaimMessage(report.Verification.AuthenticityStatus == authenticityStatusAuthentic, "authenticity"),
		verificationClaimConfidence(report.Verification.AuthenticityStatus == authenticityStatusAuthentic),
		verificationEvidenceRefs(),
	)
	add(
		"signer.trust",
		report.Verification.TrustStatus,
		verificationTrustClaimMessage(report.Verification.TrustStatus),
		trustClaimConfidence(report.Verification.TrustStatus),
		verificationEvidenceRefs(),
	)

	for _, gate := range []struct {
		Name string
		Gate QualityGate
	}{
		{Name: "format", Gate: report.QualityGates.Format},
		{Name: "lint", Gate: report.QualityGates.Lint},
		{Name: "tests", Gate: report.QualityGates.Tests},
		{Name: "race_tests", Gate: report.QualityGates.RaceTests},
		{Name: "typecheck", Gate: report.QualityGates.Typecheck},
		{Name: "security", Gate: report.QualityGates.Security},
		{Name: "coverage", Gate: report.QualityGates.Coverage},
		{Name: "build", Gate: report.QualityGates.Build},
		{Name: "smoke", Gate: report.QualityGates.Smoke},
		{Name: "verify", Gate: report.QualityGates.Verify},
	} {
		add(
			"quality_gate."+gate.Name,
			gate.Gate.Status,
			"Quality gate "+gate.Name+" status: "+gate.Gate.Status,
			qualityGateClaimConfidence(gate.Gate.Status),
			gate.Gate.EvidenceRefs,
		)
	}

	for _, check := range report.PolicyChecks {
		add(
			"policy_check."+check.Name,
			check.Status,
			check.Message,
			check.Confidence,
			check.EvidenceRefs,
		)
	}

	add(
		"privacy.redaction",
		boolStatus(report.Privacy.RedactionApplied, "pass", "pass"),
		privacyClaimMessage(report.Privacy),
		privacyClaimConfidence(report.Privacy),
		privacyEvidenceRefs(report),
	)
	add(
		"outcome",
		report.Outcome.Status,
		outcomeClaimMessage(report.Outcome),
		report.Outcome.Confidence,
		report.Outcome.EvidenceRefs,
	)

	return claims
}

func buildOutcome(report Report) Outcome {
	outcome := Outcome{
		Status:     outcomeStatusNeedsHumanReview,
		Confidence: model.ConfidenceLow,
	}
	reasons := make([]string, 0)
	refs := make([]string, 0)

	if report.Source.SessionState != model.SessionStateFinalized {
		outcome.Status = outcomeStatusAbandoned
		outcome.Confidence = model.ConfidenceHigh
		reasons = append(reasons, "session is not finalized")
		refs = append(refs, "manifest.json")
		outcome.Reasons = uniqueSorted(reasons)
		outcome.EvidenceRefs = uniqueSorted(refs)
		return outcome
	}

	if report.Verification.AuthenticityStatus == authenticityStatusUnverifiable {
		outcome.Status = outcomeStatusNeedsHumanReview
		outcome.Confidence = model.ConfidenceLowMedium
		reasons = append(reasons, "signature authenticity is unverifiable")
		refs = append(refs, "receipt.json")
		outcome.Reasons = uniqueSorted(reasons)
		outcome.EvidenceRefs = uniqueSorted(refs)
		return outcome
	}

	if !report.Verification.IntegrityValid || !report.Verification.SignatureValid || !report.Verification.PolicyValid {
		outcome.Status = outcomeStatusFailed
		outcome.Confidence = model.ConfidenceHigh
		reasons = append(reasons, verificationFailureReasons(report.Verification)...)
		refs = append(refs, verificationEvidenceRefs()...)
	}

	if hasFailedCommands(report.Commands) {
		outcome.Status = outcomeStatusFailed
		outcome.Confidence = model.ConfidenceHigh
		reasons = append(reasons, "failed command evidence was observed")
		refs = append(refs, failedCommandRefs(report.Commands)...)
	}

	for _, gate := range []struct {
		Name string
		Gate QualityGate
	}{
		{Name: "format", Gate: report.QualityGates.Format},
		{Name: "lint", Gate: report.QualityGates.Lint},
		{Name: "tests", Gate: report.QualityGates.Tests},
		{Name: "race_tests", Gate: report.QualityGates.RaceTests},
		{Name: "typecheck", Gate: report.QualityGates.Typecheck},
		{Name: "security", Gate: report.QualityGates.Security},
		{Name: "coverage", Gate: report.QualityGates.Coverage},
		{Name: "build", Gate: report.QualityGates.Build},
		{Name: "smoke", Gate: report.QualityGates.Smoke},
		{Name: "verify", Gate: report.QualityGates.Verify},
	} {
		if gate.Gate.Status == qualityGateStatusFailed {
			outcome.Status = outcomeStatusFailed
			outcome.Confidence = model.ConfidenceHigh
			reasons = append(reasons, "quality gate "+gate.Name+" failed")
			refs = append(refs, gate.Gate.EvidenceRefs...)
		}
	}

	for _, check := range report.PolicyChecks {
		if check.Status == policyCheckStatusFail {
			outcome.Status = outcomeStatusFailed
			outcome.Confidence = model.ConfidenceHigh
			reasons = append(reasons, "policy check "+check.Name+" failed")
			refs = append(refs, check.EvidenceRefs...)
		}
	}

	if outcome.Status == outcomeStatusFailed {
		outcome.Reasons = uniqueSorted(reasons)
		outcome.EvidenceRefs = uniqueSorted(append(refs, failedCommandDetailRefs(report.FailedCommandDetails)...))
		return outcome
	}

	if hasOutcomeGaps(report) {
		outcome.Status = outcomeStatusCompletedWithGaps
		outcome.Confidence = model.ConfidenceMedium
		reasons = append(reasons, outcomeGapReasons(report)...)
		refs = append(refs, failedCommandDetailRefs(report.FailedCommandDetails)...)
		outcome.Reasons = uniqueSorted(reasons)
		outcome.EvidenceRefs = uniqueSorted(refs)
		return outcome
	}

	if commitCheck, ok := findPolicyCheck(report.PolicyChecks, "commit_created"); ok && commitCheck.Status == policyCheckStatusPass {
		outcome.Status = outcomeStatusCommitted
		outcome.Confidence = model.ConfidenceMedium
		reasons = append(reasons, "commit was created")
		refs = append(refs, commitCheck.EvidenceRefs...)
		outcome.Reasons = uniqueSorted(reasons)
		outcome.EvidenceRefs = uniqueSorted(append(refs, verificationEvidenceRefs()...))
		return outcome
	}

	outcome.Status = outcomeStatusCompleted
	outcome.Confidence = model.ConfidenceMedium
	reasons = append(reasons, "session completed without gaps")
	outcome.Reasons = uniqueSorted(reasons)
	outcome.EvidenceRefs = uniqueSorted(verificationEvidenceRefs())

	return outcome
}

func redactionPatternStrings() []string {
	patterns := make([]string, 0, len(secretPatterns))
	for _, pattern := range secretPatterns {
		patterns = append(patterns, pattern.String())
	}
	sort.Strings(patterns)
	return patterns
}

func privacyEvidenceRefs(report Report) []string {
	refs := make([]string, 0)
	for _, command := range report.Commands {
		refs = append(refs, command.EvidenceRefs...)
	}
	for _, detail := range report.FailedCommandDetails {
		refs = append(refs, detail.EvidenceRefs...)
	}
	return uniqueSorted(refs)
}

func verificationEvidenceRefs() []string {
	return []string{"events.jsonl", "manifest.json", "receipt.json", "diffs/final.patch"}
}

func verificationFailureReasons(verification Verification) []string {
	reasons := make([]string, 0)
	if !verification.IntegrityValid {
		reasons = append(reasons, "integrity checks failed")
	}
	if !verification.SignatureValid {
		reasons = append(reasons, "signature verification failed")
	}
	if !verification.PolicyValid {
		reasons = append(reasons, "trust policy is invalid")
	}
	return reasons
}

func outcomeGapReasons(report Report) []string {
	reasons := make([]string, 0)
	for _, gap := range report.Gaps {
		if gap != "" {
			reasons = append(reasons, gap)
		}
	}
	for _, check := range report.PolicyChecks {
		if check.Status == policyCheckStatusWarn || check.Status == policyCheckStatusUnknown {
			reasons = append(reasons, check.Message)
		}
	}
	return uniqueSorted(reasons)
}

func hasFailedCommands(commands []Command) bool {
	for _, command := range commands {
		if command.Status == "failed" {
			return true
		}
	}
	return false
}

func failedCommandRefs(commands []Command) []string {
	refs := make([]string, 0)
	for _, command := range commands {
		if command.Status == "failed" {
			refs = append(refs, command.EvidenceRefs...)
		}
	}
	return uniqueSorted(refs)
}

func failedCommandDetailRefs(details []FailedCommandDetail) []string {
	refs := make([]string, 0)
	for _, detail := range details {
		refs = append(refs, detail.EvidenceRefs...)
	}
	return uniqueSorted(refs)
}

func hasOutcomeGaps(report Report) bool {
	if len(report.Gaps) > 0 {
		return true
	}
	for _, check := range report.PolicyChecks {
		if check.Status == policyCheckStatusWarn || check.Status == policyCheckStatusUnknown {
			return true
		}
	}
	return false
}

func boolStatus(value bool, trueStatus, falseStatus string) string {
	if value {
		return trueStatus
	}
	return falseStatus
}

func verificationClaimMessage(valid bool, noun string) string {
	if valid {
		return noun + " claim is supported by the replay evidence"
	}
	return noun + " claim is not supported by the replay evidence"
}

func verificationClaimConfidence(valid bool) model.Confidence {
	if valid {
		return model.ConfidenceHigh
	}
	return model.ConfidenceHigh
}

func verificationTrustClaimMessage(status string) string {
	switch status {
	case trustStatusTrusted:
		return "Signer is trusted by local policy"
	case trustStatusNotTrusted:
		return "Signer is not trusted by local policy"
	case trustStatusPolicyInvalid:
		return "Trust policy is invalid"
	default:
		return "Trust policy is not configured"
	}
}

func trustClaimConfidence(status string) model.Confidence {
	switch status {
	case trustStatusTrusted:
		return model.ConfidenceMedium
	case trustStatusNotTrusted:
		return model.ConfidenceMedium
	case trustStatusPolicyInvalid:
		return model.ConfidenceHigh
	default:
		return model.ConfidenceLowMedium
	}
}

func qualityGateClaimConfidence(status string) model.Confidence {
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

func privacyClaimMessage(report PrivacyReport) string {
	if report.RedactionApplied {
		return "Sensitive output was redacted before replay export"
	}
	return "No redaction was required for replay export"
}

func privacyClaimConfidence(report PrivacyReport) model.Confidence {
	if report.RedactionApplied {
		return model.ConfidenceHigh
	}
	return model.ConfidenceMedium
}

func outcomeClaimMessage(outcome Outcome) string {
	if len(outcome.Reasons) == 0 {
		return "Replay outcome classified as " + outcome.Status
	}
	return "Replay outcome classified as " + outcome.Status + " because " + strings.Join(outcome.Reasons, "; ")
}

func findPolicyCheck(checks []PolicyCheck, name string) (PolicyCheck, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return PolicyCheck{}, false
}

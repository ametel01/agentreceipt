package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

func TestBuildFocusReportPassVerdict(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	focus := BuildFocusReport(replay)

	if focus.Verdict != focusVerdictPass {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictPass)
	}
	if focus.Kind != focusKind {
		t.Fatalf("kind = %q, want %q", focus.Kind, focusKind)
	}
	if focus.SchemaVersion != replay.SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", focus.SchemaVersion, replay.SchemaVersion)
	}
	if focus.SessionID != replay.SessionID {
		t.Fatalf("session_id = %q, want %q", focus.SessionID, replay.SessionID)
	}
	if len(focus.TopReasons) != 0 {
		t.Fatalf("top_reasons should be empty for pass: %#v", focus.TopReasons)
	}
	if len(focus.ReviewTasks) != 0 {
		t.Fatalf("review_tasks should be empty for pass: %#v", focus.ReviewTasks)
	}
}

func TestBuildFocusReportBlockVerdict(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Verification.IntegrityValid = false

	focus := BuildFocusReport(replay)
	if focus.Verdict != focusVerdictBlock {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictBlock)
	}
	if len(focus.TopReasons) == 0 {
		t.Fatal("expected top reasons for block verdict")
	}
	if focus.TopReasons[0] != "Integrity verification failed." {
		t.Fatalf("top reason = %q, want %q", focus.TopReasons[0], "Integrity verification failed.")
	}
}

func TestBuildFocusReportReviewRequiredVerdict(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary.ProductionChangedWithoutTestsChanged = true
	replay.PatchSummary.ChangedFiles = []PatchSummaryFile{{Path: "main.go", Category: patchCategoryProduction}}

	focus := BuildFocusReport(replay)
	if focus.Verdict != focusVerdictReviewRequired {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictReviewRequired)
	}
	found := false
	for _, reason := range focus.TopReasons {
		if reason == "Production code changed without test file changes." {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected production-without-test reason in top_reasons: %#v", focus.TopReasons)
	}
}

func TestBuildFocusReportUnverifiableVerdict(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Verification.IntegrityValid = true
	replay.Verification.AuthenticityStatus = authenticityStatusUnverifiable
	replay.Verification.TrustStatus = trustStatusNotTrusted

	focus := BuildFocusReport(replay)
	if focus.Verdict != focusVerdictUnverifiable {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictUnverifiable)
	}
	if len(focus.TopReasons) != 0 {
		t.Fatalf("top_reasons should stay empty for pure unverifiable: %#v", focus.TopReasons)
	}
}

func TestBuildFocusReportCapsTopReasonsAndTasks(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.QualityGates = qualityGates(qualityGateStatusNotRun)
	replay.PolicyChecks = make([]PolicyCheck, 0, 10)
	for index := 0; index < 10; index++ {
		replay.PolicyChecks = append(replay.PolicyChecks, PolicyCheck{
			Name:       fmt.Sprintf("warn_%d", index),
			Status:     policyCheckStatusWarn,
			Message:    fmt.Sprintf("Policy warning %d should be reviewed", index),
			Confidence: model.ConfidenceHigh,
		})
	}
	replay.Commands = append(replay.Commands, Command{Command: "rm -rf /tmp", Status: "failed"})

	focus := BuildFocusReport(replay)
	if focus.Verdict != focusVerdictReviewRequired {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictReviewRequired)
	}
	if len(focus.TopReasons) != focusTopReasonLimit {
		t.Fatalf("top_reasons len = %d, want %d", len(focus.TopReasons), focusTopReasonLimit)
	}
	if len(focus.ReviewTasks) != focusTaskLimit {
		t.Fatalf("review_tasks len = %d, want %d", len(focus.ReviewTasks), focusTaskLimit)
	}
}

func TestBuildFocusReportJSONSerializableAndPrivacySafe(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Gaps = []string{"Session is not finalized."}
	replay.Source.SessionState = model.SessionStateActive
	replay.Verification.IntegrityValid = false

	first := BuildFocusReport(replay)
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first focus report: %v", err)
	}

	second := BuildFocusReport(replay)
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second focus report: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("focus reports must be deterministic: %s != %s", firstJSON, secondJSON)
	}

	switch {
	case strings.Contains(string(firstJSON), `"risk_signals"`):
		t.Fatalf("focus JSON unexpectedly includes risk_signals: %s", firstJSON)
	case strings.Contains(string(firstJSON), `"raw_prompt"`):
		t.Fatalf("focus JSON unexpectedly includes raw_prompt: %s", firstJSON)
	case strings.Contains(string(firstJSON), `"commands"`):
		t.Fatalf("focus JSON unexpectedly includes raw commands details: %s", firstJSON)
	}

}

func TestBuildFocusReportEvidenceReferencesAreDeterministicAndCapped(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.QualityGates = qualityGates(qualityGateStatusNotRun)
	replay.PolicyChecks = make([]PolicyCheck, 0, 10)
	for index := 0; index < 10; index++ {
		replay.PolicyChecks = append(replay.PolicyChecks, PolicyCheck{
			Name:         fmt.Sprintf("warn_%d", index),
			Status:       policyCheckStatusWarn,
			Message:      fmt.Sprintf("Policy warning %d should be reviewed", index),
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: []string{fmt.Sprintf("events.jsonl#seq=%d", index+2)},
		})
	}
	replay.Commands = append(replay.Commands, Command{Command: "rm -rf /tmp", Status: "failed", EvidenceRefs: []string{"events.jsonl#seq=99"}})

	focus := BuildFocusReport(replay)
	if len(focus.EvidenceRefs) == 0 {
		t.Fatal("expected evidence refs for non-pass focus")
	}
	for index := 1; index < len(focus.EvidenceRefs); index++ {
		if focus.EvidenceRefs[index-1] > focus.EvidenceRefs[index] {
			t.Fatalf("evidence refs not sorted: %#v", focus.EvidenceRefs)
		}
	}
	if !strings.Contains(stringMustJSON(t, focus), "task_001") {
		t.Fatalf("expected task IDs after cap: %#v", focus.ReviewTasks)
	}
}

func focusBaseReplayReport() Report {
	return Report{
		SchemaVersion: model.SchemaVersion,
		SessionID:     "session-focus",
		GeneratedAt:   time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
		Source: Source{
			SessionState: model.SessionStateFinalized,
		},
		Verification: Verification{
			IntegrityValid:      true,
			FinalPatchHashValid: true,
			EventChainValid:     true,
			ManifestHashValid:   true,
			ReceiptHashValid:    true,
			AuthenticityStatus:  authenticityStatusAuthentic,
			TrustStatus:         trustStatusTrusted,
			SignerTrusted:       true,
		},
		QualityGates: qualityGates(qualityGateStatusPassed),
		PatchSummary: PatchSummary{ChangedFiles: []PatchSummaryFile{{Path: "main.go", Category: patchCategoryProduction}}, ProductionChangedWithoutTestsChanged: false},
		PolicyChecks: []PolicyCheck{{Name: "target_file_read_before_edit", Status: policyCheckStatusPass, Message: "Baseline policy checks passed.", Confidence: model.ConfidenceHigh}},
		Commands:     []Command{{Command: "go test ./...", Status: "success", EvidenceRefs: []string{"events.jsonl#seq=1"}}},
		Files:        []File{{Path: "main.go", Action: "modify", EvidenceRefs: []string{"events.jsonl#seq=1"}}},
	}
}

func qualityGates(status string) QualityGates {
	return QualityGates{
		Format:    QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=10"}},
		Lint:      QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=11"}},
		Tests:     QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=12"}},
		RaceTests: QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=13"}},
		Typecheck: QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=14"}},
		Security:  QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=15"}},
		Coverage:  QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=16"}},
		Build:     QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=17"}},
		Smoke:     QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=18"}},
		Verify:    QualityGate{Status: status, EvidenceRefs: []string{"events.jsonl#seq=19"}},
	}
}

func stringMustJSON(t testing.TB, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(raw)
}

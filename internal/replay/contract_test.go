package replay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildFocusReportIncludesProcessContractAndReviewability(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary.ProductionChangedWithoutTestsChanged = true

	focus := BuildFocusReport(replay)
	if focus.Reviewability.Status != reviewabilityStatusPartial {
		t.Fatalf("reviewability status = %q, want %q", focus.Reviewability.Status, reviewabilityStatusPartial)
	}
	if focus.ProcessContract.ExitCode != 10 || focus.ProcessContract.Meaning != processMeaningReviewRequired {
		t.Fatalf("process contract = %+v", focus.ProcessContract)
	}
	if !focus.ProcessContract.Retryable {
		t.Fatalf("process contract should be retryable: %+v", focus.ProcessContract)
	}
	if len(focus.Reviewability.BlockingGaps) == 0 {
		t.Fatalf("expected reviewability blocking gaps: %+v", focus.Reviewability)
	}
}

func TestBuildReplayIncludesProcessContractAndReviewability(t *testing.T) {
	t.Parallel()

	report := focusBaseReplayReport()
	report.ReviewFocus = []ReviewFocusItem{{Message: "Review the changed file."}}

	reviewability := buildReviewability(report)
	if reviewability.Status != reviewabilityStatusPartial {
		t.Fatalf("reviewability status = %q, want %q", reviewability.Status, reviewabilityStatusPartial)
	}

	processContract := buildProcessContractFromReviewability(reviewability)
	if processContract.ExitCode != 10 || processContract.Meaning != processMeaningReviewRequired {
		t.Fatalf("process contract = %+v", processContract)
	}

	raw, err := json.Marshal(struct {
		ProcessContract ProcessContract `json:"process_contract"`
		Reviewability   Reviewability   `json:"reviewability"`
	}{
		ProcessContract: processContract,
		Reviewability:   reviewability,
	})
	if err != nil {
		t.Fatalf("marshal contract payload: %v", err)
	}
	if !strings.Contains(string(raw), `"partial"`) {
		t.Fatalf("expected partial reviewability in JSON: %s", raw)
	}
}

func TestContractZeroValuesMarshalDeterministically(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(struct {
		ProcessContract ProcessContract `json:"process_contract"`
		Reviewability   Reviewability   `json:"reviewability"`
	}{})
	if err != nil {
		t.Fatalf("marshal zero contract payload: %v", err)
	}
	if !strings.Contains(string(raw), `"process_contract"`) || !strings.Contains(string(raw), `"reviewability"`) {
		t.Fatalf("zero-value contract payload missing fields: %s", raw)
	}
}

func TestReviewabilityReasonCodesAreStable(t *testing.T) {
	t.Parallel()

	reason := reviewabilityCodeFromString("No provider tool events were observed.")
	if reason != string(reasonCodeProviderCommandMissing) {
		t.Fatalf("reason code = %q, want %q", reason, reasonCodeProviderCommandMissing)
	}

	taskReason := reviewTaskReasonCode("failed_gate", "Resolve failed quality gate: tests.", "quality_gate")
	if taskReason != string(reasonCodeFailedGate) {
		t.Fatalf("task reason code = %q, want %q", taskReason, reasonCodeFailedGate)
	}
}

package replay

import (
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

func TestBuildReplayOutputDefaultsToCompactIndexes(t *testing.T) {
	t.Parallel()

	report := testReplayViewReport()
	out := BuildReplayOutput(report, ReplayOutputOptions{})

	if out.Query.Full {
		t.Fatalf("compact replay query unexpectedly marked full: %+v", out.Query)
	}
	if !out.Query.Compact {
		t.Fatalf("compact replay query missing compact flag: %+v", out.Query)
	}
	if len(out.Timeline) != 0 {
		t.Fatalf("compact replay should omit timeline: %+v", out.Timeline)
	}
	if len(out.SelectedEvents) != 0 || len(out.SelectedFiles) != 0 || len(out.SelectedEvidence) != 0 {
		t.Fatalf("compact replay should not preselect filters: events=%d files=%d evidence=%d", len(out.SelectedEvents), len(out.SelectedFiles), len(out.SelectedEvidence))
	}
	if out.Indexes.Events.Count != len(report.Timeline) {
		t.Fatalf("events index count = %d, want %d", out.Indexes.Events.Count, len(report.Timeline))
	}
	if out.Indexes.Events.ArtifactRef != "events.jsonl" {
		t.Fatalf("events artifact ref = %q", out.Indexes.Events.ArtifactRef)
	}
	if out.Indexes.Files.Count != len(report.Files) {
		t.Fatalf("files index count = %d, want %d", out.Indexes.Files.Count, len(report.Files))
	}
	if out.Indexes.Files.ByCategory["production"] != 1 {
		t.Fatalf("files index categories = %#v", out.Indexes.Files.ByCategory)
	}
	if out.Indexes.Evidence.Count != len(report.EvidenceIndex) {
		t.Fatalf("evidence index count = %d, want %d", out.Indexes.Evidence.Count, len(report.EvidenceIndex))
	}
	if len(out.Indexes.TimelineRanges) != 1 {
		t.Fatalf("timeline ranges = %#v", out.Indexes.TimelineRanges)
	}
	if out.Indexes.TimelineRanges[0].Range != "1-2" || out.Indexes.TimelineRanges[0].NormalizedType != "command.run" {
		t.Fatalf("timeline range = %#v", out.Indexes.TimelineRanges[0])
	}
}

func TestBuildReplayOutputSupportsQuerySelectionAndFullMode(t *testing.T) {
	t.Parallel()

	report := testReplayViewReport()

	filtered := BuildReplayOutput(report, ReplayOutputOptions{
		Events:       []ReplayEventRange{{Start: 2, End: 2}},
		FileFilters:  []string{"README.md"},
		EvidenceRefs: []string{"events.jsonl#seq=1"},
	})
	if len(filtered.SelectedEvents) != 1 || filtered.SelectedEvents[0].Seq != 2 {
		t.Fatalf("selected events = %#v", filtered.SelectedEvents)
	}
	if len(filtered.SelectedFiles) != 1 || filtered.SelectedFiles[0].Path != "README.md" {
		t.Fatalf("selected files = %#v", filtered.SelectedFiles)
	}
	if len(filtered.SelectedEvidence) != 1 || filtered.SelectedEvidence[0].Ref != "events.jsonl#seq=1" {
		t.Fatalf("selected evidence = %#v", filtered.SelectedEvidence)
	}
	if len(filtered.Timeline) != 0 {
		t.Fatalf("filtered compact replay should omit full timeline: %#v", filtered.Timeline)
	}

	full := BuildReplayOutput(report, ReplayOutputOptions{Full: true})
	if !full.Query.Full || full.Query.Compact {
		t.Fatalf("full replay query flags incorrect: %+v", full.Query)
	}
	if len(full.Timeline) != len(report.Timeline) {
		t.Fatalf("full replay timeline count = %d, want %d", len(full.Timeline), len(report.Timeline))
	}
	if !full.Timeline[0].Observed || full.Timeline[0].NormalizedType != "command.run" {
		t.Fatalf("full replay first timeline item = %#v", full.Timeline[0])
	}
	if len(full.SelectedEvents) != 0 || len(full.SelectedFiles) != 0 || len(full.SelectedEvidence) != 0 {
		t.Fatalf("full replay without filters should not select subsets: events=%d files=%d evidence=%d", len(full.SelectedEvents), len(full.SelectedFiles), len(full.SelectedEvidence))
	}
}

func testReplayViewReport() Report {
	return Report{
		SchemaVersion: 1,
		Kind:          replayKind,
		SessionID:     "ar_ses_test",
		GeneratedAt:   time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC),
		Verification: Verification{
			Valid:          true,
			IntegrityValid: true,
			OverallVerdict: verificationVerdictPassed,
		},
		ProcessContract: ProcessContract{ExitCode: 0, Meaning: "pass", Retryable: false},
		Reviewability: Reviewability{
			Status:                  reviewabilityStatusReady,
			CanEvaluateIntegrity:    true,
			CanEvaluateCodeQuality:  true,
			RequiresRerunValidation: false,
		},
		Summary: Summary{Provider: "codex", CommandCount: 2, ChangedFileCount: 1},
		QualityGates: QualityGates{
			Format: QualityGate{Status: qualityGateStatusPassed},
		},
		PatchSummary: PatchSummary{
			ChangedFiles: []PatchSummaryFile{{Path: "README.md", Category: "production"}},
		},
		PolicyChecks: []PolicyCheck{},
		ReviewFocus:  []ReviewFocusItem{},
		Privacy:      PrivacyReport{},
		Claims:       []Claim{},
		Outcome:      Outcome{Status: "completed"},
		Timeline: []TimelineItem{
			{
				Seq:            1,
				Time:           "2026-06-21T00:00:00Z",
				Source:         "provider",
				Type:           "command.run",
				NormalizedType: "command.run",
				Observed:       true,
				Confidence:     model.ConfidenceHigh,
				EvidenceRefs:   []string{"events.jsonl#seq=1"},
			},
			{
				Seq:            2,
				Time:           "2026-06-21T00:00:01Z",
				Source:         "provider",
				Type:           "command.run",
				NormalizedType: "command.run",
				Observed:       true,
				Confidence:     model.ConfidenceHigh,
				EvidenceRefs:   []string{"events.jsonl#seq=2"},
			},
		},
		Commands: []Command{
			{Command: "go test ./...", Kind: "test", Status: "success", Confidence: model.ConfidenceHigh},
		},
		InstructionFiles: []InstructionFile{},
		Files: []File{
			{Path: "README.md", Action: "modify", InFinalPatch: true},
		},
		EvidenceIndex: []EvidenceEntry{
			{Ref: "events.jsonl#seq=1", Type: "event"},
		},
		WorkspaceChange: WorkspaceChangeSummary{
			AgentModifiedCleanFiles:   []string{"README.md"},
			FinalDiffMatchesWorkspace: true,
			FinalDiffMatchesBranch:    true,
		},
		Risks:                []Risk{},
		FailedCommandDetails: []FailedCommandDetail{},
		Gaps:                 []string{},
		VerifierTasks:        []string{},
		Artifacts: []Artifact{
			{Name: "events.jsonl", Path: "events.jsonl", Hash: "sha256:events", Exists: true},
			{Name: "diffs/final.patch", Path: "diffs/final.patch", Hash: "sha256:patch", Exists: true},
			{Name: "replay.json", Path: "replay.json", Hash: "sha256:replay", Exists: true},
		},
	}
}

package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
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

func TestBuildFocusReportIncludesInstructionFiles(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.InstructionFiles = []InstructionFile{
		{
			Path:    "AGENTS.md",
			Hash:    "sha256:abc",
			Size:    12,
			MTime:   "2026-06-21T10:00:00Z",
			Summary: []string{"instructions: test"},
		},
	}

	focus := BuildFocusReport(replay)
	if len(focus.InstructionFiles) != 1 {
		t.Fatalf("instruction_files = %#v", focus.InstructionFiles)
	}
	if focus.InstructionFiles[0].Path != "AGENTS.md" {
		t.Fatalf("instruction path = %q", focus.InstructionFiles[0].Path)
	}
}

func TestBuildFocusReportIncludesWorkspaceContextWithoutBlocking(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.WorkspaceChange = WorkspaceChangeSummary{
		PreExistingDirtyFiles:     []string{"scratch.txt"},
		FinalDiffMatchesWorkspace: true,
		FinalDiffMatchesBranch:    true,
	}

	focus := BuildFocusReport(replay)
	if focus.Verdict != focusVerdictReviewRequired {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictReviewRequired)
	}
	if focus.WorkspaceChanges.PreExistingDirtyFiles[0] != "scratch.txt" {
		t.Fatalf("workspace change summary not propagated: %#v", focus.WorkspaceChanges)
	}
	hasTask := false
	for _, task := range focus.ReviewTasks {
		if task.Kind == "pre_existing_dirty" {
			hasTask = true
			if task.Priority == focusTaskPriorityP0 {
				t.Fatalf("pre-existing dirty task incorrectly marked as block: %#v", task)
			}
		}
	}
	if !hasTask {
		t.Fatalf("expected pre_existing_dirty task in focus output")
	}
}

func TestBuildFocusReportFlagsWorkspaceDiffMismatchAsBlockingTask(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.WorkspaceChange = WorkspaceChangeSummary{
		PreExistingDirtyFiles:     []string{"scratch.txt"},
		FinalDiffMatchesWorkspace: false,
		FinalDiffMatchesBranch:    true,
	}
	replay.Verification.IntegrityValid = true
	replay.Verification.FinalPatchHashValid = true

	focus := BuildFocusReport(replay)
	if focus.Verdict != focusVerdictBlock {
		t.Fatalf("verdict = %q, want %q", focus.Verdict, focusVerdictBlock)
	}
	hasMismatchTask := false
	for _, task := range focus.ReviewTasks {
		if task.Kind == "diff_mismatch" && task.Priority == focusTaskPriorityP0 {
			hasMismatchTask = true
			break
		}
	}
	if !hasMismatchTask {
		t.Fatalf("expected blocking diff_mismatch task: %#v", focus.ReviewTasks)
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

func TestBuildFocusReportTaskRankingByPriority(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Verification.IntegrityValid = false
	replay.QualityGates = qualityGates(qualityGateStatusFailed)
	replay.PolicyChecks = []PolicyCheck{
		{Name: "destructive_command_used", Status: policyCheckStatusWarn, Message: "Destructive commands were used.", Confidence: model.ConfidenceHigh, EvidenceRefs: []string{"events.jsonl#seq=120"}},
		{Name: "generated_file_changed", Status: policyCheckStatusWarn, Message: "Generated or unknown file changed.", Confidence: model.ConfidenceMedium, EvidenceRefs: []string{"events.jsonl#seq=121"}},
		{Name: "commit_created", Status: policyCheckStatusWarn, Message: "Commit command was observed.", Confidence: model.ConfidenceMedium, EvidenceRefs: []string{"events.jsonl#seq=122"}},
	}
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{Path: "main.go", Category: patchCategoryProduction, Symbols: []string{"BuildMain"}, EvidenceRefs: []string{"files/main.go"}},
			{Path: "go.mod", Category: patchCategoryDependency, Dependency: true, EvidenceRefs: []string{"files/go.mod"}},
		},
		ProductionChangedWithoutTestsChanged: true,
	}
	replay.Files = []File{
		{Path: "go.mod", Dependency: true, EvidenceRefs: []string{"files/go.mod"}},
	}
	replay.Commands = []Command{
		{Command: "go test ./...", Status: "failed", EvidenceRefs: []string{"events.jsonl#seq=123"}},
	}

	focus := BuildFocusReport(replay)
	if len(focus.ReviewTasks) == 0 {
		t.Fatal("expected review tasks for ranked focus report")
	}

	priority := focusTaskPriorityRank(focusTaskPriorityP0)
	for index := range focus.ReviewTasks {
		current := focusTaskPriorityRank(focus.ReviewTasks[index].Priority)
		if current < priority {
			t.Fatalf("tasks are not ranked by priority at index %d: %#v", index, focus.ReviewTasks)
		}
		priority = current
	}

	hasFailedCommand := false
	for _, task := range focus.ReviewTasks {
		if task.Kind == "failed_command" {
			hasFailedCommand = true
			break
		}
	}
	if !hasFailedCommand {
		t.Fatal("expected failed_command task with P0 priority")
	}
}

func TestBuildFocusReportTaskOrderAndIDsAreStable(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Verification.IntegrityValid = false
	replay.QualityGates = qualityGates(qualityGateStatusFailed)
	replay.PolicyChecks = []PolicyCheck{{Name: "generated_file_changed", Status: policyCheckStatusWarn, Message: "Generated or unknown file changed.", Confidence: model.ConfidenceHigh, EvidenceRefs: []string{"events.jsonl#seq=10"}}}
	replay.ReviewFocus = []ReviewFocusItem{
		{Message: "Session metadata was incomplete.", Confidence: model.ConfidenceLow},
		{Message: "Runtime context changed.", Confidence: model.ConfidenceLow},
	}

	first := BuildFocusReport(replay)
	second := BuildFocusReport(replay)

	if !reflect.DeepEqual(first.ReviewTasks, second.ReviewTasks) {
		t.Fatalf("review tasks are not stable across runs:\nfirst=%#v\nsecond=%#v", first.ReviewTasks, second.ReviewTasks)
	}
	if first.ReviewTasks[0].ID == "" || second.ReviewTasks[0].ID == "" {
		t.Fatal("expected stable ranked IDs to be assigned")
	}
	if first.ReviewTasks[0].ID != "task_001" {
		t.Fatalf("first task ID = %q, want %q", first.ReviewTasks[0].ID, "task_001")
	}
}

func TestBuildFocusReportNonInformationalTasksHaveEvidenceRefs(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Verification.IntegrityValid = false
	replay.QualityGates = qualityGates(qualityGateStatusFailed)
	replay.PolicyChecks = []PolicyCheck{
		{Name: "destructive_command_used", Status: policyCheckStatusWarn, Message: "Destructive commands were used.", Confidence: model.ConfidenceHigh, EvidenceRefs: []string{"events.jsonl#seq=10"}},
	}
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{Path: "main.go", Category: patchCategoryProduction, Symbols: []string{"BuildMain"}, EvidenceRefs: []string{"files/main.go"}},
		},
		ProductionChangedWithoutTestsChanged: true,
	}
	replay.Files = []File{{Path: "main.go", EvidenceRefs: []string{"files/main.go"}, Action: "modify"}}
	replay.Commands = []Command{{Command: "make test", Status: "failed", EvidenceRefs: []string{"events.jsonl#seq=11"}}}

	focus := BuildFocusReport(replay)
	for _, task := range focus.ReviewTasks {
		if task.Priority == focusTaskPriorityP3 {
			continue
		}
		if task.Kind == "evidence_gap" && len(task.EvidenceRefs) == 0 {
			continue
		}
		if len(task.EvidenceRefs) == 0 {
			t.Fatalf("non-informational task %q should include evidence refs: %#v", task.Kind, task)
		}
	}
}

func TestBuildFocusReportFailedCommandTaskIncludesPathsAndSymbols(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.Commands = []Command{
		{
			Command:      "cat main.go",
			Status:       "failed",
			EvidenceRefs: []string{"events.jsonl#seq=300"},
		},
	}
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{
				Path:         "main.go",
				Category:     patchCategoryProduction,
				Symbols:      []string{"BuildMain"},
				EvidenceRefs: []string{"files/main.go"},
			},
		},
		ProductionChangedWithoutTestsChanged: false,
	}
	replay.Files = []File{{Path: "main.go", Action: "modify", EvidenceRefs: []string{"files/main.go"}}}

	focus := BuildFocusReport(replay)
	found := false
	for _, task := range focus.ReviewTasks {
		if task.Kind != "failed_command" || !strings.HasPrefix(task.Question, "Failed command: cat main.go") {
			continue
		}
		found = true
		if !containsSlice(task.Paths, "main.go") {
			t.Fatalf("expected failed command task to include changed file path: %#v", task.Paths)
		}
		if !containsSlice(task.Symbols, "BuildMain") {
			t.Fatalf("expected failed command task to include symbol hints: %#v", task.Symbols)
		}
		break
	}
	if !found {
		t.Fatal("expected failed command task for cat main.go")
	}
}

func TestBuildFocusReportChangedFileDossierForProductionPath(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{
				Path:         "internal/replay/focus.go",
				Action:       "modify",
				Category:     patchCategoryProduction,
				Symbols:      []string{"BuildFocusReport"},
				EvidenceRefs: []string{"files/internal/replay/focus.go"},
			},
		},
	}
	replay.Files = []File{
		{
			Path:         "internal/replay/focus.go",
			Action:       "modify",
			EvidenceRefs: []string{"files/internal/replay/focus.go"},
		},
	}
	replay.PolicyChecks = []PolicyCheck{
		{Name: "target_file_read_before_edit", Status: policyCheckStatusPass, Message: "Read commands were observed before edits.", EvidenceRefs: []string{"events.jsonl#seq=1"}},
		{Name: "related_context_read_before_edit", Status: policyCheckStatusPass, Message: "Changed-file context was read before edits.", EvidenceRefs: []string{"events.jsonl#seq=2"}},
	}
	replay.Commands = []Command{
		{Command: "cat internal/replay/focus.go", EvidenceRefs: []string{"events.jsonl#seq=10"}},
		{Command: "go test ./internal/replay", EvidenceRefs: []string{"events.jsonl#seq=11"}},
	}

	focus := BuildFocusReport(replay)
	if len(focus.ChangedFiles) != 1 {
		t.Fatalf("changed_files = %#v", focus.ChangedFiles)
	}

	file := focus.ChangedFiles[0]
	switch {
	case file.Path != "internal/replay/focus.go":
		t.Fatalf("changed file path = %q", file.Path)
	case file.Category != patchCategoryProduction:
		t.Fatalf("category = %q, want %q", file.Category, patchCategoryProduction)
	case !containsSlice(file.Symbols, "BuildFocusReport"):
		t.Fatalf("symbols = %#v", file.Symbols)
	case file.ReadBeforeEdit != policyCheckStatusPass:
		t.Fatalf("read_before_edit = %q, want %q", file.ReadBeforeEdit, policyCheckStatusPass)
	case file.RelatedContextRead != policyCheckStatusPass:
		t.Fatalf("related_context_read = %q, want %q", file.RelatedContextRead, policyCheckStatusPass)
	case !containsSlice(file.CommandsTouchingFile, "cat internal/replay/focus.go"):
		t.Fatalf("commands_touching_file = %#v", file.CommandsTouchingFile)
	case !containsSlice(file.TestsRelated, "go test ./internal/replay"):
		t.Fatalf("tests_related = %#v", file.TestsRelated)
	case !containsSlice(file.EvidenceRefs, "files/internal/replay/focus.go"):
		t.Fatalf("evidence_refs = %#v", file.EvidenceRefs)
	}
	if len(file.ReviewReasons) != 0 {
		t.Fatalf("review_reasons should be empty for clean production dossier: %#v", file.ReviewReasons)
	}
}

func TestBuildFocusReportChangedFileDossierForDocsOnly(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{
				Path:         "docs/overview.md",
				Action:       "modify",
				Category:     patchCategoryDocs,
				EvidenceRefs: []string{"files/docs/overview.md"},
			},
		},
	}
	replay.Files = []File{
		{
			Path:         "docs/overview.md",
			Action:       "modify",
			EvidenceRefs: []string{"files/docs/overview.md"},
		},
	}

	focus := BuildFocusReport(replay)
	if len(focus.ChangedFiles) != 1 {
		t.Fatalf("changed_files = %#v", focus.ChangedFiles)
	}
	file := focus.ChangedFiles[0]
	if file.Category != patchCategoryDocs {
		t.Fatalf("category = %q, want %q", file.Category, patchCategoryDocs)
	}
}

func TestBuildFocusReportChangedFileDossierForDependency(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{
				Path:         "go.mod",
				Action:       "modify",
				Category:     patchCategoryDependency,
				Dependency:   true,
				EvidenceRefs: []string{"files/go.mod"},
			},
		},
	}
	replay.Files = []File{
		{
			Path:         "go.mod",
			Action:       "modify",
			Dependency:   true,
			EvidenceRefs: []string{"files/go.mod"},
		},
	}
	replay.Commands = []Command{{Command: "cat go.mod", EvidenceRefs: []string{"events.jsonl#seq=1"}}}

	focus := BuildFocusReport(replay)
	if len(focus.ChangedFiles) != 1 {
		t.Fatalf("changed_files = %#v", focus.ChangedFiles)
	}
	file := focus.ChangedFiles[0]
	if !file.Dependency {
		t.Fatalf("dependency = %t, want true", file.Dependency)
	}
	if !containsSlice(file.ReviewReasons, "Dependency file changed.") {
		t.Fatalf("review_reasons = %#v", file.ReviewReasons)
	}
}

func TestBuildFocusReportChangedFileDossierForSensitiveAndNoCommandInference(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{
				Path:         "internal/secrets/keys.go",
				Action:       "modify",
				Category:     patchCategoryProduction,
				Sensitive:    true,
				EvidenceRefs: []string{"files/internal/secrets/keys.go"},
			},
		},
		ProductionChangedWithoutTestsChanged: true,
	}
	replay.Files = []File{
		{
			Path:         "internal/secrets/keys.go",
			Action:       "modify",
			Sensitive:    true,
			EvidenceRefs: []string{"files/internal/secrets/keys.go"},
		},
	}
	replay.PolicyChecks = []PolicyCheck{
		{Name: "target_file_read_before_edit", Status: policyCheckStatusFail, Message: "No read command was observed before edits.", EvidenceRefs: []string{"events.jsonl#seq=2"}},
		{Name: "related_context_read_before_edit", Status: policyCheckStatusWarn, Message: "Read commands were observed, but none obviously referenced changed files.", EvidenceRefs: []string{"events.jsonl#seq=3"}},
	}
	replay.Commands = []Command{
		{Command: "go test ./...", EvidenceRefs: []string{"events.jsonl#seq=10"}},
		{Command: "cat internal/secrets/keys.go", Status: "failed", EvidenceRefs: []string{"events.jsonl#seq=11"}},
	}

	focus := BuildFocusReport(replay)
	if len(focus.ChangedFiles) != 1 {
		t.Fatalf("changed_files = %#v", focus.ChangedFiles)
	}
	file := focus.ChangedFiles[0]
	if !file.Sensitive {
		t.Fatalf("sensitive = %t, want true", file.Sensitive)
	}
	if !containsSlice(file.ReviewReasons, "Sensitive file changed.") {
		t.Fatalf("review_reasons = %#v", file.ReviewReasons)
	}
	if file.ReadBeforeEdit != policyCheckStatusFail {
		t.Fatalf("read_before_edit = %q, want %q", file.ReadBeforeEdit, policyCheckStatusFail)
	}
	if file.RelatedContextRead != policyCheckStatusWarn {
		t.Fatalf("related_context_read = %q, want %q", file.RelatedContextRead, policyCheckStatusWarn)
	}
	if !containsSlice(file.ReviewReasons, "A failed command touched this file.") {
		t.Fatalf("review_reasons = %#v", file.ReviewReasons)
	}
	if !containsSlice(file.ReviewReasons, "No file-specific test command was identified for this file.") {
		t.Fatalf("review_reasons = %#v", file.ReviewReasons)
	}
	if len(file.TestsRelated) != 0 {
		t.Fatalf("tests_related should remain empty for broad tests: %#v", file.TestsRelated)
	}
}

func TestBuildFocusReportChangedFileDossierCommandAssociationForPackages(t *testing.T) {
	t.Parallel()

	replay := focusBaseReplayReport()
	replay.PatchSummary = PatchSummary{
		ChangedFiles: []PatchSummaryFile{
			{
				Path:         "internal/replay/focus.go",
				Action:       "modify",
				Category:     patchCategoryProduction,
				EvidenceRefs: []string{"files/internal/replay/focus.go"},
			},
			{
				Path:         "internal/tool/reporter.go",
				Action:       "modify",
				Category:     patchCategoryProduction,
				EvidenceRefs: []string{"files/internal/tool/reporter.go"},
			},
		},
	}
	replay.Files = []File{
		{Path: "internal/replay/focus.go", Action: "modify", EvidenceRefs: []string{"files/internal/replay/focus.go"}},
		{Path: "internal/tool/reporter.go", Action: "modify", EvidenceRefs: []string{"files/internal/tool/reporter.go"}},
	}
	replay.Commands = []Command{
		{Command: "go test ./...", EvidenceRefs: []string{"events.jsonl#seq=10"}},
		{Command: "go test ./internal/replay/...", EvidenceRefs: []string{"events.jsonl#seq=11"}},
	}

	focus := BuildFocusReport(replay)
	if len(focus.ChangedFiles) != 2 {
		t.Fatalf("changed_files len = %d", len(focus.ChangedFiles))
	}

	var replayFile, toolFile FocusChangedFile
	for _, file := range focus.ChangedFiles {
		switch file.Path {
		case "internal/replay/focus.go":
			replayFile = file
		case "internal/tool/reporter.go":
			toolFile = file
		}
	}
	if replayFile.Path == "" || toolFile.Path == "" {
		t.Fatalf("changed files missing: %#v", focus.ChangedFiles)
	}
	if !containsSlice(replayFile.TestsRelated, "go test ./internal/replay/...") {
		t.Fatalf("replay tests_related = %#v", replayFile.TestsRelated)
	}
	if len(toolFile.TestsRelated) != 0 {
		t.Fatalf("tool tests_related should be empty: %#v", toolFile.TestsRelated)
	}
}

func containsSlice(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
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

package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ReplayEventRange struct {
	Start int64
	End   int64
}

type ReplayOutputOptions struct {
	Full         bool
	Compact      bool
	Events       []ReplayEventRange
	FileFilters  []string
	EvidenceRefs []string
}

type ReplayQuery struct {
	Full     bool     `json:"full,omitempty"`
	Compact  bool     `json:"compact,omitempty"`
	Events   []string `json:"events,omitempty"`
	Files    []string `json:"files,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

type ReplayIndexSummary struct {
	Count       int    `json:"count"`
	ArtifactRef string `json:"artifact_ref"`
	SHA256      string `json:"sha256,omitempty"`
}

type ReplayFileIndex struct {
	Count       int            `json:"count"`
	ArtifactRef string         `json:"artifact_ref"`
	SHA256      string         `json:"sha256,omitempty"`
	ByCategory  map[string]int `json:"by_category,omitempty"`
}

type ReplayEvidenceIndex struct {
	Count       int    `json:"count"`
	ArtifactRef string `json:"artifact_ref"`
	SHA256      string `json:"sha256,omitempty"`
}

type ReplayTimelineRange struct {
	Range          string   `json:"range"`
	NormalizedType string   `json:"normalized_type"`
	Count          int      `json:"count"`
	EvidenceRefs   []string `json:"evidence_refs,omitempty"`
}

type ReplayIndexes struct {
	Events         ReplayIndexSummary    `json:"events"`
	Files          ReplayFileIndex       `json:"files"`
	Evidence       ReplayEvidenceIndex   `json:"evidence"`
	TimelineRanges []ReplayTimelineRange `json:"timeline_ranges,omitempty"`
}

type ReplayOutput struct {
	SchemaVersion        int                    `json:"schema_version"`
	Kind                 string                 `json:"kind"`
	SessionID            string                 `json:"session_id"`
	GeneratedAt          time.Time              `json:"generated_at"`
	Source               Source                 `json:"source"`
	Verification         Verification           `json:"verification"`
	ProcessContract      ProcessContract        `json:"process_contract"`
	Reviewability        Reviewability          `json:"reviewability"`
	Summary              Summary                `json:"summary"`
	EvaluatorSignals     EvaluatorSignals       `json:"evaluator_signals"`
	QualityGates         QualityGates           `json:"quality_gates"`
	PatchSummary         PatchSummary           `json:"patch_summary"`
	PolicyChecks         []PolicyCheck          `json:"policy_checks"`
	ReviewFocus          []ReviewFocusItem      `json:"review_focus"`
	Privacy              PrivacyReport          `json:"privacy"`
	Claims               []Claim                `json:"claims"`
	Outcome              Outcome                `json:"outcome"`
	Indexes              ReplayIndexes          `json:"indexes"`
	Query                ReplayQuery            `json:"query,omitempty"`
	SelectedEvents       []TimelineItem         `json:"selected_events,omitempty"`
	SelectedFiles        []File                 `json:"selected_files,omitempty"`
	SelectedEvidence     []EvidenceEntry        `json:"selected_evidence,omitempty"`
	Timeline             []TimelineItem         `json:"timeline,omitempty"`
	Commands             []Command              `json:"commands,omitempty"`
	Files                []File                 `json:"files,omitempty"`
	EvidenceIndex        []EvidenceEntry        `json:"evidence_index,omitempty"`
	InstructionFiles     []InstructionFile      `json:"instruction_files,omitempty"`
	Risks                []Risk                 `json:"risks,omitempty"`
	FailedCommandDetails []FailedCommandDetail  `json:"failed_command_details,omitempty"`
	Gaps                 []string               `json:"gaps"`
	VerifierTasks        []string               `json:"verifier_tasks"`
	Artifacts            []Artifact             `json:"artifacts"`
	WorkspaceChange      WorkspaceChangeSummary `json:"workspace_change_summary"`
}

func BuildReplayOutput(report Report, options ReplayOutputOptions) ReplayOutput {
	indexes := buildReplayIndexes(report)
	query := ReplayQuery{
		Full:     options.Full,
		Compact:  options.Compact || !options.Full,
		Events:   replayEventRangeStrings(options.Events),
		Files:    append([]string(nil), options.FileFilters...),
		Evidence: append([]string(nil), options.EvidenceRefs...),
	}
	selectedEvents := selectTimelineEntries(report.Timeline, options.Events)
	selectedFiles := selectFilesByFilter(report.Files, options.FileFilters)
	selectedEvidence := selectEvidenceEntries(report.EvidenceIndex, options.EvidenceRefs)

	output := ReplayOutput{
		SchemaVersion:        report.SchemaVersion,
		Kind:                 report.Kind,
		SessionID:            report.SessionID,
		GeneratedAt:          report.GeneratedAt,
		Source:               report.Source,
		Verification:         report.Verification,
		ProcessContract:      report.ProcessContract,
		Reviewability:        report.Reviewability,
		Summary:              report.Summary,
		EvaluatorSignals:     report.EvaluatorSignals,
		QualityGates:         report.QualityGates,
		PatchSummary:         report.PatchSummary,
		PolicyChecks:         report.PolicyChecks,
		ReviewFocus:          report.ReviewFocus,
		Privacy:              report.Privacy,
		Claims:               report.Claims,
		Outcome:              report.Outcome,
		Indexes:              indexes,
		Query:                query,
		SelectedEvents:       selectedEvents,
		SelectedFiles:        selectedFiles,
		SelectedEvidence:     selectedEvidence,
		Commands:             append([]Command(nil), report.Commands...),
		Files:                append([]File(nil), report.Files...),
		EvidenceIndex:        append([]EvidenceEntry(nil), report.EvidenceIndex...),
		InstructionFiles:     append([]InstructionFile(nil), report.InstructionFiles...),
		Risks:                append([]Risk(nil), report.Risks...),
		FailedCommandDetails: append([]FailedCommandDetail(nil), report.FailedCommandDetails...),
		Gaps:                 append([]string(nil), report.Gaps...),
		VerifierTasks:        append([]string(nil), report.VerifierTasks...),
		Artifacts:            append([]Artifact(nil), report.Artifacts...),
		WorkspaceChange:      report.WorkspaceChange,
	}

	if options.Full {
		output.Timeline = append([]TimelineItem(nil), report.Timeline...)
	}

	return output
}

func buildReplayIndexes(report Report) ReplayIndexes {
	eventsArtifact := artifactForRef(report.Artifacts, "events.jsonl")
	filesArtifact := artifactForRef(report.Artifacts, finalPatchEvidenceRef)
	evidenceArtifact := artifactForRef(report.Artifacts, "replay.json")
	if evidenceArtifact == nil {
		evidenceArtifact = artifactForRef(report.Artifacts, "replay.json")
	}

	indexes := ReplayIndexes{
		Events: ReplayIndexSummary{
			Count:       len(report.Timeline),
			ArtifactRef: artifactRefOrDefault(eventsArtifact, "events.jsonl"),
			SHA256:      artifactHash(eventsArtifact),
		},
		Files: ReplayFileIndex{
			Count:       len(report.Files),
			ArtifactRef: artifactRefOrDefault(filesArtifact, finalPatchEvidenceRef),
			SHA256:      artifactHash(filesArtifact),
			ByCategory:  map[string]int{},
		},
		Evidence: ReplayEvidenceIndex{
			Count:       len(report.EvidenceIndex),
			ArtifactRef: artifactRefOrDefault(evidenceArtifact, "replay.json"),
			SHA256:      hashBytes(mustJSON(report.EvidenceIndex)),
		},
		TimelineRanges: buildTimelineRanges(report.Timeline),
	}

	for _, file := range report.PatchSummary.ChangedFiles {
		if file.Category == "" {
			continue
		}
		indexes.Files.ByCategory[file.Category]++
	}
	if len(indexes.Files.ByCategory) == 0 {
		indexes.Files.ByCategory = nil
	}

	return indexes
}

func buildTimelineRanges(timeline []TimelineItem) []ReplayTimelineRange {
	if len(timeline) == 0 {
		return nil
	}
	ranges := make([]ReplayTimelineRange, 0)
	start := timeline[0]
	currentType := normalizedTimelineType(start)
	count := 1
	refs := append([]string(nil), start.EvidenceRefs...)
	lastSeq := start.Seq
	for index := 1; index < len(timeline); index++ {
		item := timeline[index]
		itemType := normalizedTimelineType(item)
		if itemType != currentType {
			ranges = append(ranges, ReplayTimelineRange{
				Range:          fmt.Sprintf("%d-%d", start.Seq, lastSeq),
				NormalizedType: currentType,
				Count:          count,
				EvidenceRefs:   uniqueSorted(refs),
			})
			start = item
			currentType = itemType
			count = 1
			refs = append([]string(nil), item.EvidenceRefs...)
		} else {
			count++
			refs = append(refs, item.EvidenceRefs...)
		}
		lastSeq = item.Seq
	}
	ranges = append(ranges, ReplayTimelineRange{
		Range:          fmt.Sprintf("%d-%d", start.Seq, lastSeq),
		NormalizedType: currentType,
		Count:          count,
		EvidenceRefs:   uniqueSorted(refs),
	})

	return ranges
}

func normalizedTimelineType(item TimelineItem) string {
	if item.NormalizedType != "" {
		return item.NormalizedType
	}
	if item.Type != "" {
		return item.Type
	}
	return "unknown"
}

func selectTimelineEntries(timeline []TimelineItem, ranges []ReplayEventRange) []TimelineItem {
	if len(ranges) == 0 {
		return nil
	}
	selected := make([]TimelineItem, 0)
	for _, item := range timeline {
		for _, eventRange := range ranges {
			if item.Seq >= eventRange.Start && item.Seq <= eventRange.End {
				selected = append(selected, item)
				break
			}
		}
	}
	return selected
}

func selectFilesByFilter(files []File, filters []string) []File {
	if len(filters) == 0 {
		return nil
	}
	selected := make([]File, 0)
	for _, file := range files {
		if fileMatchesAnyFilter(file.Path, filters) {
			selected = append(selected, file)
		}
	}
	return selected
}

func selectEvidenceEntries(entries []EvidenceEntry, refs []string) []EvidenceEntry {
	if len(refs) == 0 {
		return nil
	}
	want := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		want[strings.TrimSpace(ref)] = struct{}{}
	}
	selected := make([]EvidenceEntry, 0)
	for _, entry := range entries {
		if _, ok := want[entry.Ref]; ok {
			selected = append(selected, entry)
		}
	}
	return selected
}

func fileMatchesAnyFilter(path string, filters []string) bool {
	normalizedPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	for _, filter := range filters {
		filter = strings.ToLower(filepath.ToSlash(strings.TrimSpace(filter)))
		if filter == "" {
			continue
		}
		if normalizedPath == filter || strings.HasSuffix(normalizedPath, "/"+filter) || strings.Contains(normalizedPath, filter) {
			return true
		}
	}
	return false
}

func replayEventRangeStrings(ranges []ReplayEventRange) []string {
	if len(ranges) == 0 {
		return nil
	}
	out := make([]string, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, fmt.Sprintf("%d-%d", r.Start, r.End))
	}
	return out
}

func artifactForRef(artifacts []Artifact, ref string) *Artifact {
	ref = strings.TrimSpace(filepath.ToSlash(ref))
	for index := range artifacts {
		path := filepath.ToSlash(strings.TrimSpace(artifacts[index].Path))
		if path == ref || filepath.Base(path) == filepath.Base(ref) {
			return &artifacts[index]
		}
	}
	return nil
}

func artifactRefOrDefault(artifact *Artifact, fallback string) string {
	if artifact == nil {
		return fallback
	}
	if artifact.Path != "" {
		return filepath.ToSlash(artifact.Path)
	}
	return fallback
}

func artifactHash(artifact *Artifact) string {
	if artifact == nil {
		return ""
	}
	return artifact.Hash
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

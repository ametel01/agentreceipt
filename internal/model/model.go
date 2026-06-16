package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

const SchemaVersion = 1

type Confidence string

const (
	ConfidenceHigh      Confidence = "high"
	ConfidenceMedium    Confidence = "medium"
	ConfidenceLowMedium Confidence = "low-medium"
	ConfidenceLow       Confidence = "low"
	ConfidenceNone      Confidence = "none"
)

type RiskLevel string

const (
	RiskInfo     RiskLevel = "info"
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type SessionState string

const (
	SessionStateIdle       SessionState = "idle"
	SessionStateStarting   SessionState = "starting"
	SessionStateActive     SessionState = "active"
	SessionStateFinalizing SessionState = "finalizing"
	SessionStateFinalized  SessionState = "finalized"
	SessionStateVerified   SessionState = "verified"
)

type Event struct {
	EventID   string         `json:"event_id"`
	SessionID string         `json:"session_id"`
	Seq       int64          `json:"seq"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Type      string         `json:"type"`
	Provider  string         `json:"provider,omitempty"`
	CWD       string         `json:"cwd,omitempty"`
	Payload   map[string]any `json:"payload"`
	PrevHash  string         `json:"prev_hash,omitempty"`
	EventHash string         `json:"event_hash,omitempty"`
}

type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	SessionID     string       `json:"session_id"`
	State         SessionState `json:"state"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Artifacts     Artifacts    `json:"artifacts"`
	EventCount    int64        `json:"event_count"`
	Warnings      []Warning    `json:"warnings,omitempty"`
}

type Artifacts struct {
	EventsJSONL   string `json:"events_jsonl"`
	ReceiptJSON   string `json:"receipt_json"`
	ReceiptMD     string `json:"receipt_md"`
	ReviewMD      string `json:"review_md"`
	ManifestJSON  string `json:"manifest_json"`
	FinalPatch    string `json:"final_patch"`
	ReceiptSig    string `json:"receipt_sig"`
	CodexTraceDir string `json:"codex_trace_dir"`
}

type Receipt struct {
	SchemaVersion     int               `json:"schema_version"`
	SessionID         string            `json:"session_id"`
	CreatedAt         time.Time         `json:"created_at"`
	Mode              string            `json:"mode"`
	Agent             Agent             `json:"agent"`
	Repo              Repo              `json:"repo"`
	Summary           Summary           `json:"summary"`
	CaptureConfidence CaptureConfidence `json:"capture_confidence"`
	Risk              Risk              `json:"risk"`
	Verification      Verification      `json:"verification"`
	Warnings          []Warning         `json:"warnings,omitempty"`
}

type Agent struct {
	Provider           string     `json:"provider"`
	ProviderConfidence Confidence `json:"provider_confidence"`
	Model              string     `json:"model,omitempty"`
}

type Repo struct {
	Root        string `json:"root"`
	BranchStart string `json:"branch_start,omitempty"`
	BranchEnd   string `json:"branch_end,omitempty"`
	CommitStart string `json:"commit_start,omitempty"`
	CommitEnd   string `json:"commit_end,omitempty"`
	DirtyStart  bool   `json:"dirty_start"`
	DirtyEnd    bool   `json:"dirty_end"`
}

type Summary struct {
	ChangedFiles        []ChangedFile     `json:"changed_files"`
	DetectedCommands    []DetectedCommand `json:"detected_commands"`
	TestDetected        bool              `json:"test_detected"`
	LintDetected        bool              `json:"lint_detected"`
	TypecheckDetected   bool              `json:"typecheck_detected"`
	DurationSeconds     int64             `json:"duration_seconds"`
	MissingEvidenceGaps []string          `json:"missing_evidence_gaps,omitempty"`
}

type ChangedFile struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	Sensitive  bool   `json:"sensitive"`
	Dependency bool   `json:"dependency"`
}

type DetectedCommand struct {
	Command    string     `json:"command"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Source     string     `json:"source"`
	Confidence Confidence `json:"confidence"`
}

type CaptureConfidence struct {
	GitDiff            Confidence `json:"git_diff"`
	FilesystemWrites   Confidence `json:"filesystem_writes"`
	ProviderToolEvents Confidence `json:"provider_tool_events"`
	FileReads          Confidence `json:"file_reads"`
	NetworkCalls       Confidence `json:"network_calls"`
}

type Risk struct {
	Level   RiskLevel    `json:"level"`
	Reasons []RiskReason `json:"reasons"`
}

type RiskReason struct {
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Level      RiskLevel  `json:"level"`
	Confidence Confidence `json:"confidence"`
}

type Verification struct {
	EventChainHash     string `json:"event_chain_hash"`
	DiffHash           string `json:"diff_hash"`
	ManifestHash       string `json:"manifest_hash"`
	ReceiptHash        string `json:"receipt_hash"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	Signature          string `json:"signature"`
	Valid              bool   `json:"valid"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ReviewReport struct {
	SchemaVersion int               `json:"schema_version"`
	SessionID     string            `json:"session_id"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Risk          Risk              `json:"risk"`
	Confidence    CaptureConfidence `json:"confidence"`
	Focus         []string          `json:"focus"`
	Gaps          []string          `json:"gaps"`
	Warnings      []Warning         `json:"warnings,omitempty"`
}

func NewManifest(sessionID string, now time.Time, artifacts Artifacts) Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		SessionID:     sessionID,
		State:         SessionStateStarting,
		CreatedAt:     now.UTC(),
		UpdatedAt:     now.UTC(),
		Artifacts:     artifacts,
	}
}

func MarshalCanonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal canonical json: %w", err)
	}

	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func DecodeReceipt(data []byte) (Receipt, []string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Receipt{}, nil, fmt.Errorf("decode receipt object: %w", err)
	}
	known := map[string]struct{}{
		"schema_version":     {},
		"session_id":         {},
		"created_at":         {},
		"mode":               {},
		"agent":              {},
		"repo":               {},
		"summary":            {},
		"capture_confidence": {},
		"risk":               {},
		"verification":       {},
		"warnings":           {},
	}
	unknown := make([]string, 0)
	for key := range raw {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Receipt{}, nil, fmt.Errorf("decode receipt: %w", err)
	}

	return receipt, unknown, nil
}

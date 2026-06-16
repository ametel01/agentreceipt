package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMarshalCanonicalIsStableForMaps(t *testing.T) {
	t.Parallel()

	value := map[string]any{"z": 1, "a": map[string]any{"b": true, "a": false}}
	first, err := MarshalCanonical(value)
	if err != nil {
		t.Fatalf("MarshalCanonical() error = %v", err)
	}
	second, err := MarshalCanonical(value)
	if err != nil {
		t.Fatalf("MarshalCanonical() second error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("canonical JSON is unstable:\n%s\n%s", first, second)
	}
	if strings.Contains(string(first), "\n") {
		t.Fatalf("canonical JSON should not contain trailing newline: %q", first)
	}
}

func TestReceiptJSONRoundTripWithUnknownFields(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"schema_version": 1,
		"session_id": "ar_ses_test",
		"created_at": "2026-06-16T00:00:00Z",
		"mode": "sidecar",
		"agent": {"provider": "codex", "provider_confidence": "medium"},
		"repo": {"root": "/repo", "dirty_start": false, "dirty_end": true},
		"summary": {"changed_files": [], "detected_commands": [], "test_detected": false, "lint_detected": false, "typecheck_detected": false, "duration_seconds": 1},
		"capture_confidence": {"git_diff": "high", "filesystem_writes": "high", "provider_tool_events": "medium", "file_reads": "low-medium", "network_calls": "low"},
		"risk": {"level": "low", "reasons": []},
		"verification": {"event_chain_hash": "sha256:test", "diff_hash": "sha256:diff", "manifest_hash": "sha256:manifest", "signature": "sig", "valid": true},
		"future_field": true
	}`)

	receipt, unknown, err := DecodeReceipt(data)
	if err != nil {
		t.Fatalf("DecodeReceipt() error = %v", err)
	}
	if receipt.SessionID != "ar_ses_test" {
		t.Fatalf("SessionID = %q, want ar_ses_test", receipt.SessionID)
	}
	if len(unknown) != 1 || unknown[0] != "future_field" {
		t.Fatalf("unknown fields = %v, want [future_field]", unknown)
	}
	if _, err := json.Marshal(receipt); err != nil {
		t.Fatalf("receipt JSON marshal error = %v", err)
	}
}

func TestNewManifestDefaults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 1, 2, 3, 0, time.FixedZone("PHT", 8*60*60))
	manifest := NewManifest("ar_ses_test", now, Artifacts{EventsJSONL: "events.jsonl"})
	if manifest.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", manifest.SchemaVersion, SchemaVersion)
	}
	if manifest.State != SessionStateStarting {
		t.Fatalf("State = %q, want %q", manifest.State, SessionStateStarting)
	}
	if manifest.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt location = %v, want UTC", manifest.CreatedAt.Location())
	}
}

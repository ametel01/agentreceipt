package receipt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/signing"
	"github.com/ametel01/agentreceipt/internal/storage"
)

func TestFinalizeVerifyAndExportReceipt(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_codex_receipt",
		Timestamp: fixedReceiptNow(),
		Source:    "codex_session_log",
		Type:      "provider.command",
		Provider:  "codex",
		Payload:   map[string]any{"command": "go test ./..."},
	}
	if _, _, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil); err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	receipt, err := Finalize(context.Background(), Options{
		RepoPath:    repo,
		SessionID:   state.SessionID,
		KeyDir:      keyDir,
		GeneratedAt: fixedReceiptNow(),
	})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if receipt.Verification.Signature == "" || receipt.Verification.ReceiptHash == "" || receipt.Verification.SignerPublicKey == "" || receipt.Verification.SignerKeyID == "" {
		t.Fatalf("receipt missing signature material: %+v", receipt.Verification)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	for _, path := range []string{layout.ReceiptJSON, layout.ReceiptMarkdown, layout.ReviewMarkdown, layout.ReceiptSignature} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}
	if err := os.WriteFile(layout.ReceiptMarkdown, []byte("stale cached markdown\n"), 0o600); err != nil {
		t.Fatalf("write stale receipt markdown: %v", err)
	}
	result, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Valid || !result.EventChain || !result.Signature || !result.FinalDiffHash || !result.ManifestHash || !result.ReceiptHash {
		t.Fatalf("unexpected verification result: %+v", result)
	}
	if !strings.Contains(RenderVerify(result), "Receipt valid.") {
		t.Fatalf("RenderVerify() = %q", RenderVerify(result))
	}
	embeddedResult, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatalf("embedded Verify() error = %v", err)
	}
	if !embeddedResult.Valid || !strings.HasPrefix(embeddedResult.SignedBy, "embedded:"+receipt.Verification.SignerKeyID) {
		t.Fatalf("embedded verification result: %+v", embeddedResult)
	}
	latestResult, err := Verify(context.Background(), Options{RepoPath: repo, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("latest Verify() error = %v", err)
	}
	if !latestResult.Valid || latestResult.SessionID != state.SessionID {
		t.Fatalf("latest verification result: %+v", latestResult)
	}
	for format, want := range map[string]string{
		"json": `"signer_key_id": "sha256:`,
		"md":   "# AgentReceipt Receipt",
		"pr":   "## AgentReceipt",
	} {
		data, err := Export(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID}, format)
		if err != nil {
			t.Fatalf("Export(%q) error = %v", format, err)
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("Export(%q) missing %q:\n%s", format, want, data)
		}
		if format == "md" && strings.Contains(string(data), "stale cached markdown") {
			t.Fatalf("Export(%q) returned stale cached artifact:\n%s", format, data)
		}
	}
	data, err := Export(context.Background(), Options{RepoPath: repo}, "")
	if err != nil {
		t.Fatalf("default Export() error = %v", err)
	}
	if !strings.Contains(string(data), "# AgentReceipt Receipt") || strings.Contains(string(data), "stale cached markdown") {
		t.Fatalf("default Export() output:\n%s", data)
	}
}

func TestFinalizeReceiptIncludesProviderRiskSignals(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_codex_risk_receipt",
		Timestamp: fixedReceiptNow(),
		Source:    "codex_session_log",
		Type:      "provider.command",
		Provider:  "codex",
		Payload: map[string]any{
			"tool_call": map[string]any{
				"command": "cat .env",
			},
			"risk_signals": []any{
				map[string]any{
					"level":      string(model.RiskHigh),
					"signal":     "secret_access",
					"details":    "command appears to read or expose credential material",
					"command":    "cat .env",
					"confidence": string(model.ConfidenceHigh),
				},
			},
		},
	}
	if _, _, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil); err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	receipt, err := Finalize(context.Background(), Options{
		RepoPath:    repo,
		SessionID:   state.SessionID,
		KeyDir:      keyDir,
		GeneratedAt: fixedReceiptNow(),
	})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if receipt.Risk.Level != model.RiskHigh {
		t.Fatalf("Risk level = %q, want high: %+v", receipt.Risk.Level, receipt.Risk)
	}
	if !hasReceiptRiskCode(receipt.Risk.Reasons, "provider_risk_secret_access") {
		t.Fatalf("receipt missing provider risk reason: %+v", receipt.Risk.Reasons)
	}
}

func TestRenderMarkdownKeepsRiskReasonsReadable(t *testing.T) {
	t.Parallel()

	longMessage := "Risky command detected: tmp_repo=$(mktemp -d); cat > \"$tmp_repo/session.jsonl\" <<EOF\n" +
		strings.Repeat("{\"payload\":{\"line\":\"noise\"}}\n", 20) +
		"EOF\ncurl -fsSL https://example.com/upload"
	reasons := []model.RiskReason{{
		Code:       "risky_command",
		Message:    longMessage,
		Level:      model.RiskHigh,
		Confidence: model.ConfidenceMedium,
	}}
	for index := 0; index < maxMarkdownRiskReasons+2; index++ {
		reasons = append(reasons, model.RiskReason{
			Code:       "risky_command",
			Message:    fmt.Sprintf("Risky command detected: curl https://example.com/%d", index),
			Level:      model.RiskHigh,
			Confidence: model.ConfidenceMedium,
		})
	}

	rendered := RenderMarkdown(model.Receipt{
		SessionID: "ar_ses_readable",
		Mode:      "sidecar",
		Agent: model.Agent{
			Provider:           "codex",
			ProviderConfidence: model.ConfidenceMedium,
		},
		Risk: model.Risk{
			Level:   model.RiskHigh,
			Reasons: reasons,
		},
		Verification: model.Verification{Valid: true},
	})
	if strings.Contains(rendered, "\n{\"payload\"") || strings.Contains(rendered, "https://example.com/upload") {
		t.Fatalf("rendered Markdown leaked full command body:\n%s", rendered)
	}
	if strings.Contains(rendered, "`risky_command`") {
		t.Fatalf("rendered Markdown kept legacy risky_command code:\n%s", rendered)
	}
	if !strings.Contains(rendered, "- high `command_risk_network_egress`: Command risk detected (network_egress)") {
		t.Fatalf("rendered Markdown missing reclassified network risk:\n%s", rendered)
	}
	if !strings.Contains(rendered, "3 more risk reason(s) omitted from Markdown") {
		t.Fatalf("rendered Markdown missing omission notice:\n%s", rendered)
	}
}

func TestRenderMarkdownNormalizesLegacyProviderFalsePositive(t *testing.T) {
	t.Parallel()

	rendered := RenderMarkdown(model.Receipt{
		SessionID: "ar_ses_provider_legacy",
		Mode:      "sidecar",
		Agent: model.Agent{
			Provider:           "codex",
			ProviderConfidence: model.ConfidenceMedium,
		},
		Risk: model.Risk{
			Level: model.RiskHigh,
			Reasons: []model.RiskReason{{
				Code:       "provider_risk_network_egress",
				Message:    `Provider risk detected (network_egress): command can send data to an external host in command: rg -n "TODO|curl|wget" cmd internal`,
				Level:      model.RiskHigh,
				Confidence: model.ConfidenceMedium,
			}},
		},
		Verification: model.Verification{Valid: true},
	})
	if strings.Contains(rendered, "`provider_risk_network_egress`") {
		t.Fatalf("rendered Markdown kept stale provider network risk:\n%s", rendered)
	}
	if !strings.Contains(rendered, "`legacy_provider_risk`") || !strings.Contains(rendered, "no longer matches current classifier") {
		t.Fatalf("rendered Markdown missing legacy provider normalization:\n%s", rendered)
	}
}

func TestRenderMarkdownColorHighlightsReceiptSections(t *testing.T) {
	t.Parallel()

	receipt := model.Receipt{
		SessionID: "ar_ses_color",
		Mode:      "sidecar",
		Agent: model.Agent{
			Provider:           "codex",
			ProviderConfidence: model.ConfidenceMedium,
		},
		CaptureConfidence: model.CaptureConfidence{
			GitDiff:            model.ConfidenceHigh,
			FilesystemWrites:   model.ConfidenceNone,
			ProviderToolEvents: model.ConfidenceMedium,
		},
		Risk: model.Risk{
			Level: model.RiskHigh,
			Reasons: []model.RiskReason{{
				Code:       "command_risk_network_egress",
				Message:    "Command risk detected (network_egress): command can send data to an external host in command: curl https://example.com",
				Level:      model.RiskHigh,
				Confidence: model.ConfidenceMedium,
			}},
		},
		Verification: model.Verification{Valid: true},
	}
	plain := RenderMarkdown(receipt)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain receipt Markdown contains ANSI escapes:\n%s", plain)
	}
	colored := RenderMarkdownColor(receipt, true)
	for _, want := range []string{
		"\x1b[1;37m# AgentReceipt Receipt\x1b[0m",
		"- Risk: \x1b[31mhigh\x1b[0m",
		"- Event chain: \x1b[32mvalid\x1b[0m",
		"\x1b[1;36m## Capture Confidence\x1b[0m",
		"- \x1b[31mhigh\x1b[0m `\x1b[36mcommand_risk_network_egress\x1b[0m`",
	} {
		if !strings.Contains(colored, want) {
			t.Fatalf("colored receipt Markdown missing %q\n%s", want, colored)
		}
	}
}

func TestVerifyDetectsFinalPatchTampering(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := Finalize(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if err := os.WriteFile(layout.FinalPatch, []byte("tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper final patch: %v", err)
	}
	result, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Valid || result.FinalDiffHash {
		t.Fatalf("tampered patch verified unexpectedly: %+v", result)
	}
	if !strings.Contains(RenderVerify(result), "Receipt invalid.") {
		t.Fatalf("RenderVerify() = %q", RenderVerify(result))
	}
}

func TestVerifyBundleValidatesFinalizedArtifacts(t *testing.T) {
	bundleRoot := finalizedReceiptBundle(t)

	result, err := VerifyBundle(bundleRoot)
	if err != nil {
		t.Fatalf("VerifyBundle() error = %v", err)
	}
	if !result.Valid || !result.EventChain || !result.Signature || !result.FinalDiffHash || !result.ManifestHash || !result.ReceiptHash {
		t.Fatalf("bundle verification result: %+v", result)
	}
	if !strings.HasPrefix(result.SignedBy, "embedded:") {
		t.Fatalf("bundle verification did not use embedded signer: %+v", result)
	}
}

func TestVerifyBundleDetectsTampering(t *testing.T) {
	sourceBundle := finalizedReceiptBundle(t)
	tests := []struct {
		name   string
		tamper func(t *testing.T, bundle string)
		check  func(VerifyResult) bool
	}{
		{
			name: "events",
			tamper: func(t *testing.T, bundle string) {
				appendReceiptFile(t, filepath.Join(bundle, storage.EventsFile), "{}\n")
			},
			check: func(result VerifyResult) bool { return !result.EventChain },
		},
		{
			name: "manifest",
			tamper: func(t *testing.T, bundle string) {
				if err := os.WriteFile(filepath.Join(bundle, storage.ManifestFile), []byte("{}\n"), 0o600); err != nil {
					t.Fatalf("tamper manifest: %v", err)
				}
			},
			check: func(result VerifyResult) bool { return !result.ManifestHash },
		},
		{
			name: "final patch",
			tamper: func(t *testing.T, bundle string) {
				if err := os.WriteFile(filepath.Join(bundle, storage.DiffsDir, storage.FinalPatchFile), []byte("tampered\n"), 0o600); err != nil {
					t.Fatalf("tamper final patch: %v", err)
				}
			},
			check: func(result VerifyResult) bool { return !result.FinalDiffHash },
		},
		{
			name: "embedded signer",
			tamper: func(t *testing.T, bundle string) {
				receipt, err := readReceiptPath(filepath.Join(bundle, storage.ReceiptJSONFile))
				if err != nil {
					t.Fatalf("read receipt: %v", err)
				}
				receipt.Verification.SignerPublicKey = ""
				if err := writeJSON(filepath.Join(bundle, storage.ReceiptJSONFile), receipt); err != nil {
					t.Fatalf("write receipt: %v", err)
				}
			},
			check: func(result VerifyResult) bool {
				return !result.Signature && hasVerifyWarning(result.Warnings, "embedded signer public key")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle := copyReceiptBundle(t, sourceBundle)
			test.tamper(t, bundle)
			result, err := VerifyBundle(bundle)
			if err != nil {
				t.Fatalf("VerifyBundle() error = %v", err)
			}
			if result.Valid || !test.check(result) {
				t.Fatalf("tampered bundle verification result: %+v", result)
			}
		})
	}
}

func TestFinalizeReceiptRecordsProviderLabel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []model.Event
		want   string
	}{
		{
			name: "claude",
			events: []model.Event{{
				EventID:   "evt_claude_receipt_provider",
				Timestamp: fixedReceiptNow(),
				Source:    "claude_hook",
				Type:      "provider.command",
				Provider:  "claude",
				Payload:   map[string]any{"command": "go test ./..."},
			}},
			want: "Claude Code",
		},
		{
			name: "mixed",
			events: []model.Event{
				{
					EventID:   "evt_codex_receipt_provider",
					Timestamp: fixedReceiptNow(),
					Source:    "codex_session_log",
					Type:      "provider.command",
					Provider:  "codex",
					Payload:   map[string]any{"command": "go test ./..."},
				},
				{
					EventID:   "evt_claude_receipt_provider",
					Timestamp: fixedReceiptNow().Add(time.Second),
					Source:    "claude_hook",
					Type:      "provider.command",
					Provider:  "claude",
					Payload:   map[string]any{"command": "go test ./..."},
				},
			},
			want: "Codex CLI + Claude Code",
		},
		{name: "missing", want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newReceiptGitRepo(t)
			keyDir := t.TempDir()
			manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
			state, err := manager.Start(context.Background())
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if len(tc.events) > 0 {
				if _, _, err := manager.AppendProviderEvents(context.Background(), tc.events, nil); err != nil {
					t.Fatalf("AppendProviderEvents() error = %v", err)
				}
			}
			if _, _, err := manager.Stop(context.Background()); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}

			receipt, err := Finalize(context.Background(), Options{
				RepoPath:    repo,
				SessionID:   state.SessionID,
				KeyDir:      keyDir,
				GeneratedAt: fixedReceiptNow(),
			})
			if err != nil {
				t.Fatalf("Finalize() error = %v", err)
			}
			if receipt.Agent.Provider != tc.want {
				t.Fatalf("Agent.Provider = %q, want %q", receipt.Agent.Provider, tc.want)
			}
		})
	}
}

func TestFinalizeConfidenceDowngradesMissingProviderOnly(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	receipt, err := Finalize(context.Background(), Options{
		RepoPath:    repo,
		SessionID:   state.SessionID,
		KeyDir:      keyDir,
		GeneratedAt: fixedReceiptNow(),
	})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if receipt.Agent.ProviderConfidence != model.ConfidenceNone || receipt.CaptureConfidence.ProviderToolEvents != model.ConfidenceNone {
		t.Fatalf("provider confidence should be downgraded with no provider events: %+v", receipt.CaptureConfidence)
	}
	if receipt.CaptureConfidence.GitDiff != model.ConfidenceHigh {
		t.Fatalf("git confidence = %q, want high", receipt.CaptureConfidence.GitDiff)
	}
	if receipt.CaptureConfidence.FilesystemWrites != model.ConfidenceNone {
		t.Fatalf("filesystem confidence = %q, want none without fs events", receipt.CaptureConfidence.FilesystemWrites)
	}
	if !hasReceiptWarning(receipt.Warnings, "codex_events_missing") {
		t.Fatalf("missing provider warning not present: %+v", receipt.Warnings)
	}
}

func TestFinalizeFilesystemConfidenceRequiresFilesystemEvent(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	appendReceiptFile(t, filepath.Join(repo, "README.md"), "receipt change\n")
	waitForReceiptEvent(t, layout.EventsJSONL, func(event model.Event) bool {
		return event.Source == "fs_watcher" && event.Type == "fs.change"
	})
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	receipt, err := Finalize(context.Background(), Options{
		RepoPath:    repo,
		SessionID:   state.SessionID,
		KeyDir:      keyDir,
		GeneratedAt: fixedReceiptNow(),
	})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if receipt.CaptureConfidence.FilesystemWrites != model.ConfidenceHigh {
		t.Fatalf("filesystem confidence = %q, want high with fs event", receipt.CaptureConfidence.FilesystemWrites)
	}
	if receipt.CaptureConfidence.ProviderToolEvents != model.ConfidenceNone {
		t.Fatalf("provider confidence = %q, want none without provider events", receipt.CaptureConfidence.ProviderToolEvents)
	}
}

func TestVerifyDetectsReceiptTampering(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := Finalize(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	data, err := os.ReadFile(layout.ReceiptJSON)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	tampered := strings.Replace(string(data), `"level": "low"`, `"level": "high"`, 1)
	if tampered == string(data) {
		t.Fatalf("receipt fixture did not contain expected risk value:\n%s", data)
	}
	if err := os.WriteFile(layout.ReceiptJSON, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write tampered receipt: %v", err)
	}
	result, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Valid || result.ReceiptHash || !result.Signature {
		t.Fatalf("tampered receipt verification result: %+v", result)
	}
}

func TestVerifyRejectsUnknownReceiptFields(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := Finalize(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	addUnknownReceiptField(t, layout.ReceiptJSON)

	result, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Valid || result.ReceiptHash {
		t.Fatalf("receipt with unknown field verified unexpectedly: %+v", result)
	}
	if !hasVerifyWarning(result.Warnings, "unknown top-level fields: unauthenticated_note") {
		t.Fatalf("unknown-field warning missing: %+v", result.Warnings)
	}
}

func TestVerifyBundleRejectsUnknownReceiptFields(t *testing.T) {
	bundle := finalizedReceiptBundle(t)
	addUnknownReceiptField(t, filepath.Join(bundle, storage.ReceiptJSONFile))

	result, err := VerifyBundle(bundle)
	if err != nil {
		t.Fatalf("VerifyBundle() error = %v", err)
	}
	if result.Valid || result.ReceiptHash {
		t.Fatalf("bundle receipt with unknown field verified unexpectedly: %+v", result)
	}
	if !hasVerifyWarning(result.Warnings, "unknown top-level fields: unauthenticated_note") {
		t.Fatalf("unknown-field warning missing: %+v", result.Warnings)
	}
}

func TestVerifyDetectsSignerMetadataTampering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tamper func(t *testing.T, receipt *model.Receipt)
	}{
		{
			name: "key id",
			tamper: func(_ *testing.T, receipt *model.Receipt) {
				receipt.Verification.SignerKeyID = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			},
		},
		{
			name: "public key",
			tamper: func(t *testing.T, receipt *model.Receipt) {
				otherKeypair, err := signing.LoadOrCreateDefault(t.TempDir())
				if err != nil {
					t.Fatalf("other keypair: %v", err)
				}
				receipt.Verification.SignerPublicKey = signing.EncodePublicKey(otherKeypair.PublicKey)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newReceiptGitRepo(t)
			keyDir := t.TempDir()
			manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
			state, err := manager.Start(context.Background())
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if _, _, err := manager.Stop(context.Background()); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
			receipt, err := Finalize(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
			if err != nil {
				t.Fatalf("Finalize() error = %v", err)
			}
			layout, err := storage.NewLayout(repo, state.SessionID)
			if err != nil {
				t.Fatalf("NewLayout() error = %v", err)
			}
			tc.tamper(t, &receipt)
			if err := writeJSON(layout.ReceiptJSON, receipt); err != nil {
				t.Fatalf("write tampered receipt: %v", err)
			}

			result, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if result.Valid || result.Signature {
				t.Fatalf("tampered signer metadata verified unexpectedly: %+v", result)
			}
		})
	}
}

func TestVerifyLegacyReceiptFallsBackToLocalPublicKey(t *testing.T) {
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	receipt, err := Finalize(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	keypair, err := signing.LoadOrCreateDefault(keyDir)
	if err != nil {
		t.Fatalf("LoadOrCreateDefault() error = %v", err)
	}
	receipt.Verification.SignerPublicKey = ""
	receipt.Verification.SignerKeyID = ""
	receipt.Verification.Signature = ""
	receiptHash, err := unsignedReceiptHash(receipt)
	if err != nil {
		t.Fatalf("unsignedReceiptHash() error = %v", err)
	}
	receipt.Verification.ReceiptHash = receiptHash
	payload, err := signaturePayload(receipt.Verification)
	if err != nil {
		t.Fatalf("signaturePayload() error = %v", err)
	}
	receipt.Verification.Signature = signing.Sign(keypair.PrivateKey, payload)
	if err := writeJSON(layout.ReceiptJSON, receipt); err != nil {
		t.Fatalf("write legacy receipt: %v", err)
	}
	if err := writeFile(layout.ReceiptSignature, []byte(receipt.Verification.Signature+"\n")); err != nil {
		t.Fatalf("write legacy signature: %v", err)
	}

	result, err := Verify(context.Background(), Options{RepoPath: repo, SessionID: state.SessionID, KeyDir: keyDir})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Valid || strings.HasPrefix(result.SignedBy, "embedded:") {
		t.Fatalf("legacy verification result: %+v", result)
	}
}

func TestVerifyErrorsWhenNoSessionExists(t *testing.T) {
	repo := newReceiptGitRepo(t)
	if _, err := Verify(context.Background(), Options{RepoPath: repo, KeyDir: t.TempDir()}); err == nil {
		t.Fatal("Verify() returned nil error without sessions")
	}
}

func finalizedReceiptBundle(t *testing.T) string {
	t.Helper()
	repo := newReceiptGitRepo(t)
	keyDir := t.TempDir()
	manager := session.Manager{RepoPath: repo, Config: config.Default(), Now: fixedReceiptNow}
	state, err := manager.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	providerEvent := model.Event{
		EventID:   "evt_codex_bundle",
		Timestamp: fixedReceiptNow(),
		Source:    "codex_session_log",
		Type:      "provider.command",
		Provider:  "codex",
		Payload:   map[string]any{"command": "go test ./..."},
	}
	if _, _, err := manager.AppendProviderEvents(context.Background(), []model.Event{providerEvent}, nil); err != nil {
		t.Fatalf("AppendProviderEvents() error = %v", err)
	}
	if _, _, err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := Finalize(context.Background(), Options{
		RepoPath:    repo,
		SessionID:   state.SessionID,
		KeyDir:      keyDir,
		GeneratedAt: fixedReceiptNow(),
	}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	layout, err := storage.NewLayout(repo, state.SessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	return layout.Session
}

func newReceiptGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runReceiptGit(t, repo, "init")
	runReceiptGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runReceiptGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runReceiptGit(t, repo, "add", "README.md")
	runReceiptGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runReceiptGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func fixedReceiptNow() time.Time {
	return time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
}

func appendReceiptFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func copyReceiptBundle(t *testing.T, source string) string {
	t.Helper()
	dest := t.TempDir()
	for _, relative := range []string{
		storage.ReceiptJSONFile,
		storage.ManifestFile,
		storage.EventsFile,
		filepath.Join(storage.DiffsDir, storage.FinalPatchFile),
	} {
		sourcePath := filepath.Join(source, relative)
		destPath := filepath.Join(dest, relative)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
			t.Fatalf("mkdir bundle path: %v", err)
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read bundle file %s: %v", sourcePath, err)
		}
		if err := os.WriteFile(destPath, data, 0o600); err != nil {
			t.Fatalf("write bundle file %s: %v", destPath, err)
		}
	}

	return dest
}

func addUnknownReceiptField(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	tampered := strings.TrimSuffix(trimmed, "}")
	if tampered == trimmed {
		t.Fatalf("receipt JSON did not end with object close:\n%s", data)
	}
	tampered += `,` + "\n" + `  "unauthenticated_note": "tampered"` + "\n}"
	if err := os.WriteFile(path, []byte(tampered+"\n"), 0o600); err != nil {
		t.Fatalf("write receipt with unknown field: %v", err)
	}
}

func hasVerifyWarning(warnings []string, text string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, text) {
			return true
		}
	}

	return false
}

func waitForReceiptEvent(t *testing.T, eventsPath string, match func(model.Event) bool) model.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, err := eventlog.ReadFile(eventsPath)
		if err == nil {
			for _, event := range events {
				if match(event) {
					return event
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	events, _ := eventlog.ReadFile(eventsPath)
	t.Fatalf("timed out waiting for matching event in %+v", events)
	return model.Event{}
}

func hasReceiptWarning(warnings []model.Warning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}

	return false
}

func hasReceiptRiskCode(reasons []model.RiskReason, code string) bool {
	for _, reason := range reasons {
		if reason.Code == code {
			return true
		}
	}

	return false
}

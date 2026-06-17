package receipt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
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
	}
	data, err := Export(context.Background(), Options{RepoPath: repo}, "")
	if err != nil {
		t.Fatalf("default Export() error = %v", err)
	}
	if !strings.Contains(string(data), "# AgentReceipt Receipt") {
		t.Fatalf("default Export() output:\n%s", data)
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

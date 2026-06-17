package receipt

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/review"
	"github.com/ametel01/agentreceipt/internal/signing"
	"github.com/ametel01/agentreceipt/internal/storage"
)

type Options struct {
	RepoPath    string
	SessionID   string
	Last        bool
	KeyDir      string
	GeneratedAt time.Time
}

type VerifyResult struct {
	SessionID     string
	Valid         bool
	EventChain    bool
	Signature     bool
	FinalDiffHash bool
	ManifestHash  bool
	ReceiptHash   bool
	SignedBy      string
	Warnings      []string
}

func Finalize(ctx context.Context, options Options) (model.Receipt, error) {
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return model.Receipt{}, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return model.Receipt{}, err
	}
	report, err := review.Build(ctx, review.Options{RepoPath: repoRoot, SessionID: sessionID})
	if err != nil {
		return model.Receipt{}, err
	}
	manifestHash, err := fileHash(layout.ManifestJSON)
	if err != nil {
		return model.Receipt{}, err
	}
	receipt := model.Receipt{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		CreatedAt:     generatedAt(options).UTC(),
		Mode:          "sidecar",
		Agent: model.Agent{
			Provider:           "codex",
			ProviderConfidence: report.Confidence.ProviderToolEvents,
		},
		Repo:              model.Repo{Root: repoRoot, DirtyEnd: len(report.Summary.ChangedFiles) > 0},
		Summary:           report.Summary,
		CaptureConfidence: report.Confidence,
		Risk:              report.Risk,
		Verification:      report.Verification,
		Warnings:          report.Warnings,
	}
	receipt.Verification.ManifestHash = manifestHash
	receipt.Verification.SignatureAlgorithm = "ed25519"
	receipt.Verification.Valid = report.Verification.Valid
	keypair, err := signing.LoadOrCreateDefault(options.KeyDir)
	if err != nil {
		return model.Receipt{}, err
	}
	receipt.Verification.SignerPublicKey = signing.EncodePublicKey(keypair.PublicKey)
	receipt.Verification.SignerKeyID = signing.KeyID(keypair.PublicKey)
	receiptHash, err := unsignedReceiptHash(receipt)
	if err != nil {
		return model.Receipt{}, err
	}
	receipt.Verification.ReceiptHash = receiptHash
	signPayload, err := signaturePayload(receipt.Verification)
	if err != nil {
		return model.Receipt{}, err
	}
	receipt.Verification.Signature = signing.Sign(keypair.PrivateKey, signPayload)
	if err := writeJSON(layout.ReceiptJSON, receipt); err != nil {
		return model.Receipt{}, err
	}
	if err := writeFile(layout.ReceiptMarkdown, []byte(RenderMarkdown(receipt))); err != nil {
		return model.Receipt{}, err
	}
	report.Verification = receipt.Verification
	if err := writeFile(layout.ReviewMarkdown, []byte(review.RenderMarkdown(report))); err != nil {
		return model.Receipt{}, err
	}
	if err := writeFile(layout.ReceiptSignature, []byte(receipt.Verification.Signature+"\n")); err != nil {
		return model.Receipt{}, err
	}

	return receipt, nil
}

func Verify(ctx context.Context, options Options) (VerifyResult, error) {
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return VerifyResult{}, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return VerifyResult{}, err
	}
	receipt, err := Read(layout)
	if err != nil {
		return VerifyResult{}, err
	}
	result := VerifyResult{SessionID: sessionID, Valid: true}
	chainHash, err := replayHash(layout.EventsJSONL)
	result.EventChain = err == nil && chainHash == receipt.Verification.EventChainHash
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	manifestHash, err := fileHash(layout.ManifestJSON)
	result.ManifestHash = err == nil && manifestHash == receipt.Verification.ManifestHash
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	finalPatchHash, err := fileHash(layout.FinalPatch)
	result.FinalDiffHash = err == nil && finalPatchHash == receipt.Verification.DiffHash
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	currentDiffHash, err := currentDiffHash(ctx, repoRoot, sessionID, layout)
	if err == nil && currentDiffHash != receipt.Verification.DiffHash {
		result.FinalDiffHash = false
		result.Warnings = append(result.Warnings, "current workspace diff does not match recorded final diff hash")
	} else if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	receiptHash, err := unsignedReceiptHash(receipt)
	result.ReceiptHash = err == nil && receiptHash == receipt.Verification.ReceiptHash
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	signatureData, err := readFile(layout.ReceiptSignature)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("read receipt signature: %v", err))
	}
	publicKey, signedBy, err := publicKeyForVerification(receipt.Verification, options.KeyDir)
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	} else {
		payload, payloadErr := signaturePayload(receipt.Verification)
		if payloadErr != nil {
			result.Warnings = append(result.Warnings, payloadErr.Error())
		} else {
			signature := receipt.Verification.Signature
			if len(bytes.TrimSpace(signatureData)) > 0 {
				signature = string(bytes.TrimSpace(signatureData))
			}
			result.Signature = signing.Verify(publicKey, payload, signature)
			result.SignedBy = signedBy
		}
	}
	result.Valid = result.EventChain && result.Signature && result.FinalDiffHash && result.ManifestHash && result.ReceiptHash

	return result, nil
}

func Export(ctx context.Context, options Options, format string) ([]byte, error) {
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return nil, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return nil, err
	}
	switch format {
	case "json":
		return readFile(layout.ReceiptJSON)
	case "pr":
		return readFile(layout.ReviewMarkdown)
	default:
		return readFile(layout.ReceiptMarkdown)
	}
}

func Read(layout storage.Layout) (model.Receipt, error) {
	data, err := readFile(layout.ReceiptJSON)
	if err != nil {
		return model.Receipt{}, fmt.Errorf("read receipt json: %w", err)
	}
	receipt, _, err := model.DecodeReceipt(data)
	if err != nil {
		return model.Receipt{}, err
	}

	return receipt, nil
}

func RenderMarkdown(receipt model.Receipt) string {
	var builder bytes.Buffer
	fmt.Fprintf(&builder, "# AgentReceipt Receipt\n\n")
	fmt.Fprintf(&builder, "- Session: `%s`\n", receipt.SessionID)
	fmt.Fprintf(&builder, "- Mode: %s\n", receipt.Mode)
	fmt.Fprintf(&builder, "- Provider: %s (%s confidence)\n", receipt.Agent.Provider, receipt.Agent.ProviderConfidence)
	fmt.Fprintf(&builder, "- Risk: %s\n", receipt.Risk.Level)
	fmt.Fprintf(&builder, "- Event chain: %s\n", validText(receipt.Verification.Valid))
	fmt.Fprintf(&builder, "- Final diff hash: `%s`\n", receipt.Verification.DiffHash)
	fmt.Fprintf(&builder, "- Manifest hash: `%s`\n", receipt.Verification.ManifestHash)
	fmt.Fprintf(&builder, "- Receipt hash: `%s`\n", receipt.Verification.ReceiptHash)
	fmt.Fprintf(&builder, "- Signature: %s\n\n", receipt.Verification.SignatureAlgorithm)
	builder.WriteString("## Capture Confidence\n\n")
	fmt.Fprintf(&builder, "- Git diff: %s\n", receipt.CaptureConfidence.GitDiff)
	fmt.Fprintf(&builder, "- Filesystem writes: %s\n", receipt.CaptureConfidence.FilesystemWrites)
	fmt.Fprintf(&builder, "- Provider tool events: %s\n\n", receipt.CaptureConfidence.ProviderToolEvents)
	builder.WriteString("## Risk Reasons\n\n")
	if len(receipt.Risk.Reasons) == 0 {
		builder.WriteString("- none\n")
	}
	for _, reason := range receipt.Risk.Reasons {
		fmt.Fprintf(&builder, "- %s: %s\n", reason.Level, reason.Message)
	}

	return builder.String()
}

func RenderVerify(result VerifyResult) string {
	var builder bytes.Buffer
	if result.Valid {
		builder.WriteString("Receipt valid.\n\n")
	} else {
		builder.WriteString("Receipt invalid.\n\n")
	}
	fmt.Fprintf(&builder, "Event chain: %s\n", validText(result.EventChain))
	fmt.Fprintf(&builder, "Signature: %s\n", validText(result.Signature))
	fmt.Fprintf(&builder, "Final diff hash: %s\n", validText(result.FinalDiffHash))
	fmt.Fprintf(&builder, "Manifest hash: %s\n", validText(result.ManifestHash))
	fmt.Fprintf(&builder, "Receipt hash: %s\n", validText(result.ReceiptHash))
	if result.SignedBy != "" {
		fmt.Fprintf(&builder, "Signed by: %s\n", result.SignedBy)
	}
	if len(result.Warnings) > 0 {
		builder.WriteString("\nWarnings:\n")
		for _, warning := range result.Warnings {
			fmt.Fprintf(&builder, "- %s\n", warning)
		}
	}

	return builder.String()
}

func publicKeyForVerification(verification model.Verification, keyDir string) (ed25519.PublicKey, string, error) {
	if verification.SignerPublicKey != "" {
		publicKey, err := signing.DecodePublicKey(verification.SignerPublicKey)
		if err != nil {
			return nil, "", fmt.Errorf("decode embedded signer public key: %w", err)
		}
		keyID := signing.KeyID(publicKey)
		if verification.SignerKeyID != "" && verification.SignerKeyID != keyID {
			return nil, "", fmt.Errorf("embedded signer key id mismatch: got %s, want %s", verification.SignerKeyID, keyID)
		}
		if verification.SignerKeyID != "" {
			keyID = verification.SignerKeyID
		}

		return publicKey, "embedded:" + keyID, nil
	}

	return signing.LoadDefaultPublic(keyDir)
}

func signaturePayload(verification model.Verification) ([]byte, error) {
	payload := struct {
		EventChainHash string `json:"event_chain_hash"`
		DiffHash       string `json:"diff_hash"`
		ManifestHash   string `json:"manifest_hash"`
		ReceiptHash    string `json:"receipt_hash"`
		SignerKeyID    string `json:"signer_key_id,omitempty"`
	}{
		EventChainHash: verification.EventChainHash,
		DiffHash:       verification.DiffHash,
		ManifestHash:   verification.ManifestHash,
		ReceiptHash:    verification.ReceiptHash,
		SignerKeyID:    verification.SignerKeyID,
	}

	return model.MarshalCanonical(payload)
}

func unsignedReceiptHash(receipt model.Receipt) (string, error) {
	receipt.Verification.ReceiptHash = ""
	receipt.Verification.Signature = ""
	data, err := model.MarshalCanonical(receipt)
	if err != nil {
		return "", err
	}

	return hashBytes(data), nil
}

func replayHash(eventsPath string) (string, error) {
	events, err := eventlog.ReadFile(eventsPath)
	if err != nil {
		return "", err
	}

	return eventlog.Replay(events)
}

func fileHash(path string) (string, error) {
	data, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", filepath.Base(path), err)
	}

	return hashBytes(data), nil
}

func readFile(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = root.Close()
	}()

	return root.ReadFile(filepath.Base(path))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func currentDiffHash(ctx context.Context, repoRoot string, sessionID string, layout storage.Layout) (string, error) {
	monitor, err := gitmonitor.New(ctx, repoRoot, sessionID, layout)
	if err != nil {
		return "", err
	}

	return monitor.CurrentDiffHash(ctx)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()

	return root.WriteFile(filepath.Base(path), data, 0o600)
}

func resolveSession(ctx context.Context, options Options) (string, string, error) {
	repoRoot, err := gitmonitor.DiscoverRoot(ctx, repoPathOrCWD(options.RepoPath))
	if err != nil {
		return "", "", err
	}
	if options.SessionID != "" {
		return repoRoot, options.SessionID, nil
	}
	sessionID, err := latestSession(repoRoot)
	if err != nil {
		return "", "", err
	}

	return repoRoot, sessionID, nil
}

func latestSession(repoRoot string) (string, error) {
	sessionsPath, err := storage.SessionsPath(repoRoot)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(sessionsPath)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = root.Close()
	}()
	var latest string
	var latestTime time.Time
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil || !entry.IsDir() || path == "." {
			return nil
		}
		if storage.ValidateSessionID(entry.Name()) != nil {
			return fs.SkipDir
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return fs.SkipDir
		}
		if latest == "" || info.ModTime().After(latestTime) {
			latest = entry.Name()
			latestTime = info.ModTime()
		}

		return fs.SkipDir
	})
	if err != nil {
		return "", err
	}
	if latest == "" {
		return "", errors.New("no AgentReceipt sessions found")
	}

	return latest, nil
}

func repoPathOrCWD(path string) string {
	if path != "" {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	return cwd
}

func generatedAt(options Options) time.Time {
	if !options.GeneratedAt.IsZero() {
		return options.GeneratedAt
	}

	return time.Now().UTC()
}

func validText(valid bool) string {
	if valid {
		return "valid"
	}

	return "invalid"
}

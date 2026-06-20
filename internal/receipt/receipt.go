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
	"sort"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/commandrisk"
	"github.com/ametel01/agentreceipt/internal/config"
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
	Config      config.Config
}

type VerifyResult struct {
	SessionID          string
	Valid              bool
	EventChain         bool
	Signature          bool
	FinalDiffHash      bool
	ManifestHash       bool
	ReceiptHash        bool
	EventChainError    string
	FinalDiffHashError string
	ManifestHashError  string
	ReceiptHashError   string
	SignatureError     string
	SignatureErrorCode string
	SignedBy           string
	Warnings           []string
}

const (
	signatureErrorCodeLegacyMissingSigner = "legacy_missing_embedded_signer"
	signatureErrorCodeKeyIDMismatch       = "signature_signer_key_id_mismatch"
	signatureErrorCodePayload             = "signature_payload_error"
	signatureErrorCodeVerifier            = "signature_verification_error"
	signatureErrorCodePublicKeyResolution = "signature_public_key_error"
)

type decodedReceipt struct {
	Receipt       model.Receipt
	UnknownFields []string
}

const (
	maxMarkdownRiskReasons      = 12
	maxMarkdownRiskMessageRunes = 180
	maxMarkdownCommandRunes     = 100
)

func Finalize(ctx context.Context, options Options) (model.Receipt, error) {
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return model.Receipt{}, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return model.Receipt{}, err
	}
	report, err := review.Build(ctx, review.Options{RepoPath: repoRoot, SessionID: sessionID, Config: options.Config})
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
			Provider:           report.Provider,
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
	result, receiptVerification, err := verifyArtifacts(layout, options.KeyDir, publicKeyForVerification)
	if err != nil {
		return VerifyResult{}, err
	}
	result.SessionID = sessionID
	currentDiffHash, err := currentDiffHash(ctx, repoRoot, sessionID, layout)
	if err == nil && currentDiffHash != receiptVerification.DiffHash {
		result.FinalDiffHash = false
		result.Warnings = append(result.Warnings, "current workspace diff does not match recorded final diff hash")
	} else if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	result.Valid = result.EventChain && result.Signature && result.FinalDiffHash && result.ManifestHash && result.ReceiptHash

	return result, nil
}

func VerifyBundle(root string) (VerifyResult, error) {
	if root == "" {
		return VerifyResult{}, errors.New("bundle path is required")
	}
	receiptPath := filepath.Join(root, storage.ReceiptJSONFile)
	manifestPath := filepath.Join(root, storage.ManifestFile)
	eventsPath := filepath.Join(root, storage.EventsFile)
	finalPatchPath := filepath.Join(root, storage.DiffsDir, storage.FinalPatchFile)
	layout := storage.Layout{
		ReceiptJSON:  receiptPath,
		ManifestJSON: manifestPath,
		EventsJSONL:  eventsPath,
		FinalPatch:   finalPatchPath,
	}
	result, _, err := verifyArtifacts(layout, "", func(verification model.Verification, _ string) (ed25519.PublicKey, string, error) {
		return embeddedPublicKeyForVerification(verification)
	})
	if err != nil {
		return VerifyResult{}, err
	}

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
		receipt, err := readReceiptPath(layout.ReceiptJSON)
		if err != nil {
			return nil, err
		}

		return []byte(RenderMarkdown(receipt)), nil
	}
}

func ExportWithColor(ctx context.Context, options Options, format string, color bool) ([]byte, error) {
	if format == "json" || format == "pr" {
		return Export(ctx, options, format)
	}
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return nil, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return nil, err
	}
	receipt, err := readReceiptPath(layout.ReceiptJSON)
	if err != nil {
		return nil, err
	}

	return []byte(RenderMarkdownColor(receipt, color)), nil
}

func Read(layout storage.Layout) (model.Receipt, error) {
	receipt, err := readReceiptPath(layout.ReceiptJSON)
	if err != nil {
		return model.Receipt{}, err
	}

	return receipt, nil
}

func readReceiptPath(path string) (model.Receipt, error) {
	decoded, err := readDecodedReceiptPath(path)
	if err != nil {
		return model.Receipt{}, err
	}

	return decoded.Receipt, nil
}

func readDecodedReceiptPath(path string) (decodedReceipt, error) {
	data, err := readFile(path)
	if err != nil {
		return decodedReceipt{}, fmt.Errorf("read receipt json: %w", err)
	}
	receipt, unknownFields, err := model.DecodeReceipt(data)
	if err != nil {
		return decodedReceipt{}, err
	}

	return decodedReceipt{Receipt: receipt, UnknownFields: unknownFields}, nil
}

func rejectUnknownReceiptFields(result *VerifyResult, fields []string) {
	if len(fields) == 0 {
		return
	}
	fields = append([]string(nil), fields...)
	sort.Strings(fields)
	result.Warnings = append(result.Warnings, fmt.Sprintf("receipt contains unknown top-level fields: %s", strings.Join(fields, ", ")))
	result.ReceiptHash = false
}

func RenderMarkdown(receipt model.Receipt) string {
	return RenderMarkdownColor(receipt, false)
}

func RenderMarkdownColor(receipt model.Receipt, color bool) string {
	var builder bytes.Buffer
	fmt.Fprintf(&builder, "%s\n\n", receiptColorize("# AgentReceipt Receipt", receiptColorBoldWhite, color))
	fmt.Fprintf(&builder, "- Session: `%s`\n", receipt.SessionID)
	fmt.Fprintf(&builder, "- Mode: %s\n", receipt.Mode)
	fmt.Fprintf(&builder, "- Provider: %s (%s confidence)\n", receipt.Agent.Provider, receipt.Agent.ProviderConfidence)
	fmt.Fprintf(&builder, "- Risk: %s\n", receiptColorize(string(receipt.Risk.Level), receiptColorForRisk(receipt.Risk.Level), color))
	fmt.Fprintf(&builder, "- Event chain: %s\n", receiptColorize(validText(receipt.Verification.Valid), receiptColorForValid(receipt.Verification.Valid), color))
	fmt.Fprintf(&builder, "- Final diff hash: `%s`\n", receipt.Verification.DiffHash)
	fmt.Fprintf(&builder, "- Manifest hash: `%s`\n", receipt.Verification.ManifestHash)
	fmt.Fprintf(&builder, "- Receipt hash: `%s`\n", receipt.Verification.ReceiptHash)
	fmt.Fprintf(&builder, "- Signature: %s\n\n", receipt.Verification.SignatureAlgorithm)
	fmt.Fprintf(&builder, "%s\n\n", receiptColorize("## Capture Confidence", receiptColorBoldCyan, color))
	fmt.Fprintf(&builder, "- Git diff: %s\n", receiptColorize(string(receipt.CaptureConfidence.GitDiff), receiptColorForConfidence(receipt.CaptureConfidence.GitDiff), color))
	fmt.Fprintf(&builder, "- Filesystem writes: %s\n", receiptColorize(string(receipt.CaptureConfidence.FilesystemWrites), receiptColorForConfidence(receipt.CaptureConfidence.FilesystemWrites), color))
	fmt.Fprintf(&builder, "- Provider tool events: %s\n\n", receiptColorize(string(receipt.CaptureConfidence.ProviderToolEvents), receiptColorForConfidence(receipt.CaptureConfidence.ProviderToolEvents), color))
	fmt.Fprintf(&builder, "%s\n\n", receiptColorize("## Risk Reasons", receiptColorBoldCyan, color))
	if len(receipt.Risk.Reasons) == 0 {
		fmt.Fprintf(&builder, "- %s\n", receiptColorize("none", receiptColorGreen, color))
	}
	riskReasons := markdownRiskReasons(receipt.Risk.Reasons)
	for index, reason := range riskReasons {
		if index >= maxMarkdownRiskReasons {
			fmt.Fprintf(&builder, "- %s\n", receiptColorize(fmt.Sprintf("%d more risk reason(s) omitted from Markdown; use `agentreceipt export --json` for full details.", len(riskReasons)-index), receiptColorDim, color))
			break
		}
		code := strings.TrimSpace(reason.Code)
		if code == "" {
			code = "risk"
		}
		fmt.Fprintf(&builder, "- %s `%s`: %s\n", receiptColorize(string(reason.Level), receiptColorForRisk(reason.Level), color), receiptColorize(code, receiptColorCyan, color), markdownRiskMessage(reason.Message))
	}

	return builder.String()
}

func markdownRiskReasons(reasons []model.RiskReason) []model.RiskReason {
	normalized := make([]model.RiskReason, 0, len(reasons))
	seen := map[string]bool{}
	for _, reason := range reasons {
		for _, displayReason := range markdownRiskReason(reason) {
			key := string(displayReason.Level) + ":" + displayReason.Code + ":" + displayReason.Message
			if seen[key] {
				continue
			}
			seen[key] = true
			normalized = append(normalized, displayReason)
		}
	}

	return normalized
}

func markdownRiskReason(reason model.RiskReason) []model.RiskReason {
	if reason.Code == "risky_command" {
		return classifyLegacyCommandRisk(reason)
	}
	if strings.HasPrefix(reason.Code, "provider_risk_") {
		return normalizeProviderRiskForMarkdown(reason)
	}

	return []model.RiskReason{reason}
}

func classifyLegacyCommandRisk(reason model.RiskReason) []model.RiskReason {
	command, ok := legacyRiskCommand(reason.Message)
	if !ok {
		reason.Code = "legacy_command_risk"
		return []model.RiskReason{reason}
	}
	classifications := commandrisk.Classify(command)
	if len(classifications) == 0 {
		return []model.RiskReason{legacyUnclassifiedRisk(reason, "legacy_command_risk", "Legacy command risk no longer matches current classifier", command)}
	}
	displayReasons := make([]model.RiskReason, 0, len(classifications))
	for _, classification := range classifications {
		if classification.Level == model.RiskLow || classification.Level == model.RiskInfo || classification.Level == "" {
			continue
		}
		displayReasons = append(displayReasons, model.RiskReason{
			Code:       "command_risk_" + markdownRiskCodeFragment(classification.Signal),
			Message:    markdownCommandRiskMessage(classification, command),
			Level:      classification.Level,
			Confidence: reason.Confidence,
		})
	}
	if len(displayReasons) == 0 {
		return []model.RiskReason{legacyUnclassifiedRisk(reason, "legacy_command_risk", "Legacy command risk no longer matches current classifier", command)}
	}

	return displayReasons
}

func normalizeProviderRiskForMarkdown(reason model.RiskReason) []model.RiskReason {
	command, ok := providerRiskCommand(reason.Message)
	if !ok {
		return []model.RiskReason{reason}
	}
	signal := strings.TrimPrefix(reason.Code, "provider_risk_")
	for _, classification := range commandrisk.Classify(command) {
		if markdownRiskCodeFragment(classification.Signal) == signal {
			return []model.RiskReason{reason}
		}
	}

	return []model.RiskReason{legacyUnclassifiedRisk(reason, "legacy_provider_risk", "Legacy provider risk no longer matches current classifier", command)}
}

func legacyUnclassifiedRisk(reason model.RiskReason, code string, prefix string, command string) model.RiskReason {
	return model.RiskReason{
		Code:       code,
		Message:    prefix + ": " + markdownCommandSummary(command),
		Level:      model.RiskInfo,
		Confidence: reason.Confidence,
	}
}

func markdownCommandRiskMessage(classification commandrisk.Classification, command string) string {
	label := classification.Signal
	if label == "" {
		label = "command"
	}
	details := classification.Reason
	if details == "" {
		details = "command matched a risk rule"
	}

	return "Command risk detected (" + label + "): " + details + " in command: " + markdownCommandSummary(command)
}

func legacyRiskCommand(message string) (string, bool) {
	command := strings.TrimSpace(strings.TrimPrefix(message, "Risky command detected:"))
	if command == message || command == "" {
		return "", false
	}

	return command, true
}

func providerRiskCommand(message string) (string, bool) {
	const marker = " in command: "
	index := strings.LastIndex(message, marker)
	if index < 0 {
		return "", false
	}
	command := strings.TrimSpace(message[index+len(marker):])
	if command == "" {
		return "", false
	}

	return command, true
}

func markdownCommandSummary(command string) string {
	return truncateMarkdownRunes(strings.Join(strings.Fields(command), " "), maxMarkdownCommandRunes)
}

func markdownRiskCodeFragment(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastUnderscore := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	fragment := strings.Trim(builder.String(), "_")
	if fragment == "" {
		return "unknown"
	}

	return fragment
}

const (
	receiptColorBoldCyan  = "1;36"
	receiptColorBoldRed   = "1;31"
	receiptColorBoldWhite = "1;37"
	receiptColorCyan      = "36"
	receiptColorDim       = "2;37"
	receiptColorGreen     = "32"
	receiptColorRed       = "31"
	receiptColorYellow    = "33"
)

func receiptColorForRisk(level model.RiskLevel) string {
	switch level {
	case model.RiskInfo, model.RiskLow:
		return receiptColorGreen
	case model.RiskMedium:
		return receiptColorYellow
	case model.RiskHigh:
		return receiptColorRed
	case model.RiskCritical:
		return receiptColorBoldRed
	default:
		return receiptColorDim
	}
}

func receiptColorForConfidence(confidence model.Confidence) string {
	switch confidence {
	case model.ConfidenceHigh:
		return receiptColorGreen
	case model.ConfidenceMedium, model.ConfidenceLowMedium:
		return receiptColorYellow
	case model.ConfidenceLow:
		return receiptColorRed
	default:
		return receiptColorDim
	}
}

func receiptColorForValid(valid bool) string {
	if valid {
		return receiptColorGreen
	}

	return receiptColorRed
}

func receiptColorize(value string, code string, enabled bool) string {
	if !enabled || value == "" {
		return value
	}

	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func markdownRiskMessage(message string) string {
	return truncateMarkdownRunes(strings.Join(strings.Fields(message), " "), maxMarkdownRiskMessageRunes)
}

func truncateMarkdownRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}

	return string(runes[:maxRunes-3]) + "..."
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
		return embeddedPublicKeyForVerification(verification)
	}

	return signing.LoadDefaultPublic(keyDir)
}

func embeddedPublicKeyForVerification(verification model.Verification) (ed25519.PublicKey, string, error) {
	if verification.SignerPublicKey == "" {
		return nil, "", errors.New("bundle verification requires embedded signer public key")
	}
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

type signatureVerifier func(verification model.Verification, keyDir string) (ed25519.PublicKey, string, error)

func verifyArtifacts(layout storage.Layout, keyDir string, resolvePublicKey signatureVerifier) (VerifyResult, model.Verification, error) {
	decoded, err := readDecodedReceiptPath(layout.ReceiptJSON)
	if err != nil {
		return VerifyResult{}, model.Verification{}, err
	}
	receipt := decoded.Receipt
	result := VerifyResult{SessionID: receipt.SessionID, Valid: true}

	chainHash, err := replayHash(layout.EventsJSONL)
	if err != nil {
		result.EventChain = false
		setVerificationError(&result, "event_chain", err.Error())
	} else {
		result.EventChain = chainHash == receipt.Verification.EventChainHash
		if !result.EventChain {
			setVerificationError(&result, "event_chain", "event chain hash mismatch")
		}
	}
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}

	manifestHash, err := fileHash(layout.ManifestJSON)
	result.ManifestHash = err == nil && manifestHash == receipt.Verification.ManifestHash
	if err != nil {
		setVerificationError(&result, "manifest", err.Error())
		result.Warnings = append(result.Warnings, err.Error())
	} else if !result.ManifestHash {
		setVerificationError(&result, "manifest", "manifest hash mismatch")
	}

	finalPatchHash, err := fileHash(layout.FinalPatch)
	result.FinalDiffHash = err == nil && finalPatchHash == receipt.Verification.DiffHash
	if err != nil {
		setVerificationError(&result, "final_diff", err.Error())
		result.Warnings = append(result.Warnings, err.Error())
	} else if !result.FinalDiffHash {
		setVerificationError(&result, "final_diff", "final diff hash mismatch")
	}

	receiptHash, err := unsignedReceiptHash(receipt)
	result.ReceiptHash = err == nil && receiptHash == receipt.Verification.ReceiptHash
	if err != nil {
		setVerificationError(&result, "receipt", err.Error())
		result.Warnings = append(result.Warnings, err.Error())
	} else if !result.ReceiptHash {
		setVerificationError(&result, "receipt", "receipt hash mismatch")
	}
	rejectUnknownReceiptFields(&result, decoded.UnknownFields)

	if !result.ReceiptHash && result.ReceiptHashError == "" {
		setVerificationError(&result, "receipt", "unknown receipt hash failure")
	}
	if !result.ManifestHash && result.ManifestHashError == "" {
		setVerificationError(&result, "manifest", "unknown manifest failure")
	}
	if !result.FinalDiffHash && result.FinalDiffHashError == "" {
		setVerificationError(&result, "final_diff", "unknown final diff failure")
	}
	if !result.EventChain && result.EventChainError == "" {
		setVerificationError(&result, "event_chain", "unknown event-chain failure")
	}

	signature := receipt.Verification.Signature
	if layout.ReceiptSignature != "" {
		signatureData, err := readFile(layout.ReceiptSignature)
		if err == nil {
			if trimmed := bytes.TrimSpace(signatureData); len(trimmed) > 0 {
				signature = string(trimmed)
			}
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("read receipt signature: %v", err))
		}
	}

	publicKey, signedBy, err := resolvePublicKey(receipt.Verification, keyDir)
	if err != nil {
		result.Signature = false
		result.SignatureError = err.Error()
		result.SignatureErrorCode = signatureErrorCodeFromResolverError(err)
		result.Warnings = append(result.Warnings, err.Error())
		result.SignedBy = ""
	} else {
		payload, payloadErr := signaturePayload(receipt.Verification)
		if payloadErr != nil {
			result.Signature = false
			result.SignatureError = "could not construct signature payload"
			result.SignatureErrorCode = signatureErrorCodePayload
			result.Warnings = append(result.Warnings, payloadErr.Error())
		} else if !signing.Verify(publicKey, payload, signature) {
			result.Signature = false
			result.SignatureError = "signature verification failed"
			result.SignatureErrorCode = signatureErrorCodeVerifier
			result.Warnings = append(result.Warnings, result.SignatureError)
		} else {
			result.Signature = true
			result.SignedBy = signedBy
		}
	}
	result.Valid = result.EventChain && result.Signature && result.FinalDiffHash && result.ManifestHash && result.ReceiptHash
	return result, receipt.Verification, nil
}

func signatureErrorCodeFromResolverError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(message, "requires embedded signer public key") {
		return signatureErrorCodeLegacyMissingSigner
	}
	if strings.Contains(message, "embedded signer key id mismatch") {
		return signatureErrorCodeKeyIDMismatch
	}

	return signatureErrorCodePublicKeyResolution
}

func setVerificationError(result *VerifyResult, component string, reason string) {
	switch component {
	case "event_chain":
		result.EventChainError = firstNonEmpty(result.EventChainError, reason)
	case "final_diff":
		result.FinalDiffHashError = firstNonEmpty(result.FinalDiffHashError, reason)
	case "manifest":
		result.ManifestHashError = firstNonEmpty(result.ManifestHashError, reason)
	case "receipt":
		result.ReceiptHashError = firstNonEmpty(result.ReceiptHashError, reason)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
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

package trust

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	TrustStatusTrusted       = "trusted"
	TrustStatusNotTrusted    = "not_trusted"
	TrustStatusNotConfigured = "not_configured"
	TrustStatusUnknown       = "unknown"
)

var trustedSignerKeyIDPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Evaluation struct {
	SignerTrusted bool
	PolicyValid   bool
	TrustStatus   string
}

func NormalizeTrustedSignerKeyID(rawID string) (string, error) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return "", fmt.Errorf("trusted signer key id cannot be empty")
	}
	id = strings.ToLower(id)
	if !trustedSignerKeyIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid trusted signer key id %q", rawID)
	}

	return id, nil
}

func NormalizeTrustedSignerKeyIDs(keyIDs []string) ([]string, error) {
	normalized := make([]string, 0, len(keyIDs))
	seen := make(map[string]struct{}, len(keyIDs))

	for _, rawID := range keyIDs {
		id, err := NormalizeTrustedSignerKeyID(rawID)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}

	return normalized, nil
}

func ExtractSignerKeyID(signedBy string) string {
	signedBy = strings.TrimSpace(signedBy)
	if signedBy == "" {
		return ""
	}
	signedBy = strings.TrimPrefix(signedBy, "embedded:")
	signedBy = strings.TrimSpace(signedBy)

	if _, err := NormalizeTrustedSignerKeyID(signedBy); err != nil {
		return ""
	}

	return signedBy
}

func EvaluateSignerTrust(signedBy string, trustedSignerKeyIDs []string) Evaluation {
	normalized, err := NormalizeTrustedSignerKeyIDs(trustedSignerKeyIDs)
	if err != nil {
		return Evaluation{PolicyValid: false, TrustStatus: TrustStatusUnknown}
	}
	if len(normalized) == 0 {
		return Evaluation{PolicyValid: true, TrustStatus: TrustStatusNotConfigured}
	}

	signerKeyID := ExtractSignerKeyID(signedBy)
	if signerKeyID == "" {
		return Evaluation{PolicyValid: true, TrustStatus: TrustStatusNotTrusted}
	}

	for _, trustedID := range normalized {
		if signerKeyID == trustedID {
			return Evaluation{PolicyValid: true, SignerTrusted: true, TrustStatus: TrustStatusTrusted}
		}
	}

	return Evaluation{PolicyValid: true, TrustStatus: TrustStatusNotTrusted}
}

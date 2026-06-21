package trust

import "testing"

func TestNormalizeTrustedSignerKeyIDsAcceptsValidIDs(t *testing.T) {
	t.Parallel()

	ids, err := NormalizeTrustedSignerKeyIDs([]string{
		"sha256:ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789",
		"sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	})
	if err != nil {
		t.Fatalf("NormalizeTrustedSignerKeyIDs() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("NormalizeTrustedSignerKeyIDs() = %#v", ids)
	}
	if ids[0] != "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("NormalizeTrustedSignerKeyIDs() normalized first id = %q", ids[0])
	}
}

func TestNormalizeTrustedSignerKeyIDsRejectsMalformedIDs(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeTrustedSignerKeyIDs([]string{"not-a-key-id"}); err == nil {
		t.Fatal("NormalizeTrustedSignerKeyIDs() returned nil error for malformed key ID")
	}
}

func TestEvaluateSignerTrustMatchesTrustedSigner(t *testing.T) {
	t.Parallel()

	eval := EvaluateSignerTrust("embedded:sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", []string{
		"sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	})
	if !eval.SignerTrusted {
		t.Fatalf("EvaluateSignerTrust() should trust key, got %#v", eval)
	}
	if eval.TrustStatus != TrustStatusTrusted {
		t.Fatalf("EvaluateSignerTrust() trust_status = %q", eval.TrustStatus)
	}
	if !eval.PolicyValid {
		t.Fatalf("EvaluateSignerTrust() policy should be valid: %#v", eval)
	}
}

func TestEvaluateSignerTrustRejectsUntrustedSigner(t *testing.T) {
	t.Parallel()

	eval := EvaluateSignerTrust("embedded:sha256:1111111111111111111111111111111111111111111111111111111111111111", []string{
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if eval.SignerTrusted {
		t.Fatalf("EvaluateSignerTrust() should not trust signer, got %#v", eval)
	}
	if eval.TrustStatus != TrustStatusNotTrusted {
		t.Fatalf("EvaluateSignerTrust() trust_status = %q", eval.TrustStatus)
	}
	if !eval.PolicyValid {
		t.Fatalf("EvaluateSignerTrust() policy should be valid: %#v", eval)
	}
}

func TestEvaluateSignerTrustWithoutPolicyIndicatesNotConfigured(t *testing.T) {
	t.Parallel()

	eval := EvaluateSignerTrust("embedded:sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", nil)
	if eval.TrustStatus != TrustStatusNotConfigured {
		t.Fatalf("EvaluateSignerTrust() trust_status = %q", eval.TrustStatus)
	}
	if eval.SignerTrusted {
		t.Fatalf("EvaluateSignerTrust() should not mark signer trusted without policy: %#v", eval)
	}
	if !eval.PolicyValid {
		t.Fatalf("EvaluateSignerTrust() policy should be valid without policy entries: %#v", eval)
	}
}

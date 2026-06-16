package signing

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateDefaultSignsAndVerifies(t *testing.T) {
	keyDir := t.TempDir()
	keypair, err := LoadOrCreateDefault(keyDir)
	if err != nil {
		t.Fatalf("LoadOrCreateDefault() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(keyDir, privateKeyFile)); err != nil {
		t.Fatalf("private key was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keyDir, publicKeyFile)); err != nil {
		t.Fatalf("public key was not written: %v", err)
	}
	signature := Sign(keypair.PrivateKey, []byte("payload"))
	publicKey, _, err := LoadDefaultPublic(keyDir)
	if err != nil {
		t.Fatalf("LoadDefaultPublic() error = %v", err)
	}
	if !Verify(publicKey, []byte("payload"), signature) {
		t.Fatal("signature did not verify")
	}
	if Verify(publicKey, []byte("tampered"), signature) {
		t.Fatal("signature verified for tampered payload")
	}

	reloaded, err := LoadOrCreateDefault(keyDir)
	if err != nil {
		t.Fatalf("LoadOrCreateDefault() reload error = %v", err)
	}
	if !reloaded.PublicKey.Equal(keypair.PublicKey) {
		t.Fatal("reloaded keypair did not match original keypair")
	}
}

func TestLoadDefaultPublicFromEnv(t *testing.T) {
	keyDir := t.TempDir()
	t.Setenv(envKeyDir, keyDir)
	keypair, err := LoadOrCreateDefault("")
	if err != nil {
		t.Fatalf("LoadOrCreateDefault() error = %v", err)
	}
	publicKey, publicPath, err := LoadDefaultPublic("")
	if err != nil {
		t.Fatalf("LoadDefaultPublic() error = %v", err)
	}
	if !publicKey.Equal(keypair.PublicKey) {
		t.Fatal("env-resolved public key did not match generated key")
	}
	if filepath.Dir(publicPath) != keyDir {
		t.Fatalf("publicPath dir = %q, want %q", filepath.Dir(publicPath), keyDir)
	}
}

func TestLoadOrCreateDefaultRejectsMismatchedKeys(t *testing.T) {
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("basic exec lookup unavailable")
	}
	keyDir := t.TempDir()
	first, err := LoadOrCreateDefault(filepath.Join(keyDir, "first"))
	if err != nil {
		t.Fatalf("first keypair: %v", err)
	}
	second, err := LoadOrCreateDefault(filepath.Join(keyDir, "second"))
	if err != nil {
		t.Fatalf("second keypair: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(keyDir, "mixed"), 0o700); err != nil {
		t.Fatalf("mkdir mixed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "mixed", privateKeyFile), []byte(readFileString(t, first.Private)), 0o600); err != nil {
		t.Fatalf("write private: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "mixed", publicKeyFile), []byte(readFileString(t, second.Public)), 0o644); err != nil {
		t.Fatalf("write public: %v", err)
	}
	if _, err := LoadOrCreateDefault(filepath.Join(keyDir, "mixed")); err == nil {
		t.Fatal("LoadOrCreateDefault() accepted mismatched keys")
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

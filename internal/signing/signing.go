package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	privateKeyFile = "default.ed25519"
	publicKeyFile  = "default.pub"
	envKeyDir      = "AGENTRECEIPT_KEY_DIR"
)

type Keypair struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	Private    string
	Public     string
}

func LoadOrCreateDefault(keyDir string) (Keypair, error) {
	dir, err := defaultKeyDir(keyDir)
	if err != nil {
		return Keypair{}, err
	}
	privatePath := filepath.Join(dir, privateKeyFile)
	publicPath := filepath.Join(dir, publicKeyFile)
	privateKey, publicKey, err := readPair(dir)
	if err == nil {
		return Keypair{PrivateKey: privateKey, PublicKey: publicKey, Private: privatePath, Public: publicPath}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Keypair{}, err
	}
	publicKey, privateKey, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Keypair{}, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Keypair{}, fmt.Errorf("create key directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Keypair{}, fmt.Errorf("open key directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	if err := root.WriteFile(privateKeyFile, []byte(base64.StdEncoding.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
		return Keypair{}, fmt.Errorf("write private key: %w", err)
	}
	if err := root.WriteFile(publicKeyFile, []byte(base64.StdEncoding.EncodeToString(publicKey)+"\n"), 0o600); err != nil {
		return Keypair{}, fmt.Errorf("write public key: %w", err)
	}

	return Keypair{PrivateKey: privateKey, PublicKey: publicKey, Private: privatePath, Public: publicPath}, nil
}

func LoadDefaultPublic(keyDir string) (ed25519.PublicKey, string, error) {
	dir, err := defaultKeyDir(keyDir)
	if err != nil {
		return nil, "", err
	}
	publicPath := filepath.Join(dir, publicKeyFile)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", fmt.Errorf("open key directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := root.ReadFile(publicKeyFile)
	if err != nil {
		return nil, "", fmt.Errorf("read public key: %w", err)
	}
	publicKey, err := decodeKey(data, ed25519.PublicKeySize)
	if err != nil {
		return nil, "", fmt.Errorf("decode public key: %w", err)
	}

	return ed25519.PublicKey(publicKey), publicPath, nil
}

func Sign(privateKey ed25519.PrivateKey, payload []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
}

func Verify(publicKey ed25519.PublicKey, payload []byte, signature string) bool {
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}

	return ed25519.Verify(publicKey, payload, decoded)
}

func EncodePublicKey(publicKey ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(publicKey)
}

func DecodePublicKey(encoded string) (ed25519.PublicKey, error) {
	publicKey, err := decodeKey([]byte(encoded), ed25519.PublicKeySize)
	if err != nil {
		return nil, err
	}

	return ed25519.PublicKey(publicKey), nil
}

func KeyID(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func readPair(dir string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("open key directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	privateData, err := root.ReadFile(privateKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read private key: %w", err)
	}
	publicData, err := root.ReadFile(publicKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read public key: %w", err)
	}
	privateKey, err := decodeKey(privateData, ed25519.PrivateKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("decode private key: %w", err)
	}
	publicKey, err := decodeKey(publicData, ed25519.PublicKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("decode public key: %w", err)
	}
	derived, ok := ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(ed25519.PublicKey(publicKey)) {
		return nil, nil, errors.New("ed25519 public key does not match private key")
	}

	return ed25519.PrivateKey(privateKey), ed25519.PublicKey(publicKey), nil
}

func decodeKey(data []byte, size int) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(bytesTrimSpace(data)))
	if err != nil {
		return nil, err
	}
	if len(decoded) != size {
		return nil, fmt.Errorf("key length = %d, want %d", len(decoded), size)
	}

	return decoded, nil
}

func defaultKeyDir(keyDir string) (string, error) {
	if keyDir != "" {
		return keyDir, nil
	}
	if env := os.Getenv(envKeyDir); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".agentreceipt", "keys"), nil
}

func bytesTrimSpace(data []byte) []byte {
	for len(data) > 0 {
		last := data[len(data)-1]
		if last != '\n' && last != '\r' && last != '\t' && last != ' ' {
			break
		}
		data = data[:len(data)-1]
	}
	for len(data) > 0 {
		first := data[0]
		if first != '\n' && first != '\r' && first != '\t' && first != ' ' {
			break
		}
		data = data[1:]
	}

	return data
}

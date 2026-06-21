package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultCodexFirstConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.Version != SchemaVersion {
		t.Fatalf("Version = %d, want %d", cfg.Version, SchemaVersion)
	}
	if !cfg.Capture.Git || !cfg.Capture.Filesystem || !cfg.Capture.CodexLogs {
		t.Fatalf("default capture sources are not enabled: %+v", cfg.Capture)
	}
	if cfg.Capture.ClaudeHooks {
		t.Fatal("Claude hooks should be disabled in the Codex-first MVP")
	}
	if cfg.Privacy.StorePrompts || cfg.Privacy.StoreRawToolOutputs {
		t.Fatalf("privacy defaults should not export prompts or raw tool outputs: %+v", cfg.Privacy)
	}
	if !slices.Contains(cfg.TestCommands, "make verify") {
		t.Fatalf("default test commands missing make verify: %+v", cfg.TestCommands)
	}
}

func TestLoadMergesWithDefaults(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ConfigFile)
	if err := os.WriteFile(path, []byte("version: 1\nprivacy:\n  max_blob_bytes: 42\n"), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Privacy.MaxBlobBytes != 42 {
		t.Fatalf("MaxBlobBytes = %d, want 42", cfg.Privacy.MaxBlobBytes)
	}
	if !cfg.Capture.Git || len(cfg.SensitivePaths) == 0 {
		t.Fatalf("defaults were not preserved after load: %+v", cfg)
	}
}

func TestWriteAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", ConfigFile)
	cfg := Default()
	cfg.Session.IdleTimeoutMinutes = 10

	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Session.IdleTimeoutMinutes != 10 {
		t.Fatalf("IdleTimeoutMinutes = %d, want 10", got.Session.IdleTimeoutMinutes)
	}
}

func TestValidateRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "version", mutate: func(cfg *Config) { cfg.Version = 2 }},
		{name: "idle timeout", mutate: func(cfg *Config) { cfg.Session.IdleTimeoutMinutes = -1 }},
		{name: "max blob bytes", mutate: func(cfg *Config) { cfg.Privacy.MaxBlobBytes = 0 }},
		{name: "sensitive paths", mutate: func(cfg *Config) { cfg.SensitivePaths = nil }},
		{name: "test commands", mutate: func(cfg *Config) { cfg.TestCommands = nil }},
		{name: "trusted signer key ids", mutate: func(cfg *Config) {
			cfg.Trust.TrustedSignerKeyIDs = []string{"not-a-key-id"}
		}},
	}
	for _, test := range tests {
		cfg := Default()
		test.mutate(&cfg)
		if err := Validate(cfg); err == nil {
			t.Fatalf("Validate() returned nil for invalid %s", test.name)
		}
	}
}

func TestLoadRejectsMissingAndInvalidFiles(t *testing.T) {
	t.Parallel()

	if _, err := Load(filepath.Join(t.TempDir(), ConfigFile)); err == nil {
		t.Fatal("Load() returned nil error for missing config")
	}

	path := filepath.Join(t.TempDir(), ConfigFile)
	if err := os.WriteFile(path, []byte("version: [not-valid\n"), 0o600); err != nil {
		t.Fatalf("write invalid config fixture: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() returned nil error for invalid YAML")
	}
}

func TestWriteRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.TestCommands = nil
	if err := Write(filepath.Join(t.TempDir(), ConfigFile), cfg); err == nil {
		t.Fatal("Write() returned nil error for invalid config")
	}
}

func TestOpenRootForPathRejectsDirectoryPath(t *testing.T) {
	t.Parallel()

	root, _, err := openRootForPath(".")
	if err == nil {
		_ = root.Close()
		t.Fatal("openRootForPath() returned nil error for directory path")
	}
}

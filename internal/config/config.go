package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = 1
	ConfigFile    = "agentreceipt.yml"
	PolicyFile    = "policy.yml"
)

// Config is an optional explicit AgentReceipt configuration.
type Config struct {
	Version        int      `yaml:"version" json:"version"`
	Session        Session  `yaml:"session" json:"session"`
	Capture        Capture  `yaml:"capture" json:"capture"`
	Privacy        Privacy  `yaml:"privacy" json:"privacy"`
	Review         Review   `yaml:"review" json:"review"`
	SensitivePaths []string `yaml:"sensitive_paths" json:"sensitive_paths"`
	TestCommands   []string `yaml:"test_commands" json:"test_commands"`
}

type Session struct {
	IdleTimeoutMinutes int  `yaml:"idle_timeout_minutes" json:"idle_timeout_minutes"`
	AutoFinalizeOnStop bool `yaml:"auto_finalize_on_stop" json:"auto_finalize_on_stop"`
}

type Capture struct {
	Git                  bool `yaml:"git" json:"git"`
	Filesystem           bool `yaml:"filesystem" json:"filesystem"`
	ClaudeHooks          bool `yaml:"claude_hooks" json:"claude_hooks"`
	CodexLogs            bool `yaml:"codex_logs" json:"codex_logs"`
	StoreTerminalOutput  bool `yaml:"store_terminal_output" json:"store_terminal_output"`
	StoreProviderRawLogs bool `yaml:"store_provider_raw_logs" json:"store_provider_raw_logs"`
}

type Privacy struct {
	RedactSecrets       bool `yaml:"redact_secrets" json:"redact_secrets"`
	StorePrompts        bool `yaml:"store_prompts" json:"store_prompts"`
	StoreRawToolOutputs bool `yaml:"store_raw_tool_outputs" json:"store_raw_tool_outputs"`
	MaxBlobBytes        int  `yaml:"max_blob_bytes" json:"max_blob_bytes"`
}

type Review struct {
	RequireTestsForCodeChanges bool `yaml:"require_tests_for_code_changes" json:"require_tests_for_code_changes"`
	RequireTypecheckForTS      bool `yaml:"require_typecheck_for_ts" json:"require_typecheck_for_ts"`
	FlagDependencyChanges      bool `yaml:"flag_dependency_changes" json:"flag_dependency_changes"`
	FlagAuthChanges            bool `yaml:"flag_auth_changes" json:"flag_auth_changes"`
	FlagSecretPaths            bool `yaml:"flag_secret_paths" json:"flag_secret_paths"`
}

func Default() Config {
	return Config{
		Version: SchemaVersion,
		Session: Session{
			IdleTimeoutMinutes: 30,
			AutoFinalizeOnStop: true,
		},
		Capture: Capture{
			Git:                  true,
			Filesystem:           true,
			ClaudeHooks:          false,
			CodexLogs:            true,
			StoreTerminalOutput:  false,
			StoreProviderRawLogs: true,
		},
		Privacy: Privacy{
			RedactSecrets:       true,
			StorePrompts:        false,
			StoreRawToolOutputs: false,
			MaxBlobBytes:        200000,
		},
		Review: Review{
			RequireTestsForCodeChanges: true,
			RequireTypecheckForTS:      true,
			FlagDependencyChanges:      true,
			FlagAuthChanges:            true,
			FlagSecretPaths:            true,
		},
		SensitivePaths: []string{
			".env",
			".env.*",
			"*.pem",
			"*.key",
			"id_rsa",
			"id_ed25519",
			".npmrc",
			".pypirc",
			".netrc",
			".aws/credentials",
			".github/workflows/**",
			"src/auth/**",
			"src/payments/**",
			"src/security/**",
			"src/crypto/**",
			"migrations/**",
			"Dockerfile",
			"docker-compose.yml",
		},
		TestCommands: []string{
			"npm test",
			"npm run test",
			"npm run lint",
			"npm run typecheck",
			"pnpm test",
			"pnpm lint",
			"pnpm typecheck",
			"yarn test",
			"staticcheck ./...",
			"go vet ./...",
			"tsc --noEmit",
			"pyright",
			"cargo test",
			"pytest",
			"go test ./...",
			"make test",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	root, name, err := openRootForPath(path)
	if err != nil {
		return Config{}, err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := root.ReadFile(name)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Write(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil && filepath.Dir(path) != "." {
		return fmt.Errorf("create config directory: %w", err)
	}
	root, name, err := openRootForPath(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()
	tmpName := "." + name + ".tmp"
	defer func() {
		_ = root.Remove(tmpName)
	}()
	tmp, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := root.Rename(tmpName, name); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}

	return nil
}

func openRootForPath(path string) (*os.Root, string, error) {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	name := filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) {
		return nil, "", fmt.Errorf("config path %q does not name a file", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", fmt.Errorf("open config root %q: %w", dir, err)
	}

	return root, name, nil
}

func Validate(cfg Config) error {
	switch {
	case cfg.Version != SchemaVersion:
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	case cfg.Session.IdleTimeoutMinutes < 0:
		return errors.New("session.idle_timeout_minutes must be zero or greater")
	case cfg.Privacy.MaxBlobBytes <= 0:
		return errors.New("privacy.max_blob_bytes must be greater than zero")
	case len(cfg.SensitivePaths) == 0:
		return errors.New("sensitive_paths must contain at least one pattern")
	case len(cfg.TestCommands) == 0:
		return errors.New("test_commands must contain at least one command")
	default:
		return nil
	}
}

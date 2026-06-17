package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ametel01/agentreceipt/internal/model"
)

const (
	RootDir               = ".agentreceipt"
	HomeEnv               = "AGENTRECEIPT_HOME"
	ReposDir              = "repos"
	SessionsDir           = "sessions"
	PolicyFile            = "policy.yml"
	EventsFile            = "events.jsonl"
	ReceiptJSONFile       = "receipt.json"
	ReceiptMarkdownFile   = "receipt.md"
	ReviewMarkdownFile    = "review.md"
	ManifestFile          = "manifest.json"
	StateFile             = "state.json"
	ActiveSessionFile     = "active_session"
	FilesystemWatcherPID  = "fswatcher.pid"
	FilesystemWatcherStop = "fswatcher.stop"
	FilesystemWatcherDone = "fswatcher.done"
	DiffsDir              = "diffs"
	FinalPatchFile        = "final.patch"
	ProviderDir           = "provider"
	ProviderCodexDir      = "codex"
	ProviderClaudeDir     = "claude"
	TracesDir             = "traces"
	CodexImportedSession  = "imported-session.jsonl"
	ParseReportFile       = "parse-report.json"
	BlobsDir              = "blobs"
	SignaturesDir         = "signatures"
	ReceiptSignatureFile  = "receipt.sig"
)

var sessionIDPattern = regexp.MustCompile(`^ar_ses_[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Layout struct {
	RepoRoot                  string
	RepoKey                   string
	Root                      string
	Repo                      string
	Sessions                  string
	Session                   string
	EventsJSONL               string
	ReceiptJSON               string
	ReceiptMarkdown           string
	ReviewMarkdown            string
	ManifestJSON              string
	StateJSON                 string
	FilesystemWatcherPIDPath  string
	FilesystemWatcherStopPath string
	FilesystemWatcherDonePath string
	Diffs                     string
	FinalPatch                string
	Provider                  string
	ProviderCodex             string
	ProviderCodexTraces       string
	CodexImportedSession      string
	CodexParseReport          string
	ProviderClaude            string
	ClaudeParseReport         string
	Blobs                     string
	Signatures                string
	ReceiptSignature          string
}

func NewLayout(repoRoot string, sessionID string) (Layout, error) {
	if err := ValidateSessionID(sessionID); err != nil {
		return Layout{}, err
	}
	if repoRoot == "" {
		return Layout{}, errors.New("repo root is required")
	}
	repoRoot = canonicalRepoRoot(repoRoot)
	root, err := DefaultRoot()
	if err != nil {
		return Layout{}, err
	}
	repoKey := RepositoryKey(repoRoot)
	repo := filepath.Join(root, ReposDir, repoKey)
	session := filepath.Join(repo, SessionsDir, sessionID)
	provider := filepath.Join(session, ProviderDir)
	codex := filepath.Join(provider, ProviderCodexDir)
	claude := filepath.Join(provider, ProviderClaudeDir)

	return Layout{
		RepoRoot:                  repoRoot,
		RepoKey:                   repoKey,
		Root:                      root,
		Repo:                      repo,
		Sessions:                  filepath.Join(repo, SessionsDir),
		Session:                   session,
		EventsJSONL:               filepath.Join(session, EventsFile),
		ReceiptJSON:               filepath.Join(session, ReceiptJSONFile),
		ReceiptMarkdown:           filepath.Join(session, ReceiptMarkdownFile),
		ReviewMarkdown:            filepath.Join(session, ReviewMarkdownFile),
		ManifestJSON:              filepath.Join(session, ManifestFile),
		StateJSON:                 filepath.Join(session, StateFile),
		FilesystemWatcherPIDPath:  filepath.Join(session, FilesystemWatcherPID),
		FilesystemWatcherStopPath: filepath.Join(session, FilesystemWatcherStop),
		FilesystemWatcherDonePath: filepath.Join(session, FilesystemWatcherDone),
		Diffs:                     filepath.Join(session, DiffsDir),
		FinalPatch:                filepath.Join(session, DiffsDir, FinalPatchFile),
		Provider:                  provider,
		ProviderCodex:             codex,
		ProviderCodexTraces:       filepath.Join(codex, TracesDir),
		CodexImportedSession:      filepath.Join(codex, CodexImportedSession),
		CodexParseReport:          filepath.Join(codex, ParseReportFile),
		ProviderClaude:            claude,
		ClaudeParseReport:         filepath.Join(claude, ParseReportFile),
		Blobs:                     filepath.Join(session, BlobsDir),
		Signatures:                filepath.Join(session, SignaturesDir),
		ReceiptSignature:          filepath.Join(session, SignaturesDir, ReceiptSignatureFile),
	}, nil
}

func ValidateSessionID(sessionID string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}

	return nil
}

func DefaultRoot() (string, error) {
	if home := os.Getenv(HomeEnv); home != "" {
		return home, nil
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		return filepath.Join(os.TempDir(), "agentreceipt-test"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, RootDir), nil
}

func RepositoryPath(repoRoot string) (string, error) {
	root, err := DefaultRoot()
	if err != nil {
		return "", err
	}
	if repoRoot == "" {
		return "", errors.New("repo root is required")
	}
	repoRoot = canonicalRepoRoot(repoRoot)

	return filepath.Join(root, ReposDir, RepositoryKey(repoRoot)), nil
}

func SessionsPath(repoRoot string) (string, error) {
	repo, err := RepositoryPath(repoRoot)
	if err != nil {
		return "", err
	}

	return filepath.Join(repo, SessionsDir), nil
}

func RepositoryKey(repoRoot string) string {
	clean := canonicalRepoRoot(repoRoot)
	sum := sha256.Sum256([]byte(clean))

	return hex.EncodeToString(sum[:])[:16]
}

func canonicalRepoRoot(repoRoot string) string {
	clean := filepath.Clean(repoRoot)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}

	return clean
}

func EnsureSessionLayout(layout Layout) error {
	for _, dir := range []string{
		layout.Root,
		layout.Sessions,
		layout.Session,
		layout.Diffs,
		layout.Provider,
		layout.ProviderCodex,
		layout.ProviderCodexTraces,
		layout.ProviderClaude,
		layout.Blobs,
		layout.Signatures,
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory %q: %w", dir, err)
		}
	}

	return nil
}

func ManifestArtifacts(layout Layout) model.Artifacts {
	return model.Artifacts{
		EventsJSONL:   filepath.ToSlash(relativeToSession(layout.Session, layout.EventsJSONL)),
		ReceiptJSON:   filepath.ToSlash(relativeToSession(layout.Session, layout.ReceiptJSON)),
		ReceiptMD:     filepath.ToSlash(relativeToSession(layout.Session, layout.ReceiptMarkdown)),
		ReviewMD:      filepath.ToSlash(relativeToSession(layout.Session, layout.ReviewMarkdown)),
		ManifestJSON:  filepath.ToSlash(relativeToSession(layout.Session, layout.ManifestJSON)),
		FinalPatch:    filepath.ToSlash(relativeToSession(layout.Session, layout.FinalPatch)),
		ReceiptSig:    filepath.ToSlash(relativeToSession(layout.Session, layout.ReceiptSignature)),
		CodexTraceDir: filepath.ToSlash(relativeToSession(layout.Session, layout.ProviderCodexTraces)),
	}
}

func relativeToSession(sessionRoot string, path string) string {
	rel, err := filepath.Rel(sessionRoot, path)
	if err != nil {
		return path
	}

	return rel
}

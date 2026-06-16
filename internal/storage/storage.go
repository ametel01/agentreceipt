package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ametel01/agentreceipt/internal/model"
)

const (
	RootDir              = ".agentreceipt"
	SessionsDir          = "sessions"
	PolicyFile           = "policy.yml"
	EventsFile           = "events.jsonl"
	ReceiptJSONFile      = "receipt.json"
	ReceiptMarkdownFile  = "receipt.md"
	ReviewMarkdownFile   = "review.md"
	ManifestFile         = "manifest.json"
	DiffsDir             = "diffs"
	FinalPatchFile       = "final.patch"
	ProviderDir          = "provider"
	ProviderCodexDir     = "codex"
	ProviderClaudeDir    = "claude"
	TracesDir            = "traces"
	CodexImportedSession = "imported-session.jsonl"
	ParseReportFile      = "parse-report.json"
	BlobsDir             = "blobs"
	SignaturesDir        = "signatures"
	ReceiptSignatureFile = "receipt.sig"
)

var sessionIDPattern = regexp.MustCompile(`^ar_ses_[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Layout struct {
	RepoRoot             string
	Root                 string
	Sessions             string
	Session              string
	EventsJSONL          string
	ReceiptJSON          string
	ReceiptMarkdown      string
	ReviewMarkdown       string
	ManifestJSON         string
	Diffs                string
	FinalPatch           string
	Provider             string
	ProviderCodex        string
	ProviderCodexTraces  string
	CodexImportedSession string
	CodexParseReport     string
	ProviderClaude       string
	ClaudeParseReport    string
	Blobs                string
	Signatures           string
	ReceiptSignature     string
}

func NewLayout(repoRoot string, sessionID string) (Layout, error) {
	if err := ValidateSessionID(sessionID); err != nil {
		return Layout{}, err
	}
	if repoRoot == "" {
		return Layout{}, errors.New("repo root is required")
	}
	root := filepath.Join(repoRoot, RootDir)
	session := filepath.Join(root, SessionsDir, sessionID)
	provider := filepath.Join(session, ProviderDir)
	codex := filepath.Join(provider, ProviderCodexDir)
	claude := filepath.Join(provider, ProviderClaudeDir)

	return Layout{
		RepoRoot:             repoRoot,
		Root:                 root,
		Sessions:             filepath.Join(root, SessionsDir),
		Session:              session,
		EventsJSONL:          filepath.Join(session, EventsFile),
		ReceiptJSON:          filepath.Join(session, ReceiptJSONFile),
		ReceiptMarkdown:      filepath.Join(session, ReceiptMarkdownFile),
		ReviewMarkdown:       filepath.Join(session, ReviewMarkdownFile),
		ManifestJSON:         filepath.Join(session, ManifestFile),
		Diffs:                filepath.Join(session, DiffsDir),
		FinalPatch:           filepath.Join(session, DiffsDir, FinalPatchFile),
		Provider:             provider,
		ProviderCodex:        codex,
		ProviderCodexTraces:  filepath.Join(codex, TracesDir),
		CodexImportedSession: filepath.Join(codex, CodexImportedSession),
		CodexParseReport:     filepath.Join(codex, ParseReportFile),
		ProviderClaude:       claude,
		ClaudeParseReport:    filepath.Join(claude, ParseReportFile),
		Blobs:                filepath.Join(session, BlobsDir),
		Signatures:           filepath.Join(session, SignaturesDir),
		ReceiptSignature:     filepath.Join(session, SignaturesDir, ReceiptSignatureFile),
	}, nil
}

func ValidateSessionID(sessionID string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}

	return nil
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

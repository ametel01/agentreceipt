package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/commandrisk"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
)

type Options struct {
	RepoPath  string
	SessionID string
	Last      bool
	Security  bool
	Diff      bool
	Config    config.Config
}

type Report struct {
	SchemaVersion int                     `json:"schema_version"`
	SessionID     string                  `json:"session_id"`
	GeneratedAt   time.Time               `json:"generated_at"`
	State         model.SessionState      `json:"state"`
	Provider      string                  `json:"provider"`
	Summary       model.Summary           `json:"summary"`
	Confidence    model.CaptureConfidence `json:"confidence"`
	Risk          model.Risk              `json:"risk"`
	Verification  model.Verification      `json:"verification"`
	Focus         []string                `json:"focus"`
	Gaps          []string                `json:"gaps"`
	Warnings      []model.Warning         `json:"warnings,omitempty"`
	Timeline      []TimelineItem          `json:"timeline"`
	EventsByType  map[string]int          `json:"events_by_type"`
	Git           GitSummary              `json:"git"`
}

type TimelineItem struct {
	Seq    int64  `json:"seq"`
	Time   string `json:"time"`
	Source string `json:"source"`
	Type   string `json:"type"`
}

type GitSummary struct {
	Branch            string      `json:"branch"`
	Head              string      `json:"head"`
	Base              string      `json:"base,omitempty"`
	BaseFound         bool        `json:"base_found"`
	Ahead             int         `json:"ahead"`
	Behind            int         `json:"behind"`
	Dirty             bool        `json:"dirty"`
	Staged            int         `json:"staged"`
	Unstaged          int         `json:"unstaged"`
	Untracked         int         `json:"untracked"`
	Status            []GitStatus `json:"status"`
	BranchDiff        DiffSummary `json:"branch_diff"`
	WorkspaceDiff     DiffSummary `json:"workspace_diff"`
	ReceiptDiffStatus string      `json:"receipt_diff_status"`
}

type GitStatus struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type DiffSummary struct {
	Files      int      `json:"files"`
	Insertions int      `json:"insertions"`
	Deletions  int      `json:"deletions"`
	ShortStat  string   `json:"short_stat"`
	StatLines  []string `json:"stat_lines,omitempty"`
}

var fallbackCommandKindPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{kind: "network", pattern: regexp.MustCompile(`\b(curl|wget|ssh|nc|aws|gcloud)\b`)},
	{kind: "destructive", pattern: regexp.MustCompile(`\b(rm|dd|mkfs|shutdown|reboot)\b`)},
}

var shortStatPatterns = map[string]*regexp.Regexp{
	"files":      regexp.MustCompile(`(\d+) files? changed`),
	"insertions": regexp.MustCompile(`(\d+) insertions?\(\+\)`),
	"deletions":  regexp.MustCompile(`(\d+) deletions?\(-\)`),
}

const baseNotFoundText = "not found (looked for upstream, origin/HEAD, main/master/trunk/develop)"
const maxRiskCommandSummaryRunes = 100

func Build(ctx context.Context, options Options) (Report, error) {
	repoRoot, sessionID, err := resolveSession(ctx, options)
	if err != nil {
		return Report{}, err
	}
	layout, err := storage.NewLayout(repoRoot, sessionID)
	if err != nil {
		return Report{}, err
	}
	state, err := readState(layout)
	if err != nil {
		return Report{}, err
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		return Report{}, err
	}
	chainHash, replayErr := eventlog.Replay(events)
	report := Report{
		SchemaVersion: model.SchemaVersion,
		SessionID:     sessionID,
		GeneratedAt:   time.Now().UTC(),
		State:         state.State,
		Provider:      providerLabel(events),
		Warnings:      state.Warnings,
		EventsByType:  make(map[string]int),
	}
	cfg := configForReview(options.Config)
	report.Summary = summarize(events, cfg)
	report.Confidence = confidence(events)
	if gitSummary, gitErr := buildGitSummary(ctx, repoRoot, state.FinalDiffHash); gitErr != nil {
		report.Warnings = append(report.Warnings, model.Warning{
			Code:    "git_review_unavailable",
			Message: gitErr.Error(),
		})
	} else {
		report.Git = gitSummary
	}
	report.Risk = risk(report.Summary, state.Warnings, events, cfg)
	report.Focus = focus(report.Summary, report.Risk, cfg)
	report.Gaps = gaps(report.Summary, report.Confidence, state.Warnings, cfg)
	report.Timeline = timeline(events)
	for _, event := range events {
		report.EventsByType[event.Type]++
	}
	report.Verification = model.Verification{
		EventChainHash: chainHash,
		DiffHash:       state.FinalDiffHash,
		Valid:          replayErr == nil,
	}
	if replayErr != nil {
		report.Risk.Level = maxRisk(report.Risk.Level, model.RiskHigh)
		report.Risk.Reasons = append(report.Risk.Reasons, model.RiskReason{
			Code:       "event_chain_invalid",
			Message:    replayErr.Error(),
			Level:      model.RiskHigh,
			Confidence: model.ConfidenceHigh,
		})
		report.Gaps = append(report.Gaps, "Event chain replay failed.")
	}
	if options.Security {
		report.Focus = append(report.Focus, "Review security-sensitive path changes and provider risk signals first.")
	}
	if options.Diff {
		report.Focus = append(report.Focus, "Compare final patch hash against reviewer-visible diff.")
	}

	return report, nil
}

func RenderTerminal(report Report) string {
	return RenderTerminalColor(report, false)
}

func RenderTerminalColor(report Report, color bool) string {
	var builder strings.Builder
	builder.WriteString(reviewColorize("AgentReceipt Review", reviewColorBoldWhite, color) + "\n\n")
	fmt.Fprintf(&builder, "Session: %s\n", reviewColorize(report.SessionID, reviewColorCyan, color))
	fmt.Fprintf(&builder, "Provider: %s\n", report.Provider)
	fmt.Fprintf(&builder, "State: %s\n", reviewColorize(string(report.State), reviewColorForState(report.State), color))
	fmt.Fprintf(&builder, "Risk: %s\n", reviewColorize(string(report.Risk.Level), reviewColorForRisk(report.Risk.Level), color))
	fmt.Fprintf(&builder, "\n%s\n", reviewColorize("Branch state:", reviewColorBoldCyan, color))
	fmt.Fprintf(&builder, "- Branch: %s\n", valueOrUnknown(report.Git.Branch))
	if report.Git.BaseFound {
		fmt.Fprintf(&builder, "- Base: %s\n", report.Git.Base)
		fmt.Fprintf(&builder, "- Ahead/behind: %d ahead, %d behind\n", report.Git.Ahead, report.Git.Behind)
	} else {
		fmt.Fprintf(&builder, "- Base: %s\n", reviewColorize(baseNotFoundText, reviewColorYellow, color))
	}
	fmt.Fprintf(&builder, "- Working tree: %s (%d staged, %d unstaged, %d untracked)\n", reviewColorize(dirtyText(report.Git.Dirty), reviewColorForDirty(report.Git.Dirty), color), report.Git.Staged, report.Git.Unstaged, report.Git.Untracked)
	fmt.Fprintf(&builder, "- Receipt diff: %s\n", reviewColorize(receiptDiffText(report.Git.ReceiptDiffStatus), reviewColorForReceiptDiff(report.Git.ReceiptDiffStatus), color))
	fmt.Fprintf(&builder, "\n%s\n", reviewColorize("Diff:", reviewColorBoldCyan, color))
	renderDiffSummary(&builder, "Branch vs "+baseLabel(report.Git), report.Git.BranchDiff, color)
	renderDiffSummary(&builder, "Workspace vs HEAD", report.Git.WorkspaceDiff, color)
	fmt.Fprintf(&builder, "\n%s\n", reviewColorize("Session evidence:", reviewColorBoldCyan, color))
	fmt.Fprintf(&builder, "- Commands detected: %d\n", len(report.Summary.DetectedCommands))
	fmt.Fprintf(&builder, "- Filesystem write events: %d files\n", len(report.Summary.ChangedFiles))
	fmt.Fprintf(&builder, "- Provider tool events: %d\n", report.EventsByType["provider.command"]+report.EventsByType["provider.event"])
	fmt.Fprintf(&builder, "\n%s\n", reviewColorize("Warnings:", reviewColorBoldCyan, color))
	if len(report.Warnings) == 0 {
		fmt.Fprintf(&builder, "- %s\n", reviewColorize("none", reviewColorGreen, color))
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&builder, "- %s: %s\n", reviewColorize(warning.Code, reviewColorYellow, color), warning.Message)
	}
	fmt.Fprintf(&builder, "\n%s\n", reviewColorize("Reviewer focus:", reviewColorBoldCyan, color))
	for _, item := range report.Focus {
		fmt.Fprintf(&builder, "- %s\n", item)
	}

	return builder.String()
}

func RenderMarkdown(report Report) string {
	var builder strings.Builder
	builder.WriteString("## AgentReceipt\n\n")
	fmt.Fprintf(&builder, "Status: %s\n\n", statusText(report))
	builder.WriteString("Session:\n")
	fmt.Fprintf(&builder, "- Provider: %s\n", report.Provider)
	fmt.Fprintf(&builder, "- Session: `%s`\n", report.SessionID)
	fmt.Fprintf(&builder, "- Branch: `%s`\n", valueOrUnknown(report.Git.Branch))
	if report.Git.BaseFound {
		fmt.Fprintf(&builder, "- Base: `%s`, %d ahead / %d behind\n", report.Git.Base, report.Git.Ahead, report.Git.Behind)
	} else {
		fmt.Fprintf(&builder, "- Base: %s\n", baseNotFoundText)
	}
	fmt.Fprintf(&builder, "- Working tree: %s (%d staged, %d unstaged, %d untracked)\n", dirtyText(report.Git.Dirty), report.Git.Staged, report.Git.Unstaged, report.Git.Untracked)
	fmt.Fprintf(&builder, "- Branch diff: %s\n", diffShortStat(report.Git.BranchDiff))
	fmt.Fprintf(&builder, "- Workspace diff: %s\n", diffShortStat(report.Git.WorkspaceDiff))
	fmt.Fprintf(&builder, "- Receipt diff: %s\n", receiptDiffText(report.Git.ReceiptDiffStatus))
	fmt.Fprintf(&builder, "- Tool events: %d\n", report.EventsByType["provider.command"]+report.EventsByType["provider.event"])
	fmt.Fprintf(&builder, "- Commands detected: %d\n", len(report.Summary.DetectedCommands))
	fmt.Fprintf(&builder, "- Tests detected: %t\n\n", report.Summary.TestDetected)
	builder.WriteString("Risk:\n")
	for _, reason := range report.Risk.Reasons {
		fmt.Fprintf(&builder, "- %s: %s\n", reason.Level, reason.Message)
	}
	if len(report.Risk.Reasons) == 0 {
		builder.WriteString("- none\n")
	}
	builder.WriteString("\nEvidence:\n")
	fmt.Fprintf(&builder, "- Git snapshots: %s\n", report.Confidence.GitDiff)
	fmt.Fprintf(&builder, "- Filesystem write events: %d files\n", len(report.Summary.ChangedFiles))
	fmt.Fprintf(&builder, "- Provider tool events: %d\n\n", report.EventsByType["provider.command"]+report.EventsByType["provider.event"])
	builder.WriteString("Reviewer focus:\n")
	for _, item := range report.Focus {
		fmt.Fprintf(&builder, "- %s\n", item)
	}

	return builder.String()
}

func buildGitSummary(ctx context.Context, repoRoot string, finalDiffHash string) (GitSummary, error) {
	branch, err := gitBranchName(ctx, repoRoot)
	if err != nil {
		return GitSummary{}, err
	}
	head, err := gitShortHead(ctx, repoRoot)
	if err != nil {
		return GitSummary{}, err
	}
	statusText, err := gitStatus(ctx, repoRoot)
	if err != nil {
		return GitSummary{}, err
	}
	status, staged, unstaged, untracked := parseGitStatus(statusText)
	workspaceDiff, err := gitDiffSummary(ctx, repoRoot, "HEAD")
	if err != nil {
		return GitSummary{}, err
	}
	summary := GitSummary{
		Branch:            strings.TrimSpace(branch),
		Head:              strings.TrimSpace(head),
		Dirty:             len(status) > 0,
		Staged:            staged,
		Unstaged:          unstaged,
		Untracked:         untracked,
		Status:            status,
		WorkspaceDiff:     workspaceDiff,
		ReceiptDiffStatus: "not_finalized",
	}
	if finalDiffHash != "" {
		currentDiff, diffErr := gitWorkspacePatch(ctx, repoRoot)
		if diffErr != nil {
			return GitSummary{}, diffErr
		}
		if hashString(currentDiff) == finalDiffHash {
			summary.ReceiptDiffStatus = "matches_current_workspace"
		} else {
			summary.ReceiptDiffStatus = "differs_from_current_workspace"
		}
	}
	if base, ok := detectBaseRef(ctx, repoRoot); ok {
		summary.Base = base
		summary.BaseFound = true
		ahead, behind, countsErr := aheadBehind(ctx, repoRoot, base)
		if countsErr != nil {
			return GitSummary{}, countsErr
		}
		summary.Ahead = ahead
		summary.Behind = behind
		branchDiff, diffErr := gitDiffSummary(ctx, repoRoot, base+"...HEAD")
		if diffErr != nil {
			return GitSummary{}, diffErr
		}
		summary.BranchDiff = branchDiff
	}

	return summary, nil
}

func detectBaseRef(ctx context.Context, repoRoot string) (string, bool) {
	candidates := make([]string, 0, 10)
	if upstream, err := gitCurrentUpstream(ctx, repoRoot); err == nil {
		candidates = append(candidates, strings.TrimSpace(upstream))
	}
	if originHead, err := gitOriginHead(ctx, repoRoot); err == nil {
		candidates = append(candidates, strings.TrimSpace(originHead))
	}
	candidates = append(candidates, "main", "master", "trunk", "develop", "origin/main", "origin/master", "origin/trunk", "origin/develop")

	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if _, err := gitVerifyBase(ctx, repoRoot, candidate); err == nil {
			return candidate, true
		}
	}

	return "", false
}

func aheadBehind(ctx context.Context, repoRoot string, base string) (int, int, error) {
	output, err := gitAheadBehind(ctx, repoRoot, base)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(output)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("git rev-list returned unexpected ahead/behind output: %q", strings.TrimSpace(output))
	}
	behind, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind count %q: %w", fields[0], err)
	}
	ahead, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead count %q: %w", fields[1], err)
	}

	return ahead, behind, nil
}

func gitDiffSummary(ctx context.Context, repoRoot string, revision string) (DiffSummary, error) {
	shortStat, err := gitDiffShortStat(ctx, repoRoot, revision)
	if err != nil {
		return DiffSummary{}, err
	}
	stat, err := gitDiffStat(ctx, repoRoot, revision)
	if err != nil {
		return DiffSummary{}, err
	}
	summary := parseShortStat(shortStat)
	summary.ShortStat = strings.TrimSpace(shortStat)
	summary.StatLines = parseStatLines(stat)

	return summary, nil
}

func parseGitStatus(status string) ([]GitStatus, int, int, int) {
	lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
	entries := make([]GitStatus, 0, len(lines))
	var staged, unstaged, untracked int
	for _, line := range lines {
		if line == "" {
			continue
		}
		code := strings.TrimSpace(line[:min(2, len(line))])
		path := ""
		if len(line) > 3 {
			path = line[3:]
		}
		entries = append(entries, GitStatus{Code: code, Path: path})
		if strings.HasPrefix(line, "??") {
			untracked++
			continue
		}
		if line[0] != ' ' {
			staged++
		}
		if len(line) > 1 && line[1] != ' ' {
			unstaged++
		}
	}

	return entries, staged, unstaged, untracked
}

func parseShortStat(shortStat string) DiffSummary {
	stat := DiffSummary{}
	for field, pattern := range shortStatPatterns {
		match := pattern.FindStringSubmatch(shortStat)
		if len(match) != 2 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		switch field {
		case "files":
			stat.Files = value
		case "insertions":
			stat.Insertions = value
		case "deletions":
			stat.Deletions = value
		}
	}

	return stat
}

func parseStatLines(stat string) []string {
	lines := strings.Split(strings.TrimSpace(stat), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.Contains(trimmed, " changed") {
			continue
		}
		result = append(result, trimmed)
	}

	return result
}

func renderDiffSummary(builder *strings.Builder, label string, summary DiffSummary, color bool) {
	fmt.Fprintf(builder, "- %s: %s\n", label, reviewColorize(diffShortStat(summary), reviewColorForDiff(summary), color))
	for _, line := range summary.StatLines {
		fmt.Fprintf(builder, "  %s\n", reviewColorize(line, reviewColorDim, color))
	}
}

func diffShortStat(summary DiffSummary) string {
	if summary.ShortStat == "" {
		return "no changes"
	}

	return summary.ShortStat
}

func baseLabel(summary GitSummary) string {
	if summary.BaseFound {
		return summary.Base
	}

	return "base"
}

func dirtyText(dirty bool) string {
	if dirty {
		return "dirty"
	}

	return "clean"
}

func receiptDiffText(status string) string {
	switch status {
	case "matches_current_workspace":
		return "matches current workspace"
	case "differs_from_current_workspace":
		return "differs from current workspace"
	case "not_finalized":
		return "not finalized"
	default:
		return "unavailable"
	}
}

const (
	reviewColorBoldCyan  = "1;36"
	reviewColorBoldRed   = "1;31"
	reviewColorBoldWhite = "1;37"
	reviewColorCyan      = "36"
	reviewColorDim       = "2;37"
	reviewColorGreen     = "32"
	reviewColorRed       = "31"
	reviewColorYellow    = "33"
)

func reviewColorForRisk(level model.RiskLevel) string {
	switch level {
	case model.RiskInfo, model.RiskLow:
		return reviewColorGreen
	case model.RiskMedium:
		return reviewColorYellow
	case model.RiskHigh:
		return reviewColorRed
	case model.RiskCritical:
		return reviewColorBoldRed
	default:
		return reviewColorDim
	}
}

func reviewColorForState(state model.SessionState) string {
	switch state {
	case model.SessionStateFinalized, model.SessionStateVerified:
		return reviewColorGreen
	case model.SessionStateActive, model.SessionStateStarting, model.SessionStateFinalizing:
		return reviewColorYellow
	default:
		return reviewColorDim
	}
}

func reviewColorForDirty(dirty bool) string {
	if dirty {
		return reviewColorYellow
	}

	return reviewColorGreen
}

func reviewColorForReceiptDiff(status string) string {
	switch status {
	case "matches_current_workspace":
		return reviewColorGreen
	case "differs_from_current_workspace":
		return reviewColorRed
	case "not_finalized":
		return reviewColorYellow
	default:
		return reviewColorDim
	}
}

func reviewColorForDiff(summary DiffSummary) string {
	if summary.ShortStat == "" {
		return reviewColorDim
	}
	if summary.Deletions > 0 && summary.Insertions == 0 {
		return reviewColorRed
	}
	if summary.Insertions > 0 || summary.Deletions > 0 || summary.Files > 0 {
		return reviewColorYellow
	}

	return reviewColorDim
}

func reviewColorize(value string, code string, enabled bool) string {
	if !enabled || value == "" {
		return value
	}

	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}

	return value
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))

	return "sha256:" + hex.EncodeToString(sum[:])
}

func gitBranchName(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git rev-parse --abbrev-ref HEAD")
}

func gitShortHead(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git rev-parse --short HEAD")
}

func gitStatus(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git status --porcelain=v1")
}

func gitCurrentUpstream(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git rev-parse --abbrev-ref --symbolic-full-name @{upstream}")
}

func gitOriginHead(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git symbolic-ref --quiet --short refs/remotes/origin/HEAD")
}

func gitWorkspacePatch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--binary", "HEAD")
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git diff --binary HEAD")
}

func gitVerifyBase(ctx context.Context, dir string, base string) (string, error) {
	if !isSafeGitBaseRef(base) {
		return "", fmt.Errorf("unsupported base ref %q", base)
	}
	commitRef := base + "^{commit}"
	// #nosec G204 -- commitRef is built from a validated git ref and passed without a shell.
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", commitRef)
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git rev-parse --verify --quiet "+commitRef)
}

func gitAheadBehind(ctx context.Context, dir string, base string) (string, error) {
	if !isSafeGitBaseRef(base) {
		return "", fmt.Errorf("unsupported base ref %q", base)
	}
	revision := base + "...HEAD"
	// #nosec G204 -- revision is built from a validated git ref and passed without a shell.
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", revision)
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git rev-list --left-right --count "+revision)
}

func gitDiffShortStat(ctx context.Context, dir string, revision string) (string, error) {
	if !isSafeGitDiffRevision(revision) {
		return "", fmt.Errorf("unsupported diff revision %q", revision)
	}
	// #nosec G204 -- revision is validated and passed as a single git argument without a shell.
	cmd := exec.CommandContext(ctx, "git", "diff", "--shortstat", "--find-renames", revision)
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git diff --shortstat --find-renames "+revision)
}

func gitDiffStat(ctx context.Context, dir string, revision string) (string, error) {
	if !isSafeGitDiffRevision(revision) {
		return "", fmt.Errorf("unsupported diff revision %q", revision)
	}
	// #nosec G204 -- revision is validated and passed as a single git argument without a shell.
	cmd := exec.CommandContext(ctx, "git", "diff", "--stat", "--find-renames", revision)
	cmd.Dir = dir

	return gitCommandOutput(cmd, "git diff --stat --find-renames "+revision)
}

func isSafeGitDiffRevision(revision string) bool {
	if revision == "HEAD" {
		return true
	}
	base, ok := strings.CutSuffix(revision, "...HEAD")

	return ok && isSafeGitBaseRef(base)
}

func isSafeGitBaseRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	if strings.ContainsAny(ref, " \t\r\n~^:?*[\\") || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return false
	}
	if strings.HasSuffix(ref, ".lock") || strings.Contains(ref, "@{") {
		return false
	}
	for _, char := range ref {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '/', '.', '_', '-':
			continue
		default:
			return false
		}
	}
	return true
}

func gitCommandOutput(cmd *exec.Cmd, description string) (string, error) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", description, err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

func resolveSession(ctx context.Context, options Options) (string, string, error) {
	repoRoot, err := gitmonitor.DiscoverRoot(ctx, repoPathOrCWD(options.RepoPath))
	if err != nil {
		return "", "", err
	}
	if options.SessionID != "" {
		return repoRoot, options.SessionID, nil
	}
	if !options.Last {
		manager := session.Manager{RepoPath: repoRoot}
		if state, ok, err := manager.Status(ctx); err != nil {
			return "", "", err
		} else if ok {
			return repoRoot, state.SessionID, nil
		}
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

func configForReview(cfg config.Config) config.Config {
	if cfg.Version == 0 {
		return config.Default()
	}

	return cfg
}

func summarize(events []model.Event, cfg config.Config) model.Summary {
	changedByPath := make(map[string]model.ChangedFile)
	commandAttempts := make([]commandAttempt, 0)
	resultsByCallID := make(map[string]string)
	resultsByCommand := make(map[string]string)
	for _, event := range events {
		if event.Type == "fs.change" {
			path := stringPayload(event.Payload, "path")
			if path != "" {
				changedByPath[path] = model.ChangedFile{
					Path:       path,
					Action:     stringPayload(event.Payload, "action"),
					Sensitive:  boolPayload(event.Payload, "sensitive"),
					Dependency: boolPayload(event.Payload, "dependency"),
				}
			}
		}
		if event.Type == "provider.command" {
			command := commandFromPayload(event.Payload)
			if command != "" {
				kind := commandKind(command, cfg)
				commandAttempts = append(commandAttempts, commandAttempt{
					command: model.DetectedCommand{
						Command:    command,
						Kind:       kind,
						Status:     "unknown",
						Source:     event.Source,
						Confidence: model.ConfidenceMedium,
					},
					callID: callIDFromCommandPayload(event.Payload),
				})
			}
		}
		if event.Type == "provider.command_result" {
			result := commandResultFromPayload(event.Payload)
			if result.status != "" {
				if result.callID != "" {
					resultsByCallID[result.callID] = result.status
				}
				if result.command != "" {
					resultsByCommand[result.command] = result.status
				}
			}
		}
	}
	commands := make([]model.DetectedCommand, 0, len(commandAttempts))
	for _, attempt := range commandAttempts {
		command := attempt.command
		if attempt.callID != "" {
			if status := resultsByCallID[attempt.callID]; status != "" {
				command.Status = status
			}
		} else if status := resultsByCommand[attempt.command.Command]; status != "" {
			command.Status = status
		}
		commands = append(commands, command)
	}
	changedFiles := make([]model.ChangedFile, 0, len(changedByPath))
	for _, changed := range changedByPath {
		changedFiles = append(changedFiles, changed)
	}
	sort.Slice(changedFiles, func(i, j int) bool {
		return changedFiles[i].Path < changedFiles[j].Path
	})
	summary := model.Summary{ChangedFiles: changedFiles, DetectedCommands: commands}
	for _, command := range commands {
		switch command.Kind {
		case "test":
			summary.TestDetected = true
		case "lint":
			summary.LintDetected = true
		case "typecheck":
			summary.TypecheckDetected = true
		}
	}

	return summary
}

func hasTypeScriptChanges(summary model.Summary) bool {
	for _, changed := range summary.ChangedFiles {
		path := strings.ToLower(changed.Path)
		switch filepath.Ext(path) {
		case ".ts", ".tsx", ".mts", ".cts":
			return true
		}
	}

	return false
}

func hasCodeChanges(summary model.Summary) bool {
	for _, changed := range summary.ChangedFiles {
		path := strings.ToLower(changed.Path)
		switch filepath.Ext(path) {
		case ".go",
			".ts", ".tsx", ".mts", ".cts",
			".js", ".jsx", ".mjs", ".cjs",
			".py",
			".rs",
			".java", ".kt", ".kts", ".scala",
			".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".m", ".mm",
			".sh", ".bash", ".zsh",
			".rb", ".php", ".swift":
			return true
		}
	}

	return false
}

func isAuthPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	for _, marker := range []string{"/auth/", "/authentication/", "/oauth/", "/jwt/"} {
		if strings.Contains("/"+path+"/", marker) {
			return true
		}
	}

	return false
}

type commandAttempt struct {
	command model.DetectedCommand
	callID  string
}

type commandResult struct {
	callID  string
	command string
	status  string
}

func confidence(events []model.Event) model.CaptureConfidence {
	confidence := model.CaptureConfidence{
		GitDiff:            model.ConfidenceNone,
		FilesystemWrites:   model.ConfidenceNone,
		ProviderToolEvents: model.ConfidenceNone,
		FileReads:          model.ConfidenceNone,
		NetworkCalls:       model.ConfidenceLow,
	}
	for _, event := range events {
		switch event.Source {
		case "git_monitor":
			confidence.GitDiff = model.ConfidenceHigh
		case "fs_watcher":
			confidence.FilesystemWrites = model.ConfidenceHigh
		}
		if isProviderEvidenceSource(event) && isProviderToolEvidenceEvent(event) {
			confidence.ProviderToolEvents = model.ConfidenceMedium
		}
	}

	return confidence
}

func isProviderEvidenceSource(event model.Event) bool {
	return event.Provider != "" || event.Source == "codex_session_log" || event.Source == "claude_hook"
}

func providerLabel(events []model.Event) string {
	providers := map[string]bool{}
	for _, event := range events {
		if !isProviderToolEvidenceEvent(event) {
			continue
		}
		switch {
		case event.Provider != "":
			providers[event.Provider] = true
		case event.Source == "codex_session_log":
			providers["codex"] = true
		case event.Source == "claude_hook":
			providers["claude"] = true
		}
	}
	switch {
	case providers["codex"] && providers["claude"]:
		return "Codex CLI + Claude Code"
	case providers["claude"]:
		return "Claude Code"
	case providers["codex"]:
		return "Codex CLI"
	default:
		return "unknown"
	}
}

func isProviderToolEvidenceEvent(event model.Event) bool {
	switch event.Type {
	case "provider.command", "provider.command_result", "provider.event":
		return true
	default:
		return false
	}
}

func risk(summary model.Summary, warnings []model.Warning, events []model.Event, cfg config.Config) model.Risk {
	result := model.Risk{Level: model.RiskInfo}
	for _, changed := range summary.ChangedFiles {
		if changed.Sensitive && cfg.Review.FlagSecretPaths {
			result.Level = maxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "sensitive_path_changed",
				Message:    "Sensitive path changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
		if isAuthPath(changed.Path) && cfg.Review.FlagAuthChanges {
			result.Level = maxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "auth_path_changed",
				Message:    "Authentication-sensitive path changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
		if changed.Dependency && cfg.Review.FlagDependencyChanges {
			result.Level = maxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "dependency_changed",
				Message:    "Dependency file changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
	}
	for _, reason := range commandRiskReasons(summary.DetectedCommands) {
		result.Level = maxRisk(result.Level, reason.Level)
		result.Reasons = append(result.Reasons, reason)
	}
	for _, reason := range providerRiskReasons(events) {
		result.Level = maxRisk(result.Level, reason.Level)
		result.Reasons = append(result.Reasons, reason)
	}
	for _, warning := range warnings {
		result.Level = maxRisk(result.Level, model.RiskLow)
		result.Reasons = append(result.Reasons, model.RiskReason{
			Code:       warning.Code,
			Message:    warning.Message,
			Level:      model.RiskLow,
			Confidence: model.ConfidenceMedium,
		})
	}

	return result
}

func commandRiskReasons(commands []model.DetectedCommand) []model.RiskReason {
	reasons := make([]model.RiskReason, 0)
	seen := map[string]bool{}
	for _, command := range commands {
		for _, classification := range commandrisk.Classify(command.Command) {
			if classification.Level == model.RiskLow || classification.Level == model.RiskInfo || classification.Level == "" {
				continue
			}
			code := "command_risk_" + riskCodeFragment(classification.Signal)
			message := commandRiskMessage(classification, command.Command)
			key := code + ":" + message
			if seen[key] {
				continue
			}
			seen[key] = true
			confidence := command.Confidence
			if confidence == "" {
				confidence = model.ConfidenceMedium
			}
			reasons = append(reasons, model.RiskReason{
				Code:       code,
				Message:    message,
				Level:      classification.Level,
				Confidence: confidence,
			})
		}
	}

	return reasons
}

func commandRiskMessage(classification commandrisk.Classification, command string) string {
	label := classification.Signal
	if label == "" {
		label = "command"
	}
	details := classification.Reason
	if details == "" {
		details = "command matched a risk rule"
	}

	return "Command risk detected (" + label + "): " + details + " in command: " + commandSummary(command)
}

func providerRiskReasons(events []model.Event) []model.RiskReason {
	reasons := make([]model.RiskReason, 0)
	seen := map[string]bool{}
	for _, event := range events {
		if event.Type != "provider.command" {
			continue
		}
		fallbackCommand := commandFromPayload(event.Payload)
		for _, signal := range providerRiskSignals(event.Payload, fallbackCommand) {
			if signal.level == model.RiskLow || signal.level == model.RiskInfo || signal.level == "" {
				continue
			}
			code := "provider_risk_" + riskCodeFragment(signal.signal)
			message := providerRiskMessage(signal)
			key := code + ":" + message
			if seen[key] {
				continue
			}
			seen[key] = true
			reasons = append(reasons, model.RiskReason{
				Code:       code,
				Message:    message,
				Level:      signal.level,
				Confidence: signal.confidence,
			})
		}
	}

	return reasons
}

type providerRiskSignal struct {
	level      model.RiskLevel
	signal     string
	details    string
	command    string
	confidence model.Confidence
}

func providerRiskSignals(payload map[string]any, fallbackCommand string) []providerRiskSignal {
	rawSignals := arrayPayload(payload, "risk_signals")
	signals := make([]providerRiskSignal, 0, len(rawSignals))
	for _, raw := range rawSignals {
		signalPayload, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		level := model.RiskLevel(stringPayload(signalPayload, "level"))
		if riskRank(level) == 0 {
			continue
		}
		confidence := model.Confidence(stringPayload(signalPayload, "confidence"))
		if confidence == "" {
			confidence = model.ConfidenceMedium
		}
		command := stringPayload(signalPayload, "command")
		if command == "" {
			command = fallbackCommand
		}
		signals = append(signals, providerRiskSignal{
			level:      level,
			signal:     stringPayload(signalPayload, "signal"),
			details:    stringPayload(signalPayload, "details"),
			command:    command,
			confidence: confidence,
		})
	}

	return signals
}

func providerRiskMessage(signal providerRiskSignal) string {
	label := signal.signal
	if label == "" {
		label = "provider"
	}
	details := signal.details
	if details == "" {
		details = "provider classified the command as risky"
	}
	message := "Provider risk detected (" + label + "): " + details
	if signal.command != "" {
		message += " in command: " + commandSummary(signal.command)
	}

	return message
}

func commandSummary(command string) string {
	return truncateRunes(normalizedCommand(command), maxRiskCommandSummaryRunes)
}

func truncateRunes(value string, maxRunes int) string {
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

func riskCodeFragment(value string) string {
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

func focus(summary model.Summary, risk model.Risk, cfg config.Config) []string {
	items := make([]string, 0)
	if len(risk.Reasons) == 0 {
		items = append(items, "Review the final diff against the generated receipt.")
	}
	for _, reason := range risk.Reasons {
		items = append(items, reason.Message)
	}
	if cfg.Review.RequireTestsForCodeChanges && hasCodeChanges(summary) && !summary.TestDetected {
		items = append(items, "Confirm appropriate tests were run for code changes.")
	}
	if cfg.Review.RequireTypecheckForTS && hasTypeScriptChanges(summary) && !summary.TypecheckDetected {
		items = append(items, "Confirm typecheck coverage where relevant.")
	}

	return items
}

func gaps(summary model.Summary, confidence model.CaptureConfidence, warnings []model.Warning, cfg config.Config) []string {
	gaps := make([]string, 0)
	if confidence.ProviderToolEvents == model.ConfidenceNone {
		gaps = append(gaps, "No provider tool events were observed.")
	}
	if cfg.Review.RequireTestsForCodeChanges && hasCodeChanges(summary) && !summary.TestDetected {
		gaps = append(gaps, "No test command detected.")
	}
	if !summary.LintDetected {
		gaps = append(gaps, "No lint command detected.")
	}
	if cfg.Review.RequireTypecheckForTS && hasTypeScriptChanges(summary) && !summary.TypecheckDetected {
		gaps = append(gaps, "No typecheck command detected for TypeScript changes.")
	}
	for _, warning := range warnings {
		gaps = append(gaps, warning.Message)
	}

	return gaps
}

func timeline(events []model.Event) []TimelineItem {
	items := make([]TimelineItem, 0, len(events))
	for _, event := range events {
		items = append(items, TimelineItem{
			Seq:    event.Seq,
			Time:   event.Timestamp.Format(time.RFC3339),
			Source: event.Source,
			Type:   event.Type,
		})
	}

	return items
}

func commandKind(command string, cfg config.Config) string {
	if kind := configuredCommandKind(command, cfg.TestCommands); kind != "" {
		return kind
	}
	for _, matcher := range fallbackCommandKindPatterns {
		if matcher.pattern.MatchString(command) {
			return matcher.kind
		}
	}

	return "command"
}

func configuredCommandKind(command string, configured []string) string {
	command = normalizedCommand(command)
	for _, candidate := range configured {
		candidate = normalizedCommand(candidate)
		if candidate == "" {
			continue
		}
		if command != candidate && !strings.HasPrefix(command, candidate+" ") {
			continue
		}
		switch {
		case strings.Contains(candidate, "lint") || strings.Contains(candidate, "staticcheck") || strings.Contains(candidate, "go vet"):
			return "lint"
		case strings.Contains(candidate, "typecheck") || strings.Contains(candidate, "tsc") || strings.Contains(candidate, "pyright"):
			return "typecheck"
		default:
			return "test"
		}
	}

	return ""
}

func normalizedCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func commandFromPayload(payload map[string]any) string {
	toolCall, ok := payload["tool_call"].(map[string]any)
	if !ok {
		return stringPayload(payload, "command")
	}
	if command := stringPayload(toolCall, "command"); command != "" {
		return command
	}

	return stringPayload(mapPayload(toolCall, "arguments"), "cmd")
}

func callIDFromCommandPayload(payload map[string]any) string {
	if callID := stringPayload(payload, "call_id"); callID != "" {
		return callID
	}
	return stringPayload(mapPayload(payload, "tool_call"), "call_id")
}

func commandResultFromPayload(payload map[string]any) commandResult {
	resultPayload := mapPayload(payload, "command_result")
	if resultPayload == nil {
		resultPayload = payload
	}

	return commandResult{
		callID:  stringPayload(resultPayload, "call_id"),
		command: stringPayload(resultPayload, "command"),
		status:  normalizeCommandStatus(stringPayload(resultPayload, "status")),
	}
}

func normalizeCommandStatus(status string) string {
	switch status {
	case "success", "failed", "unknown":
		return status
	default:
		return ""
	}
}

func readState(layout storage.Layout) (session.State, error) {
	root, err := os.OpenRoot(layout.Session)
	if err != nil {
		return session.State{}, err
	}
	defer func() {
		_ = root.Close()
	}()
	data, err := root.ReadFile(storage.StateFile)
	if err != nil {
		return session.State{}, err
	}
	var state session.State
	if err := json.Unmarshal(data, &state); err != nil {
		return session.State{}, err
	}

	return state, nil
}

func stringPayload(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}

	return ""
}

func boolPayload(payload map[string]any, key string) bool {
	if value, ok := payload[key].(bool); ok {
		return value
	}

	return false
}

func mapPayload(payload map[string]any, key string) map[string]any {
	if value, ok := payload[key].(map[string]any); ok {
		return value
	}

	return map[string]any{}
}

func arrayPayload(payload map[string]any, key string) []any {
	if value, ok := payload[key].([]any); ok {
		return value
	}

	return nil
}

func maxRisk(left model.RiskLevel, right model.RiskLevel) model.RiskLevel {
	if riskRank(right) > riskRank(left) {
		return right
	}

	return left
}

func riskRank(level model.RiskLevel) int {
	switch level {
	case model.RiskLow:
		return 1
	case model.RiskMedium:
		return 2
	case model.RiskHigh:
		return 3
	case model.RiskCritical:
		return 4
	default:
		return 0
	}
}

func statusText(report Report) string {
	if report.Verification.Valid && len(report.Warnings) == 0 {
		return "Verified"
	}
	if report.Verification.Valid {
		return "Verified with warnings"
	}

	return "Invalid"
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

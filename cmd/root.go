package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/provider/claude"
	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
	"github.com/ametel01/agentreceipt/internal/receipt"
	"github.com/ametel01/agentreceipt/internal/replay"
	"github.com/ametel01/agentreceipt/internal/review"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/signing"
	"github.com/ametel01/agentreceipt/internal/storage"
	"github.com/ametel01/agentreceipt/internal/trust"
	"github.com/spf13/cobra"
)

const prCommentFile = "-"

const (
	colorModeAuto   = "auto"
	colorModeAlways = "always"
	colorModeNever  = "never"
)

const (
	eventsFormatText  = "text"
	eventsFormatJSON  = "json"
	eventsFormatJSONL = "jsonl"
)

const (
	exitCodePass            = 0
	exitCodeReviewRequired  = 10
	exitCodeBlockerEvidence = 20
	exitCodeIntegrity       = 30
	exitCodeUnverifiable    = 40
	exitCodePatchMismatch   = 50
	exitCodeInvalidInput    = 60
)

const focusTaskKindDiffMismatch = "diff_mismatch"

// Execute runs the AgentReceipt CLI.
func Execute(version string) error {
	return NewRootCommand(version).Execute()
}

// ExitError carries a specific process exit code while preserving a message.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

// ExitCodeFromError returns the process exit code for a command error.
func ExitCodeFromError(err error) int {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	return 1
}

func invalidInputError(err error) error {
	return &ExitError{Code: exitCodeInvalidInput, Err: err}
}

// NewRootCommand builds the command tree. Tests use this directly to verify
// command discovery without invoking os.Exit.
func NewRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "agentreceipt",
		Short:         "Create local, verifiable receipts for AI-assisted code changes",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			_, err := colorModeFromCommand(cmd)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("agentreceipt {{.Version}}\n")
	root.PersistentFlags().String("config", "", "Path to an AgentReceipt config file")
	root.PersistentFlags().String("repo", "", "Repository root to inspect; defaults to the current directory")
	root.PersistentFlags().Bool("quiet", false, "Reduce non-essential terminal output")
	root.PersistentFlags().String("color", colorModeAuto, "Colorize terminal output: auto, always, or never")

	root.AddCommand(
		newInitCommand(),
		newInstallCommand(),
		newStartCommand(),
		newStatusCommand(),
		newSessionsCommand(),
		newEventsCommand(),
		newDeprecatedLiveCommand(),
		newStopCommand(),
		newReviewCommand(),
		newReplayCommand(),
		newFocusCommand(),
		newSchemaCommand(),
		newVerifyCommand(),
		newExportCommand(),
		newImportCommand(),
		newInspectCommand(),
		newMarkCommand(),
		newPRCommand(),
		newVersionCommand(version),
		newInternalClaudeHookCommand(),
		newInternalFilesystemWatcherCommand(),
	)

	return root
}

func colorModeFromCommand(cmd *cobra.Command) (string, error) {
	flags := cmd.Root().PersistentFlags()
	if flags.Lookup("color") == nil {
		return colorModeAuto, nil
	}
	value, err := flags.GetString("color")
	if err != nil {
		return "", err
	}
	if err := validateColorMode(value); err != nil {
		return "", err
	}

	return value, nil
}

func validateColorMode(value string) error {
	switch value {
	case colorModeAuto, colorModeAlways, colorModeNever:
		return nil
	default:
		return fmt.Errorf("--color must be one of auto, always, or never")
	}
}

func colorOutputEnabled(mode string, out io.Writer) bool {
	switch mode {
	case colorModeAlways:
		return true
	case colorModeNever:
		return false
	default:
		file, ok := out.(*os.File)
		if !ok {
			return false
		}
		info, err := file.Stat()
		if err != nil {
			return false
		}

		return info.Mode()&os.ModeCharDevice != 0
	}
}

const rootLong = `AgentReceipt records local evidence beside normal AI coding sessions and produces signed review receipts.

Core commands:
  init
  install codex
  install claude
  start
  status
  sessions
  events
  stop
  review
  focus
  replay
  schema replay
  schema focus
  verify
  export
  import codex-jsonl
  inspect codex
  mark
  pr comment`

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Bootstrap global AgentReceipt storage and signing keys",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := storage.DefaultRoot()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(root, 0o750); err != nil {
				return fmt.Errorf("create global AgentReceipt storage: %w", err)
			}
			keypair, err := signing.LoadOrCreateDefault("")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Initialized global AgentReceipt storage\nHome: %s\nSigning key: %s\n", root, keypair.Public)

			return err
		},
	}
}

func newInstallCommand() *cobra.Command {
	install := &cobra.Command{
		Use:   "install",
		Short: "Configure provider-specific local integrations",
	}
	install.AddCommand(
		newInstallCodexCommand(),
		newInstallClaudeCommand(),
	)

	return install
}

func newInstallCodexCommand() *cobra.Command {
	installCodex := &cobra.Command{
		Use:   "codex",
		Short: "Detect local Codex log availability",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString("home")
			if err != nil {
				return err
			}
			result := codex.Inspect(home)
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "Codex home: %s\nHome source: %s\nCandidates: %d\nWarnings: %d\n", result.CodexHome, codexHomeSource(home), len(result.Candidates), len(result.Warnings)); err != nil {
				return err
			}
			if len(result.Candidates) > 0 {
				if _, err := fmt.Fprintf(out, "Newest candidate: %s\n", result.Candidates[0].Path); err != nil {
					return err
				}
			}
			for _, warning := range result.Warnings {
				if _, err := fmt.Fprintf(out, "warning[%s]: %s\n", warning.Code, warning.Message); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintln(out, "Next: agentreceipt start --watch")

			return err
		},
	}
	installCodex.Flags().String("home", "", "Codex home directory; defaults to CODEX_HOME or ~/.codex")

	return installCodex
}

func newInstallClaudeCommand() *cobra.Command {
	var dryRun bool
	var settingsPath string
	installClaude := &cobra.Command{
		Use:   "claude",
		Short: "Install Claude Code hook integration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := buildClaudeInstallPlan(settingsPath)
			if err != nil {
				return err
			}
			if dryRun {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Claude settings: %s\nWould create or modify: %s\nHook command: %s __internal-claude-hook\nPrompt retention: disabled\nRaw tool output retention: disabled\n", plan.SettingsPath, plan.SettingsPath, plan.ExecutablePath)
				return err
			}
			result, err := applyClaudeInstallPlan(plan)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintln(out, "Claude hook integration installed."); err != nil {
				return err
			}
			if result.Changed {
				if _, err := fmt.Fprintf(out, "Modified: %s\n", plan.SettingsPath); err != nil {
					return err
				}
				if result.BackupPath != "" {
					if _, err := fmt.Fprintf(out, "Backup: %s\n", result.BackupPath); err != nil {
						return err
					}
				}
			} else if _, err := fmt.Fprintf(out, "Unchanged: %s\n", plan.SettingsPath); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Hook command: %s __internal-claude-hook\nPrompt retention: disabled\nRaw tool output retention: disabled\n", plan.ExecutablePath)
			return err
		},
	}
	installClaude.Flags().BoolVar(&dryRun, "dry-run", false, "Show Claude hook settings changes without writing")
	installClaude.Flags().StringVar(&settingsPath, "settings", "", "Claude settings JSON path; defaults to CLAUDE_HOME/settings.json or ~/.claude/settings.json")

	return installClaude
}

type claudeInstallPlan struct {
	SettingsPath   string
	ExecutablePath string
	Settings       map[string]any
	Changed        bool
	ExistingData   []byte
}

type claudeInstallResult struct {
	Changed    bool
	BackupPath string
}

func buildClaudeInstallPlan(settingsPath string) (claudeInstallPlan, error) {
	if settingsPath == "" {
		defaultPath, err := defaultClaudeSettingsPath()
		if err != nil {
			return claudeInstallPlan{}, err
		}
		settingsPath = defaultPath
	}
	absSettingsPath, err := filepath.Abs(settingsPath)
	if err != nil {
		return claudeInstallPlan{}, err
	}
	executablePath, err := currentExecutablePath()
	if err != nil {
		return claudeInstallPlan{}, err
	}
	existingData, settings, err := readClaudeSettings(absSettingsPath)
	if err != nil {
		return claudeInstallPlan{}, err
	}
	changed, err := mergeClaudeSettings(settings, executablePath)
	if err != nil {
		return claudeInstallPlan{}, err
	}

	return claudeInstallPlan{
		SettingsPath:   absSettingsPath,
		ExecutablePath: executablePath,
		Settings:       settings,
		Changed:        changed,
		ExistingData:   existingData,
	}, nil
}

func applyClaudeInstallPlan(plan claudeInstallPlan) (claudeInstallResult, error) {
	if !plan.Changed {
		return claudeInstallResult{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(plan.SettingsPath), 0o750); err != nil {
		return claudeInstallResult{}, fmt.Errorf("create Claude settings directory: %w", err)
	}
	result := claudeInstallResult{Changed: true}
	if len(plan.ExistingData) > 0 {
		result.BackupPath = plan.SettingsPath + ".agentreceipt.bak"
		if err := os.WriteFile(result.BackupPath, plan.ExistingData, 0o600); err != nil {
			return claudeInstallResult{}, fmt.Errorf("write Claude settings backup: %w", err)
		}
	}
	if err := writeClaudeSettings(plan.SettingsPath, plan.Settings); err != nil {
		return claudeInstallResult{}, err
	}

	return result, nil
}

func defaultClaudeSettingsPath() (string, error) {
	if home := os.Getenv("CLAUDE_HOME"); home != "" {
		return filepath.Join(home, "settings.json"), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve Claude settings path: %w", err)
	}

	return filepath.Join(userHome, ".claude", "settings.json"), nil
}

func currentExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("validate current executable: %w", err)
	}

	return path, nil
}

func readClaudeSettings(path string) ([]byte, map[string]any, error) {
	data, err := readFileAtPath(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, map[string]any{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read Claude settings: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, nil, fmt.Errorf("decode Claude settings: %w", err)
	}
	if settings == nil {
		settings = map[string]any{}
	}

	return data, settings, nil
}

func mergeClaudeSettings(settings map[string]any, executablePath string) (bool, error) {
	hooks := map[string]any{}
	if existing, ok := settings["hooks"]; ok {
		typed, ok := existing.(map[string]any)
		if !ok {
			return false, fmt.Errorf("claude settings hooks field is not an object; refusing to overwrite")
		}
		hooks = typed
	}
	hook := claudeHookSettings(executablePath)
	if reflect.DeepEqual(hooks["agentreceipt"], hook) {
		return false, nil
	}
	hooks["agentreceipt"] = hook
	settings["hooks"] = hooks

	return true, nil
}

func claudeHookSettings(executablePath string) map[string]any {
	return map[string]any{
		"command":     executablePath,
		"args":        []any{"__internal-claude-hook"},
		"description": "Import Claude hook evidence into the active AgentReceipt session.",
		"privacy": map[string]any{
			"store_prompts":          false,
			"store_raw_tool_outputs": false,
		},
	}
}

func writeClaudeSettings(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Claude settings: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentreceipt-claude-settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create Claude settings temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write Claude settings temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close Claude settings temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod Claude settings temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace Claude settings: %w", err)
	}

	return nil
}

func readFileAtPath(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = root.Close()
	}()

	return root.ReadFile(filepath.Base(path))
}

func openFileAtPath(path string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	file, openErr := root.Open(filepath.Base(path))
	closeErr := root.Close()
	if openErr != nil {
		return nil, openErr
	}
	if closeErr != nil {
		_ = file.Close()
		return nil, closeErr
	}

	return file, nil
}

func codexHomeSource(home string) string {
	switch {
	case home != "":
		return "explicit --home"
	case os.Getenv("CODEX_HOME") != "":
		return "CODEX_HOME"
	default:
		return "default ~/.codex"
	}
}

func newStartCommand() *cobra.Command {
	start := &cobra.Command{
		Use:   "start",
		Short: "Start a local receipt capture session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			watch, err := cmd.Flags().GetBool("watch")
			if err != nil {
				return err
			}
			state, resumed, err := startOrResumeSession(cmd.Context(), manager, watch)
			if err != nil {
				return err
			}
			if resumed {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Resumed AgentReceipt session %s\n", state.SessionID)
			} else {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Started AgentReceipt session %s\n", state.SessionID)
			}
			if err != nil {
				return err
			}
			if !watch {
				return nil
			}
			options, err := watchOptionsFromStartCommand(cmd)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Watching Codex logs. Press Ctrl-C to stop watching; the AgentReceipt session stays active until `agentreceipt stop`."); err != nil {
				return err
			}
			watchCtx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stopSignals()
			if err := watchCodex(watchCtx, cmd, manager, state, options); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Stopped watching Codex logs. Run `agentreceipt stop` to finalize the receipt.")

			return err
		},
	}
	start.Flags().Bool("watch", false, "Watch Codex session logs and import provider events into the active receipt")
	start.Flags().String("codex-home", "", "Codex home directory for --watch; defaults to CODEX_HOME or ~/.codex")
	start.Flags().Duration("watch-interval", time.Second, "Polling interval for --watch")
	start.Flags().Duration("watch-duration", 0, "Stop --watch after this duration; zero watches until interrupted")
	start.Flags().Bool("watch-existing", false, "With --watch, also import existing lines from matching Codex logs")

	return start
}

func startOrResumeSession(ctx context.Context, manager session.Manager, watch bool) (session.State, bool, error) {
	if watch {
		state, ok, err := manager.Status(ctx)
		if err != nil {
			return session.State{}, false, err
		}
		if ok {
			return state, true, nil
		}
	}
	state, err := manager.Start(ctx)
	if err != nil {
		return session.State{}, false, err
	}

	return state, false, nil
}

type startWatchOptions struct {
	CodexHome       string
	Interval        time.Duration
	Duration        time.Duration
	IncludeExisting bool
}

type watchedCodexFile struct {
	offset     int64
	lineOffset int
}

func watchOptionsFromStartCommand(cmd *cobra.Command) (startWatchOptions, error) {
	codexHome, err := cmd.Flags().GetString("codex-home")
	if err != nil {
		return startWatchOptions{}, err
	}
	interval, err := cmd.Flags().GetDuration("watch-interval")
	if err != nil {
		return startWatchOptions{}, err
	}
	if interval <= 0 {
		return startWatchOptions{}, fmt.Errorf("--watch-interval must be greater than zero")
	}
	duration, err := cmd.Flags().GetDuration("watch-duration")
	if err != nil {
		return startWatchOptions{}, err
	}
	if duration < 0 {
		return startWatchOptions{}, fmt.Errorf("--watch-duration must be zero or greater")
	}
	includeExisting, err := cmd.Flags().GetBool("watch-existing")
	if err != nil {
		return startWatchOptions{}, err
	}

	return startWatchOptions{CodexHome: codexHome, Interval: interval, Duration: duration, IncludeExisting: includeExisting}, nil
}

func watchCodex(ctx context.Context, cmd *cobra.Command, manager session.Manager, state session.State, options startWatchOptions) error {
	operationCtx := ctx
	watchCtx := ctx
	if options.Duration > 0 {
		var cancel context.CancelFunc
		watchCtx, cancel = context.WithTimeout(ctx, options.Duration)
		defer cancel()
	}
	watchStarted := time.Now()
	watched := map[string]watchedCodexFile{}
	reportedWarnings := map[string]bool{}
	out := cmd.OutOrStdout()
	colorMode, err := colorModeFromCommand(cmd)
	if err != nil {
		return err
	}
	renderer := newCodexWatchRendererWithColor(out, colorOutputEnabled(colorMode, out))
	if err := seedCodexWatchTokenBaseline(operationCtx, manager, renderer); err != nil {
		return err
	}
	initialPoll := true
	poll := func() error {
		result := codex.Inspect(options.CodexHome)
		for _, warning := range result.Warnings {
			warningKey := warning.Code + ":" + warning.Message
			if reportedWarnings[warningKey] {
				continue
			}
			reportedWarnings[warningKey] = true
			if err := renderer.PrintEvent(watchWarningEvent(warning.Code, warning.Message, "")); err != nil {
				return err
			}
		}
		selectedInitialCandidate := false
		for _, candidate := range result.Candidates {
			matches, reason := codexCandidateMatches(candidate, state.RepoRoot, watchStarted)
			if !matches {
				continue
			}
			tracked, ok := watched[candidate.Path]
			if !ok {
				if !options.IncludeExisting {
					if initialPoll {
						if selectedInitialCandidate {
							continue
						}
						selectedInitialCandidate = true
					} else if !candidate.ModTime.After(watchStarted) {
						continue
					}
				}
				tracked = watchedCodexFile{}
				if !options.IncludeExisting && candidate.ModTime.Before(watchStarted.Add(-2*time.Second)) {
					tracked.offset = candidate.Size
				}
				watched[candidate.Path] = tracked
				if err := renderer.PrintEvent(watchFileEvent(candidate.Path, reason)); err != nil {
					return err
				}
			}
			tail, err := codex.TailFile(candidate.Path, codexTailOptions(state.SessionID, state.RepoRoot, manager.Config, tracked.offset, tracked.lineOffset))
			if err != nil {
				if writeErr := renderer.PrintEvent(watchWarningEvent("tail_failed", err.Error(), candidate.Path)); writeErr != nil {
					return writeErr
				}
				continue
			}
			tracked.offset = tail.NextOffset
			tracked.lineOffset = tail.NextLineOffset
			watched[candidate.Path] = tracked
			if tail.EventCount == 0 {
				continue
			}
			if _, _, err := manager.AppendProviderEvents(operationCtx, tail.Events, codexWarnings(tail.Warnings)); err != nil {
				return err
			}
			if err := renderer.Print(tail.ParseResult); err != nil {
				return err
			}
		}
		initialPoll = false

		return nil
	}
	if err := poll(); err != nil {
		return err
	}
	ticker := time.NewTicker(options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-watchCtx.Done():
			return watchCtx.Err()
		case <-ticker.C:
			if err := poll(); err != nil {
				return err
			}
		}
	}
}

func seedCodexWatchTokenBaseline(ctx context.Context, manager session.Manager, renderer *codexWatchRenderer) error {
	events, err := manager.Live(ctx, 0)
	if err != nil {
		return err
	}
	if total, ok := lastCodexTokenTotal(events); ok {
		renderer.SeedTokenTotal(total)
	}

	return nil
}

func lastCodexTokenTotal(events []model.Event) (int, bool) {
	for index := len(events) - 1; index >= 0; index-- {
		total, ok := providerevidence.TokenTotal(events[index])
		if ok {
			return total, true
		}
	}

	return 0, false
}

func codexCandidateMatches(candidate codex.Candidate, repoRoot string, watchStarted time.Time) (bool, string) {
	cwd, ok, err := codex.SessionCWD(candidate.Path)
	if err == nil && ok {
		if samePath(cwd, repoRoot) {
			return true, "cwd " + cwd
		}

		return false, ""
	}
	if candidate.ModTime.After(watchStarted.Add(-2 * time.Second)) {
		return true, "new log without cwd metadata yet"
	}

	return false, ""
}

func samePath(left string, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if resolved, err := filepath.EvalSymlinks(left); err == nil {
		left = resolved
	}
	if resolved, err := filepath.EvalSymlinks(right); err == nil {
		right = resolved
	}

	return left == right
}

func printCodexLiveResult(cmd *cobra.Command, result codex.ParseResult) error {
	return newCodexWatchRenderer(cmd.OutOrStdout()).Print(result)
}

func emptyDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func truncate(value string, max int) string {
	if max > 0 && len(value) > max {
		return value[:max]
	}

	return value
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active session health and event counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			state, ok, err := manager.Status(cmd.Context())
			if err != nil {
				return err
			}
			if !ok {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No active AgentReceipt session.")
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), session.FormatStatus(state))

			return err
		},
	}
}

func newSessionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List sessions for the current repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			summaries, err := manager.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(summaries) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No AgentReceipt sessions found for this repository.")
				return err
			}

			return printSessionSummaries(cmd.OutOrStdout(), summaries)
		},
	}
}

func printSessionSummaries(out io.Writer, summaries []session.SessionSummary) error {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "SESSION\tSTATE\tACTIVE\tUPDATED\tEVENTS\tWARNINGS"); err != nil {
		return err
	}
	for _, summary := range summaries {
		active := ""
		if summary.Active {
			active = "*"
		}
		updated := "unknown"
		if !summary.UpdatedAt.IsZero() {
			updated = summary.UpdatedAt.Local().Format("2006-01-02 15:04:05")
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%d\n", summary.SessionID, summary.State, active, updated, summary.EventCount, summary.Warnings); err != nil {
			return err
		}
	}

	return writer.Flush()
}

func newEventsCommand() *cobra.Command {
	events := &cobra.Command{
		Use:   "events",
		Short: "Show recent session events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEventsCommand(cmd)
		},
	}
	addEventsFlags(events)

	return events
}

func newDeprecatedLiveCommand() *cobra.Command {
	live := &cobra.Command{
		Use:    "live",
		Short:  "Deprecated alias for events",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "Warning: `agentreceipt live` is deprecated; use `agentreceipt events` instead."); err != nil {
				return err
			}
			return runEventsCommand(cmd)
		},
	}
	addEventsFlags(live)

	return live
}

func addEventsFlags(cmd *cobra.Command) {
	cmd.Flags().Int("limit", 20, "Maximum number of recent events to print")
	cmd.Flags().String("format", eventsFormatText, "Output format: text, json, or jsonl")
}

func runEventsCommand(cmd *cobra.Command) error {
	manager, err := managerFromCommand(cmd)
	if err != nil {
		return err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return err
	}
	events, err := manager.Live(cmd.Context(), limit)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No active AgentReceipt session.")
		return err
	}
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	switch format {
	case eventsFormatText:
		colorMode, err := colorModeFromCommand(cmd)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		_, err = fmt.Fprint(out, renderEventsTerminal(events, colorOutputEnabled(colorMode, out)))
		return err
	case eventsFormatJSON:
		return printEventsJSON(cmd.OutOrStdout(), events)
	case eventsFormatJSONL:
		return printEventsJSONL(cmd.OutOrStdout(), events)
	default:
		return fmt.Errorf("--format must be one of text, json, or jsonl")
	}
}

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Finalize the active capture session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			state, ok, err := manager.Stop(cmd.Context())
			if err != nil {
				return err
			}
			if !ok {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No active AgentReceipt session.")
				return err
			}
			if _, err := receipt.Finalize(cmd.Context(), receipt.Options{RepoPath: state.RepoRoot, SessionID: state.SessionID, Config: manager.Config}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Finalized AgentReceipt session %s\n", state.SessionID)

			return err
		},
	}
}

func newReviewCommand() *cobra.Command {
	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Build a reviewer-focused receipt summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := reviewOptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			codexJSONL, err := cmd.Flags().GetString("codex-jsonl")
			if err != nil {
				return err
			}
			if codexJSONL != "" {
				if err := importCodexJSONLForReview(cmd.Context(), cmd, codexJSONL); err != nil {
					return err
				}
			}
			report, err := review.Build(cmd.Context(), options)
			if err != nil {
				return err
			}
			asJSON, err := cmd.Flags().GetBool("json")
			if err != nil {
				return err
			}
			asMarkdown, err := cmd.Flags().GetBool("md")
			if err != nil {
				return err
			}
			asPR, err := cmd.Flags().GetBool("pr")
			if err != nil {
				return err
			}
			switch {
			case asJSON:
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			case asMarkdown || asPR:
				_, err := fmt.Fprint(cmd.OutOrStdout(), review.RenderMarkdown(report))
				return err
			default:
				out := cmd.OutOrStdout()
				colorMode, err := colorModeFromCommand(cmd)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(out, review.RenderTerminalColor(report, colorOutputEnabled(colorMode, out)))
				return err
			}
		},
	}
	reviewCmd.Flags().Bool("last", false, "Review the most recent finalized session")
	reviewCmd.Flags().String("session", "", "Review a specific session ID")
	reviewCmd.Flags().Bool("security", false, "Focus output on security-sensitive evidence")
	reviewCmd.Flags().Bool("diff", false, "Include diff-focused receipt details")
	reviewCmd.Flags().Bool("json", false, "Render review output as JSON")
	reviewCmd.Flags().Bool("md", false, "Render review output as Markdown")
	reviewCmd.Flags().Bool("pr", false, "Render concise PR-comment Markdown")
	reviewCmd.Flags().String("codex-jsonl", "", "Import a Codex JSONL trace before building the review")

	return reviewCmd
}

func newReplayCommand() *cobra.Command {
	replayCmd := &cobra.Command{
		Use:   "replay",
		Short: "Build a verifier-facing replay report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := replayOptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			var report replay.Report
			if options.BundleDir != "" {
				report, err = replay.WriteBundle(cmd.Context(), options)
			} else {
				report, err = replay.Build(cmd.Context(), options)
			}
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")

			return encoder.Encode(report)
		},
	}
	replayCmd.Flags().String("session", "", "Replay a specific finalized session ID")
	replayCmd.Flags().String("bundle", "", "Write replay artifacts to a bundle directory")
	replayCmd.Flags().Bool("json", false, "Render replay output as JSON")
	replayCmd.Flags().StringArray("trusted-signer-key-id", nil, "Trusted signer key ID to apply when evaluating replay authenticity")

	return replayCmd
}

func newFocusCommand() *cobra.Command {
	focusCmd := &cobra.Command{
		Use:   "focus",
		Short: "Build a compact reviewer-agent focus report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := focusOptionsFromCommand(cmd)
			if err != nil {
				return invalidInputError(err)
			}
			asJSON, err := cmd.Flags().GetBool("json")
			if err != nil {
				return invalidInputError(err)
			}
			if !asJSON {
				return invalidInputError(fmt.Errorf("focus command requires --json"))
			}

			var report replay.Report
			switch {
			case options.ReplayPath != "":
				report, err = readReplayJSON(options.ReplayPath)
				if err != nil {
					return invalidInputError(err)
				}
			default:
				replayOptions := replay.Options{
					RepoPath:            options.RepoPath,
					SessionID:           options.SessionID,
					TrustedSignerKeyIDs: options.TrustedSignerKeyIDs,
				}
				report, err = replay.Build(cmd.Context(), replayOptions)
				if err != nil {
					return err
				}
			}
			focusReport := replay.BuildFocusReport(report)
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(focusReport); err != nil {
				return err
			}
			if code := focusExitCodeFromReport(report, focusReport); code != exitCodePass {
				return &ExitError{
					Code: code,
					Err:  fmt.Errorf("focus report requires review action: %s", focusReport.Verdict),
				}
			}

			return nil
		},
	}
	focusCmd.Flags().String("session", "", "Build focus from a specific finalized session ID")
	focusCmd.Flags().String("replay", "", "Build focus from a replay.json file")
	focusCmd.Flags().Bool("json", false, "Render focus output as JSON")
	focusCmd.Flags().StringArray("trusted-signer-key-id", nil, "Trusted signer key ID to apply when evaluating replay authenticity")

	return focusCmd
}

func newSchemaCommand() *cobra.Command {
	schema := &cobra.Command{
		Use:   "schema",
		Short: "Print schema artifacts for machine consumers",
	}
	schema.AddCommand(
		newSchemaReplayCommand(),
		newSchemaFocusCommand(),
	)

	return schema
}

func newSchemaReplayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "replay",
		Short: "Print the replay JSON schema",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := schemaPayload("replay")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(payload)
			return err
		},
	}
}

func newSchemaFocusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "focus",
		Short: "Print the focus JSON schema",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := schemaPayload("focus")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(payload)
			return err
		},
	}
}

func newVerifyCommand() *cobra.Command {
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Verify receipt integrity and signatures",
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := receiptOptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			result, err := receipt.Verify(cmd.Context(), options)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), receipt.RenderVerify(result)); err != nil {
				return err
			}
			if !result.Valid {
				return fmt.Errorf("receipt verification failed")
			}

			return nil
		},
	}
	verify.Flags().String("session", "", "Verify a specific session ID")
	verify.AddCommand(newVerifyBundleCommand())
	verify.AddCommand(newVerifyDiffCommand())

	return verify
}

func newVerifyDiffCommand() *cobra.Command {
	diff := &cobra.Command{
		Use:   "diff",
		Short: "Compare a finalized receipt patch against a candidate patch",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, exitCode, err := buildVerifyDiffResult(cmd)
			if err != nil {
				if exitCode == exitCodeInvalidInput {
					return invalidInputError(err)
				}
				return &ExitError{
					Code: exitCode,
					Err:  err,
				}
			}
			asJSON, err := cmd.Flags().GetBool("json")
			if err != nil {
				return invalidInputError(err)
			}
			if asJSON {
				data, marshalErr := json.MarshalIndent(r, "", "  ")
				if marshalErr != nil {
					return marshalErr
				}
				if _, writeErr := fmt.Fprint(cmd.OutOrStdout(), string(data)); writeErr != nil {
					return writeErr
				}
			} else {
				if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "equivalent=%t\nreason=%s\nagainst=%s\n", r.Equivalent, r.Reason, r.Against); writeErr != nil {
					return writeErr
				}
			}
			if exitCode == exitCodePass {
				return nil
			}

			return &ExitError{
				Code: exitCode,
				Err:  fmt.Errorf("verify diff command exit code: %d", exitCode),
			}
		},
	}
	diff.Flags().String("session", "", "Session ID for local verification")
	diff.Flags().String("bundle", "", "Portable receipt bundle path for local verification")
	diff.Flags().String("against", "", "Candidate patch target: HEAD, merge-base, patch:<path>, or pr.patch")
	diff.Flags().Bool("json", false, "Render JSON output")

	return diff
}

func newVerifyBundleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "bundle <path>",
		Short: "Verify a local AgentReceipt artifact bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := receipt.VerifyBundle(args[0])
			if err != nil {
				return err
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), receipt.RenderVerify(result)); err != nil {
				return err
			}
			if !result.Valid {
				return fmt.Errorf("receipt verification failed")
			}

			return nil
		},
	}
}

type verifyDiffReport struct {
	Equivalent         bool     `json:"equivalent"`
	Against            string   `json:"against"`
	FinalPatchHash     string   `json:"final_patch_hash"`
	CandidatePatchHash string   `json:"candidate_patch_hash"`
	Reason             string   `json:"reason"`
	EvidenceRefs       []string `json:"evidence_refs"`
}

type verifyDiffOptions struct {
	SessionID  string
	BundlePath string
	Against    string
	RepoRoot   string
}

func buildVerifyDiffResult(cmd *cobra.Command) (verifyDiffReport, int, error) {
	options, err := verifyDiffOptionsFromCommand(cmd)
	if err != nil {
		return verifyDiffReport{}, exitCodeInvalidInput, err
	}

	finalPatchPath, evidenceRefs, verifyCode, err := resolveVerifyDiffFinalPatch(cmd.Context(), options)
	if err != nil {
		return verifyDiffReport{}, verifyCode, err
	}
	if verifyCode != exitCodePass {
		return verifyDiffReport{
			Equivalent:   false,
			Reason:       "receipt integrity verification failed",
			EvidenceRefs: evidenceRefs,
		}, verifyCode, nil
	}

	repoRoot := options.RepoRoot

	finalPatch, err := readFileByPath(finalPatchPath)
	if err != nil {
		return verifyDiffReport{}, exitCodeInvalidInput, err
	}
	candidatePatch, _, candidateEvidenceRefs, candidateLabel, patchErr := verifyDiffCandidate(cmd.Context(), verifyDiffCandidateInput{
		Options:  options,
		RepoRoot: repoRoot,
	})
	if patchErr.err != nil {
		return verifyDiffReport{}, patchErr.code, patchErr.err
	}
	finalNormalized := normalizeDiffPatch(finalPatch)
	candidateNormalized := normalizeDiffPatch(candidatePatch)
	equivalent := string(finalNormalized) == string(candidateNormalized)
	evidence := append(append([]string{}, evidenceRefs...), candidateEvidenceRefs...)
	report := verifyDiffReport{
		Equivalent:         equivalent,
		Against:            candidateLabel,
		FinalPatchHash:     hashBytes(finalNormalized),
		CandidatePatchHash: hashBytes(candidateNormalized),
		EvidenceRefs:       evidence,
	}
	if equivalent {
		report.Reason = "final patch is equivalent to candidate"
		return report, exitCodePass, nil
	}
	report.Reason = "final patch does not match candidate"
	return report, exitCodePatchMismatch, nil
}

type verifyDiffCandidateInput struct {
	Options  verifyDiffOptions
	RepoRoot string
}

type verifyDiffCandidateError struct {
	code int
	err  error
}

func verifyDiffCandidate(ctx context.Context, input verifyDiffCandidateInput) ([]byte, string, []string, string, *verifyDiffCandidateError) {
	against := input.Options.Against
	if against == "" {
		err := fmt.Errorf("--against is required")
		return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
	}
	against = strings.TrimSpace(against)
	if against == "HEAD" {
		if input.RepoRoot == "" {
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: fmt.Errorf("--against HEAD requires --session and --repo")}
		}
		patch, err := gitDiffBinary(ctx, input.RepoRoot, "HEAD")
		if err != nil {
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
		}
		return patch, hashDiff(patch), []string{"git diff --binary HEAD"}, "HEAD", &verifyDiffCandidateError{}
	}
	if against == "merge-base" {
		base, err := detectMergeBaseRef(ctx, input.RepoRoot)
		if err != nil {
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
		}
		revision := base + "...HEAD"
		patch, diffErr := gitDiffBinary(ctx, input.RepoRoot, revision)
		if diffErr != nil {
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: diffErr}
		}
		evidence := []string{"git diff --binary " + revision}
		return patch, hashDiff(patch), evidence, "merge-base:" + base, &verifyDiffCandidateError{}
	}
	if strings.HasPrefix(against, "patch:") {
		path := strings.TrimPrefix(against, "patch:")
		if path == "" {
			err := fmt.Errorf("--against patch: path is required")
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
		}
		if !filepath.IsAbs(path) {
			if input.RepoRoot != "" {
				path = filepath.Join(input.RepoRoot, path)
			}
		}
		patch, err := readFileByPath(path)
		if err != nil {
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
		}
		return patch, hashDiff(patch), []string{"file://" + path}, "patch:" + path, &verifyDiffCandidateError{}
	}
	if against == "pr.patch" {
		basePath := input.RepoRoot
		if basePath == "" {
			basePath = input.Options.BundlePath
		}
		path := filepath.Join(basePath, "pr.patch")
		patch, err := readFileByPath(path)
		if err != nil {
			return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
		}
		return patch, hashDiff(patch), []string{"file://" + path}, "pr.patch", &verifyDiffCandidateError{}
	}

	err := fmt.Errorf("unsupported --against value %q", against)
	return nil, "", nil, "", &verifyDiffCandidateError{code: exitCodeInvalidInput, err: err}
}

func resolveVerifyDiffFinalPatch(ctx context.Context, options verifyDiffOptions) (string, []string, int, error) {
	if options.BundlePath != "" {
		result, err := receipt.VerifyBundle(options.BundlePath)
		if err != nil {
			return "", []string{"diffs/final.patch"}, exitCodeInvalidInput, err
		}
		if !result.Valid {
			return "", []string{"diffs/final.patch"}, exitCodeIntegrity, nil
		}

		finalPatchPath := filepath.Join(options.BundlePath, storage.DiffsDir, storage.FinalPatchFile)
		return finalPatchPath, []string{"diffs/final.patch"}, exitCodePass, nil
	}

	result, err := receipt.Verify(ctx, receipt.Options{RepoPath: options.RepoRoot, SessionID: options.SessionID, Last: false})
	if err != nil {
		return "", []string{"diffs/final.patch"}, exitCodeInvalidInput, err
	}
	layout, err := storage.NewLayout(options.RepoRoot, options.SessionID)
	if err != nil {
		return "", []string{"diffs/final.patch"}, exitCodeInvalidInput, err
	}
	if !result.Valid {
		return layout.FinalPatch, []string{"diffs/final.patch"}, exitCodeIntegrity, nil
	}

	return layout.FinalPatch, []string{"diffs/final.patch"}, exitCodePass, nil
}

func verifyDiffOptionsFromCommand(cmd *cobra.Command) (verifyDiffOptions, error) {
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return verifyDiffOptions{}, err
	}
	bundlePath, err := cmd.Flags().GetString("bundle")
	if err != nil {
		return verifyDiffOptions{}, err
	}
	against, err := cmd.Flags().GetString("against")
	if err != nil {
		return verifyDiffOptions{}, err
	}
	if sessionID == "" && bundlePath == "" {
		return verifyDiffOptions{}, fmt.Errorf("provide exactly one of --session or --bundle")
	}
	if sessionID != "" && bundlePath != "" {
		return verifyDiffOptions{}, fmt.Errorf("provide only one of --session or --bundle")
	}
	if sessionID != "" {
		if err := storage.ValidateSessionID(sessionID); err != nil {
			return verifyDiffOptions{}, err
		}
	}
	if against == "" {
		return verifyDiffOptions{}, fmt.Errorf("--against is required")
	}
	repoRoot := ""
	if sessionID != "" {
		r, err := repoRootFromCommand(cmd)
		if err != nil {
			return verifyDiffOptions{}, err
		}
		repoRoot = r
	}

	return verifyDiffOptions{
		SessionID:  sessionID,
		BundlePath: bundlePath,
		Against:    against,
		RepoRoot:   repoRoot,
	}, nil
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

func isSafeGitDiffRevision(revision string) bool {
	if revision == "HEAD" {
		return true
	}
	base, ok := strings.CutSuffix(revision, "...HEAD")
	return ok && isSafeGitBaseRef(base)
}

func normalizeDiffPatch(data []byte) []byte {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return []byte(strings.TrimRight(normalized, "\n"))
}

func hashDiff(data []byte) string {
	normalized := normalizeDiffPatch(data)
	return hashBytes(normalized)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func readFileByPath(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	return root.ReadFile(filepath.Base(path))
}

func gitDiffBinary(ctx context.Context, repoRoot string, revision string) ([]byte, error) {
	if !isSafeGitDiffRevision(revision) {
		return nil, fmt.Errorf("unsupported git diff revision %q", revision)
	}
	// #nosec G204 -- revision is validated and passed as a single git argument without a shell.
	cmd := exec.CommandContext(ctx, "git", "diff", "--binary", revision)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff --binary %s: %w: %s", revision, err, strings.TrimSpace(string(output)))
	}

	return output, nil
}

func gitCommandOutput(cmd *exec.Cmd, description string) (string, error) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", description, err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

func detectMergeBaseRef(ctx context.Context, repoRoot string) (string, error) {
	if repoRoot == "" {
		return "", fmt.Errorf("--against merge-base requires --session and --repo")
	}
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
			return candidate, nil
		}
	}

	return "", fmt.Errorf("failed to determine merge-base reference")
}

func gitCurrentUpstream(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	cmd.Dir = dir
	output, err := gitCommandOutput(cmd, "git rev-parse --abbrev-ref --symbolic-full-name @{upstream}")
	if err != nil {
		return "", err
	}
	upstream := strings.TrimSpace(output)
	if upstream == "" {
		return "", fmt.Errorf("upstream is not configured")
	}

	return upstream, nil
}

func gitOriginHead(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = dir
	output, err := gitCommandOutput(cmd, "git symbolic-ref --quiet --short refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(output)
	if ref == "" {
		return "", fmt.Errorf("origin/HEAD is not configured")
	}

	return strings.TrimPrefix(ref, "origin/"), nil
}

func gitVerifyBase(ctx context.Context, dir string, ref string) (string, error) {
	if !isSafeGitBaseRef(ref) {
		return "", fmt.Errorf("unsupported base ref %q", ref)
	}
	commitRef := ref + "^{commit}"
	// #nosec G204 -- ref is constrained by isSafeGitBaseRef and passed as one git argument.
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", commitRef)
	cmd.Dir = dir
	if _, err := gitCommandOutput(cmd, "git rev-parse --verify --quiet "+commitRef); err != nil {
		return "", err
	}

	return ref, nil
}

func newExportCommand() *cobra.Command {
	export := &cobra.Command{
		Use:   "export",
		Short: "Export finalized receipt artifacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := receiptOptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := exportFormatFromCommand(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			colorMode, err := colorModeFromCommand(cmd)
			if err != nil {
				return err
			}
			data, err := receipt.ExportWithColor(cmd.Context(), options, format, exportColorEnabled(format, colorMode, out))
			if err != nil {
				return err
			}
			_, err = out.Write(data)

			return err
		},
	}
	export.Flags().Bool("json", false, "Export receipt as JSON")
	export.Flags().Bool("md", false, "Export receipt as Markdown")
	export.Flags().Bool("pr", false, "Export concise PR-comment Markdown")
	export.Flags().String("session", "", "Export a specific session ID")

	return export
}

func exportColorEnabled(format string, mode string, out io.Writer) bool {
	if format == "json" || format == "pr" {
		return false
	}

	return colorOutputEnabled(mode, out)
}

func newImportCommand() *cobra.Command {
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import provider evidence into a session",
	}
	importCmd.AddCommand(newCodexJSONLCommand())

	return importCmd
}

func newCodexJSONLCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "codex-jsonl <path>",
		Short: "Import a Codex JSONL trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			state, active, err := manager.Status(cmd.Context())
			if err != nil {
				return err
			}
			sessionID := "ar_ses_preview"
			cwd := ""
			if active {
				sessionID = state.SessionID
				cwd = state.RepoRoot
			}
			result, err := codex.ParseFile(args[0], codexParseOptions(sessionID, cwd, manager.Config))
			if err != nil {
				return err
			}
			if active {
				layout, err := storage.NewLayout(state.RepoRoot, state.SessionID)
				if err != nil {
					return err
				}
				if err := codex.WriteTraces(layout, result); err != nil {
					return err
				}
				if _, _, err := manager.AppendProviderEvents(cmd.Context(), result.Events, codexWarnings(result.Warnings)); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Imported Codex JSONL: events=%d commands=%d warnings=%d active_session=%t\n", result.EventCount, result.CommandCount, result.WarningCount, active)

			return err
		},
	}
}

func newInspectCommand() *cobra.Command {
	inspect := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect local provider evidence sources",
	}
	inspect.AddCommand(newInspectCodexCommand())

	return inspect
}

func newInspectCodexCommand() *cobra.Command {
	inspectCodex := &cobra.Command{
		Use:   "codex",
		Short: "Inspect local Codex evidence availability",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString("home")
			if err != nil {
				return err
			}
			last, err := cmd.Flags().GetBool("last")
			if err != nil {
				return err
			}
			result := codex.Inspect(home)
			candidates := result.Candidates
			if last && len(candidates) > 1 {
				candidates = candidates[:1]
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Codex home: %s\nCandidates: %d\nWarnings: %d\n", result.CodexHome, len(candidates), len(result.Warnings))
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", candidate.Path); err != nil {
					return err
				}
			}
			for _, warning := range result.Warnings {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "warning[%s]: %s\n", warning.Code, warning.Message); err != nil {
					return err
				}
			}

			return nil
		},
	}
	inspectCodex.Flags().Bool("last", false, "Inspect the most recent Codex session candidate")
	inspectCodex.Flags().String("home", "", "Codex home directory; defaults to CODEX_HOME or ~/.codex")

	return inspectCodex
}

func newMarkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mark <message>",
		Short: "Add a human context marker to the active session",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			message := strings.Join(args, " ")
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			state, ok, err := manager.Mark(cmd.Context(), message, "")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no active AgentReceipt session")
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Marked AgentReceipt session %s: %s\n", state.SessionID, message)
			return err
		},
	}
}

func newPRCommand() *cobra.Command {
	pr := &cobra.Command{
		Use:   "pr",
		Short: "Work with pull request receipt output",
	}
	pr.AddCommand(newPRCommentCommand())

	return pr
}

func newPRCommentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "comment",
		Short: "Post receipt Markdown to the current pull request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := exec.LookPath("gh"); err != nil {
				return fmt.Errorf("GitHub CLI gh is required for pr comment: %w", err)
			}
			repoRoot, err := repoRootFromCommand(cmd)
			if err != nil {
				return err
			}
			if output, err := runGitHubPRView(cmd, repoRoot); err != nil {
				return fmt.Errorf("no current pull request detected: %w: %s", err, strings.TrimSpace(output))
			}
			data, err := receipt.Export(cmd.Context(), receipt.Options{RepoPath: repoRoot, Last: true}, "pr")
			if err != nil {
				return err
			}
			if output, err := runGitHubPRComment(cmd, repoRoot, data); err != nil {
				return fmt.Errorf("gh pr comment failed: %w: %s", err, strings.TrimSpace(output))
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Posted AgentReceipt PR comment.")

			return err
		},
	}
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the AgentReceipt version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "agentreceipt %s\n", version)
			return err
		},
	}
}

func newInternalClaudeHookCommand() *cobra.Command {
	internalCmd := &cobra.Command{
		Use:    "__internal-claude-hook",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			state, active, err := manager.Status(cmd.Context())
			if err != nil {
				return err
			}
			if !active {
				return fmt.Errorf("no active AgentReceipt session")
			}
			filePath, err := cmd.Flags().GetString("file")
			if err != nil {
				return err
			}
			reader := cmd.InOrStdin()
			if filePath != "" {
				file, err := openFileAtPath(filePath)
				if err != nil {
					return fmt.Errorf("open Claude hook record: %w", err)
				}
				defer func() {
					_ = file.Close()
				}()
				reader = file
			}
			result := claude.ParseReader(reader, claudeParseOptions(state.SessionID, state.RepoRoot, manager.Config))
			if _, _, err := manager.AppendProviderEvents(cmd.Context(), result.Events, result.Warnings); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Imported Claude hook: events=%d commands=%d warnings=%d\n", result.EventCount, result.CommandCount, result.WarningCount)

			return err
		},
	}
	internalCmd.Flags().String("file", "", "Read Claude hook JSON from a file instead of stdin")

	return internalCmd
}

func newInternalFilesystemWatcherCommand() *cobra.Command {
	internalCmd := &cobra.Command{
		Use:    "__internal-fswatcher",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repoRoot, err := repoRootFromCommand(cmd)
			if err != nil {
				return err
			}
			sessionID, err := cmd.Flags().GetString("session")
			if err != nil {
				return err
			}
			watcherNonce, err := cmd.Flags().GetString("watcher-nonce")
			if err != nil {
				return err
			}
			configJSON, err := cmd.Flags().GetString("config-json")
			if err != nil {
				return err
			}
			cfg := config.Default()
			if configJSON != "" {
				if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
					return fmt.Errorf("decode filesystem watcher config: %w", err)
				}
				if err := config.Validate(cfg); err != nil {
					return err
				}
			}

			return session.RunFilesystemWatcher(cmd.Context(), session.FilesystemWatcherOptions{
				RepoRoot:  repoRoot,
				SessionID: sessionID,
				Config:    cfg,
				Nonce:     watcherNonce,
			})
		},
	}
	internalCmd.Flags().String("session", "", "AgentReceipt session ID")
	internalCmd.Flags().String("watcher-nonce", "", "AgentReceipt filesystem watcher identity nonce")
	internalCmd.Flags().String("config-json", "", "Serialized AgentReceipt config")
	_ = internalCmd.MarkFlagRequired("session")

	return internalCmd
}

func managerFromCommand(cmd *cobra.Command) (session.Manager, error) {
	repoPath, err := cmd.Root().PersistentFlags().GetString("repo")
	if err != nil {
		return session.Manager{}, err
	}
	cfg, err := configFromCommand(cmd)
	if err != nil {
		return session.Manager{}, err
	}

	return session.Manager{RepoPath: repoPath, Config: cfg}, nil
}

func configFromCommand(cmd *cobra.Command) (config.Config, error) {
	configPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return config.Config{}, err
	}
	cfg := config.Default()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return config.Config{}, err
		}
		cfg = loaded
	}

	return cfg, nil
}

func codexWarnings(warnings []codex.ParseWarning) []model.Warning {
	converted := make([]model.Warning, 0, len(warnings))
	for _, warning := range warnings {
		converted = append(converted, model.Warning{
			Code:    "codex_" + warning.Code,
			Message: warning.Message,
		})
	}

	return converted
}

func codexParseOptions(sessionID string, cwd string, cfg config.Config) codex.ParseOptions {
	return codex.ParseOptions{
		SessionID:           sessionID,
		CWD:                 cwd,
		MaxOutputBytes:      cfg.Privacy.MaxBlobBytes,
		RedactSecrets:       cfg.Privacy.RedactSecrets,
		RedactSecretsSet:    true,
		StorePrompts:        cfg.Privacy.StorePrompts,
		StoreRawToolOutputs: cfg.Privacy.StoreRawToolOutputs,
	}
}

func codexTailOptions(sessionID string, cwd string, cfg config.Config, offset int64, lineOffset int) codex.TailOptions {
	return codex.TailOptions{
		SessionID:           sessionID,
		CWD:                 cwd,
		Offset:              offset,
		LineOffset:          lineOffset,
		MaxOutputBytes:      cfg.Privacy.MaxBlobBytes,
		RedactSecrets:       cfg.Privacy.RedactSecrets,
		RedactSecretsSet:    true,
		StorePrompts:        cfg.Privacy.StorePrompts,
		StoreRawToolOutputs: cfg.Privacy.StoreRawToolOutputs,
	}
}

func claudeParseOptions(sessionID string, cwd string, cfg config.Config) claude.ParseOptions {
	return claude.ParseOptions{
		SessionID:           sessionID,
		CWD:                 cwd,
		MaxOutputBytes:      cfg.Privacy.MaxBlobBytes,
		RedactSecrets:       cfg.Privacy.RedactSecrets,
		RedactSecretsSet:    true,
		StorePrompts:        cfg.Privacy.StorePrompts,
		StoreRawToolOutputs: cfg.Privacy.StoreRawToolOutputs,
	}
}

func importCodexJSONLForReview(ctx context.Context, cmd *cobra.Command, path string) error {
	manager, err := managerFromCommand(cmd)
	if err != nil {
		return err
	}
	state, active, err := manager.Status(ctx)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("review --codex-jsonl requires an active AgentReceipt session")
	}
	result, err := codex.ParseFile(path, codexParseOptions(state.SessionID, state.RepoRoot, manager.Config))
	if err != nil {
		return err
	}
	layout, err := storage.NewLayout(state.RepoRoot, state.SessionID)
	if err != nil {
		return err
	}
	if err := codex.WriteTraces(layout, result); err != nil {
		return err
	}
	_, _, err = manager.AppendProviderEvents(ctx, result.Events, codexWarnings(result.Warnings))
	return err
}

func reviewOptionsFromCommand(cmd *cobra.Command) (review.Options, error) {
	repoPath, err := cmd.Root().PersistentFlags().GetString("repo")
	if err != nil {
		return review.Options{}, err
	}
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return review.Options{}, err
	}
	last, err := cmd.Flags().GetBool("last")
	if err != nil {
		return review.Options{}, err
	}
	security, err := cmd.Flags().GetBool("security")
	if err != nil {
		return review.Options{}, err
	}
	diff, err := cmd.Flags().GetBool("diff")
	if err != nil {
		return review.Options{}, err
	}
	cfg, err := configFromCommand(cmd)
	if err != nil {
		return review.Options{}, err
	}

	return review.Options{RepoPath: repoPath, SessionID: sessionID, Last: last, Security: security, Diff: diff, Config: cfg}, nil
}

func replayOptionsFromCommand(cmd *cobra.Command) (replay.Options, error) {
	repoPath, err := repoRootFromCommand(cmd)
	if err != nil {
		return replay.Options{}, err
	}
	cfg, err := configFromCommand(cmd)
	if err != nil {
		return replay.Options{}, err
	}
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return replay.Options{}, err
	}
	if sessionID == "" {
		return replay.Options{}, errors.New("replay requires --session")
	}
	if err := storage.ValidateSessionID(sessionID); err != nil {
		return replay.Options{}, err
	}
	bundleDir, err := cmd.Flags().GetString("bundle")
	if err != nil {
		return replay.Options{}, err
	}
	trustedSignerKeyIDs, err := cmd.Flags().GetStringArray("trusted-signer-key-id")
	if err != nil {
		return replay.Options{}, err
	}
	if len(cfg.Trust.TrustedSignerKeyIDs) > 0 {
		trustedSignerKeyIDs = append(cfg.Trust.TrustedSignerKeyIDs, trustedSignerKeyIDs...)
	}
	normalizedTrustedSignerKeyIDs, err := trust.NormalizeTrustedSignerKeyIDs(trustedSignerKeyIDs)
	if err != nil {
		return replay.Options{}, err
	}

	return replay.Options{RepoPath: repoPath, SessionID: sessionID, BundleDir: bundleDir, TrustedSignerKeyIDs: normalizedTrustedSignerKeyIDs}, nil
}

type focusOptions struct {
	RepoPath            string
	SessionID           string
	ReplayPath          string
	TrustedSignerKeyIDs []string
}

func focusOptionsFromCommand(cmd *cobra.Command) (focusOptions, error) {
	cfg, err := configFromCommand(cmd)
	if err != nil {
		return focusOptions{}, err
	}
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return focusOptions{}, err
	}
	replayPath, err := cmd.Flags().GetString("replay")
	if err != nil {
		return focusOptions{}, err
	}
	if (sessionID == "" && replayPath == "") || (sessionID != "" && replayPath != "") {
		return focusOptions{}, fmt.Errorf("provide exactly one of --session or --replay")
	}
	var repoPath string
	if sessionID != "" {
		repoPath, err = repoRootFromCommand(cmd)
		if err != nil {
			return focusOptions{}, err
		}
		if err := storage.ValidateSessionID(sessionID); err != nil {
			return focusOptions{}, err
		}
	}
	trustedSignerKeyIDs, err := cmd.Flags().GetStringArray("trusted-signer-key-id")
	if err != nil {
		return focusOptions{}, err
	}
	mergedTrustedSignerKeyIDs := trustedSignerKeyIDs
	if len(cfg.Trust.TrustedSignerKeyIDs) > 0 {
		mergedTrustedSignerKeyIDs = append(cfg.Trust.TrustedSignerKeyIDs, mergedTrustedSignerKeyIDs...)
	}
	normalizedTrustedSignerKeyIDs, err := trust.NormalizeTrustedSignerKeyIDs(mergedTrustedSignerKeyIDs)
	if err != nil {
		return focusOptions{}, err
	}

	return focusOptions{
		RepoPath:            repoPath,
		SessionID:           sessionID,
		ReplayPath:          replayPath,
		TrustedSignerKeyIDs: normalizedTrustedSignerKeyIDs,
	}, nil
}

func focusExitCodeFromReport(report replay.Report, focusReport replay.FocusReport) int {
	if !report.Verification.IntegrityValid {
		return exitCodeIntegrity
	}
	if focusReportContainsDiffMismatch(focusReport) {
		return exitCodePatchMismatch
	}
	switch focusReport.Verdict {
	case "block":
		return exitCodeBlockerEvidence
	case "unverifiable":
		return exitCodeUnverifiable
	case "review_required":
		return exitCodeReviewRequired
	default:
		return exitCodePass
	}
}

func focusReportContainsDiffMismatch(focusReport replay.FocusReport) bool {
	for _, task := range focusReport.ReviewTasks {
		if task.Kind == focusTaskKindDiffMismatch {
			return true
		}
	}
	return false
}

func readReplayJSON(path string) (replay.Report, error) {
	// #nosec G304 -- path is intentionally provided by CLI users for replay analysis.
	data, err := os.ReadFile(path)
	if err != nil {
		return replay.Report{}, fmt.Errorf("read replay file: %w", err)
	}
	var report replay.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return replay.Report{}, fmt.Errorf("decode replay JSON: %w", err)
	}

	return report, nil
}

func repoRootFromCommand(cmd *cobra.Command) (string, error) {
	repoPath, err := cmd.Root().PersistentFlags().GetString("repo")
	if err != nil {
		return "", err
	}
	if repoPath == "" {
		repoPath = "."
	}

	return gitmonitor.DiscoverRoot(cmd.Context(), repoPath)
}

func receiptOptionsFromCommand(cmd *cobra.Command) (receipt.Options, error) {
	repoPath, err := cmd.Root().PersistentFlags().GetString("repo")
	if err != nil {
		return receipt.Options{}, err
	}
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return receipt.Options{}, err
	}

	return receipt.Options{RepoPath: repoPath, SessionID: sessionID, Last: sessionID == ""}, nil
}

func exportFormatFromCommand(cmd *cobra.Command) (string, error) {
	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return "", err
	}
	asMarkdown, err := cmd.Flags().GetBool("md")
	if err != nil {
		return "", err
	}
	asPR, err := cmd.Flags().GetBool("pr")
	if err != nil {
		return "", err
	}
	selected := 0
	format := "md"
	for name, enabled := range map[string]bool{"json": asJSON, "md": asMarkdown, "pr": asPR} {
		if enabled {
			selected++
			format = name
		}
	}
	if selected > 1 {
		return "", fmt.Errorf("select only one export format")
	}

	return format, nil
}

func runGitHubPRView(cmd *cobra.Command, repoRoot string) (string, error) {
	gh := exec.CommandContext(cmd.Context(), "gh", "pr", "view", "--json", "number")
	gh.Dir = repoRoot
	output, err := gh.CombinedOutput()

	return string(output), err
}

func runGitHubPRComment(cmd *cobra.Command, repoRoot string, body []byte) (string, error) {
	gh := exec.CommandContext(cmd.Context(), "gh", "pr", "comment", "--body-file", prCommentFile)
	gh.Dir = repoRoot
	gh.Stdin = strings.NewReader(string(body))
	output, err := gh.CombinedOutput()

	return string(output), err
}

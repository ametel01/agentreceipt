package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ametel01/agentreceipt/internal/capture/gitmonitor"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/ametel01/agentreceipt/internal/receipt"
	"github.com/ametel01/agentreceipt/internal/review"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/signing"
	"github.com/ametel01/agentreceipt/internal/storage"
	"github.com/spf13/cobra"
)

const scaffoldMessage = "command scaffolded; implementation is scheduled for a later AgentReceipt plan step"
const prCommentFile = ".agentreceipt/pr-comment.md"

// Execute runs the AgentReceipt CLI.
func Execute(version string) error {
	return NewRootCommand(version).Execute()
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("agentreceipt {{.Version}}\n")
	root.PersistentFlags().String("config", "", "Path to an AgentReceipt config file")
	root.PersistentFlags().String("repo", "", "Repository root to inspect; defaults to the current directory")
	root.PersistentFlags().Bool("quiet", false, "Reduce non-essential terminal output")

	root.AddCommand(
		newInitCommand(),
		newInstallCommand(),
		newStartCommand(),
		newStatusCommand(),
		newLiveCommand(),
		newStopCommand(),
		newReviewCommand(),
		newVerifyCommand(),
		newExportCommand(),
		newImportCommand(),
		newInspectCommand(),
		newMarkCommand(),
		newPRCommand(),
		newVersionCommand(version),
	)

	return root
}

const rootLong = `AgentReceipt records local evidence beside normal AI coding sessions and produces signed review receipts.

Core commands:
  init
  install codex
  install claude
  start
  status
  live
  stop
  review
  verify
  export
  import codex-jsonl
  inspect codex
  mark
  pr comment`

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Bootstrap AgentReceipt config and local storage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			repoRoot, err := repoRootFromCommand(cmd)
			if err != nil {
				return err
			}
			configPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			if configPath == "" {
				configPath = filepath.Join(repoRoot, config.ConfigFile)
			}
			if fileExists(configPath) {
				if _, err := config.Load(configPath); err != nil {
					return err
				}
			} else if err := config.Write(configPath, config.Default()); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(repoRoot, storage.RootDir, storage.SessionsDir), 0o750); err != nil {
				return fmt.Errorf("create session storage: %w", err)
			}
			if err := writePolicyIfMissing(repoRoot); err != nil {
				return err
			}
			keypair, err := signing.LoadOrCreateDefault("")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Initialized AgentReceipt in %s\nConfig: %s\nPolicy: %s\nSigning key: %s\n", repoRoot, configPath, filepath.Join(repoRoot, config.PolicyFile), keypair.Public)

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
		newScaffoldCommand("codex", "Detect local Codex logs and configure parser defaults", "install codex will detect Codex log directories and update local parser preferences."),
		newInstallClaudeCommand(),
	)

	return install
}

func newInstallClaudeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Show deferred Claude integration status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Claude hook installation is deferred in the Codex-first MVP; no runtime hooks were configured.")
			return err
		},
	}
}

func newStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start a local receipt capture session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := managerFromCommand(cmd)
			if err != nil {
				return err
			}
			state, err := manager.Start(cmd.Context())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Started AgentReceipt session %s\n", state.SessionID)

			return err
		},
	}
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

func newLiveCommand() *cobra.Command {
	live := &cobra.Command{
		Use:   "live",
		Short: "Stream recent canonical session events",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetEscapeHTML(false)
			for _, event := range events {
				if err := encoder.Encode(event); err != nil {
					return err
				}
			}

			return nil
		},
	}
	live.Flags().Int("limit", 20, "Maximum number of recent events to print")

	return live
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
			if _, err := receipt.Finalize(cmd.Context(), receipt.Options{RepoPath: state.RepoRoot, SessionID: state.SessionID}); err != nil {
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
				_, err := fmt.Fprint(cmd.OutOrStdout(), review.RenderTerminal(report))
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
	reviewCmd.Flags().Bool("full", false, "Include expanded evidence details")
	reviewCmd.Flags().String("codex-jsonl", "", "Import a Codex JSONL trace before building the review")
	reviewCmd.Flags().String("provider", "", "Filter review output by provider")

	return reviewCmd
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

	return verify
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
			data, err := receipt.Export(cmd.Context(), options, format)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)

			return err
		},
	}
	export.Flags().Bool("json", false, "Export receipt as JSON")
	export.Flags().Bool("md", false, "Export receipt as Markdown")
	export.Flags().Bool("pr", false, "Export concise PR-comment Markdown")
	export.Flags().String("session", "", "Export a specific session ID")

	return export
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
			result, err := codex.ParseFile(args[0], codex.ParseOptions{SessionID: sessionID, CWD: cwd})
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
			if err := writeRepoRelativeFile(repoRoot, prCommentFile, data); err != nil {
				return err
			}
			defer func() {
				_ = removeRepoRelativeFile(repoRoot, prCommentFile)
			}()
			if output, err := runGitHubPRComment(cmd, repoRoot); err != nil {
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

func newScaffoldCommand(use string, short string, future string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s.\n", future, scaffoldMessage)
			return err
		},
	}
}

func managerFromCommand(cmd *cobra.Command) (session.Manager, error) {
	repoPath, err := cmd.Root().PersistentFlags().GetString("repo")
	if err != nil {
		return session.Manager{}, err
	}
	configPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return session.Manager{}, err
	}
	cfg := config.Default()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return session.Manager{}, err
		}
		cfg = loaded
	}

	return session.Manager{RepoPath: repoPath, Config: cfg}, nil
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

	return review.Options{RepoPath: repoPath, SessionID: sessionID, Last: last, Security: security, Diff: diff}, nil
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

func fileExists(path string) bool {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return false
	}
	defer func() {
		_ = root.Close()
	}()
	_, err = root.Stat(filepath.Base(path))

	return err == nil
}

func writePolicyIfMissing(repoRoot string) error {
	rootPath := filepath.Join(repoRoot, storage.RootDir)
	if err := os.MkdirAll(rootPath, 0o750); err != nil {
		return fmt.Errorf("create policy directory: %w", err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return fmt.Errorf("open policy directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	if _, err := root.Stat(storage.PolicyFile); err == nil {
		return nil
	}
	policy := []byte(`version: 1
privacy:
  redact_secrets: true
  store_prompts: false
  store_raw_tool_outputs: false
retention:
  mode: manual
`)
	if err := root.WriteFile(storage.PolicyFile, policy, 0o600); err != nil {
		return fmt.Errorf("write policy defaults: %w", err)
	}

	return nil
}

func runGitHubPRView(cmd *cobra.Command, repoRoot string) (string, error) {
	gh := exec.CommandContext(cmd.Context(), "gh", "pr", "view", "--json", "number")
	gh.Dir = repoRoot
	output, err := gh.CombinedOutput()

	return string(output), err
}

func runGitHubPRComment(cmd *cobra.Command, repoRoot string) (string, error) {
	gh := exec.CommandContext(cmd.Context(), "gh", "pr", "comment", "--body-file", prCommentFile)
	gh.Dir = repoRoot
	output, err := gh.CombinedOutput()

	return string(output), err
}

func writeRepoRelativeFile(repoRoot string, name string, data []byte) error {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return fmt.Errorf("open repo root: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	if err := os.MkdirAll(filepath.Join(repoRoot, filepath.Dir(name)), 0o750); err != nil {
		return fmt.Errorf("create PR comment directory: %w", err)
	}
	if err := root.WriteFile(name, data, 0o600); err != nil {
		return fmt.Errorf("write PR comment file: %w", err)
	}

	return nil
}

func removeRepoRelativeFile(repoRoot string, name string) error {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()

	return root.Remove(name)
}

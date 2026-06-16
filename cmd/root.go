package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
	"github.com/spf13/cobra"
)

const scaffoldMessage = "command scaffolded; implementation is scheduled for a later AgentReceipt plan step"

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
	return newScaffoldCommand("init", "Bootstrap AgentReceipt config and local storage", "init will create .agentreceipt.yml, policy defaults, session storage, and local signing keys.")
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
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Finalized AgentReceipt session %s\n", state.SessionID)

			return err
		},
	}
}

func newReviewCommand() *cobra.Command {
	review := newScaffoldCommand("review", "Build a reviewer-focused receipt summary", "review will render risk, confidence, command, diff, and evidence-gap summaries.")
	review.Flags().Bool("last", false, "Review the most recent finalized session")
	review.Flags().String("session", "", "Review a specific session ID")
	review.Flags().Bool("security", false, "Focus output on security-sensitive evidence")
	review.Flags().Bool("diff", false, "Include diff-focused receipt details")
	review.Flags().Bool("json", false, "Render review output as JSON")
	review.Flags().Bool("md", false, "Render review output as Markdown")
	review.Flags().Bool("pr", false, "Render concise PR-comment Markdown")
	review.Flags().Bool("full", false, "Include expanded evidence details")
	review.Flags().String("codex-jsonl", "", "Import a Codex JSONL trace before building the review")
	review.Flags().String("provider", "", "Filter review output by provider")

	return review
}

func newVerifyCommand() *cobra.Command {
	verify := newScaffoldCommand("verify", "Verify receipt integrity and signatures", "verify will validate event chain continuity, manifest hashes, final diff hash, and receipt signature.")
	verify.Flags().String("session", "", "Verify a specific session ID")

	return verify
}

func newExportCommand() *cobra.Command {
	export := newScaffoldCommand("export", "Export finalized receipt artifacts", "export will rehydrate finalized receipts as JSON, Markdown, or PR-ready Markdown.")
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
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "mark will record %q as signed session context; %s.\n", message, scaffoldMessage)
			return err
		},
	}
}

func newPRCommand() *cobra.Command {
	pr := &cobra.Command{
		Use:   "pr",
		Short: "Work with pull request receipt output",
	}
	pr.AddCommand(newScaffoldCommand("comment", "Post receipt Markdown to the current pull request", "pr comment will generate PR Markdown and submit it with the GitHub CLI."))

	return pr
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

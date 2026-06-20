package evidence

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/commandrisk"
	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/providerevidence"
)

const maxRiskCommandSummaryRunes = 100

var fallbackCommandKindPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{kind: "network", pattern: regexp.MustCompile(`\b(curl|wget|ssh|nc|aws|gcloud)\b`)},
	{kind: "destructive", pattern: regexp.MustCompile(`\b(rm|dd|mkfs|shutdown|reboot)\b`)},
}

type TimelineItem struct {
	Seq    int64  `json:"seq"`
	Time   string `json:"time"`
	Source string `json:"source"`
	Type   string `json:"type"`
}

type commandAttempt struct {
	command model.DetectedCommand
	callID  string
	seq     int
}

type commandResult struct {
	command         string
	status          string
	callID          string
	exitCode        *int
	stdout          string
	stdoutTruncated bool
	stderrOrError   string
	failedReason    string
	seq             int
}

// Summary extracts deterministic session evidence summary from raw events.
func Summary(events []model.Event, cfg config.Config) model.Summary {
	commands := Commands(events, cfg)
	changedFiles := changedFiles(events)
	summary := model.Summary{
		ChangedFiles:     changedFiles,
		DetectedCommands: commands,
	}
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

func changedFiles(events []model.Event) []model.ChangedFile {
	paths := make(map[string]model.ChangedFile)
	for _, event := range events {
		if event.Type != "fs.change" {
			continue
		}
		path := stringPayload(event.Payload, "path")
		if path == "" {
			continue
		}
		paths[path] = model.ChangedFile{
			Path:       path,
			Action:     stringPayload(event.Payload, "action"),
			Sensitive:  boolPayload(event.Payload, "sensitive"),
			Dependency: boolPayload(event.Payload, "dependency"),
		}
	}
	changedFiles := make([]model.ChangedFile, 0, len(paths))
	for _, path := range paths {
		changedFiles = append(changedFiles, path)
	}
	sort.Slice(changedFiles, func(i, j int) bool {
		return changedFiles[i].Path < changedFiles[j].Path
	})

	return changedFiles
}

// Commands returns command attempts with status paired from matching command results.
func Commands(events []model.Event, cfg config.Config) []model.DetectedCommand {
	commandAttempts := make([]commandAttempt, 0, len(events))
	resultsByCallID := make(map[string]string)
	resultsByCommand := make(map[string]string)
	for seq, event := range events {
		if attempt, ok := providerevidence.CommandAttemptFromEvent(event); ok {
			commandAttempts = append(commandAttempts, commandAttempt{
				command: model.DetectedCommand{
					Command:    attempt.Command,
					Kind:       CommandKind(attempt.Command, cfg),
					Status:     "unknown",
					Source:     attempt.Source,
					Confidence: model.ConfidenceMedium,
				},
				callID: attempt.CallID,
				seq:    seq,
			})
			continue
		}

		result, ok := providerevidence.CommandResultFromEvent(event)
		if !ok {
			continue
		}
		if result.CallID != "" {
			resultsByCallID[result.CallID] = result.Status
		}
		if result.Command != "" {
			resultsByCommand[result.Command] = result.Status
		}
	}

	commands := make([]model.DetectedCommand, 0, len(commandAttempts))
	for _, attempt := range commandAttempts {
		command := attempt.command
		if attempt.callID != "" {
			if status := resultsByCallID[attempt.callID]; status != "" {
				command.Status = status
			}
			commands = append(commands, command)
			continue
		}

		if status := resultsByCommand[attempt.command.Command]; status != "" {
			command.Status = status
		}
		commands = append(commands, command)
	}

	return commands
}

// CommandsWithResultAndUnpaired returns fully paired command attempts plus unpaired command results.
func CommandsWithResultAndUnpaired(events []model.Event, cfg config.Config) ([]model.DetectedCommand, []model.DetectedCommand) {
	commandAttempts := make([]commandAttempt, 0, len(events))
	results := make([]commandResult, 0, len(events))
	resultsByCallID := make(map[string]commandResult)
	for seq, event := range events {
		if attempt, ok := providerevidence.CommandAttemptFromEvent(event); ok {
			commandAttempts = append(commandAttempts, commandAttempt{
				command: model.DetectedCommand{
					Command:    attempt.Command,
					Kind:       CommandKind(attempt.Command, cfg),
					Status:     "unknown",
					Source:     attempt.Source,
					Confidence: model.ConfidenceMedium,
				},
				callID: attempt.CallID,
				seq:    seq,
			})
			continue
		}

		result, ok := commandResultFromEvent(event)
		if !ok {
			continue
		}
		result.seq = seq
		results = append(results, result)
		if result.callID != "" {
			resultsByCallID[result.callID] = result
		}
	}

	paired := make([]model.DetectedCommand, 0, len(commandAttempts))
	seenCallID := map[string]bool{}
	seenCommand := map[string]bool{}

	for _, attempt := range commandAttempts {
		command := attempt.command
		if attempt.callID != "" {
			if result, ok := resultsByCallID[attempt.callID]; ok {
				command.Status = result.status
				seenCallID[attempt.callID] = true
				if result.command != "" {
					seenCommand[result.command] = true
				}
			}
		}
		paired = append(paired, command)
	}

	unpaired := make([]model.DetectedCommand, 0)
	for _, result := range results {
		if result.command == "" {
			continue
		}
		if result.callID != "" {
			if seenCallID[result.callID] {
				continue
			}
		}
		if seenCommand[result.command] {
			continue
		}
		seenCommand[result.command] = true
		unpaired = append(unpaired, model.DetectedCommand{
			Command:    result.command,
			Kind:       CommandKind(result.command, cfg),
			Status:     result.status,
			Source:     "provider.command_result",
			Confidence: model.ConfidenceMedium,
		})
	}

	sort.SliceStable(unpaired, func(i, j int) bool {
		return unpaired[i].Command < unpaired[j].Command
	})

	return paired, unpaired
}

// Confidence derives evidence confidence levels from provider events.
func Confidence(events []model.Event) model.CaptureConfidence {
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
		if providerevidence.IsProviderEvidenceSource(event) && providerevidence.IsToolEvidenceEvent(event) {
			confidence.ProviderToolEvents = model.ConfidenceMedium
		}
	}

	return confidence
}

func Risk(summary model.Summary, warnings []model.Warning, events []model.Event, cfg config.Config) model.Risk {
	result := model.Risk{Level: model.RiskInfo}
	for _, changed := range summary.ChangedFiles {
		if changed.Sensitive && cfg.Review.FlagSecretPaths {
			result.Level = MaxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "sensitive_path_changed",
				Message:    "Sensitive path changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
		if isAuthPath(changed.Path) && cfg.Review.FlagAuthChanges {
			result.Level = MaxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "auth_path_changed",
				Message:    "Authentication-sensitive path changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
		if changed.Dependency && cfg.Review.FlagDependencyChanges {
			result.Level = MaxRisk(result.Level, model.RiskMedium)
			result.Reasons = append(result.Reasons, model.RiskReason{
				Code:       "dependency_changed",
				Message:    "Dependency file changed: " + changed.Path,
				Level:      model.RiskMedium,
				Confidence: model.ConfidenceHigh,
			})
		}
	}
	for _, reason := range commandRiskReasons(summary.DetectedCommands) {
		result.Level = MaxRisk(result.Level, reason.Level)
		result.Reasons = append(result.Reasons, reason)
	}
	for _, reason := range providerRiskReasons(events) {
		result.Level = MaxRisk(result.Level, reason.Level)
		result.Reasons = append(result.Reasons, reason)
	}
	for _, warning := range warnings {
		result.Level = MaxRisk(result.Level, model.RiskLow)
		result.Reasons = append(result.Reasons, model.RiskReason{
			Code:       warning.Code,
			Message:    warning.Message,
			Level:      model.RiskLow,
			Confidence: model.ConfidenceMedium,
		})
	}

	return result
}

func Focus(summary model.Summary, risk model.Risk, cfg config.Config) []string {
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

func Gaps(summary model.Summary, confidence model.CaptureConfidence, warnings []model.Warning, cfg config.Config) []string {
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

func Timeline(events []model.Event) []TimelineItem {
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

func commandRiskReasons(commands []model.DetectedCommand) []model.RiskReason {
	reasons := make([]model.RiskReason, 0)
	seen := map[string]bool{}
	for _, command := range commands {
		for _, classification := range commandrisk.Classify(command.Command) {
			if classification.Level == model.RiskLow || classification.Level == model.RiskInfo || classification.Level == "" {
				continue
			}
			code := "command_risk_" + RiskCodeFragment(classification.Signal)
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

func providerRiskReasons(events []model.Event) []model.RiskReason {
	reasons := make([]model.RiskReason, 0)
	seen := map[string]bool{}
	for _, event := range events {
		for _, signal := range providerevidence.RiskSignalsFromEvent(event) {
			if signal.Level == model.RiskLow || signal.Level == model.RiskInfo || signal.Level == "" {
				continue
			}
			code := "provider_risk_" + RiskCodeFragment(signal.Signal)
			message := providerRiskMessage(signal)
			key := code + ":" + message
			if seen[key] {
				continue
			}
			seen[key] = true
			reasons = append(reasons, model.RiskReason{
				Code:       code,
				Message:    message,
				Level:      signal.Level,
				Confidence: signal.Confidence,
			})
		}
	}

	return reasons
}

func CommandKind(command string, cfg config.Config) string {
	if kind := ConfiguredCommandKind(command, cfg.TestCommands); kind != "" {
		return kind
	}
	normalized := normalizedCommand(command)
	for _, matcher := range fallbackCommandKindPatterns {
		if matcher.pattern.MatchString(normalized) {
			return matcher.kind
		}
	}

	return "command"
}

func ConfiguredCommandKind(command string, configured []string) string {
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

func MaxRisk(left model.RiskLevel, right model.RiskLevel) model.RiskLevel {
	if riskRank(right) > riskRank(left) {
		return right
	}

	return left
}

func CommandSummary(command string) string {
	return truncateRunes(normalizedCommand(command), maxRiskCommandSummaryRunes)
}

func RiskCodeFragment(value string) string {
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

func hasTypeScriptChanges(summary model.Summary) bool {
	for _, changed := range summary.ChangedFiles {
		switch filepath.Ext(strings.ToLower(changed.Path)) {
		case ".ts", ".tsx", ".mts", ".cts":
			return true
		}
	}

	return false
}

func hasCodeChanges(summary model.Summary) bool {
	for _, changed := range summary.ChangedFiles {
		switch filepath.Ext(strings.ToLower(changed.Path)) {
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

func commandRiskMessage(classification commandrisk.Classification, command string) string {
	label := classification.Signal
	if label == "" {
		label = "command"
	}
	details := classification.Reason
	if details == "" {
		details = "command matched a risk rule"
	}

	return "Command risk detected (" + label + "): " + details + " in command: " + CommandSummary(command)
}

func providerRiskMessage(signal providerevidence.RiskSignal) string {
	label := signal.Signal
	if label == "" {
		label = "provider"
	}
	details := signal.Details
	if details == "" {
		details = "provider classified the command as risky"
	}
	message := "Provider risk detected (" + label + "): " + details
	if signal.Command != "" {
		message += " in command: " + CommandSummary(signal.Command)
	}

	return message
}

func normalizedCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
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

// commandResultFromEvent reads provider command result events including replay-safe metadata.
func commandResultFromEvent(event model.Event) (commandResult, bool) {
	result, ok := providerevidence.CommandResultFromEvent(event)
	if !ok {
		return commandResult{}, false
	}

	return commandResult{
		command:         result.Command,
		status:          result.Status,
		callID:          result.CallID,
		exitCode:        result.ExitCode,
		stdout:          result.Stdout,
		stdoutTruncated: result.StdoutTruncated,
		stderrOrError:   result.StderrOrError,
		failedReason:    result.FailedReason,
	}, true
}

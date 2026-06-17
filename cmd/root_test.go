package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/provider/codex"
	"github.com/ametel01/agentreceipt/internal/session"
	"github.com/ametel01/agentreceipt/internal/storage"
	"github.com/spf13/cobra"
)

func TestRootHelpListsCommandSurface(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("root help returned error: %v", err)
	}

	required := []string{
		"init",
		"install codex",
		"install claude",
		"start",
		"status",
		"live",
		"stop",
		"review",
		"verify",
		"export",
		"import codex-jsonl",
		"inspect codex",
		"mark",
		"pr comment",
		"--config",
		"--repo",
		"--quiet",
		"--color",
	}
	for _, want := range required {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q\nhelp:\n%s", want, stdout)
		}
	}
}

func TestCodexParseOptionsFromConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Privacy.RedactSecrets = false
	cfg.Privacy.StorePrompts = true
	cfg.Privacy.StoreRawToolOutputs = true
	cfg.Privacy.MaxBlobBytes = 42

	options := codexParseOptions("ar_ses_test", "/repo", cfg)
	if options.SessionID != "ar_ses_test" || options.CWD != "/repo" {
		t.Fatalf("session/cwd options = %+v", options)
	}
	if options.RedactSecrets || !options.RedactSecretsSet || !options.StorePrompts || !options.StoreRawToolOutputs || options.MaxOutputBytes != 42 {
		t.Fatalf("privacy options did not follow config: %+v", options)
	}
	tailOptions := codexTailOptions("ar_ses_test", "/repo", cfg, 10, 2)
	if tailOptions.Offset != 10 || tailOptions.LineOffset != 2 || tailOptions.RedactSecrets || !tailOptions.RedactSecretsSet || !tailOptions.StorePrompts || !tailOptions.StoreRawToolOutputs || tailOptions.MaxOutputBytes != 42 {
		t.Fatalf("tail privacy options did not follow config: %+v", tailOptions)
	}
}

func TestCommandTreeContainsRequiredCommands(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	required := [][]string{
		{"init"},
		{"install", "codex"},
		{"install", "claude"},
		{"start"},
		{"status"},
		{"live"},
		{"stop"},
		{"review"},
		{"verify"},
		{"verify", "bundle"},
		{"export"},
		{"import", "codex-jsonl"},
		{"inspect", "codex"},
		{"mark"},
		{"pr", "comment"},
		{"version"},
	}
	for _, path := range required {
		if found, _, err := root.Find(path); err != nil || found == nil {
			t.Fatalf("command %q not found: %v", strings.Join(path, " "), err)
		}
	}
}

func TestColorFlagValidation(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	if flag := root.PersistentFlags().Lookup("color"); flag == nil {
		t.Fatal("root color flag is not registered")
	}

	if _, _, err := executeCommand(t, "--color", "sometimes", "version"); err == nil || !strings.Contains(err.Error(), "--color must be one of auto, always, or never") {
		t.Fatalf("expected color validation error, got %v", err)
	}
}

func TestReviewModeFlags(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	review, _, err := root.Find([]string{"review"})
	if err != nil {
		t.Fatalf("find review command: %v", err)
	}

	for _, name := range []string{"last", "session", "security", "diff", "json", "md", "pr", "codex-jsonl"} {
		if review.Flags().Lookup(name) == nil {
			t.Fatalf("review flag %q is not registered", name)
		}
	}
	for _, name := range []string{"full", "provider"} {
		if review.Flags().Lookup(name) != nil {
			t.Fatalf("inactive review flag %q is still registered", name)
		}
	}
}

func TestStartWatchFlags(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	start, _, err := root.Find([]string{"start"})
	if err != nil {
		t.Fatalf("find start command: %v", err)
	}

	for _, name := range []string{"watch", "codex-home", "watch-interval", "watch-duration", "watch-existing"} {
		if start.Flags().Lookup(name) == nil {
			t.Fatalf("start flag %q is not registered", name)
		}
	}
}

func TestStartWatchOptionsValidation(t *testing.T) {
	t.Parallel()

	cmd := newStartCommand()
	if err := cmd.Flags().Set("watch-interval", "0"); err != nil {
		t.Fatalf("set watch-interval: %v", err)
	}
	if _, err := watchOptionsFromStartCommand(cmd); err == nil || !strings.Contains(err.Error(), "watch-interval") {
		t.Fatalf("expected interval validation error, got %v", err)
	}

	cmd = newStartCommand()
	if err := cmd.Flags().Set("watch-duration", "-1s"); err != nil {
		t.Fatalf("set watch-duration: %v", err)
	}
	if _, err := watchOptionsFromStartCommand(cmd); err == nil || !strings.Contains(err.Error(), "watch-duration") {
		t.Fatalf("expected duration validation error, got %v", err)
	}

	cmd = newStartCommand()
	if err := cmd.Flags().Set("watch-interval", "250ms"); err != nil {
		t.Fatalf("set watch-interval: %v", err)
	}
	if err := cmd.Flags().Set("watch-duration", "1s"); err != nil {
		t.Fatalf("set watch-duration: %v", err)
	}
	if err := cmd.Flags().Set("watch-existing", "true"); err != nil {
		t.Fatalf("set watch-existing: %v", err)
	}
	options, err := watchOptionsFromStartCommand(cmd)
	if err != nil {
		t.Fatalf("watchOptionsFromStartCommand() error = %v", err)
	}
	if options.Interval != 250*time.Millisecond || options.Duration != time.Second || !options.IncludeExisting {
		t.Fatalf("options = %+v", options)
	}
}

func TestCodexCandidateMatchesRepoAndNewLogs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	otherRepo := t.TempDir()
	dir := t.TempDir()
	matchingTrace := filepath.Join(dir, "matching.jsonl")
	if err := os.WriteFile(matchingTrace, []byte(`{"type":"session_meta","payload":{"type":"session_meta","cwd":"`+repo+`"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write matching trace: %v", err)
	}
	matches, reason := codexCandidateMatches(codex.Candidate{Path: matchingTrace, ModTime: time.Now()}, repo, time.Now())
	if !matches || !strings.Contains(reason, "cwd") {
		t.Fatalf("expected cwd match, got matches=%v reason=%q", matches, reason)
	}

	otherTrace := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(otherTrace, []byte(`{"type":"session_meta","payload":{"type":"session_meta","cwd":"`+otherRepo+`"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write other trace: %v", err)
	}
	matches, _ = codexCandidateMatches(codex.Candidate{Path: otherTrace, ModTime: time.Now()}, repo, time.Now())
	if matches {
		t.Fatal("candidate from another cwd matched repo")
	}

	noMetadataTrace := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(noMetadataTrace, []byte(`{"type":"response_item"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write new trace: %v", err)
	}
	matches, reason = codexCandidateMatches(codex.Candidate{Path: noMetadataTrace, ModTime: time.Now()}, repo, time.Now().Add(-time.Second))
	if !matches || !strings.Contains(reason, "new log") {
		t.Fatalf("expected new log match, got matches=%v reason=%q", matches, reason)
	}
	matches, _ = codexCandidateMatches(codex.Candidate{Path: noMetadataTrace, ModTime: time.Now().Add(-time.Minute)}, repo, time.Now())
	if matches {
		t.Fatal("old candidate without cwd metadata matched repo")
	}
}

func TestStartWatchDefaultsToNewestMatchingCodexLog(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "17")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	olderPath := filepath.Join(sessionDir, "rollout-old.jsonl")
	newerPath := filepath.Join(sessionDir, "rollout-new.jsonl")
	for _, path := range []string{olderPath, newerPath} {
		trace := `{"type":"session_meta","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"session_meta","cwd":"` + repo + `"}}` + "\n"
		if err := os.WriteFile(path, []byte(trace), 0o600); err != nil {
			t.Fatalf("write trace %s: %v", path, err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(olderPath, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtime older trace: %v", err)
	}
	if err := os.Chtimes(newerPath, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtime newer trace: %v", err)
	}

	stdout, _, err := executeCommand(t, "--repo", repo, "start", "--watch", "--codex-home", home, "--watch-interval", "1ms", "--watch-duration", "5ms")
	if err != nil {
		t.Fatalf("start --watch returned error: %v\n%s", err, stdout)
	}
	if got := strings.Count(stdout, "codex  watch   "); got != 1 {
		t.Fatalf("watching count = %d, output:\n%s", got, stdout)
	}
	if !strings.Contains(stdout, "rollout-new.jsonl") || strings.Contains(stdout, "rollout-old.jsonl") {
		t.Fatalf("watch did not select only newest log:\n%s", stdout)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestPrintCodexLiveResultFormatsToolsResultsAndWarnings(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	longCommand := strings.Repeat("x", 300)
	result := codex.ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"` + longCommand + `"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 7\nfailed"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":25,"output_tokens":10,"reasoning_output_tokens":3,"total_tokens":110}}}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"update_plan","call_id":"call_2","arguments":{"plan":[]}}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","call_id":"call_3","input":"patch"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_3","output":"Exit code: 0\nok"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":50,"cached_input_tokens":5,"output_tokens":7,"reasoning_output_tokens":0,"total_tokens":160}}}}`,
		`{malformed`,
	}, "\n")), codex.ParseOptions{SessionID: "ar_ses_test"})
	if err := printCodexLiveResult(cmd, result); err != nil {
		t.Fatalf("printCodexLiveResult() error = %v", err)
	}
	output := stdout.String()
	for _, want := range []string{
		"codex  fail    run",
		"(exit 7)",
		"codex  ok      edit apply_patch",
		"codex  tokens  110 (110 session) after",
		"codex  tokens  50 (160 session) after edit apply_patch",
		"codex  warn    malformed_json:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, longCommand) {
		t.Fatalf("long command was not truncated: %q", output)
	}
}

func TestCodexWatchRendererBuildsStructuredEvents(t *testing.T) {
	t.Parallel()

	longCommand := strings.Repeat("x", 300)
	result := codex.ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"` + longCommand + `"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 7\nfailed"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":25,"output_tokens":10,"reasoning_output_tokens":3,"total_tokens":110}}}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","call_id":"call_3","input":"patch"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_3","output":"Exit code: 0\nok"}}`,
		`{malformed`,
	}, "\n")), codex.ParseOptions{SessionID: "ar_ses_test", SourcePath: "/tmp/codex.jsonl"})

	events := newCodexWatchRenderer(io.Discard).Events(result)
	if got, want := len(events), 4; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	commandEvent := events[0]
	if commandEvent.Provider != "codex" || commandEvent.Family != codex.LogFamilyTool || commandEvent.Category != codex.CategoryFunctionCallOutput {
		t.Fatalf("command event classification = %+v", commandEvent)
	}
	if commandEvent.Status != "fail" || commandEvent.ExitCode == nil || *commandEvent.ExitCode != 7 || !strings.HasPrefix(commandEvent.Message, "run ") {
		t.Fatalf("command event fields = %+v", commandEvent)
	}
	if commandEvent.Command != longCommand {
		t.Fatalf("command event command = %q, want long command", commandEvent.Command)
	}
	tokenEvent := events[1]
	if tokenEvent.Family != codex.LogFamilyTelemetry || tokenEvent.Category != codex.CategoryTokenCount || tokenEvent.Status != "tokens" || tokenEvent.Tokens != 110 || tokenEvent.TotalTokens != 110 {
		t.Fatalf("token event fields = %+v", tokenEvent)
	}
	if !strings.HasPrefix(tokenEvent.Message, "after run ") {
		t.Fatalf("token event message = %q", tokenEvent.Message)
	}
	editEvent := events[2]
	if editEvent.Status != "ok" || editEvent.Message != "edit apply_patch" || editEvent.Tool != "apply_patch" {
		t.Fatalf("edit event fields = %+v", editEvent)
	}
	warningEvent := events[3]
	if warningEvent.Status != "warn" || warningEvent.Reason != "malformed_json" || !strings.Contains(warningEvent.Message, "malformed_json:") {
		t.Fatalf("warning event fields = %+v", warningEvent)
	}
}

func TestCodexWatchRendererDisplaysRiskBadgesWithoutReplacingOutcome(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	result := codex.ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"rm -rf dist"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_2","arguments":{"cmd":"pnpm install"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_2","output":"Exit code: 0\nok"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_3","arguments":{"cmd":"npm run build"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_3","output":"Exit code: 0\nok"}}`,
	}, "\n")), codex.ParseOptions{SessionID: "ar_ses_test"})

	renderer := newCodexWatchRenderer(&stdout)
	events := renderer.Events(result)
	if got, want := len(events), 4; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	if events[0].Status != "ok" || events[0].RiskLevel != "high" || events[0].RiskSignal != "destructive_filesystem" {
		t.Fatalf("high risk command event = %+v", events[0])
	}
	if events[1].Status != "risk" || !strings.Contains(events[1].Message, "destructive_filesystem:") {
		t.Fatalf("high risk detail event = %+v", events[1])
	}
	if events[2].Status != "ok" || events[2].RiskLevel != "medium" {
		t.Fatalf("medium risk command event = %+v", events[2])
	}
	if events[3].Status != "ok" || events[3].RiskLevel != "low" {
		t.Fatalf("low risk command event = %+v", events[3])
	}

	renderer = newCodexWatchRenderer(&stdout)
	if err := renderer.Print(result); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	output := stdout.String()
	for _, want := range []string{
		"codex  ok      [HIGH] run rm -rf dist",
		"codex  risk    destructive_filesystem:",
		"codex  ok      [MED] run pnpm install",
		"codex  ok      [low] run npm run build",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestCodexWatchRendererUsesHighestRiskSignal(t *testing.T) {
	t.Parallel()

	result := codex.ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"rm -rf dist && npm install"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
	}, "\n")), codex.ParseOptions{SessionID: "ar_ses_test"})

	events := newCodexWatchRenderer(io.Discard).Events(result)
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	if events[0].Status != "ok" || events[0].RiskLevel != model.RiskHigh || events[0].RiskSignal != "destructive_filesystem" {
		t.Fatalf("command event risk = %+v, want highest risk signal", events[0])
	}
	if events[1].Status != "risk" || !strings.Contains(events[1].Message, "destructive_filesystem:") {
		t.Fatalf("risk detail event = %+v", events[1])
	}
}

func TestCodexWatchRendererReportsBatchTokenUsage(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	result := codex.ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"git status --short"}}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_2","arguments":{"cmd":"git diff --stat"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_2","output":"Exit code: 0\nok"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":200,"cached_input_tokens":75,"output_tokens":20,"reasoning_output_tokens":4,"total_tokens":220}}}}`,
	}, "\n")), codex.ParseOptions{SessionID: "ar_ses_test"})
	renderer := newCodexWatchRenderer(&stdout)
	events := renderer.Events(result)
	if got, want := len(events), 3; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	if events[2].Status != "tokens" || events[2].Tokens != 220 || events[2].TotalTokens != 220 || events[2].Message != "after 2 actions" {
		t.Fatalf("batch token event = %+v", events[2])
	}

	renderer = newCodexWatchRenderer(&stdout)
	if err := renderer.Print(result); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "codex  tokens  220 (220 session) after 2 actions") {
		t.Fatalf("batch token output missing:\n%s", output)
	}
}

func TestCodexWatchRendererReportsTokenDeltaBeforeCumulativeTotal(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	result := codex.ParseJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"make test"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":100}}}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_2","arguments":{"cmd":"make smoke"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_2","output":"Exit code: 0\nok"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":135}}}}`,
	}, "\n")), codex.ParseOptions{SessionID: "ar_ses_test"})
	renderer := newCodexWatchRenderer(&stdout)
	events := renderer.Events(result)
	if got, want := len(events), 4; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	if events[1].Tokens != 100 || events[1].TotalTokens != 100 {
		t.Fatalf("first token event = %+v", events[1])
	}
	if events[3].Tokens != 35 || events[3].TotalTokens != 135 {
		t.Fatalf("second token event = %+v", events[3])
	}

	renderer = newCodexWatchRenderer(&stdout)
	if err := renderer.Print(result); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "codex  tokens  35 (135 session) after run make smoke") {
		t.Fatalf("delta/session token output missing:\n%s", output)
	}
}

func TestCodexWatchRendererSkipsOrphanTokenTelemetry(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	result := codex.ParseJSONL(strings.NewReader(`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":200,"cached_input_tokens":75,"output_tokens":20,"reasoning_output_tokens":4,"total_tokens":220}}}}`), codex.ParseOptions{SessionID: "ar_ses_test"})
	renderer := newCodexWatchRenderer(&stdout)
	if events := renderer.Events(result); len(events) != 0 {
		t.Fatalf("orphan token telemetry produced events: %+v", events)
	}
	if err := renderer.Print(result); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	if output := stdout.String(); strings.Contains(output, "tokens") {
		t.Fatalf("orphan token telemetry was printed:\n%s", output)
	}
}

func TestWatchCodexReportsMissingLogsOnce(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	err := watchCodex(context.Background(), cmd, session.Manager{}, session.State{RepoRoot: t.TempDir(), SessionID: "ar_ses_test"}, startWatchOptions{
		CodexHome: t.TempDir(),
		Interval:  1 * time.Millisecond,
		Duration:  3 * time.Millisecond,
	})
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("watchCodex() error = %v", err)
	}
	output := stdout.String()
	if got := strings.Count(output, "codex  warn    codex_logs_missing"); got != 1 {
		t.Fatalf("missing-log warning count = %d, output:\n%s", got, output)
	}
}

func TestInstallClaudeIsDeferredNoOp(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "install", "claude")
	if err != nil {
		t.Fatalf("install claude returned error: %v", err)
	}
	if !strings.Contains(stdout, "deferred in the Codex-first MVP") {
		t.Fatalf("install claude output did not explain deferred status: %q", stdout)
	}
}

func TestInstallCodexReportsMissingLogs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	stdout, _, err := executeCommand(t, "install", "codex", "--home", home)
	if err != nil {
		t.Fatalf("install codex returned error: %v", err)
	}
	for _, want := range []string{
		"Codex home: " + home,
		"Home source: explicit --home",
		"Candidates: 0",
		"warning[codex_logs_missing]",
		"Next: agentreceipt start --watch",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("install codex output missing %q:\n%s", want, stdout)
		}
	}
}

func TestInstallCodexReportsCandidates(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "16")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "rollout-test.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	stdout, _, err := executeCommand(t, "install", "codex", "--home", home)
	if err != nil {
		t.Fatalf("install codex returned error: %v", err)
	}
	for _, want := range []string{
		"Candidates: 1",
		"Warnings: 0",
		"Newest candidate: " + sessionPath,
		"Next: agentreceipt start --watch",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("install codex output missing %q:\n%s", want, stdout)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "version")
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	if got, want := strings.TrimSpace(stdout), "agentreceipt test-version"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestInternalFilesystemWatcherCommandValidatesConfig(t *testing.T) {
	repo := newCommandGitRepo(t)

	_, _, err := executeCommand(t, "--repo", repo, "__internal-fswatcher", "--session", "ar_ses_internal", "--config-json", "{")
	if err == nil || !strings.Contains(err.Error(), "decode filesystem watcher config") {
		t.Fatalf("expected config decode error, got %v", err)
	}
}

func TestCommandPayloadHelpers(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"string": "value",
		"map":    map[string]any{"nested": "ok"},
		"int":    7,
		"float":  8.0,
		"number": json.Number("9"),
	}
	if got := stringPayload(payload, "string"); got != "value" {
		t.Fatalf("stringPayload() = %q", got)
	}
	if got := stringPayload(payload, "missing"); got != "" {
		t.Fatalf("missing stringPayload() = %q", got)
	}
	if got := mapPayload(payload, "map"); got["nested"] != "ok" {
		t.Fatalf("mapPayload() = %+v", got)
	}
	if got := mapPayload(payload, "string"); got != nil {
		t.Fatalf("non-map mapPayload() = %+v", got)
	}
	for key, want := range map[string]int{"int": 7, "float": 8, "number": 9} {
		got, ok := intPayload(payload, key)
		if !ok || got != want {
			t.Fatalf("intPayload(%q) = %d, %v, want %d true", key, got, ok, want)
		}
	}
	if got, ok := intPayload(payload, "missing"); ok || got != 0 {
		t.Fatalf("missing intPayload() = %d, %v", got, ok)
	}
	if got := emptyDefault("", "fallback"); got != "fallback" {
		t.Fatalf("emptyDefault empty = %q", got)
	}
	if got := emptyDefault("value", "fallback"); got != "value" {
		t.Fatalf("emptyDefault value = %q", got)
	}
}

func TestInitCommandCreatesConfigPolicyStorageAndKeys(t *testing.T) {
	repo := newCommandGitRepo(t)
	homeDir := filepath.Join(t.TempDir(), "home")
	keyDir := filepath.Join(t.TempDir(), "keys")
	t.Setenv("AGENTRECEIPT_HOME", homeDir)
	t.Setenv("AGENTRECEIPT_KEY_DIR", keyDir)

	stdout, _, err := executeCommand(t, "--repo", repo, "init")
	if err != nil {
		t.Fatalf("init returned error: %v", err)
	}
	if !strings.Contains(stdout, "Initialized global AgentReceipt storage") {
		t.Fatalf("init output = %q", stdout)
	}
	for _, path := range []string{
		homeDir,
		filepath.Join(keyDir, "default.ed25519"),
		filepath.Join(keyDir, "default.pub"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected init artifact %s: %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(repo, ".agentreceipt.yml"), filepath.Join(repo, ".agentreceipt")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("init polluted repo path %s: %v", path, err)
		}
	}
}

func TestLifecycleCommandsUsePersistedSessionState(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)

	stdout, _, err := executeCommand(t, "--repo", repo, "start")
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	if !strings.Contains(stdout, "Started AgentReceipt session ar_ses_") {
		t.Fatalf("start output = %q", stdout)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(stdout, "State: active") || !strings.Contains(stdout, "Events: 1") {
		t.Fatalf("status output did not reflect active session: %q", stdout)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "live", "--limit", "1")
	if err != nil {
		t.Fatalf("live returned error: %v", err)
	}
	if !strings.Contains(stdout, `"type":"git.snapshot"`) {
		t.Fatalf("live output missing git snapshot: %q", stdout)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "stop")
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "Finalized AgentReceipt session ar_ses_") {
		t.Fatalf("stop output = %q", stdout)
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(stdout, "Finalized AgentReceipt session "))
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	for _, path := range []string{layout.ReceiptJSON, layout.ReceiptMarkdown, layout.ReviewMarkdown, layout.ReceiptSignature} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status after stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "No active AgentReceipt session.") {
		t.Fatalf("status after stop output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "verify")
	if err != nil {
		t.Fatalf("verify returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Receipt valid.") || !strings.Contains(stdout, "Signature: valid") {
		t.Fatalf("verify output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "verify", "bundle", layout.Session)
	if err != nil {
		t.Fatalf("verify bundle returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Receipt valid.") || !strings.Contains(stdout, "Signed by: embedded:") {
		t.Fatalf("verify bundle output = %q", stdout)
	}
	tamperedBundle := copyCommandReceiptBundle(t, layout.Session)
	if err := os.WriteFile(filepath.Join(tamperedBundle, storage.DiffsDir, storage.FinalPatchFile), []byte("tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper bundle: %v", err)
	}
	stdout, _, err = executeCommand(t, "verify", "bundle", tamperedBundle)
	if err == nil || !strings.Contains(stdout, "Receipt invalid.") {
		t.Fatalf("tampered verify bundle err=%v output=%q", err, stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "export", "--json")
	if err != nil {
		t.Fatalf("export json returned error: %v", err)
	}
	if !strings.Contains(stdout, `"signature_algorithm": "ed25519"`) {
		t.Fatalf("export json output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "export", "--pr")
	if err != nil {
		t.Fatalf("export pr returned error: %v", err)
	}
	if !strings.Contains(stdout, "## AgentReceipt") {
		t.Fatalf("export pr output = %q", stdout)
	}
}

func TestImportCodexJSONLCommand(t *testing.T) {
	t.Parallel()

	if _, _, err := executeCommand(t, "import", "codex-jsonl"); err == nil {
		t.Fatal("import codex-jsonl without a path returned nil error")
	}
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(tracePath, []byte(`{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	stdout, _, err := executeCommand(t, "import", "codex-jsonl", tracePath)
	if err != nil {
		t.Fatalf("import codex-jsonl returned error: %v", err)
	}
	if !strings.Contains(stdout, "events=1") || !strings.Contains(stdout, "active_session=false") {
		t.Fatalf("import output = %q", stdout)
	}
}

func TestImportCodexJSONLActiveSession(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	if _, _, err := executeCommand(t, "--repo", repo, "start"); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	trace := strings.Join([]string{
		`{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{malformed`,
	}, "\n")
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	stdout, _, err := executeCommand(t, "--repo", repo, "import", "codex-jsonl", tracePath)
	if err != nil {
		t.Fatalf("active import returned error: %v", err)
	}
	if !strings.Contains(stdout, "active_session=true") || !strings.Contains(stdout, "warnings=1") {
		t.Fatalf("active import output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(stdout, "- codex_logs: imported") {
		t.Fatalf("status did not show imported Codex logs: %q", stdout)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}

	stdout, _, err = executeCommand(t, "--repo", repo, "review", "--last", "--json")
	if err != nil {
		t.Fatalf("review json returned error: %v", err)
	}
	if !strings.Contains(stdout, `"session_id"`) || !strings.Contains(stdout, `"risk"`) {
		t.Fatalf("review json output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "review", "--last", "--pr")
	if err != nil {
		t.Fatalf("review pr returned error: %v", err)
	}
	if !strings.Contains(stdout, "## AgentReceipt") || !strings.Contains(stdout, "Working tree:") {
		t.Fatalf("review pr output = %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--color", "always", "--repo", repo, "review", "--last")
	if err != nil {
		t.Fatalf("review color returned error: %v", err)
	}
	if !strings.Contains(stdout, "\x1b[") || !strings.Contains(stdout, "AgentReceipt Review") {
		t.Fatalf("review color output = %q", stdout)
	}
}

func TestReviewCodexJSONLImportsActiveSession(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	if _, _, err := executeCommand(t, "--repo", repo, "start"); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	trace := strings.Join([]string{
		`{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{"type":"response_item","timestamp":"2026-06-16T00:00:01Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	stdout, _, err := executeCommand(t, "--repo", repo, "review", "--codex-jsonl", tracePath, "--json")
	if err != nil {
		t.Fatalf("review --codex-jsonl returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, `"command": "go test ./..."`) || !strings.Contains(stdout, `"status": "success"`) {
		t.Fatalf("review json did not include imported command result:\n%s", stdout)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestReviewUsesConfiguredTestCommands(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	configPath := filepath.Join(t.TempDir(), "agentreceipt.yml")
	configFixture := strings.Join([]string{
		"version: 1",
		"test_commands:",
		`  - "make ci"`,
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configFixture), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	if _, _, err := executeCommand(t, "--config", configPath, "--repo", repo, "start"); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	trace := `{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"make ci\"}"}}` + "\n"
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	stdout, _, err := executeCommand(t, "--config", configPath, "--repo", repo, "review", "--codex-jsonl", tracePath, "--json")
	if err != nil {
		t.Fatalf("review --config returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, `"test_detected": true`) {
		t.Fatalf("review json did not apply configured test command:\n%s", stdout)
	}
	if _, _, err := executeCommand(t, "--config", configPath, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestReviewCodexJSONLRequiresActiveSession(t *testing.T) {
	repo := newCommandGitRepo(t)
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(tracePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	_, _, err := executeCommand(t, "--repo", repo, "review", "--codex-jsonl", tracePath)
	if err == nil || !strings.Contains(err.Error(), "requires an active AgentReceipt session") {
		t.Fatalf("expected active session error, got %v", err)
	}
}

func TestStartWatchImportsMatchingCodexLog(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "17")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	tracePath := filepath.Join(sessionDir, "rollout-test.jsonl")
	trace := strings.Join([]string{
		`{"type":"session_meta","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"session_meta","cwd":"` + repo + `"}}`,
		`{"type":"response_item","timestamp":"2026-06-17T00:00:01Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{"type":"response_item","timestamp":"2026-06-17T00:00:02Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	stdout, _, err := executeCommand(t, "--repo", repo, "start", "--watch", "--codex-home", home, "--watch-existing", "--watch-interval", "1ms", "--watch-duration", "5ms")
	if err != nil {
		t.Fatalf("start --watch returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Watching Codex logs") || !strings.Contains(stdout, "codex  watch") || !strings.Contains(stdout, "codex  ok      run go test ./...") {
		t.Fatalf("watch output missing live command details: %q", stdout)
	}
	stdout, _, err = executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(stdout, "- codex_logs: imported") {
		t.Fatalf("status did not show imported Codex logs: %q", stdout)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestStartWatchResumesActiveSession(t *testing.T) {
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	repo := newCommandGitRepo(t)
	startOut, _, err := executeCommand(t, "--repo", repo, "start")
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(startOut, "Started AgentReceipt session "))
	baselineTracePath := filepath.Join(t.TempDir(), "baseline.jsonl")
	baselineTrace := `{"type":"event_msg","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":100}}}}` + "\n"
	if err := os.WriteFile(baselineTracePath, []byte(baselineTrace), 0o600); err != nil {
		t.Fatalf("write baseline trace: %v", err)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "import", "codex-jsonl", baselineTracePath); err != nil {
		t.Fatalf("baseline import returned error: %v", err)
	}

	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "17")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	tracePath := filepath.Join(sessionDir, "resume-test.jsonl")
	trace := strings.Join([]string{
		`{"type":"session_meta","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"session_meta","cwd":"` + repo + `"}}`,
		`{"type":"response_item","timestamp":"2026-06-17T00:00:01Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"go test ./..."}}}`,
		`{"type":"response_item","timestamp":"2026-06-17T00:00:02Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
		`{"type":"event_msg","timestamp":"2026-06-17T00:00:03Z","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":135}}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	stdout, _, err := executeCommand(t, "--repo", repo, "start", "--watch", "--codex-home", home, "--watch-existing", "--watch-interval", "1ms", "--watch-duration", "5ms")
	if err != nil {
		t.Fatalf("resume start --watch returned error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Resumed AgentReceipt session "+sessionID) || strings.Contains(stdout, "Started AgentReceipt session") {
		t.Fatalf("resume output did not reuse active session:\n%s", stdout)
	}
	if !strings.Contains(stdout, "codex  ok      run go test ./...") {
		t.Fatalf("resume output missing live command details:\n%s", stdout)
	}
	if !strings.Contains(stdout, "codex  tokens  35 (135 session) after run go test ./...") || strings.Contains(stdout, "codex  tokens  135 (135 session)") {
		t.Fatalf("resume output did not use existing token baseline:\n%s", stdout)
	}
	statusOut, _, err := executeCommand(t, "--repo", repo, "status")
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(statusOut, "Session: "+sessionID) || !strings.Contains(statusOut, "- codex_logs: imported") {
		t.Fatalf("status did not show resumed import for same session:\n%s", statusOut)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestStartWatchColorModes(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		wantANSI bool
	}{
		{mode: "never", wantANSI: false},
		{mode: "always", wantANSI: true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
			repo := newCommandGitRepo(t)
			home := t.TempDir()
			sessionDir := filepath.Join(home, "sessions", "2026", "06", "17")
			if err := os.MkdirAll(sessionDir, 0o750); err != nil {
				t.Fatalf("mkdir sessions: %v", err)
			}
			tracePath := filepath.Join(sessionDir, "color.jsonl")
			trace := strings.Join([]string{
				`{"type":"session_meta","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"session_meta","cwd":"` + repo + `"}}`,
				`{"type":"response_item","timestamp":"2026-06-17T00:00:01Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"go test ./..."}}}`,
				`{"type":"response_item","timestamp":"2026-06-17T00:00:02Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}`,
			}, "\n") + "\n"
			if err := os.WriteFile(tracePath, []byte(trace), 0o600); err != nil {
				t.Fatalf("write trace: %v", err)
			}

			stdout, _, err := executeCommand(t, "--color", tc.mode, "--repo", repo, "start", "--watch", "--codex-home", home, "--watch-existing", "--watch-interval", "1ms", "--watch-duration", "5ms")
			if err != nil {
				t.Fatalf("start --watch returned error: %v\n%s", err, stdout)
			}
			hasANSI := strings.Contains(stdout, "\x1b[")
			if hasANSI != tc.wantANSI {
				t.Fatalf("ANSI presence = %v, want %v\noutput:\n%q", hasANSI, tc.wantANSI, stdout)
			}
			if !strings.Contains(stdout, "codex") || !strings.Contains(stdout, "run go test ./...") {
				t.Fatalf("watch output missing expected event text:\n%s", stdout)
			}
			if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
				t.Fatalf("stop returned error: %v", err)
			}
		})
	}
}

func TestInspectCodexCommand(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "06", "16")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "rollout-test.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	stdout, _, err := executeCommand(t, "inspect", "codex", "--home", home, "--last")
	if err != nil {
		t.Fatalf("inspect codex returned error: %v", err)
	}
	if !strings.Contains(stdout, "Candidates: 1") || !strings.Contains(stdout, sessionPath) {
		t.Fatalf("inspect output = %q", stdout)
	}
}

func TestInspectCodexCommandReportsMissingLogs(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCommand(t, "inspect", "codex", "--home", t.TempDir())
	if err != nil {
		t.Fatalf("inspect codex returned error: %v", err)
	}
	if !strings.Contains(stdout, "Candidates: 0") || !strings.Contains(stdout, "warning[codex_logs_missing]") {
		t.Fatalf("missing inspect output = %q", stdout)
	}
}

func TestMarkCommandRequiresMessage(t *testing.T) {
	t.Parallel()

	if _, _, err := executeCommand(t, "mark"); err == nil {
		t.Fatal("mark without a message returned nil error")
	}
}

func TestMarkCommandWritesSignedManualMarker(t *testing.T) {
	repo := newCommandGitRepo(t)
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	stdout, _, err := executeCommand(t, "--repo", repo, "start")
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(stdout, "Started AgentReceipt session "))

	stdout, _, err = executeCommand(t, "--repo", repo, "mark", "reviewed", "auth")
	if err != nil {
		t.Fatalf("mark returned error: %v", err)
	}
	if !strings.Contains(stdout, "reviewed auth") {
		t.Fatalf("mark output missing joined message: %q", stdout)
	}
	layout, err := storage.NewLayout(repo, sessionID)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	events, err := eventlog.ReadFile(layout.EventsJSONL)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	last := events[len(events)-1]
	if last.Source != "manual_marker" || last.Type != "manual.marker" {
		t.Fatalf("last event is not manual marker: %+v", last)
	}
	if last.Payload["message"] != "reviewed auth" || last.Payload["signature"] == "" {
		t.Fatalf("marker payload missing message/signature: %+v", last.Payload)
	}
}

func TestMarkCommandRequiresActiveSession(t *testing.T) {
	repo := newCommandGitRepo(t)
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	if _, _, err := executeCommand(t, "--repo", repo, "mark", "reviewed"); err == nil {
		t.Fatal("mark without active session returned nil error")
	}
}

func TestPRCommentRequiresGitHubCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, _, err := executeCommand(t, "pr", "comment"); err == nil || !strings.Contains(err.Error(), "GitHub CLI gh is required") {
		t.Fatalf("pr comment error = %v", err)
	}
}

func TestPRCommentReportsMissingCurrentPR(t *testing.T) {
	repo := newCommandGitRepo(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	binDir := t.TempDir()
	ghPath := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\necho no pull request >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(gitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, _, err := executeCommand(t, "--repo", repo, "pr", "comment"); err == nil || !strings.Contains(err.Error(), "no current pull request detected") {
		t.Fatalf("pr comment error = %v", err)
	}
}

func TestPRCommentPostsGeneratedMarkdownWithGitHubCLI(t *testing.T) {
	repo := newCommandGitRepo(t)
	t.Setenv("AGENTRECEIPT_KEY_DIR", filepath.Join(t.TempDir(), "keys"))
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	if _, _, err := executeCommand(t, "--repo", repo, "start"); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	if _, _, err := executeCommand(t, "--repo", repo, "stop"); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")
	ghScript := `#!/bin/sh
if [ "$1 $2" = "pr view" ]; then
  echo '{"number":1}'
  exit 0
fi
if [ "$1 $2 $3 $4" = "pr comment --body-file -" ]; then
  body="$(cat)"
  test -n "$body" || exit 2
  printf '%s' "$body" | grep -q "## AgentReceipt" || exit 3
  echo commented >> "` + logPath + `"
  exit 0
fi
exit 4
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(ghScript), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(gitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	stdout, _, err := executeCommand(t, "--repo", repo, "pr", "comment")
	if err != nil {
		t.Fatalf("pr comment returned error: %v", err)
	}
	if !strings.Contains(stdout, "Posted AgentReceipt PR comment.") {
		t.Fatalf("pr comment output = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agentreceipt")); !os.IsNotExist(err) {
		t.Fatalf("pr comment polluted repo storage: %v", err)
	}
	if data, err := os.ReadFile(logPath); err != nil || !strings.Contains(string(data), "commented") {
		t.Fatalf("fake gh was not invoked data=%q err=%v", data, err)
	}
}

func executeCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := NewRootCommand("test-version")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()

	return stdout.String(), stderr.String(), err
}

func newCommandGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repo := t.TempDir()
	runCommandGit(t, repo, "init")
	runCommandGit(t, repo, "config", "user.email", "agentreceipt@example.test")
	runCommandGit(t, repo, "config", "user.name", "AgentReceipt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runCommandGit(t, repo, "add", "README.md")
	runCommandGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runCommandGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func copyCommandReceiptBundle(t *testing.T, source string) string {
	t.Helper()
	dest := t.TempDir()
	for _, relative := range []string{
		storage.ReceiptJSONFile,
		storage.ManifestFile,
		storage.EventsFile,
		filepath.Join(storage.DiffsDir, storage.FinalPatchFile),
	} {
		sourcePath := filepath.Join(source, relative)
		destPath := filepath.Join(dest, relative)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
			t.Fatalf("mkdir bundle path: %v", err)
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read bundle file %s: %v", sourcePath, err)
		}
		if err := os.WriteFile(destPath, data, 0o600); err != nil {
			t.Fatalf("write bundle file %s: %v", destPath, err)
		}
	}

	return dest
}

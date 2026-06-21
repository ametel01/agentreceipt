package instructions

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

const (
	Source                = "instruction_capture"
	TypeInstructionFile   = "instruction.file"
	instructionEventIDSrc = "evt_instruction_file_"
	maxSummaryCount       = 3
	maxSummaryRunes       = 120
)

const (
	warningCodeUnreadableInstructionFileTemplate = "instruction_capture.unreadable_%s"
	warningCodeNonRegularInstructionFileTemplate = "instruction_capture.non_regular_%s"
)

var instructionFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
}

// CaptureInstructionFiles reads repository instruction files from session start.
func CaptureInstructionFiles(repoRoot string, sessionID string) ([]model.Event, []model.Warning, error) {
	if repoRoot == "" {
		return nil, nil, errors.New("repo root is required")
	}
	if sessionID == "" {
		return nil, nil, errors.New("session ID is required")
	}

	events := make([]model.Event, 0, len(instructionFiles))
	warnings := make([]model.Warning, 0)
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("open repo root: %w", err)
	}
	defer func() { _ = root.Close() }()

	for _, instructionFile := range instructionFiles {
		info, err := root.Stat(instructionFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			warnings = append(warnings, model.Warning{
				Code:    formatWarningCode(warningCodeUnreadableInstructionFileTemplate, instructionFile),
				Message: fmt.Sprintf("Unable to inspect instruction file %s: %v", instructionFile, err),
			})
			continue
		}
		if !info.Mode().IsRegular() {
			warnings = append(warnings, model.Warning{
				Code:    formatWarningCode(warningCodeNonRegularInstructionFileTemplate, instructionFile),
				Message: fmt.Sprintf("Instruction file is not a regular file: %s", instructionFile),
			})
			continue
		}

		content, err := root.ReadFile(instructionFile)
		if err != nil {
			warnings = append(warnings, model.Warning{
				Code:    formatWarningCode(warningCodeUnreadableInstructionFileTemplate, instructionFile),
				Message: fmt.Sprintf("Unable to inspect instruction file %s: %v", instructionFile, err),
			})
			continue
		}

		hash, err := fileSHA256(content)
		if err != nil {
			warnings = append(warnings, model.Warning{
				Code:    formatWarningCode(warningCodeUnreadableInstructionFileTemplate, instructionFile),
				Message: fmt.Sprintf("Unable to hash instruction file %s: %v", instructionFile, err),
			})
			continue
		}

		events = append(events, model.Event{
			EventID:   instructionEventID(sessionID, instructionFile),
			SessionID: sessionID,
			Timestamp: time.Now().UTC(),
			Source:    Source,
			Type:      TypeInstructionFile,
			CWD:       repoRoot,
			Payload: map[string]any{
				"path":    filepath.ToSlash(instructionFile),
				"hash":    hash,
				"size":    info.Size(),
				"mtime":   info.ModTime().UTC().Format(time.RFC3339Nano),
				"summary": summarizeInstructionFileContent(content),
			},
		})
	}

	return events, warnings, nil
}

func instructionEventID(sessionID, filePath string) string {
	raw := fmt.Sprintf("%s:%s:%s", sessionID, filePath, runtime.Version())
	sum := sha256.Sum256([]byte(raw))
	return instructionEventIDSrc + hex.EncodeToString(sum[:6])
}

func fileSHA256(content []byte) (string, error) {
	hasher := sha256.New()
	if _, err := hasher.Write(content); err != nil {
		return "", err
	}
	sum := hasher.Sum(nil)

	return "sha256:" + hex.EncodeToString(sum), nil
}

func summarizeInstructionFileContent(content []byte) []string {
	text := strings.TrimSpace(string(content))
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	candidates := make([]string, 0, maxSummaryCount)
	seen := make(map[string]struct{}, len(lines))

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "\ufeff")
		if isSummaryCandidate(line) {
			line = summarizeInstructionLine(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			candidates = append(candidates, line)
			if len(candidates) == maxSummaryCount {
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	return candidates
}

func isSummaryCandidate(line string) bool {
	if strings.HasPrefix(line, "#") {
		return true
	}
	switch {
	case strings.HasPrefix(line, "-"), strings.HasPrefix(line, "*"), strings.HasPrefix(line, "+"):
		return true
	case strings.Contains(line, ":"):
		return true
	default:
		return len(strings.Fields(line)) >= 3
	}
}

func summarizeInstructionLine(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "#"):
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line != "" {
			return summarizeWithPrefix("heading: ", line)
		}
	case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ "):
		return summarizeWithPrefix("rule: ", strings.TrimSpace(line[2:]))
	case strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "+"):
		if len(line) > 2 {
			return summarizeWithPrefix("rule: ", strings.TrimSpace(line[1:]))
		}
	}
	return truncateLine(line, 0)
}

func summarizeWithPrefix(prefix, value string) string {
	return prefix + truncateLine(value, len([]rune(prefix)))
}

func truncateLine(value string, prefixLen int) string {
	runes := []rune(value)
	maxLen := maxSummaryRunes
	if prefixLen > 0 {
		if prefixLen < maxLen {
			maxLen -= prefixLen
		} else {
			maxLen = 0
		}
	}
	if len(runes) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}

	return string(runes[:maxLen-3]) + "..."
}

func formatWarningCode(pattern string, filePath string) string {
	base := filepath.Base(filePath)
	return strings.ToLower(fmt.Sprintf(pattern, strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))))
}

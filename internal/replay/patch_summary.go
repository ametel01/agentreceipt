package replay

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ametel01/agentreceipt/internal/capture/fswatcher"
)

const (
	patchCategoryProduction         = "production"
	patchCategoryTest               = "test"
	patchCategoryDocs               = "docs"
	patchCategoryConfig             = "config"
	patchCategoryDependency         = "dependency"
	patchCategoryGeneratedOrUnknown = "generated_or_unknown"
)

type PatchSummary struct {
	FileCounts                           PatchFileCounts    `json:"file_counts"`
	Additions                            int                `json:"additions"`
	Deletions                            int                `json:"deletions"`
	ChangedFiles                         []PatchSummaryFile `json:"changed_files"`
	ChangedGoSymbols                     []string           `json:"changed_go_symbols,omitempty"`
	TestsChanged                         bool               `json:"tests_changed"`
	ProductionChangedWithoutTestsChanged bool               `json:"production_changed_without_tests_changed"`
}

type PatchFileCounts struct {
	Production         int `json:"production"`
	Test               int `json:"test"`
	Docs               int `json:"docs"`
	Config             int `json:"config"`
	Dependency         int `json:"dependency"`
	GeneratedOrUnknown int `json:"generated_or_unknown"`
}

type PatchSummaryFile struct {
	Path         string   `json:"path"`
	Action       string   `json:"action"`
	Category     string   `json:"category"`
	Sensitive    bool     `json:"sensitive"`
	Dependency   bool     `json:"dependency"`
	Symbols      []string `json:"symbols,omitempty"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type patchBlock struct {
	paths     []string
	action    string
	additions int
	deletions int
	symbols   map[string]struct{}
}

var goSymbolPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*func\s+(?:\([^)]+\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
	regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s`),
	regexp.MustCompile(`^\s*(?:var|const)\s+([A-Za-z_][A-Za-z0-9_]*)\b`),
}

func buildPatchSummary(finalPatch string, classifier fswatcher.Classifier) (PatchSummary, []string) {
	summary := PatchSummary{
		FileCounts: PatchFileCounts{},
	}
	trimmed := strings.TrimSpace(finalPatch)
	if trimmed == "" {
		return summary, nil
	}

	blocks, malformed := parsePatchBlocks(finalPatch)
	if len(blocks) == 0 {
		return summary, []string{"Unable to parse final patch"}
	}

	fileSummary := map[string]PatchSummaryFile{}
	symbolSet := map[string]struct{}{}
	for _, block := range blocks {
		summary.Additions += block.additions
		summary.Deletions += block.deletions
		for _, path := range block.paths {
			if path == "" {
				continue
			}
			classification := classifier.Classify(path)
			category := classifyPatchCategory(path, classification)
			file, ok := fileSummary[path]
			if !ok {
				file = PatchSummaryFile{
					Path:         path,
					Action:       block.action,
					Category:     category,
					Sensitive:    classification.Sensitive,
					Dependency:   classification.Dependency,
					EvidenceRefs: []string{finalPatchEvidenceRef},
				}
			}
			file.Action = combineAction(file.Action, block.action)
			file.Symbols = uniqueSorted(append(file.Symbols, block.symbolList()...))
			file.EvidenceRefs = uniqueSorted(append(file.EvidenceRefs, finalPatchEvidenceRef))
			fileSummary[path] = file
		}
		for symbol := range block.symbols {
			symbolSet[symbol] = struct{}{}
		}
	}

	files := make([]PatchSummaryFile, 0, len(fileSummary))
	for _, file := range fileSummary {
		files = append(files, file)
		switch file.Category {
		case patchCategoryProduction:
			summary.FileCounts.Production++
		case patchCategoryTest:
			summary.FileCounts.Test++
		case patchCategoryDocs:
			summary.FileCounts.Docs++
		case patchCategoryConfig:
			summary.FileCounts.Config++
		case patchCategoryDependency:
			summary.FileCounts.Dependency++
		default:
			summary.FileCounts.GeneratedOrUnknown++
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Path == files[j].Path {
			return files[i].Action < files[j].Action
		}
		return files[i].Path < files[j].Path
	})

	summary.ChangedFiles = files
	summary.TestsChanged = summary.FileCounts.Test > 0
	summary.ProductionChangedWithoutTestsChanged = summary.FileCounts.Production > 0 && summary.FileCounts.Test == 0

	summary.ChangedGoSymbols = make([]string, 0, len(symbolSet))
	for symbol := range symbolSet {
		summary.ChangedGoSymbols = append(summary.ChangedGoSymbols, symbol)
	}
	sort.Strings(summary.ChangedGoSymbols)

	gaps := make([]string, 0)
	if malformed {
		gaps = append(gaps, "Unable to parse final patch")
	}

	return summary, gaps
}

func (b patchBlock) symbolList() []string {
	symbols := make([]string, 0, len(b.symbols))
	for symbol := range b.symbols {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func parsePatchBlocks(patch string) ([]patchBlock, bool) {
	lines := strings.Split(patch, "\n")
	blocks := make([]patchBlock, 0)
	var current *patchBlock
	malformed := false

	flush := func() {
		if current == nil {
			return
		}
		if len(current.paths) == 0 {
			malformed = true
			current = nil
			return
		}
		current.paths = uniqueSorted(current.paths)
		blocks = append(blocks, *current)
		current = nil
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			parts := strings.Fields(line)
			if len(parts) < 4 {
				malformed = true
				continue
			}
			left := diffPath(parts[2])
			right := diffPath(parts[3])
			paths := make([]string, 0, 2)
			if right != "" {
				paths = append(paths, right)
			}
			if left != "" && left != right {
				paths = append(paths, left)
			}
			current = &patchBlock{
				paths:   paths,
				action:  "modify",
				symbols: map[string]struct{}{},
			}
			continue
		}
		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			current.action = "add"
		case strings.HasPrefix(line, "deleted file mode "):
			current.action = "delete"
		case strings.HasPrefix(line, "rename from "):
			current.action = "rename"
			left := diffPath(strings.TrimPrefix(line, "rename from "))
			if left != "" {
				current.paths = append(current.paths, left)
			}
		case strings.HasPrefix(line, "rename to "):
			current.action = "rename"
			right := diffPath(strings.TrimPrefix(line, "rename to "))
			if right != "" {
				current.paths = append(current.paths, right)
			}
		case strings.HasPrefix(line, "@@"):
			collectPatchSymbols(current.symbols, line)
		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
			continue
		default:
			switch {
			case strings.HasPrefix(rawLine, "+") && !strings.HasPrefix(rawLine, "+++"):
				current.additions++
				collectPatchSymbols(current.symbols, rawLine[1:])
			case strings.HasPrefix(rawLine, "-") && !strings.HasPrefix(rawLine, "---"):
				current.deletions++
				collectPatchSymbols(current.symbols, rawLine[1:])
			}
		}
	}

	flush()
	if len(blocks) == 0 && strings.TrimSpace(patch) != "" {
		malformed = true
	}

	return blocks, malformed
}

func collectPatchSymbols(symbols map[string]struct{}, text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if idx := strings.LastIndex(trimmed, "@@"); idx >= 0 {
		suffix := strings.TrimSpace(trimmed[idx+2:])
		if suffix != "" {
			trimmed = suffix
		}
	}
	trimmed = strings.TrimLeft(trimmed, "+- ")
	for _, pattern := range goSymbolPatterns {
		matches := pattern.FindStringSubmatch(trimmed)
		if len(matches) != 2 {
			continue
		}
		if matches[1] != "" {
			symbols[matches[1]] = struct{}{}
		}
	}
}

func classifyPatchCategory(path string, classification fswatcher.Classification) string {
	normalized := strings.ToLower(filepath.ToSlash(path))
	switch {
	case classification.Dependency:
		return patchCategoryDependency
	case isPatchTestPath(normalized):
		return patchCategoryTest
	case isPatchDocsPath(normalized):
		return patchCategoryDocs
	case isPatchConfigPath(normalized):
		return patchCategoryConfig
	case isPatchGeneratedPath(normalized):
		return patchCategoryGeneratedOrUnknown
	default:
		return patchCategoryProduction
	}
}

func isPatchTestPath(path string) bool {
	switch {
	case strings.Contains(path, "/test/"), strings.Contains(path, "/tests/"):
		return true
	case strings.HasSuffix(path, "_test.go"), strings.HasSuffix(path, ".test.go"), strings.HasSuffix(path, ".spec.go"):
		return true
	case strings.HasSuffix(path, ".test.js"), strings.HasSuffix(path, ".test.ts"), strings.HasSuffix(path, ".spec.js"), strings.HasSuffix(path, ".spec.ts"):
		return true
	}
	return false
}

func isPatchDocsPath(path string) bool {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(path, "docs/"), strings.HasPrefix(path, "doc/"):
		return true
	case strings.HasPrefix(base, "readme"), strings.HasPrefix(base, "changelog"), strings.HasPrefix(base, "contributing"), strings.HasPrefix(base, "license"):
		return true
	case strings.HasSuffix(path, ".md"), strings.HasSuffix(path, ".rst"), strings.HasSuffix(path, ".adoc"), strings.HasSuffix(path, ".txt"), strings.HasSuffix(path, ".org"):
		return true
	}
	return false
}

func isPatchConfigPath(path string) bool {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(path, ".github/"):
		return true
	case base == "Makefile", base == "Dockerfile", base == "docker-compose.yml", base == "docker-compose.yaml":
		return true
	case base == ".editorconfig", base == ".gitignore", base == ".gitattributes":
		return true
	case strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".toml"), strings.HasSuffix(path, ".ini"), strings.HasSuffix(path, ".cfg"), strings.HasSuffix(path, ".conf"), strings.HasSuffix(path, ".json"):
		return true
	case strings.HasPrefix(base, "go.work"), strings.HasPrefix(base, "tsconfig"), strings.HasPrefix(base, "jest.config"), strings.HasPrefix(base, "vitest.config"):
		return true
	}
	return false
}

func isPatchGeneratedPath(path string) bool {
	base := filepath.Base(path)
	switch {
	case strings.Contains(path, "/generated/"), strings.Contains(path, "/gen/"), strings.Contains(path, "/vendor/"):
		return true
	case strings.HasSuffix(base, ".gen.go"), strings.HasSuffix(base, ".generated.go"), strings.HasSuffix(base, ".pb.go"), strings.HasSuffix(base, ".pb.gw.go"), strings.HasSuffix(base, ".mock.go"), strings.HasSuffix(base, ".min.js"), strings.HasSuffix(base, ".bundle.js"):
		return true
	case strings.HasSuffix(base, ".png"), strings.HasSuffix(base, ".jpg"), strings.HasSuffix(base, ".jpeg"), strings.HasSuffix(base, ".gif"), strings.HasSuffix(base, ".bmp"), strings.HasSuffix(base, ".webp"), strings.HasSuffix(base, ".ico"), strings.HasSuffix(base, ".pdf"), strings.HasSuffix(base, ".zip"), strings.HasSuffix(base, ".tar"), strings.HasSuffix(base, ".gz"), strings.HasSuffix(base, ".xz"), strings.HasSuffix(base, ".bz2"), strings.HasSuffix(base, ".7z"), strings.HasSuffix(base, ".mp3"), strings.HasSuffix(base, ".mp4"), strings.HasSuffix(base, ".avi"), strings.HasSuffix(base, ".mov"), strings.HasSuffix(base, ".woff"), strings.HasSuffix(base, ".woff2"), strings.HasSuffix(base, ".ttf"), strings.HasSuffix(base, ".otf"), strings.HasSuffix(base, ".wasm"), strings.HasSuffix(base, ".bin"), strings.HasSuffix(base, ".exe"), strings.HasSuffix(base, ".dll"), strings.HasSuffix(base, ".so"), strings.HasSuffix(base, ".dylib"), strings.HasSuffix(base, ".apk"), strings.HasSuffix(base, ".ipa"):
		return true
	}
	return false
}

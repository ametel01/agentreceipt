package replay

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBuildIncludesPatchSummaryForGoChanges(t *testing.T) {
	t.Parallel()

	report := buildPatchSummaryReport(t, `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@
 package main
-func oldName() {}
+func NewName() {}
+const patchSecret = "magic-diff-body-token"
`)

	if report.PatchSummary.FileCounts.Production != 1 {
		t.Fatalf("production count = %d", report.PatchSummary.FileCounts.Production)
	}
	if report.PatchSummary.TestsChanged {
		t.Fatalf("tests_changed = true")
	}
	if !report.PatchSummary.ProductionChangedWithoutTestsChanged {
		t.Fatalf("production_changed_without_tests_changed = false")
	}
	if report.PatchSummary.Additions == 0 || report.PatchSummary.Deletions == 0 {
		t.Fatalf("additions/deletions = %+v/%+v", report.PatchSummary.Additions, report.PatchSummary.Deletions)
	}
	file := findPatchSummaryFile(report.PatchSummary.ChangedFiles, "main.go")
	if file == nil {
		t.Fatalf("main.go missing from patch summary: %+v", report.PatchSummary.ChangedFiles)
	}
	if file.Category != patchCategoryProduction {
		t.Fatalf("main.go category = %q", file.Category)
	}
	if !containsString(file.Symbols, "NewName") {
		t.Fatalf("main.go symbols = %+v", file.Symbols)
	}
	if !containsString(report.PatchSummary.ChangedGoSymbols, "NewName") {
		t.Fatalf("changed_go_symbols = %+v", report.PatchSummary.ChangedGoSymbols)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(raw), "magic-diff-body-token") {
		t.Fatalf("patch body leaked into replay JSON: %s", raw)
	}
}

func TestBuildClassifiesPatchSummaryCategories(t *testing.T) {
	t.Parallel()

	report := buildPatchSummaryReport(t, `diff --git a/main_test.go b/main_test.go
index 1111111..2222222 100644
--- a/main_test.go
+++ b/main_test.go
@@ -1 +1,2 @@
+package main
+func TestMain() {}
diff --git a/README.md b/README.md
index 1111111..2222222 100644
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
+# Docs
diff --git a/go.mod b/go.mod
index 1111111..2222222 100644
--- a/go.mod
+++ b/go.mod
@@ -1 +1,2 @@
+module example.com/project
diff --git a/.github/workflows/ci.yaml b/.github/workflows/ci.yaml
index 1111111..2222222 100644
--- a/.github/workflows/ci.yaml
+++ b/.github/workflows/ci.yaml
@@ -1 +1,2 @@
+name: CI
`)

	if report.PatchSummary.FileCounts.Test != 1 {
		t.Fatalf("test count = %d", report.PatchSummary.FileCounts.Test)
	}
	if report.PatchSummary.FileCounts.Docs != 1 {
		t.Fatalf("docs count = %d", report.PatchSummary.FileCounts.Docs)
	}
	if report.PatchSummary.FileCounts.Dependency != 1 {
		t.Fatalf("dependency count = %d", report.PatchSummary.FileCounts.Dependency)
	}
	if report.PatchSummary.FileCounts.Config != 1 {
		t.Fatalf("config count = %d", report.PatchSummary.FileCounts.Config)
	}
	if report.PatchSummary.FileCounts.Production != 0 {
		t.Fatalf("production count = %d", report.PatchSummary.FileCounts.Production)
	}
	if !report.PatchSummary.TestsChanged {
		t.Fatalf("tests_changed = false")
	}
	if report.PatchSummary.ProductionChangedWithoutTestsChanged {
		t.Fatalf("production_changed_without_tests_changed = true")
	}
	if findPatchSummaryFile(report.PatchSummary.ChangedFiles, "main_test.go") == nil {
		t.Fatalf("missing main_test.go in patch summary")
	}
	if findPatchSummaryFile(report.PatchSummary.ChangedFiles, "README.md") == nil {
		t.Fatalf("missing README.md in patch summary")
	}
	if findPatchSummaryFile(report.PatchSummary.ChangedFiles, "go.mod") == nil {
		t.Fatalf("missing go.mod in patch summary")
	}
	if findPatchSummaryFile(report.PatchSummary.ChangedFiles, ".github/workflows/ci.yaml") == nil {
		t.Fatalf("missing ci.yaml in patch summary")
	}
}

func TestBuildIncludesRenameAndBinaryPatchSummaryEntries(t *testing.T) {
	t.Parallel()

	report := buildPatchSummaryReport(t, `diff --git a/assets/logo.png b/assets/logo.png
index 1111111..2222222 100644
Binary files a/assets/logo.png and b/assets/logo.png differ
diff --git a/internal/old.go b/internal/new.go
similarity index 100%
rename from internal/old.go
rename to internal/new.go
`)

	if report.PatchSummary.FileCounts.GeneratedOrUnknown != 1 {
		t.Fatalf("generated_or_unknown count = %d", report.PatchSummary.FileCounts.GeneratedOrUnknown)
	}
	if report.PatchSummary.FileCounts.Production != 2 {
		t.Fatalf("production count = %d", report.PatchSummary.FileCounts.Production)
	}
	if findPatchSummaryFile(report.PatchSummary.ChangedFiles, "assets/logo.png") == nil {
		t.Fatalf("missing binary file entry: %+v", report.PatchSummary.ChangedFiles)
	}
	renamed := findPatchSummaryFile(report.PatchSummary.ChangedFiles, "internal/new.go")
	if renamed == nil || renamed.Action != "rename" {
		t.Fatalf("rename entry = %+v", renamed)
	}
	if len(report.PatchSummary.ChangedGoSymbols) != 0 {
		t.Fatalf("changed_go_symbols = %+v", report.PatchSummary.ChangedGoSymbols)
	}
}

func TestBuildMarksMalformedFinalPatchWithGap(t *testing.T) {
	t.Parallel()

	report := buildPatchSummaryReport(t, "not a patch\n")
	if !containsGap(report.Gaps, "Unable to parse final patch") {
		t.Fatalf("gaps = %+v", report.Gaps)
	}
	if len(report.PatchSummary.ChangedFiles) != 0 {
		t.Fatalf("patch summary should be empty: %+v", report.PatchSummary)
	}
}

func buildPatchSummaryReport(t *testing.T, patch string) Report {
	t.Helper()

	repo, sessionID, layout := finalizedReplaySession(t, nil, nil)
	if err := os.WriteFile(layout.FinalPatch, []byte(patch), 0o600); err != nil {
		t.Fatalf("write final patch: %v", err)
	}

	report, err := Build(context.Background(), Options{
		RepoPath:  repo,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return report
}

func findPatchSummaryFile(files []PatchSummaryFile, path string) *PatchSummaryFile {
	for index := range files {
		if files[index].Path == path {
			return &files[index]
		}
	}
	return nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

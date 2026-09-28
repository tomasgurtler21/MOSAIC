package artifact_test

// Tests for the ASCII summary truncation rule: a summary over 100 characters
// keeps its first 50 and last 50 characters joined by " ... " (ASCII), and a
// fully rendered Orchestration.md contains only ASCII bytes.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

func TestTruncateSummary_OneOverBoundary_TruncatedWithAsciiDelimiter(t *testing.T) {
	input := strings.Repeat("a", 50) + "X" + strings.Repeat("b", 50) // 101 chars

	got := artifact.TruncateSummary(input)

	want := strings.Repeat("a", 50) + " ... " + strings.Repeat("b", 50)
	if got != want {
		t.Errorf("TruncateSummary(101 chars):\n  want: %q\n   got: %q", want, got)
	}
}

func TestTruncateSummary_Result_ContainsOnlyAscii(t *testing.T) {
	input := strings.Repeat("x", 500)

	got := artifact.TruncateSummary(input)

	for i := 0; i < len(got); i++ {
		if got[i] > 0x7F {
			t.Fatalf("TruncateSummary result has non-ASCII byte 0x%02x at offset %d: %q", got[i], i, got)
		}
	}
}

func TestApply_LongSummary_FullyRenderedArtifactIsAsciiOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	settings := domain.RunSettings{
		Mode:                 domain.ExecutionModeAutoReview,
		Checkpoints:          true,
		Commits:              true,
		CommitBranch:         domain.MOSAICRunBranchName(testRunID),
		PreConsultation:      true,
		ReviewLoopLimit:      3,
		InfraClassSelections: map[string]string{"commit": "commit-manager-git"},
	}
	state, err := store.Create(ctx, info, "task", settings, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), testRunID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	long := strings.Repeat("h", 50) + strings.Repeat("m", 60) + strings.Repeat("t", 50)
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil)
	step.Summary = long

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading artifact: %v", err)
	}
	for i, b := range raw {
		if b > 0x7F {
			t.Fatalf("Orchestration.md contains non-ASCII byte 0x%02x at offset %d", b, i)
		}
	}
	if !strings.Contains(string(raw), strings.Repeat("h", 50)+" ... "+strings.Repeat("t", 50)) {
		t.Errorf("summary must be head-50 + \" ... \" + tail-50 in the log row, got:\n%s", raw)
	}
}

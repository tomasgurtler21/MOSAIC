package artifact_test

// Tests for fileStore.AdoptRunnerSettings: set-once recording of the three
// Runner-owned settings on a native-created artifact, leaving every other part
// of the artifact alone, and for the cross-executor guarantee that a rewrite
// which keeps unknown keys leaves the runner settings intact.

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

// nativeArtifactStore writes a native-created artifact (no runner settings,
// plus the given extra frontmatter lines) and returns a store over it and the
// file path.
func nativeArtifactStore(t *testing.T, extraLines string) (domain.ArtifactStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	if err := os.WriteFile(path, minimalArtifactWithConfigBytes(standardConfigLines+extraLines), 0o600); err != nil {
		t.Fatalf("write native artifact: %v", err)
	}
	return artifact.NewFileStore(path), path
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestAdoptRunnerSettings_RecordsAllThreeAndPersistsThem(t *testing.T) {
	store, _ := nativeArtifactStore(t, "")
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	updated, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeAutoReview, true, true, now)

	if err != nil {
		t.Fatalf("AdoptRunnerSettings: unexpected error: %v", err)
	}
	if updated.Mode != domain.ExecutionModeAutoReview || !updated.PreConsultation || !updated.ManualResolution {
		t.Errorf("returned settings = (%q, %v, %v), want (auto-review, true, true)",
			updated.Mode, updated.PreConsultation, updated.ManualResolution)
	}
	reread, err := store.Read(context.Background())
	if err != nil {
		t.Fatalf("Read after adoption: %v", err)
	}
	if reread.Mode != domain.ExecutionModeAutoReview || !reread.PreConsultation || !reread.ManualResolution {
		t.Errorf("persisted settings = (%q, %v, %v), want (auto-review, true, true)",
			reread.Mode, reread.PreConsultation, reread.ManualResolution)
	}
	if !reread.LastUpdated.Equal(now) {
		t.Errorf("LastUpdated = %v, want %v", reread.LastUpdated, now)
	}
}

func TestAdoptRunnerSettings_WritesRunnerKeysAndNoLegacyAliases(t *testing.T) {
	store, path := nativeArtifactStore(t, "")

	if _, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeAuto, false, true, time.Now()); err != nil {
		t.Fatalf("AdoptRunnerSettings: %v", err)
	}

	text := mustReadFile(t, path)
	for _, want := range []string{"runner_mode: auto", "runner_pre_consultation:", "runner_manual_resolution:"} {
		if !strings.Contains(text, want) {
			t.Errorf("artifact lacks %q after adoption:\n%s", want, text)
		}
	}
	for _, legacy := range []string{"\nmode:", "\npre_consultation:", "\nmanual_resolution:"} {
		if strings.Contains(text, legacy) {
			t.Errorf("artifact carries legacy key %q after adoption:\n%s", strings.TrimSpace(legacy), text)
		}
	}
}

// Adoption changes the three settings and last_updated only. A recorded
// review_loop_limit and native-only keys stay as they were.
func TestAdoptRunnerSettings_LeavesOtherFieldsAndUnknownKeysUnchanged(t *testing.T) {
	store, path := nativeArtifactStore(t, "review_loop_limit: 4\nnative_note: keep me\n")

	if _, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeAuto, false, false, time.Now()); err != nil {
		t.Fatalf("AdoptRunnerSettings: %v", err)
	}

	reread, err := store.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reread.ReviewLoopLimit != 4 {
		t.Errorf("ReviewLoopLimit = %d, want 4 (untouched)", reread.ReviewLoopLimit)
	}
	if !strings.Contains(mustReadFile(t, path), "native_note: keep me") {
		t.Error("the native-only frontmatter key was dropped by adoption")
	}
}

// Adoption never writes a review_loop_limit for an artifact that records none.
func TestAdoptRunnerSettings_DoesNotWriteReviewLoopLimit(t *testing.T) {
	store, path := nativeArtifactStore(t, "")

	if _, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeAuto, false, false, time.Now()); err != nil {
		t.Fatalf("AdoptRunnerSettings: %v", err)
	}

	if strings.Contains(mustReadFile(t, path), "review_loop_limit") {
		t.Error("adoption wrote review_loop_limit; it must stay absent")
	}
}

func TestAdoptRunnerSettings_AlreadyRecorded_RefusedAndFileUnchanged(t *testing.T) {
	store, path := nativeArtifactStore(t, "")
	if _, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeAuto, false, false, time.Now()); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	before := mustReadFile(t, path)

	_, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeOrchestrated, true, true, time.Now().Add(time.Hour))

	asRefusalError(t, err)
	if after := mustReadFile(t, path); after != before {
		t.Errorf("a refused adoption changed the artifact:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestAdoptRunnerSettings_RunnerCreatedArtifact_Refused(t *testing.T) {
	store := artifact.NewFileStore(filepath.Join(t.TempDir(), "Orchestration.md"))
	_, err := store.Create(context.Background(), domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}, "task",
		domain.RunSettings{Mode: domain.ExecutionModeAuto}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), testRunID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeOrchestrated, false, false, time.Now())

	asRefusalError(t, err)
	reread, readErr := store.Read(context.Background())
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}
	if reread.Mode != domain.ExecutionModeAuto {
		t.Errorf("Mode = %q after a refused adoption, want auto", reread.Mode)
	}
}

func TestAdoptRunnerSettings_UnsetMode_RefusedAndFileUnchanged(t *testing.T) {
	store, path := nativeArtifactStore(t, "")
	before := mustReadFile(t, path)

	_, err := store.AdoptRunnerSettings(context.Background(), domain.ExecutionModeUnset, true, true, time.Now())

	asRefusalError(t, err)
	if after := mustReadFile(t, path); after != before {
		t.Error("a refused adoption changed the artifact")
	}
}

// A Runner-created artifact that another executor rewrites, keeping the keys it
// does not know, still carries the runner settings, review loop limit and
// selections, and the Runner's next write keeps the other executor's keys.
func TestRunnerSettings_SurviveNativeStyleRewriteThatKeepsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()
	_, err := store.Create(ctx, domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}, "task",
		domain.RunSettings{
			Mode:                 domain.ExecutionModeAutoReview,
			PreConsultation:      true,
			ManualResolution:     true,
			ReviewLoopLimit:      6,
			InfraClassSelections: map[string]string{"checkpoint": "checkpoint-manager-git"},
		},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), testRunID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The other executor adds a key of its own and leaves everything else.
	text := mustReadFile(t, path)
	rewritten := strings.Replace(text, "current_state:", "native_note: written by native orchestration\ncurrent_state:", 1)
	if rewritten == text {
		t.Fatal("test setup: current_state key not found to anchor the native key")
	}
	if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("write rewritten artifact: %v", err)
	}

	step := newTestStep(1, "agent-a#1", "PLANNING", "", domain.StatusSUCCESS, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil)
	current, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after native rewrite: %v", err)
	}
	if _, err := store.Apply(ctx, current, step); err != nil {
		t.Fatalf("Apply after native rewrite: %v", err)
	}

	reread, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reread.Mode != domain.ExecutionModeAutoReview || !reread.PreConsultation || !reread.ManualResolution {
		t.Errorf("runner settings = (%q, %v, %v), want (auto-review, true, true)", reread.Mode, reread.PreConsultation, reread.ManualResolution)
	}
	if reread.ReviewLoopLimit != 6 {
		t.Errorf("ReviewLoopLimit = %d, want 6", reread.ReviewLoopLimit)
	}
	if reread.InfraClassSelections["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections = %v, want checkpoint-manager-git kept", reread.InfraClassSelections)
	}
	if !strings.Contains(mustReadFile(t, path), "native_note: written by native orchestration") {
		t.Error("the Runner's write dropped the native-only key")
	}
}

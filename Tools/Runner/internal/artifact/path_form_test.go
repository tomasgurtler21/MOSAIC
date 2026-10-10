package artifact_test

// Tests for the recorded path form: the Execution Log Inputs column and the
// Artifacts registry are written without the Orchestration-{run_id}/ prefix,
// project-file paths stay as dispatched, and a prefixed and an unprefixed form
// of one artifact denote the same registry entry.

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

func runPrefix() string { return domain.RunScopedFolder(testRunID) + "/" }

// storeInDir creates an artifact store whose file lives in dir.
func storeInDir(t *testing.T, dir string) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(context.Background(), info, "test task", domain.RunSettings{}, time.Now(), testRunID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, state
}

func stepWithInputs(seq int, inputs string, written []string) domain.CompletedStep {
	step := newTestStep(seq, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), written)
	step.Inputs = inputs
	return step
}

func lastInputs(state domain.ArtifactState) string {
	return state.ExecutionLog[len(state.ExecutionLog)-1].Inputs
}

func TestApply_Inputs_RunPrefixRemoved(t *testing.T) {
	store, state := mustCreateStore(t)
	step := stepWithInputs(1, runPrefix()+"Stage-2/Plan.md, "+runPrefix()+"Requirements.md", nil)

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if want := "Stage-2/Plan.md, Requirements.md"; lastInputs(after) != want {
		t.Errorf("Inputs: want %q, got %q", want, lastInputs(after))
	}
}

func TestApply_Inputs_ProjectFilePathsUnchanged(t *testing.T) {
	store, state := mustCreateStore(t)
	step := stepWithInputs(1, "src/main.go, "+runPrefix()+"Plan.md, docs/Design.md", nil)

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if want := "src/main.go, Plan.md, docs/Design.md"; lastInputs(after) != want {
		t.Errorf("Inputs: want %q, got %q", want, lastInputs(after))
	}
}

func TestApply_Inputs_OtherRunPrefixKept(t *testing.T) {
	store, state := mustCreateStore(t)
	other := "Orchestration-20990101T000000Z-ffff/Plan.md"
	step := stepWithInputs(1, other+", "+runPrefix()+"Plan.md", nil)

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if want := other + ", Plan.md"; lastInputs(after) != want {
		t.Errorf("Inputs: want %q, got %q", want, lastInputs(after))
	}
}

func TestApply_Inputs_EmptyStaysEmpty(t *testing.T) {
	store, state := mustCreateStore(t)

	after, err := store.Apply(context.Background(), state, stepWithInputs(1, "", nil))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if lastInputs(after) != "" {
		t.Errorf("Inputs: want empty, got %q", lastInputs(after))
	}
}

func TestApply_Inputs_OnDiskCellCarriesNoRunPrefix(t *testing.T) {
	dir := t.TempDir()
	store, state := storeInDir(t, dir)
	step := stepWithInputs(1, runPrefix()+"Stage-2/Plan.md", nil)
	if _, err := store.Apply(context.Background(), state, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "Orchestration.md"))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}

	text := string(data)
	if strings.Contains(text, "Orchestration-"+testRunID+"/") {
		t.Errorf("artifact still contains the run prefix:\n%s", text)
	}
	if !strings.Contains(text, "Stage-2/Plan.md") {
		t.Errorf("artifact lost the input path:\n%s", text)
	}
}

func TestApply_Registry_RunPrefixRemoved(t *testing.T) {
	store, state := mustCreateStore(t)
	step := stepWithInputs(1, "", []string{runPrefix() + "Stage-1/Plan.md", runPrefix() + "Design.md"})

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(after.ArtifactRegistry) != 2 {
		t.Fatalf("registry rows: want 2, got %v", after.ArtifactRegistry)
	}
	for i, want := range []string{"Stage-1/Plan.md", "Design.md"} {
		if got := after.ArtifactRegistry[i].Artifact; got != want {
			t.Errorf("registry row %d: want %q, got %q", i, want, got)
		}
	}
}

func TestApply_Registry_ProjectFilePathUnchanged(t *testing.T) {
	store, state := mustCreateStore(t)
	step := stepWithInputs(1, "", []string{"src/feature.go"})

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(after.ArtifactRegistry) != 1 || after.ArtifactRegistry[0].Artifact != "src/feature.go" {
		t.Errorf("registry: want single src/feature.go, got %v", after.ArtifactRegistry)
	}
}

func TestApply_Registry_PrefixedAndUnprefixedFormsAreOneEntry(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	first, err := store.Apply(ctx, state, stepWithInputs(1, "", []string{"Plan.md"}))
	if err != nil {
		t.Fatalf("Apply first: %v", err)
	}
	second := newTestStep(2, "planner#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{runPrefix() + "Plan.md"})

	after, err := store.Apply(ctx, first, second)
	if err != nil {
		t.Fatalf("Apply second: %v", err)
	}

	if len(after.ArtifactRegistry) != 1 {
		t.Fatalf("registry rows: want 1, got %v", after.ArtifactRegistry)
	}
	row := after.ArtifactRegistry[0]
	if row.Artifact != "Plan.md" || row.CreatedBy != "planner#2" {
		t.Errorf("row: want Plan.md by planner#2 (recorded form, latest writer), got %+v", row)
	}
}

func TestApply_Registry_PrefixedRowAlreadyPresentIsReplacedByUnprefixedWrite(t *testing.T) {
	store, state := mustCreateStore(t)
	state.ArtifactRegistry = []domain.ArtifactRegistryEntry{
		{Artifact: runPrefix() + "Plan.md", CreatedIn: "PLANNING", CreatedBy: "planner#1"},
	}
	step := newTestStep(2, "planner#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"Plan.md"})

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(after.ArtifactRegistry) != 1 {
		t.Fatalf("registry rows: want 1, got %v", after.ArtifactRegistry)
	}
	if got := after.ArtifactRegistry[0].Artifact; got != "Plan.md" {
		t.Errorf("registry path: want recorded form %q, got %q", "Plan.md", got)
	}
}

func TestApply_Registry_ReReadCarriesNoRunPrefix(t *testing.T) {
	dir := t.TempDir()
	store, state := storeInDir(t, dir)
	step := stepWithInputs(1, "", []string{runPrefix() + "Stage-1/Plan.md"})
	if _, err := store.Apply(context.Background(), state, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	onDisk, err := store.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(onDisk.ArtifactRegistry) != 1 || onDisk.ArtifactRegistry[0].Artifact != "Stage-1/Plan.md" {
		t.Errorf("registry after re-read: want single Stage-1/Plan.md, got %v", onDisk.ArtifactRegistry)
	}
}

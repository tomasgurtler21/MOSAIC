package artifact_test

// Tests for fileStore.Read and fileStore.Create operations.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// ---- Read: file-based ArtifactStore ----

func TestRead_NonExistentFile_ReturnsErrNotExist(t *testing.T) {
	store := artifact.NewFileStore(filepath.Join(t.TempDir(), "does-not-exist.md"))
	ctx := context.Background()

	_, err := store.Read(ctx)

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read on non-existent file: want os.ErrNotExist, got %v", err)
	}
}

func TestRead_NonCanonicalFile_ReturnsRefusalError(t *testing.T) {
	// A file that exists but is not canonical must return a RefusalError, not os.ErrNotExist.
	store := artifact.NewFileStore(fixturePath("no-type.md"))
	ctx := context.Background()

	_, err := store.Read(ctx)

	if err == nil {
		t.Fatal("Read must return an error for a non-canonical file")
	}
	asRefusalError(t, err)
}

func TestRead_RefusalError_ResourceNamesFilePath(t *testing.T) {
	// RefusalError.Resource must name the file path so the user can locate the problem.
	path := fixturePath("no-type.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()

	_, err := store.Read(ctx)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the file path, not be empty")
	}
}

// ---- Create ----

func TestCreate_WorkflowID_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "some task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Workflow != domain.WorkflowID("quick-fix") {
		t.Errorf("state.Workflow: want %q, got %q", "quick-fix", state.Workflow)
	}
}

func TestCreate_WorkflowVersion_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "some task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.WorkflowVersion != domain.WorkflowVersion("3.0") {
		t.Errorf("state.WorkflowVersion: want %q, got %q", "3.0", state.WorkflowVersion)
	}
}

func TestCreate_Task_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "My important task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Task != "My important task" {
		t.Errorf("state.Task: want %q, got %q", "My important task", state.Task)
	}
}

func TestCreate_CheckpointsEnabled_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", domain.RunSettings{Checkpoints: true}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if !state.Checkpoints {
		t.Error("state.Checkpoints: want true (enabled), got false")
	}
}

func TestCreate_CheckpointsDisabled_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Checkpoints {
		t.Error("state.Checkpoints: want false (disabled), got true")
	}
}

func TestCreate_Type_SetInState(t *testing.T) {
	// A freshly created artifact must have Type="orchestration-artifact" so that
	// it passes its own Parse check when read back (the "type" field is required
	// for canonical format identification).
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Type != "orchestration-artifact" {
		t.Errorf("state.Type: want %q, got %q", "orchestration-artifact", state.Type)
	}
}

func TestCreate_Started_SetFromNowParameter(t *testing.T) {
	// The now parameter passed to Create must appear as state.Started.
	// This allows the caller to control the start timestamp precisely.
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	state, err := store.Create(ctx, info, "task", domain.RunSettings{}, now, "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if !state.Started.Equal(now) {
		t.Errorf("state.Started: want %v (now parameter), got %v", now, state.Started)
	}
}

func TestCreate_GlobalSequence_InitiallyZero(t *testing.T) {
	// A new artifact has no completed steps, so global_sequence starts at 0.
	// Each Apply call increments it; the zero value signals "no invocations yet".
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.GlobalSequence != 0 {
		t.Errorf("state.GlobalSequence: want 0 for new artifact, got %d", state.GlobalSequence)
	}
}

func TestCreate_ExecutionLog_Empty(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if len(state.ExecutionLog) != 0 {
		t.Errorf("ExecutionLog: want empty on new artifact, got %d entries", len(state.ExecutionLog))
	}
}

func TestCreate_ArtifactRegistry_Empty(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if len(state.ArtifactRegistry) != 0 {
		t.Errorf("ArtifactRegistry: want empty on new artifact, got %d entries", len(state.ArtifactRegistry))
	}
}

func TestCreate_FailsIfFileAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Orchestration.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	// Create the file once.
	if _, err := store.Create(ctx, info, "first", domain.RunSettings{}, time.Now(), ""); err != nil {
		t.Fatalf("first Create: unexpected error: %v", err)
	}

	// A second Create on the same path must fail.
	_, err := store.Create(ctx, info, "second", domain.RunSettings{}, time.Now(), "")
	if err == nil {
		t.Fatal("second Create: want error because file already exists, got nil")
	}
}

func TestCreate_FileReadableAfterCreate(t *testing.T) {
	// Create then immediately Read must produce consistent state.
	dir := t.TempDir()
	path := filepath.Join(dir, "Orchestration.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()
	now := time.Date(2026, 1, 29, 9, 0, 0, 0, time.UTC)
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	created, err := store.Create(ctx, info, "Fix something", domain.RunSettings{}, now, "")
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	read, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Create: unexpected error: %v", err)
	}

	if read.Workflow != created.Workflow {
		t.Errorf("Read.Workflow: want %q (same as Create), got %q", created.Workflow, read.Workflow)
	}
	if read.Task != created.Task {
		t.Errorf("Read.Task: want %q (same as Create), got %q", created.Task, read.Task)
	}
}

// ---- Create: run_id ----

func TestCreate_RunID_SetInReturnedState(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	_, state := newTestFixtureWithRunID(t, runID)

	if state.RunID != runID {
		t.Errorf("state.RunID: want %q, got %q", runID, state.RunID)
	}
}

func TestCreate_RunID_PersistedToFrontmatter(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	store, _ := newTestFixtureWithRunID(t, runID)
	ctx := context.Background()

	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Create: unexpected error: %v", err)
	}

	if readBack.RunID != runID {
		t.Errorf("RunID after Read: want %q, got %q", runID, readBack.RunID)
	}
}

func TestCreate_RunID_EmptyRunID_NotInFrontmatter(t *testing.T) {
	_, state := newTestFixtureWithRunID(t, "")

	if state.RunID != "" {
		t.Errorf("state.RunID: want empty string when runID param is empty, got %q", state.RunID)
	}
}

func TestCreate_RunID_DifferentValues_Stored(t *testing.T) {
	const runID = "20260101T000000Z-ffff"
	_, state := newTestFixtureWithRunID(t, runID)

	if state.RunID != runID {
		t.Errorf("state.RunID: want %q, got %q", runID, state.RunID)
	}
}

func TestCreate_RunID_RoundTrip_PreservesAllOtherFields(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "my-workflow", Version: "2.0"}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	created, err := store.Create(ctx, info, "important task", domain.RunSettings{Checkpoints: true}, now, runID)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	// Verify all fields beyond RunID are still correct.
	if created.Workflow != domain.WorkflowID("my-workflow") {
		t.Errorf("Workflow: want %q, got %q", "my-workflow", created.Workflow)
	}
	if created.WorkflowVersion != domain.WorkflowVersion("2.0") {
		t.Errorf("WorkflowVersion: want %q, got %q", "2.0", created.WorkflowVersion)
	}
	if created.Task != "important task" {
		t.Errorf("Task: want %q, got %q", "important task", created.Task)
	}
	if !created.Checkpoints {
		t.Error("Checkpoints: want true, got false")
	}
	if created.GlobalSequence != 0 {
		t.Errorf("GlobalSequence: want 0, got %d", created.GlobalSequence)
	}
	// And verify RunID is also correct.
	if created.RunID != runID {
		t.Errorf("RunID: want %q, got %q", runID, created.RunID)
	}
}

// ---- Create: RunSettings persisted to disk ----

func TestCreate_AllRunSettings_PersistedToFile(t *testing.T) {
	// Create is called with an explicit RunSettings carrying non-zero values for
	// all six configuration fields. A subsequent Read must return a state whose
	// fields match those values exactly.
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	settings := domain.RunSettings{
		Mode:                domain.ExecutionModeAuto,
		Checkpoints:         true,
		Commits:             true,
		CommitBranchVariant: domain.CommitBranchUserOwn,
		CommitBranch:        "mosaic/run/testrun",
		PreConsultation:     true,
		ManualResolution:    true,
	}

	if _, err := store.Create(ctx, info, "persisted settings task", settings, time.Now(), ""); err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Create: unexpected error: %v", err)
	}

	if onDisk.Mode != domain.ExecutionModeAuto {
		t.Errorf("on-disk Mode: want %q (persisted by Create), got %q",
			domain.ExecutionModeAuto, onDisk.Mode)
	}
	if !onDisk.Checkpoints {
		t.Error("on-disk Checkpoints: want true (persisted by Create), got false")
	}
	if !onDisk.Commits {
		t.Error("on-disk Commits: want true (persisted by Create), got false")
	}
	if onDisk.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("on-disk CommitBranchVariant: want %q (persisted by Create), got %q",
			domain.CommitBranchUserOwn, onDisk.CommitBranchVariant)
	}
	if onDisk.CommitBranch != "mosaic/run/testrun" {
		t.Errorf("on-disk CommitBranch: want %q (persisted by Create), got %q",
			"mosaic/run/testrun", onDisk.CommitBranch)
	}
	if !onDisk.PreConsultation {
		t.Error("on-disk PreConsultation: want true (persisted by Create), got false")
	}
	if !onDisk.ManualResolution {
		t.Error("on-disk ManualResolution: want true (persisted by Create), got false")
	}
}

// ---- Create: non-absolute path rejection ----

func TestCreate_RelativePath_ReturnsError(t *testing.T) {
	relativePath := "Orchestration.md"
	store := artifact.NewFileStore(relativePath)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}

	_, err := store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "")

	// Defensive cleanup: in the RED phase Create may succeed and write the file.
	// Remove it so subsequent test runs are not affected by a leftover artifact.
	wd, wdErr := os.Getwd()
	if wdErr == nil {
		_ = os.Remove(filepath.Join(wd, relativePath))
	}

	if err == nil {
		t.Fatal("Create with relative store path: want error, got nil")
	}
	if !strings.Contains(err.Error(), mustBeAbsoluteSubstring) {
		t.Errorf("Create error = %q; want substring %q", err.Error(), mustBeAbsoluteSubstring)
	}
	// The error message must name the offending path so the caller can diagnose it.
	if !strings.Contains(err.Error(), relativePath) {
		t.Errorf("Create error = %q; want the offending path %q to appear in the message",
			err.Error(), relativePath)
	}
}

func TestCreate_RelativePath_WritesNoFile(t *testing.T) {
	relativePath := "Orchestration.md"
	store := artifact.NewFileStore(relativePath)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	_, _ = store.Create(ctx, info, "task", domain.RunSettings{}, time.Now(), "") // error expected; ignore

	// Verify no file was written at the relative path (resolved against the test's CWD).
	candidate := filepath.Join(wd, relativePath)
	if _, statErr := os.Stat(candidate); statErr == nil {
		// File exists — clean it up and fail.
		_ = os.Remove(candidate)
		t.Errorf("Create with relative path wrote a file at %s; want no file created", candidate)
	}
}

func TestCreate_AbsoluteNonRunScopedPath_Succeeds(t *testing.T) {
	// A plain temp directory — absolute but not run-scoped.
	store := artifact.NewFileStore(filepath.Join(t.TempDir(), "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}

	_, err := store.Create(ctx, info, "regression guard task", domain.RunSettings{}, time.Now(), "")

	if err != nil {
		t.Errorf("Create with absolute non-run-scoped path: unexpected error: %v", err)
	}
}

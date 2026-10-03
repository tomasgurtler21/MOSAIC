package artifact_test

// Tests for schema behaviour at the store boundary: global_sequence recovery
// on read, unknown and legacy frontmatter surviving rewrites, legacy aliases
// re-rendered only under prefixed names, and the full settings round-trip.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// writeArtifact writes data to a temp Orchestration.md and returns the store
// and the file path.
func writeArtifact(t *testing.T, data []byte) (domain.ArtifactStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing artifact: %v", err)
	}
	return artifact.NewFileStore(path), path
}

// withGlobalSequence returns the one-row execution log artifact (highest Seq 1)
// with its stored global_sequence replaced by seq.
func withGlobalSequence(seq string) []byte {
	return []byte(strings.Replace(string(minimalArtifactWithExecutionRow("")), "global_sequence: 1", "global_sequence: "+seq, 1))
}

// ---- Recovery: global_sequence reconciled against the Execution Log ----

func TestParse_LaggingGlobalSequence_CorrectedToMaxSeq(t *testing.T) {
	state, err := artifact.Parse(withGlobalSequence("0"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if state.GlobalSequence != 1 {
		t.Errorf("GlobalSequence: want max(Seq)=1 for a lagging stored value, got %d", state.GlobalSequence)
	}
}

func TestParse_HigherGlobalSequence_Preserved(t *testing.T) {
	// A stored value above max(Seq) marks an allocation that was interrupted
	// before its row was written. It is kept so the number is never reused.
	state, err := artifact.Parse(withGlobalSequence("5"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if state.GlobalSequence != 5 {
		t.Errorf("GlobalSequence: want stored 5 preserved, got %d", state.GlobalSequence)
	}
}

func TestParse_MatchingGlobalSequence_Unchanged(t *testing.T) {
	state, err := artifact.Parse(withGlobalSequence("1"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if state.GlobalSequence != 1 {
		t.Errorf("GlobalSequence: want 1, got %d", state.GlobalSequence)
	}
}

func TestParse_EmptyExecutionLog_GlobalSequenceKeptAsStored(t *testing.T) {
	data := []byte(strings.Replace(string(minimalArtifactBytes(testRunID)), "global_sequence: 0", "global_sequence: 3", 1))

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if state.GlobalSequence != 3 {
		t.Errorf("GlobalSequence: want stored 3 with no rows, got %d", state.GlobalSequence)
	}
}

func TestRead_LaggingGlobalSequence_ReturnsCorrectedValueAndNextDispatchIsPlusOne(t *testing.T) {
	store, _ := writeArtifact(t, withGlobalSequence("0"))
	ctx := context.Background()

	state, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if state.GlobalSequence != 1 {
		t.Fatalf("Read GlobalSequence: want corrected 1, got %d", state.GlobalSequence)
	}
	// The next dispatch is global_sequence + 1; recovery itself never pre-applies it.
	next := state.GlobalSequence + 1
	step := newTestStep(next, "planner#2", "PLANNING", "", domain.StatusSUCCESS, time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC), nil)
	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply of the next dispatch: %v", err)
	}
	if after.GlobalSequence != 2 {
		t.Errorf("GlobalSequence after next dispatch: want 2, got %d", after.GlobalSequence)
	}
}

func TestRead_HigherGlobalSequence_NextDispatchContinuesAboveIt(t *testing.T) {
	store, _ := writeArtifact(t, withGlobalSequence("4"))
	ctx := context.Background()
	state, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	step := newTestStep(state.GlobalSequence+1, "planner#5", "PLANNING", "", domain.StatusSUCCESS, time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC), nil)
	after, err := store.Apply(ctx, state, step)

	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if after.GlobalSequence != 5 {
		t.Errorf("GlobalSequence after next dispatch: want 5, got %d", after.GlobalSequence)
	}
}

// ---- Unknown and legacy frontmatter survive rewrites ----

const unknownKeysConfig = "checkpoints: disabled\ncommits: disabled\n" +
	"native_note: keep me\n" +
	"native_block:\n" +
	"  a: 1\n" +
	"  b:\n" +
	"    - x\n" +
	"    - y\n"

func wantUnknownKeys() []domain.FrontmatterEntry {
	return []domain.FrontmatterEntry{
		{Key: "native_note", Lines: []string{"native_note: keep me"}},
		{Key: "native_block", Lines: []string{"native_block:", "  a: 1", "  b:", "    - x", "    - y"}},
	}
}

func TestRender_Parse_UnknownKeysRoundTrip(t *testing.T) {
	state, err := artifact.Parse(minimalArtifactWithConfigBytes(unknownKeysConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	again, err := artifact.Parse(rendered)

	if err != nil {
		t.Fatalf("Parse rendered: %v", err)
	}
	if !reflect.DeepEqual(again.UnknownFrontmatter, wantUnknownKeys()) {
		t.Errorf("UnknownFrontmatter after round-trip:\n want %#v\n  got %#v", wantUnknownKeys(), again.UnknownFrontmatter)
	}
}

func TestApply_PreservesUnknownFrontmatterOnDisk(t *testing.T) {
	store, _ := writeArtifact(t, minimalArtifactWithConfigBytes(unknownKeysConfig))
	ctx := context.Background()
	state, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if _, err := store.Apply(ctx, state, newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: %v", err)
	}
	if !reflect.DeepEqual(onDisk.UnknownFrontmatter, wantUnknownKeys()) {
		t.Errorf("UnknownFrontmatter after Apply:\n want %#v\n  got %#v", wantUnknownKeys(), onDisk.UnknownFrontmatter)
	}
}

func TestSetPhase_PreservesUnknownFrontmatterOnDisk(t *testing.T) {
	store, _ := writeArtifact(t, minimalArtifactWithConfigBytes(unknownKeysConfig))
	ctx := context.Background()

	if _, err := store.SetPhase(ctx, domain.ArtifactState{}, "COMPLETED", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("SetPhase: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetPhase: %v", err)
	}
	if !reflect.DeepEqual(onDisk.UnknownFrontmatter, wantUnknownKeys()) {
		t.Errorf("UnknownFrontmatter after SetPhase:\n want %#v\n  got %#v", wantUnknownKeys(), onDisk.UnknownFrontmatter)
	}
}

func TestSetPhase_UnknownKeysRenderedBeforeCurrentState(t *testing.T) {
	store, path := writeArtifact(t, minimalArtifactWithConfigBytes(unknownKeysConfig))

	if _, err := store.SetPhase(context.Background(), domain.ArtifactState{}, "COMPLETED", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("SetPhase: %v", err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(strings.ReplaceAll(string(raw), "\r\n", "\n"),
		"native_note: keep me\nnative_block:\n  a: 1\n  b:\n    - x\n    - y\ncurrent_state:\n") {
		t.Errorf("unknown keys must be written verbatim, in order, immediately before current_state, got:\n%s", raw)
	}
}

// ---- Legacy aliases are migrated on rewrite ----

func TestSetPhase_LegacyAliasArtifact_RewrittenUnderPrefixedNamesOnly(t *testing.T) {
	data, err := os.ReadFile(fixturePath("legacy-aliases.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	store, path := writeArtifact(t, data)

	if _, err := store.SetPhase(context.Background(), domain.ArtifactState{}, "COMPLETED", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("SetPhase: %v", err)
	}

	raw, _ := os.ReadFile(path)
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, want := range []string{"runner_mode: auto-review\n", "runner_pre_consultation: enabled\n", "runner_manual_resolution: enabled\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("rewritten artifact must contain %q, got:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		for _, legacy := range []string{"mode:", "pre_consultation:", "manual_resolution:", "commit_branch_variant:"} {
			if strings.HasPrefix(line, legacy) {
				t.Errorf("rewritten artifact must not carry legacy key, found %q", line)
			}
		}
	}
}

// ---- Full settings round-trip ----

func TestRoundTrip_AllRunSettings_SurviveRenderAndParse(t *testing.T) {
	original := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "quick-fix",
		RunSettings: domain.RunSettings{
			Mode:             domain.ExecutionModeAutoReview,
			Checkpoints:      true,
			Commits:          true,
			CommitBranch:     domain.MOSAICRunBranchName(testRunID),
			PreConsultation:  true,
			ManualResolution: true,
			ReviewLoopLimit:  4,
			InfraClassSelections: map[string]string{
				"checkpoint": "checkpoint-manager-git",
				"commit":     "commit-manager-git",
				"restore":    "restore-manager-git",
			},
		},
	}

	rendered, err := artifact.Render(original)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	parsed, err := artifact.Parse(rendered)

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Mode != original.Mode || parsed.Checkpoints != true || parsed.Commits != true ||
		parsed.CommitBranch != original.CommitBranch || !parsed.PreConsultation || !parsed.ManualResolution ||
		parsed.ReviewLoopLimit != 4 {
		t.Errorf("settings changed across round-trip: %+v", parsed.RunSettings)
	}
	if !reflect.DeepEqual(parsed.InfraClassSelections, original.InfraClassSelections) {
		t.Errorf("InfraClassSelections: want %v, got %v", original.InfraClassSelections, parsed.InfraClassSelections)
	}
	if parsed.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant: want derived %q, got %q", domain.CommitBranchMOSAICOwned, parsed.CommitBranchVariant)
	}
}

func TestRoundTrip_ArtifactWithoutLimitOrSelections_StaysValid(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ReviewLoopLimit != 0 || len(state.InfraClassSelections) != 0 {
		t.Errorf("canonical fixture must parse as no limit / no selections, got %d / %v", state.ReviewLoopLimit, state.InfraClassSelections)
	}
	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(rendered), "review_loop_limit") || strings.Contains(string(rendered), "infrastructure_selections") {
		t.Errorf("optional keys must stay absent, got:\n%s", rendered)
	}
}

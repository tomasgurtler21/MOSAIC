package artifact_test

// Tests for the Artifacts registry rows produced by Apply from a step's
// written outputs: one row per key, wildcard matches each get a row, rework
// replaces the row in place, and duplicate rows already present in the
// artifact collapse to a single row.

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

func registryRowsFor(state domain.ArtifactState, key string) []domain.ArtifactRegistryEntry {
	var rows []domain.ArtifactRegistryEntry
	for _, e := range state.ArtifactRegistry {
		if e.Artifact == key {
			rows = append(rows, e)
		}
	}
	return rows
}

func TestApply_WrittenArtifacts_EachEntryGetsOneRow(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	written := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"}
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), written)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(after.ArtifactRegistry) != len(written) {
		t.Fatalf("registry rows: want %d (one per written output), got %d: %v",
			len(written), len(after.ArtifactRegistry), after.ArtifactRegistry)
	}
	for i, w := range written {
		if after.ArtifactRegistry[i].Artifact != w {
			t.Errorf("registry row %d: want %q, got %q", i, w, after.ArtifactRegistry[i].Artifact)
		}
	}
}

func TestApply_NoWrittenArtifacts_RegistryUnchanged(t *testing.T) {
	// A step that wrote nothing registers nothing, and rows registered earlier
	// stay as they were.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	first := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"Plan.md"})
	state1, err := store.Apply(ctx, state, first)
	if err != nil {
		t.Fatalf("Apply first: %v", err)
	}

	second := newTestStep(2, "reviewer#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	state2, err := store.Apply(ctx, state1, second)
	if err != nil {
		t.Fatalf("Apply second: %v", err)
	}

	if len(state2.ArtifactRegistry) != 1 {
		t.Fatalf("registry rows: want 1, got %d: %v", len(state2.ArtifactRegistry), state2.ArtifactRegistry)
	}
	if got := state2.ArtifactRegistry[0]; got.CreatedBy != "planner#1" {
		t.Errorf("earlier row must be untouched by a step that wrote nothing: CreatedBy = %q", got.CreatedBy)
	}
}

func TestApply_WrittenArtifacts_RepeatedKeyInOneStep_SingleRow(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(),
		[]string{"Plan.md", "Plan.md"})

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if rows := registryRowsFor(after, "Plan.md"); len(rows) != 1 {
		t.Errorf("Plan.md rows: want 1, got %d", len(rows))
	}
}

func TestApply_InheritedDuplicateRows_ConsolidateToOneRowAtFirstPosition(t *testing.T) {
	// An artifact written by an earlier executor may already hold several rows
	// for one key. Re-registering the key leaves exactly one row, in the
	// position of the first occurrence, attributed to the latest writer.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.ArtifactRegistry = []domain.ArtifactRegistryEntry{
		{Artifact: "Plan.md", CreatedIn: "PLANNING", CreatedBy: "planner#1"},
		{Artifact: "Design.md", CreatedIn: "DESIGN", CreatedBy: "designer#2"},
		{Artifact: "Plan.md", CreatedIn: "PLANNING", CreatedBy: "planner#3"},
	}
	step := newTestStep(4, "planner#4", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"Plan.md"})

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows := registryRowsFor(after, "Plan.md")
	if len(rows) != 1 {
		t.Fatalf("Plan.md rows after consolidation: want 1, got %d: %v", len(rows), after.ArtifactRegistry)
	}
	if rows[0].CreatedBy != "planner#4" {
		t.Errorf("consolidated row CreatedBy: want planner#4, got %q", rows[0].CreatedBy)
	}
	if len(after.ArtifactRegistry) != 2 {
		t.Fatalf("total rows: want 2, got %d: %v", len(after.ArtifactRegistry), after.ArtifactRegistry)
	}
	if after.ArtifactRegistry[0].Artifact != "Plan.md" || after.ArtifactRegistry[1].Artifact != "Design.md" {
		t.Errorf("row order: want Plan.md then Design.md, got %v", after.ArtifactRegistry)
	}
}

func TestApply_InheritedDuplicateRows_ConsolidationSurvivesReRead(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.ArtifactRegistry = []domain.ArtifactRegistryEntry{
		{Artifact: "Plan.md", CreatedIn: "PLANNING", CreatedBy: "planner#1"},
		{Artifact: "Plan.md", CreatedIn: "PLANNING", CreatedBy: "planner#2"},
	}
	step := newTestStep(3, "planner#3", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"Plan.md"})
	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if rows := registryRowsFor(onDisk, "Plan.md"); len(rows) != 1 {
		t.Errorf("Plan.md rows on disk: want 1, got %d", len(rows))
	}
}

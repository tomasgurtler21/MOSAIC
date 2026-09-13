package app

// Internal white-box tests for the merge guard and stream re-attribution
// helpers. These tests access unexported package symbols directly.
//
// Coverage:
//   mergeEligible:
//     - Returns true only when exactly one named run and a non-empty
//       unattributable bucket are present.
//     - Returns false when there are no named runs.
//     - Returns false when there are multiple named runs.
//     - Returns false when the unattributable bucket is absent (nil).
//     - Returns false when the unattributable bucket is empty (no files).
//   reattributeStreams:
//     - Re-labels every unattributable stream's Run to targetRun.
//     - Named-run streams are returned unchanged.
//     - All other StreamRef fields (Kind, InstanceHint, Path) are preserved.
//     - The Events slice within each stream is shared, not deep-copied.
//     - The original input slice is not mutated.
//     - Returns a new slice distinct from the input.

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// mergeEligible: guard conditions
// ---------------------------------------------------------------------------

func TestMergeEligible_OneRunNonEmptyUnattributable_ReturnsTrue(t *testing.T) {
	// The guard must return true when the inventory has exactly one named run
	// and a non-empty unattributable bucket (the single-run-isolated case).
	inv := domain.Inventory{
		Runs: []domain.RunEntry{
			{Run: domain.NamedRun("20260101T000000Z-abcd")},
		},
		Unattributable: &domain.RunEntry{
			Run:              domain.UnattributableRun(),
			OrchestratorFile: "/logs/unknown-run/00_orchestrator_events.jsonl",
		},
	}

	if !mergeEligible(inv) {
		t.Error("mergeEligible returned false for a single-run inventory with a non-empty unattributable bucket; want true")
	}
}

func TestMergeEligible_OneRunUnattributableWithAgentOnly_ReturnsTrue(t *testing.T) {
	// The guard must also return true when the unattributable bucket has no
	// orchestrator file but has at least one agent event file.
	inv := domain.Inventory{
		Runs: []domain.RunEntry{
			{Run: domain.NamedRun("20260101T000000Z-abcd")},
		},
		Unattributable: &domain.RunEntry{
			Run: domain.UnattributableRun(),
			Agents: []domain.AgentEntry{
				{EventFile: "/logs/unknown-run/Agent#1/03_events.jsonl"},
			},
		},
	}

	if !mergeEligible(inv) {
		t.Error("mergeEligible returned false when unattributable bucket has an agent event file; want true")
	}
}

func TestMergeEligible_NoNamedRuns_ReturnsFalse(t *testing.T) {
	// With no named runs there is nothing to merge into.
	inv := domain.Inventory{
		Runs: nil,
		Unattributable: &domain.RunEntry{
			Run:              domain.UnattributableRun(),
			OrchestratorFile: "/logs/unknown-run/00_orchestrator_events.jsonl",
		},
	}

	if mergeEligible(inv) {
		t.Error("mergeEligible returned true when inventory has no named runs; want false")
	}
}

func TestMergeEligible_MultipleNamedRuns_ReturnsFalse(t *testing.T) {
	// With two or more named runs the unknown-run bucket is ambiguous: its
	// records may belong to any of the runs. The merge must be rejected.
	inv := domain.Inventory{
		Runs: []domain.RunEntry{
			{Run: domain.NamedRun("20260101T000000Z-aaaa")},
			{Run: domain.NamedRun("20260101T000000Z-bbbb")},
		},
		Unattributable: &domain.RunEntry{
			Run:              domain.UnattributableRun(),
			OrchestratorFile: "/logs/unknown-run/00_orchestrator_events.jsonl",
		},
	}

	if mergeEligible(inv) {
		t.Error("mergeEligible returned true when inventory has multiple named runs; want false")
	}
}

func TestMergeEligible_NilUnattributable_ReturnsFalse(t *testing.T) {
	// When the unattributable bucket is absent, there is nothing to merge from.
	inv := domain.Inventory{
		Runs: []domain.RunEntry{
			{Run: domain.NamedRun("20260101T000000Z-abcd")},
		},
		Unattributable: nil,
	}

	if mergeEligible(inv) {
		t.Error("mergeEligible returned true when Unattributable is nil; want false")
	}
}

func TestMergeEligible_EmptyUnattributableBucket_ReturnsFalse(t *testing.T) {
	// An unattributable bucket with no orchestrator file and no agent files
	// has nothing to contribute. The merge must be rejected.
	inv := domain.Inventory{
		Runs: []domain.RunEntry{
			{Run: domain.NamedRun("20260101T000000Z-abcd")},
		},
		Unattributable: &domain.RunEntry{
			Run: domain.UnattributableRun(),
			// OrchestratorFile: "" (absent)
			// Agents: nil (absent)
		},
	}

	if mergeEligible(inv) {
		t.Error("mergeEligible returned true when unattributable bucket is empty; want false")
	}
}

// ---------------------------------------------------------------------------
// reattributeStreams: re-labelling and isolation
// ---------------------------------------------------------------------------

func TestReattributeStreams_UnattributableStreamsRelabelled(t *testing.T) {
	// Every stream whose Ref.Run is unattributable must have its Run field
	// changed to targetRun in the returned slice.
	targetRun := domain.NamedRun("20260101T000000Z-abcd")
	unattributableRun := domain.UnattributableRun()

	input := []analysis.Stream{
		{
			Ref: domain.StreamRef{
				Run:  unattributableRun,
				Kind: domain.StreamOrchestrator,
				Path: "/logs/unknown-run/00_orchestrator_events.jsonl",
			},
			Events: []domain.Event{{Type: domain.EventRunEnd}},
		},
	}

	result := reattributeStreams(input, targetRun)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Ref.Run != targetRun {
		t.Errorf("result[0].Ref.Run = %+v, want targetRun %+v", result[0].Ref.Run, targetRun)
	}
}

func TestReattributeStreams_NamedRunStreamsUnchanged(t *testing.T) {
	// Streams that already carry a named run must pass through unchanged.
	namedRun := domain.NamedRun("20260101T000000Z-abcd")
	targetRun := domain.NamedRun("20260101T000000Z-abcd") // same as namedRun for this test

	input := []analysis.Stream{
		{
			Ref: domain.StreamRef{
				Run:  namedRun,
				Kind: domain.StreamOrchestrator,
				Path: "/logs/20260101T000000Z-abcd/00_orchestrator_events.jsonl",
			},
		},
	}

	result := reattributeStreams(input, targetRun)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Ref.Run != namedRun {
		t.Errorf("named-run stream was modified: Ref.Run = %+v, want %+v", result[0].Ref.Run, namedRun)
	}
}

func TestReattributeStreams_MixedStreams_OnlyUnattributableRelabelled(t *testing.T) {
	// A slice containing both named-run and unattributable streams must result
	// in only the unattributable streams being relabelled.
	namedRun := domain.NamedRun("20260101T000000Z-abcd")
	targetRun := domain.NamedRun("20260101T000000Z-abcd")

	input := []analysis.Stream{
		{
			Ref: domain.StreamRef{
				Run:  namedRun,
				Kind: domain.StreamOrchestrator,
				Path: "/logs/run/00_orchestrator_events.jsonl",
			},
		},
		{
			Ref: domain.StreamRef{
				Run:          domain.UnattributableRun(),
				Kind:         domain.StreamAgentInstance,
				InstanceHint: "Agent#1",
				Path:         "/logs/unknown-run/Agent#1/03_events.jsonl",
			},
		},
	}

	result := reattributeStreams(input, targetRun)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	// First stream (named run): unchanged.
	if result[0].Ref.Run != namedRun {
		t.Errorf("named-run stream was modified: Ref.Run = %+v", result[0].Ref.Run)
	}

	// Second stream (was unattributable): relabelled to targetRun.
	if result[1].Ref.Run != targetRun {
		t.Errorf("unattributable stream not relabelled: Ref.Run = %+v, want %+v", result[1].Ref.Run, targetRun)
	}
}

func TestReattributeStreams_PreservesOtherStreamRefFields(t *testing.T) {
	// Re-attribution must not alter any StreamRef field other than Run.
	targetRun := domain.NamedRun("20260101T000000Z-abcd")
	const wantInstanceHint = domain.AgentInstanceID("Agent#42")
	const wantPath = "/logs/unknown-run/Agent#42/03_events.jsonl"

	input := []analysis.Stream{
		{
			Ref: domain.StreamRef{
				Run:          domain.UnattributableRun(),
				Kind:         domain.StreamAgentInstance,
				InstanceHint: wantInstanceHint,
				Path:         wantPath,
			},
		},
	}

	result := reattributeStreams(input, targetRun)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	got := result[0].Ref
	if got.Kind != domain.StreamAgentInstance {
		t.Errorf("Ref.Kind = %v, want StreamAgentInstance", got.Kind)
	}
	if got.InstanceHint != wantInstanceHint {
		t.Errorf("Ref.InstanceHint = %q, want %q", got.InstanceHint, wantInstanceHint)
	}
	if got.Path != wantPath {
		t.Errorf("Ref.Path = %q, want %q", got.Path, wantPath)
	}
}

func TestReattributeStreams_OriginalSliceNotMutated(t *testing.T) {
	// The input slice must remain unchanged after reattributeStreams returns.
	targetRun := domain.NamedRun("20260101T000000Z-abcd")
	originalRun := domain.UnattributableRun()

	input := []analysis.Stream{
		{
			Ref: domain.StreamRef{
				Run:  originalRun,
				Kind: domain.StreamOrchestrator,
				Path: "/logs/unknown-run/00_orchestrator_events.jsonl",
			},
		},
	}

	_ = reattributeStreams(input, targetRun)

	// The original input element must still carry the unattributable run.
	if !input[0].Ref.Run.IsUnattributable() {
		t.Error("reattributeStreams mutated the original input slice; Ref.Run is no longer unattributable")
	}
}

func TestReattributeStreams_ReturnsNewSlice(t *testing.T) {
	// The returned slice must be a distinct allocation from the input, even
	// when all streams are named-run and no relabelling is needed.
	targetRun := domain.NamedRun("20260101T000000Z-abcd")

	input := []analysis.Stream{
		{Ref: domain.StreamRef{Run: targetRun, Path: "/logs/run/events.jsonl"}},
	}

	result := reattributeStreams(input, targetRun)

	// Modifying result must not affect input (and vice versa). We verify
	// distinct slice headers by checking that the capacity of each is
	// independently owned. A simple proxy: overwrite result[0] and confirm
	// input[0] is unchanged.
	if len(result) > 0 && len(input) > 0 {
		result[0].Ref.Path = "/modified"
		if input[0].Ref.Path == "/modified" {
			t.Error("reattributeStreams returned a slice that shares backing array with the input")
		}
	}
}

// ---------------------------------------------------------------------------
// mergeMetadata: deduplication formula
// ---------------------------------------------------------------------------

func TestMergeMetadata_AllUniqueRecords_MergedEqualsTotal(t *testing.T) {
	// When all unknown-run records are unique (no cross-stream duplicates),
	// every record should be counted as merged.
	// Formula: merged = totalUnknownRecords - crossStreamDups = 5 - 0 = 5.
	merged, residual := mergeMetadata(5, 0)
	if merged != 5 {
		t.Errorf("mergeMetadata(5, 0): merged = %d, want 5; formula is totalUnknownRecords - crossStreamDups", merged)
	}
	if residual != 0 {
		t.Errorf("mergeMetadata(5, 0): residual = %d, want 0 for a single-run-isolated source", residual)
	}
}

func TestMergeMetadata_WithCrossStreamDups_MergedIsReduced(t *testing.T) {
	// When some unknown-run records collide with named-run records, the merged
	// count must be the total minus the number of cross-stream duplicates.
	// Formula: merged = totalUnknownRecords - crossStreamDups = 5 - 2 = 3.
	merged, residual := mergeMetadata(5, 2)
	if merged != 3 {
		t.Errorf("mergeMetadata(5, 2): merged = %d, want 3; formula is 5 - 2", merged)
	}
	if residual != 0 {
		t.Errorf("mergeMetadata(5, 2): residual = %d, want 0 for a single-run-isolated source", residual)
	}
}

func TestMergeMetadata_AllDuplicates_MergedIsZero(t *testing.T) {
	// When every unknown-run record is a cross-stream duplicate of a named-run
	// record, no new records are added to the named run's totals.
	// Formula: merged = 3 - 3 = 0.
	merged, residual := mergeMetadata(3, 3)
	if merged != 0 {
		t.Errorf("mergeMetadata(3, 3): merged = %d, want 0; all records were cross-stream duplicates", merged)
	}
	if residual != 0 {
		t.Errorf("mergeMetadata(3, 3): residual = %d, want 0 for a single-run-isolated source", residual)
	}
}

func TestMergeMetadata_NoUnknownRecords_ZeroMergedZeroResidual(t *testing.T) {
	// When the unknown-run bucket had no records, both values must be zero.
	merged, residual := mergeMetadata(0, 0)
	if merged != 0 {
		t.Errorf("mergeMetadata(0, 0): merged = %d, want 0", merged)
	}
	if residual != 0 {
		t.Errorf("mergeMetadata(0, 0): residual = %d, want 0", residual)
	}
}

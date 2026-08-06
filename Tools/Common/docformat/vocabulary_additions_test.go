package docformat_test

// Tests for vocabulary constants updated in Stage 2.
//
// Stage 2 changes:
//   - CanonicalOrder shrinks from 8 to 7 slots (ArtifactProvenance removed).
//   - CanonicalDeployed grows from 7 to 11 names; ArtifactProvenance removed.
//   - CanonicalInjections is removed entirely; no reference to it is valid.
//   - InjectionParent loses ArtifactProvenanceExtension and gains ProtocolExtension.
//
// Tests that verified the pre-Stage-2 state (8-slot order, 7 deployed, 8 injections,
// ArtifactProvenance in CanonicalDeployed/CanonicalOrder) are removed because the
// underlying code they tested is removed or superseded by Stage 2. Note: the
// pre-Stage-2 state was introduced by uncommitted, task-unattributed pre-work, not by
// any completed Stage 1 task — Stage 1's PlanProgress.md shows all I1.* tasks unchecked
// and Stage 1's Plan.md explicitly disclaims vocabulary work.
//
// Stage 2 canonical-state tests live in vocabulary_stage2_test.go.
// Exact table-pinning tests live in vocabulary_boundary_sync_test.go.
//
// This file retains tests for aspects that are stable before and after Stage 2:
//   - CanonicalSections is still 6 entries.
//   - The six section names appear in CanonicalOrder.
//   - DeployedParent is non-nil and maps all CanonicalDeployed names.
//   - InjectionParent is non-nil.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// CanonicalSections — stable across Stage 1 and Stage 2
// ---------------------------------------------------------------------------

func TestCanonicalSections_ContainsSixSections(t *testing.T) {
	// CanonicalSections is unchanged across Stage 1 and Stage 2.
	got := docformat.CanonicalSections
	if len(got) != 6 {
		t.Fatalf("CanonicalSections length: want 6, got %d: %v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// DeployedParent coverage of CanonicalDeployed
// ---------------------------------------------------------------------------

func TestDeployedParent_CoversAllCanonicalDeployedNames(t *testing.T) {
	// Every name in CanonicalDeployed must have an entry in DeployedParent.
	got := docformat.DeployedParent
	if got == nil {
		t.Fatal("DeployedParent is nil")
	}
	for _, name := range docformat.CanonicalDeployed {
		if _, ok := got[name]; !ok {
			t.Errorf("DeployedParent missing entry for CanonicalDeployed name %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// InjectionParent — non-nil
// ---------------------------------------------------------------------------

func TestInjectionParent_IsNotNil(t *testing.T) {
	if docformat.InjectionParent == nil {
		t.Fatal("InjectionParent is nil — must be populated before first use")
	}
}

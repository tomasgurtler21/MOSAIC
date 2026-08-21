//go:build stage3

package plan_test

// hook_staleness_recorded_test.go covers HookStalenessFromRecorded: the staleness comparison
// that reads the hook bundle's version from the manifest entry (recorded at deploy time) rather
// than from the deployed files on disk (which carry no version marker).
//
// HookStalenessFromRecorded(recordedVersion, sourceVersion string) []domain.VersionDelta
//
// Contract (from ContractsDesign.md, plan.classifyHookItem version source):
//   - Returns nil when recordedVersion == sourceVersion (not stale).
//   - Returns a single VersionDelta with Field "version" when the versions differ.
//   - VersionDelta.Deployed is recordedVersion; VersionDelta.Source is sourceVersion.
//   - Returns a delta when recordedVersion is empty and sourceVersion is not: a manifest entry
//     that predates version recording is treated as stale.
//   - Returns nil when both versions are the empty string.
//
// These tests will fail to compile until HookStalenessFromRecorded is implemented (I3.3),
// which is the intended TDD RED state.

import (
	"testing"

	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// HookStalenessFromRecorded — versions match
// ---------------------------------------------------------------------------

// TestHookStalenessFromRecorded_EqualVersions_ReturnsNil verifies that when the manifest's
// recorded version matches the source bundle version, the function returns nil — the bundle is
// not stale and should classify as ActionUnchanged.
func TestHookStalenessFromRecorded_EqualVersions_ReturnsNil(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "1.0")

	if len(deltas) != 0 {
		t.Errorf("expected nil deltas when recorded == source, got %v", deltas)
	}
}

// TestHookStalenessFromRecorded_BothEmpty_ReturnsNil verifies that when both versions are
// empty strings, the function returns nil. An empty recorded version against an empty source
// version is a matching state, not a staleness signal.
func TestHookStalenessFromRecorded_BothEmpty_ReturnsNil(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("", "")

	if len(deltas) != 0 {
		t.Errorf("expected nil deltas when both versions are empty, got %v", deltas)
	}
}

// TestHookStalenessFromRecorded_SameNonTrivialVersion_ReturnsNil verifies that two identical
// non-trivial version strings produce no deltas. This confirms the comparison is exact string
// equality, not a semantic version comparison.
func TestHookStalenessFromRecorded_SameNonTrivialVersion_ReturnsNil(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("2.1.4-beta+build.99", "2.1.4-beta+build.99")

	if len(deltas) != 0 {
		t.Errorf("expected nil deltas for identical non-trivial version strings, got %v", deltas)
	}
}

// ---------------------------------------------------------------------------
// HookStalenessFromRecorded — versions differ
// ---------------------------------------------------------------------------

// TestHookStalenessFromRecorded_VersionAdvanced_ReturnsSingleDelta verifies that when the
// source version has advanced beyond the recorded version, exactly one VersionDelta is returned
// with Field "version".
func TestHookStalenessFromRecorded_VersionAdvanced_ReturnsSingleDelta(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "2.0")

	if len(deltas) != 1 {
		t.Fatalf("expected exactly 1 delta when versions differ, got %d: %v", len(deltas), deltas)
	}
}

// TestHookStalenessFromRecorded_VersionAdvanced_DeltaFieldIsVersion verifies that the single
// delta returned when versions differ has Field set to "version".
func TestHookStalenessFromRecorded_VersionAdvanced_DeltaFieldIsVersion(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "2.0")
	if len(deltas) == 0 {
		t.Fatal("expected a delta but got none")
	}

	if deltas[0].Field != "version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "version")
	}
}

// TestHookStalenessFromRecorded_VersionAdvanced_DeployedFieldCarriesRecordedVersion verifies
// that the delta's Deployed field carries the recordedVersion argument — the version that was
// recorded in the manifest at deploy time — so the review screen can show the truthful
// "deployed version changed from X to Y" message.
func TestHookStalenessFromRecorded_VersionAdvanced_DeployedFieldCarriesRecordedVersion(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "2.0")
	if len(deltas) == 0 {
		t.Fatal("expected a delta but got none")
	}

	if deltas[0].Deployed != "1.0" {
		t.Errorf("delta.Deployed = %q, want %q (the recorded version)", deltas[0].Deployed, "1.0")
	}
}

// TestHookStalenessFromRecorded_VersionAdvanced_SourceFieldCarriesSourceVersion verifies that
// the delta's Source field carries the sourceVersion argument — the current catalog version —
// so the review screen shows the version the bundle will be updated to.
func TestHookStalenessFromRecorded_VersionAdvanced_SourceFieldCarriesSourceVersion(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "2.0")
	if len(deltas) == 0 {
		t.Fatal("expected a delta but got none")
	}

	if deltas[0].Source != "2.0" {
		t.Errorf("delta.Source = %q, want %q (the source version)", deltas[0].Source, "2.0")
	}
}

// ---------------------------------------------------------------------------
// HookStalenessFromRecorded — empty recorded version (pre-version-recording manifest entry)
// ---------------------------------------------------------------------------

// TestHookStalenessFromRecorded_EmptyRecordedNonEmptySource_ReturnsDelta verifies that when
// the recorded version is empty (the manifest entry was created before version recording was
// implemented) and the source version is non-empty, exactly one delta is returned. The empty
// recorded version is evidence that the manifest predates version tracking, and the bundle
// should be updated to stamp the current version.
func TestHookStalenessFromRecorded_EmptyRecordedNonEmptySource_ReturnsDelta(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("", "1.0")

	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta when recorded is empty and source is non-empty, got %d: %v",
			len(deltas), deltas)
	}
	if deltas[0].Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty (the absent recorded version)", deltas[0].Deployed)
	}
	if deltas[0].Source != "1.0" {
		t.Errorf("delta.Source = %q, want %q", deltas[0].Source, "1.0")
	}
}

// TestHookStalenessFromRecorded_NonEmptyRecordedEmptySource_ReturnsDelta verifies the reverse
// case: a non-empty recorded version against an empty source version also produces a delta.
// This handles a source bundle whose version field was removed or is absent.
func TestHookStalenessFromRecorded_NonEmptyRecordedEmptySource_ReturnsDelta(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "")

	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta when recorded is non-empty and source is empty, got %d: %v",
			len(deltas), deltas)
	}
}

// ---------------------------------------------------------------------------
// HookStalenessFromRecorded — return type invariants
// ---------------------------------------------------------------------------

// TestHookStalenessFromRecorded_EqualVersions_ReturnsNilNotEmptySlice verifies that the nil
// return (not an empty slice) is used when versions match. Callers use len(deltas) > 0 to
// gate updates, so nil vs empty slice does not matter functionally — but returning nil follows
// the existing convention established by HookStaleness and AgentStaleness.
func TestHookStalenessFromRecorded_EqualVersions_ReturnsNilNotEmptySlice(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "1.0")

	if deltas != nil {
		t.Errorf("expected nil (not empty slice) when versions match, got %v; "+
			"HookStalenessFromRecorded must follow the nil-for-no-deltas convention "+
			"established by HookStaleness and AgentStaleness", deltas)
	}
}

// TestHookStalenessFromRecorded_VersionsDiffer_ReturnsSingleDeltaNotMultiple verifies that the
// function returns at most one delta regardless of how different the versions are. Hook bundles
// have a single "version" field, not the multi-field set that agents carry.
func TestHookStalenessFromRecorded_VersionsDiffer_ReturnsSingleDeltaNotMultiple(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("0.1", "9.9.9")

	if len(deltas) != 1 {
		t.Errorf("expected exactly 1 delta for hook version mismatch, got %d: %v; "+
			"hook bundles have a single version field, not a multi-field set", len(deltas), deltas)
	}
}

// TestHookStalenessFromRecorded_DeltaFieldNeverCollidesWithAgentFields verifies that the delta
// field name "version" does not accidentally match the agent-specific fields that classifyAgentItem
// generates ("transform_version", "injections_version", etc.). This is a documentation test that
// confirms the hook delta has the same field name as HookStaleness, keeping the two functions
// consistent and preventing any downstream field-switch from misrouting hook deltas.
func TestHookStalenessFromRecorded_DeltaFieldNeverCollidesWithAgentFields(t *testing.T) {
	deltas := plan.HookStalenessFromRecorded("1.0", "2.0")
	if len(deltas) == 0 {
		t.Fatal("expected a delta but got none")
	}

	agentFields := []string{"harness_version", "injections_version", "orchestrator_injections_version", "tool_mappings_version"}
	for _, f := range agentFields {
		if deltas[0].Field == f {
			t.Errorf("hook delta Field = %q; must not collide with agent-specific field %q", deltas[0].Field, f)
		}
	}
}

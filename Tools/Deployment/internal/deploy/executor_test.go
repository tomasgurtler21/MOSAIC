package deploy

// executor_test.go covers the resolveVersionStamp function.
//
// resolveVersionStamp uses a string-literal switch on delta.Field to populate the
// corresponding field in a domain.VersionStamp. Go does not flag unmatched string
// switch cases at compile time, so a field rename in AgentStaleness that is not
// mirrored here silently drops the override path for ActionUpdate items — the deployed
// file receives an empty version stamp field on every subsequent update.
//
// Tests here verify:
//   - The renamed field "harness_version" correctly populates stamp.HarnessVersion.
//   - The old name "transform_version" does NOT populate stamp.HarnessVersion
//     (confirming the rename is complete: the old literal is gone).
//   - All other delta field names (version, injections_version,
//     orchestrator_injections_version, tool_mappings_version) continue to populate
//     their respective fields without regression.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// "harness_version" delta field: renamed from "transform_version"
// ---------------------------------------------------------------------------

// TestResolveVersionStamp_HarnessVersionDelta_PopulatesHarnessVersionField verifies that a
// VersionDelta with Field "harness_version" correctly sets stamp.HarnessVersion on an
// ActionUpdate item. This is the critical post-rename assertion: without it, the switch
// case silently fails and every subsequent update deploys an empty harness version stamp.
func TestResolveVersionStamp_HarnessVersionDelta_PopulatesHarnessVersionField(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/test-agent.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "harness_version", Source: "4.0"},
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/test-agent.md": {HarnessVersion: "3.0"},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.HarnessVersion != "4.0" {
		t.Errorf("resolveVersionStamp with harness_version delta: HarnessVersion = %q, want %q; "+
			"the switch case for \"harness_version\" must be present and must set stamp.HarnessVersion",
			stamp.HarnessVersion, "4.0")
	}
}

// TestResolveVersionStamp_OldTransformVersionDelta_DoesNotMatchHarnessVersionField verifies
// that the old field name "transform_version" does NOT match the renamed case "harness_version".
// Once AgentStaleness produces "harness_version" deltas, no delta named "transform_version"
// will be produced in normal operation. This test confirms that a stray "transform_version"
// delta (from a buggy caller) does not accidentally populate HarnessVersion.
func TestResolveVersionStamp_OldTransformVersionDelta_DoesNotMatchHarnessVersionField(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/test-agent.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "transform_version", Source: "4.0"}, // old name
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/test-agent.md": {HarnessVersion: "3.0"},
	}

	stamp := resolveVersionStamp(item, stamps)

	// "transform_version" must not match the "harness_version" case.
	// HarnessVersion should remain at the map entry's value "3.0" (not overridden by 4.0).
	if stamp.HarnessVersion != "3.0" {
		t.Errorf("resolveVersionStamp with old transform_version delta: HarnessVersion = %q, want %q "+
			"(delta with old name must not match the renamed switch case)",
			stamp.HarnessVersion, "3.0")
	}
}

// ---------------------------------------------------------------------------
// ActionUpdate: stale deltas override the stamp map values
// ---------------------------------------------------------------------------

// TestResolveVersionStamp_ActionUpdate_HarnessVersionDelta_OverridesMapValue verifies that
// for ActionUpdate items, a "harness_version" delta's Source overrides the stamp map's
// HarnessVersion value. This is the authoritative-delta contract for ActionUpdate.
func TestResolveVersionStamp_ActionUpdate_HarnessVersionDelta_OverridesMapValue(t *testing.T) {
	const mapValue = "2.0"
	const deltaSource = "3.0"

	item := domain.PlanItem{
		TargetPath: "agents/test-agent.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "harness_version", Source: deltaSource},
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/test-agent.md": {HarnessVersion: mapValue},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.HarnessVersion != deltaSource {
		t.Errorf("ActionUpdate with harness_version delta: HarnessVersion = %q, want %q (delta source overrides map)",
			stamp.HarnessVersion, deltaSource)
	}
}

// TestResolveVersionStamp_ActionCreate_HarnessVersionDelta_Ignored verifies that for
// non-ActionUpdate items (e.g., ActionCreate), stale deltas are not applied. The stamp
// comes entirely from the map for non-update actions.
func TestResolveVersionStamp_ActionCreate_HarnessVersionDelta_Ignored(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/test-agent.md",
		Action:     domain.ActionCreate,
		Stale: []domain.VersionDelta{
			{Field: "harness_version", Source: "9.9"}, // would override if ActionUpdate
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/test-agent.md": {HarnessVersion: "2.0"},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.HarnessVersion != "2.0" {
		t.Errorf("ActionCreate item: HarnessVersion = %q, want %q (delta must not override for non-ActionUpdate)",
			stamp.HarnessVersion, "2.0")
	}
}

// ---------------------------------------------------------------------------
// Non-regression: other delta field names still populate correctly
// ---------------------------------------------------------------------------

// TestResolveVersionStamp_VersionDelta_PopulatesVersion verifies that "version" delta field
// still populates stamp.Version after the harness_version rename (no regression).
func TestResolveVersionStamp_VersionDelta_PopulatesVersion(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/test-agent.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "version", Source: "2.0"},
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/test-agent.md": {Version: "1.0"},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.Version != "2.0" {
		t.Errorf("version delta: Version = %q, want %q", stamp.Version, "2.0")
	}
}

// TestResolveVersionStamp_InjectionsVersionDelta_PopulatesInjectionsVersion verifies that
// "injections_version" delta field still populates stamp.InjectionsVersion (no regression).
func TestResolveVersionStamp_InjectionsVersionDelta_PopulatesInjectionsVersion(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/test-agent.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "injections_version", Source: "5.0"},
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/test-agent.md": {InjectionsVersion: "4.0"},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.InjectionsVersion != "5.0" {
		t.Errorf("injections_version delta: InjectionsVersion = %q, want %q",
			stamp.InjectionsVersion, "5.0")
	}
}

// TestResolveVersionStamp_OrchestratorInjectionsVersionDelta_PopulatesField verifies that a
// VersionDelta with Field "orchestrator_injections_version" correctly sets
// stamp.OrchestratorInjectionsVersion on an ActionUpdate item. This is an isolated regression
// guard for the orchestrator_injections_version path after the harness_version rename: the
// rename must not have inadvertently disrupted any adjacent switch cases.
func TestResolveVersionStamp_OrchestratorInjectionsVersionDelta_PopulatesField(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "orchestrator_injections_version", Source: "8.0"},
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/orchestrator.md": {OrchestratorInjectionsVersion: "7.0"},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.OrchestratorInjectionsVersion != "8.0" {
		t.Errorf("resolveVersionStamp with orchestrator_injections_version delta: "+
			"OrchestratorInjectionsVersion = %q, want %q; "+
			"the switch case for \"orchestrator_injections_version\" must populate the field",
			stamp.OrchestratorInjectionsVersion, "8.0")
	}
}

// TestResolveVersionStamp_AllRenamedAndPreservedFields_OverrideCorrectly verifies that a
// single ActionUpdate item with deltas for all field names — including the renamed
// "harness_version" — produces the expected stamp with all fields overridden.
func TestResolveVersionStamp_AllRenamedAndPreservedFields_OverrideCorrectly(t *testing.T) {
	item := domain.PlanItem{
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "version", Source: "2.0"},
			{Field: "harness_version", Source: "3.0"},
			{Field: "injections_version", Source: "4.0"},
			{Field: "orchestrator_injections_version", Source: "5.0"},
			{Field: "tool_mappings_version", Source: "6.0"},
		},
	}
	stamps := map[string]domain.VersionStamp{
		"agents/orchestrator.md": {
			Version:                       "1.0",
			HarnessVersion:                "1.0",
			InjectionsVersion:             "1.0",
			OrchestratorInjectionsVersion: "1.0",
			ToolMappingsVersion:           "1.0",
		},
	}

	stamp := resolveVersionStamp(item, stamps)

	if stamp.Version != "2.0" {
		t.Errorf("version: got %q, want %q", stamp.Version, "2.0")
	}
	if stamp.HarnessVersion != "3.0" {
		t.Errorf("HarnessVersion: got %q, want %q", stamp.HarnessVersion, "3.0")
	}
	if stamp.InjectionsVersion != "4.0" {
		t.Errorf("InjectionsVersion: got %q, want %q", stamp.InjectionsVersion, "4.0")
	}
	if stamp.OrchestratorInjectionsVersion != "5.0" {
		t.Errorf("OrchestratorInjectionsVersion: got %q, want %q", stamp.OrchestratorInjectionsVersion, "5.0")
	}
	if stamp.ToolMappingsVersion != "6.0" {
		t.Errorf("ToolMappingsVersion: got %q, want %q", stamp.ToolMappingsVersion, "6.0")
	}
}

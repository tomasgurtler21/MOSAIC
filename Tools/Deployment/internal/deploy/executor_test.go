package deploy

// executor_test.go covers the resolveVersionStamp function and ActionRecord.SourceVersion
// population for agent artifacts.
//
// resolveVersionStamp tests:
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
//
// ActionRecord.SourceVersion tests:
// ActionRecord carries the deployed agent's declared frontmatter version so that
// deploy --output json exposes it per agent. These tests verify:
//   - An agent plan item whose SourceVersion is non-empty produces an ActionRecord
//     with SourceVersion set to that value.
//   - An agent plan item with no declared version produces an ActionRecord with an
//     empty SourceVersion.
//   - A non-agent artifact (skill) always produces an ActionRecord with an empty
//     SourceVersion, regardless of any SourceVersion on the plan item.
// Tests are written against simulateAction (the DryRun code path) because it is a
// package-level function accessible from this white-box test file without the full
// executor dependency graph. The same population contract applies to the non-DryRun
// paths (executeItem, executeFallbackItem, executeConflict); those will satisfy the
// same assertions once the implementation populates the field at all sites.

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

// ---------------------------------------------------------------------------
// ActionRecord.SourceVersion: agent artifacts carry the declared version
// ---------------------------------------------------------------------------

// TestSimulateAction_AgentArtifact_DeclaredVersion_PopulatesSourceVersion verifies that
// when an agent plan item carries a declared SourceVersion, the resulting ActionRecord
// has that same value in its SourceVersion field. This is the primary behavioral
// specification: the executor must copy the declared version from the plan item to the
// action record for every agent artifact action (Create, Update, Unchanged, and Conflict).
func TestSimulateAction_AgentArtifact_DeclaredVersion_PopulatesSourceVersion(t *testing.T) {
	// Arrange
	item := domain.PlanItem{
		Ref:           domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "my-agent"},
		TargetPath:    "agents/my-agent.md",
		Action:        domain.ActionCreate,
		SourceVersion: "1.2.3",
	}
	req := ExecRequest{DryRun: true}

	// Act
	ar := simulateAction(item, req)

	// Assert
	if ar.SourceVersion != "1.2.3" {
		t.Errorf("agent artifact with declared version: ActionRecord.SourceVersion = %q, want %q; "+
			"the executor must copy PlanItem.SourceVersion into the ActionRecord for agent artifacts",
			ar.SourceVersion, "1.2.3")
	}
}

// TestSimulateAction_AgentArtifact_NoDeclaredVersion_SourceVersionIsEmpty verifies that
// when an agent declares no version in its frontmatter, ActionRecord.SourceVersion is
// empty. An empty value is the legitimate "no version declared" state and must never
// be substituted with a fabricated or reconstructed value.
func TestSimulateAction_AgentArtifact_NoDeclaredVersion_SourceVersionIsEmpty(t *testing.T) {
	// Arrange
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "unversioned-agent"},
		TargetPath: "agents/unversioned-agent.md",
		Action: domain.ActionCreate,
		// SourceVersion deliberately absent (zero value "")
	}
	req := ExecRequest{DryRun: true}

	// Act
	ar := simulateAction(item, req)

	// Assert
	if ar.SourceVersion != "" {
		t.Errorf("agent artifact with no declared version: ActionRecord.SourceVersion = %q, want %q; "+
			"an absent version must not be fabricated — the field must remain empty",
			ar.SourceVersion, "")
	}
}

// TestSimulateAction_AgentArtifact_UpdateAction_DeclaredVersion_PopulatesSourceVersion
// verifies that the SourceVersion contract holds for ActionUpdate items, not just
// ActionCreate. Update is the most common production path (agents that already exist
// are updated, not re-created from scratch on each deployment).
func TestSimulateAction_AgentArtifact_UpdateAction_DeclaredVersion_PopulatesSourceVersion(t *testing.T) {
	// Arrange
	item := domain.PlanItem{
		Ref:           domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "my-agent"},
		TargetPath:    "agents/my-agent.md",
		Action:        domain.ActionUpdate,
		SourceVersion: "2.0.0",
	}
	req := ExecRequest{DryRun: true}

	// Act
	ar := simulateAction(item, req)

	// Assert
	if ar.SourceVersion != "2.0.0" {
		t.Errorf("agent artifact (ActionUpdate) with declared version: ActionRecord.SourceVersion = %q, want %q",
			ar.SourceVersion, "2.0.0")
	}
}

// TestSimulateAction_NonAgentArtifact_SourceVersionIsAlwaysEmpty verifies that for
// non-agent artifacts (e.g., skills), ActionRecord.SourceVersion is always empty,
// even if a SourceVersion value is present on the plan item. Non-agent kinds have
// no declared agent version and must never produce a non-empty SourceVersion.
func TestSimulateAction_NonAgentArtifact_SourceVersionIsAlwaysEmpty(t *testing.T) {
	// Arrange
	item := domain.PlanItem{
		Ref:           domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
		TargetPath:    "skills/lean-tdd/SKILL.md",
		Action:        domain.ActionCreate,
		SourceVersion: "should-be-ignored", // non-agent kinds must not carry this through
	}
	req := ExecRequest{DryRun: true}

	// Act
	ar := simulateAction(item, req)

	// Assert
	if ar.SourceVersion != "" {
		t.Errorf("skill artifact: ActionRecord.SourceVersion = %q, want %q; "+
			"non-agent artifacts must never carry a SourceVersion in the ActionRecord",
			ar.SourceVersion, "")
	}
}

// TestSimulateAction_AgentArtifact_UnchangedAction_DeclaredVersion_PopulatesSourceVersion
// verifies that the SourceVersion contract holds for ActionUnchanged items. An agent that
// is already up to date must still report its declared version so that downstream consumers
// (e.g., deploy --output json) can read the version without distinguishing the action type.
func TestSimulateAction_AgentArtifact_UnchangedAction_DeclaredVersion_PopulatesSourceVersion(t *testing.T) {
	// Arrange
	item := domain.PlanItem{
		Ref:           domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "stable-agent"},
		TargetPath:    "agents/stable-agent.md",
		Action:        domain.ActionUnchanged,
		SourceVersion: "4.5.6",
	}
	req := ExecRequest{DryRun: true}

	// Act
	ar := simulateAction(item, req)

	// Assert
	if ar.SourceVersion != "4.5.6" {
		t.Errorf("agent artifact (ActionUnchanged) with declared version: ActionRecord.SourceVersion = %q, want %q; "+
			"the SourceVersion contract must hold for all action types, including Unchanged",
			ar.SourceVersion, "4.5.6")
	}
}

// ---------------------------------------------------------------------------
// ActionRecord.SourceVersion: executeItem (non-DryRun path)
// ---------------------------------------------------------------------------
//
// The tests below exercise executeItem, which is the primary production path taken when
// DryRun is false. simulateAction is called only on the DryRun path, so a defect that
// populates SourceVersion only in simulateAction would make the DryRun tests pass while
// leaving deploy --output json (the non-DryRun path) without version data. These tests
// close that gap by calling executeItem directly.
//
// ActionUnchanged is chosen as the action type because it returns the ActionRecord
// immediately without calling req.Content or performing any filesystem write. This keeps
// the test free of I/O and stub infrastructure while still exercising the executeItem
// code path instead of simulateAction.

// TestExecuteItem_AgentArtifact_DeclaredVersion_PopulatesSourceVersion verifies that when
// the non-DryRun executor processes an agent plan item carrying a declared SourceVersion,
// the ActionRecord it produces carries that same value. This is the primary enforcement
// test for the non-DryRun path: an implementation that only fixes simulateAction would
// fail here, proving that deploy --output json would never carry the version.
func TestExecuteItem_AgentArtifact_DeclaredVersion_PopulatesSourceVersion(t *testing.T) {
	// Arrange
	// executor fields (store, log, todos) are interfaces; they are not called by the
	// ActionUnchanged branch of executeItem, so nil is safe here.
	e := &executor{}
	item := domain.PlanItem{
		Ref:           domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "versioned-agent"},
		TargetPath:    "agents/versioned-agent.md",
		Action:        domain.ActionUnchanged,
		SourceVersion: "3.1.4",
	}
	// DryRun: false — this is the production path; executeItem is not called on DryRun runs.
	req := ExecRequest{DryRun: false}

	// Act
	ar, _ := e.executeItem(item, req, "/deploy/root", domain.Manifest{}, nil)

	// Assert
	if ar.SourceVersion != "3.1.4" {
		t.Errorf("executeItem (non-DryRun) agent artifact with declared version: "+
			"ActionRecord.SourceVersion = %q, want %q; "+
			"the non-DryRun path must copy PlanItem.SourceVersion into the ActionRecord "+
			"for agent artifacts, not only the DryRun (simulateAction) path",
			ar.SourceVersion, "3.1.4")
	}
}

// TestExecuteItem_AgentArtifact_NoDeclaredVersion_SourceVersionIsEmpty verifies that when
// the non-DryRun executor processes an agent with no declared version, ActionRecord.SourceVersion
// is empty. This guards against fabrication on the production path.
func TestExecuteItem_AgentArtifact_NoDeclaredVersion_SourceVersionIsEmpty(t *testing.T) {
	// Arrange
	e := &executor{}
	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "unversioned-agent"},
		TargetPath: "agents/unversioned-agent.md",
		Action:     domain.ActionUnchanged,
		// SourceVersion deliberately absent (zero value "")
	}
	req := ExecRequest{DryRun: false}

	// Act
	ar, _ := e.executeItem(item, req, "/deploy/root", domain.Manifest{}, nil)

	// Assert
	if ar.SourceVersion != "" {
		t.Errorf("executeItem (non-DryRun) agent artifact with no declared version: "+
			"ActionRecord.SourceVersion = %q, want %q; "+
			"an absent version must not be fabricated on the production path",
			ar.SourceVersion, "")
	}
}

package plan_test

// injection_staleness_test.go covers the conditional InjectionsVersion comparison in
// AgentStaleness, controlled by DeployedArtifactState.HasInjectionRegion.
//
// Key behaviors verified:
//
//   Conditional gate (HasInjectionRegion = false):
//   - When the deployed file contains no InjectionHarness-class region, AgentStaleness
//     skips the InjectionsVersion comparison entirely and produces no injections_version delta,
//     even when deployed.InjectionsVersion and stamps.InjectionsVersion differ.
//   - This prevents false-positive staleness for agents whose harness emits no injection
//     content (e.g. mosaic-helper). The canonical false-positive is: deployed.InjectionsVersion
//     is "" (probe found no region) while stamps.InjectionsVersion is non-empty (harness has
//     injection content). Without the gate, "" != "3.0" would trigger a spurious re-deploy.
//
//   Preserved behavior (HasInjectionRegion = true):
//   - When the deployed file contains at least one InjectionHarness-class region, the
//     InjectionsVersion comparison is unchanged: a mismatch produces exactly one delta with
//     Field "injections_version" and delta.Deployed carrying the value from the deployed file.
//   - An exact match with HasInjectionRegion = true produces no injections_version delta.
//
//   Unconditional fields:
//   - Version, HarnessVersion, and ToolMappingsVersion comparisons are not gated by
//     HasInjectionRegion. A mismatch on any of these fields always produces a delta,
//     regardless of whether the deployed file has an injection region.
//
//   Orchestrator-specific comparison:
//   - The OrchestratorInjectionsVersion comparison (already role-gated for orchestrators)
//     is unaffected by HasInjectionRegion. An orchestrator with an injection region and a
//     mismatch on OrchestratorInjectionsVersion still produces the expected delta.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers specific to injection staleness tests
// ---------------------------------------------------------------------------

// deployedStateNoInjectionRegion returns a DeployedArtifactState with HasInjectionRegion=false
// and the given InjectionsVersion. All other version fields are populated with fixed values
// so that only the injection-related fields need to be varied by individual test cases.
func deployedStateNoInjectionRegion(injectionsVersion string) domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:            true,
		ContentHash:        "sha256:inject-test",
		Version:            "1.0",
		HarnessVersion:     "2.0",
		InjectionsVersion:  injectionsVersion,
		ToolMappingsVersion: "4.0",
		HasInjectionRegion: false, // no InjectionHarness region in the deployed file
	}
}

// deployedStateWithInjectionRegion returns a DeployedArtifactState with HasInjectionRegion=true
// and the given InjectionsVersion. All other version fields are populated with fixed values.
func deployedStateWithInjectionRegion(injectionsVersion string) domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:            true,
		ContentHash:        "sha256:inject-test",
		Version:            "1.0",
		HarnessVersion:     "2.0",
		InjectionsVersion:  injectionsVersion,
		ToolMappingsVersion: "4.0",
		HasInjectionRegion: true, // InjectionHarness region found in the deployed file
	}
}

// ---------------------------------------------------------------------------
// Conditional gate: HasInjectionRegion = false suppresses injections_version delta
// ---------------------------------------------------------------------------

// TestAgentStaleness_NoInjectionRegion_DeployedEmptyStampsNonEmpty_NoInjectionsDelta verifies
// the primary false-positive scenario: when the deployed file has no InjectionHarness region
// (HasInjectionRegion=false), deployed.InjectionsVersion is "" (the probe found no region to
// read from), but stamps.InjectionsVersion is non-empty (the harness has injection content).
// Without the gate, "" != "3.0" would produce a spurious injections_version delta. With the
// gate, the comparison is skipped entirely and no injections_version delta is produced.
func TestAgentStaleness_NoInjectionRegion_DeployedEmptyStampsNonEmpty_NoInjectionsDelta(t *testing.T) {
	// Arrange: deployed file has no injection region; InjectionsVersion is "" from the probe.
	deployed := deployedStateNoInjectionRegion("") // no region -> probe returns ""
	agent := makeAgent("mosaic-helper", deployed.Version)
	stamps := domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: "3.0", // harness has injection content, but this agent's body does not
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: no injections_version delta must be present.
	for _, d := range deltas {
		if d.Field == "injections_version" {
			t.Errorf(
				"agent with no injection region (HasInjectionRegion=false): "+
					"got unexpected injections_version delta (Deployed=%q, Source=%q); "+
					"the InjectionsVersion comparison must be skipped when HasInjectionRegion is false "+
					"to prevent false-positive staleness for agents with no InjectionHarness content",
				d.Deployed, d.Source,
			)
		}
	}
}

// TestAgentStaleness_NoInjectionRegion_BothNonEmptyButDiffer_NoInjectionsDelta verifies that
// even when both deployed.InjectionsVersion and stamps.InjectionsVersion are non-empty but
// differ, AgentStaleness produces no injections_version delta when HasInjectionRegion is false.
// The gate must suppress the comparison regardless of whether the deployed side is empty or not.
func TestAgentStaleness_NoInjectionRegion_BothNonEmptyButDiffer_NoInjectionsDelta(t *testing.T) {
	// Arrange: both sides have values but they differ. HasInjectionRegion is false.
	deployed := deployedStateNoInjectionRegion("2.0")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: "3.0", // differs from deployed "2.0"
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: no injections_version delta must be produced.
	for _, d := range deltas {
		if d.Field == "injections_version" {
			t.Errorf(
				"agent with HasInjectionRegion=false: got injections_version delta (Deployed=%q, Source=%q); "+
					"the gate must suppress the comparison even when both sides are non-empty and differ",
				d.Deployed, d.Source,
			)
		}
	}
}

// TestAgentStaleness_NoInjectionRegion_InjectionsVersionMismatch_TotalDeltaCount verifies
// that when HasInjectionRegion=false suppresses the injections_version comparison, only the
// remaining mismatching fields (version and harness_version in this case) produce deltas.
// The total delta count must reflect the gate: the injections_version field is not counted.
func TestAgentStaleness_NoInjectionRegion_InjectionsVersionMismatch_OnlyOtherFieldsCount(t *testing.T) {
	// Arrange: all three core version fields differ, but HasInjectionRegion=false.
	// Expected: deltas for version and harness_version only (injections_version suppressed).
	deployed := domain.DeployedArtifactState{
		Present:            true,
		ContentHash:        "sha256:abc",
		Version:            "1.0",
		HarnessVersion:     "2.0",
		InjectionsVersion:  "",    // probe returns "" when no region is present
		HasInjectionRegion: false, // no InjectionHarness region in the deployed file
	}
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1", // differs from deployed.Version "1.0"
		HarnessVersion:    "2.1", // differs from deployed.HarnessVersion "2.0"
		InjectionsVersion: "3.0", // differs from deployed.InjectionsVersion "" -- but gate should suppress
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: exactly 2 deltas (version and harness_version), no injections_version delta.
	if len(deltas) != 2 {
		t.Errorf(
			"agent with HasInjectionRegion=false and three-field mismatch: "+
				"got %d deltas, want 2 (version and harness_version only); "+
				"injections_version must be suppressed by the gate; deltas: %v",
			len(deltas), deltas,
		)
	}
	for _, d := range deltas {
		if d.Field == "injections_version" {
			t.Errorf(
				"injections_version delta found but must be suppressed when HasInjectionRegion=false; "+
					"deltas: %v",
				deltas,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Preserved behavior: HasInjectionRegion = true keeps the existing comparison
// ---------------------------------------------------------------------------

// TestAgentStaleness_WithInjectionRegion_InjectionsVersionMismatch_ProducesDelta verifies that
// when the deployed file contains an InjectionHarness region (HasInjectionRegion=true) and
// the InjectionsVersion differs from the source stamp, the existing comparison behavior is
// preserved: exactly one delta with Field "injections_version" is produced.
func TestAgentStaleness_WithInjectionRegion_InjectionsVersionMismatch_ProducesDelta(t *testing.T) {
	// Arrange: agent has an injection region; versions differ.
	deployed := deployedStateWithInjectionRegion("3.0")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: "3.1", // differs from deployed.InjectionsVersion "3.0"
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: exactly one delta with Field "injections_version".
	var injDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "injections_version" {
			injDelta = &deltas[i]
			break
		}
	}
	if injDelta == nil {
		t.Fatalf(
			"agent with HasInjectionRegion=true and InjectionsVersion mismatch: "+
				"no injections_version delta produced; "+
				"the gate must not suppress the comparison when an injection region is present; "+
				"got deltas: %v",
			deltas,
		)
	}
	if injDelta.Deployed != "3.0" {
		t.Errorf("delta.Deployed = %q, want %q (value from deployed file)", injDelta.Deployed, "3.0")
	}
	if injDelta.Source != "3.1" {
		t.Errorf("delta.Source = %q, want %q (source stamp value)", injDelta.Source, "3.1")
	}
}

// TestAgentStaleness_WithInjectionRegion_InjectionsVersionMatch_NoDelta verifies that when
// the deployed file has an injection region and the InjectionsVersion matches the source stamp,
// no injections_version delta is produced. The gate does not cause spurious non-detection.
func TestAgentStaleness_WithInjectionRegion_InjectionsVersionMatch_NoDelta(t *testing.T) {
	// Arrange: agent has an injection region; all version fields match the source stamps.
	deployed := deployedStateWithInjectionRegion("3.0")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:             deployed.Version,
		HarnessVersion:      deployed.HarnessVersion,
		InjectionsVersion:   "3.0",                    // matches deployed.InjectionsVersion
		ToolMappingsVersion: deployed.ToolMappingsVersion, // matches deployed.ToolMappingsVersion
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: no deltas at all (all fields match).
	if len(deltas) != 0 {
		t.Errorf(
			"agent with HasInjectionRegion=true and all versions matching: "+
				"got %d deltas, want 0; deltas: %v",
			len(deltas), deltas,
		)
	}
}

// ---------------------------------------------------------------------------
// Unconditional fields: Version, HarnessVersion, and ToolMappingsVersion
// ---------------------------------------------------------------------------

// TestAgentStaleness_NoInjectionRegion_HarnessVersionMismatch_DeltaStillProduced verifies
// that the HarnessVersion comparison is not affected by HasInjectionRegion. A mismatch on
// harness_version must produce a delta even when the deployed file has no injection region.
// mosaic_harness_version staleness must remain unconditional.
func TestAgentStaleness_NoInjectionRegion_HarnessVersionMismatch_DeltaStillProduced(t *testing.T) {
	// Arrange: no injection region; HarnessVersion differs, InjectionsVersion matches.
	deployed := deployedStateNoInjectionRegion("") // InjectionsVersion="" (no region)
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    "2.1", // differs from deployed.HarnessVersion "2.0"
		InjectionsVersion: "3.0", // differs, but gate should suppress injections_version
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: harness_version delta must be present.
	var harnessVersionDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "harness_version" {
			harnessVersionDelta = &deltas[i]
			break
		}
	}
	if harnessVersionDelta == nil {
		t.Errorf(
			"agent with HasInjectionRegion=false and harness_version mismatch: "+
				"no harness_version delta produced; "+
				"mosaic_harness_version comparison must remain unconditional regardless of HasInjectionRegion; "+
				"got deltas: %v",
			deltas,
		)
	}
}

// TestAgentStaleness_NoInjectionRegion_VersionMismatch_DeltaStillProduced verifies that the
// Version comparison is not gated by HasInjectionRegion. A version mismatch always produces a
// delta even when the deployed file has no injection region.
func TestAgentStaleness_NoInjectionRegion_VersionMismatch_DeltaStillProduced(t *testing.T) {
	// Arrange: no injection region; Version differs.
	deployed := deployedStateNoInjectionRegion("")
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1",   // differs from deployed.Version "1.0"
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: "3.0",   // differs but gate should suppress
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: version delta must be present.
	var versionDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "version" {
			versionDelta = &deltas[i]
			break
		}
	}
	if versionDelta == nil {
		t.Errorf(
			"agent with HasInjectionRegion=false and version mismatch: "+
				"no version delta produced; version comparison must be unconditional; "+
				"got deltas: %v",
			deltas,
		)
	}
}

// TestAgentStaleness_NoInjectionRegion_ToolMappingsVersionMismatch_DeltaStillProduced verifies
// that the ToolMappingsVersion comparison is not gated by HasInjectionRegion. A mismatch on
// tool_mappings_version must produce a delta regardless of injection region presence.
func TestAgentStaleness_NoInjectionRegion_ToolMappingsVersionMismatch_DeltaStillProduced(t *testing.T) {
	// Arrange: no injection region; ToolMappingsVersion differs.
	deployed := deployedStateNoInjectionRegion("")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:             deployed.Version,
		HarnessVersion:      deployed.HarnessVersion,
		InjectionsVersion:   "3.0",  // differs but gate should suppress
		ToolMappingsVersion: "4.1",  // differs from deployed.ToolMappingsVersion "4.0"
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: tool_mappings_version delta must be present.
	var toolDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "tool_mappings_version" {
			toolDelta = &deltas[i]
			break
		}
	}
	if toolDelta == nil {
		t.Errorf(
			"agent with HasInjectionRegion=false and tool_mappings_version mismatch: "+
				"no tool_mappings_version delta produced; "+
				"the tool_mappings_version comparison must remain unconditional; "+
				"got deltas: %v",
			deltas,
		)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator-specific: OrchestratorInjectionsVersion unaffected by HasInjectionRegion
// ---------------------------------------------------------------------------

// TestAgentStaleness_Orchestrator_WithInjectionRegion_OrchestratorInjectionsVersionMismatch_ProducesDelta
// verifies that the role-gated OrchestratorInjectionsVersion comparison is not affected by the
// HasInjectionRegion gate. An orchestrator with an injection region and a mismatch on
// orchestrator_injections_version must still produce the expected delta.
func TestAgentStaleness_Orchestrator_WithInjectionRegion_OrchestratorInjVersionMismatch_ProducesDelta(t *testing.T) {
	// Arrange: orchestrator with an injection region; OrchestratorInjectionsVersion mismatch.
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:orch-inject",
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "4.0",
		HasInjectionRegion:            true,
	}
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := domain.VersionStamps{
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "5.0", // differs from deployed "4.0"
	}

	// Act
	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// Assert: orchestrator_injections_version delta must be present.
	var orchDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "orchestrator_injections_version" {
			orchDelta = &deltas[i]
			break
		}
	}
	if orchDelta == nil {
		t.Errorf(
			"orchestrator with HasInjectionRegion=true and OrchestratorInjectionsVersion mismatch: "+
				"no orchestrator_injections_version delta produced; "+
				"the HasInjectionRegion gate must not affect the role-gated orchestrator comparison; "+
				"got deltas: %v",
			deltas,
		)
	}
}

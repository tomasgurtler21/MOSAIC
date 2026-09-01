package plan_test

// orchestrator_staleness_test.go covers AgentStaleness behavior for the
// orchestrator_injections_version field.
//
// The orchestrator_injections_version field is compared independently of injections_version.
// It follows the same staleness detection pattern as the three existing version fields, but is
// stamped only for the orchestrator agent. Subagent deployed files carry an empty string for
// this field, and the source VersionStamps also carry an empty string for subagents, so no
// spurious staleness is triggered.
//
// Key behaviors verified:
//   - A mismatch on orchestrator_injections_version alone produces exactly one delta.
//   - The delta's Field name is exactly "orchestrator_injections_version" — the name the
//     executor's resolveVersionStamp switches on when writing manifest version stamps.
//   - The field is independent of injections_version: each can mismatch without the other.
//   - When both deployed and source carry empty strings (subagent scenario), no extra delta
//     is produced beyond what the other three fields produce.
//   - When the deployed file lacks the field (pre-migration, empty string) but source is
//     non-empty, a delta is produced, triggering re-deploy after the first migration.
//   - When all four version fields mismatch, four deltas are returned in fixed order.
//   - Delta order is: version, harness_version, injections_version,
//     orchestrator_injections_version — deterministic and independent of which fields differ.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers specific to orchestrator_injections_version staleness tests
// ---------------------------------------------------------------------------

// deployedStateWithOrchVersion returns a DeployedArtifactState that includes all four version
// fields. Use this when a test must set orchestratorInjectionsVersion explicitly.
func deployedStateWithOrchVersion(contentHash, version, harnessVersion, injectionsVersion, orchestratorInjectionsVersion string) domain.DeployedArtifactState {
	s := deployedState(contentHash, version, harnessVersion, injectionsVersion)
	s.OrchestratorInjectionsVersion = orchestratorInjectionsVersion
	// Orchestrator files contain InjectionHarness-class regions; set the signal so
	// AgentStaleness performs the InjectionsVersion comparison for these tests.
	s.HasInjectionRegion = true
	return s
}

// orchestratorStamps returns VersionStamps that exactly match the given deployed state
// including the OrchestratorInjectionsVersion field (not stale).
func orchestratorStampsFromDeployed(deployed domain.DeployedArtifactState) domain.VersionStamps {
	stamps := sampleStampsFromDeployed(deployed)
	stamps.OrchestratorInjectionsVersion = deployed.OrchestratorInjectionsVersion
	return stamps
}

// ---------------------------------------------------------------------------
// OrchestratorInjectionsVersion mismatch detection
// ---------------------------------------------------------------------------

// TestAgentStaleness_OrchestratorInjectionsVersionMismatch_Only_ReturnsSingleDelta verifies that
// a mismatch on orchestrator_injections_version alone, with all other fields matching, produces
// exactly one delta naming "orchestrator_injections_version". The delta's Deployed and Source
// values carry the deployed-file value and the source catalog value respectively.
func TestAgentStaleness_OrchestratorInjectionsVersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "1.0")
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := orchestratorStampsFromDeployed(deployed)
	stamps.OrchestratorInjectionsVersion = "1.1" // differs from deployed "1.0"

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with orchestrator_injections_version mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "orchestrator_injections_version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "orchestrator_injections_version")
	}
}

// TestAgentStaleness_OrchestratorInjectionsVersion_DeltaCarriesDeployedAndSourceValues verifies
// that the delta for an orchestrator_injections_version mismatch carries the deployed-file value
// in delta.Deployed and the source catalog value in delta.Source.
func TestAgentStaleness_OrchestratorInjectionsVersion_DeltaCarriesDeployedAndSourceValues(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "1.0")
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := orchestratorStampsFromDeployed(deployed)
	stamps.OrchestratorInjectionsVersion = "2.0" // source is newer than deployed "1.0"

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Deployed != "1.0" {
		t.Errorf("delta.Deployed = %q, want %q (value read from the deployed file)", deltas[0].Deployed, "1.0")
	}
	if deltas[0].Source != "2.0" {
		t.Errorf("delta.Source = %q, want %q (source catalog value)", deltas[0].Source, "2.0")
	}
}

// TestAgentStaleness_OrchestratorInjectionsVersion_FieldName_CompatibleWithExecutorStamping
// verifies that the field name produced by AgentStaleness for an orchestrator_injections_version
// mismatch is exactly "orchestrator_injections_version" — the name the executor's
// resolveVersionStamp switches on when writing manifest version stamps.
// A different name would silently drop the orchestrator injection version from the manifest.
func TestAgentStaleness_OrchestratorInjectionsVersion_FieldName_CompatibleWithExecutorStamping(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "1.0", "1.0", "1.0")
	agent := makeOrchestrator()
	agent.Version = "2.0"
	stamps := domain.VersionStamps{
		Version:                       "2.0",
		HarnessVersion:                "1.0",
		InjectionsVersion:             "1.0",
		OrchestratorInjectionsVersion: "2.0", // only this and version differ
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var orchDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "orchestrator_injections_version" {
			orchDelta = &deltas[i]
			break
		}
	}
	if orchDelta == nil {
		t.Fatalf("no delta with Field=\"orchestrator_injections_version\"; got deltas: %v", deltas)
	}
}

// ---------------------------------------------------------------------------
// Independence from injections_version
// ---------------------------------------------------------------------------

// TestAgentStaleness_OrchestratorInjectionsVersion_IndependentOf_InjectionsVersion verifies
// that orchestrator_injections_version mismatch and injections_version mismatch are reported
// as separate, independent deltas. A mismatch on only one field does not cause a delta on
// the other.
func TestAgentStaleness_OrchestratorInjectionsVersion_IndependentOf_InjectionsVersion(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "1.0")
	agent := makeOrchestrator()
	agent.Version = deployed.Version

	t.Run("injections_mismatch_only_no_orch_delta", func(t *testing.T) {
		stamps := orchestratorStampsFromDeployed(deployed)
		stamps.InjectionsVersion = "3.1" // injections_version differs; orch matches

		deltas := plan.AgentStaleness(deployed, agent, stamps)

		if len(deltas) != 1 {
			t.Fatalf("expected 1 delta for injections_version only, got %d", len(deltas))
		}
		if deltas[0].Field != "injections_version" {
			t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "injections_version")
		}
	})

	t.Run("orch_mismatch_only_no_injections_delta", func(t *testing.T) {
		stamps := orchestratorStampsFromDeployed(deployed)
		stamps.OrchestratorInjectionsVersion = "1.1" // orch differs; injections_version matches

		deltas := plan.AgentStaleness(deployed, agent, stamps)

		if len(deltas) != 1 {
			t.Fatalf("expected 1 delta for orchestrator_injections_version only, got %d", len(deltas))
		}
		if deltas[0].Field != "orchestrator_injections_version" {
			t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "orchestrator_injections_version")
		}
	})

	t.Run("both_mismatch_produces_two_deltas", func(t *testing.T) {
		stamps := orchestratorStampsFromDeployed(deployed)
		stamps.InjectionsVersion = "3.1"             // injections_version differs
		stamps.OrchestratorInjectionsVersion = "1.1" // orch also differs

		deltas := plan.AgentStaleness(deployed, agent, stamps)

		if len(deltas) != 2 {
			t.Fatalf("expected 2 deltas when both injections fields differ, got %d", len(deltas))
		}
	})
}

// ---------------------------------------------------------------------------
// Subagent scenario: both sides empty
// ---------------------------------------------------------------------------

// TestAgentStaleness_OrchestratorInjectionsVersion_Subagent_BothEmpty_NoExtraDelta verifies
// that when both the deployed file's OrchestratorInjectionsVersion and the source stamps'
// OrchestratorInjectionsVersion are empty (the subagent scenario), no delta is produced for
// this field. Subagent files never carry this field; empty vs empty must not trigger staleness.
func TestAgentStaleness_OrchestratorInjectionsVersion_Subagent_BothEmpty_NoExtraDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "2.0",
		HarnessVersion:                "1.5",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "", // absent from deployed file — subagent
	}
	agent := makeAgent("some-subagent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:                       "2.0",
		HarnessVersion:                "1.5",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "", // not populated for subagents
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	// No delta expected: all four fields match (three explicitly, one by both being empty).
	if len(deltas) != 0 {
		t.Errorf("AgentStaleness for subagent with both OrchestratorInjectionsVersion empty: "+
			"got %d deltas, want 0; empty vs empty must not produce a delta", len(deltas))
	}
}

// ---------------------------------------------------------------------------
// Pre-migration scenario: deployed file lacks the field
// ---------------------------------------------------------------------------

// TestAgentStaleness_OrchestratorInjectionsVersion_DeployedAbsent_SourceNonEmpty_ProducesDelta
// verifies that when the deployed orchestrator file predates the migration (empty
// OrchestratorInjectionsVersion) but the source stamps carry a non-empty value, a delta is
// produced. This triggers a re-deploy on the first run after the field is introduced, ensuring
// the deployed file is updated to include the new frontmatter field.
func TestAgentStaleness_OrchestratorInjectionsVersion_DeployedAbsent_SourceNonEmpty_ProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "5.0",
		HarnessVersion:                "3.0",
		InjectionsVersion:             "2.0",
		OrchestratorInjectionsVersion: "", // pre-migration: field not yet in deployed file
	}
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := domain.VersionStamps{
		Version:                       "5.0",
		HarnessVersion:                "3.0",
		InjectionsVersion:             "2.0",
		OrchestratorInjectionsVersion: "1.0", // source now carries the field
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var orchDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "orchestrator_injections_version" {
			orchDelta = &deltas[i]
			break
		}
	}
	if orchDelta == nil {
		t.Fatal("no delta for orchestrator_injections_version; "+
			"pre-migration deployed file with empty field must produce a delta when source is non-empty")
	}
	if orchDelta.Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty string (field absent from pre-migration deployed file)",
			orchDelta.Deployed)
	}
	if orchDelta.Source != "1.0" {
		t.Errorf("delta.Source = %q, want %q", orchDelta.Source, "1.0")
	}
}

// ---------------------------------------------------------------------------
// All four fields: count and order
// ---------------------------------------------------------------------------

// TestAgentStaleness_AllFourVersionsMismatch_ReturnsFourDeltas verifies that when all four
// version fields differ, AgentStaleness returns four deltas — one per field — and all four
// carry correct Deployed and Source values.
func TestAgentStaleness_AllFourVersionsMismatch_ReturnsFourDeltas(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "4.0")
	agent := makeOrchestrator()
	agent.Version = "1.1"
	stamps := domain.VersionStamps{
		Version:                       "1.1", // all four differ
		HarnessVersion:                "2.1",
		InjectionsVersion:             "3.1",
		OrchestratorInjectionsVersion: "4.1",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 4 {
		t.Fatalf("AgentStaleness with all four fields mismatching: got %d deltas, want 4", len(deltas))
	}
}

// TestAgentStaleness_DeltaOrder_OrchestratorInjectionsVersion_IsLast verifies that when all
// four version fields mismatch, the deltas are returned in the fixed order:
//
//	version, harness_version, injections_version, orchestrator_injections_version
//
// The order must be deterministic and independent of which fields differ.
func TestAgentStaleness_DeltaOrder_OrchestratorInjectionsVersion_IsLast(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "4.0")
	agent := makeOrchestrator()
	agent.Version = "1.1"
	stamps := domain.VersionStamps{
		Version:                       "1.1",
		HarnessVersion:                "2.1",
		InjectionsVersion:             "3.1",
		OrchestratorInjectionsVersion: "4.1",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 4 {
		t.Fatalf("expected 4 deltas, got %d", len(deltas))
	}
	wantOrder := []string{
		"version",
		"harness_version",
		"injections_version",
		"orchestrator_injections_version",
	}
	for i, want := range wantOrder {
		if deltas[i].Field != want {
			t.Errorf("deltas[%d].Field = %q, want %q; delta order must be fixed", i, deltas[i].Field, want)
		}
	}
}

// TestAgentStaleness_OrchestratorInjectionsVersion_AllMatch_NoExtraDelta verifies that when
// all four version fields match — including orchestrator_injections_version — AgentStaleness
// returns an empty slice.
func TestAgentStaleness_OrchestratorInjectionsVersion_AllMatch_NoExtraDelta(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "1.0")
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := orchestratorStampsFromDeployed(deployed)

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with all four fields matching: got %d deltas, want 0", len(deltas))
	}
}

// TestAgentStaleness_OrchestratorInjectionsVersion_Downgrade_StillProducesDelta verifies that
// a downgrade scenario (deployed has a newer orchestrator_injections_version than source)
// still produces a delta. Version delta direction is irrelevant: any difference is stale.
func TestAgentStaleness_OrchestratorInjectionsVersion_Downgrade_StillProducesDelta(t *testing.T) {
	deployed := deployedStateWithOrchVersion("sha256:aaaa", "1.0", "2.0", "3.0", "2.0")
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := orchestratorStampsFromDeployed(deployed)
	stamps.OrchestratorInjectionsVersion = "1.0" // source is older than deployed "2.0"

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var orchDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "orchestrator_injections_version" {
			orchDelta = &deltas[i]
			break
		}
	}
	if orchDelta == nil {
		t.Error("AgentStaleness with downgraded orchestrator_injections_version: got no delta; " +
			"downgrade must still be detected as stale")
	}
}

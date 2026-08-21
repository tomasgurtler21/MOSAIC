package plan_test

// harness_version_staleness_test.go covers AgentStaleness behavior after the
// TransformVersion -> HarnessVersion field rename across domain structs.
//
// Key behaviors verified:
//
//   - A mismatch on HarnessVersion produces a delta with Field "harness_version"
//     (not the old name "transform_version").
//
//   - The delta field name "harness_version" is exactly what the executor's
//     resolveVersionStamp switches on when writing manifest version stamps. Any
//     different name silently drops the manifest stamp, leaving deployed files
//     with empty harness version fields. This test locks that coupling.
//
//   - The fixed delta order after the rename is: version, harness_version,
//     injections_version, orchestrator_injections_version (for orchestrators).
//     Inserting "harness_version" in the same position as the old "transform_version"
//     preserves the deterministic ordering contract.
//
//   - The orchestrator_injections_version comparison is now role-conditional:
//     it fires only when agent.Role == domain.RoleOrchestrator. For non-orchestrator
//     agents (workers, utility, standalone), the comparison is skipped entirely,
//     preventing false positives from empty-vs-empty or unexpected field population.
//
//   - A non-orchestrator agent with both deployed.OrchestratorInjectionsVersion and
//     stamps.OrchestratorInjectionsVersion set to non-empty but equal values produces
//     no delta -- the comparison is skipped, not run.
//
//   - An orchestrator agent with a mismatch on orchestrator_injections_version still
//     produces the expected delta (the role gate does not over-apply).

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers specific to harness version staleness tests
// ---------------------------------------------------------------------------

// deployedStateWithHarnessVersion returns a DeployedArtifactState with HarnessVersion set.
// All other version fields are populated so that only HarnessVersion needs to be varied
// by individual test cases to produce a single-field mismatch.
func deployedStateWithHarnessVersion(harnessVersion string) domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:hv-test",
		Version:           "1.0",
		HarnessVersion:    harnessVersion,
		InjectionsVersion: "3.0",
	}
}

// stampsMatchingHarnessDeployed returns VersionStamps that exactly match the given deployed
// state including HarnessVersion. Use this to establish a clean baseline before introducing
// a single mismatch.
func stampsMatchingHarnessDeployed(deployed domain.DeployedArtifactState) domain.VersionStamps {
	return domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: deployed.InjectionsVersion,
	}
}

// ---------------------------------------------------------------------------
// HarnessVersion field: mismatch detection and delta field name
// ---------------------------------------------------------------------------

// TestAgentStaleness_HarnessVersionMismatch_ProducesDeltaNamedHarnessVersion verifies that a
// mismatch on HarnessVersion (renamed from TransformVersion) produces exactly one delta with
// Field "harness_version" (not the old "transform_version"). The executor's resolveVersionStamp
// switches on this string literal; any mismatch silently drops the harness version stamp.
func TestAgentStaleness_HarnessVersionMismatch_ProducesDeltaNamedHarnessVersion(t *testing.T) {
	deployed := deployedStateWithHarnessVersion("3.0")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := stampsMatchingHarnessDeployed(deployed)
	stamps.HarnessVersion = "4.0" // differs from deployed "3.0"

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with HarnessVersion mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "harness_version" {
		t.Errorf("delta.Field = %q, want %q; "+
			"the renamed field must produce a delta named \"harness_version\", not \"transform_version\"",
			deltas[0].Field, "harness_version")
	}
}

// TestAgentStaleness_HarnessVersionMismatch_DeltaCarriesDeployedAndSourceValues verifies that
// the HarnessVersion delta carries the value read from the deployed file in delta.Deployed
// and the source catalog value in delta.Source.
func TestAgentStaleness_HarnessVersionMismatch_DeltaCarriesDeployedAndSourceValues(t *testing.T) {
	deployed := deployedStateWithHarnessVersion("3.0")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := stampsMatchingHarnessDeployed(deployed)
	stamps.HarnessVersion = "4.0"

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Deployed != "3.0" {
		t.Errorf("delta.Deployed = %q, want %q (value read from deployed file)", deltas[0].Deployed, "3.0")
	}
	if deltas[0].Source != "4.0" {
		t.Errorf("delta.Source = %q, want %q (source catalog value)", deltas[0].Source, "4.0")
	}
}

// TestAgentStaleness_HarnessVersionMatch_NoHarnessVersionDelta verifies that when all version
// fields match — including HarnessVersion — AgentStaleness returns an empty slice.
func TestAgentStaleness_HarnessVersionMatch_NoHarnessVersionDelta(t *testing.T) {
	deployed := deployedStateWithHarnessVersion("3.0")
	agent := makeAgent("test-agent", deployed.Version)
	stamps := stampsMatchingHarnessDeployed(deployed)

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with all matching fields including HarnessVersion: got %d deltas, want 0", len(deltas))
	}
}

// TestAgentStaleness_HarnessVersionField_Name_CompatibleWithExecutorStamping verifies that
// the field name produced by AgentStaleness for a HarnessVersion mismatch is exactly
// "harness_version" — the name the executor's resolveVersionStamp switches on. This test
// is the compile-time-immune counterpart to the executor's switch statement: Go does not
// flag mismatched string switch cases, so this test is the only safeguard.
func TestAgentStaleness_HarnessVersionField_Name_CompatibleWithExecutorStamping(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: "3.0",
	}
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1",
		HarnessVersion:    "2.1",
		InjectionsVersion: "3.1",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var harnessVersionDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "harness_version" {
			harnessVersionDelta = &deltas[i]
			break
		}
	}
	if harnessVersionDelta == nil {
		t.Fatalf("no delta with Field=\"harness_version\"; got deltas: %v; "+
			"this field name is relied on by the executor's resolveVersionStamp for manifest stamping",
			deltas)
	}
}

// TestAgentStaleness_DeltaOrder_HarnessVersionInPositionTwo verifies that when all three
// core version fields mismatch, the delta for "harness_version" is at index 1 — the same
// position that "transform_version" occupied before the rename. The ordering contract is
// deterministic and field-rename-transparent.
func TestAgentStaleness_DeltaOrder_HarnessVersionInPositionTwo(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: "3.0",
	}
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1",
		HarnessVersion:    "2.1",
		InjectionsVersion: "3.1",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas (all fields differ), got %d: %v", len(deltas), deltas)
	}
	wantOrder := []string{"version", "harness_version", "injections_version"}
	for i, want := range wantOrder {
		if deltas[i].Field != want {
			t.Errorf("deltas[%d].Field = %q, want %q; delta order must be deterministic and use the renamed field name",
				i, deltas[i].Field, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Role-conditional orchestrator_injections_version comparison
// ---------------------------------------------------------------------------

// TestAgentStaleness_NonOrchestrator_OrchestratorInjectionsVersionMismatch_NoDelta verifies
// that when both deployed.OrchestratorInjectionsVersion and stamps.OrchestratorInjectionsVersion
// differ, but the agent is NOT an orchestrator, no delta is produced for that field. The
// role gate ensures non-orchestrator agents never produce spurious orchestrator-injection deltas
// even if the field is unexpectedly populated on either side.
func TestAgentStaleness_NonOrchestrator_OrchestratorInjectionsVersionMismatch_NoDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "5.0", // non-empty but agent is NOT orchestrator
	}
	// Use a worker-role agent (not orchestrator).
	agent := makeAgent("test-worker", "1.0")
	stamps := domain.VersionStamps{
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "6.0", // differs from deployed — but role gate should skip
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	for _, d := range deltas {
		if d.Field == "orchestrator_injections_version" {
			t.Errorf("non-orchestrator agent produced a delta with Field=%q; "+
				"the orchestrator_injections_version comparison must be skipped for non-orchestrator agents",
				d.Field)
		}
	}
}

// TestAgentStaleness_NonOrchestrator_AllVersionsMatch_NoDeltaProduced verifies that a
// non-orchestrator agent with all core version fields matching produces no deltas,
// even when OrchestratorInjectionsVersion is non-empty on the deployed side.
func TestAgentStaleness_NonOrchestrator_AllVersionsMatch_NoDeltaProduced(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "5.0",
	}
	agent := makeAgent("test-worker", "1.0")
	stamps := domain.VersionStamps{
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: "3.0",
		// OrchestratorInjectionsVersion intentionally omitted: non-orchestrator sources don't populate it
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("non-orchestrator agent with all core fields matching: got %d deltas, want 0; "+
			"deltas: %v", len(deltas), deltas)
	}
}

// TestAgentStaleness_Orchestrator_OrchestratorInjectionsVersionMismatch_ProducesDelta verifies
// that the role gate does not over-apply: an orchestrator agent with a mismatch on
// orchestrator_injections_version still produces the expected delta.
func TestAgentStaleness_Orchestrator_OrchestratorInjectionsVersionMismatch_ProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "4.0",
	}
	// Use an orchestrator-role agent.
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := domain.VersionStamps{
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "5.0", // differs from deployed "4.0"
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
		t.Fatalf("orchestrator agent with orchestrator_injections_version mismatch: "+
			"no delta produced; role gate must not suppress this comparison for orchestrators; "+
			"got deltas: %v", deltas)
	}
}

// TestAgentStaleness_Orchestrator_AllVersionsMatch_NoDeltaProduced verifies that an
// orchestrator agent with all four version fields matching (including OrchestratorInjectionsVersion)
// produces no deltas.
func TestAgentStaleness_Orchestrator_AllVersionsMatch_NoDeltaProduced(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "4.0",
	}
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := domain.VersionStamps{
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "3.0",
		OrchestratorInjectionsVersion: "4.0",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("orchestrator with all four fields matching: got %d deltas, want 0; deltas: %v",
			len(deltas), deltas)
	}
}

// ---------------------------------------------------------------------------
// NoVersionInfo and unversioned-file scenarios with renamed field
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Orchestrator InjectionsVersion: realistic production path via build.go override
// ---------------------------------------------------------------------------

// TestAgentStaleness_Orchestrator_InjectionsVersionMatchesOrchestratorValue_NoDelta verifies
// the AC3.2 production path after the build.go override described in ContractsDesign.md:
// for orchestrator agents, build.go sets stamps.InjectionsVersion to
// desc.OrchestratorInjectionsVersion (the orchestrator-specific value). The deployed file
// carries this same value as its tag version (written role-conditionally by Stage 2). When
// they match, AgentStaleness must not produce an injections_version delta — no spurious
// re-deploys for an up-to-date orchestrator.
func TestAgentStaleness_Orchestrator_InjectionsVersionMatchesOrchestratorValue_NoDelta(t *testing.T) {
	const orchInjVersion = "7.0" // the orchestrator-specific injections version
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: orchInjVersion, // tag carried the orchestrator value when deployed
	}
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	// After the build.go fix: stamps.InjectionsVersion = desc.OrchestratorInjectionsVersion
	stamps := domain.VersionStamps{
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: orchInjVersion, // build.go overrides with orchestrator value
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	for _, d := range deltas {
		if d.Field == "injections_version" {
			t.Errorf("orchestrator with deployed.InjectionsVersion == stamps.InjectionsVersion (both %q): "+
				"got unexpected injections_version delta; an up-to-date orchestrator must not be marked stale; "+
				"this requires build.go to set stamps.InjectionsVersion = desc.OrchestratorInjectionsVersion",
				orchInjVersion)
		}
	}
}

// TestAgentStaleness_Orchestrator_InjectionsVersionDiffersFromOrchestratorValue_ProducesDelta
// verifies the AC3.2 production path when the orchestrator's tag version (in deployed.InjectionsVersion)
// differs from stamps.InjectionsVersion (which build.go sets to desc.OrchestratorInjectionsVersion).
// This is the realistic staleness signal: the orchestrator file was deployed with an older
// injections version and must be updated.
func TestAgentStaleness_Orchestrator_InjectionsVersionDiffersFromOrchestratorValue_ProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: "6.0", // old orchestrator injections version on the deployed tag
	}
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	// After the build.go fix: stamps.InjectionsVersion = desc.OrchestratorInjectionsVersion (newer)
	stamps := domain.VersionStamps{
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: "7.0", // build.go uses the newer orchestrator-specific value
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var injDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "injections_version" {
			injDelta = &deltas[i]
			break
		}
	}
	if injDelta == nil {
		t.Fatalf("orchestrator with deployed.InjectionsVersion=%q and stamps.InjectionsVersion=%q "+
			"(the orchestrator-specific value): no injections_version delta produced; "+
			"a stale orchestrator must be detected via the injections_version field; "+
			"got deltas: %v",
			"6.0", "7.0", deltas)
	}
}

// TestAgentStaleness_Orchestrator_BothOrchestratorInjectionsVersionEmpty_NoDelta verifies the
// realistic production case for OrchestratorInjectionsVersion after Stage 3: the read side
// (probeDeployedArtifact) no longer populates state.OrchestratorInjectionsVersion from any
// source, and build.go no longer populates stamps.OrchestratorInjectionsVersion. Both sides
// are always "". AgentStaleness must not produce an orchestrator_injections_version delta in
// this case (comparing "" != "" is false — correct).
func TestAgentStaleness_Orchestrator_BothOrchestratorInjectionsVersionEmpty_NoDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:                       true,
		ContentHash:                   "sha256:abc",
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "7.0",
		OrchestratorInjectionsVersion: "", // read side: always empty after Stage 3
	}
	agent := makeOrchestrator()
	agent.Version = deployed.Version
	stamps := domain.VersionStamps{
		Version:                       "1.0",
		HarnessVersion:                "2.0",
		InjectionsVersion:             "7.0",
		OrchestratorInjectionsVersion: "", // build.go: no longer populated after Stage 3
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	for _, d := range deltas {
		if d.Field == "orchestrator_injections_version" {
			t.Errorf("orchestrator with both OrchestratorInjectionsVersion empty: "+
				"got unexpected orchestrator_injections_version delta; "+
				"this is the realistic post-Stage-3 state and must produce no delta; "+
				"got deltas: %v", deltas)
		}
	}
}

// ---------------------------------------------------------------------------
// NoVersionInfo and unversioned-file scenarios with renamed field
// ---------------------------------------------------------------------------

// TestAgentStaleness_NoHarnessVersion_SourceHasValue_ProducesDelta verifies that when the
// deployed file carries no HarnessVersion stamp (empty string) but the source has a value,
// AgentStaleness produces a delta for the harness_version field with an empty Deployed value.
func TestAgentStaleness_NoHarnessVersion_SourceHasValue_ProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "1.0",
		HarnessVersion:    "", // no stamp in deployed file
		InjectionsVersion: "3.0",
	}
	agent := makeAgent("test-agent", "1.0")
	stamps := domain.VersionStamps{
		Version:           "1.0",
		HarnessVersion:    "2.0", // source has a value
		InjectionsVersion: "3.0",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var harnessVersionDelta *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "harness_version" {
			harnessVersionDelta = &deltas[i]
			break
		}
	}
	if harnessVersionDelta == nil {
		t.Fatal("no harness_version delta when deployed file has no stamp; " +
			"an unversioned file cannot be proven current and must produce a delta")
	}
	if harnessVersionDelta.Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty string (no stamp in deployed file)",
			harnessVersionDelta.Deployed)
	}
}

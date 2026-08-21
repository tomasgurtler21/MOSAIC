package plan_test

// staleness_test.go covers version staleness comparison and local-modification detection.
//
// Staleness-function tests (T4.1, T4.8):
//   - AgentStaleness, SkillStaleness, HookStaleness compare source stamps against the versions
//     read from the deployed file (domain.DeployedArtifactState), not against the manifest entry.
//   - Each of the three agent version fields drives staleness independently.
//   - A mismatch on any single field produces one delta naming that field.
//   - The delta carries the value from the deployed file in delta.Deployed, so the review
//     screen can show the truthful "deployed version changed from X to Y" message.
//   - Deltas are returned in the fixed order: version, harness_version, injections_version.
//   - All fields matching produces an empty slice (not stale).
//   - A deployed file carrying no version stamps at all (all fields empty) produces deltas
//     whenever the source has non-empty stamps; delta.Deployed is "" in each case.
//   - Skills and hook bundles use their single "version" field only.
//
// Local-modification detection (T4.5, T4.8):
//   - The deployed file's content hash is compared against the manifest's recorded hash.
//   - A hash mismatch classifies the item as ActionConflict regardless of staleness.
//   - A deployed file whose hash matches the manifest's recorded hash, and whose version
//     stamps match the source stamps, classifies as ActionUnchanged.
//   - A file present on disk with no manifest record for the current target path is a conflict.
//
// Missing-manifest policy (T4.8):
//   - A workspace with deployed files but a StateAbsent or StateCorrupt manifest classifies
//     every present file as ActionConflict with LocalModification.ManifestMissing = true.

import (
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Staleness function helpers
// ---------------------------------------------------------------------------

// sampleDeployedState returns a DeployedArtifactState with all version fields set to
// distinct values. Callers may override individual fields to create mismatch scenarios.
func sampleDeployedState() domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:aaaa",
		Version:           "1.0",
		HarnessVersion:    "2.0",
		InjectionsVersion: "3.0",
	}
}

// sampleStampsFromDeployed returns VersionStamps that exactly match the given deployed state
// (not stale). Use this to establish a clean baseline before introducing a single mismatch.
//
// Note: OrchestratorInjectionsVersion is intentionally omitted here — it is left as the
// zero-value empty string. Tests that need the orchestrator-specific field use
// orchestratorStampsFromDeployed (in orchestrator_staleness_test.go), which extends the
// result of this helper by copying OrchestratorInjectionsVersion from the deployed state.
func sampleStampsFromDeployed(deployed domain.DeployedArtifactState) domain.VersionStamps {
	return domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: deployed.InjectionsVersion,
		// OrchestratorInjectionsVersion intentionally omitted — see note above.
	}
}

// ---------------------------------------------------------------------------
// T4.1 / T4.8 — AgentStaleness: comparing against deployed-file versions
// ---------------------------------------------------------------------------

// TestAgentStaleness_AllMatch_ReturnsEmptySlice verifies that when all three version
// fields in the deployed state match the stamps, AgentStaleness returns an empty slice.
// An empty slice is the signal that the item is not stale and should be ActionUnchanged.
func TestAgentStaleness_AllMatch_ReturnsEmptySlice(t *testing.T) {
	deployed := sampleDeployedState()
	agent := makeAgent("test-agent", deployed.Version)
	stamps := sampleStampsFromDeployed(deployed)

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with all matching fields: got %d deltas, want 0 (not stale)", len(deltas))
	}
}

// TestAgentStaleness_VersionMismatch_Only_ReturnsSingleDelta verifies that a mismatch on
// the "version" field alone produces exactly one delta naming "version".
func TestAgentStaleness_VersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	deployed := sampleDeployedState() // deployed.Version = "1.0"
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1", // differs from deployed.Version "1.0"
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: deployed.InjectionsVersion,
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with version mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "version")
	}
}

// TestAgentStaleness_VersionMismatch_DeployedCarriesFileValue verifies that the delta for
// a version mismatch carries the value actually present in the deployed file in delta.Deployed,
// not the manifest entry value. The review screen depends on this to show "file has X".
func TestAgentStaleness_VersionMismatch_DeployedCarriesFileValue(t *testing.T) {
	deployed := sampleDeployedState() // deployed.Version = "1.0" — value in the file
	stamps := domain.VersionStamps{
		Version:           "1.1", // source is newer
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: deployed.InjectionsVersion,
	}
	agent := makeAgent("test-agent", "1.1")

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Deployed != "1.0" {
		t.Errorf("delta.Deployed = %q, want %q (value read from the deployed file)", deltas[0].Deployed, "1.0")
	}
	if deltas[0].Source != "1.1" {
		t.Errorf("delta.Source = %q, want %q (source catalog value)", deltas[0].Source, "1.1")
	}
}

// TestAgentStaleness_HarnessVersionMismatch_Only_ReturnsSingleDelta verifies that a
// mismatch on "harness_version" alone produces exactly one delta naming "harness_version".
func TestAgentStaleness_HarnessVersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	deployed := sampleDeployedState()
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    "2.1", // differs from deployed.HarnessVersion "2.0"
		InjectionsVersion: deployed.InjectionsVersion,
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with harness_version mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "harness_version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "harness_version")
	}
}

// TestAgentStaleness_InjectionsVersionMismatch_Only_ReturnsSingleDelta verifies that a
// mismatch on "injections_version" alone produces exactly one delta naming "injections_version".
func TestAgentStaleness_InjectionsVersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	deployed := sampleDeployedState()
	agent := makeAgent("test-agent", deployed.Version)
	stamps := domain.VersionStamps{
		Version:           deployed.Version,
		HarnessVersion:    deployed.HarnessVersion,
		InjectionsVersion: "3.1", // differs from deployed.InjectionsVersion "3.0"
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with injections_version mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "injections_version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "injections_version")
	}
}

// TestAgentStaleness_AllThreeVersionsMismatch_ReturnsThreeDeltas verifies that when all
// three version fields differ, AgentStaleness returns three deltas, one per field.
func TestAgentStaleness_AllThreeVersionsMismatch_ReturnsThreeDeltas(t *testing.T) {
	deployed := sampleDeployedState()
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1", // all three differ from deployed
		HarnessVersion:    "2.1",
		InjectionsVersion: "3.1",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("AgentStaleness with all three fields mismatching: got %d deltas, want 3", len(deltas))
	}
}

// TestAgentStaleness_DeltaOrder_IsFixed_VersionFirst verifies that when multiple fields
// mismatch, the deltas are returned in the fixed order: version, harness_version,
// injections_version. The order must be deterministic and independent of which fields differ.
func TestAgentStaleness_DeltaOrder_IsFixed_VersionFirst(t *testing.T) {
	deployed := sampleDeployedState()
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1",
		HarnessVersion:    "2.1",
		InjectionsVersion: "3.1",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(deltas))
	}
	wantOrder := []string{"version", "harness_version", "injections_version"}
	for i, want := range wantOrder {
		if deltas[i].Field != want {
			t.Errorf("deltas[%d].Field = %q, want %q; delta order must be fixed", i, deltas[i].Field, want)
		}
	}
}

// TestAgentStaleness_PartialMismatch_VersionAndHarness_TwoDeltas verifies that a
// mismatch on two out of three fields returns exactly two deltas.
func TestAgentStaleness_PartialMismatch_VersionAndHarness_TwoDeltas(t *testing.T) {
	deployed := sampleDeployedState()
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1", // differs
		HarnessVersion:    "2.1", // differs
		InjectionsVersion: deployed.InjectionsVersion, // matches
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 2 {
		t.Fatalf("AgentStaleness with version+harness_version mismatch: got %d deltas, want 2", len(deltas))
	}
}

// TestAgentStaleness_NoVersionInfo_SourceHasValues_ProducesThreeDeltas verifies that when
// the deployed file carries no readable version stamps (all fields empty), AgentStaleness
// produces three deltas with empty delta.Deployed values. An unversioned deployed file
// cannot be proven current; the comparison produces deltas so the classifier detects stale.
func TestAgentStaleness_NoVersionInfo_SourceHasValues_ProducesThreeDeltas(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "",  // no version stamp in the deployed file
		HarnessVersion:    "",
		InjectionsVersion: "",
	}
	agent := makeAgent("test-agent", "2.0")
	stamps := domain.VersionStamps{
		Version:           "2.0",
		HarnessVersion:    "1.5",
		InjectionsVersion: "3.0",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("AgentStaleness with no version info in deployed file: got %d deltas, want 3", len(deltas))
	}
	// Every delta.Deployed must be empty, reflecting what was read from the deployed file.
	for _, d := range deltas {
		if d.Deployed != "" {
			t.Errorf("delta for field %q: Deployed = %q, want empty string (no stamp in deployed file)",
				d.Field, d.Deployed)
		}
	}
}

// TestAgentStaleness_DeltaFieldNames_CompatibleWithExecutorStamping verifies that the field
// names produced by AgentStaleness are exactly "version", "harness_version", and
// "injections_version" — the three names the executor's resolveVersionStamp switches on
// when writing manifest version stamps. Changing these names would silently break the
// manifest stamping and leave deployed files with empty version fields.
func TestAgentStaleness_DeltaFieldNames_CompatibleWithExecutorStamping(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "1.0",
		HarnessVersion:    "1.0",
		InjectionsVersion: "1.0",
	}
	agent := makeAgent("test-agent", "2.0")
	stamps := domain.VersionStamps{
		Version:           "2.0",
		HarnessVersion:    "1.5",
		InjectionsVersion: "3.0",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas (all fields differ), got %d", len(deltas))
	}

	wantFields := []string{"version", "harness_version", "injections_version"}
	for i, want := range wantFields {
		if deltas[i].Field != want {
			t.Errorf("deltas[%d].Field = %q, want %q; this name is relied on by the executor for manifest stamping",
				i, deltas[i].Field, want)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.1 / T4.8 — SkillStaleness: single version field from deployed file
// ---------------------------------------------------------------------------

// TestSkillStaleness_VersionMatch_ReturnsEmptySlice verifies that when the skill's version
// from the deployed file matches the source, SkillStaleness returns an empty slice.
func TestSkillStaleness_VersionMatch_ReturnsEmptySlice(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: "sha256:abc",
		Version:     "1.5",
	}
	skill := makeSkill("lean-tdd", "1.5") // version matches deployed file

	deltas := plan.SkillStaleness(deployed, skill)

	if len(deltas) != 0 {
		t.Errorf("SkillStaleness with matching version: got %d deltas, want 0", len(deltas))
	}
}

// TestSkillStaleness_VersionMismatch_ReturnsSingleDelta verifies that a version mismatch
// produces exactly one delta with Field = "version", carrying the deployed and source values.
func TestSkillStaleness_VersionMismatch_ReturnsSingleDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: "sha256:abc",
		Version:     "1.5", // deployed file version
	}
	skill := makeSkill("lean-tdd", "1.6") // source is newer

	deltas := plan.SkillStaleness(deployed, skill)

	if len(deltas) != 1 {
		t.Fatalf("SkillStaleness with version mismatch: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "version" {
		t.Errorf("SkillStaleness delta.Field = %q, want %q", deltas[0].Field, "version")
	}
	if deltas[0].Deployed != "1.5" {
		t.Errorf("SkillStaleness delta.Deployed = %q, want %q (value from deployed file)", deltas[0].Deployed, "1.5")
	}
	if deltas[0].Source != "1.6" {
		t.Errorf("SkillStaleness delta.Source = %q, want %q", deltas[0].Source, "1.6")
	}
}

// TestSkillStaleness_NoVersionInfo_ProducesDelta verifies that when the deployed file carries
// no version stamp (empty string), SkillStaleness produces a delta with empty Deployed.
func TestSkillStaleness_NoVersionInfo_ProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: "sha256:abc",
		Version:     "", // no version stamp in the deployed file
	}
	skill := makeSkill("lean-tdd", "1.5")

	deltas := plan.SkillStaleness(deployed, skill)

	if len(deltas) != 1 {
		t.Fatalf("SkillStaleness with no version info: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty string (no version stamp in deployed file)", deltas[0].Deployed)
	}
}

// ---------------------------------------------------------------------------
// T4.1 / T4.8 — HookStaleness: single version field from deployed file
// ---------------------------------------------------------------------------

// TestHookStaleness_VersionMatch_ReturnsEmptySlice verifies that when the hook bundle's
// version from the deployed file matches the catalog version, HookStaleness returns an empty slice.
func TestHookStaleness_VersionMatch_ReturnsEmptySlice(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: "sha256:abc",
		Version:     "2.0",
	}
	bundle := makeHookBundle("my-hooks", "2.0") // version matches

	deltas := plan.HookStaleness(deployed, bundle)

	if len(deltas) != 0 {
		t.Errorf("HookStaleness with matching version: got %d deltas, want 0", len(deltas))
	}
}

// TestHookStaleness_VersionMismatch_ReturnsSingleDelta verifies that a version mismatch
// produces exactly one delta with Field = "version".
func TestHookStaleness_VersionMismatch_ReturnsSingleDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: "sha256:abc",
		Version:     "2.0",
	}
	bundle := makeHookBundle("my-hooks", "2.1") // source is newer

	deltas := plan.HookStaleness(deployed, bundle)

	if len(deltas) != 1 {
		t.Fatalf("HookStaleness with version mismatch: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "version" {
		t.Errorf("HookStaleness delta.Field = %q, want %q", deltas[0].Field, "version")
	}
}

// TestHookStaleness_NoVersionInfo_ProducesDelta verifies that when the deployed file carries
// no version stamp, HookStaleness produces a delta with empty Deployed.
func TestHookStaleness_NoVersionInfo_ProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: "sha256:abc",
		Version:     "", // no version stamp in the deployed file
	}
	bundle := makeHookBundle("my-hooks", "2.0")

	deltas := plan.HookStaleness(deployed, bundle)

	if len(deltas) != 1 {
		t.Fatalf("HookStaleness with no version info: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty string (no version stamp in deployed file)", deltas[0].Deployed)
	}
}

// ---------------------------------------------------------------------------
// T4.5 / T4.8 — Local-modification detection via manifest hash
// ---------------------------------------------------------------------------

// buildInputWithSingleAgent returns a minimal plan.Input that includes the given agent
// via a single workflow. The agent's default target path from newFakeModule is "agents/<key>.md".
// The manifest snapshot and deployed state are provided by the caller.
func buildInputWithSingleAgent(
	agent domain.Agent,
	snap manifest.Snapshot,
	deployedState map[string]domain.DeployedArtifactState,
) plan.Input {
	wfID := "test-wf-for-" + agent.Key
	wf := makeWorkflow(wfID, agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	module := newFakeModule()
	return plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{wfID},
		UtilityAgentIDs: nil,
		Models: map[string]domain.ModelSelection{
			agent.Key: {
				ModelID: "test-model",
				Origin:  domain.OriginHarnessList,
			},
			"orchestrator": {
				ModelID: "test-model",
				Origin:  domain.OriginHarnessList,
			},
		},
		DeployedState: deployedState,
	}
}

// TestBuild_LocalModification_HashMatch_ClassifiesAsUnchanged verifies that when the current
// hash of a deployed file matches the hash recorded in the manifest, and all version stamps
// also match, the plan item is ActionUnchanged — not a conflict.
func TestBuild_LocalModification_HashMatch_ClassifiesAsUnchanged(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const hash = "sha256:abc123"

	agent := makeAgent("test-agent", "1.0")

	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", hash)
	manifestEntry.HarnessVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed file is present, hash matches, all version stamps match source.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState(hash, "1.0", "1.0", "1.0"),
	}

	input := buildInputWithSingleAgent(agent, snap, ds)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("test-agent with matching hash and versions: Action = %q, want %q",
			item.Action, domain.ActionUnchanged)
	}
}

// TestBuild_LocalModification_HashMismatch_ClassifiesAsConflict verifies that when the
// current hash of a deployed file differs from the hash in the manifest, the plan item is
// ActionConflict with a populated Conflict field.
func TestBuild_LocalModification_HashMismatch_ClassifiesAsConflict(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const recordedHash = "sha256:aaaa"
	const currentHash = "sha256:bbbb"

	agent := makeAgent("test-agent", "1.0")

	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", recordedHash)
	manifestEntry.HarnessVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed hash differs from manifest — locally modified.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState(currentHash, "1.0", "1.0", "1.0"),
	}

	input := buildInputWithSingleAgent(agent, snap, ds)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("test-agent with hash mismatch: Action = %q, want %q",
			item.Action, domain.ActionConflict)
	}
	if item.Conflict == nil {
		t.Error("test-agent with hash mismatch: Conflict is nil; expected a populated LocalModification")
	}
}

// TestBuild_LocalModification_WhitespaceChange_ClassifiesAsConflict verifies that a
// whitespace-only change is detected as a local modification. Content hashes use no
// normalisation: a line-ending or whitespace change changes the hash.
func TestBuild_LocalModification_WhitespaceChange_ClassifiesAsConflict(t *testing.T) {
	const targetPath = "agents/test-agent.md"

	// Simulate: original content was "content\n", now the file has "content\r\n".
	originalContent := []byte("content\n")
	modifiedContent := []byte("content\r\n")
	recordedHash := manifest.Hash(originalContent)
	currentHash := manifest.Hash(modifiedContent)

	if recordedHash == currentHash {
		t.Skip("test infrastructure: hash function normalises line endings; cannot test whitespace-change detection")
	}

	agent := makeAgent("test-agent", "1.0")

	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", recordedHash)
	manifestEntry.HarnessVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState(currentHash, "1.0", "1.0", "1.0"),
	}

	input := buildInputWithSingleAgent(agent, snap, ds)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("test-agent with whitespace-only hash change: Action = %q, want %q",
			item.Action, domain.ActionConflict)
	}
}

// TestBuild_LocalModification_FileWithNoManifestRecord_ClassifiesAsConflict verifies that
// a file present in DeployedState but absent from the manifest is treated as locally
// modified (ActionConflict with ManifestMissing = true). The conservative classification
// protects hand-deployed files.
func TestBuild_LocalModification_FileWithNoManifestRecord_ClassifiesAsConflict(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const currentHash = "sha256:abcdef"

	agent := makeAgent("test-agent", "1.0")

	// Manifest is present but has NO entry for test-agent.
	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       nil, // no entries
	})

	// File exists on disk (Present: true) but has no manifest record.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState(currentHash, "1.0", "1.0", "1.0"),
	}

	input := buildInputWithSingleAgent(agent, snap, ds)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("test-agent present on disk with no manifest record: Action = %q, want %q",
			item.Action, domain.ActionConflict)
	}
	if item.Conflict == nil {
		t.Error("test-agent with no manifest record: Conflict is nil; expected a populated LocalModification")
	}
}

// TestBuild_LocalModification_ManifestMissingFlag_SetWhenFileHasNoRecord verifies that when
// a file is present on disk but has no manifest record, LocalModification.ManifestMissing is
// true. This distinguishes "no record" from "hash changed" for the user-facing message.
func TestBuild_LocalModification_ManifestMissingFlag_SetWhenFileHasNoRecord(t *testing.T) {
	const targetPath = "agents/test-agent.md"

	agent := makeAgent("test-agent", "1.0")
	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       nil,
	})
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState("sha256:abc123", "1.0", "1.0", "1.0"),
	}

	input := buildInputWithSingleAgent(agent, snap, ds)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Conflict == nil {
		t.Fatal("expected Conflict to be non-nil for file with no manifest record")
	}
	if !item.Conflict.ManifestMissing {
		t.Error("LocalModification.ManifestMissing = false; expected true when file has no manifest record")
	}
}

// ---------------------------------------------------------------------------
// T4.8 — Missing-manifest policy
// ---------------------------------------------------------------------------

// buildInputWithManifestState returns a plan.Input with the given manifest snapshot and
// the agent's target path marked as present in DeployedState.
func buildInputWithManifestState(agent domain.Agent, snap manifest.Snapshot, deployedTargetPath string) plan.Input {
	ds := map[string]domain.DeployedArtifactState{
		deployedTargetPath: {
			Present:     true,
			ContentHash: "sha256:cafebabe",
		},
	}
	return buildInputWithSingleAgent(agent, snap, ds)
}

// TestBuild_MissingManifest_StateAbsent_ClassifiesDeployedFilesAsConflict verifies that
// when the manifest is absent (StateAbsent) and files exist at target paths, every such
// file is classified as ActionConflict. The conservative policy prevents silently overwriting
// hand-deployed files in workspaces that were never tracked by the manifest.
func TestBuild_MissingManifest_StateAbsent_ClassifiesDeployedFilesAsConflict(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	targetPath := "agents/test-agent.md"

	input := buildInputWithManifestState(agent, absentSnapshot(), targetPath)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("StateAbsent manifest + deployed file: Action = %q, want ActionConflict",
			item.Action)
	}
}

// TestBuild_MissingManifest_StateAbsent_ManifestMissingFlagIsTrue verifies that when the
// manifest is StateAbsent, LocalModification.ManifestMissing is true on every conflict item.
func TestBuild_MissingManifest_StateAbsent_ManifestMissingFlagIsTrue(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	targetPath := "agents/test-agent.md"

	input := buildInputWithManifestState(agent, absentSnapshot(), targetPath)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Conflict == nil {
		t.Fatal("expected Conflict to be non-nil for StateAbsent manifest + deployed file")
	}
	if !item.Conflict.ManifestMissing {
		t.Error("LocalModification.ManifestMissing = false; expected true for StateAbsent manifest")
	}
}

// TestBuild_MissingManifest_StateCorrupt_ClassifiesDeployedFilesAsConflict verifies that
// a corrupt manifest (StateCorrupt) triggers the same conservative classification as an
// absent manifest. A corrupt file cannot be read; its absence is equivalent to no manifest.
func TestBuild_MissingManifest_StateCorrupt_ClassifiesDeployedFilesAsConflict(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	targetPath := "agents/test-agent.md"

	input := buildInputWithManifestState(agent, corruptSnapshot(), targetPath)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("StateCorrupt manifest + deployed file: Action = %q, want ActionConflict",
			item.Action)
	}
}

// TestBuild_MissingManifest_StateCorrupt_ManifestMissingFlagIsTrue verifies that a corrupt
// manifest sets LocalModification.ManifestMissing to true on conflict items.
func TestBuild_MissingManifest_StateCorrupt_ManifestMissingFlagIsTrue(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	targetPath := "agents/test-agent.md"

	input := buildInputWithManifestState(agent, corruptSnapshot(), targetPath)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Conflict == nil {
		t.Fatal("expected Conflict to be non-nil for StateCorrupt manifest + deployed file")
	}
	if !item.Conflict.ManifestMissing {
		t.Error("LocalModification.ManifestMissing = false; expected true for StateCorrupt manifest")
	}
}

// TestBuild_MissingManifest_FileAbsentFromDeployedState_ClassifiesAsCreate verifies that
// when the manifest is absent but a file is NOT present in DeployedState (it doesn't exist
// on disk), the item is ActionCreate — not ActionConflict. The conflict classification only
// applies to files actually present on disk.
func TestBuild_MissingManifest_FileAbsentFromDeployedState_ClassifiesAsCreate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")

	// Absent manifest, no deployed state — file is not on disk.
	input := buildInputWithSingleAgent(agent, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("absent manifest + file not on disk: Action = %q, want ActionCreate", item.Action)
	}
}

// ---------------------------------------------------------------------------
// Version-downgrade staleness: direction of delta is irrelevant
// ---------------------------------------------------------------------------

// TestAgentStaleness_VersionDowngrade_StillProducesDelta verifies that when the deployed file
// carries a version that is higher than the source version (a downgrade scenario, e.g. after
// a catalog rollback or a manual stamp edit), AgentStaleness still produces a delta.
// Version delta direction is irrelevant: any difference between deployed and source is stale.
func TestAgentStaleness_VersionDowngrade_StillProducesDelta(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       "sha256:abc",
		Version:           "2.0", // deployed is newer than source
		HarnessVersion:    "1.0",
		InjectionsVersion: "1.0",
	}
	agent := makeAgent("test-agent", "1.0") // source is older
	stamps := domain.VersionStamps{
		Version:           "1.0",
		HarnessVersion:    "1.0",
		InjectionsVersion: "1.0",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) == 0 {
		t.Error("AgentStaleness with downgraded version: got 0 deltas, want at least 1; downgrade must still be detected as stale")
	}
	if len(deltas) > 0 && deltas[0].Deployed != "2.0" {
		t.Errorf("delta.Deployed = %q, want %q (value from deployed file)", deltas[0].Deployed, "2.0")
	}
	if len(deltas) > 0 && deltas[0].Source != "1.0" {
		t.Errorf("delta.Source = %q, want %q (source catalog value)", deltas[0].Source, "1.0")
	}
}

// TestBuild_PresentManifest_FileNotInManifest_NotInDeployedState_ClassifiesAsCreate verifies
// that an artifact in the set that has no manifest entry and is not present in DeployedState
// is classified as ActionCreate (not ActionConflict). The conflict guard applies only when
// the file is actually deployed.
func TestBuild_PresentManifest_FileNotInManifest_NotInDeployedState_ClassifiesAsCreate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")

	// Manifest is present but has no entry for test-agent; file is also not on disk.
	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       nil,
	})

	input := buildInputWithSingleAgent(agent, snap, nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("present manifest + file not in manifest + not on disk: Action = %q, want ActionCreate",
			item.Action)
	}
}

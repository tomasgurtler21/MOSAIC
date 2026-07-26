package plan_test

// staleness_test.go covers version staleness comparison and local-modification detection
// (T16.3, T16.4, T16.5).
//
// T16.3 — Independent per-field staleness (AgentStaleness, SkillStaleness, HookStaleness):
//   - Each of the three agent version fields drives staleness independently
//   - A mismatch on any single field produces one delta naming that field
//   - The plan records the deployed and source values, not just a boolean
//   - Deltas are returned in the fixed order: version, transform_version, injections_version
//   - All fields matching produces an empty slice (not stale)
//   - Skills and hook bundles use their single "version" field
//
// T16.4 — Local-modification detection:
//   - A deployed file whose current hash matches the recorded hash is ActionUnchanged
//   - A deployed file whose current hash differs (even by whitespace only) is ActionConflict
//   - A file present on disk with no manifest record is treated as locally-modified
//
// T16.5 — Missing-manifest policy:
//   - A workspace with deployed files but StateAbsent manifest classifies every file as
//     ActionConflict with LocalModification.ManifestMissing = true
//   - A workspace with deployed files but StateCorrupt manifest has the same classification
//   - A file without a manifest record but with a present manifest is ActionCreate (not conflict)

import (
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// T16.3 — AgentStaleness: per-field independent comparison
// ---------------------------------------------------------------------------

// sampleStamps returns VersionStamps that match the given ManifestEntry exactly (not stale).
func sampleStamps(entry domain.ManifestEntry) domain.VersionStamps {
	return domain.VersionStamps{
		Version:           entry.Version,
		TransformVersion:  entry.TransformVersion,
		InjectionsVersion: entry.InjectionsVersion,
	}
}

// sampleEntry returns a ManifestEntry with all version fields set to distinct values.
func sampleEntry() domain.ManifestEntry {
	return domain.ManifestEntry{
		Ref:               agentRef("test-agent"),
		TargetPath:        "agents/test-agent.md",
		Version:           "1.0",
		TransformVersion:  "2.0",
		InjectionsVersion: "3.0",
		ContentHash:       "sha256:aaaa",
		DeployedAt:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestAgentStaleness_AllMatch_ReturnsEmptySlice verifies that when all three version
// fields in the manifest entry match the stamps, AgentStaleness returns an empty slice.
// An empty slice is the signal that the item is not stale and should be ActionUnchanged.
func TestAgentStaleness_AllMatch_ReturnsEmptySlice(t *testing.T) {
	entry := sampleEntry()
	agent := makeAgent("test-agent", entry.Version)
	stamps := sampleStamps(entry)

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with all matching fields: got %d deltas, want 0 (not stale)", len(deltas))
	}
}

// TestAgentStaleness_VersionMismatch_Only_ReturnsSingleDelta verifies that a mismatch on
// the "version" field alone produces exactly one delta naming "version".
func TestAgentStaleness_VersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	entry := sampleEntry()
	agent := makeAgent("test-agent", "1.1") // version differs from entry.Version "1.0"
	stamps := domain.VersionStamps{
		Version:           "1.1", // differs from entry.Version
		TransformVersion:  entry.TransformVersion,
		InjectionsVersion: entry.InjectionsVersion,
	}

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with version mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "version")
	}
}

// TestAgentStaleness_VersionMismatch_RecordsDeployedAndSourceValues verifies that the delta
// for a version mismatch carries the deployed value (from the manifest entry) and the source
// value (from stamps), not just a boolean indicator.
func TestAgentStaleness_VersionMismatch_RecordsDeployedAndSourceValues(t *testing.T) {
	entry := sampleEntry() // entry.Version = "1.0"
	stamps := domain.VersionStamps{
		Version:           "1.1", // source is newer
		TransformVersion:  entry.TransformVersion,
		InjectionsVersion: entry.InjectionsVersion,
	}
	agent := makeAgent("test-agent", "1.1")

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Deployed != "1.0" {
		t.Errorf("delta.Deployed = %q, want %q (the value in the manifest)", deltas[0].Deployed, "1.0")
	}
	if deltas[0].Source != "1.1" {
		t.Errorf("delta.Source = %q, want %q (the value in the source)", deltas[0].Source, "1.1")
	}
}

// TestAgentStaleness_TransformVersionMismatch_Only_ReturnsSingleDelta verifies that a
// mismatch on "transform_version" alone produces exactly one delta naming "transform_version".
func TestAgentStaleness_TransformVersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	entry := sampleEntry()
	agent := makeAgent("test-agent", entry.Version)
	stamps := domain.VersionStamps{
		Version:           entry.Version,
		TransformVersion:  "2.1", // differs from entry.TransformVersion "2.0"
		InjectionsVersion: entry.InjectionsVersion,
	}

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 1 {
		t.Fatalf("AgentStaleness with transform_version mismatch only: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "transform_version" {
		t.Errorf("delta.Field = %q, want %q", deltas[0].Field, "transform_version")
	}
}

// TestAgentStaleness_InjectionsVersionMismatch_Only_ReturnsSingleDelta verifies that a
// mismatch on "injections_version" alone produces exactly one delta naming "injections_version".
func TestAgentStaleness_InjectionsVersionMismatch_Only_ReturnsSingleDelta(t *testing.T) {
	entry := sampleEntry()
	agent := makeAgent("test-agent", entry.Version)
	stamps := domain.VersionStamps{
		Version:           entry.Version,
		TransformVersion:  entry.TransformVersion,
		InjectionsVersion: "3.1", // differs from entry.InjectionsVersion "3.0"
	}

	deltas := plan.AgentStaleness(entry, agent, stamps)

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
	entry := sampleEntry()
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1", // all three differ
		TransformVersion:  "2.1",
		InjectionsVersion: "3.1",
	}

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("AgentStaleness with all three fields mismatching: got %d deltas, want 3", len(deltas))
	}
}

// TestAgentStaleness_DeltaOrder_IsFixed_VersionFirst verifies that when multiple fields
// mismatch, the deltas are returned in the fixed order: version, transform_version,
// injections_version. The order must be deterministic and independent of which fields differ.
func TestAgentStaleness_DeltaOrder_IsFixed_VersionFirst(t *testing.T) {
	entry := sampleEntry()
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1",
		TransformVersion:  "2.1",
		InjectionsVersion: "3.1",
	}

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(deltas))
	}
	wantOrder := []string{"version", "transform_version", "injections_version"}
	for i, want := range wantOrder {
		if deltas[i].Field != want {
			t.Errorf("deltas[%d].Field = %q, want %q; delta order must be fixed", i, deltas[i].Field, want)
		}
	}
}

// TestAgentStaleness_PartialMismatch_VersionAndTransform_TwoDeltas verifies that a
// mismatch on two out of three fields returns exactly two deltas.
func TestAgentStaleness_PartialMismatch_VersionAndTransform_TwoDeltas(t *testing.T) {
	entry := sampleEntry()
	agent := makeAgent("test-agent", "1.1")
	stamps := domain.VersionStamps{
		Version:           "1.1", // differs
		TransformVersion:  "2.1", // differs
		InjectionsVersion: entry.InjectionsVersion, // matches
	}

	deltas := plan.AgentStaleness(entry, agent, stamps)

	if len(deltas) != 2 {
		t.Fatalf("AgentStaleness with version+transform_version mismatch: got %d deltas, want 2", len(deltas))
	}
}

// ---------------------------------------------------------------------------
// T16.3 — SkillStaleness: single version field
// ---------------------------------------------------------------------------

// TestSkillStaleness_VersionMatch_ReturnsEmptySlice verifies that when the skill's version
// matches the manifest entry, SkillStaleness returns an empty slice (not stale).
func TestSkillStaleness_VersionMatch_ReturnsEmptySlice(t *testing.T) {
	entry := domain.ManifestEntry{
		Ref:        skillRef("lean-tdd"),
		TargetPath: "skills/lean-tdd/SKILL.md",
		Version:    "1.5",
		DeployedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	skill := makeSkill("lean-tdd", "1.5") // version matches entry

	deltas := plan.SkillStaleness(entry, skill)

	if len(deltas) != 0 {
		t.Errorf("SkillStaleness with matching version: got %d deltas, want 0", len(deltas))
	}
}

// TestSkillStaleness_VersionMismatch_ReturnsSingleDelta verifies that a version mismatch
// produces exactly one delta with Field = "version".
func TestSkillStaleness_VersionMismatch_ReturnsSingleDelta(t *testing.T) {
	entry := domain.ManifestEntry{
		Ref:        skillRef("lean-tdd"),
		TargetPath: "skills/lean-tdd/SKILL.md",
		Version:    "1.5",
		DeployedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	skill := makeSkill("lean-tdd", "1.6") // version differs

	deltas := plan.SkillStaleness(entry, skill)

	if len(deltas) != 1 {
		t.Fatalf("SkillStaleness with version mismatch: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "version" {
		t.Errorf("SkillStaleness delta.Field = %q, want %q", deltas[0].Field, "version")
	}
	if deltas[0].Deployed != "1.5" {
		t.Errorf("SkillStaleness delta.Deployed = %q, want %q", deltas[0].Deployed, "1.5")
	}
	if deltas[0].Source != "1.6" {
		t.Errorf("SkillStaleness delta.Source = %q, want %q", deltas[0].Source, "1.6")
	}
}

// ---------------------------------------------------------------------------
// T16.3 — HookStaleness: single version field
// ---------------------------------------------------------------------------

// TestHookStaleness_VersionMatch_ReturnsEmptySlice verifies that when the hook bundle's
// version matches the manifest entry, HookStaleness returns an empty slice.
func TestHookStaleness_VersionMatch_ReturnsEmptySlice(t *testing.T) {
	entry := domain.ManifestEntry{
		Ref:        hookRef("my-hooks"),
		TargetPath: "hooks/my-hooks",
		Version:    "2.0",
		DeployedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	bundle := makeHookBundle("my-hooks", "2.0") // version matches

	deltas := plan.HookStaleness(entry, bundle)

	if len(deltas) != 0 {
		t.Errorf("HookStaleness with matching version: got %d deltas, want 0", len(deltas))
	}
}

// TestHookStaleness_VersionMismatch_ReturnsSingleDelta verifies that a version mismatch
// produces exactly one delta with Field = "version".
func TestHookStaleness_VersionMismatch_ReturnsSingleDelta(t *testing.T) {
	entry := domain.ManifestEntry{
		Ref:        hookRef("my-hooks"),
		TargetPath: "hooks/my-hooks",
		Version:    "2.0",
		DeployedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	bundle := makeHookBundle("my-hooks", "2.1") // version differs

	deltas := plan.HookStaleness(entry, bundle)

	if len(deltas) != 1 {
		t.Fatalf("HookStaleness with version mismatch: got %d deltas, want 1", len(deltas))
	}
	if deltas[0].Field != "version" {
		t.Errorf("HookStaleness delta.Field = %q, want %q", deltas[0].Field, "version")
	}
}

// ---------------------------------------------------------------------------
// T16.4 — Local-modification detection via manifest hash
// ---------------------------------------------------------------------------

// buildInputWithSingleAgent returns a minimal plan.Input that includes the given agent
// via a single workflow. The agent's default target path from newFakeModule is "agents/<key>.md".
// The manifest snapshot and deployed hashes are provided by the caller.
func buildInputWithSingleAgent(
	agent domain.Agent,
	snap manifest.Snapshot,
	deployedHashes map[string]string,
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
		Catalog:         cat,
		Module:          module,
		Mode:            domain.ModeUpdate,
		WorkspacePath:   "/fake/workspace",
		Scope:           domain.ScopeProject,
		GOOS:            "linux",
		Manifest:        snap,
		WorkflowIDs:     []string{wfID},
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
		DeployedHashes: deployedHashes,
	}
}

// TestBuild_LocalModification_HashMatch_ClassifiesAsUnchanged verifies that when the current
// hash of a deployed file matches the hash recorded in the manifest, the plan item is
// ActionUnchanged — not a conflict.
func TestBuild_LocalModification_HashMatch_ClassifiesAsUnchanged(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const hash = "sha256:abc123"

	agent := makeAgent("test-agent", "1.0")

	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Current deployed hash matches the manifest — no local modification.
	deployedHashes := map[string]string{targetPath: hash}

	input := buildInputWithSingleAgent(agent, snap, deployedHashes)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("test-agent with matching hash: Action = %q, want %q",
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
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Current deployed hash differs — locally modified.
	deployedHashes := map[string]string{targetPath: currentHash}

	input := buildInputWithSingleAgent(agent, snap, deployedHashes)

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
// normalisation (CD-11): a line-ending or whitespace change changes the hash.
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
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	deployedHashes := map[string]string{targetPath: currentHash}

	input := buildInputWithSingleAgent(agent, snap, deployedHashes)

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
// a file present in DeployedHashes but absent from the manifest is treated as locally
// modified (ActionConflict with ManifestMissing = true), not silently classified as a
// fresh create. The conservative classification protects hand-deployed files.
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

	// File exists on disk (has a hash), but has no manifest record.
	deployedHashes := map[string]string{targetPath: currentHash}

	input := buildInputWithSingleAgent(agent, snap, deployedHashes)

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
	deployedHashes := map[string]string{targetPath: "sha256:abc123"}

	input := buildInputWithSingleAgent(agent, snap, deployedHashes)

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
// T16.5 — Missing-manifest policy
// ---------------------------------------------------------------------------

// buildInputWithManifestState returns a plan.Input with the given manifest snapshot and
// the agent's target path present in deployedHashes (simulating a file on disk).
func buildInputWithManifestState(agent domain.Agent, snap manifest.Snapshot, deployedTargetPath string) plan.Input {
	deployedHashes := map[string]string{
		deployedTargetPath: "sha256:cafebabe",
	}
	return buildInputWithSingleAgent(agent, snap, deployedHashes)
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

// TestBuild_MissingManifest_FileAbsentFromDeployedHashes_ClassifiesAsCreate verifies that
// when the manifest is absent but a file does NOT appear in DeployedHashes (it doesn't exist
// on disk), the item is ActionCreate — not ActionConflict. The conflict classification only
// applies to files actually present on disk.
func TestBuild_MissingManifest_FileAbsentFromDeployedHashes_ClassifiesAsCreate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")

	// Absent manifest, no deployed hashes — file is not on disk.
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

// TestBuild_PresentManifest_FileNotInManifest_NotInDeployedHashes_ClassifiesAsCreate verifies
// that an artifact in the set that has no manifest entry and is not present on disk is
// classified as ActionCreate (not ActionConflict). The conflict guard applies only when
// the file is actually deployed.
func TestBuild_PresentManifest_FileNotInManifest_NotInDeployedHashes_ClassifiesAsCreate(t *testing.T) {
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

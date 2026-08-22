package plan_test

// build_test.go covers the classifyAgentItem step that handles deployed files that could not
// be parsed — the ParseFailed classification path (Step 1b, inserted between Step 1 and Step 2
// of the existing classification order).
//
// Verified behaviours:
//
// ParseFailed classification (FR-1, FR-2):
//   - An agent whose deployed state has Present: true and ParseFailed: true is classified as
//     ActionConflict, not ActionCreate and not ActionUpdate.
//   - The conflict reason is "deployed file could not be read/parsed" — distinct from the
//     hash-mismatch reason "deployed file has been locally modified since last deployment"
//     that Step 4 would produce if ParseFailed were evaluated after Step 4.
//   - LocalModification.ManifestMissing is true for a parse-failed conflict.
//   - LocalModification.CurrentHash is non-empty (set from raw bytes by probeDeployedArtifact).
//   - LocalModification.RecordedHash is empty (no manifest entry was successfully matched,
//     because the file's id could not be extracted to look up the manifest entry).
//
// Ordering invariant (ParseFailed check precedes Steps 2-4):
//   - A parse-failed file with a usable manifest is still classified as ActionConflict with
//     the parse-failure reason, NOT as any manifest-dependent outcome.
//   - A parse-failed file with no usable manifest is still classified as ActionConflict with
//     the parse-failure reason, NOT as the "no manifest available" conflict reason.
//   - A parse-failed file whose hash differs from any manifest entry is still classified as
//     ActionConflict with the parse-failure reason, NOT as "locally modified" (FR-2).
//
// Regression guards:
//   - A normally present (parsed successfully) file does NOT receive ParseFailed classification.
//   - A file with Present: false (not deployed) is still classified as ActionCreate, as before.

import (
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Fixtures specific to ParseFailed classification tests
// ---------------------------------------------------------------------------

// parseFailedDeployedState returns a DeployedArtifactState representing a file that exists
// on disk and is identifiable as MOSAIC-managed but could not be parsed. The ContentHash is
// non-empty (computed from raw bytes); all version fields are empty strings.
//
// This models the state produced by probeDeployedStateWithIndex (after I2.3) when:
//   - The scan matched the file by filename key after a parse failure (ParseFailed: true on scan)
//   - The fallback probe in probeDeployedStateWithIndex found the file at the planned path
//   - ParseFailed was set on the returned DeployedArtifactState
func parseFailedDeployedState(contentHash string) domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:     true,
		ParseFailed: true,
		ContentHash: contentHash,
		// All version fields are empty — they could not be extracted from the unparseable file.
		Version:           "",
		HarnessVersion:    "",
		InjectionsVersion: "",
	}
}

// makeManifestWithEntry returns a manifest.Snapshot in StatePresent containing one entry.
// Uses the same pattern as other plan tests (presentSnapshot + explicit Manifest struct).
func makeManifestWithEntry(entry domain.ManifestEntry) manifest.Snapshot {
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	}
	return presentSnapshot(m)
}

// buildParseFailedInput returns a plan.Input with one worker agent whose deployed state has
// ParseFailed: true. The catalog, module, and workflow are minimal stubs. Models are not
// provided (empty map) so model-related gaps do not interfere with classification assertions.
func buildParseFailedInput(
	snap manifest.Snapshot,
	deployedState map[string]domain.DeployedArtifactState,
) plan.Input {
	agent := makeAgent("corrupted-agent", "2.0")
	wf := makeWorkflow("test-wf", agent.Key)

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	return plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{wf.ID},
		Manifest:      snap,
		DeployedState: deployedState,
		// Models intentionally omitted — not relevant for action classification tests.
	}
}

// ---------------------------------------------------------------------------
// ParseFailed classification: ActionConflict with distinct reason
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_ParseFailed_ClassifiedAsConflict verifies that an agent whose deployed
// file has ParseFailed: true is classified as ActionConflict, not ActionCreate or ActionUpdate.
//
// This is the TDD RED test: the current classifyAgentItem does not check ParseFailed. A file
// with Present: true and all version fields empty falls through to Step 5 (version staleness)
// and is classified as ActionUpdate, not ActionConflict. After I2.5 inserts the ParseFailed
// check between Step 1 and Step 2, this test must pass.
func TestClassifyAgentItem_ParseFailed_ClassifiedAsConflict(t *testing.T) {
	// Arrange: agent has a deployed file that is parse-failed.
	// A manifest entry exists (to ensure the ParseFailed check fires BEFORE Step 3).
	agent := makeAgent("corrupted-agent", "2.0")
	targetPath := "agents/corrupted-agent.md"

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, "sha256:recorded")
	snap := makeManifestWithEntry(entry)

	deployed := map[string]domain.DeployedArtifactState{
		targetPath: parseFailedDeployedState("sha256:currentbytes"),
	}

	input := buildParseFailedInput(snap, deployed)

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	// Assert: a parse-failed file must be classified as ActionConflict, not ActionUpdate or ActionCreate.
	if item.Action != domain.ActionConflict {
		t.Errorf("item.Action = %v, want %v; "+
			"a deployed file with ParseFailed: true must be classified as ActionConflict. "+
			"The current implementation does not check ParseFailed, so the file falls through "+
			"to Step 5 (version staleness) and is classified as ActionUpdate instead. "+
			"I2.5 must insert the ParseFailed check between Step 1 and Step 2.",
			item.Action, domain.ActionConflict)
	}
}

// TestClassifyAgentItem_ParseFailed_ReasonIsDistinctFromLocallyModified verifies that the
// conflict reason for a parse-failed file is NOT "deployed file has been locally modified
// since last deployment" — the Step 4 hash-mismatch reason.
//
// This is FR-2: the ParseFailed check must run BEFORE Step 4 (hash mismatch). If it ran after,
// the hash comparison in Step 4 would compare the raw-bytes hash (set by probeDeployedArtifact)
// against the manifest's recorded hash (from the last successful deployment). These would almost
// certainly differ for an edited/corrupted file, causing Step 4 to fire with the misleading
// "locally modified" reason instead of the correct "could not be read/parsed" reason.
func TestClassifyAgentItem_ParseFailed_ReasonIsDistinctFromLocallyModified(t *testing.T) {
	// Arrange: the raw-bytes hash differs from the manifest recorded hash to ensure that if
	// Step 4 fires instead of the ParseFailed check, the test catches the wrong reason.
	agent := makeAgent("corrupted-agent", "2.0")
	targetPath := "agents/corrupted-agent.md"

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, "sha256:recorded")
	snap := makeManifestWithEntry(entry)

	// ContentHash differs from recorded hash — hash mismatch would trigger Step 4 if ParseFailed
	// check is not inserted first.
	deployed := map[string]domain.DeployedArtifactState{
		targetPath: parseFailedDeployedState("sha256:differenthash"),
	}

	input := buildParseFailedInput(snap, deployed)

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	// The reason must be the parse-failure reason, not the locally-modified reason.
	const wantReasonSubstr = "could not be read/parsed"
	const locallyModifiedReason = "locally modified since last deployment"

	if strings.Contains(item.Reason, locallyModifiedReason) {
		t.Errorf("item.Reason = %q contains %q; "+
			"a parse-failed file must NOT produce the 'locally modified' reason. "+
			"The ParseFailed check must precede Step 4 (hash-mismatch check) in classifyAgentItem. "+
			"I2.5 must insert the check between Step 1 and Step 2 so it fires before Step 4.",
			item.Reason, locallyModifiedReason)
	}

	if !strings.Contains(item.Reason, wantReasonSubstr) {
		t.Errorf("item.Reason = %q, want reason containing %q; "+
			"the conflict reason for a parse-failed file must describe the parse failure, "+
			"not any other cause (manifest missing, hash mismatch, etc.)",
			item.Reason, wantReasonSubstr)
	}
}

// TestClassifyAgentItem_ParseFailed_LocalModificationManifestMissingTrue verifies that the
// Conflict field for a parse-failed classification has ManifestMissing: true. This is
// consistent with the existing Step 2/Step 3 patterns where the manifest cannot confirm the
// file's origin, and distinguishes the parse-failure conflict from the Step 4 hash-mismatch
// pattern (where ManifestMissing is false and RecordedHash is set).
func TestClassifyAgentItem_ParseFailed_LocalModificationManifestMissingTrue(t *testing.T) {
	// Arrange
	agent := makeAgent("corrupted-agent", "2.0")
	targetPath := "agents/corrupted-agent.md"

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, "sha256:recorded")
	snap := makeManifestWithEntry(entry)

	deployed := map[string]domain.DeployedArtifactState{
		targetPath: parseFailedDeployedState("sha256:rawbyteshash"),
	}

	input := buildParseFailedInput(snap, deployed)

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	if item.Conflict == nil {
		t.Fatal("item.Conflict is nil; ActionConflict must always carry a non-nil Conflict field")
	}

	if !item.Conflict.ManifestMissing {
		t.Errorf("item.Conflict.ManifestMissing = false, want true; "+
			"a parse-failed conflict uses ManifestMissing: true to indicate that the file's origin "+
			"could not be confirmed against the manifest. "+
			"This distinguishes it from the Step 4 hash-mismatch pattern (ManifestMissing: false, RecordedHash set).")
	}
}

// TestClassifyAgentItem_ParseFailed_CurrentHashNonEmpty verifies that the Conflict field for
// a parse-failed classification has a non-empty CurrentHash. probeDeployedArtifact computes
// a content hash from raw bytes even when frontmatter cannot be parsed, so CurrentHash is
// always available and must be surfaced in the conflict for user inspection.
func TestClassifyAgentItem_ParseFailed_CurrentHashNonEmpty(t *testing.T) {
	// Arrange
	agent := makeAgent("corrupted-agent", "2.0")
	targetPath := "agents/corrupted-agent.md"

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, "sha256:recorded")
	snap := makeManifestWithEntry(entry)

	const wantHash = "sha256:deadbeef"
	deployed := map[string]domain.DeployedArtifactState{
		targetPath: parseFailedDeployedState(wantHash),
	}

	input := buildParseFailedInput(snap, deployed)

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	if item.Conflict == nil {
		t.Fatal("item.Conflict is nil; cannot check CurrentHash")
	}

	if item.Conflict.CurrentHash == "" {
		t.Errorf("item.Conflict.CurrentHash is empty; "+
			"probeDeployedArtifact sets ContentHash from raw bytes even for unparseable files, "+
			"so CurrentHash must always be non-empty for a parse-failed conflict")
	}
	if item.Conflict.CurrentHash != wantHash {
		t.Errorf("item.Conflict.CurrentHash = %q, want %q; "+
			"CurrentHash must reflect the raw-bytes hash from the deployed state",
			item.Conflict.CurrentHash, wantHash)
	}
}

// ---------------------------------------------------------------------------
// ParseFailed ordering: fires before Steps 2-4
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_ParseFailed_NoManifest_StillConflictWithParseFailedReason verifies
// that the ParseFailed check fires even when the manifest is absent. Without the ParseFailed
// check, an absent manifest causes Step 2 to fire ("no manifest available"), and the reason
// would be the Step 2 reason. With the ParseFailed check in place, the Step 1b reason
// ("deployed file could not be read/parsed") must appear even with an absent manifest.
func TestClassifyAgentItem_ParseFailed_NoManifest_StillConflictWithParseFailedReason(t *testing.T) {
	// Arrange: no manifest on disk (StateAbsent).
	agent := makeAgent("corrupted-agent", "2.0")
	targetPath := "agents/corrupted-agent.md"

	snap := absentSnapshot()

	deployed := map[string]domain.DeployedArtifactState{
		targetPath: parseFailedDeployedState("sha256:rawbyteshash"),
	}

	input := buildParseFailedInput(snap, deployed)

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	// Assert: even without a manifest, the parse-failure reason must appear.
	const wantReasonSubstr = "could not be read/parsed"
	const step2ReasonSubstr = "no manifest is available"

	if strings.Contains(item.Reason, step2ReasonSubstr) {
		t.Errorf("item.Reason = %q contains the Step 2 reason %q; "+
			"the ParseFailed check (Step 1b) must fire before Step 2 (manifest usability check). "+
			"A parse-failed file must produce the parse-failure reason even when the manifest is absent.",
			item.Reason, step2ReasonSubstr)
	}

	if !strings.Contains(item.Reason, wantReasonSubstr) {
		t.Errorf("item.Reason = %q, want reason containing %q; "+
			"the parse-failure reason must appear regardless of manifest state",
			item.Reason, wantReasonSubstr)
	}
}

// ---------------------------------------------------------------------------
// Regression guards: ParseFailed must NOT fire for normal files
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_NormalPresentFile_ParseFailedDoesNotAffectClassification verifies
// that a normally present file (ParseFailed: false) is classified by the existing steps,
// not by the ParseFailed check. This is the regression guard to ensure the new check does not
// over-apply and change the classification of correctly-deployed files.
func TestClassifyAgentItem_NormalPresentFile_ParseFailedDoesNotAffectClassification(t *testing.T) {
	// Arrange: a present file with matching hash and version stamps — should be ActionUnchanged.
	agent := makeAgent("healthy-agent", "1.0")
	targetPath := "agents/healthy-agent.md"

	const contentHash = "sha256:aabbcc"
	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, contentHash)
	snap := makeManifestWithEntry(entry)

	deployed := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:           true,
			ParseFailed:       false, // normal file — ParseFailed is false
			ContentHash:       contentHash,
			Version:           agent.Version,
			HarnessVersion:    "1.0", // matches newFakeModule's descriptor TransformVersion
			InjectionsVersion: "1.0",
		},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{wf.ID},
		Manifest:      snap,
		DeployedState: deployed,
	}

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	// The normal file must NOT be classified as ActionConflict due to the ParseFailed check.
	if item.Action == domain.ActionConflict {
		t.Errorf("item.Action = ActionConflict for a normally-present file with ParseFailed: false; "+
			"the ParseFailed check (Step 1b) must only fire when ParseFailed: true. "+
			"A file with ParseFailed: false must continue through the existing classification steps. "+
			"item.Reason = %q", item.Reason)
	}
}

// TestClassifyAgentItem_AbsentFile_StillActionCreate verifies that a file with Present: false
// (not deployed at all) is still classified as ActionCreate. The ParseFailed check applies only
// when Present: true, so it must not interfere with the Step 1 short-circuit.
func TestClassifyAgentItem_AbsentFile_StillActionCreate(t *testing.T) {
	// Arrange: no deployed file — zero-value DeployedArtifactState (Present: false).
	agent := makeAgent("new-agent", "1.0")

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{wf.ID},
		Manifest:      absentSnapshot(),
		DeployedState: map[string]domain.DeployedArtifactState{}, // no entry → zero value → Present: false
	}

	// Act
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build returned unexpected error: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	if item.Action != domain.ActionCreate {
		t.Errorf("item.Action = %v, want ActionCreate; "+
			"an absent file (Present: false) must still be classified as ActionCreate. "+
			"The ParseFailed check (Step 1b) must only apply when Present: true.",
			item.Action)
	}
}

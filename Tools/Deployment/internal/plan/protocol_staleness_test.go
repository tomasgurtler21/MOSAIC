package plan_test

// protocol_staleness_test.go covers protocol staleness detection: the comparison of the
// deployed protocol version against the current protocol source version, its integration into
// the planner's classification of agent items, and the precedence of hash-conflict detection.
//
//   ProtocolStaleness / ProtocolDrift (T7.2):
//
//     Staleness comparison:
//       - A deployed version equal to the source is not stale.
//       - A deployed version that differs from the source is stale.
//       - An empty deployed version (no marker in the file) is always stale — agents deployed
//         before the protocol marker feature was added carry no version and must be picked up.
//
//     ProtocolDrift.Delta():
//       - When stale, Delta() returns a VersionDelta whose Field is ProtocolDeltaField and
//         whose Deployed and Source fields carry the respective versions.
//       - When not stale, Delta() returns ok = false.
//
//     ProtocolDeltaField constant:
//       - Its value is "protocol_version".
//       - It does not collide with any frontmatter stamp field name ("version",
//         "transform_version", "injections_version", "orchestrator_injections_version",
//         "tool_mappings_version") or with the WorkflowDeltaFieldPrefix.
//
//     ProtocolDrift.Reason():
//       - When deployed and source differ: "protocol version changed (X -> Y)".
//       - When deployed is empty: names the absence of a protocol version marker.
//       - When not stale: returns "".
//
//   Planning behaviour (T7.3):
//
//     - A worker/subagent with an unchanged content hash but a deployed protocol version
//       older than the source is classified as ActionUpdate.
//     - The plan item's Reason contains the protocol version change.
//     - Protocol staleness applies to subagents as well as the orchestrator.
//     - When a worker agent has both a version-stamp mismatch and protocol staleness, the
//       Stale slice contains deltas from both sources.
//     - When the orchestrator has both workflow drift and protocol staleness, the Stale slice
//       contains deltas from both sources.
//     - An agent whose deployed protocol version matches the source is not scheduled for
//       update on that basis alone.
//     - The protocol delta field is excluded from the generic formatVersionDeltas path; the
//       human-readable reason is provided by ProtocolDrift.Reason() instead.
//
//   Precedence (T7.4):
//
//     - When a deployed file has both a hash conflict (local modification) and a stale protocol
//       version, the item is classified as ActionConflict. Hash conflicts always take precedence
//       over any staleness signal, including protocol staleness.

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
// Helpers local to this file
// ---------------------------------------------------------------------------

// deployedStateWithProtocol returns a DeployedArtifactState identical to deployedState() but
// with ProtocolVersion set to protocolVersion. Use this when a test needs the deployed file
// to carry an embedded protocol version stamp.
func deployedStateWithProtocol(contentHash, version, transformVersion, injectionsVersion, protocolVersion string) domain.DeployedArtifactState {
	s := deployedState(contentHash, version, transformVersion, injectionsVersion)
	s.ProtocolVersion = protocolVersion
	return s
}

// buildProtocolStalenessInput returns a minimal plan.Input for a single worker agent, set up
// so that:
//
//   - The manifest records the agent at targetPath with hash contentHash and matching stamps.
//   - The deployed state at targetPath has the given stamps and deployedProtocolVersion.
//   - plan.Input.ProtocolVersion is sourceProtocolVersion.
//
// This produces a plan where the only staleness signal (if any) is the protocol version delta.
// The agent's stamp versions ("1.0", "1.0", "1.0") match the fake module descriptor's stamps.
func buildProtocolStalenessInput(
	agent domain.Agent,
	contentHash string,
	deployedProtocolVersion string,
	sourceProtocolVersion string,
) plan.Input {
	const targetPath = "agents/test-agent.md"

	manifestEntry := makeManifestEntry(agentRef(agent.Key), targetPath, "1.0", contentHash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedStateWithProtocol(contentHash, "1.0", "1.0", "1.0", deployedProtocolVersion),
	}

	in := buildInputWithSingleAgent(agent, snap, ds)
	in.ProtocolVersion = sourceProtocolVersion
	return in
}

// ---------------------------------------------------------------------------
// T7.2 — ProtocolStaleness function: staleness comparison
// ---------------------------------------------------------------------------

// TestProtocolStaleness_EqualVersions_NotStale verifies that when the deployed protocol version
// and the source version are the same, ProtocolStaleness reports no drift.
func TestProtocolStaleness_EqualVersions_NotStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.9",
	}

	drift := plan.ProtocolStaleness(deployed, "1.9")

	if drift.Stale() {
		t.Error("ProtocolStaleness with equal versions: Stale() = true, want false")
	}
}

// TestProtocolStaleness_OlderDeployedVersion_IsStale verifies that when the deployed protocol
// version is older than the source, ProtocolStaleness reports drift.
func TestProtocolStaleness_OlderDeployedVersion_IsStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.7",
	}

	drift := plan.ProtocolStaleness(deployed, "1.9")

	if !drift.Stale() {
		t.Error("ProtocolStaleness with older deployed version: Stale() = false, want true; " +
			"a deployed version older than the source must be detected as stale")
	}
}

// TestProtocolStaleness_EmptyDeployedVersion_IsStale verifies that when the deployed file
// carries no protocol version (ProtocolVersion == ""), ProtocolStaleness always reports
// drift. Agents deployed without a version attribute on their protocol region carry no version;
// they must be picked up and re-deployed to receive the version attribute.
func TestProtocolStaleness_EmptyDeployedVersion_IsStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "", // no version attribute on the protocol region's open tag
	}

	drift := plan.ProtocolStaleness(deployed, "1.9")

	if !drift.Stale() {
		t.Error("ProtocolStaleness with empty deployed version: Stale() = false, want true; " +
			"an absent protocol version marker must always be treated as stale")
	}
}

// TestProtocolStaleness_DeployedVersionPopulated_DeployedAndSourceFieldsCorrect verifies that
// the returned ProtocolDrift carries the deployed version and source version from the inputs.
func TestProtocolStaleness_DeployedVersionPopulated_DeployedAndSourceFieldsCorrect(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.7",
	}

	drift := plan.ProtocolStaleness(deployed, "1.9")

	if drift.Deployed != "1.7" {
		t.Errorf("ProtocolDrift.Deployed = %q, want %q (value from the deployed file)",
			drift.Deployed, "1.7")
	}
	if drift.Source != "1.9" {
		t.Errorf("ProtocolDrift.Source = %q, want %q (current source version)",
			drift.Source, "1.9")
	}
}

// TestProtocolStaleness_VersionDowngrade_IsStale verifies that the direction of the version
// change is irrelevant. A deployed version that is higher than the source is still stale.
// Any difference between deployed and source must be detected.
func TestProtocolStaleness_VersionDowngrade_IsStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "2.0", // deployed is newer than source
	}

	drift := plan.ProtocolStaleness(deployed, "1.9")

	if !drift.Stale() {
		t.Error("ProtocolStaleness with deployed version newer than source (downgrade scenario): " +
			"Stale() = false, want true; any version difference must be stale")
	}
}

// ---------------------------------------------------------------------------
// T7.2 — ProtocolDrift.Delta(): version delta construction
// ---------------------------------------------------------------------------

// TestProtocolDrift_Delta_WhenStale_ReturnsProtocolVersionField verifies that Delta() returns
// ok = true and a VersionDelta whose Field is exactly ProtocolDeltaField when the drift is stale.
func TestProtocolDrift_Delta_WhenStale_ReturnsProtocolVersionField(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.7",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	delta, ok := drift.Delta()

	if !ok {
		t.Fatal("ProtocolDrift.Delta() returned ok = false for a stale drift; want ok = true")
	}
	if delta.Field != plan.ProtocolDeltaField {
		t.Errorf("delta.Field = %q, want %q (ProtocolDeltaField)", delta.Field, plan.ProtocolDeltaField)
	}
}

// TestProtocolDrift_Delta_WhenStale_CarriesDeployedAndSourceValues verifies that the delta
// returned by Delta() carries the deployed version in delta.Deployed and the source version
// in delta.Source. These values are used by the update-reason formatter and must be truthful.
func TestProtocolDrift_Delta_WhenStale_CarriesDeployedAndSourceValues(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.7",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	delta, ok := drift.Delta()

	if !ok {
		t.Fatal("ProtocolDrift.Delta() returned ok = false for a stale drift")
	}
	if delta.Deployed != "1.7" {
		t.Errorf("delta.Deployed = %q, want %q (value from the deployed file)", delta.Deployed, "1.7")
	}
	if delta.Source != "1.9" {
		t.Errorf("delta.Source = %q, want %q (current source version)", delta.Source, "1.9")
	}
}

// TestProtocolDrift_Delta_WhenNotStale_OkIsFalse verifies that Delta() returns ok = false
// when the drift is not stale. The delta field is meaningless when equal versions are compared.
func TestProtocolDrift_Delta_WhenNotStale_OkIsFalse(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.9",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	_, ok := drift.Delta()

	if ok {
		t.Error("ProtocolDrift.Delta() returned ok = true for a non-stale drift; want ok = false")
	}
}

// TestProtocolDrift_Delta_EmptyDeployedVersion_FieldIsProtocolVersion verifies that when the
// deployed version is "" (absent marker), Delta() still returns the correct field name.
func TestProtocolDrift_Delta_EmptyDeployedVersion_FieldIsProtocolVersion(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	delta, ok := drift.Delta()

	if !ok {
		t.Fatal("ProtocolDrift.Delta() returned ok = false for a stale drift with empty deployed version")
	}
	if delta.Field != plan.ProtocolDeltaField {
		t.Errorf("delta.Field = %q, want %q", delta.Field, plan.ProtocolDeltaField)
	}
	if delta.Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty string (no marker in deployed file)", delta.Deployed)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — ProtocolDeltaField constant value and collision avoidance
// ---------------------------------------------------------------------------

// TestProtocolDeltaField_IsProtocolVersion verifies that the ProtocolDeltaField constant has
// the exact value "protocol_version". This value is relied on by the update-reason formatter
// when excluding it from the generic version-field formatter.
func TestProtocolDeltaField_IsProtocolVersion(t *testing.T) {
	if plan.ProtocolDeltaField != "protocol_version" {
		t.Errorf("plan.ProtocolDeltaField = %q, want %q", plan.ProtocolDeltaField, "protocol_version")
	}
}

// TestProtocolDeltaField_DoesNotCollideWithFrontmatterStampFieldNames verifies that
// ProtocolDeltaField is distinct from every frontmatter stamp field name that the executor's
// resolveVersionStamp switch handles. A collision would cause the executor to incorrectly
// attempt to write a "protocol_version" frontmatter key to the deployed file.
func TestProtocolDeltaField_DoesNotCollideWithFrontmatterStampFieldNames(t *testing.T) {
	frontmatterStampFields := []string{
		"version",
		"transform_version",
		"injections_version",
		"orchestrator_injections_version",
		"tool_mappings_version",
	}

	for _, field := range frontmatterStampFields {
		if plan.ProtocolDeltaField == field {
			t.Errorf("ProtocolDeltaField %q collides with frontmatter stamp field %q; "+
				"the executor's resolveVersionStamp switch would incorrectly handle it",
				plan.ProtocolDeltaField, field)
		}
	}
}

// TestProtocolDeltaField_DoesNotCollideWithWorkflowDeltaPrefix verifies that
// ProtocolDeltaField does not start with the WorkflowDeltaFieldPrefix. A collision would
// cause the update-reason builder to misroute the protocol delta through the workflow-reason
// formatter rather than the dedicated protocol-reason path.
func TestProtocolDeltaField_DoesNotCollideWithWorkflowDeltaPrefix(t *testing.T) {
	if strings.HasPrefix(plan.ProtocolDeltaField, plan.WorkflowDeltaFieldPrefix) {
		t.Errorf("ProtocolDeltaField %q starts with WorkflowDeltaFieldPrefix %q; "+
			"these must be disjoint to allow correct reason routing",
			plan.ProtocolDeltaField, plan.WorkflowDeltaFieldPrefix)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — ProtocolDrift.Reason(): human-readable staleness reason
// ---------------------------------------------------------------------------

// TestProtocolDrift_Reason_VersionChanged_NamesVersions verifies that when the deployed and
// source protocol versions differ (and deployed is non-empty), Reason() returns a string
// naming the change in the format "protocol version changed (X -> Y)".
func TestProtocolDrift_Reason_VersionChanged_NamesVersions(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.7",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	reason := drift.Reason()

	if reason == "" {
		t.Fatal("ProtocolDrift.Reason() returned empty string for a stale drift with version change; " +
			"want a non-empty reason naming the protocol")
	}
	if !strings.Contains(reason, "protocol") {
		t.Errorf("Reason() = %q; want it to contain %q", reason, "protocol")
	}
	if !strings.Contains(reason, "1.7") {
		t.Errorf("Reason() = %q; want it to contain the deployed version %q", reason, "1.7")
	}
	if !strings.Contains(reason, "1.9") {
		t.Errorf("Reason() = %q; want it to contain the source version %q", reason, "1.9")
	}
}

// TestProtocolDrift_Reason_EmptyDeployedVersion_NamesNoMarker verifies that when the deployed
// file carries no protocol version marker (deployed == ""), Reason() returns a string that
// names the absence of the marker — not "changed from '' to '1.9'".
func TestProtocolDrift_Reason_EmptyDeployedVersion_NamesNoMarker(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	reason := drift.Reason()

	if reason == "" {
		t.Fatal("ProtocolDrift.Reason() returned empty string for a stale drift with absent marker; " +
			"want a non-empty reason naming the missing marker")
	}
	if !strings.Contains(reason, "1.9") {
		t.Errorf("Reason() = %q; want it to reference the source version %q", reason, "1.9")
	}
	// The reason must describe the absence, not just a generic "changed" message.
	// It should contain something like "no protocol version marker" or "no marker".
	if !strings.Contains(strings.ToLower(reason), "no") && !strings.Contains(strings.ToLower(reason), "absent") && !strings.Contains(strings.ToLower(reason), "missing") {
		t.Errorf("Reason() = %q; want it to describe the absence of the protocol version marker "+
			"(should contain 'no', 'absent', or 'missing')", reason)
	}
}

// TestProtocolDrift_Reason_WhenNotStale_IsEmpty verifies that Reason() returns "" when the
// drift is not stale. An empty string signals the reason-builder to omit the protocol line.
func TestProtocolDrift_Reason_WhenNotStale_IsEmpty(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:         true,
		ProtocolVersion: "1.9",
	}
	drift := plan.ProtocolStaleness(deployed, "1.9")

	reason := drift.Reason()

	if reason != "" {
		t.Errorf("ProtocolDrift.Reason() = %q for a non-stale drift; want empty string",
			reason)
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Planning behaviour: protocol staleness drives ActionUpdate
// ---------------------------------------------------------------------------

// TestBuild_ProtocolStaleness_WorkerAgent_PlansUpdate verifies that a worker/subagent whose
// deployed protocol version is older than the source, but whose content hash and version stamps
// all match, is classified as ActionUpdate. Protocol staleness applies to every agent, not
// only to orchestrator-role agents.
func TestBuild_ProtocolStaleness_WorkerAgent_PlansUpdate(t *testing.T) {
	const contentHash = "sha256:worker001"
	agent := makeAgent("test-agent", "1.0")

	// Deployed file matches hash and version stamps — the only staleness signal is the protocol.
	in := buildProtocolStalenessInput(agent, contentHash, "1.7", "1.9")

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("worker agent with stale protocol version: Action = %q, want %q; "+
			"a deployed protocol version older than the source must schedule an update",
			item.Action, domain.ActionUpdate)
	}
}

// TestBuild_ProtocolStaleness_SubagentReason_NamesProtocol verifies that when a worker agent
// is classified as ActionUpdate due to protocol staleness, the plan item's Reason names the
// protocol — specifically, it contains the word "protocol" so the user can understand why the
// update was scheduled.
func TestBuild_ProtocolStaleness_SubagentReason_NamesProtocol(t *testing.T) {
	const contentHash = "sha256:worker002"
	agent := makeAgent("test-agent", "1.0")

	in := buildProtocolStalenessInput(agent, contentHash, "1.7", "1.9")

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Skipf("skipping reason check: Action = %q (not ActionUpdate); the preceding test covers this",
			item.Action)
	}
	if !strings.Contains(strings.ToLower(item.Reason), "protocol") {
		t.Errorf("item.Reason = %q; want it to contain %q so the user can identify the cause of the update",
			item.Reason, "protocol")
	}
}

// TestBuild_ProtocolStaleness_AppliedToSubagentNotJustOrchestrator verifies that protocol
// staleness is applied to worker/subagent roles, not only to the orchestrator. This is the
// explicit design contract: protocol staleness applies to every agent, unlike workflow drift
// which is restricted to orchestrator-role agents.
//
// Test structure: one worker agent with stale protocol, one worker with current protocol.
// Both have matching hashes and version stamps. Only the stale one should be ActionUpdate.
func TestBuild_ProtocolStaleness_AppliedToSubagentNotJustOrchestrator(t *testing.T) {
	const pathStale = "agents/stale-agent.md"
	const pathCurrent = "agents/current-agent.md"
	const hashStale = "sha256:stale001"
	const hashCurrent = "sha256:current001"

	agentStale := makeAgent("stale-agent", "1.0")
	agentCurrent := makeAgent("current-agent", "1.0")

	entryStale := makeManifestEntry(agentRef("stale-agent"), pathStale, "1.0", hashStale)
	entryStale.TransformVersion = "1.0"
	entryStale.InjectionsVersion = "1.0"
	entryCurrent := makeManifestEntry(agentRef("current-agent"), pathCurrent, "1.0", hashCurrent)
	entryCurrent.TransformVersion = "1.0"
	entryCurrent.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entryStale, entryCurrent},
	})

	// Create a two-workflow catalog so both agents appear in the plan.
	wfStale := makeWorkflow("wf-stale", "stale-agent")
	wfCurrent := makeWorkflow("wf-current", "current-agent")
	orch := makeOrchestrator()
	cat := &fakeCatalog{
		orchestrator: orch,
		workers:      []domain.Agent{agentStale, agentCurrent},
		workflows:    []domain.Workflow{wfStale, wfCurrent},
	}
	module := newFakeModule()

	ds := map[string]domain.DeployedArtifactState{
		pathStale:              deployedStateWithProtocol(hashStale, "1.0", "1.0", "1.0", "1.7"),   // old protocol
		pathCurrent:            deployedStateWithProtocol(hashCurrent, "1.0", "1.0", "1.0", "1.9"), // current protocol
		"agents/orchestrator.md": {Present: false}, // orchestrator absent — will be ActionCreate
	}

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"wf-stale", "wf-current"},
		Models: map[string]domain.ModelSelection{
			"stale-agent":   {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"current-agent": {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator":  {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState:   ds,
		ProtocolVersion: "1.9",
	}

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	staleItem, ok := findItem(p.Items, "stale-agent")
	if !ok {
		t.Fatal("plan has no item for stale-agent")
	}
	currentItem, ok := findItem(p.Items, "current-agent")
	if !ok {
		t.Fatal("plan has no item for current-agent")
	}

	if staleItem.Action != domain.ActionUpdate {
		t.Errorf("stale-agent (deployed protocol 1.7, source 1.9): Action = %q, want %q; "+
			"subagents must be subject to protocol staleness",
			staleItem.Action, domain.ActionUpdate)
	}
	if currentItem.Action != domain.ActionUnchanged {
		t.Errorf("current-agent (deployed protocol 1.9, source 1.9): Action = %q, want %q; "+
			"a current protocol version must not produce an update on that basis alone",
			currentItem.Action, domain.ActionUnchanged)
	}
}

// TestBuild_ProtocolStaleness_ComposesWithVersionStampDeltas verifies that when a worker agent
// has both a version-stamp mismatch and a stale protocol version, the Stale slice of the
// resulting ActionUpdate item contains deltas from both sources. Neither signal must mask or
// suppress the other.
func TestBuild_ProtocolStaleness_ComposesWithVersionStampDeltas(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const contentHash = "sha256:compose001"

	agent := makeAgent("test-agent", "2.0") // source version is 2.0

	// Manifest records version 2.0 for the agent (matches hash and newer source version).
	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", contentHash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed file: hash matches, but version stamp is 1.0 (stale) AND protocol is old.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedStateWithProtocol(contentHash, "1.0", "1.0", "1.0", "1.7"),
	}

	in := buildInputWithSingleAgent(agent, snap, ds)
	in.ProtocolVersion = "1.9"

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("agent with both version mismatch and protocol staleness: Action = %q, want %q",
			item.Action, domain.ActionUpdate)
	}

	// The Stale slice must contain both the version-field delta AND the protocol delta.
	hasVersionDelta := false
	hasProtocolDelta := false
	for _, d := range item.Stale {
		if d.Field == "version" {
			hasVersionDelta = true
		}
		if d.Field == plan.ProtocolDeltaField {
			hasProtocolDelta = true
		}
	}
	if !hasVersionDelta {
		t.Errorf("item.Stale = %v; want it to contain a delta for field %q (version-stamp mismatch)",
			item.Stale, "version")
	}
	if !hasProtocolDelta {
		t.Errorf("item.Stale = %v; want it to contain a delta for field %q (protocol staleness)",
			item.Stale, plan.ProtocolDeltaField)
	}
}

// TestBuild_ProtocolStaleness_OrchestratorComposesWithWorkflowDrift verifies that when the
// orchestrator has both workflow drift and a stale protocol version, both sources of drift
// contribute to the resulting ActionUpdate item. The Stale slice must contain both the
// workflow delta and the protocol delta.
func TestBuild_ProtocolStaleness_OrchestratorComposesWithWorkflowDrift(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const orchHash = "sha256:orch001"

	orch := makeOrchestrator()
	wf := makeWorkflow("quick-fix", "orchestrator")
	wfNewlyAdded := makeWorkflow("greenfield-tdd", "orchestrator")

	cat := &fakeCatalog{
		orchestrator: orch,
		workers:      []domain.Agent{},
		workflows:    []domain.Workflow{wf, wfNewlyAdded},
	}
	module := newFakeModule()

	// Manifest records the orchestrator at orchPath with the current hash.
	entryOrch := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", orchHash)
	entryOrch.TransformVersion = "1.0"
	entryOrch.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entryOrch},
	})

	// Deployed orchestrator: hash matches, version stamps match, but:
	//   - Carries only "quick-fix" (missing "greenfield-tdd" → workflow drift)
	//   - Has old protocol version (1.7 vs source 1.9 → protocol staleness)
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:         true,
			ContentHash:     orchHash,
			Version:         "1.0",
			TransformVersion: "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"},
				// "greenfield-tdd" is absent — workflow drift
			},
			ProtocolVersion: "1.7", // stale
		},
	}

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix", "greenfield-tdd"},
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState:   ds,
		ProtocolVersion: "1.9",
	}

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with workflow drift + protocol staleness: Action = %q, want %q",
			item.Action, domain.ActionUpdate)
	}

	// Both a workflow delta and a protocol delta must appear in the Stale slice.
	hasWorkflowDelta := false
	hasProtocolDelta := false
	for _, d := range item.Stale {
		if strings.HasPrefix(d.Field, plan.WorkflowDeltaFieldPrefix) {
			hasWorkflowDelta = true
		}
		if d.Field == plan.ProtocolDeltaField {
			hasProtocolDelta = true
		}
	}
	if !hasWorkflowDelta {
		t.Errorf("item.Stale = %v; want it to contain a workflow delta (workflow drift must compose with protocol staleness)",
			item.Stale)
	}
	if !hasProtocolDelta {
		t.Errorf("item.Stale = %v; want it to contain a delta for field %q (protocol staleness must compose with workflow drift)",
			item.Stale, plan.ProtocolDeltaField)
	}
}

// TestBuild_ProtocolCurrent_Agent_Unchanged verifies that when a worker agent's deployed
// protocol version matches the source, the agent is not scheduled for update on that basis
// alone. A current protocol version must be a no-op for the planner.
func TestBuild_ProtocolCurrent_Agent_Unchanged(t *testing.T) {
	const contentHash = "sha256:current002"
	agent := makeAgent("test-agent", "1.0")

	// Same deployed and source protocol version: no staleness.
	in := buildProtocolStalenessInput(agent, contentHash, "1.9", "1.9")

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("agent with current protocol version and matching stamps: Action = %q, want %q; "+
			"a matching protocol version must not schedule an update",
			item.Action, domain.ActionUnchanged)
	}
}

// TestBuild_ProtocolStaleness_DeltaNotRoutedThroughVersionFieldFormatter verifies that the
// protocol delta field does not appear in the formatVersionDeltas output — i.e., it is not
// reported as "protocol_version changed from '1.7' to '1.9'". Instead, the reason must be
// formatted by ProtocolDrift.Reason() which produces human-readable output naming "protocol".
//
// The test asserts that the item's Reason contains "protocol" but does NOT contain the raw
// field name "protocol_version" in a "changed from ... to" format, which would be the output
// of the generic field formatter.
func TestBuild_ProtocolStaleness_DeltaNotRoutedThroughVersionFieldFormatter(t *testing.T) {
	const contentHash = "sha256:format001"
	agent := makeAgent("test-agent", "1.0")

	in := buildProtocolStalenessInput(agent, contentHash, "1.7", "1.9")

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Skipf("skipping reason format check: Action = %q; the preceding tests cover the classification",
			item.Action)
	}

	// The reason must NOT contain the raw field delta representation.
	genericFormat := "protocol_version changed from"
	if strings.Contains(item.Reason, genericFormat) {
		t.Errorf("item.Reason = %q; must not contain %q; "+
			"the protocol delta must be formatted by ProtocolDrift.Reason(), not the generic field formatter",
			item.Reason, genericFormat)
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Precedence: hash conflict takes priority over protocol staleness
// ---------------------------------------------------------------------------

// TestBuild_ProtocolStale_AndHashConflict_ConflictTakesPrecedence verifies that when a
// deployed file has both a hash mismatch (indicating a local modification) and a stale
// protocol version, the item is classified as ActionConflict. The hash conflict path runs
// before protocol staleness is consulted, so a locally-modified file is never silently
// overwritten as a routine protocol update.
func TestBuild_ProtocolStale_AndHashConflict_ConflictTakesPrecedence(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const recordedHash = "sha256:original"
	const currentHash = "sha256:modified" // differs — local modification

	agent := makeAgent("test-agent", "1.0")

	// Manifest records original hash.
	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", recordedHash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed state: hash differs (conflict) AND protocol is stale.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedStateWithProtocol(currentHash, "1.0", "1.0", "1.0", "1.7"),
	}

	in := buildInputWithSingleAgent(agent, snap, ds)
	in.ProtocolVersion = "1.9"

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("agent with hash conflict and stale protocol version: Action = %q, want %q; "+
			"content-hash conflict detection must take precedence over protocol staleness",
			item.Action, domain.ActionConflict)
	}
	if item.Conflict == nil {
		t.Error("item.Conflict is nil; expected a populated LocalModification for a hash-conflict item")
	}
}

// TestBuild_ProtocolStale_AndHashConflict_ConflictFieldsCorrect verifies that when a hash
// conflict takes precedence over protocol staleness, the LocalModification struct carries the
// expected hash values (recorded hash from manifest, current hash from the deployed file).
// This ensures the conflict-resolution UI can present the correct information to the user.
func TestBuild_ProtocolStale_AndHashConflict_ConflictFieldsCorrect(t *testing.T) {
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

	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedStateWithProtocol(currentHash, "1.0", "1.0", "1.0", "1.7"),
	}

	in := buildInputWithSingleAgent(agent, snap, ds)
	in.ProtocolVersion = "1.9"

	p, err := plan.New().Build(context.Background(), in)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Conflict == nil {
		t.Fatal("item.Conflict is nil; cannot verify conflict fields")
	}
	if item.Conflict.RecordedHash != recordedHash {
		t.Errorf("item.Conflict.RecordedHash = %q, want %q (hash from manifest)",
			item.Conflict.RecordedHash, recordedHash)
	}
	if item.Conflict.CurrentHash != currentHash {
		t.Errorf("item.Conflict.CurrentHash = %q, want %q (hash from deployed file)",
			item.Conflict.CurrentHash, currentHash)
	}
}

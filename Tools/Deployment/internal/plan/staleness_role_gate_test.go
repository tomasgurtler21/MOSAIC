package plan_test

// staleness_role_gate_test.go covers the staleness role gate in classifyAgentItem.
//
// The symptom being fixed: every deployed utility (and, once the constant exists, standalone)
// agent is classified as ActionUpdate on every run, even when it is fully up to date. The
// root cause is that classifyAgentItem calls ProtocolStaleness and BundleStaleness for every
// agent. Both functions report staleness when the deployed file carries no version marker
// at all, and utility/standalone agents intentionally carry no protocol marker and no bundle
// stamp — so the absence of those markers fires as staleness on every run.
//
// The fix introduces a single call-site gate:
//
//	markersApply := domain.RoleCarriesVersionMarkers(agent.Role)
//
// Step 6b (protocol staleness) is only evaluated when markersApply is true.
// Step 6c (bundle staleness Applies field) is derived from markersApply rather than the
// previous "role != orchestrator" expression.
//
// After the fix the staleness-suppression path must be complete end-to-end: the gate alone
// is not enough if the pre-existing step 7 branch (!deployed.HasVersionInfo()) independently
// forces ActionUpdate. The test fixture is constructed so that HasVersionInfo() is true
// (Version, TransformVersion, and InjectionsVersion are all non-empty and match the source),
// confirming that the gate produces ActionUnchanged and not merely "fewer update reasons".
//
// Test coverage:
//
//   T2.1: Utility and standalone agents with no protocol or bundle marker in their deployed
//     file are classified as ActionUnchanged when all other version stamps match. No delta
//     appears in the Stale slice; no protocol or bundle clause appears in Reason.
//
//   T2.2: Subagent and orchestrator agents with no protocol marker in their deployed file
//     are still classified as ActionUpdate. The Stale slice contains a "protocol_version"
//     delta. This is the regression lock — the gate must not over-apply.

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
// Fixtures specific to role gate tests
// ---------------------------------------------------------------------------

// makeStandaloneAgent returns a minimal domain.Agent with role RoleStandalone. Standalone
// agents are deployed on their own and receive harness transformation only; neither a
// protocol marker nor a bundle stamp is written to their deployed file.
func makeStandaloneAgent(key string) domain.Agent {
	return domain.Agent{
		Key:            key,
		Version:        "1.0",
		Name:           key + "-name",
		Description:    key + "-description",
		Role:           domain.RoleStandalone,
		RequiredSkills: []string{},
		SourcePath:     "/fake/agents/" + key + ".md",
	}
}

// deployedStateNoVersionMarkers returns a DeployedArtifactState that has full version-stamp
// info (Version, TransformVersion, InjectionsVersion all "1.0", matching the fake module
// descriptor's stamps so HasVersionInfo() returns true) but carries no protocol version
// marker and no bundle version stamp. This represents a correctly-deployed utility or
// standalone agent: it is up to date on its own stamps, but by design it was never given a
// protocol region or a bundle stamp.
//
// The target content hash is the caller-supplied contentHash so the manifest lookup succeeds
// and the item is not classified as a local modification.
func deployedStateNoVersionMarkers(contentHash string) domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:           true,
		ContentHash:       contentHash,
		Version:           "1.0", // matches agent.Version set by make*Agent helpers
		TransformVersion:  "1.0", // matches newFakeModule().desc.TransformVersion
		InjectionsVersion: "1.0", // matches newFakeModule().desc.InjectionsVersion
		// ProtocolVersion: ""  — absent by design for utility and standalone agents
		// BundleVersion:   ""  — absent by design for utility and standalone agents
	}
}

// buildRoleGateInput returns a minimal plan.Input for a single utility or standalone agent
// included via UtilityAgentIDs. The deployed state has full version-stamp info but no
// protocol or bundle markers (see deployedStateNoVersionMarkers). The Input carries a
// non-empty ProtocolVersion ("1.9") and BundleVersion ("2.0") so that any role for which
// markers apply would see both as stale.
//
// The fixture is designed to isolate the role gate: the only signals that could produce
// ActionUpdate are protocol staleness and bundle staleness; all other stamps match the
// deployed file, the content hash matches the manifest, and HasVersionInfo() is true.
// After the gate is in place, utility and standalone agents should produce ActionUnchanged.
func buildRoleGateInput(agent domain.Agent) plan.Input {
	targetPath := "agents/" + agent.Key + ".md"
	const contentHash = "sha256:role-gate-fixture"

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, "1.0", contentHash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedStateNoVersionMarkers(contentHash),
	}

	cat := &fakeCatalog{
		orchestrator:  makeOrchestrator(),
		utilityAgents: []domain.Agent{agent},
	}

	return plan.Input{
		Catalog:         cat,
		Module:          newFakeModule(),
		Mode:            domain.ModeUpdate,
		WorkspacePath:   "/fake/workspace",
		Scope:           domain.ScopeProject,
		GOOS:            "linux",
		Manifest:        snap,
		UtilityAgentIDs: []string{agent.Key},
		ProtocolVersion: "1.9",
		BundleVersion:   "2.0",
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}
}

// buildSubagentRoleGateInput returns a minimal plan.Input for a subagent-role agent.
// The deployed state has full version-stamp info but no protocol marker and no bundle stamp.
// This is the regression fixture: a subagent without these markers must still be detected
// as stale (ActionUpdate), confirming the gate does not suppress staleness for managed roles.
func buildSubagentRoleGateInput(agent domain.Agent) plan.Input {
	// Re-use buildProtocolStalenessInput (defined in protocol_staleness_test.go): it produces
	// the same fixture — a subagent with no deployed protocol version against a non-empty
	// source protocol version — so the only needed addition is BundleVersion.
	in := buildProtocolStalenessInput(agent, "sha256:subagent-gate", "", "1.9")
	in.BundleVersion = "2.0"
	return in
}

// buildOrchestratorRoleGateInput returns a minimal plan.Input whose only planned agent is
// the orchestrator. The orchestrator's deployed state has full version-stamp info but no
// protocol marker. No workflows are selected so workflow drift is absent; the only staleness
// signal is the missing protocol marker. This is the regression fixture for the orchestrator.
func buildOrchestratorRoleGateInput() plan.Input {
	const contentHash = "sha256:orch-role-gate"
	orc := makeOrchestrator()
	targetPath := "agents/" + orc.Key + ".md"

	entry := makeManifestEntry(agentRef(orc.Key), targetPath, "1.0", contentHash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedStateNoVersionMarkers(contentHash),
	}

	cat := &fakeCatalog{
		orchestrator: orc,
	}

	return plan.Input{
		Catalog:         cat,
		Module:          newFakeModule(),
		Mode:            domain.ModeUpdate,
		WorkspacePath:   "/fake/workspace",
		Scope:           domain.ScopeProject,
		GOOS:            "linux",
		Manifest:        snap,
		ProtocolVersion: "1.9",
		BundleVersion:   "2.0",
		Models: map[string]domain.ModelSelection{
			orc.Key: {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}
}

// ---------------------------------------------------------------------------
// T2.1 — Utility agent: gate suppresses protocol and bundle staleness
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_UtilityAgent_NoProtocolOrBundleMarkers_ClassifiesAsUnchanged verifies
// that a deployed utility agent carrying no protocol marker and no bundle stamp — but with all
// other version stamps matching the source — is classified as ActionUnchanged. A utility agent
// receives harness transformation only; the absence of those markers is by design and must
// never cause a perpetual-update loop.
func TestClassifyAgentItem_UtilityAgent_NoProtocolOrBundleMarkers_ClassifiesAsUnchanged(t *testing.T) {
	agent := makeUtilityAgent("util-agent")
	in := buildRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in %d items", agent.Key, len(got.Items))
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("utility agent with no protocol or bundle markers: action = %v, want ActionUnchanged; "+
			"reason = %q", item.Action, item.Reason)
	}
}

// TestClassifyAgentItem_UtilityAgent_NoProtocolOrBundleMarkers_StaleSliceIsEmpty verifies that
// the Stale slice is empty for the utility agent fixture. An empty slice is required: any
// protocol or bundle delta in the slice would indicate the gate is not suppressing those checks.
func TestClassifyAgentItem_UtilityAgent_NoProtocolOrBundleMarkers_StaleSliceIsEmpty(t *testing.T) {
	agent := makeUtilityAgent("util-agent")
	in := buildRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	if len(item.Stale) != 0 {
		t.Errorf("utility agent with no protocol or bundle markers: Stale = %v, want empty slice; "+
			"the gate must suppress both protocol and bundle deltas for utility agents", item.Stale)
	}
}

// TestClassifyAgentItem_UtilityAgent_NoProtocolOrBundleMarkers_ReasonIsEmpty verifies that
// the Reason field is empty for the utility agent fixture. ActionUnchanged items have an
// empty Reason; any non-empty Reason indicates the item was classified as ActionUpdate.
func TestClassifyAgentItem_UtilityAgent_NoProtocolOrBundleMarkers_ReasonIsEmpty(t *testing.T) {
	agent := makeUtilityAgent("util-agent")
	in := buildRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	if item.Reason != "" {
		t.Errorf("utility agent with no protocol or bundle markers: Reason = %q, want empty; "+
			"ActionUnchanged items must have an empty Reason", item.Reason)
	}
}

// ---------------------------------------------------------------------------
// T2.1 — Standalone agent: gate suppresses protocol and bundle staleness
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_StandaloneAgent_NoProtocolOrBundleMarkers_ClassifiesAsUnchanged
// verifies the same gate behaviour for a standalone-role agent. Standalone agents share the
// same deployment contract as utility agents with respect to version markers: neither a
// protocol marker nor a bundle stamp is written to their deployed file by design.
func TestClassifyAgentItem_StandaloneAgent_NoProtocolOrBundleMarkers_ClassifiesAsUnchanged(t *testing.T) {
	agent := makeStandaloneAgent("standalone-agent")
	in := buildRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in %d items", agent.Key, len(got.Items))
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("standalone agent with no protocol or bundle markers: action = %v, want ActionUnchanged; "+
			"reason = %q", item.Action, item.Reason)
	}
}

// TestClassifyAgentItem_StandaloneAgent_NoProtocolOrBundleMarkers_StaleSliceIsEmpty verifies
// that the Stale slice is empty for the standalone agent fixture.
func TestClassifyAgentItem_StandaloneAgent_NoProtocolOrBundleMarkers_StaleSliceIsEmpty(t *testing.T) {
	agent := makeStandaloneAgent("standalone-agent")
	in := buildRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	if len(item.Stale) != 0 {
		t.Errorf("standalone agent with no protocol or bundle markers: Stale = %v, want empty slice; "+
			"the gate must suppress both protocol and bundle deltas for standalone agents", item.Stale)
	}
}

// TestClassifyAgentItem_StandaloneAgent_NoProtocolOrBundleMarkers_ReasonIsEmpty verifies that
// the Reason field is empty for the standalone agent fixture.
func TestClassifyAgentItem_StandaloneAgent_NoProtocolOrBundleMarkers_ReasonIsEmpty(t *testing.T) {
	agent := makeStandaloneAgent("standalone-agent")
	in := buildRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	if item.Reason != "" {
		t.Errorf("standalone agent with no protocol or bundle markers: Reason = %q, want empty; "+
			"ActionUnchanged items must have an empty Reason", item.Reason)
	}
}

// ---------------------------------------------------------------------------
// T2.1 — Table-driven: both excluded roles produce ActionUnchanged
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_ExcludedRoles_NoMarkers_ClassifiesAsUnchanged runs the gate scenario
// for both excluded roles in a table to catch future additions or regressions in one place.
// Each row confirms: marker-less deployed file + excluded role → ActionUnchanged.
func TestClassifyAgentItem_ExcludedRoles_NoMarkers_ClassifiesAsUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		agent domain.Agent
	}{
		{"utility", makeUtilityAgent("util-agent-table")},
		{"standalone", makeStandaloneAgent("standalone-agent-table")},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			in := buildRoleGateInput(tc.agent)
			p := plan.New()
			got, err := p.Build(context.Background(), in)
			if err != nil {
				t.Fatalf("Build returned error: %v", err)
			}
			item, found := findItem(got.Items, tc.agent.Key)
			if !found {
				t.Fatalf("plan item for %q not found", tc.agent.Key)
			}
			if item.Action != domain.ActionUnchanged {
				t.Errorf("%s agent: action = %v, want ActionUnchanged; reason = %q",
					tc.name, item.Action, item.Reason)
			}
			if len(item.Stale) != 0 {
				t.Errorf("%s agent: Stale = %v, want empty", tc.name, item.Stale)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Subagent regression: protocol staleness still applies
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_Subagent_NoProtocolMarker_ClassifiesAsActionUpdate verifies that
// a deployed subagent carrying no protocol marker is still classified as ActionUpdate. The
// gate must not over-apply; managed roles (subagent and orchestrator) must continue to be
// detected as stale when their protocol marker is absent.
func TestClassifyAgentItem_Subagent_NoProtocolMarker_ClassifiesAsActionUpdate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	in := buildSubagentRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("subagent with no protocol marker: action = %v, want ActionUpdate; "+
			"a missing protocol marker must be detected as staleness for subagents",
			item.Action)
	}
}

// TestClassifyAgentItem_Subagent_NoProtocolMarker_StaleSliceContainsProtocolDelta verifies
// that the Stale slice for the subagent fixture contains a delta whose Field is
// "protocol_version". This confirms the gate did not suppress protocol staleness for a
// managed role.
func TestClassifyAgentItem_Subagent_NoProtocolMarker_StaleSliceContainsProtocolDelta(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	in := buildSubagentRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	hasProtocolDelta := false
	for _, d := range item.Stale {
		if d.Field == plan.ProtocolDeltaField {
			hasProtocolDelta = true
			break
		}
	}
	if !hasProtocolDelta {
		t.Errorf("subagent with no protocol marker: Stale = %v; expected a delta with Field = %q; "+
			"protocol staleness must still apply to subagents",
			item.Stale, plan.ProtocolDeltaField)
	}
}

// TestClassifyAgentItem_Subagent_NoProtocolMarker_ReasonMentionsProtocol verifies that the
// Reason string for the subagent fixture references the absent protocol version marker. This
// is the human-readable confirmation that the staleness reason includes the protocol clause.
func TestClassifyAgentItem_Subagent_NoProtocolMarker_ReasonMentionsProtocol(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	in := buildSubagentRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	if !strings.Contains(item.Reason, "protocol") {
		t.Errorf("subagent with no protocol marker: Reason = %q; expected it to mention \"protocol\"; "+
			"the protocol staleness reason must appear for managed agents",
			item.Reason)
	}
}

// TestClassifyAgentItem_Subagent_NoBundleMarker_StaleSliceContainsBundleDelta verifies that
// a subagent deployed without a bundle stamp is detected as bundle-stale. Bundle staleness
// applies to subagents (the orchestrator is excluded today); the gate must not expand the
// bundle exclusion beyond orchestrators.
func TestClassifyAgentItem_Subagent_NoBundleMarker_StaleSliceContainsBundleDelta(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	// buildSubagentRoleGateInput already sets BundleVersion = "2.0" against an empty
	// deployed BundleVersion, so this test exercises bundle staleness in isolation if
	// the protocol staleness delta is also present — that is fine; we only assert membership.
	in := buildSubagentRoleGateInput(agent)

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item, found := findItem(got.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}

	hasBundleDelta := false
	for _, d := range item.Stale {
		if d.Field == plan.BundleDeltaField {
			hasBundleDelta = true
			break
		}
	}
	if !hasBundleDelta {
		t.Errorf("subagent with no bundle stamp: Stale = %v; expected a delta with Field = %q; "+
			"bundle staleness must still apply to subagents",
			item.Stale, plan.BundleDeltaField)
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Orchestrator regression: protocol staleness still applies
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_Orchestrator_NoProtocolMarker_ClassifiesAsActionUpdate verifies that
// the orchestrator with no deployed protocol marker is classified as ActionUpdate. The gate
// must not suppress protocol staleness for the orchestrator.
func TestClassifyAgentItem_Orchestrator_NoProtocolMarker_ClassifiesAsActionUpdate(t *testing.T) {
	in := buildOrchestratorRoleGateInput()

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	const orchKey = "orchestrator"
	item, found := findItem(got.Items, orchKey)
	if !found {
		t.Fatalf("plan item for %q not found in %d items", orchKey, len(got.Items))
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with no protocol marker: action = %v, want ActionUpdate; "+
			"a missing protocol marker must be detected as staleness for the orchestrator",
			item.Action)
	}
}

// TestClassifyAgentItem_Orchestrator_NoProtocolMarker_StaleSliceContainsProtocolDelta verifies
// that the Stale slice for the orchestrator fixture contains a "protocol_version" delta.
func TestClassifyAgentItem_Orchestrator_NoProtocolMarker_StaleSliceContainsProtocolDelta(t *testing.T) {
	in := buildOrchestratorRoleGateInput()

	p := plan.New()
	got, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	const orchKey = "orchestrator"
	item, found := findItem(got.Items, orchKey)
	if !found {
		t.Fatalf("plan item for %q not found", orchKey)
	}

	hasProtocolDelta := false
	for _, d := range item.Stale {
		if d.Field == plan.ProtocolDeltaField {
			hasProtocolDelta = true
			break
		}
	}
	if !hasProtocolDelta {
		t.Errorf("orchestrator with no protocol marker: Stale = %v; expected a delta with Field = %q; "+
			"protocol staleness must still apply to the orchestrator",
			item.Stale, plan.ProtocolDeltaField)
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Regression table: both managed roles produce ActionUpdate
// ---------------------------------------------------------------------------

// TestClassifyAgentItem_ManagedRoles_NoProtocolMarker_ClassifiesAsActionUpdate is a
// table-driven regression guard confirming that both managed roles (subagent and orchestrator)
// remain ActionUpdate when their deployed file carries no protocol marker. Running both roles
// in a single table prevents a gate change from accidentally suppressing either.
func TestClassifyAgentItem_ManagedRoles_NoProtocolMarker_ClassifiesAsActionUpdate(t *testing.T) {
	subagent := makeAgent("test-agent", "1.0")
	subagentIn := buildSubagentRoleGateInput(subagent)

	orchestratorIn := buildOrchestratorRoleGateInput()

	cases := []struct {
		name    string
		in      plan.Input
		agentKey string
	}{
		{"subagent", subagentIn, subagent.Key},
		{"orchestrator", orchestratorIn, "orchestrator"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := plan.New()
			got, err := p.Build(context.Background(), tc.in)
			if err != nil {
				t.Fatalf("Build returned error: %v", err)
			}
			item, found := findItem(got.Items, tc.agentKey)
			if !found {
				t.Fatalf("plan item for %q not found", tc.agentKey)
			}
			if item.Action != domain.ActionUpdate {
				t.Errorf("%s with no protocol marker: action = %v, want ActionUpdate; "+
					"the gate must not suppress protocol staleness for managed roles",
					tc.name, item.Action)
			}

			hasProtocolDelta := false
			for _, d := range item.Stale {
				if d.Field == plan.ProtocolDeltaField {
					hasProtocolDelta = true
					break
				}
			}
			if !hasProtocolDelta {
				t.Errorf("%s with no protocol marker: Stale = %v; expected field %q in slice",
					tc.name, item.Stale, plan.ProtocolDeltaField)
			}
		})
	}
}

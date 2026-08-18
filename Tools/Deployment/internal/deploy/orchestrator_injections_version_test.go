package deploy_test

// orchestrator_injections_version_test.go covers manifest stamping and version-stamp
// resolution for the orchestrator_injections_version field.
//
// Key behaviors verified:
//   - newManifestEntry copies OrchestratorInjectionsVersion from VersionStamp into ManifestEntry.
//   - resolveVersionStamp handles a delta with Field="orchestrator_injections_version",
//     overriding the VersionStamps map value with delta.Source for ActionUpdate items.
//   - The field name "orchestrator_injections_version" in deltas is the exact name the
//     executor's switch matches; a different name silently drops the stamp.
//   - Subagent items have an empty OrchestratorInjectionsVersion in both VersionStamp and
//     ManifestEntry, producing no false staleness on subsequent runs.
//   - orchestrator_injections_version is independent of injections_version: each field in
//     a Stale delta independently updates only its own manifest field.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// ActionCreate: OrchestratorInjectionsVersion stamped from VersionStamps
// ---------------------------------------------------------------------------

// TestManifest_CreatedItem_OrchestratorInjectionsVersion_StampedFromVersionStamps verifies
// that after an ActionCreate run for the orchestrator agent, the manifest entry carries the
// OrchestratorInjectionsVersion value provided in ExecRequest.VersionStamps. This is the
// primary path for newly deployed orchestrator files.
func TestManifest_CreatedItem_OrchestratorInjectionsVersion_StampedFromVersionStamps(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("orchestrator", "agents/orchestrator.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator content")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/orchestrator.md": {
				Version:                       "5.0",
				TransformVersion:              "3.0",
				InjectionsVersion:             "2.0",
				OrchestratorInjectionsVersion: "1.0",
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest has no entry for created orchestrator item")
	}
	if entry.OrchestratorInjectionsVersion != "1.0" {
		t.Errorf("manifest entry OrchestratorInjectionsVersion = %q; want %q (from VersionStamps)",
			entry.OrchestratorInjectionsVersion, "1.0")
	}
}

// ---------------------------------------------------------------------------
// ActionUpdate: OrchestratorInjectionsVersion resolved from Stale delta
// ---------------------------------------------------------------------------

// TestManifest_UpdatedItem_OrchestratorInjectionsVersionDelta_StampsManifestEntry verifies
// that when an ActionUpdate plan item's Stale slice contains a delta with
// Field="orchestrator_injections_version", the executor's resolveVersionStamp applies
// delta.Source as the OrchestratorInjectionsVersion in the manifest entry. The delta value
// takes precedence over the VersionStamps map for this field.
func TestManifest_UpdatedItem_OrchestratorInjectionsVersionDelta_StampsManifestEntry(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		SourcePath: "/fake/agents/orchestrator.md",
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "orchestrator_injections_version", Deployed: "1.0", Source: "2.0"},
		},
	}

	// VersionStamps provides the non-stale fields.
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator updated")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/orchestrator.md": {
				Version:                       "5.0",
				TransformVersion:              "3.0",
				InjectionsVersion:             "2.0",
				OrchestratorInjectionsVersion: "1.0", // delta overrides this
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for updated orchestrator item")
	}
	// The delta Source "2.0" must override the VersionStamps value "1.0".
	if entry.OrchestratorInjectionsVersion != "2.0" {
		t.Errorf("manifest entry OrchestratorInjectionsVersion = %q; want %q (from delta Source)",
			entry.OrchestratorInjectionsVersion, "2.0")
	}
}

// TestManifest_UpdatedItem_OrchestratorInjectionsVersion_FieldName_IsExact verifies that
// only the exact field name "orchestrator_injections_version" triggers the stamp in
// resolveVersionStamp. An incorrectly named delta (e.g. "orch_injections_version") must not
// update the manifest field; the manifest entry for that field comes from VersionStamps.
// This test guards against typos in the executor's switch statement.
func TestManifest_UpdatedItem_OrchestratorInjectionsVersion_FieldName_IsExact(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// A delta with a subtly wrong field name — must not match.
	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		SourcePath: "/fake/agents/orchestrator.md",
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			// Intentionally wrong name to verify the switch only matches the exact string.
			{Field: "orch_injections_version", Deployed: "1.0", Source: "99.0"},
		},
	}

	const wantOrchestratorVersion = "1.5"
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/orchestrator.md": {
				OrchestratorInjectionsVersion: wantOrchestratorVersion,
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for updated orchestrator item")
	}
	// The wrongly-named delta must not have overwritten the VersionStamps value.
	if entry.OrchestratorInjectionsVersion != wantOrchestratorVersion {
		t.Errorf("manifest entry OrchestratorInjectionsVersion = %q; want %q "+
			"(wrongly named delta must not overwrite VersionStamps value)",
			entry.OrchestratorInjectionsVersion, wantOrchestratorVersion)
	}
}

// ---------------------------------------------------------------------------
// Independence: orchestrator_injections_version and injections_version are separate
// ---------------------------------------------------------------------------

// TestManifest_UpdatedItem_OrchestratorAndInjectionsVersionDeltas_AreIndependent verifies that
// when an ActionUpdate item has stale deltas for both "injections_version" and
// "orchestrator_injections_version", each delta updates only its corresponding field in the
// manifest entry. The two fields are stamped independently.
func TestManifest_UpdatedItem_OrchestratorAndInjectionsVersionDeltas_AreIndependent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		SourcePath: "/fake/agents/orchestrator.md",
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "injections_version", Deployed: "2.0", Source: "3.0"},
			{Field: "orchestrator_injections_version", Deployed: "1.0", Source: "2.0"},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/orchestrator.md": {
				Version:          "5.0",
				TransformVersion: "3.0",
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for updated orchestrator item")
	}
	// Each delta must update only its own field.
	if entry.InjectionsVersion != "3.0" {
		t.Errorf("manifest entry InjectionsVersion = %q; want %q (from delta Source)",
			entry.InjectionsVersion, "3.0")
	}
	if entry.OrchestratorInjectionsVersion != "2.0" {
		t.Errorf("manifest entry OrchestratorInjectionsVersion = %q; want %q (from delta Source)",
			entry.OrchestratorInjectionsVersion, "2.0")
	}
}

// ---------------------------------------------------------------------------
// Subagent: OrchestratorInjectionsVersion must be empty
// ---------------------------------------------------------------------------

// TestManifest_SubagentItem_OrchestratorInjectionsVersion_EmptyInManifest verifies that a
// subagent item whose VersionStamp carries an empty OrchestratorInjectionsVersion results in
// an empty OrchestratorInjectionsVersion in the manifest entry. Subagent files never receive
// this field; the manifest must not invent a value for it.
func TestManifest_SubagentItem_OrchestratorInjectionsVersion_EmptyInManifest(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("test-writer", "agents/test-writer.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# subagent content")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/test-writer.md": {
				Version:                       "2.0",
				TransformVersion:              "1.5",
				InjectionsVersion:             "3.0",
				OrchestratorInjectionsVersion: "", // empty for subagents
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for subagent item")
	}
	if entry.OrchestratorInjectionsVersion != "" {
		t.Errorf("manifest entry OrchestratorInjectionsVersion = %q for subagent; "+
			"want empty string (subagents do not carry this field)",
			entry.OrchestratorInjectionsVersion)
	}
}

// ---------------------------------------------------------------------------
// Workflow deltas must not corrupt OrchestratorInjectionsVersion
// ---------------------------------------------------------------------------

// TestManifest_WorkflowDeltasDoNotCorruptOrchestratorInjectionsVersion verifies that when an
// ActionUpdate plan item's Stale slice contains only workflow-prefixed deltas, the executor
// does not overwrite OrchestratorInjectionsVersion in the manifest entry. The correct value
// must come from VersionStamps, not from a workflow delta whose Field starts with "workflow:".
func TestManifest_WorkflowDeltasDoNotCorruptOrchestratorInjectionsVersion(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		SourcePath: "/fake/agents/orchestrator.md",
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			// Only a workflow-prefixed delta — must not affect OrchestratorInjectionsVersion.
			{Field: "workflow:greenfield-tdd", Deployed: "1.0", Source: "2.0"},
		},
	}

	const wantOrchestratorVersion = "1.0"
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/orchestrator.md": {
				Version:                       "5.0",
				TransformVersion:              "3.0",
				InjectionsVersion:             "2.0",
				OrchestratorInjectionsVersion: wantOrchestratorVersion,
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for orchestrator after workflow-only update")
	}
	if entry.OrchestratorInjectionsVersion != wantOrchestratorVersion {
		t.Errorf("manifest entry OrchestratorInjectionsVersion = %q; want %q; "+
			"workflow delta field must not overwrite the orchestrator_injections_version stamp",
			entry.OrchestratorInjectionsVersion, wantOrchestratorVersion)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: OrchestratorInjectionsVersion preserved across unchanged runs
// ---------------------------------------------------------------------------

// TestManifest_UnchangedOrchestrator_PriorOrchestratorInjectionsVersionPreserved verifies
// that when an ActionUnchanged orchestrator item has an existing manifest entry with a
// non-empty OrchestratorInjectionsVersion, that value is preserved byte-for-byte after the
// run. The executor must not alter any field of an unchanged item's manifest entry.
func TestManifest_UnchangedOrchestrator_PriorOrchestratorInjectionsVersionPreserved(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	const priorOrchVersion = "1.0"
	priorHash := "sha256:" + repeatStr("b", 64)

	store := manifest.NewStore()
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{
				Ref:                           domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath:                    "agents/orchestrator.md",
				ContentHash:                   priorHash,
				OrchestratorInjectionsVersion: priorOrchVersion,
			},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	item := newAgentItem("orchestrator", "agents/orchestrator.md", domain.ActionUnchanged)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator content")),
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("prior manifest entry was removed for an unchanged orchestrator item")
	}
	if entry.OrchestratorInjectionsVersion != priorOrchVersion {
		t.Errorf("unchanged orchestrator: manifest OrchestratorInjectionsVersion = %q; "+
			"want unchanged %q", entry.OrchestratorInjectionsVersion, priorOrchVersion)
	}
}

// repeatStr is a local helper that returns s repeated n times.
// It avoids importing strings just for strings.Repeat in test helpers.
func repeatStr(s string, n int) string {
	result := make([]byte, len(s)*n)
	for i := 0; i < n; i++ {
		copy(result[i*len(s):], s)
	}
	return string(result)
}

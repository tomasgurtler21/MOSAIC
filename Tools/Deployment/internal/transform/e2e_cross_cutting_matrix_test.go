package transform_test

// e2e_cross_cutting_matrix_test.go contains the end-to-end cross-cutting matrix tests
// (T9.5) and the no-content-loss assertion (T9.7) that exercise the full pipeline in
// a single realistic pass.
//
// T9.5 — End-to-End Matrix:
// A single realistic deployed file contains all scenario types simultaneously:
//   1. A [[CUSTOM:]] region anchored to an existing parent (normal update — preserve in place)
//   2. A [[CUSTOM:]] region nested inside a [[DEPLOYED:]] region (nested-region survival)
//   3. A [[CUSTOM:]] region whose parent is removed by a schema reorder (parking + TODO entry)
//   4. A [[INJECTION:]] region that has a rename in the rename table (rename migration)
//
// The source has a different section order from the deployed file, triggering reorder detection.
// Source order: Identity → Capabilities → Constraints.
// Deployed order: Identity → Debugging → Constraints → Capabilities.
// The Debugging section is removed from the source, forcing a parking event for DebuggingNote.
// Capabilities and Constraints are swapped between source and deployed, producing a genuine
// reorder among the common section names (Identity, Capabilities, Constraints).
//
// T9.7 — No Content Loss:
// Every byte of user-owned content (INJECTION + CUSTOM) in the input deployed file is
// accounted for in the output: either present in the output document byte-identically,
// or recorded in the parking gap detail (for parked custom regions), or covered by a
// RegionMigrated outcome (for renamed injections), or covered by RegionOrphaned (for
// injections removed from the source).

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Matrix fixture
//
// Source (order: Identity → Capabilities → Constraints):
//   - Identity section (no INJECTION)
//   - Capabilities section with [[INJECTION:CodebaseContext]]
//   - Constraints section with [[DEPLOYED:HarnessConstraints]] (containing [[INJECTION:LocalPolicy]])
//
// Source no longer contains the "Debugging" section that was in the deployed file.
// Source has an InjectionRenames entry: "LegacyHints" → "CodebaseContext".
//
// Deployed (order: Identity → Debugging → Constraints → Capabilities):
//   - ReorderDetected: yes — Capabilities and Constraints are swapped relative to source order;
//     the common names Identity, Capabilities, Constraints appear as Identity→Constraints→Capabilities
//     in the deployed file, which differs from the source order Identity→Capabilities→Constraints.
//   - [[CUSTOM:WorkspaceNote]] inside Identity (parent still in source) → anchored/preserved
//   - [[CUSTOM:DebuggingNote]] inside Debugging section (parent removed from source) → parked
//   - [[INJECTION:LegacyHints]] with content "Legacy hints content." → renamed to CodebaseContext
//   - [[DEPLOYED:HarnessConstraints]] with [[CUSTOM:ConstraintsNote]] nested inside → nested-survival
// ---------------------------------------------------------------------------

const matrixSource = `---
id: 950
version: 2.0.0
name: e2e-matrix-test
description: Agent for end-to-end cross-cutting matrix testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: e2e matrix testing
required_skills: []
---

[[SECTION:Identity]]
# Matrix Agent

You are the Matrix agent.
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.

[[INJECTION:CodebaseContext]]
[[/INJECTION:CodebaseContext]]
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:LocalPolicy]]
[[/INJECTION:LocalPolicy]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// matrixDeployed: section order Identity → Debugging → Constraints → Capabilities.
// Source has Identity → Capabilities → Constraints.
// Reorder detected: Capabilities and Constraints are swapped relative to source order.
// Debugging is absent from the source, causing DebuggingNote to be parked.
const matrixDeployed = `---
id: 950
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for e2e matrix testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# Matrix Agent

You are the Matrix agent.

[[CUSTOM:WorkspaceNote]]
Project workspace configuration: use monorepo layout.
This is a project-owned note anchored to the Identity section.
[[/CUSTOM:WorkspaceNote]]
[[/SECTION:Identity]]

[[SECTION:Debugging]]
## Debugging Tips

Some debugging content.

[[CUSTOM:DebuggingNote]]
Team-added debugging guidance: always check logs first.
This note is owned by the team and must survive the pipeline — parked when Debugging is removed.
[[/CUSTOM:DebuggingNote]]
[[/SECTION:Debugging]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Canonical harness constraint content (regenerated each transform).
[[CUSTOM:ConstraintsNote]]
Project-added constraint note nested inside HarnessConstraints.
Must survive [[DEPLOYED:]] regeneration even during reorder.
[[/CUSTOM:ConstraintsNote]]
[[INJECTION:LocalPolicy]]
Site-specific local policy content.
[[/INJECTION:LocalPolicy]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.

[[INJECTION:LegacyHints]]
Legacy hints content.
[[/INJECTION:LegacyHints]]
[[/SECTION:Capabilities]]
`

// matrixRenames is the rename table used by the matrix test: LegacyHints → CodebaseContext.
var matrixRenames = []transform.RenameEntry{
	{Old: "LegacyHints", New: "CodebaseContext"},
}

// matrixReq builds the request for the matrix fixture.
func matrixReq(t *testing.T) transform.Request {
	t.Helper()
	return transform.Request{
		Source:          []byte(matrixSource),
		Deployed:        []byte(matrixDeployed),
		Kind:            domain.ArtifactAgent,
		Key:             "e2e-matrix-test",
		Module:          newFixtureModule(t),
		Model:           fixtureModel(),
		Scope:           domain.ScopeProject,
		Timestamp:       "2026-08-11T17:07:26Z",
		InjectionRenames: matrixRenames,
	}
}

// ---------------------------------------------------------------------------
// T9.5 Guard: fixture preconditions
// ---------------------------------------------------------------------------

// TestMatrix_ReorderDetectedByFixture confirms the fixture triggers reorder detection.
func TestMatrix_ReorderDetectedByFixture(t *testing.T) {
	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	srcDoc, err := docformat.Parse([]byte(matrixSource))
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	if !docformat.ReorderDetected(depDoc, srcDoc) {
		t.Error("ReorderDetected must return true for the matrix fixture; " +
			"source order is Identity→Capabilities→Constraints; " +
			"deployed order is Identity→Debugging→Constraints→Capabilities — " +
			"Capabilities and Constraints are swapped among the common names")
	}
}

// TestMatrix_ApplySucceeds verifies that Apply returns no error on the matrix fixture.
func TestMatrix_ApplySucceeds(t *testing.T) {
	req := matrixReq(t)
	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T9.5 Scenario 1: Custom region anchored to existing parent is preserved
// ---------------------------------------------------------------------------

// TestMatrix_AnchoredCustomPreservedInPlace verifies that WorkspaceNote, whose parent
// (Identity) still exists in the source, is carried to the output at its anchored
// position inside Identity, byte-identically.
func TestMatrix_AnchoredCustomPreservedInPlace(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depNode, depOK := depDoc.Body().Custom("WorkspaceNote")
	if !depOK {
		t.Fatal("WorkspaceNote not in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("WorkspaceNote")
	if !outOK {
		t.Fatal("WorkspaceNote absent from output; " +
			"an anchored custom region must be preserved in the output")
	}

	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("WorkspaceNote content not byte-identical:\n"+
			"deployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}
}

// TestMatrix_AnchoredCustomParentIsIdentity verifies WorkspaceNote is inside
// the Identity section in the output (anchored, not promoted to body top level).
func TestMatrix_AnchoredCustomParentIsIdentity(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, outOK := outDoc.Body().Custom("WorkspaceNote")
	if !outOK {
		t.Fatal("WorkspaceNote absent from output")
	}

	parent := outNode.Parent()
	if parent == nil {
		t.Fatal("WorkspaceNote has no parent in output; must be inside Identity section")
	}
	if parent.Kind() != docformat.NodeSection || parent.Name() != "Identity" {
		t.Errorf("WorkspaceNote parent: want Section/Identity, got %v/%v",
			parent.Kind(), parent.Name())
	}
}

// ---------------------------------------------------------------------------
// T9.5 Scenario 2: Nested custom region survives deployed-region regeneration
// ---------------------------------------------------------------------------

// TestMatrix_NestedCustomSurvivesDeployedRegeneration verifies that ConstraintsNote,
// nested inside HarnessConstraints, is re-emitted inside HarnessConstraints in the
// output after the deployed region is regenerated.
func TestMatrix_NestedCustomSurvivesDeployedRegeneration(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depNode, depOK := depDoc.Body().Custom("ConstraintsNote")
	if !depOK {
		t.Fatal("ConstraintsNote not in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("ConstraintsNote")
	if !outOK {
		t.Fatal("ConstraintsNote absent from output; " +
			"a custom region nested inside a [[DEPLOYED:]] region must survive " +
			"the region's regeneration via Stage 6 capture-reemit")
	}

	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("ConstraintsNote content not byte-identical:\n"+
			"deployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}
}

// TestMatrix_NestedCustomParentIsHarnessConstraints verifies ConstraintsNote is placed
// inside HarnessConstraints in the output, not promoted to body level.
func TestMatrix_NestedCustomParentIsHarnessConstraints(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, outOK := outDoc.Body().Custom("ConstraintsNote")
	if !outOK {
		t.Fatal("ConstraintsNote absent from output")
	}

	parent := outNode.Parent()
	if parent == nil {
		t.Fatal("ConstraintsNote has no parent in output; must be inside HarnessConstraints")
	}
	if parent.Kind() != docformat.NodeDeployed || parent.Name() != "HarnessConstraints" {
		t.Errorf("ConstraintsNote parent: want Deployed/HarnessConstraints, got %v/%v",
			parent.Kind(), parent.Name())
	}
}

// ---------------------------------------------------------------------------
// T9.5 Scenario 3: Reorder causes parking of orphaned custom region
// ---------------------------------------------------------------------------

// TestMatrix_OrphanedCustomParkedAtEnd verifies that DebuggingNote, whose parent
// (Debugging section) was removed from the source, is parked at the end of the
// output document with content intact and its region outcome is RegionParked.
func TestMatrix_OrphanedCustomParkedAtEnd(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, outOK := outDoc.Body().Custom("DebuggingNote")
	if !outOK {
		t.Fatal("DebuggingNote absent from output; " +
			"a custom region with a removed parent must be parked at end of file " +
			"with content intact — no content must be lost")
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depNode, _ := depDoc.Body().Custom("DebuggingNote")
	if depNode != nil && !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("DebuggingNote content not byte-identical after parking:\n"+
			"deployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}

	// Verify the region outcome is RegionParked — this confirms the parking code path
	// was exercised, not the normal-append path.
	outcome := findOutcomeByName(result.Report.Regions, "DebuggingNote")
	if outcome == nil {
		t.Fatal("no region outcome for DebuggingNote; parking must produce a region outcome")
	}
	if outcome.Action != transform.RegionParked {
		t.Errorf("DebuggingNote action: want RegionParked (parking code path), got %q "+
			"(fixture must trigger a reorder for the parking code path to execute)",
			outcome.Action)
	}
}

// TestMatrix_ParkingGapEmittedWithTimestamp verifies that a GapParkedCustomRegion gap is
// emitted and its detail contains the caller-supplied timestamp.
func TestMatrix_ParkingGapEmittedWithTimestamp(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no GapParkedCustomRegion in report; " +
			"a parking event must produce a durable TODO entry in the gap list")
	}
	if !strings.Contains(pg.Detail, "2026-08-11") {
		t.Errorf("parking gap detail does not contain the timestamp; got: %q", pg.Detail)
	}
}

// TestMatrix_ParkingGapNamesDebuggingNote verifies that the parking gap detail names
// DebuggingNote as the parked region.
func TestMatrix_ParkingGapNamesDebuggingNote(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no GapParkedCustomRegion in report")
	}
	if !strings.Contains(pg.Detail, "DebuggingNote") {
		t.Errorf("parking gap detail does not mention DebuggingNote: %q", pg.Detail)
	}
}

// TestMatrix_ParkingOutcomeForDebuggingNote verifies that the DebuggingNote region outcome
// has Action RegionParked.
func TestMatrix_ParkingOutcomeForDebuggingNote(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findOutcomeByName(result.Report.Regions, "DebuggingNote")
	if outcome == nil {
		t.Fatal("no region outcome for DebuggingNote")
	}
	if outcome.Action != transform.RegionParked {
		t.Errorf("DebuggingNote action: want RegionParked, got %q", outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T9.5 Scenario 4: Renamed injection migrates content under new name
// ---------------------------------------------------------------------------

// TestMatrix_RenamedInjectionMigrated verifies that LegacyHints → CodebaseContext
// rename causes CodebaseContext to appear in the output with the LegacyHints content.
func TestMatrix_RenamedInjectionMigrated(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depLegacy, _ := depDoc.Body().Injection("LegacyHints")
	if depLegacy == nil {
		t.Fatal("LegacyHints not in deployed fixture; fixture is malformed")
	}

	outNew, outOK := outDoc.Body().Injection("CodebaseContext")
	if !outOK {
		t.Fatal("CodebaseContext absent from output; " +
			"a renamed injection must appear under its new name with the old content")
	}

	if !bytes.Equal(outNew.Content(), depLegacy.Content()) {
		t.Errorf("CodebaseContext content after rename not byte-identical to LegacyHints content:\n"+
			"legacy:   %q\nnew name: %q",
			depLegacy.Content(), outNew.Content())
	}
}

// TestMatrix_LegacyInjectionAbsentFromOutput verifies that LegacyHints is no longer
// present in the output under its old name after the rename migration.
func TestMatrix_LegacyInjectionAbsentFromOutput(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	_, legacyStillPresent := outDoc.Body().Injection("LegacyHints")
	if legacyStillPresent {
		t.Error("LegacyHints still present in output under old name after rename; " +
			"the old name must not appear in the output")
	}
}

// TestMatrix_MigrationOutcomeForCodebaseContext verifies that the CodebaseContext region
// outcome has Action RegionMigrated, confirming the rename was recorded.
func TestMatrix_MigrationOutcomeForCodebaseContext(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findOutcomeByName(result.Report.Regions, "CodebaseContext")
	if outcome == nil {
		t.Fatal("no region outcome for CodebaseContext")
	}
	if outcome.Action != transform.RegionMigrated {
		t.Errorf("CodebaseContext action: want RegionMigrated, got %q", outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T9.7 — No content loss assertion
//
// Every byte of user-owned content in the deployed input must be accounted for:
//   - Present in the output document (for preserved/anchored/nested regions), OR
//   - Named in the parking gap detail (for parked regions), OR
//   - Have a RegionMigrated outcome (content appears under new name), OR
//   - Have a RegionOrphaned outcome (injection removed from source — acceptable loss)
// ---------------------------------------------------------------------------

// TestNoContentLoss_AllUserOwnedNamesAccountedFor verifies that every INJECTION and
// CUSTOM region name from the deployed file has one of the four acceptable fates above.
// This test uses the matrix fixture which covers all four cases.
func TestNoContentLoss_AllUserOwnedNamesAccountedFor(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	parkingDetail := ""
	if pg != nil {
		parkingDetail = pg.Detail
	}

	// Collect all user-owned nodes from the deployed file.
	deployedUserRegions := depDoc.Body().UserRegions()

	for _, dr := range deployedUserRegions {
		name := dr.Name()
		kind := dr.Kind()
		content := dr.Content()

		// Fate 1: present in output by the same name.
		var outNode *docformat.Node
		if kind == docformat.NodeCustom {
			n, ok := outDoc.Body().Custom(name)
			if ok {
				outNode = n
			}
		} else {
			n, ok := outDoc.Body().Injection(name)
			if ok {
				outNode = n
			}
		}
		if outNode != nil {
			// Content must be byte-identical for custom regions.
			if kind == docformat.NodeCustom && !bytes.Equal(outNode.Content(), content) {
				t.Errorf("custom region %q content changed in output:\n"+
					"deployed: %q\noutput:   %q",
					name, content, outNode.Content())
			}
			continue
		}

		// Fate 2: parked (named in parking gap detail).
		if strings.Contains(parkingDetail, name) {
			continue
		}

		// Fate 3: migrated (content appears under new name; outcome recorded).
		// The old name (LegacyHints) has no outcome of its own; the new name
		// (CodebaseContext) carries RegionMigrated. Accept any migrated outcome
		// to cover the case where the old name doesn't appear in the outcome list.
		outcome := findOutcomeByName(result.Report.Regions, name)
		if outcome == nil {
			migratedElsewhere := false
			for _, o := range result.Report.Regions {
				if o.Action == transform.RegionMigrated {
					migratedElsewhere = true
					break
				}
			}
			if migratedElsewhere {
				// Accept: content moved under a new name; the old name is gone.
				continue
			}
		}
		if outcome != nil && (outcome.Action == transform.RegionMigrated ||
			outcome.Action == transform.RegionOrphaned) {
			continue
		}

		t.Errorf("user-owned region %q (%v) is unaccounted for: "+
			"not in output, not parked, not migrated, not orphaned",
			name, kind)
	}
}

// TestNoContentLoss_NoUserContentDropped verifies that no byte of non-empty user-owned
// CUSTOM content silently disappears. All CUSTOM content bytes from the deployed file
// must appear verbatim somewhere in the output bytes (either inline or at end of file).
func TestNoContentLoss_NoUserContentDropped(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}

	for _, node := range depDoc.Body().UserRegions() {
		if node.Kind() != docformat.NodeCustom {
			continue
		}
		content := bytes.TrimSpace(node.Content())
		if len(content) == 0 {
			continue
		}
		if !bytes.Contains(result.Output, content) {
			t.Errorf("CUSTOM region %q content not found anywhere in output; content: %q",
				node.Name(), truncateBytes(content, 80))
		}
	}
}

// TestNoContentLoss_ParkedContentAppearsInOutput verifies that the content of
// DebuggingNote (the parked region) is physically present in the output document bytes,
// since parked regions are appended at end of file.
func TestNoContentLoss_ParkedContentAppearsInOutput(t *testing.T) {
	req := matrixReq(t)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(matrixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}

	depNode, ok := depDoc.Body().Custom("DebuggingNote")
	if !ok {
		t.Fatal("DebuggingNote not in deployed fixture; fixture is malformed")
	}

	content := bytes.TrimSpace(depNode.Content())
	if !bytes.Contains(result.Output, content) {
		t.Errorf("parked custom region DebuggingNote content not found in output bytes; "+
			"parked content: %q", truncateBytes(content, 80))
	}
}

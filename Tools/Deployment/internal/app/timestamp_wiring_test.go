package app_test

// timestamp_wiring_test.go verifies that the app/deploy layer populates
// transform.Request.Timestamp from its injected clock (Deps.Now) before calling
// transform.Apply, and that the resulting parking gap reaches the todo collector carrying
// that timestamp in RFC3339 format.
//
// Verified behaviour (I7.1):
//
//   - When the update flow processes an agent whose deployed file has sections in a
//     different order than its source (schema reorder), and Deps.Now returns a known time,
//     the todo collector receives a GapParkedCustomRegion gap whose Detail contains the
//     RFC3339-formatted timestamp derived from Deps.Now().UTC().
//
// This test is in the TDD RED phase. It will FAIL against the current implementation
// because:
//   - buildContent does not yet populate transform.Request.Timestamp from Deps.Now (I7.1).
//   - transform.Apply does not yet detect schema reorders (I7.3).
//   - No parking gap is emitted by the transform (I7.6 transform side).
//   - No parking gap is forwarded to the todo collector (I7.6 app-layer side).
//
// The test passes once I7.1, I7.3, I7.5, I7.6, and the app-layer gap forwarding are
// all implemented.

import (
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Fixtures for timestamp wiring test
//
// The source has sections in order Identity → Capabilities.
// The deployed has sections in the reversed order Capabilities → Identity, plus a
// top-level custom region (ParkedNote) that should be parked when the reorder is detected.
// ---------------------------------------------------------------------------

// timestampWiringSource is a generic MOSAIC agent file with sections in the order
// Identity → Capabilities. This is the order the implementation will treat as canonical.
const timestampWiringSource = `---
id: 1
version: 1.0.0
name: test-runner
description: Runs tests
model: {model-identifier}
tools: [bash]
recommended_tier: HIGH
tier_rationale: needs capable model
required_skills: []
---

[[SECTION:Identity]]
# Test Runner

You are the test runner.
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

Run tests.
[[/SECTION:Capabilities]]
`

// timestampWiringDeployed is a harness-deployed agent file with sections in the reversed
// order Capabilities → Identity (opposite to timestampWiringSource) plus a top-level
// custom region at body level. The reversed section order triggers reorder detection.
// ParkedNote has no enclosing section parent and will be parked when the reorder is
// detected. The parking gap Detail must include the RFC3339 timestamp that the app layer
// supplies from its clock seam (Deps.Now).
const timestampWiringDeployed = `---
id: 1
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Runs tests
mode: subagent
model: model-a
tools: [bash]
---

[[SECTION:Capabilities]]
## Capabilities

Run tests.
[[/SECTION:Capabilities]]

[[SECTION:Identity]]
# Test Runner

You are the test runner.
[[/SECTION:Identity]]

[[CUSTOM:ParkedNote]]
A top-level custom region to be parked when the schema reorder is detected.
[[/CUSTOM:ParkedNote]]
`

// ---------------------------------------------------------------------------
// I7.1 — app layer populates Request.Timestamp from Deps.Now
// ---------------------------------------------------------------------------

// TestTimestampWiring_UpdateFlow_ParkingGapContainsRFC3339Timestamp verifies that the
// update flow populates transform.Request.Timestamp with an RFC3339-formatted UTC instant
// from Deps.Now before calling transform.Apply, and that the resulting parking gap
// forwarded to the todo collector carries that exact timestamp in its Detail field.
//
// The test uses Deps.Now as the clock seam (already present in the service) to inject a
// fixed, known time, then runs a real update with a schema-reorder scenario and asserts
// the parking gap Detail contains the expected RFC3339 string.
func TestTimestampWiring_UpdateFlow_ParkingGapContainsRFC3339Timestamp(t *testing.T) {
	// Fixed clock so we know the exact RFC3339 string to look for in the parking gap.
	knownTime := time.Date(2026, 8, 11, 17, 7, 26, 0, time.UTC)
	wantTimestamp := knownTime.UTC().Format(time.RFC3339)

	// Write the deployed agent file. The sections are reversed relative to the source,
	// and a top-level custom region is present — both conditions that trigger parking.
	ws := t.TempDir()
	writeTempFile(t, ws, "test-runner.md", []byte(timestampWiringDeployed))

	// Configure the catalog to return valid source bytes when the content callback reads
	// the agent source. minimalAgent.SourcePath is "" so the lookup key is "".
	cat := newMinimalCatalog()
	cat.sources = map[string][]byte{"": []byte(timestampWiringSource)}

	// The capturing executor invokes req.Content(item) for every agent plan item so the
	// real content-generation path (including transform.Apply) runs for test-runner.
	capExec := &contentCapturingExecutor{
		result: deploy.ExecResult{DeploymentRoot: ws},
	}

	// Plan: one ActionUpdate item for test-runner. The content callback is exercised for
	// this item, which causes the app layer to call transform.Apply with the deployed file.
	updateItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionUpdate,
	}

	// todoSpy observes what gaps (if any) the content callback forwards to the collector.
	// After I7.1 and I7.6 are implemented, it must contain a GapParkedCustomRegion whose
	// Detail carries the RFC3339 timestamp from Deps.Now.
	todoSpy := &spyTodo{}

	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdate,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Items:         []domain.PlanItem{updateItem},
	}}
	deps.Executor = capExec
	deps.Todo = todoSpy
	deps.Now = func() time.Time { return knownTime }
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AddWorkflowIDs:  []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The capturing executor must have invoked the content callback for the ActionUpdate
	// item. If not, either the plan was empty or the executor did not call Content.
	if _, ok := capExec.capturedContents["test-runner"]; !ok {
		t.Fatal("Content callback was not invoked for test-runner; the capturing executor " +
			"must call req.Content(item) for every ActionUpdate agent plan item, and " +
			"transform.Apply must not return an error for the fixture content")
	}

	// After the content callback runs (with a reorder scenario), the app layer must have
	// forwarded a GapParkedCustomRegion gap to the todo collector. This requires:
	//   (a) transform.Apply detects the reorder and emits a GapParkedCustomRegion (I7.3–I7.6)
	//   (b) the buildContent closure forwards transform.Result.Report gaps to s.deps.Todo
	var parkingGap *domain.Gap
	for i := range todoSpy.gaps {
		if todoSpy.gaps[i].Kind == domain.GapParkedCustomRegion {
			parkingGap = &todoSpy.gaps[i]
			break
		}
	}
	if parkingGap == nil {
		t.Fatal("no GapParkedCustomRegion in the todo collector after an update with a " +
			"schema-reorder deployed file; the app layer must forward parking gaps from " +
			"transform.Result.Report.Gaps to the todo collector, and the transform must " +
			"emit a GapParkedCustomRegion when a reorder is detected and a top-level " +
			"custom region is present (I7.3–I7.6)")
	}

	// The parking gap Detail must contain the RFC3339 timestamp derived from Deps.Now.
	// This is the observable proof that the app layer populated transform.Request.Timestamp
	// with Deps.Now().UTC().Format(time.RFC3339) before calling transform.Apply (I7.1).
	if !strings.Contains(parkingGap.Detail, wantTimestamp) {
		t.Errorf("parking gap Detail does not contain the RFC3339 timestamp %q from Deps.Now; "+
			"the app layer must call Deps.Now().UTC().Format(time.RFC3339) and supply it as "+
			"transform.Request.Timestamp before calling transform.Apply (I7.1).\n"+
			"parking gap Detail: %s", wantTimestamp, parkingGap.Detail)
	}
}

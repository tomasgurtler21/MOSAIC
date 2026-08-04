package transform_test

// region_orphan_test.go covers orphan detection for the new marker vocabulary (T3.3):
//
//   - A user-owned [[INJECTION:]] region present in the deployed file but absent from the
//     source produces an orphaned RegionOutcome and a GapRemovedInjection gap carrying
//     the user's content.
//   - A tool-managed [[DEPLOYED:]] region present in the deployed file but absent from the
//     source produces no gap — tool-managed content is regenerated, never recovered.
//   - When multiple orphaned user-owned regions exist, their outcomes are emitted in sorted
//     (alphabetical) name order regardless of their order in the deployed file, ensuring
//     the report is deterministic across invocations.

import (
	"sort"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// sourceWithoutContextLimits is a source whose Constraints section does NOT include a
// ContextLimits injection point that existed in the deployed file below. A deployed file
// with ContextLimits user content must produce a recovery gap.
const sourceWithoutContextLimits = `---
id: 50
version: 2.0.0
name: orphan-test
description: Agent for orphan detection testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: orphan detection testing
required_skills: []
---

[[SECTION:Identity]]
# OrphanTest Agent

You are the OrphanTest agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// deployedWithContextLimitsContent is the deployed predecessor of sourceWithoutContextLimits.
// It has [[INJECTION:ContextLimits]] with user content, which has since been removed from
// the source. The removal must produce a recovery gap carrying the user's content.
// It also has [[DEPLOYED:HarnessConstraints]] whose removal from source must NOT produce a gap.
const deployedWithContextLimitsContent = `---
id: 50
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for orphan detection testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# OrphanTest Agent

You are the OrphanTest agent.

[[INJECTION:IdentityExtension]]
User identity extension content.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

[[INJECTION:ContextLimits]]
Context limit: focus on the single task described in the request.
Do not start new investigations without completing the current one.
[[/INJECTION:ContextLimits]]
[[/SECTION:ExecutionPhilosophy]]

[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:HarnessConstraints]]
Stale harness content — tool-managed, must never produce a recovery gap.
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// T3.3: User-owned region removed from source yields a recovery gap
// ---------------------------------------------------------------------------

// TestOrphanDetection_UserOwnedRegionRemovedFromSource_GapEmitted asserts that when a
// [[INJECTION:]] region is present in the deployed file with user content but is absent
// from the source, a GapRemovedInjection gap is emitted carrying the user's content.
// The orphaned region must not appear in the output.
func TestOrphanDetection_UserOwnedRegionRemovedFromSource_GapEmitted(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutContextLimits),
		Deployed: []byte(deployedWithContextLimitsContent),
		Kind:     domain.ArtifactAgent,
		Key:      "orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A GapRemovedInjection gap must be emitted for ContextLimits.
	var removedGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection &&
			result.Report.Gaps[i].Subject == "ContextLimits" {
			removedGap = &result.Report.Gaps[i]
			break
		}
	}
	if removedGap == nil {
		t.Fatalf("expected GapRemovedInjection for ContextLimits; gaps: %v", result.Report.Gaps)
	}

	// The gap fragment must carry the orphaned user content.
	expectedContent := "Context limit: focus on the single task described in the request."
	if removedGap.Fragment == "" || !strings.Contains(removedGap.Fragment, expectedContent) {
		t.Errorf("GapRemovedInjection.Fragment must contain the orphaned user content;\nFragment: %q\nexpected to contain: %q",
			removedGap.Fragment, expectedContent)
	}

	// Report.Regions must have an orphaned outcome for ContextLimits.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "ContextLimits" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for ContextLimits absent from Report.Regions; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionOrphaned {
		t.Errorf("ContextLimits Action: want %q, got %q", transform.RegionOrphaned, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T3.3: Tool-managed region removed from source yields no gap
// ---------------------------------------------------------------------------

// TestOrphanDetection_ToolManagedRegionRemovedFromSource_NoGap asserts that when a
// [[DEPLOYED:]] region is present in the deployed file but absent from the source, no
// GapRemovedInjection gap is emitted. Tool-managed content is regenerated on every
// transform; it is never user-authored and therefore has no recovery value.
func TestOrphanDetection_ToolManagedRegionRemovedFromSource_NoGap(t *testing.T) {
	// sourceWithoutHarnessConstraints is a source that no longer declares
	// [[DEPLOYED:HarnessConstraints]], which was present in deployedWithContextLimitsContent.
	const sourceWithoutHarnessConstraints = `---
id: 50
version: 3.0.0
name: orphan-no-gap-test
description: Agent that no longer has the HarnessConstraints region
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: no-gap orphan testing
required_skills: []
---

[[SECTION:Identity]]
# OrphanNoGapTest Agent

You are the OrphanNoGapTest agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

	req := transform.Request{
		Source:   []byte(sourceWithoutHarnessConstraints),
		Deployed: []byte(deployedWithContextLimitsContent),
		Kind:     domain.ArtifactAgent,
		Key:      "orphan-no-gap-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No GapRemovedInjection gap must be emitted for HarnessConstraints.
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "HarnessConstraints" {
			t.Errorf("unexpected GapRemovedInjection for tool-managed region HarnessConstraints; "+
				"tool-managed regions removed from source must produce no recovery gap.\n"+
				"Gap: %+v", g)
		}
	}

	// No orphaned outcome must exist for HarnessConstraints in Report.Regions.
	for _, outcome := range result.Report.Regions {
		if outcome.Name == "HarnessConstraints" && outcome.Action == transform.RegionOrphaned {
			t.Errorf("unexpected RegionOrphaned outcome for tool-managed HarnessConstraints; "+
				"tool-managed regions produce no orphaned outcome")
		}
	}
}

// ---------------------------------------------------------------------------
// T3.3: Multiple orphaned user-owned regions are emitted in sorted order
// ---------------------------------------------------------------------------

// deployedWithMultipleOrphans is a deployed file with three [[INJECTION:]] regions that are
// all absent from the source below. Their orphaned outcomes must be emitted in sorted name
// order so the report and gap list are deterministic.
const deployedWithMultipleOrphans = `---
id: 50
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent with multiple orphan injection points
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Capabilities]]
## Capabilities

[[INJECTION:CodebaseContext]]
Codebase context: A-content.
[[/INJECTION:CodebaseContext]]

[[INJECTION:OutputArtifactTemplate]]
Output template content: B-content.
[[/INJECTION:OutputArtifactTemplate]]
[[/SECTION:Capabilities]]

[[SECTION:ErrorHandling]]
## Error Handling

[[INJECTION:ErrorHandlingExtension]]
Error handling content: C-content.
[[/INJECTION:ErrorHandlingExtension]]
[[/SECTION:ErrorHandling]]

[[SECTION:ExecutionPhilosophy]]
## ExecutionPhilosophy

[[INJECTION:ContextLimits]]
Context limits: D-content.
[[/INJECTION:ContextLimits]]
[[/SECTION:ExecutionPhilosophy]]
`

// sourceWithNoInjections is a source document that has none of the injection regions present
// in deployedWithMultipleOrphans. All four user-owned regions become orphans.
const sourceWithNoInjections = `---
id: 50
version: 2.0.0
name: orphan-order-test
description: Agent for orphan ordering testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: orphan order testing
required_skills: []
---

[[SECTION:Identity]]
# OrphanOrderTest Agent

You are the OrphanOrderTest agent.
[[/SECTION:Identity]]
`

// TestOrphanDetection_MultipleOrphans_EmittedInSortedNameOrder asserts that when multiple
// user-owned [[INJECTION:]] regions are orphaned, their outcomes in Report.Regions appear
// in alphabetically sorted name order, and the corresponding GapRemovedInjection gaps also
// appear in sorted order. This determinism is required because Go's map iteration order is
// randomised, and the report must be consistent across runs.
func TestOrphanDetection_MultipleOrphans_EmittedInSortedNameOrder(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithNoInjections),
		Deployed: []byte(deployedWithMultipleOrphans),
		Kind:     domain.ArtifactAgent,
		Key:      "orphan-order-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Collect orphaned outcomes from Report.Regions.
	var orphanedOutcomes []transform.RegionOutcome
	for _, outcome := range result.Report.Regions {
		if outcome.Action == transform.RegionOrphaned {
			orphanedOutcomes = append(orphanedOutcomes, outcome)
		}
	}

	// All four orphans must be present.
	expectedOrphans := []string{"CodebaseContext", "ContextLimits", "ErrorHandlingExtension", "OutputArtifactTemplate"}
	if len(orphanedOutcomes) != len(expectedOrphans) {
		t.Fatalf("orphaned outcomes: want %d, got %d: %v", len(expectedOrphans), len(orphanedOutcomes), orphanedOutcomes)
	}

	// The outcomes must be in sorted name order.
	orphanNames := make([]string, len(orphanedOutcomes))
	for i, o := range orphanedOutcomes {
		orphanNames[i] = o.Name
	}

	sortedNames := make([]string, len(orphanNames))
	copy(sortedNames, orphanNames)
	sort.Strings(sortedNames)

	for i, want := range sortedNames {
		if orphanNames[i] != want {
			t.Errorf("orphaned outcomes not in sorted order at index %d: want %q, got %q\nfull order: %v",
				i, want, orphanNames[i], orphanNames)
			break
		}
	}

	// Verify the gaps are also in sorted order.
	var removedGapSubjects []string
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection {
			removedGapSubjects = append(removedGapSubjects, g.Subject)
		}
	}

	sortedGapSubjects := make([]string, len(removedGapSubjects))
	copy(sortedGapSubjects, removedGapSubjects)
	sort.Strings(sortedGapSubjects)

	for i, want := range sortedGapSubjects {
		if removedGapSubjects[i] != want {
			t.Errorf("removal gaps not in sorted order at index %d: want %q, got %q\nfull order: %v",
				i, want, removedGapSubjects[i], removedGapSubjects)
			break
		}
	}
}

// ---------------------------------------------------------------------------
// T3.3: Orphan gap carries the user's content for recovery
// ---------------------------------------------------------------------------

// TestOrphanDetection_GapCarriesOrphanedContent asserts that the GapRemovedInjection gap
// emitted for an orphaned [[INJECTION:]] region carries the region's content verbatim, so
// the user can recover whatever they had written there.
func TestOrphanDetection_GapCarriesOrphanedContent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutContextLimits),
		Deployed: []byte(deployedWithContextLimitsContent),
		Kind:     domain.ArtifactAgent,
		Key:      "orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Find the GapRemovedInjection for ContextLimits.
	var removedGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection &&
			result.Report.Gaps[i].Subject == "ContextLimits" {
			removedGap = &result.Report.Gaps[i]
			break
		}
	}
	if removedGap == nil {
		t.Fatalf("GapRemovedInjection for ContextLimits not found; gaps: %v", result.Report.Gaps)
	}

	// The fragment must not be empty and must contain the exact user content from the
	// deployed file. An empty fragment silently loses the user's work.
	if removedGap.Fragment == "" {
		t.Error("GapRemovedInjection.Fragment must not be empty; it must carry the user's content for recovery")
	}
	expectedLines := []string{
		"Context limit: focus on the single task described in the request.",
		"Do not start new investigations without completing the current one.",
	}
	for _, line := range expectedLines {
		if !strings.Contains(removedGap.Fragment, line) {
			t.Errorf("GapRemovedInjection.Fragment missing expected content line %q\nFragment: %q",
				line, removedGap.Fragment)
		}
	}
}

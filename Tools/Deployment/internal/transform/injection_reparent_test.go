package transform_test

// injection_reparent_test.go covers advisory TODO emission when a source [[INJECTION:]]
// region that already existed in the previously deployed file is found under a different
// parent section in the new source document than it had in the deployed file.
//
// Key invariants under test:
//   - A re-parented injection (present in the deployed file under a different parent)
//     produces exactly one GapInjectionReparented gap naming the injection and both parents.
//   - The gap is advisory: the transform still succeeds and the injection's content is
//     preserved verbatim — placement and outcome reporting are unchanged.
//   - An injection whose parent is unchanged produces no GapInjectionReparented gap.
//   - An injection new in the source (absent from the deployed file) produces no gap.
//   - A create run (req.Deployed == nil) produces no GapInjectionReparented gap.
//   - Injections nested inside [[DEPLOYED:]] regions are in scope for re-parenting detection
//     (AD-E); the comparison is a read-only inspection independent of the main-loop skip.
//   - The comparison is keyed on the resolved (post-rename) injection name, so a rename
//     alone in the same parent never trips the check.
//   - Top-level (empty parent name) to nested and nested to top-level are both genuine
//     re-parenting events and both emit the advisory.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Fixture 900 — Basic re-parenting: injection moved to a different section
//
// Source has IdentityExtension inside Capabilities.
// Deployed has IdentityExtension inside Identity with user content.
// The injection moved from Identity to Capabilities → re-parenting gap expected.
// IdentityExtension is present in both documents, so this is an update run, and the
// gap fires.
// ---------------------------------------------------------------------------

const reparentSource = `---
id: 900
version: 2.0.0
name: reparent-test
description: Agent for injection re-parenting tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: reparent testing
required_skills: []
---

[[SECTION:Identity]]
# Reparent Agent

You are the Reparent agent.
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Capabilities]]
`

// reparentDeployed has IdentityExtension inside Identity (not Capabilities).
// The source moved it to Capabilities, so the anchor parent changed.
const reparentDeployed = `---
id: 900
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection re-parenting tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# Reparent Agent

You are the Reparent agent.

[[INJECTION:IdentityExtension]]
User identity extension content — this must be preserved at the new location.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.
[[/SECTION:Capabilities]]
`

// ---------------------------------------------------------------------------
// Fixture 901 — Unchanged parent: no re-parenting gap expected
//
// Both source and deployed have IdentityExtension inside Identity.
// The parent is the same → no GapInjectionReparented.
// ---------------------------------------------------------------------------

const unchangedParentSource = `---
id: 901
version: 2.0.0
name: unchanged-parent-test
description: Agent for unchanged-parent negative tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: unchanged parent testing
required_skills: []
---

[[SECTION:Identity]]
# UnchangedParent Agent

You are the UnchangedParent agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

const unchangedParentDeployed = `---
id: 901
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for unchanged-parent negative tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# UnchangedParent Agent

You are the UnchangedParent agent.

[[INJECTION:IdentityExtension]]
Saved identity extension content.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

// ---------------------------------------------------------------------------
// Fixture 902 — New injection in source: no re-parenting gap expected
//
// Source has ContextExtension (new in this version) plus IdentityExtension.
// Deployed has only IdentityExtension; ContextExtension is absent.
// ContextExtension has no previous parent to differ from → no gap.
// IdentityExtension parent is unchanged (Identity in both) → no gap.
// ---------------------------------------------------------------------------

const newInjectionSource = `---
id: 902
version: 2.0.0
name: new-injection-test
description: Agent for new-injection negative tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: new injection testing
required_skills: []
---

[[SECTION:Identity]]
# NewInjection Agent

You are the NewInjection agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.

[[INJECTION:ContextExtension]]
[[/INJECTION:ContextExtension]]
[[/SECTION:Capabilities]]
`

const newInjectionDeployed = `---
id: 902
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for new-injection negative tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NewInjection Agent

You are the NewInjection agent.

[[INJECTION:IdentityExtension]]
Saved identity extension content.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

// ---------------------------------------------------------------------------
// Fixture 903 — Injection nested inside [[DEPLOYED:]] region (AD-E: in scope)
//
// Source: ContextExtension nested inside HarnessConstraints (inside Constraints section).
//   The injection's anchor parent name is "HarnessConstraints".
// Deployed: ContextExtension inside Identity section (not nested inside a deployed region).
//   The injection's anchor parent name is "Identity".
//
// The anchor parents differ → re-parenting gap must be emitted even though the injection
// is skipped by the main source-region loop (it is nested inside a deployed region).
// Detection walks all source [[INJECTION:]] nodes at any depth, independent of the loop skip.
// ---------------------------------------------------------------------------

const nestedReparentSource = `---
id: 903
version: 2.0.0
name: nested-reparent-test
description: Agent for nested-injection re-parenting tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested reparent testing
required_skills: []
---

[[SECTION:Identity]]
# NestedReparent Agent

You are the NestedReparent agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:ContextExtension]]
[[/INJECTION:ContextExtension]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedReparentDeployed has ContextExtension in Identity — NOT nested inside a deployed
// region. After the source moves it inside HarnessConstraints, the anchor parent changed
// from "Identity" to "HarnessConstraints".
const nestedReparentDeployed = `---
id: 903
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested-injection re-parenting tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedReparent Agent

You are the NestedReparent agent.

[[INJECTION:ContextExtension]]
User context extension content inside Identity.
[[/INJECTION:ContextExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// Fixture 904 — Injection nested inside deployed region, parent unchanged
//
// Source: ContextExtension nested inside HarnessConstraints (anchor parent: HarnessConstraints)
// Deployed: ContextExtension nested inside HarnessConstraints (anchor parent: HarnessConstraints)
// Parent is the same → no GapInjectionReparented.
// ---------------------------------------------------------------------------

const nestedSameParentSource = `---
id: 904
version: 2.0.0
name: nested-same-parent-test
description: Agent for nested-same-parent negative tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested same parent testing
required_skills: []
---

[[SECTION:Identity]]
# NestedSameParent Agent

You are the NestedSameParent agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:ContextExtension]]
[[/INJECTION:ContextExtension]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedSameParentDeployed has ContextExtension nested inside HarnessConstraints — matching
// the source. Anchor parent in both is "HarnessConstraints" → no re-parenting gap.
const nestedSameParentDeployed = `---
id: 904
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested-same-parent negative tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedSameParent Agent

You are the NestedSameParent agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.

[[INJECTION:ContextExtension]]
User context extension content inside HarnessConstraints.
[[/INJECTION:ContextExtension]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// Fixture 905 — Top-level to nested transition
//
// Source: TopLevelInjection nested inside Identity section (anchor parent: "Identity")
// Deployed: TopLevelInjection at body top level (anchor parent: "", i.e. top level)
// Top-level → nested is genuine re-parenting → gap emitted.
// ---------------------------------------------------------------------------

const topLevelToNestedSource = `---
id: 905
version: 2.0.0
name: toplevel-to-nested-test
description: Agent for top-level-to-nested re-parenting tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: toplevel to nested testing
required_skills: []
---

[[SECTION:Identity]]
# TopLevelToNested Agent

You are the TopLevelToNested agent.

[[INJECTION:TopLevelInjection]]
[[/INJECTION:TopLevelInjection]]
[[/SECTION:Identity]]
`

// topLevelToNestedDeployed has TopLevelInjection at body top level (anchor parent: "").
// The source moves it inside the Identity section, so the parent changes from "" to "Identity".
const topLevelToNestedDeployed = `---
id: 905
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for top-level-to-nested re-parenting tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[INJECTION:TopLevelInjection]]
User top-level injection content.
[[/INJECTION:TopLevelInjection]]

[[SECTION:Identity]]
# TopLevelToNested Agent

You are the TopLevelToNested agent.
[[/SECTION:Identity]]
`

// ---------------------------------------------------------------------------
// Fixture 906 — Nested to top-level transition
//
// Source: TopLevelInjection at body top level (anchor parent: "")
// Deployed: TopLevelInjection inside Identity section (anchor parent: "Identity")
// Nested → top-level is genuine re-parenting → gap emitted.
// ---------------------------------------------------------------------------

const nestedToTopLevelSource = `---
id: 906
version: 2.0.0
name: nested-to-toplevel-test
description: Agent for nested-to-top-level re-parenting tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested to toplevel testing
required_skills: []
---

[[INJECTION:TopLevelInjection]]
[[/INJECTION:TopLevelInjection]]

[[SECTION:Identity]]
# NestedToTopLevel Agent

You are the NestedToTopLevel agent.
[[/SECTION:Identity]]
`

// nestedToTopLevelDeployed has TopLevelInjection inside Identity (anchor parent: "Identity").
// The source moves it to body top level, so the parent changes from "Identity" to "".
const nestedToTopLevelDeployed = `---
id: 906
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested-to-top-level re-parenting tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedToTopLevel Agent

You are the NestedToTopLevel agent.

[[INJECTION:TopLevelInjection]]
User injection content that was inside Identity.
[[/INJECTION:TopLevelInjection]]
[[/SECTION:Identity]]
`

// ---------------------------------------------------------------------------
// Helper: find a GapInjectionReparented gap by injection name (nil when absent)
// ---------------------------------------------------------------------------

func findReparentGap(gaps []domain.Gap, injectionName string) *domain.Gap {
	for i := range gaps {
		if gaps[i].Kind == domain.GapInjectionReparented && gaps[i].Subject == injectionName {
			return &gaps[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// T4.1 — Positive: injection moved to a different section emits advisory gap
// ---------------------------------------------------------------------------

// TestInjectionReparented_DifferentParent_EmitsGap verifies that when an injection that
// existed in the deployed file now sits under a different parent section in the source,
// a GapInjectionReparented gap is produced.
func TestInjectionReparented_DifferentParent_EmitsGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("expected GapInjectionReparented for IdentityExtension (moved from Identity to Capabilities); got no such gap")
	}
}

// TestInjectionReparented_DifferentParent_TransformSucceeds verifies that the transform
// still succeeds when a re-parenting is detected. The gap is advisory: it must not
// abort or modify the transform outcome.
func TestInjectionReparented_DifferentParent_TransformSucceeds(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply returned error when re-parenting was detected; the gap is advisory and must not abort the transform: %v", err)
	}
}

// TestInjectionReparented_DifferentParent_ContentPreservedVerbatim verifies that the
// injection's content is preserved byte-identically from the deployed file even when a
// re-parenting gap is emitted. The advisory changes signalling only, not content.
func TestInjectionReparented_DifferentParent_ContentPreservedVerbatim(t *testing.T) {
	const expectedContent = "User identity extension content — this must be preserved at the new location.\n"

	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("IdentityExtension absent from output after re-parenting; injection must still be present and filled")
	}
	if !bytes.Contains(node.Content(), []byte(expectedContent)) {
		t.Errorf("IdentityExtension content not preserved verbatim after re-parenting:\nwant (substr): %q\ngot: %q",
			expectedContent, node.Content())
	}
}

// TestInjectionReparented_DifferentParent_OutcomeActionPreserved verifies that the
// re-parenting advisory does not alter the RegionOutcome action for the injection. The
// content is still lifted from the deployed file; the action remains RegionPreserved.
func TestInjectionReparented_DifferentParent_OutcomeActionPreserved(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findOutcomeByName(result.Report.Regions, "IdentityExtension")
	if outcome == nil {
		t.Fatal("no RegionOutcome for IdentityExtension in report")
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("IdentityExtension action: want %q (re-parenting advisory must not change the action), got %q",
			transform.RegionPreserved, outcome.Action)
	}
}

// TestInjectionReparented_DifferentParent_GapSubjectIsInjectionName verifies that the
// GapInjectionReparented gap's Subject field is the injection name — not the artifact key.
// Unlike the fallthrough gap (which aggregates a whole artifact), this gap is per-injection.
func TestInjectionReparented_DifferentParent_GapSubjectIsInjectionName(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("no GapInjectionReparented gap for IdentityExtension")
	}
	if gap.Subject != "IdentityExtension" {
		t.Errorf("GapInjectionReparented Subject: want %q (injection name), got %q",
			"IdentityExtension", gap.Subject)
	}
}

// TestInjectionReparented_DifferentParent_GapDetailContainsInjectionName verifies that
// the gap's Detail field names the injection that was re-parented.
func TestInjectionReparented_DifferentParent_GapDetailContainsInjectionName(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("no GapInjectionReparented gap in report")
	}
	if !strings.Contains(gap.Detail, "IdentityExtension") {
		t.Errorf("GapInjectionReparented Detail does not contain the injection name %q: %s",
			"IdentityExtension", gap.Detail)
	}
}

// TestInjectionReparented_DifferentParent_GapDetailContainsOldParent verifies that the
// gap's Detail field names the injection's previous parent (from the deployed file).
func TestInjectionReparented_DifferentParent_GapDetailContainsOldParent(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("no GapInjectionReparented gap in report")
	}
	// Old parent was "Identity" (the section IdentityExtension lived in before the move).
	if !strings.Contains(gap.Detail, "Identity") {
		t.Errorf("GapInjectionReparented Detail does not contain old parent %q: %s",
			"Identity", gap.Detail)
	}
}

// TestInjectionReparented_DifferentParent_GapDetailContainsNewParent verifies that the
// gap's Detail field names the injection's new parent (from the source document).
func TestInjectionReparented_DifferentParent_GapDetailContainsNewParent(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("no GapInjectionReparented gap in report")
	}
	// New parent is "Capabilities" (the section IdentityExtension was moved to in the source).
	if !strings.Contains(gap.Detail, "Capabilities") {
		t.Errorf("GapInjectionReparented Detail does not contain new parent %q: %s",
			"Capabilities", gap.Detail)
	}
}

// TestInjectionReparented_DifferentParent_GapDetailContainsTimestamp verifies that the
// gap's Detail field contains the caller-supplied timestamp from Request.Timestamp.
func TestInjectionReparented_DifferentParent_GapDetailContainsTimestamp(t *testing.T) {
	const ts = "2026-08-12T14:25:33Z"
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		ts,
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("no GapInjectionReparented gap in report")
	}
	if !strings.Contains(gap.Detail, ts) {
		t.Errorf("GapInjectionReparented Detail does not contain caller-supplied timestamp %q: %s",
			ts, gap.Detail)
	}
}

// TestInjectionReparented_DifferentParent_GapFragmentIsEmpty verifies that the
// GapInjectionReparented gap's Fragment field is empty. Content is preserved in the output
// at the new location; no paste-fragment is required.
func TestInjectionReparented_DifferentParent_GapFragmentIsEmpty(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap == nil {
		t.Fatal("no GapInjectionReparented gap in report")
	}
	if gap.Fragment != "" {
		t.Errorf("GapInjectionReparented Fragment must be empty (content is in the output at the new location); got %q",
			gap.Fragment)
	}
}

// TestInjectionReparented_ExactlyOneGapPerReparentedInjection verifies that exactly one
// GapInjectionReparented gap is emitted per re-parented injection (not per transform).
// Cardinality: one gap per re-parented injection, identified by Subject.
func TestInjectionReparented_ExactlyOneGapPerReparentedInjection(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		[]byte(reparentDeployed),
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var reparentGaps []domain.Gap
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapInjectionReparented && g.Subject == "IdentityExtension" {
			reparentGaps = append(reparentGaps, g)
		}
	}
	if len(reparentGaps) == 0 {
		t.Fatal("no GapInjectionReparented gap for IdentityExtension; expected exactly one")
	}
	if len(reparentGaps) > 1 {
		t.Errorf("expected exactly one GapInjectionReparented gap for IdentityExtension; got %d", len(reparentGaps))
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Negative: unchanged parent produces no re-parenting gap
// ---------------------------------------------------------------------------

// TestInjectionReparented_SameParent_NoGap verifies that an injection whose parent
// section is the same in both the source and the deployed file produces no
// GapInjectionReparented gap. The gap fires only on a genuine parent change.
func TestInjectionReparented_SameParent_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(unchangedParentSource),
		[]byte(unchangedParentDeployed),
		"unchanged-parent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "IdentityExtension")
	if gap != nil {
		t.Errorf("GapInjectionReparented emitted for IdentityExtension even though its parent section is unchanged; "+
			"the gap must fire only when the parent changes: detail=%s", gap.Detail)
	}
}

// TestInjectionReparented_NewInSource_NoGap verifies that an injection that is new in
// the source (absent from the deployed file) produces no GapInjectionReparented gap.
// A region with no prior parent cannot have moved.
func TestInjectionReparented_NewInSource_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(newInjectionSource),
		[]byte(newInjectionDeployed),
		"new-injection-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// ContextExtension is new in the source; it must produce no GapInjectionReparented.
	gap := findReparentGap(result.Report.Gaps, "ContextExtension")
	if gap != nil {
		t.Errorf("GapInjectionReparented emitted for ContextExtension which is new in the source (absent from deployed); "+
			"a region with no prior parent cannot have been re-parented: detail=%s", gap.Detail)
	}
}

// TestInjectionReparented_CreateRun_NoGap verifies that a create run (req.Deployed == nil)
// produces no GapInjectionReparented gap. When there is no deployed file, no injection
// has a previous parent to differ from.
func TestInjectionReparented_CreateRun_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(reparentSource),
		nil, // new deployment — no deployed file
		"reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapInjectionReparented {
			t.Errorf("GapInjectionReparented emitted on a create run (Deployed == nil); "+
				"no injection has a previous parent on create: subject=%q detail=%s",
				g.Subject, g.Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Nested injection inside [[DEPLOYED:]] region: in scope for detection (AD-E)
// ---------------------------------------------------------------------------

// TestInjectionReparented_NestedInsideDeployed_DifferentParent_EmitsGap verifies that when
// an injection nested inside a [[DEPLOYED:]] region in the source had a different parent in
// the deployed file, the re-parenting gap is still emitted. Per AD-E, detection walks all
// source [[INJECTION:]] nodes at any depth, independent of the main-loop skip that bypasses
// applyProjectRegion for nested injections.
func TestInjectionReparented_NestedInsideDeployed_DifferentParent_EmitsGap(t *testing.T) {
	// nestedReparentSource has ContextExtension nested inside HarnessConstraints (parent: HarnessConstraints).
	// nestedReparentDeployed has ContextExtension inside Identity (parent: Identity).
	req := reorderReq(t,
		[]byte(nestedReparentSource),
		[]byte(nestedReparentDeployed),
		"nested-reparent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "ContextExtension")
	if gap == nil {
		t.Fatal("expected GapInjectionReparented for ContextExtension (moved from Identity to inside HarnessConstraints); " +
			"re-parenting detection must walk nested injections inside deployed regions (AD-E)")
	}
}

// TestInjectionReparented_NestedInsideDeployed_SameParent_NoGap verifies that when an
// injection nested inside a [[DEPLOYED:]] region in the source was also nested inside the
// same deployed region in the deployed file, no re-parenting gap is emitted. The parent
// is the same in both documents.
func TestInjectionReparented_NestedInsideDeployed_SameParent_NoGap(t *testing.T) {
	// Both source and deployed have ContextExtension nested inside HarnessConstraints.
	req := reorderReq(t,
		[]byte(nestedSameParentSource),
		[]byte(nestedSameParentDeployed),
		"nested-same-parent-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "ContextExtension")
	if gap != nil {
		t.Errorf("GapInjectionReparented emitted for ContextExtension whose parent HarnessConstraints is unchanged "+
			"in both source and deployed; the gap must fire only when the parent changes: detail=%s", gap.Detail)
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Top-level ↔ nested transitions
// ---------------------------------------------------------------------------

// TestInjectionReparented_TopLevelToNested_EmitsGap verifies that moving an injection from
// body top level (anchor parent = "") to inside a section is genuine re-parenting and emits
// the advisory. The empty-string parent must not be silently treated as equal to any name.
func TestInjectionReparented_TopLevelToNested_EmitsGap(t *testing.T) {
	// topLevelToNestedDeployed: TopLevelInjection at body top level (parent: "").
	// topLevelToNestedSource: TopLevelInjection inside Identity section (parent: "Identity").
	req := reorderReq(t,
		[]byte(topLevelToNestedSource),
		[]byte(topLevelToNestedDeployed),
		"toplevel-to-nested-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "TopLevelInjection")
	if gap == nil {
		t.Fatal("expected GapInjectionReparented for TopLevelInjection (moved from top level to Identity); " +
			"top-level-to-nested is genuine re-parenting and must emit the advisory")
	}
}

// TestInjectionReparented_NestedToTopLevel_EmitsGap verifies that moving an injection from
// inside a section to body top level (anchor parent = "") is genuine re-parenting and emits
// the advisory. Both directions must be detected.
func TestInjectionReparented_NestedToTopLevel_EmitsGap(t *testing.T) {
	// nestedToTopLevelSource: TopLevelInjection at body top level (parent: "").
	// nestedToTopLevelDeployed: TopLevelInjection inside Identity section (parent: "Identity").
	req := reorderReq(t,
		[]byte(nestedToTopLevelSource),
		[]byte(nestedToTopLevelDeployed),
		"nested-to-toplevel-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findReparentGap(result.Report.Gaps, "TopLevelInjection")
	if gap == nil {
		t.Fatal("expected GapInjectionReparented for TopLevelInjection (moved from Identity to body top level); " +
			"nested-to-top-level is genuine re-parenting and must emit the advisory")
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Rename table: resolved (post-rename) name, same parent → no gap (AD-C)
// ---------------------------------------------------------------------------

// TestInjectionReparented_RenamedInjection_SameParent_NoGap verifies that an injection
// renamed through the rename table and remaining in the same parent section does not
// produce a re-parenting gap. Comparison is keyed on the resolved name, so a rename alone
// in the same section compares equal and must not trip the check (AD-C).
func TestInjectionReparented_RenamedInjection_SameParent_NoGap(t *testing.T) {
	// Source has NewName inside Identity. Deployed has OldName inside Identity.
	// Rename table maps OldName → NewName. After resolution both have parent "Identity" → no gap.
	const renamedSource = `---
id: 910
version: 2.0.0
name: rename-same-parent-test
description: Agent for rename-same-parent negative tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: rename same parent testing
required_skills: []
---

[[SECTION:Identity]]
# RenameSameParent Agent

You are the RenameSameParent agent.

[[INJECTION:NewName]]
[[/INJECTION:NewName]]
[[/SECTION:Identity]]
`
	const renamedDeployed = `---
id: 910
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for rename-same-parent negative tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# RenameSameParent Agent

You are the RenameSameParent agent.

[[INJECTION:OldName]]
Saved injection content for OldName, which maps to NewName.
[[/INJECTION:OldName]]
[[/SECTION:Identity]]
`

	req := transform.Request{
		Source:   []byte(renamedSource),
		Deployed: []byte(renamedDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "rename-same-parent-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Timestamp: "2026-08-12T14:25:33Z",
		InjectionRenames: []transform.RenameEntry{
			{Old: "OldName", New: "NewName"},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// NewName (the resolved name) must not produce a GapInjectionReparented.
	gap := findReparentGap(result.Report.Gaps, "NewName")
	if gap != nil {
		t.Errorf("GapInjectionReparented emitted for NewName after rename from OldName, both in Identity; "+
			"a rename alone in the same parent must not trip the re-parenting check (AD-C): detail=%s", gap.Detail)
	}
	// OldName must also not produce a gap (it was the pre-rename name, now migrated).
	gap = findReparentGap(result.Report.Gaps, "OldName")
	if gap != nil {
		t.Errorf("GapInjectionReparented emitted for OldName (the pre-rename name); "+
			"resolved-name comparison must key on NewName only: detail=%s", gap.Detail)
	}
}

// ---------------------------------------------------------------------------
// InjectionReparentedDetail — pure formatting function tests
// ---------------------------------------------------------------------------

// TestInjectionReparentedDetail_WithAllParams verifies that InjectionReparentedDetail
// produces detail text naming the injection, both parents, and the timestamp when all
// parameters are supplied.
func TestInjectionReparentedDetail_WithAllParams(t *testing.T) {
	detail := transform.InjectionReparentedDetail("IdentityExtension", "Identity", "Capabilities", "2026-08-12T14:25:33Z")

	if !strings.Contains(detail, "IdentityExtension") {
		t.Errorf("detail does not contain injection name %q: %s", "IdentityExtension", detail)
	}
	if !strings.Contains(detail, "Identity") {
		t.Errorf("detail does not contain old parent name %q: %s", "Identity", detail)
	}
	if !strings.Contains(detail, "Capabilities") {
		t.Errorf("detail does not contain new parent name %q: %s", "Capabilities", detail)
	}
	if !strings.Contains(detail, "2026-08-12T14:25:33Z") {
		t.Errorf("detail does not contain timestamp %q: %s", "2026-08-12T14:25:33Z", detail)
	}
	if !strings.Contains(detail, "preserved") {
		t.Errorf("detail does not state that content was preserved: %s", detail)
	}
}

// TestInjectionReparentedDetail_EmptyTimestamp_OmitsClause verifies that when the
// timestamp is empty, the "(detected …)" clause is omitted. The rest of the message must
// still be complete.
func TestInjectionReparentedDetail_EmptyTimestamp_OmitsClause(t *testing.T) {
	detail := transform.InjectionReparentedDetail("IdentityExtension", "Identity", "Capabilities", "")

	if !strings.Contains(detail, "IdentityExtension") {
		t.Errorf("detail does not contain injection name with empty timestamp: %s", detail)
	}
	if strings.Contains(detail, "(detected") {
		t.Errorf("detail contains a timestamp clause when timestamp is empty; the clause must be omitted: %s", detail)
	}
}

// TestInjectionReparentedDetail_EmptyOldParent_RendersAsTopLevel verifies that an empty
// oldParent renders as the literal "top level" in the detail text. An injection that
// previously had no parent was at document top level, and this must be stated explicitly.
func TestInjectionReparentedDetail_EmptyOldParent_RendersAsTopLevel(t *testing.T) {
	detail := transform.InjectionReparentedDetail("MyInjection", "", "Capabilities", "")

	if !strings.Contains(detail, "top level") {
		t.Errorf("empty oldParent must render as %q in detail, but detail does not contain it: %s",
			"top level", detail)
	}
}

// TestInjectionReparentedDetail_EmptyNewParent_RendersAsTopLevel verifies that an empty
// newParent renders as the literal "top level" in the detail text.
func TestInjectionReparentedDetail_EmptyNewParent_RendersAsTopLevel(t *testing.T) {
	detail := transform.InjectionReparentedDetail("MyInjection", "Identity", "", "")

	if !strings.Contains(detail, "top level") {
		t.Errorf("empty newParent must render as %q in detail, but detail does not contain it: %s",
			"top level", detail)
	}
}

// TestInjectionReparentedDetail_BothParentsEmpty_RendersTwoTopLevel verifies that both
// empty parent names render as "top level" (identity-move edge case; in practice this
// pair would never produce a gap, but the formatter must not panic or produce empty text).
func TestInjectionReparentedDetail_BothParentsEmpty_RendersTwoTopLevel(t *testing.T) {
	detail := transform.InjectionReparentedDetail("MyInjection", "", "", "")

	if !strings.Contains(detail, "top level") {
		t.Errorf("empty parents must render as 'top level'; detail: %s", detail)
	}
	if !strings.Contains(detail, "MyInjection") {
		t.Errorf("detail must contain the injection name even when both parents are empty; detail: %s", detail)
	}
}

// TestInjectionReparentedDetail_DifferentTimestamps_ProduceDifferentOutputs verifies that
// two calls with identical parameters except for the timestamp produce different output.
// The timestamp must be visible in the output.
func TestInjectionReparentedDetail_DifferentTimestamps_ProduceDifferentOutputs(t *testing.T) {
	ts1 := "2026-08-12T14:25:33Z"
	ts2 := "2026-08-12T18:00:00Z"

	d1 := transform.InjectionReparentedDetail("MyInjection", "Identity", "Capabilities", ts1)
	d2 := transform.InjectionReparentedDetail("MyInjection", "Identity", "Capabilities", ts2)

	if !strings.Contains(d1, ts1) {
		t.Errorf("detail with timestamp %q does not contain the timestamp: %s", ts1, d1)
	}
	if !strings.Contains(d2, ts2) {
		t.Errorf("detail with timestamp %q does not contain the timestamp: %s", ts2, d2)
	}
	if d1 == d2 {
		t.Errorf("InjectionReparentedDetail produced identical output for different timestamps; "+
			"the timestamp must be reflected in the detail:\nd1: %s\nd2: %s", d1, d2)
	}
}

// TestInjectionReparentedDetail_NoTimestamp_EndsWithPeriod verifies that when the timestamp
// is empty, the detail text ends with the period after the main message body (no dangling
// space or parenthesis from the omitted clause).
func TestInjectionReparentedDetail_NoTimestamp_EndsWithPeriod(t *testing.T) {
	detail := transform.InjectionReparentedDetail("MyInjection", "Identity", "Capabilities", "")

	if len(detail) == 0 {
		t.Fatal("InjectionReparentedDetail returned empty string; must return non-empty detail text")
	}
	// The text must not end with a space when the timestamp clause is absent.
	if detail[len(detail)-1] == ' ' {
		t.Errorf("InjectionReparentedDetail with empty timestamp ends with a trailing space; detail: %q", detail)
	}
}

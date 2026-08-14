package transform_test

// region_merge_test.go covers user-owned region merging (T3.2):
//
//   - project-injection region content is emptied with a GapEmptyInjection gap on a new deployment.
//   - project-injection region content is lifted byte-identically from the deployed file on update.
//   - A project-injection region region that is new in the source (absent from deployed) starts
//     empty with action RegionAdded.
//   - RegionOutcome carries Marker == NodeInjection and ToolManaged() == false for every
//     user-owned region.
//
// The test documents in this file use the new canonical marker vocabulary: user-owned regions
// are declared with project-injection region and tool-managed regions with managed region. The mix of
// both kinds in a single document exercises the new routing logic introduced in Stage 3.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// mergeSourceNewVocab is a source document using the Stage 3 marker vocabulary:
//   - <HarnessConstraints type="managed"> — tool-managed, filled from harness
//   - <IdentityExtension type="project"> — user-owned, preserved on update
//   - <CodebaseContext type="project">  — user-owned, preserved on update
const mergeSourceNewVocab = `---
id: 40
version: 3.0.0
name: merge-new-vocab-test
description: Agent for testing user-owned region merging in Stage 3 vocabulary
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: merge new vocab testing
required_skills: []
---

<Identity type="core">
# MergeNewVocabTest Agent

You are the MergeNewVocabTest agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>

<Constraints type="core">
## Constraints

Always use the approved tool list.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// mergeDeployedNewVocabFilled is a deployed file (Stage 3 vocabulary) with user content in
// the project-injection region regions and harness content in the managed region region. The harness
// content must be regenerated on update, not lifted. The injection content must survive.
const mergeDeployedNewVocabFilled = `---
id: 40
version: 3.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for testing user-owned region merging in Stage 3 vocabulary
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# MergeNewVocabTest Agent

You are the MergeNewVocabTest agent.

<IdentityExtension type="project">
User identity extension: Go project, hexagonal architecture.
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
User codebase context: mosaic-deploy module, domain-driven design.
Three lines of context.
</CodebaseContext>
</Capabilities>

<Constraints type="core">
## Constraints

Always use the approved tool list.

<HarnessConstraints type="managed">
Old harness content in the deployed file — must be regenerated, not lifted.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// T3.2: User-owned injection is empty with a gap on new deployment
// ---------------------------------------------------------------------------

// TestUserOwnedRegion_EmptyWithGapOnCreate asserts that when Deployed is nil (new
// deployment), every project-injection region region in the source is left empty in the output and a
// GapEmptyInjection gap is emitted for it. The corresponding RegionOutcome must have:
//   - Marker == NodeInjection
//   - ToolManaged() == false
//   - Action == RegionEmptied
func TestUserOwnedRegion_EmptyWithGapOnCreate(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceNewVocab),
		Deployed: nil, // new deployment
		Kind:     domain.ArtifactAgent,
		Key:      "merge-new-vocab-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	userOwnedNames := []string{"IdentityExtension", "CodebaseContext"}

	// Build a set of GapEmptyInjection subjects.
	emptyGaps := make(map[string]bool)
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEmptyInjection {
			emptyGaps[g.Subject] = true
		}
	}

	for _, name := range userOwnedNames {
		name := name
		t.Run(name, func(t *testing.T) {
			// The injection node must be empty (whitespace-only) in the output.
			node, ok := outDoc.Body().Injection(name)
			if !ok {
				t.Fatalf("%s injection absent from output document", name)
			}
			if !node.IsEmpty() {
				t.Errorf("%s injection must be empty on new deployment, got content: %q",
					name, node.Content())
			}

			// A GapEmptyInjection gap must be emitted.
			if !emptyGaps[name] {
				t.Errorf("expected GapEmptyInjection for %q on new deployment; gaps: %v",
					name, result.Report.Gaps)
			}

			// Find the outcome in Report.Regions (not Report.Injections).
			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == name {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for %q absent from Report.Regions", name)
			}
			if outcome.Marker != docformat.NodeInjection {
				t.Errorf("%s Marker: want %q, got %q", name, docformat.NodeInjection, outcome.Marker)
			}
			if outcome.ToolManaged() {
				t.Errorf("%s ToolManaged() must be false for a project-injection region region", name)
			}
			if outcome.Action != transform.RegionEmptied {
				t.Errorf("%s Action: want %q, got %q", name, transform.RegionEmptied, outcome.Action)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.2: User-owned injection content is lifted byte-identically on update
// ---------------------------------------------------------------------------

// TestUserOwnedRegion_ContentLiftedByteIdenticallyOnUpdate asserts that when Deployed is
// non-nil (update), project-injection region region content is carried verbatim from the deployed file.
// The content must be byte-identical — no trimming, normalisation, or re-encoding.
// The corresponding RegionOutcome must have Action == RegionPreserved.
func TestUserOwnedRegion_ContentLiftedByteIdenticallyOnUpdate(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceNewVocab),
		Deployed: []byte(mergeDeployedNewVocabFilled),
		Kind:     domain.ArtifactAgent,
		Key:      "merge-new-vocab-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(mergeDeployedNewVocabFilled))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	cases := []string{"IdentityExtension", "CodebaseContext"}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			depNode, depOK := depDoc.Body().Injection(name)
			if !depOK {
				t.Fatalf("%s injection absent from deployed fixture", name)
			}
			outNode, outOK := outDoc.Body().Injection(name)
			if !outOK {
				t.Fatalf("%s injection absent from output", name)
			}

			// Content must be byte-identical.
			if !bytes.Equal(outNode.Content(), depNode.Content()) {
				t.Errorf("%s content not byte-identical to deployed:\ndeployed: %q\noutput:   %q",
					name, depNode.Content(), outNode.Content())
			}

			// Outcome must be RegionPreserved.
			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == name {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for %q absent from Report.Regions", name)
			}
			if outcome.Action != transform.RegionPreserved {
				t.Errorf("%s Action: want %q, got %q", name, transform.RegionPreserved, outcome.Action)
			}
			if outcome.Marker != docformat.NodeInjection {
				t.Errorf("%s Marker: want %q, got %q", name, docformat.NodeInjection, outcome.Marker)
			}
			if outcome.ToolManaged() {
				t.Errorf("%s ToolManaged() must be false", name)
			}
			if outcome.Bytes != len(depNode.Content()) {
				t.Errorf("%s RegionOutcome.Bytes: want %d, got %d",
					name, len(depNode.Content()), outcome.Bytes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.2: New injection point in source starts empty with RegionAdded action
// ---------------------------------------------------------------------------

// sourceWithNewInjection adds ErrorHandlingExtension to mergeSourceNewVocab. This injection
// point does not exist in mergeDeployedNewVocabFilled, so it must start empty.
const sourceWithNewInjection = `---
id: 40
version: 4.0.0
name: merge-new-vocab-test
description: Agent for testing added injection in Stage 3 vocabulary
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: added injection testing
required_skills: []
---

<Identity type="core">
# MergeNewVocabTest Agent

You are the MergeNewVocabTest agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>

<ErrorHandling type="core">
## Error Handling

<ErrorHandlingExtension type="project">
</ErrorHandlingExtension>
</ErrorHandling>

<Constraints type="core">
## Constraints

Always use the approved tool list.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// TestUserOwnedRegion_NewInSourceStartsEmpty asserts that when a project-injection region region is
// present in the source but absent from the deployed file, it starts empty in the output
// and the RegionOutcome has Action == RegionAdded.
func TestUserOwnedRegion_NewInSourceStartsEmpty(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithNewInjection),
		Deployed: []byte(mergeDeployedNewVocabFilled), // does not have ErrorHandlingExtension
		Kind:     domain.ArtifactAgent,
		Key:      "merge-new-vocab-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// ErrorHandlingExtension is new in the source; the deployed file does not have it.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("ErrorHandlingExtension")
	if !ok {
		t.Fatal("ErrorHandlingExtension injection absent from output")
	}
	if !node.IsEmpty() {
		t.Errorf("ErrorHandlingExtension should be empty (new in source), got content: %q", node.Content())
	}

	// Outcome must be RegionAdded.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "ErrorHandlingExtension" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for ErrorHandlingExtension absent from Report.Regions")
	}
	if outcome.Action != transform.RegionAdded {
		t.Errorf("ErrorHandlingExtension Action: want %q, got %q", transform.RegionAdded, outcome.Action)
	}
	if outcome.Marker != docformat.NodeInjection {
		t.Errorf("ErrorHandlingExtension Marker: want %q, got %q", docformat.NodeInjection, outcome.Marker)
	}

	// Pre-existing injection must still be lifted from the deployed file.
	extNode, extOK := outDoc.Body().Injection("IdentityExtension")
	if !extOK {
		t.Fatal("IdentityExtension absent from output")
	}
	if !bytes.Contains(extNode.Content(), []byte("User identity extension")) {
		t.Errorf("IdentityExtension content lost during update with new injection: %q", extNode.Content())
	}
}

// ---------------------------------------------------------------------------
// T3.2: User-owned outcome carries Marker == NodeInjection
// ---------------------------------------------------------------------------

// TestUserOwnedRegion_OutcomeMarkerIsNodeInjection asserts that every RegionOutcome in
// Report.Regions for a user-owned project-injection region region has Marker == NodeInjection and
// ToolManaged() returns false.
func TestUserOwnedRegion_OutcomeMarkerIsNodeInjection(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceNewVocab),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "merge-new-vocab-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	userOwned := map[string]bool{
		"IdentityExtension": true,
		"CodebaseContext":   true,
	}

	for _, outcome := range result.Report.Regions {
		if !userOwned[outcome.Name] {
			continue
		}
		if outcome.Marker != docformat.NodeInjection {
			t.Errorf("user-owned region %q: Marker want %q, got %q",
				outcome.Name, docformat.NodeInjection, outcome.Marker)
		}
		if outcome.ToolManaged() {
			t.Errorf("user-owned region %q: ToolManaged() must be false", outcome.Name)
		}
	}

	// Both user-owned names must appear in the outcomes.
	seen := make(map[string]bool, len(userOwned))
	for _, outcome := range result.Report.Regions {
		if userOwned[outcome.Name] {
			seen[outcome.Name] = true
		}
	}
	for name := range userOwned {
		if !seen[name] {
			t.Errorf("user-owned region %q absent from Report.Regions", name)
		}
	}
}

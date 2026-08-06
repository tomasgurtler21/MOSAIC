package transform_test

// region_body_test.go covers body preservation and determinism across documents that mix
// all three marker kinds (T3.5):
//
//   - Body bytes outside ALL managed regions (both [[INJECTION:]] and [[DEPLOYED:]]) remain
//     byte-identical to the source across every scenario.
//   - [[SECTION:]] structure bytes are preserved byte-identically.
//   - Repeated transforms of identical inputs produce byte-identical output (determinism).
//   - Transforms of distinct agents do not cross-contaminate.
//
// These tests use the Stage 3 canonical vocabulary where tool-managed names use [[DEPLOYED:]]
// and user-owned names use [[INJECTION:]]. The helper stripManagedRegionContent strips content
// from both kinds so prose comparisons are independent of region content.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// mixedMarkerSource is a document that contains all three boundary kinds:
//   - [[SECTION:]] — structural sections
//   - [[INJECTION:IdentityExtension]] — user-owned region
//   - [[INJECTION:CodebaseContext]]   — user-owned region
//   - [[DEPLOYED:HarnessConstraints]] — tool-managed region (harness class)
//
// The body prose inside sections but outside managed regions must survive byte-for-byte.
const mixedMarkerSource = `---
id: 70
version: 1.0.0
name: mixed-markers-test
description: Agent for testing body preservation across all three marker kinds
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: mixed marker testing
required_skills: []
---

[[SECTION:Identity]]
# MixedMarkersTest Agent

You are the MixedMarkersTest agent.

This prose must survive byte-for-byte.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

More prose after the injection.
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

Capability prose that must survive.

[[INJECTION:CodebaseContext]]
[[/INJECTION:CodebaseContext]]

Further capability prose that must survive.
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints

Constraint prose that must survive.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

Final constraint prose.
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// T3.5: Body bytes outside managed regions are byte-identical to source
// ---------------------------------------------------------------------------

// TestAllMarkerKinds_BodyBytesPreservedOutsideRegions asserts that bytes outside ALL managed
// regions — both [[INJECTION:]] and [[DEPLOYED:]] — are byte-identical to the source. Only
// region content is allowed to differ; structural boundary tags, section prose, and all
// whitespace between elements must be unchanged.
func TestAllMarkerKinds_BodyBytesPreservedOutsideRegions(t *testing.T) {
	scenarios := []struct {
		name     string
		source   string
		deployed string // empty means nil Deployed (new deployment)
	}{
		{
			name:   "new deployment — all three marker kinds",
			source: mixedMarkerSource,
		},
		{
			name:     "update — content lifted and regenerated across both marker kinds",
			source:   mixedMarkerSource,
			deployed: mixedMarkerDeployedFilled,
		},
	}

	for _, s := range scenarios {
		s := s
		t.Run(s.name, func(t *testing.T) {
			var deployed []byte
			if s.deployed != "" {
				deployed = []byte(s.deployed)
			}

			req := transform.Request{
				Source:   []byte(s.source),
				Deployed: deployed,
				Kind:     domain.ArtifactAgent,
				Key:      "mixed-markers-test",
				Module:   newInjectionFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			// Extract body bytes from source and output.
			_, srcBody, err := docformat.SplitFrontmatter([]byte(s.source))
			if err != nil {
				t.Fatalf("SplitFrontmatter (source): %v", err)
			}
			_, outBody, err := docformat.SplitFrontmatter(result.Output)
			if err != nil {
				t.Fatalf("SplitFrontmatter (output): %v", err)
			}

			// Strip content from both [[INJECTION:]] and [[DEPLOYED:]] regions and compare.
			// The stripped form must be byte-identical: only region content may differ.
			strippedSrc := stripManagedRegionContent(srcBody)
			strippedOut := stripManagedRegionContent(outBody)

			if !bytes.Equal(strippedSrc, strippedOut) {
				t.Errorf("body prose outside managed regions differs from source:\n"+
					"source (%d bytes stripped):\n%q\n\noutput (%d bytes stripped):\n%q",
					len(strippedSrc), truncateBytes(strippedSrc, 500),
					len(strippedOut), truncateBytes(strippedOut, 500))
			}
		})
	}
}

// mixedMarkerDeployedFilled is a deployed version of mixedMarkerSource with user content in
// the injection regions and harness content in the deployed region.
const mixedMarkerDeployedFilled = `---
id: 70
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for testing body preservation across all three marker kinds
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# MixedMarkersTest Agent

You are the MixedMarkersTest agent.

This prose must survive byte-for-byte.

[[INJECTION:IdentityExtension]]
Deployed user identity extension content.
[[/INJECTION:IdentityExtension]]

More prose after the injection.
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

Capability prose that must survive.

[[INJECTION:CodebaseContext]]
Deployed codebase context: Go monorepo.
[[/INJECTION:CodebaseContext]]

Further capability prose that must survive.
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints

Constraint prose that must survive.

[[DEPLOYED:HarnessConstraints]]
Previously deployed harness constraint content.
[[/DEPLOYED:HarnessConstraints]]

Final constraint prose.
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// T3.5: Tool-managed region in output contains regenerated content
// ---------------------------------------------------------------------------

// TestAllMarkerKinds_DeployedRegionFilledWithHarnessContent asserts that after applying to a
// mixed-marker source, the [[DEPLOYED:HarnessConstraints]] region in the output contains the
// harness module's declared content. This distinguishes a correct Stage 3 transform (which
// fills the region) from a no-op (which would leave it empty or unchanged).
func TestAllMarkerKinds_DeployedRegionFilledWithHarnessContent(t *testing.T) {
	mod := newInjectionFixtureModule(t)
	harnessContent, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok || harnessContent == "" {
		t.Fatalf("injection_fixture.yaml must declare HarnessConstraints content but does not")
	}

	req := transform.Request{
		Source:   []byte(mixedMarkerSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "mixed-markers-test",
		Module:   mod,
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

	// The [[DEPLOYED:HarnessConstraints]] region must contain the harness content.
	node, ok := outDoc.Body().Deployed("HarnessConstraints")
	if !ok {
		t.Fatal("HarnessConstraints deployed region absent from output")
	}
	if !bytes.Contains(node.Content(), []byte(harnessContent)) {
		t.Errorf("HarnessConstraints deployed region not filled with harness content\nwant (in): %q\ngot: %q",
			harnessContent, node.Content())
	}

	// The injections must still be empty on create.
	injNode, injOK := outDoc.Body().Injection("IdentityExtension")
	if !injOK {
		t.Fatal("IdentityExtension injection absent from output")
	}
	if !injNode.IsEmpty() {
		t.Errorf("IdentityExtension injection must be empty on new deployment, got: %q", injNode.Content())
	}
}

// ---------------------------------------------------------------------------
// T3.5: Repeated transforms of identical inputs are byte-identical
// ---------------------------------------------------------------------------

// TestAllMarkerKinds_RepeatedTransformIsDeterministic asserts that calling Apply twice with
// identical inputs over a mixed-marker document produces byte-identical outputs. This catches
// implementations that sort orphan names non-deterministically or accumulate state across calls.
func TestAllMarkerKinds_RepeatedTransformIsDeterministic(t *testing.T) {
	mod := newInjectionFixtureModule(t)

	makeReq := func() transform.Request {
		return transform.Request{
			Source:   []byte(mixedMarkerSource),
			Deployed: nil,
			Kind:     domain.ArtifactAgent,
			Key:      "mixed-markers-test",
			Module:   mod,
			Model:    fixtureModel(),
			Scope:    domain.ScopeProject,
		}
	}

	r1, err := transform.Apply(makeReq())
	if err != nil {
		t.Fatalf("Apply (first call): %v", err)
	}
	r2, err := transform.Apply(makeReq())
	if err != nil {
		t.Fatalf("Apply (second call): %v", err)
	}

	if !bytes.Equal(r1.Output, r2.Output) {
		diff := firstDiff(r1.Output, r2.Output)
		t.Errorf("Apply is non-deterministic over a mixed-marker document:\n"+
			"first call:  %d bytes\n"+
			"second call: %d bytes\n"+
			"first difference at byte %d\n"+
			"first call  (first 400 bytes): %q\n"+
			"second call (first 400 bytes): %q",
			len(r1.Output), len(r2.Output), diff,
			truncateBytes(r1.Output, 400), truncateBytes(r2.Output, 400))
	}
}

// TestAllMarkerKinds_RepeatedTransformReportIsDeterministic asserts that the Report for a
// mixed-marker transform is also deterministic: Region outcomes appear in the same order,
// with the same actions and markers, across repeated calls.
func TestAllMarkerKinds_RepeatedTransformReportIsDeterministic(t *testing.T) {
	mod := newInjectionFixtureModule(t)

	req := transform.Request{
		Source:   []byte(mixedMarkerSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "mixed-markers-test",
		Module:   mod,
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	r1, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (first call): %v", err)
	}
	r2, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (second call): %v", err)
	}

	// Report.Regions must have the same length and content on both calls.
	if len(r1.Report.Regions) != len(r2.Report.Regions) {
		t.Errorf("Report.Regions length differs: %d vs %d",
			len(r1.Report.Regions), len(r2.Report.Regions))
	} else {
		for i, o1 := range r1.Report.Regions {
			o2 := r2.Report.Regions[i]
			if o1.Name != o2.Name || o1.Marker != o2.Marker || o1.Action != o2.Action {
				t.Errorf("Report.Regions[%d] differs:\n  first:  %+v\n  second: %+v", i, o1, o2)
			}
		}
	}

	// OutputBytes must agree.
	if r1.Report.OutputBytes != r2.Report.OutputBytes {
		t.Errorf("Report.OutputBytes: %d vs %d", r1.Report.OutputBytes, r2.Report.OutputBytes)
	}
}

// ---------------------------------------------------------------------------
// T3.5: Report.Regions is document-ordered (source regions then orphans)
// ---------------------------------------------------------------------------

// TestAllMarkerKinds_RegionsReportDocumentOrdered asserts that Report.Regions lists source
// regions (both [[INJECTION:]] and [[DEPLOYED:]]) in document order, followed by any
// orphaned user-owned regions in sorted name order. This ordering makes the report a faithful
// audit trail that maps to the document's structure.
func TestAllMarkerKinds_RegionsReportDocumentOrdered(t *testing.T) {
	// deployedWithExtraInjection adds an injection (ErrorHandlingExtension) that is
	// absent from mixedMarkerSource, creating an orphan to appear at the end.
	const deployedWithExtraInjection = `---
id: 70
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for testing body preservation across all three marker kinds
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# MixedMarkersTest Agent

You are the MixedMarkersTest agent.

This prose must survive byte-for-byte.

[[INJECTION:IdentityExtension]]
Identity content.
[[/INJECTION:IdentityExtension]]

More prose after the injection.
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

Capability prose that must survive.

[[INJECTION:CodebaseContext]]
Context content.
[[/INJECTION:CodebaseContext]]

Further capability prose that must survive.
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints

Constraint prose that must survive.

[[DEPLOYED:HarnessConstraints]]
Old harness content.
[[/DEPLOYED:HarnessConstraints]]

Final constraint prose.
[[/SECTION:Constraints]]

[[SECTION:ErrorHandling]]
## ErrorHandling

[[INJECTION:ErrorHandlingExtension]]
Error handling extension content: will become orphan.
[[/INJECTION:ErrorHandlingExtension]]
[[/SECTION:ErrorHandling]]
`

	req := transform.Request{
		Source:   []byte(mixedMarkerSource), // does not have ErrorHandlingExtension
		Deployed: []byte(deployedWithExtraInjection),
		Kind:     domain.ArtifactAgent,
		Key:      "mixed-markers-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(result.Report.Regions) == 0 {
		t.Fatal("Report.Regions is empty; expected outcomes for source regions and the orphan")
	}

	// The last outcome must be the orphaned region (ErrorHandlingExtension), since
	// orphans are emitted after all source regions in sorted name order.
	last := result.Report.Regions[len(result.Report.Regions)-1]
	if last.Action != transform.RegionOrphaned {
		t.Errorf("last entry in Report.Regions must be an orphaned outcome; got Action=%q Name=%q",
			last.Action, last.Name)
	}
	if last.Name != "ErrorHandlingExtension" {
		t.Errorf("orphaned outcome name: want %q, got %q", "ErrorHandlingExtension", last.Name)
	}
}

// ---------------------------------------------------------------------------
// Helper: stripManagedRegionContent
// ---------------------------------------------------------------------------

// stripManagedRegionContent replaces the content between every [[INJECTION:...]] and
// [[DEPLOYED:...]] pair with a fixed placeholder, so the surrounding prose can be compared
// independently of region content. [[SECTION:]] tags and inter-region prose are preserved.
func stripManagedRegionContent(body []byte) []byte {
	var out []byte
	lines := bytes.Split(body, []byte("\n"))
	insideManaged := false

	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)

		if !insideManaged {
			// Check for opening [[INJECTION:...]] or [[DEPLOYED:...]] tag.
			if (bytes.HasPrefix(trimmed, []byte("[[INJECTION:")) ||
				bytes.HasPrefix(trimmed, []byte("[[DEPLOYED:"))) &&
				!bytes.HasPrefix(trimmed, []byte("[[/INJECTION:")) &&
				!bytes.HasPrefix(trimmed, []byte("[[/DEPLOYED:")) {
				insideManaged = true
				out = append(out, line...)
				out = append(out, '\n')
				continue
			}
		} else {
			// Check for closing [[/INJECTION:...]] or [[/DEPLOYED:...]] tag.
			if bytes.HasPrefix(trimmed, []byte("[[/INJECTION:")) ||
				bytes.HasPrefix(trimmed, []byte("[[/DEPLOYED:")) {
				insideManaged = false
				out = append(out, line...)
				out = append(out, '\n')
				continue
			}
			// Skip content inside managed regions.
			continue
		}

		out = append(out, line...)
		out = append(out, '\n')
	}

	// Remove the trailing newline added by the final iteration.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out
}

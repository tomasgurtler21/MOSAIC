package transform_test

// provenance_retirement_test.go verifies that the provenance retirement (Stage 5) is
// complete from the transform layer's perspective. These are regression tests: after
// retirement, no transform output must contain a provenance version comment or a
// provenance region, regardless of what was in the deployed file.
//
// Regression assertions (AC5.4):
//   - Transform output for a standard agent source contains no "<!-- provenance-version:" comment.
//   - Transform output contains no <ArtifactProvenance type="managed"> region.
//   - The transform report contains no region outcome named "ArtifactProvenance".

import (
	"bytes"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// T5.4 — Transform output contains no provenance region or version comment
// ---------------------------------------------------------------------------

// sourceWithoutProvenanceRegion is a minimal agent source file that does not declare
// <ArtifactProvenance type="managed">. This is the target state of all migrated agent source
// files: the provenance region has been removed, so the transform has nothing to fill.
const sourceWithoutProvenanceRegion = `---
id: 55
version: 1.0.0
name: provenance-retirement-test
description: Agent for provenance retirement regression tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Identity type="core">
## Identity

Provenance retirement regression test agent.

<IdentityExtension type="project">
</IdentityExtension>

</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Capabilities type="core">
## Capabilities

Agent capabilities.

</Capabilities>
`

// TestProvenanceRetirement_TransformOutput_NoProvenanceVersionComment verifies that
// transform.Apply produces output containing no provenance version comment for a
// migrated agent source file (one that no longer declares <ArtifactProvenance type="managed">).
// The comment format "<!-- provenance-version: X -->" must not appear anywhere in the
// output after the provenance system is retired.
func TestProvenanceRetirement_TransformOutput_NoProvenanceVersionComment(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutProvenanceRegion),
		Kind:     domain.ArtifactAgent,
		Key:      "provenance-retirement-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The provenance version comment must not appear anywhere in the output.
	// This pattern covers "<!-- provenance-version: X -->" in any form.
	if bytes.Contains(result.Output, []byte("provenance-version:")) {
		t.Errorf("transform output contains a provenance version comment; "+
			"the transform must not write any provenance version comment after retirement.\n"+
			"Output snippet containing the comment:\n%s",
			extractContext(result.Output, []byte("provenance-version:"), 120))
	}
}

// TestProvenanceRetirement_TransformOutput_NoProvenanceRegion verifies that
// transform.Apply produces output containing no <ArtifactProvenance type="managed"> region
// for a migrated agent source file. After retirement, no agent source declares this
// region, so the transform output must not contain it either.
func TestProvenanceRetirement_TransformOutput_NoProvenanceRegion(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutProvenanceRegion),
		Kind:     domain.ArtifactAgent,
		Key:      "provenance-retirement-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Neither the opening nor the closing provenance region tag must appear in the output.
	if bytes.Contains(result.Output, []byte("<ArtifactProvenance type=\"managed\">")) {
		t.Errorf("transform output contains <ArtifactProvenance type=\"managed\"> opening tag; "+
			"the provenance region must not appear in transform output after retirement")
	}
	if bytes.Contains(result.Output, []byte("</ArtifactProvenance>")) {
		t.Errorf("transform output contains </ArtifactProvenance> closing tag; "+
			"the provenance region must not appear in transform output after retirement")
	}
}

// TestProvenanceRetirement_TransformReport_NoProvenanceRegionOutcome verifies that
// transform.Apply produces a report with no RegionOutcome named "ArtifactProvenance"
// when transforming a migrated source that does not declare the provenance region.
// A region outcome for a region absent from the source would indicate a phantom fill.
func TestProvenanceRetirement_TransformReport_NoProvenanceRegionOutcome(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutProvenanceRegion),
		Kind:     domain.ArtifactAgent,
		Key:      "provenance-retirement-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, r := range result.Report.Regions {
		if r.Name == "ArtifactProvenance" {
			t.Errorf("Report.Regions contains an ArtifactProvenance outcome with Action=%q; "+
				"a source that does not declare the provenance region must produce no such outcome",
				r.Action)
		}
	}
}

// TestProvenanceRetirement_TransformOutput_AppliesSuccessfully verifies that a fully
// migrated agent source (no <ArtifactProvenance type="managed">, no Provenance field in
// the request) transforms without error. This is the baseline for AC5.4: the transform
// must succeed for any valid source that does not use provenance.
func TestProvenanceRetirement_TransformOutput_AppliesSuccessfully(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutProvenanceRegion),
		Kind:     domain.ArtifactAgent,
		Key:      "provenance-retirement-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Errorf("Apply returned unexpected error for a migrated source with no provenance region: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// extractContext returns up to n bytes of context surrounding the first occurrence of
// needle in haystack. Used for diagnostic output when an unexpected token is found.
func extractContext(haystack, needle []byte, n int) []byte {
	idx := bytes.Index(haystack, needle)
	if idx < 0 {
		return nil
	}
	start := idx - n/2
	if start < 0 {
		start = 0
	}
	end := idx + len(needle) + n/2
	if end > len(haystack) {
		end = len(haystack)
	}
	return haystack[start:end]
}

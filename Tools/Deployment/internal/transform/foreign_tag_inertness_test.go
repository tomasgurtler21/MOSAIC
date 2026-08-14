package transform_test

// foreign_tag_inertness_test.go proves that tags without a valid type attribute pass through
// the transform pipeline as inert prose rather than being interpreted as region boundaries.
//
// New-syntax region tags carry a type attribute with one of four recognised values:
// "core", "managed", "project", or "custom". A tag that is missing the type attribute
// entirely, or that carries an unrecognised type value, is not a region boundary and must
// appear verbatim in the transform output unchanged. No RegionOutcome for the foreign tag
// should appear in Report.Regions.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// foreignTagNoTypeSource is an agent document whose Capabilities section contains a tag
// with no type attribute at all. The tag should pass through transform as inert prose.
const foreignTagNoTypeSource = `---
version: 1.0.0
name: foreign-tag-no-type
description: Agent for foreign-tag (no type attr) inertness testing
model: {model-identifier}
tools: []
---

<Identity type="core">
## Identity

You are the ForeignTagNoType agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

Core capabilities of this agent.

<ForeignTag>
This content is inside a tag with no type attribute.
It must not be treated as a region boundary.
</ForeignTag>
</Capabilities>
`

// foreignTagUnknownTypeSource is an agent document whose Capabilities section contains a
// tag with an unrecognised type value ("alien"). The tag should pass through transform as
// inert prose.
const foreignTagUnknownTypeSource = `---
version: 1.0.0
name: foreign-tag-unknown-type
description: Agent for foreign-tag (unknown type) inertness testing
model: {model-identifier}
tools: []
---

<Identity type="core">
## Identity

You are the ForeignTagUnknownType agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

Core capabilities of this agent.

<UnrecognisedTag type="alien">
This content is inside a tag with an unrecognised type value.
It must not be treated as a region boundary.
</UnrecognisedTag>
</Capabilities>
`

// ---------------------------------------------------------------------------
// T13.5a: tag without any type attribute
// ---------------------------------------------------------------------------

// TestForeignTag_NoTypeAttribute_ApplyDoesNotError asserts that transform.Apply succeeds
// on a source document that contains a tag with no type attribute. A foreign tag is inert
// prose and must not cause parsing errors or transform failures.
func TestForeignTag_NoTypeAttribute_ApplyDoesNotError(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagNoTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-no-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply must not error on a source containing a typeless tag; got: %v", err)
	}
}

// TestForeignTag_NoTypeAttribute_TagAppearsVerbatimInOutput asserts that the typeless tag
// lines are preserved verbatim in the output of transform.Apply. The tag must not be
// consumed or replaced by any region processing step.
func TestForeignTag_NoTypeAttribute_TagAppearsVerbatimInOutput(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagNoTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-no-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if !bytes.Contains(result.Output, []byte("<ForeignTag>")) {
		t.Errorf("output must contain verbatim <ForeignTag> open line; not found in:\n%s", result.Output)
	}
	if !bytes.Contains(result.Output, []byte("</ForeignTag>")) {
		t.Errorf("output must contain verbatim </ForeignTag> close line; not found in:\n%s", result.Output)
	}
	if !bytes.Contains(result.Output, []byte("This content is inside a tag with no type attribute.")) {
		t.Errorf("output must contain the prose inside the typeless tag; not found in:\n%s", result.Output)
	}
}

// TestForeignTag_NoTypeAttribute_NotReportedAsRegion asserts that no RegionOutcome with
// Name "ForeignTag" appears in Report.Regions. A typeless tag is not a region boundary and
// must not be processed as one.
func TestForeignTag_NoTypeAttribute_NotReportedAsRegion(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagNoTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-no-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, outcome := range result.Report.Regions {
		if outcome.Name == "ForeignTag" {
			t.Errorf("Report.Regions must not contain an entry for the typeless <ForeignTag>; got: %+v", outcome)
		}
	}
}

// TestForeignTag_NoTypeAttribute_OutputParseSucceeds asserts that the output of
// transform.Apply on a source containing a typeless tag is parseable by docformat.Parse
// without error, proving that the transform did not corrupt the document structure.
func TestForeignTag_NoTypeAttribute_OutputParseSucceeds(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagNoTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-no-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := docformat.Parse(result.Output); err != nil {
		t.Errorf("docformat.Parse must succeed on transform output containing a typeless tag; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T13.5b: tag with an unrecognised type value
// ---------------------------------------------------------------------------

// TestForeignTag_UnrecognisedTypeValue_ApplyDoesNotError asserts that transform.Apply
// succeeds on a source document that contains a tag with an unrecognised type value
// ("alien"). The tag is inert prose and must not cause parsing errors or transform failures.
func TestForeignTag_UnrecognisedTypeValue_ApplyDoesNotError(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagUnknownTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-unknown-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply must not error on a source containing a tag with an unrecognised type; got: %v", err)
	}
}

// TestForeignTag_UnrecognisedTypeValue_TagAppearsVerbatimInOutput asserts that the tag
// lines with an unrecognised type value are preserved verbatim in the transform output.
// The tag must not be consumed or replaced by any region processing step.
func TestForeignTag_UnrecognisedTypeValue_TagAppearsVerbatimInOutput(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagUnknownTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-unknown-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if !bytes.Contains(result.Output, []byte(`<UnrecognisedTag type="alien">`)) {
		t.Errorf("output must contain verbatim <UnrecognisedTag type=\"alien\"> open line; not found in:\n%s", result.Output)
	}
	if !bytes.Contains(result.Output, []byte("</UnrecognisedTag>")) {
		t.Errorf("output must contain verbatim </UnrecognisedTag> close line; not found in:\n%s", result.Output)
	}
	if !bytes.Contains(result.Output, []byte("This content is inside a tag with an unrecognised type value.")) {
		t.Errorf("output must contain the prose inside the unknown-type tag; not found in:\n%s", result.Output)
	}
}

// TestForeignTag_UnrecognisedTypeValue_NotReportedAsRegion asserts that no RegionOutcome
// with Name "UnrecognisedTag" appears in Report.Regions. A tag with an unrecognised type
// value is not a region boundary and must not be processed as one.
func TestForeignTag_UnrecognisedTypeValue_NotReportedAsRegion(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagUnknownTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-unknown-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, outcome := range result.Report.Regions {
		if outcome.Name == "UnrecognisedTag" {
			t.Errorf("Report.Regions must not contain an entry for <UnrecognisedTag type=\"alien\">; got: %+v", outcome)
		}
	}
}

// TestForeignTag_UnrecognisedTypeValue_OutputParseSucceeds asserts that the output of
// transform.Apply on a source containing a tag with an unrecognised type value is
// parseable by docformat.Parse without error.
func TestForeignTag_UnrecognisedTypeValue_OutputParseSucceeds(t *testing.T) {
	req := transform.Request{
		Source:   []byte(foreignTagUnknownTypeSource),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "foreign-tag-unknown-type",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := docformat.Parse(result.Output); err != nil {
		t.Errorf("docformat.Parse must succeed on transform output containing an unknown-type tag; got: %v", err)
	}
}

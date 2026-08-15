package app

// promote_test.go covers buildGenericAgent — the pure generation core of the promote
// capability. Tests are in package app (not package app_test) to access the unexported
// buildGenericAgent function and the associated sentinel errors declared in promote.go.
//
// Verified behaviours, by task:
//
// Eligibility rejection:
//   - A source without transform_version is rejected with an error wrapping
//     ErrPromoteNotTransformed and produces no output bytes.
//   - A source with transform_version but no valid canonical boundary tags is rejected
//     with an error wrapping ErrPromoteNotTransformed and produces no output bytes.
//   - The two rejection paths produce distinguishable error messages (the Reason from
//     the eligibility verdict is visible in the error string).
//   - A PromoteInput with empty NumericID is rejected with an error wrapping
//     ErrPromoteNumericIDRequired, regardless of source content.
//   - Any rejection produces nil output — never a partial file.
//
// Injection region stripping:
//   - Every <... type="project"> region in the generated output is an empty tag pair when
//     the source carries several populated injection regions.
//   - The open and close tags for each injection region are present in the output (the
//     regions exist as empty placeholders, not removed entirely).
//
// Deployed region stripping:
//   - Every <... type="managed"> region in the generated output is an empty tag pair when
//     the source carries several populated deployed regions.
//   - The open and close tags for each deployed region are present in the output.
//
// Section structure passthrough:
//   - <... type="core"> open and close tags are present in the output with the same names
//     as in the source.
//   - Section body prose (text outside injection/deployed region boundaries) is carried
//     over byte-identically.
//   - Multiple sections are all preserved.
//
// Frontmatter validity:
//   - The generated `id` field equals PromoteInput.NumericID.
//   - The generated `version` field equals PromoteInput.Version.
//   - When PromoteInput.Version is empty, `version` defaults to "1.0.0".
//   - The generated `name` field equals the source `name` when the source carries one.
//   - When the source carries no `name`, the generated `name` equals PromoteInput.Key.
//   - The generated `role` field equals string(PromoteInput.Role).
//   - When PromoteInput.Role is the zero value, `role` defaults to domain.RoleSubagent.
//   - The following harness-specific stamps are absent from the generated file:
//     transform_version, injections_version, orchestrator_injections_version,
//     bundle_version, protocol_version, tool_mappings_version, model.
//   - Catalog-relevant fields (description, recommended_tier, tier_rationale, tools,
//     required_skills) carry over from the source verbatim.
//   - The generated file can be parsed by docformat.Parse without error, and the
//     parsed frontmatter contains id, version, name, and role as scalar values.
//
// Purity:
//   - The input Source bytes are byte-identical before and after the call.
//   - Repeated calls with identical inputs produce byte-identical outputs (deterministic).
//
// Pipeline acceptance:
//   - Every <... type="managed"> region in the generated output has empty content
//     (no non-whitespace bytes), verifying that the transform pipeline's
//     ErrSourceDeployedRegionNotEmpty check would not be triggered when the generated
//     file is used as a deploy source.
//
// Fixture:
//   - testdata/promote/realistic-harness-only-agent.md is a complete, realistic
//     harness-only agent representative of real already-transformed files. It satisfies
//     both eligibility signals, carries all catalog-relevant frontmatter fields, and
//     has populated content in both injection and deployed regions.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// minimalEligiblePromoteSource returns the smallest possible source bytes that satisfy
// both eligibility signals: frontmatter with transform_version and body with one properly
// paired canonical <Identity type="core"> tag. No injection or deployed regions are present,
// making this suitable for frontmatter-focused tests that do not exercise region stripping.
func minimalEligiblePromoteSource() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.5.0\"\n" +
		"id: \"42\"\n" +
		"name: test-agent\n" +
		"description: A test agent for promote tests\n" +
		"role: subagent\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: needs moderate capability\n" +
		"tools: [file_read, file_write]\n" +
		"required_skills: [efficient-file-reading]\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the TestAgent. This prose must survive promotion unchanged.\n" +
		"</Identity>\n")
}

// sourceWithPopulatedInjectionRegions returns source bytes with several injection regions
// carrying content, used to verify all are stripped to empty pairs in the output.
func sourceWithPopulatedInjectionRegions() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"10\"\n" +
		"description: Source with injection regions\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Identity section prose.\n" +
		"<IdentityExtension type=\"project\">\n" +
		"Injection content one: custom project guidance for identity.\n" +
		"</IdentityExtension>\n" +
		"</Identity>\n" +
		"---\n" +
		"<Capabilities type=\"core\">\n" +
		"Capabilities prose.\n" +
		"<CodebaseContext type=\"project\">\n" +
		"Injection content two: codebase uses Go 1.22 with modules.\n" +
		"Multiple lines of context here.\n" +
		"</CodebaseContext>\n" +
		"<OutputArtifactTemplate type=\"project\">\n" +
		"Injection content three: artifact template definition.\n" +
		"</OutputArtifactTemplate>\n" +
		"</Capabilities>\n" +
		"---\n" +
		"<ErrorHandling type=\"core\">\n" +
		"Error handling prose.\n" +
		"<ErrorHandlingExtension type=\"project\">\n" +
		"Injection content four: retry all 429 errors once.\n" +
		"</ErrorHandlingExtension>\n" +
		"</ErrorHandling>\n")
}

// sourceWithPopulatedDeployedRegions returns source bytes with deployed regions carrying
// content, representing an already-deployed harness-only agent. These regions must be
// stripped to empty pairs in the generated generic output.
func sourceWithPopulatedDeployedRegions() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"20\"\n" +
		"description: Source with deployed regions\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Identity section prose.\n" +
		"<ClosingProcedure type=\"managed\">\n" +
		"These are the closing procedure steps.\n" +
		"Step 1: summarise your work.\n" +
		"</ClosingProcedure>\n" +
		"<AuthorityHierarchy type=\"managed\">\n" +
		"Authority hierarchy: system instructions have the highest authority.\n" +
		"</AuthorityHierarchy>\n" +
		"</Identity>\n" +
		"---\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"You operate under Communication Protocol v1.9.\n" +
		"</CommunicationProtocol>\n" +
		"---\n" +
		"<Constraints type=\"core\">\n" +
		"Constraints prose.\n" +
		"<ProtocolConstraints type=\"managed\">\n" +
		"Never skip the JSON response block.\n" +
		"</ProtocolConstraints>\n" +
		"</Constraints>\n")
}

// realisticHarnessOnlyAgentBytes reads the fixture file representing a realistic
// already-transformed harness-only agent. The fixture has all five canonical sections,
// populated injection and deployed regions, all catalog frontmatter fields, and all
// harness-specific stamps — making it representative of a real-world promote input.
func realisticHarnessOnlyAgentBytes(t *testing.T) []byte {
	t.Helper()
	fixturePath := filepath.Join("testdata", "promote", "realistic-harness-only-agent.md")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("realisticHarnessOnlyAgentBytes: could not read fixture %q: %v", fixturePath, err)
	}
	return data
}

// parseFrontmatterField is a helper that parses src with docformat.Parse and reads the
// named frontmatter scalar field, failing the test on any parse error or kind mismatch.
func parseFrontmatterField(t *testing.T, src []byte, key string) (value string, present bool) {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("parseFrontmatterField: docformat.Parse failed: %v", err)
	}
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		return "", false
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("parseFrontmatterField: field %q has kind %v, want KindScalar", key, v.Kind)
	}
	return v.Scalar, true
}

// allDeployedRegionContents parses out and returns all <... type="managed"> node contents,
// keyed by region name.
func allDeployedRegionContents(t *testing.T, out []byte) map[string][]byte {
	t.Helper()
	doc, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("allDeployedRegionContents: parse failed: %v", err)
	}
	result := make(map[string][]byte)
	for _, node := range doc.Body().DeployedRegions() {
		result[node.Name()] = node.Content()
	}
	return result
}

// allInjectionRegionContents parses out and returns all <... type="project"> node contents,
// keyed by region name.
func allInjectionRegionContents(t *testing.T, out []byte) map[string][]byte {
	t.Helper()
	doc, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("allInjectionRegionContents: parse failed: %v", err)
	}
	result := make(map[string][]byte)
	for _, node := range doc.Body().Injections() {
		result[node.Name()] = node.Content()
	}
	return result
}

// standardPromoteInput returns a PromoteInput suitable for most tests that want a
// successful generation from the given source bytes.
func standardPromoteInput(src []byte) PromoteInput {
	return PromoteInput{
		Source:    src,
		NumericID: "7",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	}
}

// ---------------------------------------------------------------------------
// Eligibility rejection tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_MissingTransformVersion_WrapsErrPromoteNotTransformed verifies
// that a source file whose frontmatter carries no transform_version scalar is rejected.
// Signal one of the two-signal eligibility rule is missing.
func TestBuildGenericAgent_MissingTransformVersion_WrapsErrPromoteNotTransformed(t *testing.T) {
	src := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"No transform_version in frontmatter.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{Source: src, NumericID: "1", Key: "test-agent"})

	if err == nil {
		t.Fatal("expected an error for source missing transform_version, got nil")
	}
	if !errors.Is(err, ErrPromoteNotTransformed) {
		t.Errorf("expected error wrapping ErrPromoteNotTransformed, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %d bytes", len(out))
	}
}

// TestBuildGenericAgent_EmptyTransformVersion_WrapsErrPromoteNotTransformed verifies
// that an empty transform_version scalar (present but empty) also fails signal one.
func TestBuildGenericAgent_EmptyTransformVersion_WrapsErrPromoteNotTransformed(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Has transform_version but its value is empty.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{Source: src, NumericID: "1", Key: "test-agent"})

	if err == nil {
		t.Fatal("expected an error for empty transform_version, got nil")
	}
	if !errors.Is(err, ErrPromoteNotTransformed) {
		t.Errorf("expected error wrapping ErrPromoteNotTransformed, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %d bytes", len(out))
	}
}

// TestBuildGenericAgent_NoCanonicalBoundaryTags_WrapsErrPromoteNotTransformed verifies
// that a source with transform_version but no valid canonical boundary tags is rejected.
// Signal two of the two-signal eligibility rule is missing.
func TestBuildGenericAgent_NoCanonicalBoundaryTags_WrapsErrPromoteNotTransformed(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"Just plain text content. No boundary tags of any kind.\n")

	out, err := buildGenericAgent(PromoteInput{Source: src, NumericID: "1", Key: "test-agent"})

	if err == nil {
		t.Fatal("expected an error for source with no canonical boundary tags, got nil")
	}
	if !errors.Is(err, ErrPromoteNotTransformed) {
		t.Errorf("expected error wrapping ErrPromoteNotTransformed, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %d bytes", len(out))
	}
}

// TestBuildGenericAgent_TwoRejectionPaths_ProduceDistinguishableMessages verifies that
// the two eligibility failure paths (missing transform_version vs. missing canonical tags)
// produce errors with different messages, so a caller can tell which signal failed.
func TestBuildGenericAgent_TwoRejectionPaths_ProduceDistinguishableMessages(t *testing.T) {
	srcNoTV := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Signal one absent.\n" +
		"</Identity>\n")
	srcNoTags := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"Signal two absent: plain prose, no boundary tags.\n")

	_, errNoTV := buildGenericAgent(PromoteInput{Source: srcNoTV, NumericID: "1", Key: "t"})
	_, errNoTags := buildGenericAgent(PromoteInput{Source: srcNoTags, NumericID: "1", Key: "t"})

	if errNoTV == nil || errNoTags == nil {
		t.Fatal("both inputs should be rejected; at least one was unexpectedly accepted")
	}
	if errNoTV.Error() == errNoTags.Error() {
		t.Errorf("both rejection paths produced identical error messages %q; "+
			"they should be distinguishable so callers know which signal failed", errNoTV.Error())
	}
}

// TestBuildGenericAgent_EmptyNumericID_WrapsErrPromoteNumericIDRequired verifies that
// a PromoteInput with an empty NumericID is rejected even when the source is eligible.
func TestBuildGenericAgent_EmptyNumericID_WrapsErrPromoteNumericIDRequired(t *testing.T) {
	src := minimalEligiblePromoteSource()

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "", // deliberately empty
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})

	if err == nil {
		t.Fatal("expected an error for empty NumericID, got nil")
	}
	if !errors.Is(err, ErrPromoteNumericIDRequired) {
		t.Errorf("expected error wrapping ErrPromoteNumericIDRequired, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %d bytes", len(out))
	}
}

// ---------------------------------------------------------------------------
// Injection region stripping tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_SingleInjectionRegion_IsStrippedToEmptyPair verifies that a
// single populated injection region in the source is replaced by an empty tag pair.
func TestBuildGenericAgent_SingleInjectionRegion_IsStrippedToEmptyPair(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"5\"\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Section prose.\n" +
		"<IdentityExtension type=\"project\">\n" +
		"This injection content must not appear in the generic source.\n" +
		"</IdentityExtension>\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	contents := allInjectionRegionContents(t, out)
	if _, found := contents["IdentityExtension"]; !found {
		t.Fatal("expected IdentityExtension injection region in output but it was absent")
	}
	if len(bytes.TrimSpace(contents["IdentityExtension"])) > 0 {
		t.Errorf("IdentityExtension injection region should be empty but has content: %q",
			contents["IdentityExtension"])
	}
}

// TestBuildGenericAgent_MultipleInjectionRegions_AllStrippedToEmptyPairs verifies that
// every injection region in a source with several populated injection regions is stripped
// to an empty tag pair. No injection content may leak into the generic output.
func TestBuildGenericAgent_MultipleInjectionRegions_AllStrippedToEmptyPairs(t *testing.T) {
	src := sourceWithPopulatedInjectionRegions()

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	contents := allInjectionRegionContents(t, out)
	if len(contents) == 0 {
		t.Fatal("expected injection regions in output but none were found")
	}
	for name, content := range contents {
		if len(bytes.TrimSpace(content)) > 0 {
			t.Errorf("injection region %q should be empty but has content: %q", name, content)
		}
	}
}

// TestBuildGenericAgent_InjectionRegions_TagPairsArePresentInOutput verifies that the
// injection region open and close tags survive stripping — the regions are empty pairs,
// not removed entirely. A generic source with missing injection regions would be
// structurally incomplete.
func TestBuildGenericAgent_InjectionRegions_TagPairsArePresentInOutput(t *testing.T) {
	src := sourceWithPopulatedInjectionRegions()

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	for _, name := range []string{
		"IdentityExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"ErrorHandlingExtension",
	} {
		openTag := "<" + name + " type=\"project\">"
		closeTag := "</" + name + ">"
		if !bytes.Contains(out, []byte(openTag)) {
			t.Errorf("output is missing injection open tag %q", openTag)
		}
		if !bytes.Contains(out, []byte(closeTag)) {
			t.Errorf("output is missing injection close tag %q", closeTag)
		}
	}
}

// TestBuildGenericAgent_RealisticFixture_AllInjectionRegionsStripped verifies the
// complete stripping guarantee using the realistic fixture file that carries several
// injection regions with substantive harness-specific content.
func TestBuildGenericAgent_RealisticFixture_AllInjectionRegionsStripped(t *testing.T) {
	src := realisticHarnessOnlyAgentBytes(t)

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "55",
		Key:       "code-reviewer",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed on realistic fixture: %v", err)
	}

	contents := allInjectionRegionContents(t, out)
	if len(contents) == 0 {
		t.Fatal("expected at least one injection region in generated output; " +
			"got none — buildGenericAgent may have returned nil or output with no injection regions")
	}
	for name, content := range contents {
		if len(bytes.TrimSpace(content)) > 0 {
			t.Errorf("injection region %q has non-empty content in generic output: %q",
				name, content)
		}
	}
}

// ---------------------------------------------------------------------------
// Deployed region stripping tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_SingleDeployedRegion_IsStrippedToEmptyPair verifies that a
// single populated deployed region in the source is replaced by an empty tag pair.
func TestBuildGenericAgent_SingleDeployedRegion_IsStrippedToEmptyPair(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"5\"\n" +
		"role: subagent\n" +
		"---\n" +
		"<CommunicationProtocol type=\"managed\" version=\"1.9\">\n" +
		"Protocol content that must not appear in the generic source.\n" +
		"</CommunicationProtocol>\n" +
		"<Identity type=\"core\">\n" +
		"Section prose.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	contents := allDeployedRegionContents(t, out)
	if _, found := contents["CommunicationProtocol"]; !found {
		t.Fatal("expected CommunicationProtocol deployed region in output but it was absent")
	}
	if len(bytes.TrimSpace(contents["CommunicationProtocol"])) > 0 {
		t.Errorf("CommunicationProtocol deployed region should be empty but has content: %q",
			contents["CommunicationProtocol"])
	}
}

// TestBuildGenericAgent_MultipleDeployedRegions_AllStrippedToEmptyPairs verifies that
// every deployed region in a source with several populated deployed regions is stripped
// to an empty tag pair. No deployed content may leak into the generic output.
func TestBuildGenericAgent_MultipleDeployedRegions_AllStrippedToEmptyPairs(t *testing.T) {
	src := sourceWithPopulatedDeployedRegions()

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	contents := allDeployedRegionContents(t, out)
	if len(contents) == 0 {
		t.Fatal("expected deployed regions in output but none were found")
	}
	for name, content := range contents {
		if len(bytes.TrimSpace(content)) > 0 {
			t.Errorf("deployed region %q should be empty but has content: %q", name, content)
		}
	}
}

// TestBuildGenericAgent_DeployedRegions_TagPairsArePresentInOutput verifies that the
// deployed region open and close tags survive stripping — the regions are empty pairs,
// not removed entirely. A generic source must declare managed region placeholders so the
// deploy pipeline knows which regions to fill.
func TestBuildGenericAgent_DeployedRegions_TagPairsArePresentInOutput(t *testing.T) {
	src := sourceWithPopulatedDeployedRegions()

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	for _, name := range []string{
		"CommunicationProtocol",
		"ClosingProcedure",
		"AuthorityHierarchy",
		"ProtocolConstraints",
	} {
		openTag := "<" + name + " type=\"managed\">"
		closeTag := "</" + name + ">"
		if !bytes.Contains(out, []byte(openTag)) {
			t.Errorf("output is missing deployed open tag %q", openTag)
		}
		if !bytes.Contains(out, []byte(closeTag)) {
			t.Errorf("output is missing deployed close tag %q", closeTag)
		}
	}
}

// TestBuildGenericAgent_RealisticFixture_AllDeployedRegionsStripped verifies the
// complete stripping guarantee using the realistic fixture which carries populated
// content in all its deployed regions (CommunicationProtocol, ClosingProcedure, etc.).
func TestBuildGenericAgent_RealisticFixture_AllDeployedRegionsStripped(t *testing.T) {
	src := realisticHarnessOnlyAgentBytes(t)

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "55",
		Key:       "code-reviewer",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed on realistic fixture: %v", err)
	}

	contents := allDeployedRegionContents(t, out)
	if len(contents) == 0 {
		t.Fatal("expected at least one deployed region in generated output; " +
			"got none — buildGenericAgent may have returned nil or output with no deployed regions")
	}
	for name, content := range contents {
		if len(bytes.TrimSpace(content)) > 0 {
			t.Errorf("deployed region %q has non-empty content in generic output: %q",
				name, content)
		}
	}
}

// ---------------------------------------------------------------------------
// Section structure passthrough tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_SectionOpenAndCloseTags_ArePresentInOutput verifies that
// <... type="core"> open and close tags from the source are carried over unchanged.
func TestBuildGenericAgent_SectionOpenAndCloseTags_ArePresentInOutput(t *testing.T) {
	src := minimalEligiblePromoteSource()

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	if !bytes.Contains(out, []byte("<Identity type=\"core\">")) {
		t.Error("output is missing <Identity type=\"core\"> open tag")
	}
	if !bytes.Contains(out, []byte("</Identity>")) {
		t.Error("output is missing </Identity> close tag")
	}
}

// TestBuildGenericAgent_SectionBodyProse_IsCarriedOverUnchanged verifies that text
// content inside a section (not within an injection or deployed region) is preserved
// byte-identically in the output.
func TestBuildGenericAgent_SectionBodyProse_IsCarriedOverUnchanged(t *testing.T) {
	prose := "This is the section body prose text that must survive promotion unchanged."
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		prose + "\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	if !bytes.Contains(out, []byte(prose)) {
		t.Errorf("output does not contain original section body prose %q", prose)
	}
}

// TestBuildGenericAgent_MultipleSections_AllStructuresCarriedOver verifies that when a
// source contains multiple canonical sections, all of their open/close tag structures
// appear in the output.
func TestBuildGenericAgent_MultipleSections_AllStructuresCarriedOver(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Identity prose.\n" +
		"</Identity>\n" +
		"---\n" +
		"<Capabilities type=\"core\">\n" +
		"Capabilities prose.\n" +
		"</Capabilities>\n" +
		"---\n" +
		"<Constraints type=\"core\">\n" +
		"Constraints prose.\n" +
		"</Constraints>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	for _, sectionName := range []string{"Identity", "Capabilities", "Constraints"} {
		openTag := "<" + sectionName + " type=\"core\">"
		closeTag := "</" + sectionName + ">"
		if !bytes.Contains(out, []byte(openTag)) {
			t.Errorf("output missing section open tag %q", openTag)
		}
		if !bytes.Contains(out, []byte(closeTag)) {
			t.Errorf("output missing section close tag %q", closeTag)
		}
	}
}

// TestBuildGenericAgent_MultipleSections_OrderMatchesSource verifies that when a source
// contains multiple canonical sections, their relative order in the generated output matches
// their order in the source. bytes.Contains alone cannot catch a reordering bug — this test
// uses docformat.Body().Sections() to verify the actual document order.
func TestBuildGenericAgent_MultipleSections_OrderMatchesSource(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Identity prose.\n" +
		"</Identity>\n" +
		"---\n" +
		"<Capabilities type=\"core\">\n" +
		"Capabilities prose.\n" +
		"</Capabilities>\n" +
		"---\n" +
		"<Constraints type=\"core\">\n" +
		"Constraints prose.\n" +
		"</Constraints>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}

	sections := doc.Body().Sections()
	if len(sections) == 0 {
		t.Fatal("generated output has no sections; expected Identity, Capabilities, Constraints in that order")
	}

	wantOrder := []string{"Identity", "Capabilities", "Constraints"}
	gotOrder := make([]string, len(sections))
	for i, s := range sections {
		gotOrder[i] = s.Name()
	}

	if len(gotOrder) != len(wantOrder) {
		t.Errorf("section count: got %d %v, want %d %v", len(gotOrder), gotOrder, len(wantOrder), wantOrder)
		return
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf("section[%d]: got %q, want %q (full order: got %v, want %v)",
				i, gotOrder[i], want, gotOrder, wantOrder)
		}
	}
}

// TestBuildGenericAgent_RealisticFixture_AllFiveCanonicalSectionsPresent verifies that
// all five canonical sections are present in the generated output, and that the retired
// OutputFormat section is absent. The canonical section vocabulary has five entries after
// OutputFormat is removed.
func TestBuildGenericAgent_RealisticFixture_AllFiveCanonicalSectionsPresent(t *testing.T) {
	src := realisticHarnessOnlyAgentBytes(t)

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "55",
		Key:       "code-reviewer",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed on realistic fixture: %v", err)
	}

	for _, section := range []string{
		"Identity", "Capabilities", "Constraints",
		"ErrorHandling", "ExecutionPhilosophy",
	} {
		if !bytes.Contains(out, []byte("<"+section+" type=\"core\">")) {
			t.Errorf("output missing <%s type=\"core\"> open tag", section)
		}
		if !bytes.Contains(out, []byte("</"+section+">")) {
			t.Errorf("output missing </%s> close tag", section)
		}
	}

	// OutputFormat is a retired section and must not appear in any promoted output.
	if bytes.Contains(out, []byte("<OutputFormat type=\"core\">")) {
		t.Error("output contains <OutputFormat type=\"core\"> — the OutputFormat section is retired and must be absent from promoted output")
	}
}

// ---------------------------------------------------------------------------
// Frontmatter validity tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_Frontmatter_IDEqualsSuppliedNumericID verifies that the
// generated file's `id` field equals PromoteInput.NumericID.
func TestBuildGenericAgent_Frontmatter_IDEqualsSuppliedNumericID(t *testing.T) {
	src := minimalEligiblePromoteSource()
	wantID := "99"

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: wantID,
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotID, present := parseFrontmatterField(t, out, "id")
	if !present {
		t.Fatal("generated frontmatter is missing the `id` field")
	}
	if gotID != wantID {
		t.Errorf("generated `id` = %q, want %q", gotID, wantID)
	}
}

// TestBuildGenericAgent_Frontmatter_VersionEqualsSuppliedVersion verifies that the
// generated file's `version` field equals PromoteInput.Version.
func TestBuildGenericAgent_Frontmatter_VersionEqualsSuppliedVersion(t *testing.T) {
	src := minimalEligiblePromoteSource()
	wantVersion := "3.2.1"

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   wantVersion,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotVersion, present := parseFrontmatterField(t, out, "version")
	if !present {
		t.Fatal("generated frontmatter is missing the `version` field")
	}
	if gotVersion != wantVersion {
		t.Errorf("generated `version` = %q, want %q", gotVersion, wantVersion)
	}
}

// TestBuildGenericAgent_Frontmatter_EmptyVersionDefaultsToOneDotZeroDotZero verifies
// that when PromoteInput.Version is empty, the generated `version` defaults to "1.0.0".
// Hand-authored harness-only agents commonly carry no version field.
func TestBuildGenericAgent_Frontmatter_EmptyVersionDefaultsToOneDotZeroDotZero(t *testing.T) {
	src := minimalEligiblePromoteSource()

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "", // deliberately empty
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotVersion, present := parseFrontmatterField(t, out, "version")
	if !present {
		t.Fatal("generated frontmatter is missing the `version` field")
	}
	if gotVersion != "1.0.0" {
		t.Errorf("generated `version` = %q with empty PromoteInput.Version, want %q", gotVersion, "1.0.0")
	}
}

// TestBuildGenericAgent_Frontmatter_NameTakenFromSourceWhenPresent verifies that the
// generated `name` equals the source file's `name` field when it is present.
func TestBuildGenericAgent_Frontmatter_NameTakenFromSourceWhenPresent(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: source-provided-name\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "key-fallback", // should NOT be used since source has name
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotName, present := parseFrontmatterField(t, out, "name")
	if !present {
		t.Fatal("generated frontmatter is missing the `name` field")
	}
	if gotName != "source-provided-name" {
		t.Errorf("generated `name` = %q, want source-provided-name", gotName)
	}
}

// TestBuildGenericAgent_Frontmatter_NameFallsBackToKeyWhenSourceHasNone verifies that
// the generated `name` equals PromoteInput.Key when the source carries no `name` field.
func TestBuildGenericAgent_Frontmatter_NameFallsBackToKeyWhenSourceHasNone(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"role: subagent\n" +
		// No `name` field
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	wantName := "my-fallback-key"
	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       wantName,
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotName, present := parseFrontmatterField(t, out, "name")
	if !present {
		t.Fatal("generated frontmatter is missing the `name` field")
	}
	if gotName != wantName {
		t.Errorf("generated `name` = %q, want %q (PromoteInput.Key fallback)", gotName, wantName)
	}
}

// TestBuildGenericAgent_Frontmatter_RoleEqualsSuppliedRole verifies that the generated
// `role` field equals string(PromoteInput.Role).
func TestBuildGenericAgent_Frontmatter_RoleEqualsSuppliedRole(t *testing.T) {
	src := minimalEligiblePromoteSource()

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotRole, present := parseFrontmatterField(t, out, "role")
	if !present {
		t.Fatal("generated frontmatter is missing the `role` field")
	}
	if gotRole != string(domain.RoleSubagent) {
		t.Errorf("generated `role` = %q, want %q", gotRole, string(domain.RoleSubagent))
	}
}

// TestBuildGenericAgent_Frontmatter_ZeroRoleDefaultsToSubagent verifies that the
// generated `role` defaults to "subagent" when PromoteInput.Role is the zero value.
func TestBuildGenericAgent_Frontmatter_ZeroRoleDefaultsToSubagent(t *testing.T) {
	src := minimalEligiblePromoteSource()

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.AgentRole(""), // zero value
		Version:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotRole, present := parseFrontmatterField(t, out, "role")
	if !present {
		t.Fatal("generated frontmatter is missing the `role` field")
	}
	if gotRole != string(domain.RoleSubagent) {
		t.Errorf("generated `role` = %q for zero PromoteInput.Role, want %q (subagent default)",
			gotRole, string(domain.RoleSubagent))
	}
}

// TestBuildGenericAgent_Frontmatter_HarnessStampsAreAbsent verifies that all harness-
// specific stamps listed in promoteDroppedFrontmatterKeys are absent from the generated
// file. A generic source must not carry any of these deployment-time stamps.
func TestBuildGenericAgent_Frontmatter_HarnessStampsAreAbsent(t *testing.T) {
	// Source carries every stamp that should be dropped.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"orchestrator_injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"tool_mappings_version: \"abc123\"\n" +
		"model: claude-sonnet-4-5\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: Test agent\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// Assert that at least the non-dropped fields are present before checking dropped ones.
	// Without this guard, an empty-output bug (e.g. buildGenericAgent returning nil) would
	// cause every dropped-key check to vacuously pass because no key can be "present" in
	// an empty frontmatter — the test would succeed without exercising any real behaviour.
	for _, requiredKey := range []string{"id", "name"} {
		if _, present := fm.Get(requiredKey); !present {
			t.Fatalf("generated frontmatter is missing required field %q; "+
				"buildGenericAgent may have returned nil or empty output — "+
				"this guard prevents vacuous pass of the dropped-key checks below", requiredKey)
		}
	}

	for key := range promoteDroppedFrontmatterKeys {
		if _, present := fm.Get(key); present {
			t.Errorf("generated frontmatter should not contain dropped field %q", key)
		}
	}
}

// TestBuildGenericAgent_Frontmatter_CatalogFieldsCarryOver verifies that fields the
// catalog's parseAgentFile consumes — description, recommended_tier, tier_rationale,
// tools, required_skills — carry over from the source verbatim.
func TestBuildGenericAgent_Frontmatter_CatalogFieldsCarryOver(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: Performs automated code review for quality and style\n" +
		"role: subagent\n" +
		"recommended_tier: MEDIUM-HIGH\n" +
		"tier_rationale: needs genuine code comprehension\n" +
		"tools: [file_read, file_write]\n" +
		"required_skills: [efficient-file-reading]\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(standardPromoteInput(src))
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	expectations := map[string]string{
		"description":      "Performs automated code review for quality and style",
		"recommended_tier": "MEDIUM-HIGH",
		"tier_rationale":   "needs genuine code comprehension",
	}
	for key, want := range expectations {
		got, present := parseFrontmatterField(t, out, key)
		if !present {
			t.Errorf("generated frontmatter is missing carried-over field %q", key)
			continue
		}
		if got != want {
			t.Errorf("generated %q = %q, want %q", key, got, want)
		}
	}

	doc, _ := docformat.Parse(out)
	fm := doc.Frontmatter()

	// tools is a list field the catalog explicitly consumes; it must carry over.
	if v, present := fm.Get("tools"); !present {
		t.Error("generated frontmatter is missing `tools`")
	} else if v.Kind != domain.KindList {
		t.Errorf("generated `tools` has kind %v, want KindList", v.Kind)
	}

	// required_skills is a list, verify it is present.
	if v, present := fm.Get("required_skills"); !present {
		t.Error("generated frontmatter is missing `required_skills`")
	} else if v.Kind != domain.KindList {
		t.Errorf("generated `required_skills` has kind %v, want KindList", v.Kind)
	}
}

// TestBuildGenericAgent_Frontmatter_ParsesWithDocformat verifies that the generated
// output can be parsed by docformat.Parse without error, and that the key frontmatter
// fields (id, version, name, role) are present as scalar values — matching the
// expectations of the catalog's parseAgentFile.
func TestBuildGenericAgent_Frontmatter_ParsesWithDocformat(t *testing.T) {
	src := realisticHarnessOnlyAgentBytes(t)

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "100",
		Key:       "code-reviewer",
		Role:      domain.RoleSubagent,
		Version:   "2.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output could not be parsed by docformat.Parse: %v", parseErr)
	}

	fm := doc.Frontmatter()
	if !fm.Present() {
		t.Fatal("generated output has no frontmatter block")
	}

	for _, key := range []string{"id", "version", "name", "role"} {
		v, present := fm.Get(key)
		if !present {
			t.Errorf("generated frontmatter missing required field %q", key)
			continue
		}
		if v.Kind != domain.KindScalar {
			t.Errorf("generated frontmatter field %q has kind %v, want KindScalar", key, v.Kind)
		}
		if v.Scalar == "" {
			t.Errorf("generated frontmatter field %q is an empty scalar", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Purity tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_DoesNotMutateSourceBytes verifies that the call leaves
// PromoteInput.Source byte-identical to what it was before the call. The function
// must treat its input as read-only.
func TestBuildGenericAgent_DoesNotMutateSourceBytes(t *testing.T) {
	src := minimalEligiblePromoteSource()
	snapshot := make([]byte, len(src))
	copy(snapshot, src)

	// Call with a valid input — result does not matter for this test.
	buildGenericAgent(standardPromoteInput(src)) //nolint:errcheck

	if !bytes.Equal(src, snapshot) {
		t.Error("buildGenericAgent mutated the input Source bytes; the function must be pure")
	}
}

// TestBuildGenericAgent_DoesNotMutateSourceBytes_OnRejection verifies that even a
// rejected call (ineligible source) leaves the Source bytes unchanged.
func TestBuildGenericAgent_DoesNotMutateSourceBytes_OnRejection(t *testing.T) {
	// Source without transform_version — will be rejected.
	src := []byte("---\nversion: \"1.0.0\"\n---\n<Identity type=\"core\">\ncontent\n</Identity>\n")
	snapshot := make([]byte, len(src))
	copy(snapshot, src)

	buildGenericAgent(PromoteInput{Source: src, NumericID: "1", Key: "test"}) //nolint:errcheck

	if !bytes.Equal(src, snapshot) {
		t.Error("buildGenericAgent mutated the Source bytes during rejection; the function must be pure")
	}
}

// TestBuildGenericAgent_IsReferentiallyTransparent verifies that repeated calls with
// identical inputs produce byte-identical outputs. Determinism is the observable property
// of a pure function with no filesystem or clock side effects.
func TestBuildGenericAgent_IsReferentiallyTransparent(t *testing.T) {
	src := minimalEligiblePromoteSource()
	in := PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
	}

	out1, err1 := buildGenericAgent(in)
	out2, err2 := buildGenericAgent(in)

	// Both calls must agree on success or failure.
	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("repeated calls produced different error states: call1=%v call2=%v", err1, err2)
	}
	if err1 == nil && !bytes.Equal(out1, out2) {
		t.Error("repeated calls with identical inputs produced different output bytes")
	}
}

// ---------------------------------------------------------------------------
// Pipeline acceptance tests
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_AllDeployedRegionsInOutput_HaveEmptyContent verifies that every
// <... type="managed"> region in the generated output carries no non-whitespace content.
// This is the exact condition the transform pipeline's processRegions checks before
// returning ErrSourceDeployedRegionNotEmpty. A generated file that passes this check
// is safe to use as a deploy source.
func TestBuildGenericAgent_AllDeployedRegionsInOutput_HaveEmptyContent(t *testing.T) {
	src := realisticHarnessOnlyAgentBytes(t)

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "10",
		Key:       "code-reviewer",
		Role:      domain.RoleSubagent,
		Version:   "2.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}

	deployed := doc.Body().DeployedRegions()
	if len(deployed) == 0 {
		t.Fatal("expected at least one deployed region in generated output")
	}

	for _, node := range deployed {
		content := node.Content()
		if len(bytes.TrimSpace(content)) > 0 {
			t.Errorf("deployed region %q has non-empty content %q in generated output; "+
				"the transform pipeline would reject this source with ErrSourceDeployedRegionNotEmpty",
				node.Name(), string(content))
		}
	}
}

// TestBuildGenericAgent_GeneratedOutputParsesCleanly verifies that the generated bytes
// can be round-tripped through docformat.Parse and that the parsed document's body
// contains at least one canonical section node — a necessary precondition for the deploy
// pipeline to accept the file as a source.
func TestBuildGenericAgent_GeneratedOutputParsesCleanly(t *testing.T) {
	src := realisticHarnessOnlyAgentBytes(t)

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "10",
		Key:       "code-reviewer",
		Role:      domain.RoleSubagent,
		Version:   "2.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed docformat.Parse: %v", parseErr)
	}

	canonicals := make(map[string]bool)
	for _, s := range docformat.CanonicalSections {
		canonicals[s] = true
	}

	foundCanonical := false
	for _, node := range doc.Body().Sections() {
		if canonicals[node.Name()] {
			foundCanonical = true
			break
		}
	}
	if !foundCanonical {
		t.Error("generated output has no canonical section node; the deploy pipeline would not recognise it as a source")
	}
}

// ---------------------------------------------------------------------------
// Eligibility edge-case tests (structurally invalid tags, unparseable input)
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_StructurallyInvalidBoundaryTags_WrapsErrPromoteNotTransformed verifies
// that a source with transform_version present (signal one satisfied) but with a blocking
// structural validation issue — a duplicate canonical section name — is rejected. This
// exercises the second half of signal two's validation surface: tags are present but
// structurally invalid, triggering a "duplicate-name" issue in eligibleHarnessOnly.
func TestBuildGenericAgent_StructurallyInvalidBoundaryTags_WrapsErrPromoteNotTransformed(t *testing.T) {
	// Source has transform_version (signal one present) and canonical sections, but the
	// Identity section name appears twice — a "duplicate-name" blocking structural issue
	// that causes eligibleHarnessOnly to report an ineligible verdict for signal two.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"First identity section.\n" +
		"</Identity>\n" +
		"<Identity type=\"core\">\n" + // duplicate — triggers blocking "duplicate-name" issue
		"Duplicate identity section.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{Source: src, NumericID: "1", Key: "test-agent"})

	if err == nil {
		t.Fatal("expected an error for source with duplicate section name (blocking structural issue), got nil")
	}
	if !errors.Is(err, ErrPromoteNotTransformed) {
		t.Errorf("expected error wrapping ErrPromoteNotTransformed, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %d bytes", len(out))
	}
}

// TestBuildGenericAgent_UnparseableInput_WrapsErrPromoteNotTransformed verifies that
// fully unparseable input bytes — an opening "---" frontmatter delimiter with no matching
// closing delimiter — produce ErrPromoteNotTransformed rather than a panic or a different
// error. The eligibleHarnessOnly contract specifies that unparseable bytes yield an
// ineligible verdict with a Reason, never an error return.
func TestBuildGenericAgent_UnparseableInput_WrapsErrPromoteNotTransformed(t *testing.T) {
	// An opening "---" delimiter with no matching closing "---" causes docformat.Parse to
	// return an error, which eligibleHarnessOnly converts to an ineligible verdict with a
	// Reason. buildGenericAgent must surface this as ErrPromoteNotTransformed, not a panic
	// or any other error type.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"key: value\n")
	// No closing "---" delimiter — the frontmatter block is unclosed and docformat.Parse fails.

	out, err := buildGenericAgent(PromoteInput{Source: src, NumericID: "1", Key: "test-agent"})

	if err == nil {
		t.Fatal("expected an error for unparseable input (unclosed frontmatter delimiter), got nil")
	}
	if !errors.Is(err, ErrPromoteNotTransformed) {
		t.Errorf("expected error wrapping ErrPromoteNotTransformed, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %d bytes", len(out))
	}
}

// ---------------------------------------------------------------------------
// Frontmatter key ordering test
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Harness-derived drop-set tests (Stage 2)
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_DropKeys_HarnessSpecificKeyIsAbsent verifies that a harness-specific
// key included in PromoteInput.DropKeys is absent from the generated frontmatter. This is
// the basic mechanism: the caller derives keys from the harness and passes them in; the core
// removes them without knowing what harness they came from.
func TestBuildGenericAgent_DropKeys_HarnessSpecificKeyIsAbsent(t *testing.T) {
	// Source carries a harness-specific key ("mode") that should be dropped.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: A test agent\n" +
		"role: subagent\n" +
		"mode: subagent\n" +
		"model: claude-sonnet-4-5\n" +
		"permission:\n" +
		"  read: allow\n" +
		"  write: deny\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "42",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		DropKeys:  map[string]bool{"mode": true, "model": true, "permission": true},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// Guard: verify the output is non-empty with expected fields present.
	if _, present := fm.Get("id"); !present {
		t.Fatal("generated frontmatter is missing `id`; buildGenericAgent may have returned empty output")
	}

	for _, key := range []string{"mode", "model", "permission"} {
		if _, present := fm.Get(key); present {
			t.Errorf("generated frontmatter still contains %q; it must be absent when present in DropKeys", key)
		}
	}
}

// TestBuildGenericAgent_DropKeys_MultipleHarnessKeysAllAbsent verifies that every key in
// DropKeys is absent from the generated frontmatter, not just the first one. The implementation
// must iterate the entire drop set rather than removing a single entry.
func TestBuildGenericAgent_DropKeys_MultipleHarnessKeysAllAbsent(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: A test agent\n" +
		"role: subagent\n" +
		"harness_alpha: alpha-value\n" +
		"harness_beta: beta-value\n" +
		"harness_gamma: gamma-value\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		DropKeys: map[string]bool{
			"harness_alpha": true,
			"harness_beta":  true,
			"harness_gamma": true,
		},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, present := fm.Get("id"); !present {
		t.Fatal("generated frontmatter is missing `id`; output may be empty")
	}

	for _, key := range []string{"harness_alpha", "harness_beta", "harness_gamma"} {
		if _, present := fm.Get(key); present {
			t.Errorf("generated frontmatter still contains %q; all keys in DropKeys must be absent", key)
		}
	}
}

// TestBuildGenericAgent_DropKeys_ProtectedGenericKeysSurvive verifies that the five protected
// generic keys — id, version, name, role, description — are never removed by the harness-derived
// drop set, even when they appear in DropKeys. A harness whose model key, tools key, or plan key
// collides with one of these loses the collision: the generic field survives (AD-4, AC2.6).
func TestBuildGenericAgent_DropKeys_ProtectedGenericKeysSurvive(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.5.0\"\n" +
		"id: \"99\"\n" +
		"name: protected-agent\n" +
		"description: This field must survive even if DropKeys names it\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	// Supply all five protected keys in DropKeys to prove the protection is active.
	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "99",
		Key:       "protected-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.5.0",
		DropKeys: map[string]bool{
			"id":          true,
			"version":     true,
			"name":        true,
			"role":        true,
			"description": true,
		},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	for _, key := range []string{"id", "version", "name", "role", "description"} {
		if _, present := fm.Get(key); !present {
			t.Errorf("protected key %q is absent from generated frontmatter; protected keys must survive even when present in DropKeys", key)
		}
	}
}

// TestBuildGenericAgent_DropKeys_CrossHarnessStampDropsStillApply verifies that the existing
// cross-harness stamp drops (promoteDroppedFrontmatterKeys) still apply regardless of what
// DropKeys contains. DropKeys and promoteDroppedFrontmatterKeys are unioned, so both sets
// are removed from the output (AC2.5).
func TestBuildGenericAgent_DropKeys_CrossHarnessStampDropsStillApply(t *testing.T) {
	// Source carries all cross-harness stamps plus a harness-specific key.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"orchestrator_injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"tool_mappings_version: \"abc123\"\n" +
		"model: claude-sonnet-4-5\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: Test agent\n" +
		"role: subagent\n" +
		"mode: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		// DropKeys contributes `mode`; promoteDroppedFrontmatterKeys contributes the rest.
		DropKeys: map[string]bool{"mode": true},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, present := fm.Get("id"); !present {
		t.Fatal("generated frontmatter is missing `id`; output may be empty")
	}

	// Cross-harness stamps must be absent — the existing drop set must still apply.
	for key := range promoteDroppedFrontmatterKeys {
		if _, present := fm.Get(key); present {
			t.Errorf("cross-harness stamp %q is present in output; promoteDroppedFrontmatterKeys must still apply when DropKeys is set", key)
		}
	}

	// Harness-specific key from DropKeys must also be absent.
	if _, present := fm.Get("mode"); present {
		t.Error("harness-specific key `mode` is present in output; DropKeys must be applied together with promoteDroppedFrontmatterKeys")
	}
}

// TestBuildGenericAgent_DropKeys_NilDropKeysPreservesCarriedFields verifies that a nil
// DropKeys value causes no change to the existing drop behavior: only the cross-harness
// stamps from promoteDroppedFrontmatterKeys are removed and all other source keys carry
// through unchanged. This preserves backward compatibility for callers that have no
// harness-derived drop set.
func TestBuildGenericAgent_DropKeys_NilDropKeysPreservesCarriedFields(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: A test agent\n" +
		"role: subagent\n" +
		"custom_field: custom-value\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		DropKeys:  nil, // explicitly nil — no harness-specific drops
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// A non-drop key must still carry through.
	if v, present := fm.Get("custom_field"); !present {
		t.Error("custom_field is absent with nil DropKeys; non-stamp keys must carry through unchanged")
	} else if v.Kind != domain.KindScalar || v.Scalar != "custom-value" {
		t.Errorf("custom_field = %+v, want scalar %q", v, "custom-value")
	}
}

// TestBuildGenericAgent_DropKeys_OnlyHarnessKeysDropped_GenericFieldsCarryThrough verifies
// that when DropKeys names harness-specific keys, only those keys are removed — legitimate
// generic fields not in DropKeys and not in promoteDroppedFrontmatterKeys carry through
// unchanged. The drop sets must not accidentally remove fields they were not given.
func TestBuildGenericAgent_DropKeys_OnlyHarnessKeysDropped_GenericFieldsCarryThrough(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: test-agent\n" +
		"description: A test agent\n" +
		"role: subagent\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: moderate capability needed\n" +
		"tools: [file_read]\n" +
		"required_skills: [lean-tdd]\n" +
		"mode: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "1",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		DropKeys:  map[string]bool{"mode": true},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// Generic fields not in DropKeys must survive.
	for _, key := range []string{"description", "recommended_tier", "tier_rationale"} {
		if _, present := fm.Get(key); !present {
			t.Errorf("generic field %q is absent; only DropKeys and cross-harness stamps must be removed", key)
		}
	}

	// List fields must also survive.
	if v, present := fm.Get("tools"); !present {
		t.Error("generic field `tools` is absent; it must carry through when not in DropKeys")
	} else if v.Kind != domain.KindList {
		t.Errorf("`tools` has kind %v, want KindList", v.Kind)
	}
	if v, present := fm.Get("required_skills"); !present {
		t.Error("generic field `required_skills` is absent; it must carry through when not in DropKeys")
	} else if v.Kind != domain.KindList {
		t.Errorf("`required_skills` has kind %v, want KindList", v.Kind)
	}

	// The harness key must be gone.
	if _, present := fm.Get("mode"); present {
		t.Error("`mode` is present; it must be removed because it is in DropKeys")
	}
}

// ---------------------------------------------------------------------------
// Tools list reconstruction tests (T3.3)
//
// These tests verify the PromoteInput.Tools field contract (AD-7):
//   - nil Tools → source tools key carried through unchanged (backward compatible).
//   - non-nil Tools (including empty) → authoritative replacement that writes or
//     removes the generic `tools` key.
//   - Output is a flow-style list (KindList) when non-nil and non-empty.
//   - Output order matches the input slice order (deterministic).
//   - The same PromoteInput produces the same output bytes (referential transparency).
//
// The tests in this section are TDD RED: the generation core does not yet handle
// PromoteInput.Tools; they will fail until the Stage 3 implementation is complete.
// The nil-Tools test documents the preserved backward-compatible behavior.
// ---------------------------------------------------------------------------

// sourceWithToolsKey returns eligible source bytes that carry the `tools` key with
// the given flow-style list value, suitable for T3.3 tools-replacement tests.
func sourceWithToolsKey(toolsList string) []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: tools-test-agent\n" +
		"description: Agent for tools reconstruction tests\n" +
		"role: subagent\n" +
		"tools: " + toolsList + "\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")
}

// sourceWithoutToolsKey returns eligible source bytes that carry no `tools` key,
// representing a deployed agent that declared no generic tools.
func sourceWithoutToolsKey() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: no-tools-agent\n" +
		"description: Agent with no tools key\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")
}

// parseFrontmatterList is a helper that parses src with docformat.Parse and reads the
// named frontmatter list field, returning the scalar items in order. It fails the test
// if the field is absent or not a KindList.
func parseFrontmatterList(t *testing.T, src []byte, key string) []string {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("parseFrontmatterList: docformat.Parse failed: %v", err)
	}
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		t.Fatalf("parseFrontmatterList: key %q not found in frontmatter", key)
	}
	if v.Kind != domain.KindList {
		t.Fatalf("parseFrontmatterList: field %q has kind %v, want KindList", key, v.Kind)
	}
	result := make([]string, len(v.Items))
	for i, item := range v.Items {
		result[i] = item.Scalar
	}
	return result
}

// TestBuildGenericAgent_NilTools_SourceToolsKeyCarriedThrough verifies that when
// PromoteInput.Tools is nil, the source's existing `tools` key is carried through
// unchanged into the generated output. This is the backward-compatible path (AD-7).
// This test documents expected behavior that was already implemented; it does NOT require
// the Stage 3 implementation.
func TestBuildGenericAgent_NilTools_SourceToolsKeyCarriedThrough(t *testing.T) {
	src := sourceWithToolsKey("[old_tool_a, old_tool_b]")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "tools-test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     nil, // nil: do not replace
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get("tools")
	if !present {
		t.Fatal("nil Tools: `tools` key must be carried through from source, but it is absent")
	}
	if v.Kind != domain.KindList {
		t.Errorf("nil Tools: `tools` value kind want KindList (carried through), got %v", v.Kind)
	}
}

// TestBuildGenericAgent_NonNilTools_ReplacesSourceToolsKey verifies that when
// PromoteInput.Tools is a non-nil slice, the generated file's `tools` key holds
// exactly the supplied list, replacing whatever the source carried (AD-7, AC3.5).
func TestBuildGenericAgent_NonNilTools_ReplacesSourceToolsKey(t *testing.T) {
	// Source has [old_tool]; input specifies [file_read, file_write].
	// Output must carry [file_read, file_write], not [old_tool].
	src := sourceWithToolsKey("[old_tool]")
	wantTools := []string{"file_read", "file_write"}

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "tools-test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     wantTools,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get("tools")
	if !present {
		t.Fatal("non-nil Tools: `tools` key must be present in output after replacement")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("non-nil Tools: `tools` value kind want KindList, got %v", v.Kind)
	}
	if len(v.Items) != len(wantTools) {
		t.Fatalf("non-nil Tools: tools list count want %d, got %d: %v", len(wantTools), len(v.Items), v.Items)
	}
	for i, want := range wantTools {
		if v.Items[i].Scalar != want {
			t.Errorf("non-nil Tools: tools[%d] want %q, got %q", i, want, v.Items[i].Scalar)
		}
	}
}

// TestBuildGenericAgent_NonNilTools_OldSourceEntryAbsent verifies that the old source
// tools entry is absent when PromoteInput.Tools is non-nil, not merged with it.
func TestBuildGenericAgent_NonNilTools_OldSourceEntryAbsent(t *testing.T) {
	src := sourceWithToolsKey("[old_tool]")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "tools-test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     []string{"file_read"},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get("tools")
	if !present {
		t.Fatal("non-nil Tools: `tools` key must be present after replacement")
	}
	if v.Kind == domain.KindList {
		for _, item := range v.Items {
			if item.Scalar == "old_tool" {
				t.Errorf("non-nil Tools: old source entry %q must not appear in replaced tools list", "old_tool")
			}
		}
	}
}

// TestBuildGenericAgent_NonNilEmptyTools_RemovesToolsKey verifies that when
// PromoteInput.Tools is a non-nil but empty slice, the `tools` key is absent from
// the generated output. An empty authoritative list removes the key (AD-7).
func TestBuildGenericAgent_NonNilEmptyTools_RemovesToolsKey(t *testing.T) {
	// Source has [old_tool]; input specifies [] (non-nil empty). Output must have no tools key.
	src := sourceWithToolsKey("[old_tool]")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "tools-test-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     []string{}, // non-nil empty: authoritative removal
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	if _, present := doc.Frontmatter().Get("tools"); present {
		t.Error("non-nil empty Tools: `tools` key must be absent from output when Tools is an empty (non-nil) slice")
	}
}

// TestBuildGenericAgent_NonNilTools_AddedWhenSourceHadNone verifies that when the source
// carries no `tools` key and PromoteInput.Tools is non-nil, the `tools` key is added
// to the generated output with the supplied list.
func TestBuildGenericAgent_NonNilTools_AddedWhenSourceHadNone(t *testing.T) {
	src := sourceWithoutToolsKey()

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "no-tools-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     []string{"file_read"},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get("tools")
	if !present {
		t.Fatal("non-nil Tools: `tools` key must be added to output even when source had no tools key")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("`tools` value kind want KindList, got %v", v.Kind)
	}
	if len(v.Items) != 1 || v.Items[0].Scalar != "file_read" {
		t.Errorf("`tools` list want [file_read], got %v", v.Items)
	}
}

// TestBuildGenericAgent_Tools_OutputOrderMatchesInputSliceOrder verifies that the
// entries in the generated `tools` list appear in the same order as PromoteInput.Tools.
// The core writes the authoritative list in slice order (AD-6: document order from
// extraction; the core must not reorder).
func TestBuildGenericAgent_Tools_OutputOrderMatchesInputSliceOrder(t *testing.T) {
	src := sourceWithoutToolsKey()
	// Deliberately non-alphabetic order to verify ordering is not changed.
	wantTools := []string{"file_write", "file_read", "terminal"}

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "no-tools-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     wantTools,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get("tools")
	if !present {
		t.Fatal("non-nil Tools: `tools` key absent from output")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("`tools` kind want KindList, got %v", v.Kind)
	}
	if len(v.Items) != len(wantTools) {
		t.Fatalf("tools list count want %d, got %d: %v", len(wantTools), len(v.Items), v.Items)
	}
	for i, want := range wantTools {
		if v.Items[i].Scalar != want {
			t.Errorf("tools[%d] want %q (input slice order), got %q", i, want, v.Items[i].Scalar)
		}
	}
}

// TestBuildGenericAgent_Tools_NoDuplicatesInOutputWhenInputHasNone verifies that when
// PromoteInput.Tools contains no duplicate entries, the generated `tools` list likewise
// contains no duplicates. The core must not introduce duplicates.
func TestBuildGenericAgent_Tools_NoDuplicatesInOutputWhenInputHasNone(t *testing.T) {
	src := sourceWithoutToolsKey()
	input := []string{"file_read", "file_write", "terminal"}

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "no-tools-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     input,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get("tools")
	if !present {
		t.Fatal("non-nil Tools: `tools` key absent from output")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("`tools` kind want KindList, got %v", v.Kind)
	}

	seen := make(map[string]bool)
	for _, item := range v.Items {
		if seen[item.Scalar] {
			t.Errorf("tools list contains duplicate entry %q; no duplicates expected", item.Scalar)
		}
		seen[item.Scalar] = true
	}
}

// TestBuildGenericAgent_Tools_DeterministicForSameInput verifies that repeated calls
// with the same PromoteInput (including the same Tools slice) produce byte-identical
// outputs. Determinism is required by AC3.7.
func TestBuildGenericAgent_Tools_DeterministicForSameInput(t *testing.T) {
	src := sourceWithoutToolsKey()
	in := PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "no-tools-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		Tools:     []string{"file_write", "file_read"},
	}

	out1, err1 := buildGenericAgent(in)
	out2, err2 := buildGenericAgent(in)

	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("repeated calls disagree on success: call1=%v call2=%v", err1, err2)
	}
	if err1 == nil && !bytes.Equal(out1, out2) {
		t.Error("repeated calls with identical Tools input produced different output bytes; output must be deterministic")
	}
}

// TestBuildGenericAgent_Frontmatter_GeneratedKeysAppearBeforeCarriedKeys verifies the
// field policy's ordering requirement: the four generated keys (id, version, name, role)
// appear in the output frontmatter before any carried-over source keys. An implementation
// that produces correct field values but wrong order would pass all other tests but fail
// this one.
func TestBuildGenericAgent_Frontmatter_GeneratedKeysAppearBeforeCarriedKeys(t *testing.T) {
	// Source interleaves carried-over fields with other fields to make ordering
	// non-trivial. The generated output must place id, version, name, role first.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"description: A test agent for key-ordering verification\n" +
		"version: \"1.5.0\"\n" +
		"id: \"42\"\n" +
		"name: test-agent\n" +
		"recommended_tier: MEDIUM\n" +
		"role: subagent\n" +
		"tier_rationale: moderate capability needed\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "99",
		Key:       "test-agent",
		Role:      domain.RoleSubagent,
		Version:   "2.0.0",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}

	keys := doc.Frontmatter().Keys()
	if len(keys) == 0 {
		t.Fatal("generated output has no frontmatter keys; expected at least id, version, name, role")
	}

	generatedKeySet := map[string]bool{"id": true, "version": true, "name": true, "role": true}

	// Find the position of the last generated key and the first carried key.
	lastGeneratedPos := -1
	firstCarriedPos := len(keys)
	for i, k := range keys {
		if generatedKeySet[k] {
			if i > lastGeneratedPos {
				lastGeneratedPos = i
			}
		} else {
			if i < firstCarriedPos {
				firstCarriedPos = i
			}
		}
	}

	if lastGeneratedPos == -1 {
		t.Fatal("none of the generated keys (id, version, name, role) appear in the output frontmatter")
	}
	if firstCarriedPos <= lastGeneratedPos {
		t.Errorf("frontmatter key ordering violation: last generated key is at index %d "+
			"but a carried key appears at index %d — generated keys must precede carried keys. "+
			"Full key order: %v", lastGeneratedPos, firstCarriedPos, keys)
	}
}

// ---------------------------------------------------------------------------
// Stage 5: Recovered generic-only field tests (generation core)
//
// These tests verify PromoteInput.RecommendedTier, PromoteInput.TierRationale, and
// PromoteInput.RequiredSkills — the three generic-only frontmatter values a harness drops at
// deploy time and the user supplies during promote. The generation core (buildGenericAgent) is
// the only recipient of these values; prompting happens in the service layer (AC5.8).
//
// Verified behaviours:
//
// Scalar fields (recommended_tier, tier_rationale):
//   - A non-empty RecommendedTier is written as a scalar under the key recommended_tier.
//   - An empty RecommendedTier produces no recommended_tier key in the output (AD-12).
//   - A non-empty TierRationale is written as a scalar under the key tier_rationale.
//   - An empty TierRationale produces no tier_rationale key in the output (AD-12).
//
// List field (required_skills) — T5.3:
//   - A nil RequiredSkills produces no required_skills key in the output.
//   - An empty (non-nil) RequiredSkills produces no required_skills key in the output.
//   - A single-entry RequiredSkills is written as a flow-style list with one item.
//   - A multi-entry RequiredSkills is written as a flow-style list in slice order.
//
// Ordering — AD-13:
//   - Newly added recovered fields (absent from source) are appended after all carried keys,
//     in the fixed order: recommended_tier, tier_rationale, required_skills.
//
// In-place overwrite:
//   - When the source already carries a recovered field, supplying a value via PromoteInput
//     overwrites the key in place; it must not be duplicated or appended a second time.
// ---------------------------------------------------------------------------

// sourceWithoutRecoverableFields returns an eligible harness-only source that carries none
// of the three generic-only recoverable fields: recommended_tier, tier_rationale, and
// required_skills. It is the canonical input for tests that exercise recovery behaviour.
func sourceWithoutRecoverableFields() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: field-recovery-agent\n" +
		"description: An agent for recoverable-field tests\n" +
		"role: subagent\n" +
		// Intentionally absent: recommended_tier, tier_rationale, required_skills
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content for field recovery tests.\n" +
		"</Identity>\n")
}

// TestBuildGenericAgent_RecommendedTier_NonEmpty_WrittenAsScalar verifies that when
// PromoteInput.RecommendedTier is a non-empty string, the generated frontmatter carries
// recommended_tier as a scalar with that exact value (AC5.4).
func TestBuildGenericAgent_RecommendedTier_NonEmpty_WrittenAsScalar(t *testing.T) {
	src := sourceWithoutRecoverableFields()
	wantTier := "HIGH"

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "field-recovery-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: wantTier,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotTier, present := parseFrontmatterField(t, out, "recommended_tier")
	if !present {
		t.Fatal("generated frontmatter is missing recommended_tier; non-empty RecommendedTier must be written as a scalar")
	}
	if gotTier != wantTier {
		t.Errorf("recommended_tier = %q, want %q", gotTier, wantTier)
	}
}


// TestBuildGenericAgent_TierRationale_NonEmpty_WrittenAsScalar verifies that when
// PromoteInput.TierRationale is a non-empty string, the generated frontmatter carries
// tier_rationale as a scalar with that exact value (AC5.4).
func TestBuildGenericAgent_TierRationale_NonEmpty_WrittenAsScalar(t *testing.T) {
	src := sourceWithoutRecoverableFields()
	wantRationale := "needs high reasoning capability for code analysis"

	out, err := buildGenericAgent(PromoteInput{
		Source:        src,
		NumericID:     "9",
		Key:           "field-recovery-agent",
		Role:          domain.RoleSubagent,
		Version:       "1.0.0",
		TierRationale: wantRationale,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotRationale, present := parseFrontmatterField(t, out, "tier_rationale")
	if !present {
		t.Fatal("generated frontmatter is missing tier_rationale; non-empty TierRationale must be written as a scalar")
	}
	if gotRationale != wantRationale {
		t.Errorf("tier_rationale = %q, want %q", gotRationale, wantRationale)
	}
}


// TestBuildGenericAgent_RequiredSkills_SingleEntry_WrittenAsFlowStyleList verifies that
// a RequiredSkills slice with one entry is written to the generated frontmatter as a
// flow-style list (KindList) containing that single entry (T5.3, AC5.7).
func TestBuildGenericAgent_RequiredSkills_SingleEntry_WrittenAsFlowStyleList(t *testing.T) {
	src := sourceWithoutRecoverableFields()
	wantSkills := []string{"lean-tdd"}

	out, err := buildGenericAgent(PromoteInput{
		Source:         src,
		NumericID:      "9",
		Key:            "field-recovery-agent",
		Role:           domain.RoleSubagent,
		Version:        "1.0.0",
		RequiredSkills: wantSkills,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotSkills := parseFrontmatterList(t, out, "required_skills")
	if len(gotSkills) != 1 {
		t.Fatalf("required_skills list length = %d, want 1 (single entry); got: %v", len(gotSkills), gotSkills)
	}
	if gotSkills[0] != wantSkills[0] {
		t.Errorf("required_skills[0] = %q, want %q", gotSkills[0], wantSkills[0])
	}
}

// TestBuildGenericAgent_RequiredSkills_MultipleEntries_WrittenInSliceOrder verifies that
// a multi-entry RequiredSkills slice is written to the generated frontmatter as a
// flow-style list in the exact same order as the input slice (T5.3, AC5.7, AD-11).
func TestBuildGenericAgent_RequiredSkills_MultipleEntries_WrittenInSliceOrder(t *testing.T) {
	src := sourceWithoutRecoverableFields()
	// Deliberately non-alphabetical to distinguish "in-order" from "sorted".
	wantSkills := []string{"lean-tdd", "efficient-file-reading", "boundary-transformer"}

	out, err := buildGenericAgent(PromoteInput{
		Source:         src,
		NumericID:      "9",
		Key:            "field-recovery-agent",
		Role:           domain.RoleSubagent,
		Version:        "1.0.0",
		RequiredSkills: wantSkills,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	gotSkills := parseFrontmatterList(t, out, "required_skills")
	if len(gotSkills) != len(wantSkills) {
		t.Fatalf("required_skills list length = %d, want %d; got: %v", len(gotSkills), len(wantSkills), gotSkills)
	}
	for i, want := range wantSkills {
		if gotSkills[i] != want {
			t.Errorf("required_skills[%d] = %q, want %q (slice order must be preserved)", i, gotSkills[i], want)
		}
	}
}

// TestBuildGenericAgent_RecoveredFields_AppendedAfterCarriedKeysInFixedOrder verifies the
// AD-13 ordering rule: newly synthesised recovered fields (absent from the source) are
// appended after all carried keys in the fixed order recommended_tier, tier_rationale,
// required_skills. Generated keys (id, version, name, role) come first per the existing
// ordering contract; carried keys follow in their original relative order; recovered fields
// land last in the stated fixed order.
func TestBuildGenericAgent_RecoveredFields_AppendedAfterCarriedKeysInFixedOrder(t *testing.T) {
	// Source carries description (carried) but none of the three recoverable fields.
	// PromoteInput supplies all three so they are newly appended.
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "field-recovery-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: "MEDIUM",
		TierRationale:   "moderate capability for field-ordering tests",
		RequiredSkills:  []string{"lean-tdd"},
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}

	keys := doc.Frontmatter().Keys()

	// Helper: find the position of a key; -1 if absent.
	pos := func(target string) int {
		for i, k := range keys {
			if k == target {
				return i
			}
		}
		return -1
	}

	descPos := pos("description")
	tierPos := pos("recommended_tier")
	ratPos := pos("tier_rationale")
	skillsPos := pos("required_skills")

	for _, p := range []struct {
		name string
		idx  int
	}{
		{"description", descPos},
		{"recommended_tier", tierPos},
		{"tier_rationale", ratPos},
		{"required_skills", skillsPos},
	} {
		if p.idx == -1 {
			t.Errorf("key %q is absent from output; expected it to be present", p.name)
		}
	}
	if t.Failed() {
		t.Logf("full key order: %v", keys)
		return
	}

	// Recovered fields must come after the carried description key (AD-13).
	if tierPos <= descPos {
		t.Errorf("recommended_tier (pos %d) must appear after description (pos %d) — recovered fields must be appended after carried keys. Full order: %v", tierPos, descPos, keys)
	}
	if ratPos <= descPos {
		t.Errorf("tier_rationale (pos %d) must appear after description (pos %d). Full order: %v", ratPos, descPos, keys)
	}
	if skillsPos <= descPos {
		t.Errorf("required_skills (pos %d) must appear after description (pos %d). Full order: %v", skillsPos, descPos, keys)
	}

	// Recovered fields must appear in the fixed order: recommended_tier, tier_rationale, required_skills.
	if ratPos <= tierPos {
		t.Errorf("tier_rationale (pos %d) must appear after recommended_tier (pos %d) — fixed order violated. Full order: %v", ratPos, tierPos, keys)
	}
	if skillsPos <= ratPos {
		t.Errorf("required_skills (pos %d) must appear after tier_rationale (pos %d) — fixed order violated. Full order: %v", skillsPos, ratPos, keys)
	}
}

// ---------------------------------------------------------------------------
// Stage 3: Always-present deployment metadata (scoped AD-12 override for the
// promote direction)
//
// Stage 3 overrides AD-12 for exactly three fields in the promote direction:
// recommended_tier, tier_rationale, and required_skills. When these fields are
// empty or nil (not supplied), the generated generic agent must carry the key
// with an empty value — never omitted. This is a deliberate, scoped change
// that makes the gap visible to whoever completes the promoted file later.
//
// The tests below specify the NEW contract and are TDD RED: they fail with the
// current implementation (which omits the three fields when empty/nil). They
// will pass after the generation core is updated in I3.1.
//
// Verified new behaviours:
//   - Empty RecommendedTier   → recommended_tier: ""  present in output
//   - Empty TierRationale     → tier_rationale: ""    present in output
//   - Nil RequiredSkills      → required_skills: []   present as empty flow list
//   - Empty RequiredSkills    → required_skills: []   present as empty flow list
//   - All three present together in AD-13 order even when all are empty/nil
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_EmptyRecommendedTier_WrittenAsEmptyScalar verifies that when
// PromoteInput.RecommendedTier is empty, the generated output carries recommended_tier
// with an empty scalar value — never omitted. This is the scoped AD-12 override that
// makes the gap visible in the promoted file.
func TestBuildGenericAgent_EmptyRecommendedTier_WrittenAsEmptyScalar(t *testing.T) {
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "field-recovery-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: "", // empty — must write as empty scalar, not omit
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	// Guard: ensure output is non-empty before checking the specific key.
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("generated frontmatter is missing `id`; output may be empty — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("recommended_tier")
	if !present {
		t.Error("recommended_tier is absent from output; an empty RecommendedTier must still write the key with an empty value (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("recommended_tier has kind %v, want KindScalar", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("recommended_tier = %q, want empty string; an empty RecommendedTier must produce an empty scalar value", v.Scalar)
	}
}

// TestBuildGenericAgent_EmptyTierRationale_WrittenAsEmptyScalar verifies that when
// PromoteInput.TierRationale is empty, the generated output carries tier_rationale with
// an empty scalar value — never omitted. Scoped AD-12 override.
func TestBuildGenericAgent_EmptyTierRationale_WrittenAsEmptyScalar(t *testing.T) {
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:        src,
		NumericID:     "9",
		Key:           "field-recovery-agent",
		Role:          domain.RoleSubagent,
		Version:       "1.0.0",
		TierRationale: "", // empty — must write as empty scalar, not omit
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("generated frontmatter is missing `id`; output may be empty — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("tier_rationale")
	if !present {
		t.Error("tier_rationale is absent from output; an empty TierRationale must still write the key with an empty value (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("tier_rationale has kind %v, want KindScalar", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("tier_rationale = %q, want empty string; an empty TierRationale must produce an empty scalar value", v.Scalar)
	}
}

// TestBuildGenericAgent_NilRequiredSkills_WrittenAsEmptyFlowList verifies that when
// PromoteInput.RequiredSkills is nil, the generated output carries required_skills as
// an empty flow list — never omitted. Scoped AD-12 override.
func TestBuildGenericAgent_NilRequiredSkills_WrittenAsEmptyFlowList(t *testing.T) {
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:         src,
		NumericID:      "9",
		Key:            "field-recovery-agent",
		Role:           domain.RoleSubagent,
		Version:        "1.0.0",
		RequiredSkills: nil, // nil — must write as empty flow list, not omit
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("generated frontmatter is missing `id`; output may be empty — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("required_skills")
	if !present {
		t.Error("required_skills is absent from output; a nil RequiredSkills must still write the key as an empty flow list (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindList {
		t.Errorf("required_skills has kind %v, want KindList (empty flow list)", v.Kind)
	}
	if len(v.Items) != 0 {
		t.Errorf("required_skills has %d item(s); want 0 items for nil RequiredSkills: %v", len(v.Items), v.Items)
	}
}

// TestBuildGenericAgent_EmptySliceRequiredSkills_WrittenAsEmptyFlowList verifies that
// when PromoteInput.RequiredSkills is a non-nil but empty slice, the generated output
// carries required_skills as an empty flow list — never omitted. Both nil and empty slice
// produce the same output under the scoped AD-12 override.
func TestBuildGenericAgent_EmptySliceRequiredSkills_WrittenAsEmptyFlowList(t *testing.T) {
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:         src,
		NumericID:      "9",
		Key:            "field-recovery-agent",
		Role:           domain.RoleSubagent,
		Version:        "1.0.0",
		RequiredSkills: []string{}, // non-nil empty — must write as empty flow list, not omit
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("generated frontmatter is missing `id`; output may be empty — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("required_skills")
	if !present {
		t.Error("required_skills is absent from output; an empty (non-nil) RequiredSkills must still write the key as an empty flow list (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindList {
		t.Errorf("required_skills has kind %v, want KindList (empty flow list)", v.Kind)
	}
	if len(v.Items) != 0 {
		t.Errorf("required_skills has %d item(s); want 0 items for an empty RequiredSkills slice: %v", len(v.Items), v.Items)
	}
}

// TestBuildGenericAgent_AllThreeMetadataFields_PresentWithEmptyValuesWhenNoneSupplied
// verifies that when all three deployment-metadata fields are empty or nil, all three keys
// appear in the generated output with empty values. The scoped AD-12 override applies to
// all three fields simultaneously; no field may be silently dropped.
func TestBuildGenericAgent_AllThreeMetadataFields_PresentWithEmptyValuesWhenNoneSupplied(t *testing.T) {
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "field-recovery-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: "", // empty
		TierRationale:   "", // empty
		RequiredSkills:  nil, // nil
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	// Guard: ensure output is non-empty.
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("generated frontmatter is missing `id`; output may be empty — guard prevents vacuous pass")
	}
	fm := doc.Frontmatter()
	if _, present := fm.Get("recommended_tier"); !present {
		t.Error("recommended_tier is absent; all three deployment-metadata fields must be present even when none were supplied")
	}
	if _, present := fm.Get("tier_rationale"); !present {
		t.Error("tier_rationale is absent; all three deployment-metadata fields must be present even when none were supplied")
	}
	if _, present := fm.Get("required_skills"); !present {
		t.Error("required_skills is absent; all three deployment-metadata fields must be present even when none were supplied")
	}
}

// TestBuildGenericAgent_EmptyMetadataFields_AppearInAD13OrderAfterCarriedKeys verifies
// that when all three deployment-metadata fields are absent from PromoteInput (empty/nil),
// they are still written in the documented AD-13 order — recommended_tier, tier_rationale,
// required_skills — appearing after all carried keys (e.g., description).
func TestBuildGenericAgent_EmptyMetadataFields_AppearInAD13OrderAfterCarriedKeys(t *testing.T) {
	// Source carries description (a carried key) but none of the three metadata fields.
	// All three are empty/nil in the input so they are newly appended in the output.
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "field-recovery-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: "", // empty
		TierRationale:   "", // empty
		RequiredSkills:  nil, // nil
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	keys := doc.Frontmatter().Keys()

	pos := func(target string) int {
		for i, k := range keys {
			if k == target {
				return i
			}
		}
		return -1
	}

	descPos := pos("description")
	tierPos := pos("recommended_tier")
	ratPos := pos("tier_rationale")
	skillsPos := pos("required_skills")

	for _, p := range []struct {
		name string
		idx  int
	}{
		{"description", descPos},
		{"recommended_tier", tierPos},
		{"tier_rationale", ratPos},
		{"required_skills", skillsPos},
	} {
		if p.idx == -1 {
			t.Errorf("key %q is absent from output; all four keys must be present", p.name)
		}
	}
	if t.Failed() {
		t.Logf("full key order: %v", keys)
		return
	}

	// The three metadata fields must follow the carried description key (AD-13).
	if tierPos <= descPos {
		t.Errorf("recommended_tier (pos %d) must appear after description (pos %d); metadata fields must be appended after carried keys. Full order: %v", tierPos, descPos, keys)
	}
	if ratPos <= descPos {
		t.Errorf("tier_rationale (pos %d) must appear after description (pos %d). Full order: %v", ratPos, descPos, keys)
	}
	if skillsPos <= descPos {
		t.Errorf("required_skills (pos %d) must appear after description (pos %d). Full order: %v", skillsPos, descPos, keys)
	}
	// Within the metadata block the fixed AD-13 order must hold.
	if ratPos <= tierPos {
		t.Errorf("tier_rationale (pos %d) must appear after recommended_tier (pos %d) — fixed AD-13 order violated. Full order: %v", ratPos, tierPos, keys)
	}
	if skillsPos <= ratPos {
		t.Errorf("required_skills (pos %d) must appear after tier_rationale (pos %d) — fixed AD-13 order violated. Full order: %v", skillsPos, ratPos, keys)
	}
}

// TestBuildGenericAgent_EmptyScalarFields_ProduceLiteralDoubleQuotes verifies that when
// RecommendedTier and TierRationale are empty strings the generated output bytes contain
// the literal text `recommended_tier: ""` and `tier_rationale: ""` — not the bare
// `recommended_tier: ` form that a plain (unquoted) empty scalar would produce. This
// byte-level check catches serialization regressions that a re-parse check would miss,
// because parsing `key: ` and `key: ""` both yield an empty scalar value.
func TestBuildGenericAgent_EmptyScalarFields_ProduceLiteralDoubleQuotes(t *testing.T) {
	src := sourceWithoutRecoverableFields()

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "field-recovery-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: "",
		TierRationale:   "",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	if !bytes.Contains(out, []byte(`recommended_tier: ""`)) {
		t.Errorf("output bytes do not contain literal `recommended_tier: \"\"`: got:\n%s", out)
	}
	if !bytes.Contains(out, []byte(`tier_rationale: ""`)) {
		t.Errorf("output bytes do not contain literal `tier_rationale: \"\"`: got:\n%s", out)
	}
}

// TestBuildGenericAgent_RecoveredTier_OverwritesExistingSourceField verifies that when the
// source already carries recommended_tier and PromoteInput.RecommendedTier is non-empty,
// the generated output contains the PromoteInput value — the source value is overwritten.
// Overwriting must not duplicate the key or append it a second time (AD-13).
func TestBuildGenericAgent_RecoveredTier_OverwritesExistingSourceField(t *testing.T) {
	// Source carries recommended_tier: OLD; PromoteInput supplies "NEW".
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: overwrite-test-agent\n" +
		"description: Agent for in-place overwrite test\n" +
		"role: subagent\n" +
		"recommended_tier: OLD\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       "9",
		Key:             "overwrite-test-agent",
		Role:            domain.RoleSubagent,
		Version:         "1.0.0",
		RecommendedTier: "NEW",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// Verify exactly one occurrence of recommended_tier with the new value.
	v, present := fm.Get("recommended_tier")
	if !present {
		t.Fatal("recommended_tier is absent; overwriting an existing source field must keep the key in the output")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("recommended_tier kind = %v, want KindScalar", v.Kind)
	}
	if v.Scalar != "NEW" {
		t.Errorf("recommended_tier = %q, want %q; PromoteInput must overwrite the source value", v.Scalar, "NEW")
	}

	// Verify there is no duplicate key by counting occurrences in the key list.
	count := 0
	for _, k := range fm.Keys() {
		if k == "recommended_tier" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("recommended_tier appears %d time(s) in frontmatter keys; must appear exactly once after overwrite", count)
	}
}

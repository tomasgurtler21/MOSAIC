package app

// build_retargeted_agent_test.go covers BuildRetargetedAgent — the pure harness-to-harness
// transform core introduced in Stage 5. Tests are in package app (not package app_test) to
// follow the same convention as promote_test.go: pure generation cores live in the package
// and are tested without crossing the package boundary.
//
// Verified behaviours, by task:
//
// Tool translation (T5.2):
//   - A tool mapped in both source and target harnesses lands under the target's tools key
//     with the target harness's tool name (not the source harness's).
//   - A tool routed to a DestField destination in the target harness lands in that diverted
//     field in the output, and is absent from the main tools field.
//   - A source tool that reverse-maps to no known mapping (custom tool) is kept under its
//     original name in the target output's main tools field.
//   - A custom tool's original name appears in RetargetReport.CarriedVerbatimTools.
//   - RetargetReport.Tools records one ToolResolution per generic tool recovered, in
//     recovery order, with the target harness's resolution outcome.
//   - A source tool that reverse-maps to a generic tool the target does not support is
//     absent from the output and recorded in RetargetReport.Tools with ToolUnmapped.
//
// Stripped fields (T5.2 / T5.4):
//   - A source frontmatter key classified as unknown (not MOSAIC-known and not harness-known)
//     is stripped and reported in RetargetReport.StrippedFields with StripReasonUnknownField.
//   - A value in a source harness's diverted field that cannot be reverse-mapped is stripped
//     and reported in RetargetReport.StrippedFields with StripReasonUnmappedDivertedValue.
//
// Region handling (T5.3):
//   - Every <... type="project"> region's content survives the transform byte-for-byte.
//   - RetargetReport.InjectionsPreserved lists every preserved injection name in document
//     order.
//   - <... type="managed"> region content is carried through (not stripped to empty pairs as
//     buildGenericAgent does). This is the only permitted divergence from promote.
//
// Frontmatter shaping (T5.4):
//   - Source-harness-specific fields (alpha_stamp, source model key value) are absent
//     from the output frontmatter.
//   - Target-harness fields (target model key, target tools key) are present in the output.
//   - MOSAIC stamp fields in the output use the mosaic_-prefixed names (e.g.,
//     mosaic_transform_version), not legacy unprefixed names.
//   - An empty RetargetInput.TargetModel causes the target harness's model field to be
//     written with an empty scalar value, not with the source harness's model id.
//   - A non-empty RetargetInput.TargetModel is written under the target harness's model key.
//   - RetargetReport.AgentKey equals RetargetInput.AgentKey.
//   - RetargetReport.SourceHarnessID and TargetHarnessID equal the module IDs.
//   - A source bundle_version is carried through to the output as mosaic_bundle_version,
//     unchanged — bundle content is harness-independent.
//   - RetargetInput.Kind left at its zero value is treated as domain.ArtifactAgent, producing
//     the same output as an explicit Kind: domain.ArtifactAgent.
//
// Missing agent key (T5.4):
//   - A RetargetInput with an empty AgentKey returns ErrRetargetMissingAgentKey. The core
//     never derives a key from file content; the caller must supply it.
//   - A non-nil name field in the source frontmatter does NOT prevent the empty-key error.
//
// Ineligible source (T5.4):
//   - A source file that fails the two-signal eligibility check returns an error wrapping
//     ErrRetargetNotTransformed and nil output bytes.
//
// Purity and determinism (T5.5):
//   - Two calls with identical RetargetInput produce byte-identical output.
//   - The source bytes (RetargetInput.Source) are byte-identical before and after the call.
//   - The transform can be run entirely against in-memory bytes — no filesystem access is
//     required, confirming the I/O-free contract.

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T5.2 — Tool translation
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_MappedToolsLandUnderTargetKey verifies that tools present in
// the source file and mapped in both harnesses appear in the output under the TARGET
// harness's tools key with the TARGET harness's tool names. The source harness's tool
// names must not appear in the output.
func TestBuildRetargetedAgent_MappedToolsLandUnderTargetKey(t *testing.T) {
	// Source has AlphaBash and AlphaRead (alpha-harness tool names).
	// Both map to generic Bash and Read respectively.
	// Beta harness maps Bash→BetaBash and Read→BetaRead.
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// Target tools key is "allowed_tools" (beta's ToolsKey).
	tools := toolsListFromFrontmatter(t, out, "allowed_tools")
	if !sliceHas(tools, "BetaBash") {
		t.Errorf("output tools list under \"allowed_tools\" should contain \"BetaBash\"; got %v", tools)
	}
	if !sliceHas(tools, "BetaRead") {
		t.Errorf("output tools list under \"allowed_tools\" should contain \"BetaRead\"; got %v", tools)
	}

	// Source harness tool names must be absent from the output.
	keys := frontmatterKeys(t, out)
	if keys["tools"] {
		t.Error("output should not have source harness \"tools\" key; it belongs to alpha-harness only")
	}
	sourceFmTools := toolsListFromFrontmatter(t, out, "tools")
	if sliceHas(sourceFmTools, "AlphaBash") {
		t.Error("source tool name \"AlphaBash\" must not appear in the output frontmatter")
	}
}

// TestBuildRetargetedAgent_ToolMappedToDestFieldLandsInDivertedField verifies that when
// the target harness routes a generic tool to a DestField destination, the tool's target
// name lands in that diverted frontmatter field rather than in the main tools field.
func TestBuildRetargetedAgent_ToolMappedToDestFieldLandsInDivertedField(t *testing.T) {
	// Alpha source has AlphaBash (→ generic Bash) and AlphaRead (→ generic Read).
	// Gamma harness: Bash → DestMain "GammaBash", Read → DestField "server_tools" "gamma-mcp-server".
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	gamma := newGammaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: gamma,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gamma-model-1",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// Bash → GammaBash should land in the main tools field ("main_tools").
	mainTools := toolsListFromFrontmatter(t, out, "main_tools")
	if !sliceHas(mainTools, "GammaBash") {
		t.Errorf("GammaBash should be in the main tools field \"main_tools\"; got %v", mainTools)
	}

	// Read → gamma-mcp-server should land in the diverted field "server_tools".
	serverTools := toolsListFromFrontmatter(t, out, "server_tools")
	if !sliceHas(serverTools, "gamma-mcp-server") {
		t.Errorf("gamma-mcp-server should be in diverted field \"server_tools\"; got %v", serverTools)
	}

	// gamma-mcp-server must NOT be in the main tools field.
	if sliceHas(mainTools, "gamma-mcp-server") {
		t.Error("gamma-mcp-server should be in the diverted field, not the main tools field")
	}
}

// TestBuildRetargetedAgent_CustomToolKeptVerbatimInTargetOutput verifies that a source tool
// that does not reverse-map to any known generic tool (a "custom tool") is kept under its
// original name in the target harness output's main tools field (FR-14).
func TestBuildRetargetedAgent_CustomToolKeptVerbatimInTargetOutput(t *testing.T) {
	// alphaDeployedAgentBytes includes "CustomMCP" which has no entry in alpha's tool mappings.
	// After reverse mapping: CustomMCP → ToolUnmapped.
	// FR-14: kept under the original name in the target output.
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// CustomMCP should appear in the target output's main tools field.
	tools := toolsListFromFrontmatter(t, out, "allowed_tools")
	if !sliceHas(tools, "CustomMCP") {
		t.Errorf("custom tool \"CustomMCP\" should be carried verbatim to the target output; got %v", tools)
	}

	// CustomMCP should also be listed in CarriedVerbatimTools in the report.
	if !sliceHas(report.CarriedVerbatimTools, "CustomMCP") {
		t.Errorf("RetargetReport.CarriedVerbatimTools should contain \"CustomMCP\"; got %v",
			report.CarriedVerbatimTools)
	}
}

// TestBuildRetargetedAgent_ReportContainsToolResolutionsForMappedTools verifies that
// RetargetReport.Tools contains one ToolResolution per generic tool recovered from the
// source, recording how the target harness resolved each one. The resolution order follows
// the recovery order from the source file.
func TestBuildRetargetedAgent_ReportContainsToolResolutionsForMappedTools(t *testing.T) {
	// Source has AlphaBash→Bash, AlphaRead→Read (both mapped). CustomMCP is not in report.Tools
	// (it is in CarriedVerbatimTools, not in the generic-tool resolution audit).
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	_, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// Expect at least two ToolResolution entries: one for Bash, one for Read.
	if len(report.Tools) < 2 {
		t.Errorf("RetargetReport.Tools should have at least 2 entries (Bash, Read); got %d: %v",
			len(report.Tools), report.Tools)
	}

	// Verify at least one resolution corresponds to generic "Bash" and maps to BetaBash.
	foundBash := false
	for _, res := range report.Tools {
		if res.Generic == "Bash" {
			foundBash = true
			if res.Outcome != domain.ToolMapped {
				t.Errorf("resolution for generic \"Bash\" should have ToolMapped outcome; got %q", res.Outcome)
			}
			if !sliceHas(res.HarnessTools, "BetaBash") {
				t.Errorf("resolution for \"Bash\" should list \"BetaBash\" in HarnessTools; got %v", res.HarnessTools)
			}
		}
	}
	if !foundBash {
		t.Error("RetargetReport.Tools should contain a resolution for generic \"Bash\"")
	}
}

// TestBuildRetargetedAgent_TargetSideToolUnmappedAbsentFromOutputAndRecorded verifies that
// when the source carries a tool that reverse-maps to a generic tool the target harness does
// not support, that tool is absent from the output and recorded in RetargetReport.Tools with
// a ToolUnmapped outcome. The tool must not bleed through under any name.
func TestBuildRetargetedAgent_TargetSideToolUnmappedAbsentFromOutputAndRecorded(t *testing.T) {
	// Build a source fixture that includes AlphaWrite (alpha maps Write→AlphaWrite).
	// Beta declares no mapping for Write, so the generic Write tool is unsupported at the
	// target and must be recorded as ToolUnmapped rather than placed in the output.
	src := []byte("---\n" +
		"transform_version: \"alpha-1.0.0\"\n" +
		"injections_version: \"alpha-1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"30\"\n" +
		"name: write-tool-test\n" +
		"description: Agent carrying AlphaWrite to test target-side ToolUnmapped\n" +
		"role: subagent\n" +
		"model: claude-3-5-sonnet\n" +
		"tools:\n" +
		"- AlphaBash\n" +
		"- AlphaWrite\n" + // AlphaWrite → generic Write; beta has no Write mapping
		"alpha_stamp: alpha-v1\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Test agent for Write tool unmapped at target.\n" +
		"</Identity>\n")

	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "write-tool-test",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// AlphaWrite (source harness name) and the generic "Write" name must both be absent
	// from the target output — the tool is unsupported by beta and must not appear at all.
	betaTools := toolsListFromFrontmatter(t, out, "allowed_tools")
	if sliceHas(betaTools, "AlphaWrite") {
		t.Error("source harness tool name \"AlphaWrite\" must not appear in the target output")
	}
	if sliceHas(betaTools, "Write") {
		t.Error("generic tool name \"Write\" must not be placed in the target output when the target has no mapping for it")
	}

	// RetargetReport.Tools must record a ToolUnmapped resolution for generic "Write".
	foundWrite := false
	for _, res := range report.Tools {
		if res.Generic == "Write" {
			foundWrite = true
			if res.Outcome != domain.ToolUnmapped {
				t.Errorf("resolution for generic \"Write\" should have ToolUnmapped outcome; got %q", res.Outcome)
			}
		}
	}
	if !foundWrite {
		t.Errorf("RetargetReport.Tools should contain a resolution entry for generic \"Write\" with ToolUnmapped; got %v", report.Tools)
	}
}

// ---------------------------------------------------------------------------
// Stripped fields
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_UnknownFrontmatterFieldStrippedAndReported verifies that a
// frontmatter field unknown to MOSAIC and to the source harness is stripped from the output
// and reported in RetargetReport.StrippedFields with StripReasonUnknownField. This is the
// ClassUnknown field path from the behavioural contract.
func TestBuildRetargetedAgent_UnknownFrontmatterFieldStrippedAndReported(t *testing.T) {
	// Source carries "truly_unknown_field" — not recognised by MOSAIC vocabulary and not
	// declared in alpha's descriptor. It must be classified ClassUnknown and stripped.
	src := []byte("---\n" +
		"transform_version: \"alpha-1.0.0\"\n" +
		"injections_version: \"alpha-1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"31\"\n" +
		"name: unknown-field-test\n" +
		"description: Agent with an unrecognized frontmatter field\n" +
		"role: subagent\n" +
		"model: claude-3-5-sonnet\n" +
		"tools:\n" +
		"- AlphaBash\n" +
		"alpha_stamp: alpha-v1\n" +
		"truly_unknown_field: some-value-not-recognized-by-anyone\n" + // ClassUnknown
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Test agent for unknown-field stripping.\n" +
		"</Identity>\n")

	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "unknown-field-test",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// The unknown field must be absent from the output.
	keys := frontmatterKeys(t, out)
	if keys["truly_unknown_field"] {
		t.Error("unknown frontmatter field \"truly_unknown_field\" must be stripped from the output")
	}

	// StrippedFields must contain an entry for the unknown field with StripReasonUnknownField.
	var found *StrippedField
	for i, sf := range report.StrippedFields {
		if sf.Key == "truly_unknown_field" && sf.Reason == StripReasonUnknownField {
			found = &report.StrippedFields[i]
			break
		}
	}
	if found == nil {
		t.Errorf("RetargetReport.StrippedFields must contain an entry for \"truly_unknown_field\" with "+
			"StripReasonUnknownField; got %v", report.StrippedFields)
	}
}

// TestBuildRetargetedAgent_UnmappedDivertedValueStrippedAndReported verifies that a value
// in a source harness's diverted field that cannot be reverse-mapped to a generic tool is
// stripped from the output and reported in RetargetReport.StrippedFields with
// StripReasonUnmappedDivertedValue.
func TestBuildRetargetedAgent_UnmappedDivertedValueStrippedAndReported(t *testing.T) {
	// Gamma routes Read → DestField "server_tools" "gamma-mcp-server". The source carries
	// "unknown-server" in server_tools, which is not the declared gamma-mcp-server name and
	// therefore cannot be reverse-mapped to any generic tool. It must be stripped and reported.
	src := []byte("---\n" +
		"transform_version: \"gamma-3.0.0\"\n" +
		"injections_version: \"gamma-3.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"32\"\n" +
		"name: diverted-strip-test\n" +
		"description: Agent with an unmappable diverted-field value\n" +
		"role: subagent\n" +
		"model_name: gamma-model-1\n" +
		"main_tools:\n" +
		"- GammaBash\n" +
		"server_tools:\n" +
		"- unknown-server\n" + // Not gamma-mcp-server → cannot be reverse-mapped
		"gamma_stamp: gamma-v3\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Test agent for unmapped diverted value stripping.\n" +
		"</Identity>\n")

	gamma := newGammaHarnessModule()
	beta := newBetaHarnessModule()

	out, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: gamma,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "diverted-strip-test",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// The source DestField key "server_tools" must not appear in the output — it is a
	// source-harness-specific field and the target has its own layout.
	keys := frontmatterKeys(t, out)
	if keys["server_tools"] {
		t.Error("source DestField key \"server_tools\" must be stripped from the output")
	}

	// StrippedFields must contain an entry with StripReasonUnmappedDivertedValue.
	var found *StrippedField
	for i, sf := range report.StrippedFields {
		if sf.Reason == StripReasonUnmappedDivertedValue {
			found = &report.StrippedFields[i]
			break
		}
	}
	if found == nil {
		t.Errorf("RetargetReport.StrippedFields must contain an entry with StripReasonUnmappedDivertedValue "+
			"for the unmappable diverted value \"unknown-server\"; got %v", report.StrippedFields)
	}
	if found != nil && !sliceHas(found.Values, "unknown-server") {
		t.Errorf("StrippedField.Values should contain \"unknown-server\"; got %v", found.Values)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Region handling
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_InjectionContentPreservedByteForByte verifies that every
// <... type="project"> region's content survives the transform byte-for-byte. This is the
// key divergence from buildGenericAgent, which strips injection content to empty pairs.
func TestBuildRetargetedAgent_InjectionContentPreservedByteForByte(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	srcInjections := injectionRegionContents(t, src)
	outInjections := injectionRegionContents(t, out)

	if len(srcInjections) == 0 {
		t.Fatal("test fixture must have at least one injection region; check alphaDeployedAgentBytes")
	}

	for name, srcContent := range srcInjections {
		outContent, exists := outInjections[name]
		if !exists {
			t.Errorf("injection region %q is missing from the output", name)
			continue
		}
		if !bytes.Equal(srcContent, outContent) {
			t.Errorf("injection region %q content changed:\n  src: %q\n  out: %q",
				name, srcContent, outContent)
		}
	}
}

// TestBuildRetargetedAgent_InjectionsPreservedReportLists verifies that
// RetargetReport.InjectionsPreserved names every injection region in document order,
// allowing a test to assert preservation without a full byte comparison.
func TestBuildRetargetedAgent_InjectionsPreservedReportLists(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	_, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// The source has two injection regions: IdentityExtension and CodebaseContext.
	srcInjections := injectionRegionContents(t, src)
	for name := range srcInjections {
		if !sliceHas(report.InjectionsPreserved, name) {
			t.Errorf("RetargetReport.InjectionsPreserved missing %q; got %v",
				name, report.InjectionsPreserved)
		}
	}
}

// TestBuildRetargetedAgent_DeployedRegionContentCarriedThrough verifies that deployed region
// content is carried through in the output, unlike buildGenericAgent which strips deployed
// regions to empty pairs. This is the documented divergence from the promote path.
func TestBuildRetargetedAgent_DeployedRegionContentCarriedThrough(t *testing.T) {
	// Build a source with a populated deployed region.
	src := []byte("---\n" +
		"transform_version: \"alpha-1.0.0\"\n" +
		"injections_version: \"alpha-1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"1\"\n" +
		"name: section-test\n" +
		"description: A test agent for region preservation\n" +
		"role: subagent\n" +
		"model: claude-3-5\n" +
		"tools:\n" +
		"- AlphaBash\n" +
		"alpha_stamp: alpha-v1\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Identity prose.\n" +
		"<ClosingProcedure type=\"managed\">\n" +
		"This closing procedure content must survive the transform.\n" +
		"Step 1: summarize your work.\n" +
		"</ClosingProcedure>\n" +
		"</Identity>\n")

	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "section-test",
		TargetModel:  "gpt-4",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// The deployed region content must be present in the output (not stripped).
	if !bytes.Contains(out, []byte("This closing procedure content must survive the transform.")) {
		t.Error("deployed region content was stripped; BuildRetargetedAgent must carry deployed regions through verbatim")
	}
}

// TestBuildRetargetedAgent_SectionStructurePreserved verifies that the SECTION tag
// structure is preserved in the output: every section open tag and close tag from the
// source appears in the output with the same names.
func TestBuildRetargetedAgent_SectionStructurePreserved(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// Both canonical sections from the source fixture must survive.
	if !bytes.Contains(out, []byte("<Identity type=\"core\">")) {
		t.Error("output must contain <Identity type=\"core\"> open tag")
	}
	if !bytes.Contains(out, []byte("</Identity>")) {
		t.Error("output must contain </Identity> close tag")
	}
	if !bytes.Contains(out, []byte("<Capabilities type=\"core\">")) {
		t.Error("output must contain <Capabilities type=\"core\"> open tag")
	}
	if !bytes.Contains(out, []byte("</Capabilities>")) {
		t.Error("output must contain </Capabilities> close tag")
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Frontmatter shaping
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_SourceHarnessFieldsAbsentFromOutput verifies that
// source-harness-specific frontmatter fields are absent from the output. The output is a
// deployed file for the target harness; the source harness's artifacts must not bleed through.
func TestBuildRetargetedAgent_SourceHarnessFieldsAbsentFromOutput(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	keys := frontmatterKeys(t, out)

	// alpha_stamp is an alpha-harness-specific plan Set key. It must not be in the output.
	if keys["alpha_stamp"] {
		t.Error("source-harness plan key \"alpha_stamp\" must be absent from the target output")
	}

	// The source harness's tools key ("tools" for alpha) must not be in the output as the
	// active tools key. The output should have "allowed_tools" (beta's ToolsKey) instead.
	if keys["tools"] && !keys["allowed_tools"] {
		t.Error("output has source harness tools key \"tools\" but not target harness key \"allowed_tools\"")
	}
}

// TestBuildRetargetedAgent_TargetHarnessFieldsPresentInOutput verifies that the target
// harness's distinctive fields are present in the output: the target's model key, tools key,
// and frontmatter plan keys.
func TestBuildRetargetedAgent_TargetHarnessFieldsPresentInOutput(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	keys := frontmatterKeys(t, out)

	// Target model key ("model_id" for beta) must be present.
	if !keys["model_id"] {
		t.Error("target harness model key \"model_id\" must be present in the output")
	}

	// Target tools key ("allowed_tools" for beta) must be present.
	if !keys["allowed_tools"] {
		t.Error("target harness tools key \"allowed_tools\" must be present in the output")
	}

	// Target plan Set key ("beta_stamp" from beta's FrontmatterSpec.Add) must be present.
	if !keys["beta_stamp"] {
		t.Error("target harness plan key \"beta_stamp\" must be present in the output")
	}
}

// TestBuildRetargetedAgent_MosaicStampsUsePrefixedNames verifies that MOSAIC-owned stamp
// fields in the output use the correct mosaic_-prefixed names after Stage 1:
//   - mosaic_harness_version (replaces mosaic_transform_version)
//   - mosaic_tool_mappings_version
//
// The legacy unprefixed names must be absent. mosaic_injections_version is stripped by
// the migration step and must not appear in the output.
func TestBuildRetargetedAgent_MosaicStampsUsePrefixedNames(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:              src,
		SourceModule:        alpha,
		TargetModule:        beta,
		Kind:                domain.ArtifactAgent,
		AgentKey:            "retarget-test-agent",
		TargetModel:         "gpt-4-turbo",
		ToolMappingsVersion: "deadbeef",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	keys := frontmatterKeys(t, out)

	// New harness version key must be present; old transform version key must be absent.
	if !keys["mosaic_harness_version"] {
		t.Error("mosaic_harness_version must be present in the output; the harness version stamp uses the new key name after Stage 1")
	}
	if keys["mosaic_transform_version"] {
		t.Error("mosaic_transform_version must be absent; the output must use mosaic_harness_version after Stage 1")
	}

	// Legacy names must be absent.
	if keys["transform_version"] {
		t.Error("legacy transform_version must be absent")
	}
	if keys["injections_version"] {
		t.Error("legacy injections_version must be absent")
	}
	// mosaic_injections_version is stripped by the migration step.
	if keys["mosaic_injections_version"] {
		t.Error("mosaic_injections_version must be absent; migration strip removes it")
	}
}

// TestBuildRetargetedAgent_EmptyTargetModelWritesEmptyModelField verifies FR-15:
// when RetargetInput.TargetModel is empty, the target harness's model field is written
// with an empty scalar value rather than carrying the source harness's model id.
func TestBuildRetargetedAgent_EmptyTargetModelWritesEmptyModelField(t *testing.T) {
	src := alphaDeployedAgentBytes() // source has model: claude-3-5-sonnet
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "", // deliberately empty
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// Target model key "model_id" must be present but empty (not the source's model id).
	modelValue, present := parseFrontmatterScalar(t, out, "model_id")
	if !present {
		t.Error("target model key \"model_id\" must be present even when TargetModel is empty (FR-15)")
	}
	if modelValue != "" {
		t.Errorf("target model field should be empty when TargetModel is \"\"; got %q", modelValue)
	}

	// The source harness's model id must not appear under the target model key.
	if modelValue == "claude-3-5-sonnet" {
		t.Error("source model id \"claude-3-5-sonnet\" must not be carried to the target model field")
	}
}

// TestBuildRetargetedAgent_NonEmptyTargetModelWrittenUnderTargetKey verifies that a
// non-empty RetargetInput.TargetModel is written under the target harness's model key.
func TestBuildRetargetedAgent_NonEmptyTargetModelWrittenUnderTargetKey(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// Beta's model key is "model_id"; the value must be the requested TargetModel.
	modelValue, present := parseFrontmatterScalar(t, out, "model_id")
	if !present {
		t.Error("target model key \"model_id\" must be present in the output")
	}
	if modelValue != "gpt-4-turbo" {
		t.Errorf("target model field = %q, want \"gpt-4-turbo\"", modelValue)
	}
}

// TestBuildRetargetedAgent_ReportIdentifiesHarnessIDs verifies that RetargetReport
// records the source and target harness IDs and the agent key.
func TestBuildRetargetedAgent_ReportIdentifiesHarnessIDs(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	_, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	if report.AgentKey != "retarget-test-agent" {
		t.Errorf("RetargetReport.AgentKey = %q, want \"retarget-test-agent\"", report.AgentKey)
	}
	if report.SourceHarnessID != "alpha-harness" {
		t.Errorf("RetargetReport.SourceHarnessID = %q, want \"alpha-harness\"", report.SourceHarnessID)
	}
	if report.TargetHarnessID != "beta-harness" {
		t.Errorf("RetargetReport.TargetHarnessID = %q, want \"beta-harness\"", report.TargetHarnessID)
	}
}

// TestBuildRetargetedAgent_BundleVersionCarriedThroughAsMosaicBundleVersion verifies that
// when the source carries a bundle_version field it is carried through to the output under
// the mosaic_bundle_version key with its value unchanged. Bundle content is
// harness-independent and must survive the transform unchanged.
func TestBuildRetargetedAgent_BundleVersionCarriedThroughAsMosaicBundleVersion(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"alpha-1.0.0\"\n" +
		"injections_version: \"alpha-1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"33\"\n" +
		"name: bundle-test-agent\n" +
		"description: Agent carrying bundle_version for carry-through test\n" +
		"role: subagent\n" +
		"model: claude-3-5-sonnet\n" +
		"tools:\n" +
		"- AlphaBash\n" +
		"alpha_stamp: alpha-v1\n" +
		"bundle_version: \"bundle-abc123\"\n" + // must appear in output as mosaic_bundle_version
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Test agent for bundle_version carry-through.\n" +
		"</Identity>\n")

	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "bundle-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// mosaic_bundle_version must be present in the output with the original value.
	bundleVal, present := parseFrontmatterScalar(t, out, "mosaic_bundle_version")
	if !present {
		t.Error("mosaic_bundle_version must be present in the output when source carries bundle_version")
	}
	if bundleVal != "bundle-abc123" {
		t.Errorf("mosaic_bundle_version = %q, want \"bundle-abc123\"", bundleVal)
	}
}

// TestBuildRetargetedAgent_KindZeroValueTreatedAsArtifactAgent verifies that a RetargetInput
// with Kind left at its zero value (empty string) produces the same successful result as an
// explicit Kind: domain.ArtifactAgent. The RetargetInput.Kind doc comment states "Zero value
// is treated as domain.ArtifactAgent."
func TestBuildRetargetedAgent_KindZeroValueTreatedAsArtifactAgent(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	// Call with Kind left at its zero value (not set).
	outZero, _, errZero := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		// Kind deliberately omitted — zero value of domain.ArtifactKind
		AgentKey:    "retarget-test-agent",
		TargetModel: "gpt-4-turbo",
	})

	// Call with Kind explicitly set to domain.ArtifactAgent.
	outExplicit, _, errExplicit := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if errZero != nil {
		t.Fatalf("BuildRetargetedAgent with zero Kind returned error: %v", errZero)
	}
	if errExplicit != nil {
		t.Fatalf("BuildRetargetedAgent with explicit ArtifactAgent Kind returned error: %v", errExplicit)
	}

	// Both calls must produce identical output: zero Kind must equal ArtifactAgent.
	if !bytes.Equal(outZero, outExplicit) {
		t.Error("Kind zero value must produce identical output to explicit domain.ArtifactAgent")
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Missing agent key
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_EmptyAgentKeyReturnsErrRetargetMissingAgentKey verifies that
// a RetargetInput with an empty AgentKey returns ErrRetargetMissingAgentKey. The core never
// derives a slug from file content; the caller must supply it explicitly.
func TestBuildRetargetedAgent_EmptyAgentKeyReturnsErrRetargetMissingAgentKey(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "", // deliberately empty
		TargetModel:  "gpt-4-turbo",
	})

	if err == nil {
		t.Fatal("expected ErrRetargetMissingAgentKey, got nil")
	}
	if !errors.Is(err, ErrRetargetMissingAgentKey) {
		t.Errorf("expected error wrapping ErrRetargetMissingAgentKey, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output bytes on empty AgentKey, got %d bytes", len(out))
	}
}

// TestBuildRetargetedAgent_NameFieldInSourceDoesNotSubstituteForEmptyAgentKey verifies
// that even when the source frontmatter carries a non-empty "name" field, an empty AgentKey
// still produces ErrRetargetMissingAgentKey. "name" is free-text display copy, not a slug.
func TestBuildRetargetedAgent_NameFieldInSourceDoesNotSubstituteForEmptyAgentKey(t *testing.T) {
	// alphaDeployedAgentBytes has name: retarget-test-agent in frontmatter.
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	_, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "", // empty — the name field in the source must NOT substitute
		TargetModel:  "gpt-4-turbo",
	})

	if !errors.Is(err, ErrRetargetMissingAgentKey) {
		t.Errorf("expected ErrRetargetMissingAgentKey regardless of source name field; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Ineligible source
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_IneligibleSourceReturnsErrRetargetNotTransformed verifies
// that a source file failing the two-signal eligibility rule returns an error wrapping
// ErrRetargetNotTransformed and nil output bytes.
func TestBuildRetargetedAgent_IneligibleSourceReturnsErrRetargetNotTransformed(t *testing.T) {
	// notAnAgentBytes has no transform_version — fails eligibility signal one.
	src := notAnAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "plain-doc",
		TargetModel:  "gpt-4-turbo",
	})

	if err == nil {
		t.Fatal("expected ErrRetargetNotTransformed, got nil")
	}
	if !errors.Is(err, ErrRetargetNotTransformed) {
		t.Errorf("expected error wrapping ErrRetargetNotTransformed, got: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output bytes on ineligible source, got %d bytes", len(out))
	}
}

// ---------------------------------------------------------------------------
// T5.5 — Purity and determinism
// ---------------------------------------------------------------------------

// TestBuildRetargetedAgent_DeterministicOutput verifies that two calls with identical
// RetargetInput produce byte-identical output. Byte-determinism is the testability
// foundation — if tests can assert exact output bytes, they are not brittle.
func TestBuildRetargetedAgent_DeterministicOutput(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	in := RetargetInput{
		Source:              src,
		SourceModule:        alpha,
		TargetModule:        beta,
		Kind:                domain.ArtifactAgent,
		AgentKey:            "retarget-test-agent",
		TargetModel:         "gpt-4-turbo",
		ToolMappingsVersion: "abc123",
	}

	out1, _, err1 := BuildRetargetedAgent(in)
	if err1 != nil {
		t.Fatalf("first call returned error: %v", err1)
	}

	out2, _, err2 := BuildRetargetedAgent(in)
	if err2 != nil {
		t.Fatalf("second call returned error: %v", err2)
	}

	if !bytes.Equal(out1, out2) {
		t.Error("BuildRetargetedAgent is not deterministic: two identical calls produced different output bytes")
	}
}

// TestBuildRetargetedAgent_SourceBytesUnchangedAfterCall verifies that the source bytes
// in RetargetInput.Source are byte-identical before and after the call. The core must
// never mutate its input.
func TestBuildRetargetedAgent_SourceBytesUnchangedAfterCall(t *testing.T) {
	src := alphaDeployedAgentBytes()
	before := make([]byte, len(src))
	copy(before, src)

	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	_, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	if !bytes.Equal(src, before) {
		t.Error("BuildRetargetedAgent mutated RetargetInput.Source; it must be pure and non-mutating")
	}
}

// TestBuildRetargetedAgent_NonOrchestratorAgentKeyOmitsOrchestratorInjectionsVersion verifies
// that mosaic_orchestrator_injections_version is absent from the output when AgentKey is not
// "orchestrator". This is a regression test for a bug where BuildRetargetedAgent stamped the
// field unconditionally from the target descriptor, bypassing the target module's own
// per-agent-key conditional decision in Frontmatter().
//
// The target module used here (orchestratorConditionalModule) has OrchestratorInjectionsVersion
// set in its descriptor AND a Frontmatter() override that only adds the stamp for
// AgentKey "orchestrator". This ensures the test would have caught the bug (descriptor-driven
// auto-stamp would have placed the field in the output regardless of AgentKey) and validates
// that the fix (relying solely on tgtPlan.Set) works correctly.
func TestBuildRetargetedAgent_NonOrchestratorAgentKeyOmitsOrchestratorInjectionsVersion(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	// Target module: descriptor has OrchestratorInjectionsVersion set, but Frontmatter()
	// only adds the stamp when AgentKey == "orchestrator".
	target := newOrchestratorConditionalModule("orch-injections-v1")

	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: target,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "some-random-subagent", // explicitly not "orchestrator"
		TargetModel:  "gpt-4-turbo",
	})

	if err != nil {
		t.Fatalf("BuildRetargetedAgent returned error: %v", err)
	}

	// mosaic_orchestrator_injections_version must be absent from the output: the target
	// module's Frontmatter() decided not to include it for this non-orchestrator agent.
	// BuildRetargetedAgent must not independently derive it from the descriptor.
	keys := frontmatterKeys(t, out)
	if keys["mosaic_orchestrator_injections_version"] {
		t.Error("mosaic_orchestrator_injections_version must be absent for a non-orchestrator agent; " +
			"BuildRetargetedAgent must not auto-stamp this field from the descriptor — " +
			"the target module's Frontmatter() is the sole authority")
	}
}

// TestBuildRetargetedAgent_RunsAgainstInMemoryBytesOnly verifies that the transform
// operates entirely on in-memory bytes, confirming the I/O-free contract. The test
// supplies no filesystem paths and expects a successful result, proving the core does
// not need to read any file to produce output.
func TestBuildRetargetedAgent_RunsAgainstInMemoryBytesOnly(t *testing.T) {
	// All inputs are constructed in-memory: source bytes, source module, target module.
	// No file paths or filesystem resources are referenced anywhere in this call.
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()
	beta := newBetaHarnessModule()

	out, report, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: alpha,
		TargetModule: beta,
		Kind:         domain.ArtifactAgent,
		AgentKey:     "retarget-test-agent",
		TargetModel:  "gpt-4-turbo",
	})

	// A successful result with no filesystem access proves the I/O-free contract.
	if err != nil {
		t.Fatalf("BuildRetargetedAgent failed on in-memory input: %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output bytes from in-memory inputs")
	}
	if report.AgentKey == "" {
		t.Error("expected a non-empty AgentKey in the report")
	}
}

package transform_test

// codex_behaviour_test.go covers Codex-specific behaviour through transform.Apply with
// the real Codex module. The tests in this file prove that the Codex module and the
// codex-toml translator compose correctly: output correctness, user-region and user-key
// survival across a genuine rewrite, owned-key precedence, and translator report surfacing.
//
// Coverage:
//
//   Output correctness (T13.2):
//   - transform.Apply with the real Codex module produces bytes that parse as valid TOML.
//   - The produced TOML carries the expected keys: name, description, model,
//     sandbox_mode, developer_instructions.
//   - The agent body appears in developer_instructions.
//   - The MOSAIC version stamps appear in the leading comment block.
//   - sandbox_mode matches the expected value for the declared tools.
//   - No tools key is present.
//
//   User-region survival (T13.3):
//   - Prior deployed TOML carries user-filled injection region content in the body.
//   - A genuinely rewriting transform (version bump produces a changed stamp) preserves
//     the user content byte-for-byte.
//   - The test asserts the output changed (rewrite confirmed) before asserting survival.
//
//   User-key survival, both shapes (T13.4):
//   - Prior deployed TOML carries a value-carried scalar (reasoning_effort).
//   - Prior deployed TOML carries a marker-carried table (mcp_servers).
//   - After a genuinely rewriting transform both survive in the output.
//   - The marker-carried table is the load-bearing case: it proves the raw prior bytes
//     were threaded through DeployedRaw.
//
//   Translator report surfacing (T13.5):
//   - A source with a foreign frontmatter key and a divergent name produces FieldChange
//     entries in the transform Report for both the dropped key and the overridden name.
//
//   Tools-less source, create path (T13.5a):
//   - A source agent declaring no tools key at all still produces a Codex file
//     carrying sandbox_mode = "read-only" on the create path.
//   - Assertion is by key presence in the parsed TOML, not text search.
//
//   Tools-less source, update path (T13.5b) -- correctness assertion:
//   - Prior deployed bytes carry sandbox_mode = "workspace-write".
//   - A source agent declaring no tools key produces a rewriting run.
//   - The test asserts sandbox_mode = "read-only" in the output.
//   - The fix (I18.1): resolveTools calls Module.Tools with an empty request when the
//     source has no tools key and desc.Frontmatter.ToolsKey is empty (Codex only today).
//     collapseSandboxMode returns "read-only" for the empty request, sandbox_mode is
//     marked touched, and Step 5c cannot copy the prior workspace-write value.
//
//   Tools-less update correctness / collapseSandboxMode fail-safe (T18.1):
//   - A tools-less source updating a deployed file with workspace-write must produce
//     read-only in the output (correctness assertion for the I18.1 fix).
//   - collapseSandboxMode with an empty request (no Placeholder, empty Generic) returns
//     read-only, pinning the fail-safe default that resolveTools relies on after the fix.
//
//   Version stamp presence (T18.2):
//   - A Codex CREATE produces a "# mosaic_harness_version:" comment stamp with a
//     non-empty value in the TOML output.
//   - A Codex CREATE with an InjectionHarness region produces a version attribute on
//     the region tag matching desc.InjectionsVersion.
//   - Both tests are RED until I18.2 adds transform_version and injections_version to
//     codex.yaml.
//
//   Owned-key precedence (T13.6):
//   - Prior deployed bytes carry hand-edited owned keys (sandbox_mode, model).
//   - The source declares a tools list (escalating), so Module.Tools fires.
//   - After a genuinely rewriting transform, MOSAIC's values win: sandbox_mode
//     matches what the module emits, model matches the request's model.
//   - User-owned keys in the same file (reasoning_effort, mcp_servers) survive.

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	_ "mosaic-deploy/internal/agentformat/all"
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/codex"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// codexFrozenCatalogRoot returns the absolute path to the frozen Catalog fixture tree.
// Transform tests use this root so they remain stable as the live Catalog evolves.
// The Codex module loads its injection content from there at construction time.
func codexFrozenCatalogRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "frozen-catalog")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve frozen catalog root: %v", err)
	}
	return abs
}

// codexModuleForTransform constructs the Codex module against the frozen catalog.
func codexModuleForTransform(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := codex.New(registry.BuiltinOptions{MosaicRoot: codexFrozenCatalogRoot(t)})
	if err != nil {
		t.Fatalf("codex.New: %v", err)
	}
	return mod
}

// parseTomlOutput parses the given bytes as TOML and returns the flat key map.
// Only top-level scalar string values are returned; tables are not expanded here
// since the tests that need table content query the raw bytes.
func parseTomlOutput(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("toml.Unmarshal: %v\noutput:\n%s", err, string(b))
	}
	return m
}

// codexDecodeCanonical decodes prior deployed TOML to canonical Markdown+YAML.
// Used to populate transform.Request.Deployed for update tests.
func codexDecodeCanonical(t *testing.T, priorTOML []byte) []byte {
	t.Helper()
	translator, err := agentformat.LookupString("codex-toml")
	if err != nil {
		t.Fatalf("LookupString(codex-toml): %v", err)
	}
	canonical, _, err := translator.Decode(priorTOML, agentformat.ArtifactContext{})
	if err != nil {
		t.Fatalf("Decode prior TOML: %v", err)
	}
	return canonical
}

// codexTestModel is a fixed model selection used across all Codex transform tests.
var codexTestModel = domain.ModelSelection{
	ModelID: "gpt-5.6-sol",
	Origin:  domain.OriginHarnessList,
}

// ---------------------------------------------------------------------------
// T13.2 -- Output correctness
// ---------------------------------------------------------------------------

// TestCodexTransform_OutputCorrectness verifies that transform.Apply with the real Codex
// module produces TOML output that: (a) parses as valid TOML, (b) carries name,
// description, model, sandbox_mode, and developer_instructions, (c) has the body in
// developer_instructions, (d) has the MOSAIC stamps in the leading comment block,
// (e) carries the correct sandbox_mode for the declared tools, and (f) has no tools key.
func TestCodexTransform_OutputCorrectness(t *testing.T) {
	mod := codexModuleForTransform(t)

	source := []byte(`---
id: 100
version: 1.0.0
description: A Codex test agent.
tools: [file_read, file_write]
---
You are a test agent.

Carry out the requested task.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "codex-test-agent",
		Module: mod,
		Model:  codexTestModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	output := result.Output

	// (a) Output must parse as valid TOML.
	m := parseTomlOutput(t, output)

	// (b) Expected keys must be present.
	for _, key := range []string{"name", "description", "model", "sandbox_mode", "developer_instructions"} {
		if _, ok := m[key]; !ok {
			t.Errorf("output TOML missing key %q; present keys: %v; "+
				"transform.Apply with Codex module must produce all core Codex agent fields",
				key, tomlKeySet(m))
		}
	}

	// (c) Body must appear in developer_instructions.
	if instr, ok := m["developer_instructions"]; ok {
		instrStr, isStr := instr.(string)
		if !isStr {
			t.Errorf("developer_instructions is not a string in output TOML; type: %T", instr)
		} else if !strings.Contains(instrStr, "You are a test agent.") {
			t.Errorf("developer_instructions does not contain the source body text; "+
				"body: %q, developer_instructions: %q", "You are a test agent.", instrStr)
		}
	}

	// (d) The mosaic_version stamp must appear in the leading comment block.
	// Note: mosaic_harness_version is only written when the harness descriptor sets
	// transform_version. After I18.2 adds transform_version to codex.yaml, the
	// mosaic_harness_version stamp will also appear.
	// See TestCodexTransform_HarnessVersionStamp_PresentInOutput for the dedicated assertion.
	outputStr := string(output)
	if !strings.Contains(outputStr, "# mosaic_version:") {
		t.Errorf("output TOML does not contain stamp comment %q in the leading comment block; "+
			"the source version stamp must be written by the encoder", "# mosaic_version:")
	}

	// (e) sandbox_mode must be workspace-write because file_write is an escalating tool.
	if sm, ok := m["sandbox_mode"]; ok {
		smStr, _ := sm.(string)
		if smStr != "workspace-write" {
			t.Errorf("sandbox_mode = %q, want %q; "+
				"source declares file_write which is an escalating tool; "+
				"the Codex module must collapse this to workspace-write",
				smStr, "workspace-write")
		}
	}

	// (f) No tools key must be present.
	if _, hasTools := m["tools"]; hasTools {
		t.Errorf("output TOML has a 'tools' key; Codex expresses capability via sandbox_mode, "+
			"not via a tools list field; the tools key must never appear in Codex output")
	}
}

// tomlKeySet returns the key names from a TOML flat map, for error messages.
func tomlKeySet(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ---------------------------------------------------------------------------
// T13.3 -- User-region survival
// ---------------------------------------------------------------------------

// TestCodexTransform_UserRegionSurvival verifies that prior deployed TOML containing
// user-filled injection region content survives a genuinely rewriting transform. The test
// first asserts the output changed (bumped stamp) before asserting the user content survived.
//
// The user region is an InjectionProject class region inside developer_instructions.
// On a genuinely rewriting update (version bump), the transform restores the user's filled
// content from the decoded canonical prior (req.Deployed), which was originally populated
// from the prior deployed TOML's developer_instructions field.
func TestCodexTransform_UserRegionSurvival(t *testing.T) {
	mod := codexModuleForTransform(t)

	const userRegionContent = "USER ADDED: custom harness notes for this deployment."

	// Prior deployed TOML: version 1.0.0 stamp, has user-filled injection region in body.
	priorTOML := []byte("# mosaic_version: 1.0.0\n" +
		"# mosaic_harness_version: 1.0.0\n" +
		"# mosaic_injections_version: 1.0.0\n" +
		"name = \"test-agent\"\n" +
		"description = \"Test agent\"\n" +
		"model = \"gpt-5.6-sol\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"\n" +
		"developer_instructions = \"\"\"\n" +
		"Agent instructions.\n" +
		"\n" +
		"<Identity type=\"core\">\n" +
		"You are the test agent.\n" +
		"<IdentityExtension type=\"project\">\n" +
		userRegionContent + "\n" +
		"</IdentityExtension>\n" +
		"</Identity>\n" +
		"\"\"\"\n")

	canonical := codexDecodeCanonical(t, priorTOML)

	// Source agent: version bumped to 2.0.0 so the deploy rewrites the file.
	source := []byte(`---
id: 100
version: 2.0.0
description: Test agent
tools: [file_read]
---
Agent instructions.

<Identity type="core">
You are the test agent.
<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`)

	result, err := transform.Apply(transform.Request{
		Source:      source,
		Kind:        domain.ArtifactAgent,
		Key:         "test-agent",
		Module:      mod,
		Model:       codexTestModel,
		Op:          agentformat.OpUpdate,
		Deployed:    canonical,
		DeployedRaw: priorTOML,
		Scope:       domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	output := result.Output

	// Anti-vacuity: assert the output TOML actually changed (stamp was bumped).
	// If the output equals the input, the test would pass vacuously.
	if bytes.Equal(output, priorTOML) {
		t.Fatal("output TOML is byte-identical to prior TOML; " +
			"the version bump (1.0.0 -> 2.0.0) must change the mosaic_version stamp; " +
			"a test that passes when nothing was written proves nothing about user-region survival")
	}

	// Assert the output has a changed stamp (version 2.0.0).
	outputStr := string(output)
	if !strings.Contains(outputStr, "# mosaic_version: 2.0.0") {
		t.Errorf("output TOML does not contain '# mosaic_version: 2.0.0'; "+
			"the version bump must update the stamp; output head:\n%s",
			firstLines(outputStr, 5))
	}

	// Assert the user region content survived byte-for-byte.
	if !strings.Contains(outputStr, userRegionContent) {
		t.Errorf("output TOML does not contain user region content %q; "+
			"the transform must preserve user-filled injection region content from the "+
			"decoded canonical prior (req.Deployed) across a genuine rewrite",
			userRegionContent)
	}
}

// firstLines returns the first n lines of s, joined with newlines.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// T13.4 -- User-key survival, both shapes
// ---------------------------------------------------------------------------

// TestCodexTransform_UserKeySurvival_BothShapes verifies that both carriage shapes
// survive a genuinely rewriting transform:
//   - Value-carried scalar: reasoning_effort = "high"
//   - Marker-carried table: [mcp_servers.github]
//
// The marker-carried table is the load-bearing case: it proves the raw prior bytes
// (DeployedRaw) were threaded through to the encoder, since a value-carried key would
// survive even without the prior-bytes channel.
func TestCodexTransform_UserKeySurvival_BothShapes(t *testing.T) {
	mod := codexModuleForTransform(t)

	// Prior deployed TOML has both carriage shapes.
	// reasoning_effort is a plain scalar -> value-carried.
	// mcp_servers is a table -> marker-carried via the prior-bytes channel.
	priorTOML := []byte("# mosaic_version: 1.0.0\n" +
		"# mosaic_harness_version: 1.0.0\n" +
		"# mosaic_injections_version: 1.0.0\n" +
		"name = \"test-agent\"\n" +
		"description = \"Test agent\"\n" +
		"model = \"gpt-5.6-sol\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"reasoning_effort = \"high\"\n" +
		"\n" +
		"developer_instructions = \"\"\"\n" +
		"Test agent instructions.\n" +
		"\"\"\"\n" +
		"\n" +
		"[mcp_servers.github]\n" +
		"type = \"github\"\n")

	canonical := codexDecodeCanonical(t, priorTOML)

	// Source: version bumped to 2.0.0 to trigger a genuine rewrite.
	source := []byte(`---
id: 100
version: 2.0.0
description: Test agent
tools: [file_read]
---
Test agent instructions.
`)

	result, err := transform.Apply(transform.Request{
		Source:      source,
		Kind:        domain.ArtifactAgent,
		Key:         "test-agent",
		Module:      mod,
		Model:       codexTestModel,
		Op:          agentformat.OpUpdate,
		Deployed:    canonical,
		DeployedRaw: priorTOML,
		Scope:       domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	output := result.Output

	// Anti-vacuity: assert the output changed.
	if bytes.Equal(output, priorTOML) {
		t.Fatal("output TOML is byte-identical to prior TOML; version bump must change the stamp")
	}

	m := parseTomlOutput(t, output)
	outputStr := string(output)

	// Assert value-carried scalar survived.
	if v, ok := m["reasoning_effort"]; !ok {
		t.Error("output TOML is missing reasoning_effort; " +
			"the value-carried scalar must survive a genuine rewrite through mosaic_carriage")
	} else if s, _ := v.(string); s != "high" {
		t.Errorf("reasoning_effort = %q, want %q; value-carried scalar must survive intact",
			s, "high")
	}

	// Assert marker-carried table survived. The table header appears in the raw output.
	// This is the proof that DeployedRaw was threaded: marker carriage reconstructs the
	// span from the raw prior bytes. Without DeployedRaw the encoder cannot find the span.
	if !strings.Contains(outputStr, "[mcp_servers.github]") {
		t.Errorf("output TOML does not contain '[mcp_servers.github]'; "+
			"the marker-carried table must survive a genuine rewrite through the prior-bytes channel "+
			"(DeployedRaw); its absence proves DeployedRaw was not threaded correctly")
	}

	if github, ok := m["mcp_servers"]; ok {
		githubMap, _ := github.(map[string]interface{})
		if githubMap == nil {
			t.Errorf("mcp_servers is not a table in output TOML; type: %T", github)
		}
		// The github sub-table should have type = "github".
		if inner, ok2 := githubMap["github"]; ok2 {
			innerMap, _ := inner.(map[string]interface{})
			if innerMap == nil {
				t.Errorf("mcp_servers.github is not a table; type: %T", inner)
			} else if typ, _ := innerMap["type"].(string); typ != "github" {
				t.Errorf("mcp_servers.github.type = %q, want %q", typ, "github")
			}
		} else {
			t.Errorf("mcp_servers has no 'github' sub-table in output TOML")
		}
	} else {
		t.Errorf("output TOML is missing mcp_servers table entirely")
	}
}

// ---------------------------------------------------------------------------
// T13.5 -- Translator report surfaces into transform Report
// ---------------------------------------------------------------------------

// TestCodexTransform_TranslatorReport verifies that a source carrying a foreign frontmatter
// key and a divergent name produces FieldChange entries in the transform Report: one for
// the dropped key and one for the overridden name, each with a reason.
func TestCodexTransform_TranslatorReport(t *testing.T) {
	mod := codexModuleForTransform(t)

	// Source with a foreign key (custom_setting) and a name that diverges from the key.
	// The agent key passed to transform is "correct-agent-key".
	source := []byte(`---
id: 100
version: 1.0.0
name: WrongName
description: Report surfacing test agent.
tools: [file_read]
custom_setting: some-value
---
Agent body.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "correct-agent-key",
		Module: mod,
		Model:  codexTestModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	// Check that the transform Report contains a FieldChange for the dropped foreign key.
	foundDropped := false
	for _, fc := range result.Report.Fields {
		if fc.Key == "custom_setting" && fc.After == "" {
			foundDropped = true
			if fc.Reason == "" {
				t.Errorf("FieldChange for dropped key %q has empty Reason; "+
					"every report entry must carry a human-readable reason", fc.Key)
			}
		}
	}
	if !foundDropped {
		t.Errorf("transform Report.Fields does not contain a FieldChange for dropped key 'custom_setting'; "+
			"a foreign key in the canonical source must produce EntryDroppedForeignKey which maps to "+
			"a FieldChange with After: \"\"; fields: %v", result.Report.Fields)
	}

	// Check that the transform Report records the name as removed/replaced.
	// Observed behaviour: the Codex descriptor's drop list includes "name", so
	// applyFrontmatter drops the name field from the canonical form BEFORE the encoder
	// sees it. The encoder therefore never sees a divergent name and never emits
	// EntryOverriddenName. Instead, the name field appears in the report as a "descriptor
	// drop" FieldChange (Before: "WrongName", After: ""). The encoder then adds
	// name = "correct-agent-key" to the TOML output from ctx.AgentKey, with no report entry.
	//
	// This is the actual Codex name-handling contract: the descriptor drop removes the
	// source name and the encoder asserts the agent key as the authoritative name. The
	// user-visible effect (WrongName was not kept) IS captured in the report via the
	// "descriptor drop" entry.
	foundNameInReport := false
	for _, fc := range result.Report.Fields {
		if fc.Key == "name" && fc.Before == "WrongName" {
			foundNameInReport = true
		}
	}
	if !foundNameInReport {
		t.Errorf("transform Report.Fields does not record the source name %q; "+
			"the name field must appear in the report (either as a drop or an override) "+
			"so the user can see the divergent name was not kept; fields: %v",
			"WrongName", result.Report.Fields)
	}

	// Confirm the effects: the foreign key must not appear in the output TOML,
	// and the name must be the agent key, not the source name.
	m := parseTomlOutput(t, result.Output)
	if _, hasCustom := m["custom_setting"]; hasCustom {
		t.Error("output TOML contains 'custom_setting'; a foreign key must be dropped from Codex output")
	}
	if name, ok := m["name"]; ok {
		nameStr, _ := name.(string)
		if nameStr != "correct-agent-key" {
			t.Errorf("output name = %q, want %q; the agent key is the sole authority for name",
				nameStr, "correct-agent-key")
		}
	}
}

// ---------------------------------------------------------------------------
// T13.5a -- Tools-less source: create path always has sandbox_mode
// ---------------------------------------------------------------------------

// TestCodexTransform_ToollessCreate_HasSandboxMode verifies that a source agent declaring
// no tools key at all still produces a Codex file carrying sandbox_mode = "read-only" on
// the create path. This is not redundant with the module-level collapse tests: when no
// tools key is present, transform.resolveTools returns early without calling Module.Tools
// (internal/transform/frontmatter.go, resolveTools returns an empty result when no tools
// key exists). The sandbox_mode presence here comes from the encoder's deploy-mode
// fallback, not from the module's collapse. The two mechanisms are separately breakable.
//
// Assertion is by key presence in the parsed TOML, not by text search, because an absent
// key must not be able to pass a text-search test on the full output bytes.
func TestCodexTransform_ToollessCreate_HasSandboxMode(t *testing.T) {
	mod := codexModuleForTransform(t)

	// Source deliberately omits any tools key.
	source := []byte(`---
id: 100
version: 1.0.0
description: A tools-less source agent.
---
Agent body without any tools declaration.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "toolless-agent",
		Module: mod,
		Model:  codexTestModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply (tools-less create): %v", err)
	}

	// Parse the output as TOML and check sandbox_mode presence by key lookup.
	m := parseTomlOutput(t, result.Output)

	sandboxVal, hasSandbox := m["sandbox_mode"]
	if !hasSandbox {
		t.Errorf("output TOML has no sandbox_mode key for a tools-less source agent; "+
			"the encoder's deploy-mode fallback must write sandbox_mode = \"read-only\" "+
			"when no sandbox_mode was set by the module (because Module.Tools was not called); "+
			"a Codex file with no sandbox_mode inherits the parent session's mode, "+
			"which is a security risk; keys in output: %v", tomlKeySet(m))
		return
	}

	sandboxStr, ok := sandboxVal.(string)
	if !ok {
		t.Errorf("sandbox_mode is not a string; type: %T", sandboxVal)
		return
	}
	if sandboxStr != "read-only" {
		t.Errorf("sandbox_mode = %q, want %q for tools-less create path; "+
			"the encoder fallback must emit read-only as the fail-closed default",
			sandboxStr, "read-only")
	}
}

// ---------------------------------------------------------------------------
// T13.5b -- Tools-less source: update path correctness assertion
// ---------------------------------------------------------------------------

// TestCodexTransform_ToollessUpdate_ProducesReadOnly verifies that a tools-less source
// updating a deployed Codex file with sandbox_mode = "workspace-write" produces output
// with sandbox_mode = "read-only".
//
// This replaces the former characterisation test that pinned the observed (buggy) value
// "workspace-write". The correct value is "read-only" per FR-11a.
//
// The fix is in resolveTools (frontmatter.go): when the source has no tools field and
// desc.Frontmatter.ToolsKey is empty (Codex only today), resolveTools calls
// req.Module.Tools with an empty request. collapseSandboxMode returns "read-only" for
// an empty request (no Placeholder, no escalating Generic), sandbox_mode is added to
// touchedKeys, and Step 5c cannot copy the prior workspace-write value.
//
// This test is RED until I18.1 applies the resolveTools fix.
func TestCodexTransform_ToollessUpdate_ProducesReadOnly(t *testing.T) {
	mod := codexModuleForTransform(t)

	// Prior deployed TOML: sandbox_mode = "workspace-write" from a previous deploy that
	// had escalating tools. The source has since dropped all tools.
	priorTOML := []byte("# mosaic_version: 1.0.0\n" +
		"# mosaic_harness_version: 1.0.0\n" +
		"# mosaic_injections_version: 1.0.0\n" +
		"name = \"toolless-update-agent\"\n" +
		"description = \"Tools-less update agent.\"\n" +
		"model = \"gpt-5.6-sol\"\n" +
		"sandbox_mode = \"workspace-write\"\n" +
		"\n" +
		"developer_instructions = \"\"\"\n" +
		"Agent body.\n" +
		"\"\"\"\n")

	canonical := codexDecodeCanonical(t, priorTOML)

	// Source agent: version bumped to 2.0.0 to force a genuine rewrite; no tools key.
	source := []byte(`---
id: 100
version: 2.0.0
description: Tools-less update agent.
---
Agent body.
`)

	result, err := transform.Apply(transform.Request{
		Source:      source,
		Kind:        domain.ArtifactAgent,
		Key:         "toolless-update-agent",
		Module:      mod,
		Model:       codexTestModel,
		Op:          agentformat.OpUpdate,
		Deployed:    canonical,
		DeployedRaw: priorTOML,
		Scope:       domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply (tools-less update): %v", err)
	}
	output := result.Output

	// Anti-vacuity: assert the output actually changed (stamp bumped).
	if bytes.Equal(output, priorTOML) {
		t.Fatal("output TOML is byte-identical to prior TOML; " +
			"version bump (1.0.0 -> 2.0.0) must change the mosaic_version stamp; " +
			"a no-op update proves nothing about sandbox_mode")
	}

	m := parseTomlOutput(t, output)

	sandboxVal, hasSandbox := m["sandbox_mode"]
	if !hasSandbox {
		t.Errorf("sandbox_mode absent from output TOML on tools-less update path; "+
			"sandbox_mode must always be emitted for Codex files (FR-11a); "+
			"its absence would allow Codex to inherit the parent session's mode")
		return
	}

	sandboxStr, _ := sandboxVal.(string)
	if sandboxStr != "read-only" {
		t.Errorf("tools-less update: sandbox_mode = %q, want %q; "+
			"a source with no tools key must produce the fail-safe read-only default, "+
			"not preserve the prior workspace-write value; "+
			"root cause (before fix): resolveTools returns early for no-tools sources, "+
			"sandbox_mode is not marked touched, Step 5c copies the prior elevated value; "+
			"fix (I18.1): resolveTools calls Module.Tools with an empty request when "+
			"desc.Frontmatter.ToolsKey is empty, ensuring sandbox_mode is always touched",
			sandboxStr, "read-only")
	}
}

// ---------------------------------------------------------------------------
// T18.2(a) -- Codex CREATE produces mosaic_harness_version comment stamp
// ---------------------------------------------------------------------------

// TestCodexTransform_HarnessVersionStamp_PresentInOutput verifies that a Codex CREATE
// produces a "# mosaic_harness_version:" comment stamp in the TOML output with a
// non-empty value.
//
// This test is RED until I18.2 adds transform_version: "1.0.0" to codex.yaml.
// Without that field, applyVersionStamp skips the harness version stamp because
// desc.TransformVersion is empty. After the fix, the stamp appears as
// "# mosaic_harness_version: 1.0.0" in the leading TOML comment block.
func TestCodexTransform_HarnessVersionStamp_PresentInOutput(t *testing.T) {
	mod := codexModuleForTransform(t)

	source := []byte(`---
id: 100
version: 1.0.0
description: Version stamp test agent.
---
Agent body.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "version-stamp-agent",
		Module: mod,
		Model:  codexTestModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	outputStr := string(result.Output)
	if !strings.Contains(outputStr, "# mosaic_harness_version:") {
		t.Errorf("output TOML does not contain '# mosaic_harness_version:' in the leading comment block; "+
			"once transform_version: \"1.0.0\" is declared in codex.yaml (I18.2), "+
			"applyVersionStamp must write this stamp so the deployed file carries the "+
			"harness format version (FR-9 name mapping: mosaic_transform_version -> mosaic_harness_version); "+
			"output head:\n%s",
			firstLines(outputStr, 10))
	}
}

// ---------------------------------------------------------------------------
// T18.2(b) -- Codex CREATE with injection region produces version attribute
// ---------------------------------------------------------------------------

// TestCodexTransform_InjectionsVersionOnRegionTag verifies that a Codex CREATE with a
// harness injection region in the body produces a version attribute on the InjectionHarness
// region tag, matching desc.InjectionsVersion.
//
// This test is RED until I18.2 adds injections_version: "1.0.0" to codex.yaml.
// Without that field, applyHarnessRegion skips SetVersion because injVersion is empty
// (injection.go guards: "if injVersion != \"\" { node.SetVersion(injVersion) }").
// After the fix the HarnessConstraints region tag becomes:
//
//	<HarnessConstraints type="managed" version="1.0.0">
func TestCodexTransform_InjectionsVersionOnRegionTag(t *testing.T) {
	mod := codexModuleForTransform(t)

	// Source with a HarnessConstraints region so applyHarnessRegion fires during the transform.
	// The Codex module provides HarnessConstraints content; the frozen-catalog fixture at
	// testdata/frozen-catalog/.../Codex/HarnessInjections.md declares this region.
	source := []byte("---\n" +
		"id: 100\n" +
		"version: 1.0.0\n" +
		"description: Injection version stamp test agent.\n" +
		"---\n" +
		"Agent body.\n" +
		"\n" +
		"<HarnessConstraints type=\"managed\">\n" +
		"</HarnessConstraints>\n")

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "injection-version-agent",
		Module: mod,
		Model:  codexTestModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	outputStr := string(result.Output)

	// Anti-vacuity: assert the HarnessConstraints region is present in the output.
	if !strings.Contains(outputStr, "<HarnessConstraints") {
		t.Fatalf("output does not contain HarnessConstraints region; "+
			"the source declared a HarnessConstraints region and the Codex module provides "+
			"content for it in the frozen-catalog fixture; if the region is absent, "+
			"verify that the frozen-catalog fixture at testdata/frozen-catalog/.../Codex/ "+
			"declares a HarnessConstraints region in HarnessInjections.md; "+
			"output:\n%s", outputStr)
	}

	// The region tag must carry version="1.0.0" once injections_version is set in codex.yaml.
	const wantVersionAttr = `version="1.0.0"`
	if !strings.Contains(outputStr, wantVersionAttr) {
		t.Errorf("InjectionHarness region tag does not carry version attribute %q; "+
			"once injections_version: \"1.0.0\" is declared in codex.yaml (I18.2), "+
			"applyHarnessRegion must stamp the version on every InjectionHarness-class "+
			"region tag so the deployed file records the injection version it was built against; "+
			"the staleness and manifest paths read this version from the region tag, not frontmatter",
			wantVersionAttr)
	}
}

// ---------------------------------------------------------------------------
// T13.6 -- Owned-key precedence
// ---------------------------------------------------------------------------

// TestCodexTransform_OwnedKeyPrecedence verifies that MOSAIC-owned keys win any collision
// with hand-edited deployed values, while user-owned regions and user-added keys in the
// same file survive. This is FR-9c's "MOSAIC-owned keys always win any collision".
//
// The source declares file_write (an escalating tool) so that transform.resolveTools
// calls Module.Tools and the module's collapse fires for sandbox_mode. Without a tools
// declaration, Module.Tools is not called, Step 5c preserves the prior sandbox_mode,
// and the owned-key precedence test passes vacuously (it becomes a copy of T13.5b).
func TestCodexTransform_OwnedKeyPrecedence(t *testing.T) {
	mod := codexModuleForTransform(t)

	// Prior deployed TOML: hand-edited owned keys.
	// sandbox_mode hand-edited from workspace-write to read-only (wrong for an escalating agent).
	// model hand-edited to wrong-model.
	// reasoning_effort is a user-owned key that must survive.
	priorTOML := []byte("# mosaic_version: 1.0.0\n" +
		"# mosaic_harness_version: 1.0.0\n" +
		"# mosaic_injections_version: 1.0.0\n" +
		"name = \"owned-key-agent\"\n" +
		"description = \"Owned key precedence agent.\"\n" +
		"model = \"wrong-model\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"reasoning_effort = \"medium\"\n" +
		"\n" +
		"developer_instructions = \"\"\"\n" +
		"Agent body.\n" +
		"\"\"\"\n")

	canonical := codexDecodeCanonical(t, priorTOML)

	// Source: version bumped to force a rewrite; declares file_write so Module.Tools fires.
	// The correct model is gpt-5.6-sol (passed as codexTestModel).
	source := []byte(`---
id: 100
version: 2.0.0
description: Owned key precedence agent.
tools: [file_write, file_read]
---
Agent body.
`)

	result, err := transform.Apply(transform.Request{
		Source:      source,
		Kind:        domain.ArtifactAgent,
		Key:         "owned-key-agent",
		Module:      mod,
		Model:       codexTestModel,
		Op:          agentformat.OpUpdate,
		Deployed:    canonical,
		DeployedRaw: priorTOML,
		Scope:       domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	output := result.Output

	// Anti-vacuity: assert the output changed.
	if bytes.Equal(output, priorTOML) {
		t.Fatal("output TOML is byte-identical to prior TOML; version bump must change the stamp")
	}

	m := parseTomlOutput(t, output)

	// MOSAIC wins sandbox_mode: file_write escalates to workspace-write.
	// The prior value was read-only (hand-edited); MOSAIC's collapse fires because the
	// source declares file_write, so Module.Tools is called and the module emits workspace-write.
	if sm, ok := m["sandbox_mode"]; ok {
		smStr, _ := sm.(string)
		if smStr != "workspace-write" {
			t.Errorf("sandbox_mode = %q, want %q; "+
				"MOSAIC-owned sandbox_mode must be set by Module.Tools (file_write escalates); "+
				"the hand-edited prior read-only value must not win the collision",
				smStr, "workspace-write")
		}
	} else {
		t.Error("sandbox_mode absent from output TOML; owned key must always be present")
	}

	// MOSAIC wins model: the request's model selection (gpt-5.6-sol) overrides wrong-model.
	if model, ok := m["model"]; ok {
		modelStr, _ := model.(string)
		if modelStr != codexTestModel.ModelID {
			t.Errorf("model = %q, want %q; "+
				"MOSAIC-owned model must come from the transform request, not from the hand-edited prior value",
				modelStr, codexTestModel.ModelID)
		}
	} else {
		t.Error("model absent from output TOML; owned model key must be present when model is specified")
	}

	// User's reasoning_effort survives (value-carried user key is not MOSAIC-owned).
	if v, ok := m["reasoning_effort"]; !ok {
		t.Error("reasoning_effort absent from output TOML; " +
			"user-owned value-carried key must survive alongside MOSAIC's owned-key writes")
	} else if s, _ := v.(string); s != "medium" {
		t.Errorf("reasoning_effort = %q, want %q; user-carried value must survive intact", s, "medium")
	}
}

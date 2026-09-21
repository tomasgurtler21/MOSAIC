package app

// codex_refresh_test.go tests harness-only refresh for Codex (.toml) agents.
//
// These tests assert the post-implementation contract of the encode step that was
// added to refreshHarnessOnly in Stage 10 (I10.3):
//
//   T10.1 - A refresh of a deployed .toml harness-only agent updates managed regions
//           and leaves the file valid TOML with user content intact.
//
//   T10.2 - A deployed Codex harness-only agent carrying user-added keys still has them
//           after a refresh. This catches a refresh path that reads through the funnel but
//           forgets to thread the raw prior bytes on the way back out, which is a failure
//           mode invisible on Markdown harnesses.
//
//   T10.4 - A no-op refresh of a deployed .toml harness-only agent whose in-scope managed
//           regions already hold exactly the canonical content leaves the file semantically
//           unchanged: every owned key holds the value it held before, every user key is
//           still present, and the body is unchanged. Assertions are on decoded values, not
//           raw bytes (re-encoding is not required to be byte-identical).
//
// Package note: these tests are in package app (not app_test) because refreshHarnessOnly
// is unexported.
//
// Translator note: _ "mosaic-deploy/internal/agentformat/all" is imported to register the
// Codex TOML translator, matching the pattern in codex_index_scan_test.go and codex_probe_test.go.

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/agentformat"
	_ "mosaic-deploy/internal/agentformat/all"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers shared by T10.1, T10.2, T10.4
// ---------------------------------------------------------------------------

// codexRefreshToml encodes canonical bytes to Codex TOML using the registered
// translator and returns the TOML bytes. The agentKey is used for the name field.
// Uses OpCreate (no prior bytes) so this is only suitable for producing fixture
// prior-bytes, not for testing refresh behaviour.
func codexRefreshToml(t *testing.T, canonical []byte, agentKey string) []byte {
	t.Helper()
	tr, err := agentformat.LookupString("codex-toml")
	if err != nil {
		t.Fatalf("LookupString(codex-toml): %v", err)
	}
	out, _, encErr := tr.Encode(canonical, agentformat.ArtifactContext{
		AgentKey: agentKey,
		Op:       agentformat.OpCreate,
	})
	if encErr != nil {
		t.Fatalf("initial TOML encode: %v", encErr)
	}
	return out
}

// parseTomlMap parses TOML bytes into a map and fails the test if parsing fails.
func parseTomlMap(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("TOML parse failed: %v\nBytes:\n%s", err, b)
	}
	return m
}

// codexRefreshDecodeToml decodes Codex TOML bytes back to canonical form using the
// registered translator. Used in T10.4 to compare decoded values rather than raw bytes.
func codexRefreshDecodeToml(t *testing.T, tomlBytes []byte) []byte {
	t.Helper()
	tr, err := agentformat.LookupString("codex-toml")
	if err != nil {
		t.Fatalf("LookupString(codex-toml) for decode: %v", err)
	}
	canonical, _, decErr := tr.Decode(tomlBytes, agentformat.ArtifactContext{})
	if decErr != nil {
		t.Fatalf("TOML decode failed: %v\nBytes:\n%s", decErr, tomlBytes)
	}
	return canonical
}

// makeCodexRefreshRequest constructs a HarnessOnlyRefreshRequest for a Codex agent.
// canonical is the YAML+Markdown canonical form; tomlBytes is the DeployedRaw form.
func makeCodexRefreshRequest(canonical, tomlBytes []byte, agentKey string, scope RefreshScope) HarnessOnlyRefreshRequest {
	return HarnessOnlyRefreshRequest{
		Deployed:    canonical,
		DeployedRaw: tomlBytes,
		AgentKey:    agentKey,
		FormatID:    "codex-toml",
		Scope:       scope,
		Role:        domain.RoleSubagent,
		Protocol:    fixtureRefreshProtocol(fixtureRefreshProtocolVersion),
		Bundle:      fixtureRefreshBundle(fixtureRefreshBundleVersion),
		Subject:     agentKey + ".toml",
	}
}

// ---------------------------------------------------------------------------
// T10.1 -- Refresh updates managed regions; output is valid TOML with user content
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_Codex_UpdatesManagedRegion_OutputIsValidTOML verifies that a
// harness-only refresh of a deployed Codex .toml agent with an outdated CommunicationProtocol
// region:
//   - produces output that a standard TOML parser accepts (the file remains valid TOML);
//   - updates the CommunicationProtocol region to the current canonical content;
//   - preserves owned keys (name, description, sandbox_mode) from the input file.
func TestRefreshHarnessOnly_Codex_UpdatesManagedRegion_OutputIsValidTOML(t *testing.T) {
	const agentKey = "codex-refresh-test"

	// Canonical document with an outdated CommunicationProtocol region.
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A test Codex agent.\nsandbox_mode: read-only\n---\n" +
		"\n<CommunicationProtocol type=\"managed\">\nold communication protocol content\n</CommunicationProtocol>\n")

	// Encode to TOML to get the raw prior bytes.
	rawTOML := codexRefreshToml(t, canonical, agentKey)

	req := makeCodexRefreshRequest(canonical, rawTOML, agentKey, RefreshProtocolOnly)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	// Output must be valid TOML.
	m := parseTomlMap(t, result.Output)

	// developer_instructions must be present (the body was non-empty).
	instructions, ok := m["developer_instructions"].(string)
	if !ok {
		t.Fatal("developer_instructions absent from output TOML or not a string")
	}

	// The CommunicationProtocol region must carry the new canonical content.
	if !strings.Contains(instructions, fixtureRefreshSubagentProtocolBlock) {
		t.Errorf("developer_instructions does not contain the updated protocol block; got:\n%s", instructions)
	}

	// The old content must be gone.
	if strings.Contains(instructions, "old communication protocol content") {
		t.Error("developer_instructions still contains the old protocol content after refresh")
	}

	// Owned keys must be preserved.
	if name, _ := m["name"].(string); name != agentKey {
		t.Errorf("name = %q after refresh; want %q", name, agentKey)
	}
	if desc, _ := m["description"].(string); desc != "A test Codex agent." {
		t.Errorf("description = %q after refresh; want %q", desc, "A test Codex agent.")
	}
	if sandbox, _ := m["sandbox_mode"].(string); sandbox != "read-only" {
		t.Errorf("sandbox_mode = %q after refresh; want %q", sandbox, "read-only")
	}
}

// TestRefreshHarnessOnly_Codex_Refresh_OutputIsDeterministic verifies that repeated calls
// with identical Codex inputs produce byte-identical TOML output. This guards against any
// non-determinism introduced by the encode step.
func TestRefreshHarnessOnly_Codex_Refresh_OutputIsDeterministic(t *testing.T) {
	const agentKey = "codex-determinism"

	canonical := []byte("---\nname: " + agentKey + "\ndescription: Determinism test.\nsandbox_mode: read-only\n---\n" +
		"\n<CommunicationProtocol type=\"managed\">\nold content\n</CommunicationProtocol>\n")
	rawTOML := codexRefreshToml(t, canonical, agentKey)

	req := makeCodexRefreshRequest(canonical, rawTOML, agentKey, RefreshProtocolOnly)

	res1, err1 := refreshHarnessOnly(req)
	if err1 != nil {
		t.Fatalf("first call: %v", err1)
	}
	res2, err2 := refreshHarnessOnly(req)
	if err2 != nil {
		t.Fatalf("second call: %v", err2)
	}
	if string(res1.Output) != string(res2.Output) {
		t.Error("repeated refreshHarnessOnly calls produced different output for Codex; encode step must be deterministic")
	}
}

// ---------------------------------------------------------------------------
// T10.2 -- User-key survival across a refresh
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_Codex_UserKey_SurvivesRefresh verifies that a user-added TOML key
// in a deployed Codex harness-only agent is still present in the output after a refresh.
// This is the assertion that catches a refresh path that reads the file through the funnel
// but forgets to thread the raw prior bytes on the encode path (the user key would be lost
// silently because it cannot travel through the canonical frontmatter without the carriage
// container, and the carriage container requires the prior bytes to re-emit marker spans).
//
// The user key is value-carried (a simple TOML string), so it passes through the carriage
// container in the canonical form and is re-emitted from the prior bytes by the encoder.
func TestRefreshHarnessOnly_Codex_UserKey_SurvivesRefresh(t *testing.T) {
	const agentKey = "codex-userkey-test"

	// Build a TOML file that includes a user-added key alongside the Codex-owned keys.
	// The user key is a simple string that the decoder will value-carry through mosaic_carriage.
	rawTOML := []byte("name = \"" + agentKey + "\"\n" +
		"description = \"User key survival test.\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"my_user_key = \"preserved_value\"\n" +
		"developer_instructions = \"\\n<CommunicationProtocol type=\\\"managed\\\">\\nold protocol content\\n</CommunicationProtocol>\\n\"\n")

	// Decode the TOML to get the canonical form with the user key in mosaic_carriage.
	canonical := codexRefreshDecodeToml(t, rawTOML)

	req := makeCodexRefreshRequest(canonical, rawTOML, agentKey, RefreshProtocolOnly)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	// Output must be valid TOML.
	m := parseTomlMap(t, result.Output)

	// The user key must still be present.
	val, ok := m["my_user_key"].(string)
	if !ok {
		t.Fatal("my_user_key absent from output TOML after refresh; user-added key must survive")
	}
	if val != "preserved_value" {
		t.Errorf("my_user_key = %q after refresh; want %q", val, "preserved_value")
	}

	// The managed region must also have been updated.
	instructions, _ := m["developer_instructions"].(string)
	if !strings.Contains(instructions, fixtureRefreshSubagentProtocolBlock) {
		t.Error("developer_instructions does not contain the updated protocol block after refresh")
	}
}

// TestRefreshHarnessOnly_Codex_MultipleUserKeys_AllSurviveRefresh verifies that multiple
// user-added keys in a deployed Codex agent all survive a refresh.
func TestRefreshHarnessOnly_Codex_MultipleUserKeys_AllSurviveRefresh(t *testing.T) {
	const agentKey = "codex-multikey"

	rawTOML := []byte("name = \"" + agentKey + "\"\n" +
		"description = \"Multi-key test.\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"key_alpha = \"value_alpha\"\n" +
		"key_beta = \"value_beta\"\n" +
		"developer_instructions = \"\\n<CommunicationProtocol type=\\\"managed\\\">\\nold content\\n</CommunicationProtocol>\\n\"\n")

	canonical := codexRefreshDecodeToml(t, rawTOML)
	req := makeCodexRefreshRequest(canonical, rawTOML, agentKey, RefreshProtocolOnly)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	m := parseTomlMap(t, result.Output)

	if v, _ := m["key_alpha"].(string); v != "value_alpha" {
		t.Errorf("key_alpha = %q after refresh; want %q", v, "value_alpha")
	}
	if v, _ := m["key_beta"].(string); v != "value_beta" {
		t.Errorf("key_beta = %q after refresh; want %q", v, "value_beta")
	}
}

// ---------------------------------------------------------------------------
// T10.4 -- No-op refresh leaves the file semantically unchanged
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_Codex_NoOpRefresh_OwnedKeysUnchanged verifies that refreshing a
// deployed Codex agent whose CommunicationProtocol region already holds exactly the
// canonical content leaves every owned key with its original value. The assertions are on
// decoded values rather than raw bytes: re-encoding is not required to be byte-identical,
// but it must change nothing a user can observe.
//
// This is the test that fails if encode's deploy-path normalisations (description fallback,
// name forcing, sandbox_mode read-only fallback) leak into the refresh path.
func TestRefreshHarnessOnly_Codex_NoOpRefresh_OwnedKeysUnchanged(t *testing.T) {
	const agentKey = "stem-key"
	const userName = "My Pretty Agent Name"
	const userDesc = "" // blank description: must not be replaced by the fallback
	const userSandbox = "write"

	// Build canonical with the current protocol block already in place.
	// The protocol content is the fixture subagent block, which is what refresh would write.
	// The protocol version in the tag matches the fixture version.
	protoContent := string(fixtureRefreshProtocol(fixtureRefreshProtocolVersion).Blocks[domain.ProtocolSubagent])
	protoVersion := fixtureRefreshProtocolVersion

	canonicalBody := "\n<CommunicationProtocol type=\"managed\" version=\"" + protoVersion + "\">\n" +
		protoContent +
		"</CommunicationProtocol>\n"

	// Canonical frontmatter: name diverges from agentKey; description is blank; sandbox is "write".
	// These are deliberately non-default values to prove normalisations are suppressed.
	canonical := []byte("---\nname: " + userName + "\ndescription: \"\"\nsandbox_mode: " + userSandbox + "\n---\n" + canonicalBody)

	// Encode to TOML for DeployedRaw.
	rawTOML := codexRefreshToml(t, canonical, agentKey)

	req := makeCodexRefreshRequest(canonical, rawTOML, agentKey, RefreshProtocolOnly)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly no-op: %v", err)
	}

	// Decode the output TOML for value-level comparison (not byte comparison).
	decodedOut := codexRefreshDecodeToml(t, result.Output)

	// Parse the decoded canonical form to extract frontmatter values.
	// We use the TOML output directly since parsing canonical YAML is harder from here.
	outMap := parseTomlMap(t, result.Output)

	// name must still be the user-supplied name (not forced to agentKey).
	if name, _ := outMap["name"].(string); name != userName {
		t.Errorf("name = %q after no-op refresh; want %q (must not be forced to agent key)", name, userName)
	}

	// description must still be blank (fallback must not be invented).
	if desc, _ := outMap["description"].(string); desc != userDesc {
		t.Errorf("description = %q after no-op refresh; want %q (blank description must not become fallback)", desc, userDesc)
	}

	// sandbox_mode must still be "write" (must not become "read-only" fallback).
	if sandbox, _ := outMap["sandbox_mode"].(string); sandbox != userSandbox {
		t.Errorf("sandbox_mode = %q after no-op refresh; want %q (explicit user value must survive)", sandbox, userSandbox)
	}

	// developer_instructions must still contain the protocol content (not lost).
	instructions, _ := outMap["developer_instructions"].(string)
	if !strings.Contains(instructions, protoContent) {
		t.Errorf("developer_instructions does not contain the expected protocol block after no-op refresh")
	}

	// The decoded output must be non-nil (basic sanity).
	if len(decodedOut) == 0 {
		t.Error("decoded output is empty after no-op refresh")
	}
}

// TestRefreshHarnessOnly_Codex_NoOpRefresh_UserKeyUnchanged verifies that a no-op refresh
// does not remove a user-added key. The user key must have the same value after refresh as
// before, because refresh is supposed to change nothing the user can observe.
func TestRefreshHarnessOnly_Codex_NoOpRefresh_UserKeyUnchanged(t *testing.T) {
	const agentKey = "codex-noop-userkey"

	// Build the protocol content and construct the canonical body with the block in place.
	protoContent := string(fixtureRefreshProtocol(fixtureRefreshProtocolVersion).Blocks[domain.ProtocolSubagent])
	protoVersion := fixtureRefreshProtocolVersion

	canonicalBody := "\n<CommunicationProtocol type=\"managed\" version=\"" + protoVersion + "\">\n" +
		protoContent +
		"</CommunicationProtocol>\n"

	// Canonical form with a user key in the mosaic_carriage container (value-carried).
	// goccy/go-yaml decodes the mosaic_carriage mapping as map[string]interface{}.
	canonicalForEncode := []byte("---\nname: " + agentKey +
		"\ndescription: No-op user key test.\nsandbox_mode: read-only\n" +
		"mosaic_carriage:\n  user_setting: my_preserved_value\n---\n" + canonicalBody)

	// Encode to TOML using the registered translator (handles multi-line body correctly).
	rawTOML := codexRefreshToml(t, canonicalForEncode, agentKey)

	// Decode back to obtain the accurate canonical form, including the user key in carriage.
	canonical := codexRefreshDecodeToml(t, rawTOML)

	req := makeCodexRefreshRequest(canonical, rawTOML, agentKey, RefreshProtocolOnly)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly no-op with user key: %v", err)
	}

	m := parseTomlMap(t, result.Output)

	if v, _ := m["user_setting"].(string); v != "my_preserved_value" {
		t.Errorf("user_setting = %q after no-op refresh; want %q (user key must be unchanged)", v, "my_preserved_value")
	}
}

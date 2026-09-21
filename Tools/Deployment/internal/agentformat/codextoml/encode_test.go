package codextoml_test

// encode_test.go covers the Codex TOML encode path: valid TOML output, key values,
// normalisations, foreign-key handling, name-divergence, empty-body failure, and the
// stripped-field reporting channel. Tests for key classification (T2.3a), sandbox_mode
// fallback (T2.3b), hostile content (T2.4), and stamp block (T2.5) are in their own
// files in this directory.

import (
	"errors"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
)

// makeCanonical builds a minimal canonical MOSAIC Markdown document.
// frontmatterYAML must end with a newline. body is appended after the closing delimiter.
func makeCanonical(frontmatterYAML, body string) []byte {
	return []byte("---\n" + frontmatterYAML + "---\n" + body)
}

// codexCtx returns a minimal ArtifactContext for Codex TOML encode tests.
func codexCtx(agentKey string) agentformat.ArtifactContext {
	return agentformat.ArtifactContext{
		AgentKey: agentKey,
		Op:       agentformat.OpCreate,
	}
}

// codexTranslator returns the registered Codex TOML translator or fails the test.
func codexTranslator(t *testing.T) agentformat.Translator {
	t.Helper()
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup(CodexTOML): %v", err)
	}
	return tr
}

// parseToml parses TOML bytes into a map and fails the test if parsing fails.
func parseToml(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("TOML parse failed: %v\nBytes:\n%s", err, b)
	}
	return m
}

// TestCodexEncode_ProducesValidTOML verifies that a well-formed canonical document
// encodes to bytes that a standard TOML parser accepts without error.
func TestCodexEncode_ProducesValidTOML(t *testing.T) {
	canonical := makeCanonical(
		"name: my-agent\ndescription: A test agent.\nmodel: gpt-5\nsandbox_mode: read-only\n",
		"These are the agent instructions.\n",
	)

	tr := codexTranslator(t)
	out, _, err := tr.Encode(canonical, codexCtx("my-agent"))
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	parseToml(t, out) // fails if not valid TOML
}

// TestCodexEncode_NameFromAgentKey verifies that the encoded TOML name key equals
// ctx.AgentKey, not any name value in the canonical frontmatter.
func TestCodexEncode_NameFromAgentKey(t *testing.T) {
	canonical := makeCanonical(
		"name: canonical-name\ndescription: A test agent.\n",
		"Body text.\n",
	)

	tr := codexTranslator(t)
	ctx := codexCtx("agent-key-wins")
	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	m := parseToml(t, out)
	name, ok := m["name"].(string)
	if !ok {
		t.Fatal("Encoded TOML has no string 'name' key")
	}
	if name != "agent-key-wins" {
		t.Errorf("name = %q; want %q (ctx.AgentKey, not canonical frontmatter)", name, "agent-key-wins")
	}
}

// TestCodexEncode_DescriptionFallback_WhenBlank verifies that a blank or absent
// description in canonical input becomes "MOSAIC agent " + ctx.AgentKey in the output.
func TestCodexEncode_DescriptionFallback_WhenBlank(t *testing.T) {
	cases := []struct {
		name      string
		canonical []byte
	}{
		{
			name:     "absent_description",
			canonical: makeCanonical("name: test-agent\n", "Body.\n"),
		},
		{
			name:     "blank_description",
			canonical: makeCanonical("name: test-agent\ndescription: \n", "Body.\n"),
		},
	}

	tr := codexTranslator(t)
	agentKey := "test-agent"
	wantDesc := "MOSAIC agent " + agentKey

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := tr.Encode(tc.canonical, codexCtx(agentKey))
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			m := parseToml(t, out)
			desc, ok := m["description"].(string)
			if !ok {
				t.Fatal("Encoded TOML has no string 'description' key")
			}
			if desc != wantDesc {
				t.Errorf("description = %q; want %q (fallback)", desc, wantDesc)
			}
		})
	}
}

// TestCodexEncode_ModelKeyOmitted_WhenNoModel verifies that when the canonical input
// carries no model key (or an empty one), the encoded TOML omits the model key entirely.
func TestCodexEncode_ModelKeyOmitted_WhenNoModel(t *testing.T) {
	cases := []struct {
		name      string
		canonical []byte
	}{
		{
			name:     "absent_model",
			canonical: makeCanonical("name: a\ndescription: d\n", "Body.\n"),
		},
		{
			name:     "empty_model",
			canonical: makeCanonical("name: a\ndescription: d\nmodel: \n", "Body.\n"),
		},
	}

	tr := codexTranslator(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := tr.Encode(tc.canonical, codexCtx("a"))
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			m := parseToml(t, out)
			if _, hasModel := m["model"]; hasModel {
				t.Error("Encoded TOML contains a 'model' key when source had no/empty model; key must be omitted")
			}
		})
	}
}

// TestCodexEncode_ForeignKey_DroppedAndNotInTOML verifies that a key in row three of
// the classification table (generic vocabulary that Codex does not emit) is absent from
// the encoded TOML output.
func TestCodexEncode_ForeignKey_DroppedAndNotInTOML(t *testing.T) {
	// "role" is a known MOSAIC generic key but not in the Codex-native emitted set.
	canonical := makeCanonical(
		"name: a\ndescription: d\nrole: subagent\n",
		"Body.\n",
	)

	tr := codexTranslator(t)
	out, _, err := tr.Encode(canonical, codexCtx("a"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	m := parseToml(t, out)
	if _, ok := m["role"]; ok {
		t.Error("Encoded TOML contains 'role' key; foreign keys must be dropped")
	}
}

// TestCodexEncode_ForeignKey_AppearsInReport verifies that when a foreign key is
// dropped, the report contains an EntryDroppedForeignKey entry for that key.
func TestCodexEncode_ForeignKey_AppearsInReport(t *testing.T) {
	canonical := makeCanonical(
		"name: a\ndescription: d\nrole: subagent\n",
		"Body.\n",
	)

	tr := codexTranslator(t)
	_, report, err := tr.Encode(canonical, codexCtx("a"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	foundDrop := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryDroppedForeignKey && entry.Key == "role" {
			foundDrop = true
			if entry.Detail == "" {
				t.Error("EntryDroppedForeignKey for 'role' has empty Detail; Detail must contain the dropped value")
			}
			if entry.Reason == "" {
				t.Error("EntryDroppedForeignKey for 'role' has empty Reason; Reason must be non-empty")
			}
			break
		}
	}
	if !foundDrop {
		t.Errorf("Report does not contain EntryDroppedForeignKey for 'role'; got entries: %v", report.Entries)
	}
}

// TestCodexEncode_ForeignKey_ContainerShapedKey_DroppedAndReported verifies that a
// container-shaped key (mosaic_carriage) arriving in canonical input is consumed by the
// carriage path: the container key must not appear in the TOML output, the contained
// user key is re-emitted as a native TOML key, and the report contains EntryCarriedContainer
// (not EntryDroppedForeignKey) for the re-emitted user key.
func TestCodexEncode_ForeignKey_ContainerShapedKey_DroppedAndReported(t *testing.T) {
	// mosaic_carriage is the carriage container; when present in canonical input it is
	// consumed by the carriage encode path. The container key itself never appears in
	// the TOML output, and user_key is re-emitted as a native TOML key.
	canonical := makeCanonical(
		"name: a\ndescription: d\nmosaic_carriage:\n  user_key: value\n",
		"Body.\n",
	)

	tr := codexTranslator(t)
	out, report, err := tr.Encode(canonical, codexCtx("a"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	m := parseToml(t, out)
	if _, ok := m["mosaic_carriage"]; ok {
		t.Error("Encoded TOML contains 'mosaic_carriage'; carriage containers must not appear in encoded output")
	}

	// The carriage path must report EntryCarriedContainer for the re-emitted user key,
	// not EntryDroppedForeignKey. EntryDroppedForeignKey is reserved for truly foreign
	// keys (not the carriage container).
	foundCarried := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryCarriedContainer && entry.Key == "user_key" {
			foundCarried = true
			break
		}
	}
	if !foundCarried {
		t.Errorf("Report does not contain EntryCarriedContainer for 'user_key'; got entries: %v", report.Entries)
	}
}

// TestCodexEncode_NameDivergence_AgentKeyWins verifies that when the canonical
// frontmatter carries a name different from ctx.AgentKey, the agent key wins.
func TestCodexEncode_NameDivergence_AgentKeyWins(t *testing.T) {
	canonical := makeCanonical(
		"name: wrong-name\ndescription: d\n",
		"Body.\n",
	)

	tr := codexTranslator(t)
	out, _, err := tr.Encode(canonical, codexCtx("correct-key"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	m := parseToml(t, out)
	if name, _ := m["name"].(string); name != "correct-key" {
		t.Errorf("name = %q; want %q (agent key must win over canonical frontmatter name)", name, "correct-key")
	}
}

// TestCodexEncode_NameDivergence_ReportedAsOverride verifies that when the canonical
// frontmatter carries a name different from ctx.AgentKey, the divergent name is
// reported as EntryOverriddenName.
func TestCodexEncode_NameDivergence_ReportedAsOverride(t *testing.T) {
	const divergentName = "wrong-name"
	canonical := makeCanonical(
		"name: "+divergentName+"\ndescription: d\n",
		"Body.\n",
	)

	tr := codexTranslator(t)
	_, report, err := tr.Encode(canonical, codexCtx("correct-key"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	foundOverride := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryOverriddenName && entry.Key == "name" {
			foundOverride = true
			if entry.Detail != divergentName {
				t.Errorf("EntryOverriddenName Detail = %q; want %q (the divergent name that was lost)", entry.Detail, divergentName)
			}
			if entry.Reason == "" {
				t.Error("EntryOverriddenName for 'name' has empty Reason; Reason must be non-empty")
			}
			break
		}
	}
	if !foundOverride {
		t.Errorf("Report does not contain EntryOverriddenName for 'name'; got entries: %v", report.Entries)
	}
}

// TestCodexEncode_EmptyBody_FailsWithErrEmptyBody verifies that when the agent body
// is empty, Encode fails with an error wrapping ErrEmptyBody. This is not suppressed
// by RefreshMode: no instructions fallback can be invented.
func TestCodexEncode_EmptyBody_FailsWithErrEmptyBody(t *testing.T) {
	canonical := makeCanonical(
		"name: a\ndescription: d\n",
		"", // empty body
	)

	tr := codexTranslator(t)
	_, _, err := tr.Encode(canonical, codexCtx("a"))
	if err == nil {
		t.Fatal("Encode with empty body returned nil error; want error wrapping ErrEmptyBody")
	}
	if !errors.Is(err, agentformat.ErrEmptyBody) {
		t.Errorf("Encode empty body error = %v; want error wrapping ErrEmptyBody", err)
	}
}

// TestCodexEncode_EmptyBody_AlsoFailsInRefreshMode verifies that the empty-body
// failure is NOT suppressed by ctx.RefreshMode, unlike the other normalisations.
func TestCodexEncode_EmptyBody_AlsoFailsInRefreshMode(t *testing.T) {
	canonical := makeCanonical(
		"name: a\ndescription: d\n",
		"",
	)

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:    "a",
		Op:          agentformat.OpCreate,
		RefreshMode: true,
	}
	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode with empty body in RefreshMode returned nil error; ErrEmptyBody must not be suppressed")
	}
	if !errors.Is(err, agentformat.ErrEmptyBody) {
		t.Errorf("Encode empty body in RefreshMode error = %v; want error wrapping ErrEmptyBody", err)
	}
}

// TestCodexEncode_AllRequiredKeysPresent verifies that a well-formed encode produces
// all the keys the Codex format requires (name, description, developer_instructions)
// and that sandbox_mode is always present.
func TestCodexEncode_AllRequiredKeysPresent(t *testing.T) {
	canonical := makeCanonical(
		"name: agent\ndescription: An agent.\nmodel: gpt-5\nsandbox_mode: read-only\n",
		"These are the instructions.\n",
	)

	tr := codexTranslator(t)
	out, _, err := tr.Encode(canonical, codexCtx("agent"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	m := parseToml(t, out)

	required := []string{"name", "description", "developer_instructions", "sandbox_mode"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("Encoded TOML missing required key %q", key)
		}
	}
}

// TestCodexEncode_DeveloperInstructions_EqualsBody verifies that the
// developer_instructions value in the encoded TOML equals the canonical body bytes.
func TestCodexEncode_DeveloperInstructions_EqualsBody(t *testing.T) {
	body := "# Instructions\n\nDo something useful.\n"
	canonical := makeCanonical(
		"name: a\ndescription: d\n",
		body,
	)

	tr := codexTranslator(t)
	out, _, err := tr.Encode(canonical, codexCtx("a"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	m := parseToml(t, out)
	got, ok := m["developer_instructions"].(string)
	if !ok {
		t.Fatal("developer_instructions is not a string in encoded TOML")
	}
	if got != body {
		t.Errorf("developer_instructions = %q; want %q (byte-identical to canonical body)", got, body)
	}
}

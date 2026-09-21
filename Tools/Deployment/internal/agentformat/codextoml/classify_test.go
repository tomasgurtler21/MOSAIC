package codextoml_test

// classify_test.go pins the vocabulary boundary defined in the stage preamble. It
// covers T2.3a (key-classification in all four directions) and T2.3b (sandbox_mode
// fallback). These tests are the acceptance gate for the correctness of isMosaicOwned,
// isStampKey, and emittedKeys, and they are designed so that an implementation built
// on either of the two forbidden predicates (IsKnownMosaicKey, IsMosaicOnlyDeployedKey)
// cannot make all cases pass simultaneously.

import (
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/codextoml"
	"mosaic-deploy/internal/agentformat/formatid"
)

// classifyCtx returns a minimal ArtifactContext for classification tests.
func classifyCtx(agentKey string) agentformat.ArtifactContext {
	return agentformat.ArtifactContext{
		AgentKey: agentKey,
		Op:       agentformat.OpCreate,
	}
}

// classifyEncode calls Encode and returns the parsed TOML map and the report.
// It fails the test if Encode returns an error or the output is not valid TOML.
func classifyEncode(t *testing.T, frontmatterYAML, body string, agentKey string) (map[string]interface{}, agentformat.Report) {
	t.Helper()
	canonical := []byte("---\n" + frontmatterYAML + "---\n" + body)
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup CodexTOML: %v", err)
	}
	out, report, encErr := tr.Encode(canonical, classifyCtx(agentKey))
	if encErr != nil {
		t.Fatalf("Encode error: %v", encErr)
	}
	var m map[string]interface{}
	if tomlErr := toml.Unmarshal(out, &m); tomlErr != nil {
		t.Fatalf("Encoded output is not valid TOML: %v\nOutput:\n%s", tomlErr, out)
	}
	return m, report
}

// hasStampComment reports whether the raw TOML bytes contain a comment line of the
// form "# <key>: <value>" anywhere in the leading comment block.
func hasStampComment(b []byte, key string) bool {
	prefix := []byte("# " + key + ":")
	lines := splitLines(b)
	for _, line := range lines {
		// Stop at first non-comment, non-blank line (end of header).
		trimmed := trimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if len(trimmed) > 0 && trimmed[0] != '#' {
			break
		}
		if hasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// splitLines splits b on newline boundaries.
func splitLines(b []byte) [][]byte {
	var lines [][]byte
	for len(b) > 0 {
		i := indexOf(b, '\n')
		if i < 0 {
			lines = append(lines, b)
			break
		}
		lines = append(lines, b[:i+1])
		b = b[i+1:]
	}
	return lines
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i, c := range prefix {
		if b[i] != c {
			return false
		}
	}
	return true
}

func indexOf(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// findReportEntry returns the first report entry matching the given kind and key.
func findReportEntry(report agentformat.Report, kind agentformat.EntryKind, key string) (agentformat.ReportEntry, bool) {
	for _, e := range report.Entries {
		if e.Kind == kind && e.Key == key {
			return e, true
		}
	}
	return agentformat.ReportEntry{}, false
}

// --- T2.3a(a): Codex-native emitted keys appear as native TOML keys ---

// TestClassification_EmittedKeys_AreNativeTOMLKeys verifies that each of the five
// Codex-native emitted keys (name, description, model, sandbox_mode,
// developer_instructions) is emitted as a native TOML key when present in canonical
// input. sandbox_mode is the critical case: it is not in agentfields and a
// registry-driven rule would drop it.
func TestClassification_EmittedKeys_AreNativeTOMLKeys(t *testing.T) {
	// Build a canonical document that carries all five keys. developer_instructions
	// becomes the body in TOML; the others are in frontmatter.
	agentKey := "classify-test"
	m, _ := classifyEncode(t,
		"name: classify-test\ndescription: A test.\nmodel: gpt-5\nsandbox_mode: read-only\n",
		"Instructions body.\n",
		agentKey,
	)

	// (a) sandbox_mode must be a TOML key, not absent. A registry-driven rule would
	// drop it because it is absent from agentfields.
	if _, ok := m["sandbox_mode"]; !ok {
		t.Error("sandbox_mode is absent from encoded TOML; it must be a native TOML key even though it is not in agentfields")
	}

	// name must be present (comes from AgentKey).
	if _, ok := m["name"]; !ok {
		t.Error("name is absent from encoded TOML")
	}

	// description must be present.
	if _, ok := m["description"]; !ok {
		t.Error("description is absent from encoded TOML")
	}

	// model must be present (was supplied).
	if _, ok := m["model"]; !ok {
		t.Error("model is absent from encoded TOML")
	}

	// developer_instructions carries the body.
	if _, ok := m["developer_instructions"]; !ok {
		t.Error("developer_instructions is absent from encoded TOML")
	}
}

// TestClassification_DeveloperInstructions_IsNativeTOMLKey separately asserts that
// developer_instructions is a native TOML key (the body is embedded as the value).
func TestClassification_DeveloperInstructions_IsNativeTOMLKey(t *testing.T) {
	body := "This is the body.\n"
	m, _ := classifyEncode(t,
		"name: a\ndescription: d\n",
		body,
		"a",
	)
	got, ok := m["developer_instructions"].(string)
	if !ok {
		t.Fatal("developer_instructions is not a string TOML key in encoded output")
	}
	if got != body {
		t.Errorf("developer_instructions = %q; want %q", got, body)
	}
}

// --- T2.3a(b): Deployed stamp names go to the comment block, not a TOML key ---

// TestClassification_DeployedStampNames_GoToCommentBlock iterates agentfields.All()
// and for each entry asserts that its Deployed name (e.g. mosaic_id, mosaic_version)
// appears in the comment block of the encoded TOML and NOT as a native TOML key. This
// test fails if the stamp predicate is built on Deployed names from agentfields.All().
func TestClassification_DeployedStampNames_GoToCommentBlock(t *testing.T) {
	tr, _ := agentformat.Lookup(formatid.CodexTOML)

	for _, f := range agentfields.All() {
		f := f // capture
		t.Run(f.Deployed, func(t *testing.T) {
			// Build a canonical document that carries this stamp's Deployed name.
			canonical := []byte("---\n" +
				"name: a\ndescription: d\n" +
				f.Deployed + ": stamp-value\n" +
				"---\nBody.\n")
			out, _, err := tr.Encode(canonical, classifyCtx("a"))
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}

			// Assert: the Deployed name must NOT be a native TOML key.
			var m map[string]interface{}
			if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
				t.Fatalf("Encoded output is not valid TOML: %v\nOutput:\n%s", parseErr, out)
			}
			if _, ok := m[f.Deployed]; ok {
				t.Errorf("%q appears as a native TOML key; it must go to the comment block", f.Deployed)
			}

			// Assert: the Deployed name must appear in the comment block.
			if !hasStampComment(out, f.Deployed) {
				t.Errorf("%q is absent from the comment block; every Deployed stamp must appear there", f.Deployed)
			}
		})
	}
}

// --- T2.3a(b2): Legacy names (where different from Deployed) are foreign ---

// TestClassification_LegacyNames_TreatedAsForeign iterates agentfields.All() and for
// each entry whose Legacy name differs from its Deployed name asserts that the Legacy
// name is NOT treated as a stamp. It must take the row-three foreign path: dropped and
// reported. This is the assertion that fails an implementation built on
// agentfields.IsMosaicOnlyDeployedKey (which also matches Legacy names, causing id,
// role, and version to be routed to the comment block as if they were stamps).
func TestClassification_LegacyNames_TreatedAsForeign(t *testing.T) {
	tr, _ := agentformat.Lookup(formatid.CodexTOML)

	for _, f := range agentfields.All() {
		f := f
		if f.Legacy == f.Deployed || f.Legacy == "" {
			continue // only test entries with a distinct Legacy name
		}
		t.Run(f.Legacy, func(t *testing.T) {
			canonical := []byte("---\n" +
				"name: a\ndescription: d\n" +
				f.Legacy + ": legacy-value\n" +
				"---\nBody.\n")
			out, report, err := tr.Encode(canonical, classifyCtx("a"))
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}

			// Assert: the Legacy name must NOT appear as a native TOML key.
			var m map[string]interface{}
			if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
				t.Fatalf("Encoded output is not valid TOML: %v\nOutput:\n%s", parseErr, out)
			}
			if _, ok := m[f.Legacy]; ok {
				t.Errorf("%q (Legacy of %q) appears as a native TOML key; it must be treated as foreign", f.Legacy, f.Deployed)
			}

			// Assert: the Legacy name must NOT appear in the comment block as a stamp.
			if hasStampComment(out, f.Legacy) {
				t.Errorf("%q (Legacy of %q) appears in the comment block; Legacy names must not be treated as stamps", f.Legacy, f.Deployed)
			}

			// Assert: the Legacy name must be reported as a dropped foreign key.
			if entry, found := findReportEntry(report, agentformat.EntryDroppedForeignKey, f.Legacy); !found {
				t.Errorf("Report does not contain EntryDroppedForeignKey for Legacy name %q; got: %v", f.Legacy, report.Entries)
			} else if entry.Reason == "" {
				t.Errorf("EntryDroppedForeignKey for Legacy name %q has empty Reason; Reason must be non-empty", f.Legacy)
			}
		})
	}
}

// --- T2.3a(c): Generic vocabulary keys Codex never emits are dropped and reported ---

// TestClassification_GenericVocabulary_DroppedAndReported verifies that each of the
// following generic-vocabulary keys is dropped from encoded output and reported:
// role, id, version, tools, recommended_tier, tier_rationale, required_skills.
//
// These all return true from agentfields.IsKnownMosaicKey (they are the generic
// vocabulary). The first three (id, role, version) also return true from
// agentfields.IsMosaicOnlyDeployedKey because they are Legacy names. This test catches
// both discarded rules: an emitter using IsKnownMosaicKey would emit them (wrong); an
// emitter using IsMosaicOnlyDeployedKey as the stamp predicate would route id, role, and
// version to the comment block (also wrong). tools is the sharpest emit case: Codex has
// no tools key at all and emitting one would produce a file Codex rejects.
func TestClassification_GenericVocabulary_DroppedAndReported(t *testing.T) {
	foreignKeys := []string{
		"role",            // IsKnownMosaicKey=true, IsMosaicOnlyDeployedKey=true (Legacy)
		"id",              // IsKnownMosaicKey=true, IsMosaicOnlyDeployedKey=true (Legacy)
		"version",         // IsKnownMosaicKey=true, IsMosaicOnlyDeployedKey=true (Legacy)
		"tools",           // IsKnownMosaicKey=true; Codex has no tools key
		"recommended_tier",
		"tier_rationale",
		"required_skills",
	}

	tr, _ := agentformat.Lookup(formatid.CodexTOML)

	for _, key := range foreignKeys {
		key := key
		t.Run(key, func(t *testing.T) {
			canonical := []byte("---\n" +
				"name: a\ndescription: d\n" +
				key + ": some-value\n" +
				"---\nBody.\n")
			out, report, err := tr.Encode(canonical, classifyCtx("a"))
			if err != nil {
				t.Fatalf("Encode error for key %q: %v", key, err)
			}

			// Assert: the key must NOT be in the native TOML output.
			var m map[string]interface{}
			if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
				t.Fatalf("Encoded output is not valid TOML: %v", parseErr)
			}
			if _, ok := m[key]; ok {
				t.Errorf("%q is present as a TOML key; it must be dropped (Codex does not emit it)", key)
			}

			// Assert: the key must be reported as a dropped foreign key.
			if entry, found := findReportEntry(report, agentformat.EntryDroppedForeignKey, key); !found {
				t.Errorf("Report does not contain EntryDroppedForeignKey for %q; got: %v", key, report.Entries)
			} else if entry.Reason == "" {
				t.Errorf("EntryDroppedForeignKey for %q has empty Reason; Reason must be non-empty", key)
			}
		})
	}
}

// --- T2.3a(d): isMosaicOwned predicate agrees with all four directions ---

// TestClassification_IsMosaicOwned_TrueForEmittedKeys verifies that IsMosaicOwned
// returns true for each of the five Codex-native emitted keys.
func TestClassification_IsMosaicOwned_TrueForEmittedKeys(t *testing.T) {
	emitted := []string{"name", "description", "model", "sandbox_mode", "developer_instructions"}
	for _, key := range emitted {
		key := key
		t.Run(key, func(t *testing.T) {
			if !codextoml.IsMosaicOwned(key) {
				t.Errorf("IsMosaicOwned(%q) = false; want true (emitted key)", key)
			}
		})
	}
}

// TestClassification_IsMosaicOwned_TrueForDeployedStampNames verifies that
// IsMosaicOwned returns true for every Deployed stamp name in agentfields.All().
func TestClassification_IsMosaicOwned_TrueForDeployedStampNames(t *testing.T) {
	for _, f := range agentfields.All() {
		f := f
		t.Run(f.Deployed, func(t *testing.T) {
			if !codextoml.IsMosaicOwned(f.Deployed) {
				t.Errorf("IsMosaicOwned(%q) = false; want true (Deployed stamp name)", f.Deployed)
			}
		})
	}
}

// TestClassification_IsMosaicOwned_FalseForLegacyNames verifies that IsMosaicOwned
// returns false for every Legacy stamp name that differs from its Deployed name.
// This is consistent with the row-three foreign treatment: Legacy names are user-owned
// and carriable, not MOSAIC-owned.
func TestClassification_IsMosaicOwned_FalseForLegacyNames(t *testing.T) {
	for _, f := range agentfields.All() {
		f := f
		if f.Legacy == f.Deployed || f.Legacy == "" {
			continue
		}
		t.Run(f.Legacy, func(t *testing.T) {
			if codextoml.IsMosaicOwned(f.Legacy) {
				t.Errorf("IsMosaicOwned(%q) = true; want false (Legacy name, must be foreign/user-owned)", f.Legacy)
			}
		})
	}
}

// TestClassification_IsMosaicOwned_FalseForGenericVocabulary verifies that IsMosaicOwned
// returns false for the generic-vocabulary keys Codex does not emit. This assertion is
// consistent with those keys being row-three foreign (dropped and reported), and
// downstream being carriable as user keys. It also confirms that neither IsKnownMosaicKey
// nor IsMosaicOnlyDeployedKey was used as the implementation.
func TestClassification_IsMosaicOwned_FalseForGenericVocabulary(t *testing.T) {
	generic := []string{
		"role", "id", "version", "tools", "recommended_tier", "tier_rationale", "required_skills",
	}
	for _, key := range generic {
		key := key
		t.Run(key, func(t *testing.T) {
			if codextoml.IsMosaicOwned(key) {
				t.Errorf("IsMosaicOwned(%q) = true; want false (generic vocabulary, Codex does not emit it)", key)
			}
		})
	}
}

// TestClassification_IsStampKey_TrueForDeployedNames verifies that IsStampKey returns
// true for every Deployed name in agentfields.All().
func TestClassification_IsStampKey_TrueForDeployedNames(t *testing.T) {
	for _, f := range agentfields.All() {
		f := f
		t.Run(f.Deployed, func(t *testing.T) {
			if !codextoml.IsStampKey(f.Deployed) {
				t.Errorf("IsStampKey(%q) = false; want true", f.Deployed)
			}
		})
	}
}

// TestClassification_IsStampKey_FalseForLegacyNames verifies that IsStampKey returns
// false for Legacy names that differ from Deployed names. This is the Deployed-only
// contract that distinguishes isStampKey from IsMosaicOnlyDeployedKey.
func TestClassification_IsStampKey_FalseForLegacyNames(t *testing.T) {
	for _, f := range agentfields.All() {
		f := f
		if f.Legacy == f.Deployed || f.Legacy == "" {
			continue
		}
		t.Run(f.Legacy, func(t *testing.T) {
			if codextoml.IsStampKey(f.Legacy) {
				t.Errorf("IsStampKey(%q) = true; want false (Legacy name must not be treated as a stamp)", f.Legacy)
			}
		})
	}
}

// TestClassification_IsStampKey_FalseForEmittedKeys verifies that isStampKey returns
// false for sandbox_mode and developer_instructions, which are in the emitted set but
// not in agentfields (they are Codex-format-native, not MOSAIC stamps).
func TestClassification_IsStampKey_FalseForEmittedKeys(t *testing.T) {
	notStamps := []string{"sandbox_mode", "developer_instructions"}
	for _, key := range notStamps {
		key := key
		t.Run(key, func(t *testing.T) {
			if codextoml.IsStampKey(key) {
				t.Errorf("IsStampKey(%q) = true; want false (Codex-native emitted key, not a stamp)", key)
			}
		})
	}
}

// --- T2.3b: sandbox_mode fallback ---

// TestSandboxMode_Absent_EmitsReadOnly_InDeployMode verifies that when canonical input
// carries no sandbox_mode, the encoded TOML file contains sandbox_mode = "read-only".
// The assertion is made by parsing the emitted TOML and checking key presence, not by
// string search, so an absent key cannot pass as an empty match.
func TestSandboxMode_Absent_EmitsReadOnly_InDeployMode(t *testing.T) {
	// No sandbox_mode in canonical input.
	canonical := []byte("---\nname: a\ndescription: d\n---\nBody.\n")

	tr, _ := agentformat.Lookup(formatid.CodexTOML)
	out, _, err := tr.Encode(canonical, classifyCtx("a"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	var m map[string]interface{}
	if tomlErr := toml.Unmarshal(out, &m); tomlErr != nil {
		t.Fatalf("Encoded output is not valid TOML: %v\nOutput:\n%s", tomlErr, out)
	}

	// The key must be present (not absent), asserted by map lookup, not string search.
	val, ok := m["sandbox_mode"]
	if !ok {
		t.Fatal("sandbox_mode is absent from encoded TOML; it must be present (read-only fallback) even when not in canonical input")
	}
	if val != "read-only" {
		t.Errorf("sandbox_mode = %v; want %q (fail-closed read-only fallback)", val, "read-only")
	}
}

// TestSandboxMode_Absent_ReportsAppliedFallback verifies that when sandbox_mode is
// absent from canonical input, the report contains an EntryAppliedFallback entry for
// sandbox_mode. The fallback is visible rather than silent.
func TestSandboxMode_Absent_ReportsAppliedFallback(t *testing.T) {
	canonical := []byte("---\nname: a\ndescription: d\n---\nBody.\n")

	tr, _ := agentformat.Lookup(formatid.CodexTOML)
	_, report, err := tr.Encode(canonical, classifyCtx("a"))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	foundFallback := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "sandbox_mode" {
			foundFallback = true
			if entry.Detail == "" {
				t.Error("EntryAppliedFallback for sandbox_mode has empty Detail; Detail must contain the invented value")
			}
			if entry.Reason == "" {
				t.Error("EntryAppliedFallback for sandbox_mode has empty Reason; Reason must be non-empty")
			}
			break
		}
	}
	if !foundFallback {
		t.Errorf("Report does not contain EntryAppliedFallback for sandbox_mode; got: %v", report.Entries)
	}
}

// TestSandboxMode_ExplicitValue_EmittedUnchanged verifies that when canonical input
// carries an explicit sandbox_mode, it is emitted unchanged. The fallback must not
// override a supplied mode.
func TestSandboxMode_ExplicitValue_EmittedUnchanged(t *testing.T) {
	cases := []string{"read-only", "workspace-write", "custom-mode"}
	tr, _ := agentformat.Lookup(formatid.CodexTOML)

	for _, mode := range cases {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			canonical := []byte("---\nname: a\ndescription: d\nsandbox_mode: " + mode + "\n---\nBody.\n")
			out, _, err := tr.Encode(canonical, classifyCtx("a"))
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			var m map[string]interface{}
			if tomlErr := toml.Unmarshal(out, &m); tomlErr != nil {
				t.Fatalf("Encoded output is not valid TOML: %v", tomlErr)
			}
			got, ok := m["sandbox_mode"].(string)
			if !ok {
				t.Fatal("sandbox_mode is absent or not a string in encoded TOML")
			}
			if got != mode {
				t.Errorf("sandbox_mode = %q; want %q (explicit value must not be overridden)", got, mode)
			}
		})
	}
}

// TestSandboxMode_Absent_DoesNotReportFallbackInRefreshMode verifies that when
// ctx.RefreshMode is true and canonical input carries NO sandbox_mode, the fallback
// is suppressed: no read-only value is invented and the key remains absent from the
// encoded TOML. No EntryAppliedFallback entry must appear in the report.
//
// This is the T2.3b suppression case. The canonical input deliberately carries no
// sandbox_mode so that any implementation that ignores RefreshMode and always invents
// "read-only" will fail this test.
func TestSandboxMode_Absent_DoesNotReportFallbackInRefreshMode(t *testing.T) {
	// No sandbox_mode in canonical input — this is the key difference from the
	// explicit-value tests.
	canonical := []byte("---\nname: a\ndescription: d\n---\nBody.\n")

	tr, _ := agentformat.Lookup(formatid.CodexTOML)
	ctx := agentformat.ArtifactContext{
		AgentKey:    "a",
		Op:          agentformat.OpCreate,
		RefreshMode: true,
	}
	out, report, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode in RefreshMode error: %v", err)
	}
	var m map[string]interface{}
	if tomlErr := toml.Unmarshal(out, &m); tomlErr != nil {
		t.Fatalf("Encoded output is not valid TOML: %v", tomlErr)
	}
	// The key must remain absent: in RefreshMode the fallback must not be invented.
	if _, ok := m["sandbox_mode"]; ok {
		t.Errorf("sandbox_mode is present in encoded TOML in RefreshMode with absent canonical value; fallback must be suppressed (key must remain absent)")
	}
	// No EntryAppliedFallback for sandbox_mode must appear in the report.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "sandbox_mode" {
			t.Error("Report contains EntryAppliedFallback for sandbox_mode in RefreshMode; fallback must be suppressed")
			break
		}
	}
}

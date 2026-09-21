package codextoml_test

// refresh_mode_test.go pins the contract of ArtifactContext.RefreshMode in the Codex
// TOML translator. These tests live in the translator package because the flag is
// translator behaviour: a future translator refactor can silently drop the suppression
// and only an app-level test two layers up will notice unless the contract is pinned here.
//
// Every ArtifactContext in this file carries Op: OpUpdate explicitly. Refresh always
// operates on an existing file, so OpUpdate is the only valid operation on this path.
// OpUnspecified would raise ErrUnspecifiedOperation before the refresh-mode behaviour is
// reached, making every sub-case unreachable.
//
// Sub-cases:
//
//   (a) Refresh mode suppresses deploy-path normalisations: blank description stays
//       blank, a name diverging from the agent key stays as given, an absent
//       sandbox_mode stays absent, and a foreign key present in the canonical document
//       is not dropped from the encoded output.
//
//   (b) The empty-body abort is retained in refresh mode -- the one deploy-path
//       behaviour the flag does not suppress, because no instructions fallback can be
//       invented.
//
//   (c) Default mode (RefreshMode == false) with Op: OpUpdate produces the same
//       normalised output as today: the description fallback fires, the name is forced
//       to the agent key, an absent sandbox_mode becomes "read-only", and a foreign key
//       is dropped and reported as EntryDroppedForeignKey.
//
//   (d) A context with RefreshMode true but nil PriorDeployed raises
//       ErrMissingPriorBytes. Refresh always operates on an existing file, so prior
//       bytes are mandatory. A wiring error that forgets to thread them must fail loudly.

import (
	"errors"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
)

// refreshCtx returns a minimal ArtifactContext for refresh-mode tests.
// Op is always OpUpdate; PriorDeployed is set to the provided prior bytes.
func refreshCtx(agentKey string, priorBytes []byte) agentformat.ArtifactContext {
	return agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
		RefreshMode:   true,
	}
}

// defaultUpdateCtx returns an ArtifactContext with Op: OpUpdate and RefreshMode false,
// for testing that default-mode behaviour is unchanged on the update path.
func defaultUpdateCtx(agentKey string, priorBytes []byte) agentformat.ArtifactContext {
	return agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
		RefreshMode:   false,
	}
}

// minimalRefreshPrior returns a minimal valid Codex TOML byte slice for use as
// PriorDeployed in refresh-mode tests.
func minimalRefreshPrior(agentKey string) []byte {
	return []byte("name = \"" + agentKey + "\"\ndescription = \"Prior description.\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"Prior body.\"\n")
}

// parseRefreshToml parses TOML bytes into a map and fails the test if parsing fails.
func parseRefreshToml(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("TOML parse failed: %v\nBytes:\n%s", err, b)
	}
	return m
}

// refreshTranslator returns the registered Codex TOML translator or fails the test.
func refreshTranslator(t *testing.T) agentformat.Translator {
	t.Helper()
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup(CodexTOML): %v", err)
	}
	return tr
}

// --- (a) Refresh mode suppresses deploy-path normalisations ---

// TestRefreshMode_BlankDescription_StaysBlank verifies that in refresh mode a blank
// (empty-string) description in the canonical frontmatter is not replaced by the
// deterministic fallback. The fallback fires only in deploy mode.
func TestRefreshMode_BlankDescription_StaysBlank(t *testing.T) {
	const agentKey = "refresh-agent"
	// Canonical input with an explicitly empty description: description: ""
	canonical := []byte("---\nname: " + agentKey + "\ndescription: \"\"\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	desc, ok := m["description"].(string)
	if !ok {
		t.Fatal("Encoded TOML has no string 'description' key")
	}
	if desc != "" {
		t.Errorf("description = %q in refresh mode; want empty string (fallback must be suppressed)", desc)
	}

	// No EntryAppliedFallback for description must appear in the report.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "description" {
			t.Error("Report contains EntryAppliedFallback for 'description' in refresh mode; fallback must be suppressed")
			break
		}
	}
}

// TestRefreshMode_AbsentDescription_StaysAbsent verifies that in refresh mode an absent
// description (not present in the canonical frontmatter at all) stays absent from the
// encoded output. The fallback "MOSAIC agent <key>" must not be invented.
func TestRefreshMode_AbsentDescription_StaysAbsent(t *testing.T) {
	const agentKey = "refresh-agent"
	// Canonical input with no description key at all.
	canonical := []byte("---\nname: " + agentKey + "\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	if desc, ok := m["description"].(string); ok {
		// Only the fallback-invented value should be rejected; an empty string is still
		// a present key, so check for the fallback text specifically.
		fallback := "MOSAIC agent " + agentKey
		if desc == fallback {
			t.Errorf("description = %q in refresh mode; the fallback must not be invented (absent description must stay absent or be empty)", desc)
		}
	}

	// No EntryAppliedFallback for description.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "description" {
			t.Error("Report contains EntryAppliedFallback for 'description' in refresh mode; fallback must be suppressed")
			break
		}
	}
}

// TestRefreshMode_DivergentName_StaysAsGiven verifies that in refresh mode a name in
// the canonical frontmatter that differs from ctx.AgentKey is preserved. The name-forcing
// normalisation must not fire; the user's own name must be emitted.
func TestRefreshMode_DivergentName_StaysAsGiven(t *testing.T) {
	const agentKey = "stem-name"
	const userSuppliedName = "My Custom Agent"
	// Canonical input with a name that differs from the agent key.
	canonical := []byte("---\nname: " + userSuppliedName + "\ndescription: A description.\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	name, ok := m["name"].(string)
	if !ok {
		t.Fatal("Encoded TOML has no string 'name' key")
	}
	if name != userSuppliedName {
		t.Errorf("name = %q in refresh mode; want %q (user-supplied name must not be forced to agent key)", name, userSuppliedName)
	}

	// No EntryOverriddenName must appear in the report.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryOverriddenName && entry.Key == "name" {
			t.Error("Report contains EntryOverriddenName in refresh mode; name forcing must be suppressed")
			break
		}
	}
}

// TestRefreshMode_AbsentSandboxMode_StaysAbsent verifies that in refresh mode an absent
// sandbox_mode stays absent from the encoded output. The "read-only" fallback that
// fires in deploy mode must be suppressed; a Codex file refreshed without sandbox_mode
// must remain without it.
func TestRefreshMode_AbsentSandboxMode_StaysAbsent(t *testing.T) {
	const agentKey = "refresh-agent"
	// Canonical input with no sandbox_mode key.
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\n---\nBody text.\n")

	// Prior bytes also have no sandbox_mode, to avoid it being injected via carriage.
	prior := []byte("name = \"" + agentKey + "\"\ndescription = \"Prior description.\"\ndeveloper_instructions = \"Body text.\"\n")

	tr := refreshTranslator(t)
	out, report, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	if _, hasSandbox := m["sandbox_mode"]; hasSandbox {
		t.Error("sandbox_mode is present in encoded TOML in refresh mode with absent canonical value; fallback must be suppressed (key must remain absent)")
	}

	// No EntryAppliedFallback for sandbox_mode.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "sandbox_mode" {
			t.Error("Report contains EntryAppliedFallback for 'sandbox_mode' in refresh mode; fallback must be suppressed")
			break
		}
	}
}

// TestRefreshMode_ForeignKey_SurvivesAndIsNotDropped verifies that in refresh mode a
// key that would be classified as foreign (not in the Codex-native emitted set, not a
// stamp, not a carriage container key) is not dropped from the encoded output. In deploy
// mode the same key is dropped and reported as EntryDroppedForeignKey; in refresh mode
// the drop normalisation is suppressed and the key must appear in the TOML output.
func TestRefreshMode_ForeignKey_SurvivesAndIsNotDropped(t *testing.T) {
	const agentKey = "refresh-agent"
	// "extra_setting" is not in the Codex emittedKeys set and is not a MOSAIC stamp.
	// In deploy mode it would be dropped and reported as foreign.
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\nextra_setting: custom_value\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	if _, ok := m["extra_setting"]; !ok {
		t.Error("extra_setting is absent from encoded TOML in refresh mode; foreign-key drop must be suppressed (key must survive)")
	}

	// No EntryDroppedForeignKey for this key in refresh mode.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryDroppedForeignKey && entry.Key == "extra_setting" {
			t.Error("Report contains EntryDroppedForeignKey for 'extra_setting' in refresh mode; drop reporting must be suppressed")
			break
		}
	}
}

// TestRefreshMode_CarriageKey_SurvivesInOutput verifies that a user-owned key in the
// mosaic_carriage container of the canonical form survives in the encoded TOML output
// when RefreshMode is true. This is the normal path user keys take after decode.
func TestRefreshMode_CarriageKey_SurvivesInOutput(t *testing.T) {
	const agentKey = "refresh-agent"
	// Canonical form with a user key in the carriage container.
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\nmosaic_carriage:\n  my_user_key: my_value\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := []byte("name = \"" + agentKey + "\"\ndescription = \"Prior description.\"\nsandbox_mode = \"read-only\"\nmy_user_key = \"my_value\"\ndeveloper_instructions = \"Body text.\"\n")
	out, _, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	val, ok := m["my_user_key"].(string)
	if !ok {
		t.Error("my_user_key is absent from encoded TOML in refresh mode; carriage-carried user key must survive")
	} else if val != "my_value" {
		t.Errorf("my_user_key = %q; want %q", val, "my_value")
	}
}

// --- (b) Empty-body abort is retained in refresh mode ---

// TestRefreshMode_EmptyBody_AbortIsRetained verifies that the empty-body abort fires
// even when RefreshMode is true. An empty developer_instructions is not representable;
// no fallback can be invented, so the one deploy-path behaviour RefreshMode does not
// suppress is this abort. Op is OpUpdate as required for all refresh-mode contexts.
func TestRefreshMode_EmptyBody_AbortIsRetained(t *testing.T) {
	const agentKey = "refresh-agent"
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\n---\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	_, _, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err == nil {
		t.Fatal("Encode with empty body in refresh mode returned nil error; ErrEmptyBody must not be suppressed by RefreshMode")
	}
	if !errors.Is(err, agentformat.ErrEmptyBody) {
		t.Errorf("Encode empty body in refresh mode error = %v; want error wrapping ErrEmptyBody", err)
	}
}

// --- (c) Default mode (RefreshMode false) with Op: OpUpdate is unchanged ---

// TestDefaultMode_DescriptionFallback_FiresOnUpdate verifies that the description
// fallback fires in default mode with Op: OpUpdate, just as it does on the create path.
func TestDefaultMode_DescriptionFallback_FiresOnUpdate(t *testing.T) {
	const agentKey = "deploy-agent"
	// No description in canonical input.
	canonical := []byte("---\nname: " + agentKey + "\n---\nBody text.\n")
	wantDesc := "MOSAIC agent " + agentKey

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, defaultUpdateCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in default mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	desc, ok := m["description"].(string)
	if !ok {
		t.Fatal("Encoded TOML has no string 'description' key")
	}
	if desc != wantDesc {
		t.Errorf("description = %q in default mode; want %q (fallback must fire)", desc, wantDesc)
	}

	// EntryAppliedFallback for description must appear in the report.
	foundFallback := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "description" {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Error("Report does not contain EntryAppliedFallback for 'description' in default mode; fallback must be reported")
	}
}

// TestDefaultMode_NameForcing_FiresOnUpdate verifies that the name-forcing normalisation
// fires in default mode with Op: OpUpdate. The agent key must win over a divergent name
// in the canonical frontmatter.
func TestDefaultMode_NameForcing_FiresOnUpdate(t *testing.T) {
	const agentKey = "correct-key"
	const wrongName = "wrong-name"
	canonical := []byte("---\nname: " + wrongName + "\ndescription: A description.\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, defaultUpdateCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in default mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	name, ok := m["name"].(string)
	if !ok {
		t.Fatal("Encoded TOML has no string 'name' key")
	}
	if name != agentKey {
		t.Errorf("name = %q in default mode; want %q (agent key must win)", name, agentKey)
	}

	// EntryOverriddenName must appear in the report.
	foundOverride := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryOverriddenName && entry.Key == "name" {
			foundOverride = true
			break
		}
	}
	if !foundOverride {
		t.Error("Report does not contain EntryOverriddenName in default mode; name override must be reported")
	}
}

// TestDefaultMode_SandboxModeFallback_FiresOnUpdate verifies that an absent sandbox_mode
// in canonical input becomes "read-only" in default mode with Op: OpUpdate.
func TestDefaultMode_SandboxModeFallback_FiresOnUpdate(t *testing.T) {
	const agentKey = "deploy-agent"
	// Canonical input with no sandbox_mode.
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\n---\nBody text.\n")

	// Prior bytes without sandbox_mode so no carriage injects it.
	prior := []byte("name = \"" + agentKey + "\"\ndescription = \"Prior description.\"\ndeveloper_instructions = \"Body text.\"\n")

	tr := refreshTranslator(t)
	out, report, err := tr.Encode(canonical, defaultUpdateCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in default mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	sandbox, ok := m["sandbox_mode"].(string)
	if !ok {
		t.Fatal("sandbox_mode is absent from encoded TOML in default mode; fallback must fire")
	}
	if sandbox != "read-only" {
		t.Errorf("sandbox_mode = %q in default mode; want %q (fallback)", sandbox, "read-only")
	}

	// EntryAppliedFallback for sandbox_mode must appear in the report.
	foundFallback := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryAppliedFallback && entry.Key == "sandbox_mode" {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Error("Report does not contain EntryAppliedFallback for 'sandbox_mode' in default mode; fallback must be reported")
	}
}

// TestDefaultMode_ForeignKey_IsDroppedAndReported verifies that a foreign key in the
// canonical frontmatter is dropped from the encoded TOML and reported as
// EntryDroppedForeignKey in default mode with Op: OpUpdate.
func TestDefaultMode_ForeignKey_IsDroppedAndReported(t *testing.T) {
	const agentKey = "deploy-agent"
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\nextra_setting: custom_value\n---\nBody text.\n")

	tr := refreshTranslator(t)
	prior := minimalRefreshPrior(agentKey)
	out, report, err := tr.Encode(canonical, defaultUpdateCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in default mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)
	if _, ok := m["extra_setting"]; ok {
		t.Error("extra_setting is present in encoded TOML in default mode; foreign key must be dropped")
	}

	// EntryDroppedForeignKey must appear in the report.
	foundDrop := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryDroppedForeignKey && entry.Key == "extra_setting" {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Error("Report does not contain EntryDroppedForeignKey for 'extra_setting' in default mode; drop must be reported")
	}
}

// TestDefaultMode_AllNormalisations_ProduceNormalisedOutput verifies that all four
// deploy-path normalisations fire together in default mode with Op: OpUpdate. This is
// the "same inputs produce exactly today's normalised output" assertion that guards
// against an inverted or always-on RefreshMode flag.
func TestDefaultMode_AllNormalisations_ProduceNormalisedOutput(t *testing.T) {
	const agentKey = "normalise-agent"
	// Canonical input that exercises all four normalisations:
	//   - description absent              -> fallback fires
	//   - name diverges from agent key    -> name forced to key
	//   - sandbox_mode absent             -> becomes "read-only"
	//   - extra_key is foreign            -> dropped and reported
	canonical := []byte("---\nname: divergent-name\nextra_key: extra_value\n---\nBody text.\n")

	// Prior bytes without sandbox_mode so the fallback fires cleanly.
	prior := []byte("name = \"normalise-agent\"\ndescription = \"d\"\ndeveloper_instructions = \"Body text.\"\n")

	tr := refreshTranslator(t)
	out, report, err := tr.Encode(canonical, defaultUpdateCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in default mode returned error: %v", err)
	}

	m := parseRefreshToml(t, out)

	// Name must be the agent key.
	if name, _ := m["name"].(string); name != agentKey {
		t.Errorf("name = %q; want %q (agent key must win in default mode)", name, agentKey)
	}

	// Description must be the fallback.
	wantDesc := "MOSAIC agent " + agentKey
	if desc, _ := m["description"].(string); desc != wantDesc {
		t.Errorf("description = %q; want %q (fallback must fire in default mode)", desc, wantDesc)
	}

	// sandbox_mode must be "read-only".
	if sandbox, _ := m["sandbox_mode"].(string); sandbox != "read-only" {
		t.Errorf("sandbox_mode = %q; want %q (fallback must fire in default mode)", sandbox, "read-only")
	}

	// extra_key must be absent.
	if _, ok := m["extra_key"]; ok {
		t.Error("extra_key is present in encoded TOML in default mode; foreign key must be dropped")
	}

	// Report must include EntryDroppedForeignKey for extra_key.
	foundDrop := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryDroppedForeignKey && entry.Key == "extra_key" {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Error("Report does not contain EntryDroppedForeignKey for 'extra_key' in default mode")
	}

	// Verify the output is valid TOML.
	parseRefreshToml(t, out)
}

// --- (d) Refresh mode with nil PriorDeployed raises ErrMissingPriorBytes ---

// TestRefreshMode_NilPriorDeployed_RaisesErrMissingPriorBytes verifies that a context
// with RefreshMode true and Op: OpUpdate but nil PriorDeployed raises ErrMissingPriorBytes.
// Refresh always operates on an existing file, so prior bytes are mandatory. A wiring
// error that omits them must fail loudly rather than silently discarding the user's keys.
func TestRefreshMode_NilPriorDeployed_RaisesErrMissingPriorBytes(t *testing.T) {
	const agentKey = "refresh-agent"
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A description.\n---\nBody text.\n")

	tr := refreshTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpUpdate,
		PriorDeployed: nil, // the wiring error: refresh forgot to thread the raw bytes
		RefreshMode:   true,
	}
	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode with RefreshMode=true and nil PriorDeployed returned nil error; ErrMissingPriorBytes must be raised")
	}
	if !errors.Is(err, agentformat.ErrMissingPriorBytes) {
		t.Errorf("Encode with RefreshMode=true and nil PriorDeployed returned %v; want error wrapping ErrMissingPriorBytes", err)
	}
}

// TestRefreshMode_EncodesValidTOML verifies that a successful refresh-mode encode
// produces bytes that a standard TOML parser accepts. This is the basic sanity check
// that all the suppressed normalisations do not break the output structure.
func TestRefreshMode_EncodesValidTOML(t *testing.T) {
	const agentKey = "refresh-agent"
	canonical := []byte("---\nname: " + agentKey + "\ndescription: A user description.\nmodel: gpt-5\nsandbox_mode: write\n---\nMulti-line body.\nSecond line.\n")

	tr := refreshTranslator(t)
	prior := []byte("name = \"" + agentKey + "\"\ndescription = \"A user description.\"\nmodel = \"gpt-5\"\nsandbox_mode = \"write\"\ndeveloper_instructions = \"Multi-line body.\\nSecond line.\\n\"\n")
	out, _, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Encode in refresh mode returned error: %v", err)
	}

	// Must parse as valid TOML.
	m := parseRefreshToml(t, out)

	// Spot-check a few key values.
	if name, _ := m["name"].(string); name != agentKey {
		t.Errorf("name = %q; want %q", name, agentKey)
	}
	if desc, _ := m["description"].(string); desc != "A user description." {
		t.Errorf("description = %q; want %q", desc, "A user description.")
	}
	if sandbox, _ := m["sandbox_mode"].(string); sandbox != "write" {
		t.Errorf("sandbox_mode = %q; want %q (explicit user value must be preserved in refresh mode)", sandbox, "write")
	}
}

// TestRefreshMode_Contrast_SuppressedVsDefault provides a direct A/B comparison:
// the same canonical inputs and agent key encoded once with RefreshMode true and once
// with RefreshMode false (default), using Op: OpUpdate in both cases. It asserts that
// the two outputs differ in the expected ways: description is absent vs fallback, name
// is user-supplied vs agent key, sandbox_mode absent vs "read-only".
func TestRefreshMode_Contrast_SuppressedVsDefault(t *testing.T) {
	const agentKey = "stem"
	const userNameInCanonical = "Pretty Name"
	// Canonical with no description (absent, not empty string), divergent name,
	// no sandbox_mode, no foreign keys. The absent description is what triggers the
	// fallback in default mode (the fallback fires on nil/absent, not on "").
	canonical := []byte("---\nname: Pretty Name\n---\nBody.\n")

	// Prior bytes without sandbox_mode for the refresh case.
	prior := []byte("name = \"Pretty Name\"\ndeveloper_instructions = \"Body.\"\n")

	tr := refreshTranslator(t)

	// Refresh mode encode.
	refreshOut, _, err := tr.Encode(canonical, refreshCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Refresh mode encode error: %v", err)
	}
	refreshMap := parseRefreshToml(t, refreshOut)

	// Default mode encode (needs prior bytes that satisfy sandbox_mode fallback check).
	defaultOut, _, err := tr.Encode(canonical, defaultUpdateCtx(agentKey, prior))
	if err != nil {
		t.Fatalf("Default mode encode error: %v", err)
	}
	defaultMap := parseRefreshToml(t, defaultOut)

	// In refresh mode: name is from canonical frontmatter.
	if name := refreshMap["name"].(string); name != userNameInCanonical {
		t.Errorf("refresh: name = %q; want %q (user name preserved)", name, userNameInCanonical)
	}
	// In default mode: name is the agent key.
	if name := defaultMap["name"].(string); name != agentKey {
		t.Errorf("default: name = %q; want %q (agent key wins)", name, agentKey)
	}

	// In refresh mode: description is absent (no fallback invented).
	// The key may be absent or have an empty value; what must NOT happen is the
	// fallback string "MOSAIC agent stem" appearing.
	wantFallback := "MOSAIC agent " + agentKey
	if desc, ok := refreshMap["description"].(string); ok && desc == wantFallback {
		t.Errorf("refresh: description = %q; fallback must not be invented in refresh mode", desc)
	}
	// In default mode: description is the fallback.
	if desc := defaultMap["description"].(string); desc != wantFallback {
		t.Errorf("default: description = %q; want %q (fallback)", desc, wantFallback)
	}

	// In refresh mode: sandbox_mode is absent (prior had none, no fallback).
	if _, hasSandbox := refreshMap["sandbox_mode"]; hasSandbox {
		t.Error("refresh: sandbox_mode is present; fallback must be suppressed")
	}
	// In default mode: sandbox_mode is "read-only".
	if sandbox := defaultMap["sandbox_mode"].(string); sandbox != "read-only" {
		t.Errorf("default: sandbox_mode = %q; want %q (fallback)", sandbox, "read-only")
	}

	// The two outputs must be different (this guards against RefreshMode being ignored).
	if strings.EqualFold(string(refreshOut), string(defaultOut)) {
		t.Error("refresh mode output equals default mode output; RefreshMode must produce different results for this input")
	}
}

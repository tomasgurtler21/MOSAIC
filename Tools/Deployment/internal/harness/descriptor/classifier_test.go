package descriptor_test

// classifier_test.go covers the Stage 2 FieldClassifier addition:
//
//   NewFieldClassifier — builds a classifier from a descriptor and a frontmatter plan.
//   Classify — classifies one frontmatter key as mosaic, harness, diverted-tool, or unknown.
//   DivertedField — returns the diverted-field declaration for a ClassDivertedTool key.
//   HarnessKeys — returns every key classified ClassHarness, sorted.
//   DropCandidateKeys — returns the subset of ClassHarness keys the harness actually adds.
//
// These tests are the TDD RED phase for I2.2. They compile against the stubs in
// classifier.go and fail because the stub always returns ClassUnknown, nil slices, etc.
//
// Verified behaviours:
//
// Classify — ClassMosaic:
//   - A generic vocabulary key (id, version, name, description, role, model, tools,
//     recommended_tier, tier_rationale, required_skills) classifies as ClassMosaic.
//   - A MOSAIC-only deployed key under its legacy form (e.g. transform_version)
//     classifies as ClassMosaic.
//   - A MOSAIC-only deployed key under its prefixed form (e.g. mosaic_transform_version)
//     classifies as ClassMosaic.
//
// Classify — ClassHarness:
//   - The descriptor's ModelKey classifies as ClassHarness.
//   - The descriptor's ToolsKey classifies as ClassHarness.
//   - A key in FrontmatterPlan.Set classifies as ClassHarness.
//   - A key in FrontmatterSpec.Drop classifies as ClassHarness (it is known to the harness
//     even though it names a generic field the harness removes).
//   - A key in FrontmatterPlan.Remove classifies as ClassHarness (same reasoning).
//   - A key in FrontmatterSpec.KeyOrder classifies as ClassHarness.
//
// Classify — ClassDivertedTool:
//   - A key named as a DestField destination classifies as ClassDivertedTool.
//   - A descriptor without DestField destinations never produces ClassDivertedTool.
//
// Classify — ClassUnknown:
//   - A key that is none of the above classifies as ClassUnknown.
//
// Classify — precedence:
//   - ClassMosaic beats ClassDivertedTool: when a key is both a MOSAIC vocabulary key
//     and a DestField destination (pathological harness), ClassMosaic is returned.
//   - ClassMosaic beats ClassHarness: a protected generic key declared as ModelKey or
//     ToolsKey by a harness still classifies as ClassMosaic.
//   - ClassDivertedTool beats ClassHarness: a key that is both a DestField destination
//     and appears in FrontmatterPlan.Set classifies as ClassDivertedTool.
//
// DivertedField:
//   - Returns the DivertedToolField and true when Classify(key) == ClassDivertedTool.
//   - Returns false for a key that is not a diverted tool destination.
//
// HarnessKeys:
//   - Contains ModelKey, ToolsKey, plan Set keys, Spec.Drop keys, plan Remove keys,
//     and KeyOrder keys.
//   - Does NOT contain MOSAIC vocabulary keys (they are ClassMosaic, not ClassHarness).
//   - Does NOT contain ClassDivertedTool keys.
//   - Is sorted alphabetically.
//   - Does NOT include empty strings.
//
// DropCandidateKeys:
//   - Contains ModelKey, ToolsKey, plan Set keys.
//   - Does NOT contain Spec.Drop-only or plan Remove-only keys (AD-3).
//   - A key in both Set and Drop IS included in DropCandidateKeys (harness does add it).
//   - Is sorted alphabetically.
//
// HarnessKeys vs DropCandidateKeys split (AD-3 regression guard):
//   - A key exclusively in Spec.Drop appears in HarnessKeys but NOT DropCandidateKeys.
//   - A key exclusively in plan Remove appears in HarnessKeys but NOT DropCandidateKeys.
//   - A key in both plan Set and plan Remove appears in HarnessKeys AND DropCandidateKeys.

import (
	"sort"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// divertedClassifierDescriptorYAML declares a harness similar to Claude Code's shape:
// ModelKey="model", ToolsKey="tools", with one DestField destination ("mcpServers")
// and one DestMain destination. The descriptor also has Drop (generic keys the harness
// removes at deploy time) and KeyOrder entries.
const divertedClassifierDescriptorYAML = `schema_version: "1"
id: "classifier-harness"
display_name: "Classifier Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read-file"
    - generic: "mcp_skill"
      destinations:
        - to: field
          field: mcpServers
          format: list-block
          names:
            - "mcp-skill-server"
frontmatter:
  model_key: "model"
  tools_key: "tools"
  drop:
    - "recommended_tier"
    - "tier_rationale"
    - "required_skills"
  key_order:
    - "model"
    - "tools"
    - "mcpServers"
    - "mode"
`

// noDestFieldClassifierDescriptorYAML declares a harness without any DestField destinations.
const noDestFieldClassifierDescriptorYAML = `schema_version: "1"
id: "no-destfield-classifier-harness"
display_name: "No DestField Classifier Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read-file"
frontmatter:
  model_key: "llm_model"
  tools_key: "tool_list"
`

// buildClassifierWithPlan constructs a FieldClassifier from the given descriptor YAML
// and the supplied FrontmatterPlan. Fails the test on parse error.
func buildClassifierWithPlan(t *testing.T, yaml string, plan domain.FrontmatterPlan) descriptor.FieldClassifier {
	t.Helper()
	d := loadDescriptor(t, yaml)
	return descriptor.NewFieldClassifier(d, plan)
}

// buildClassifier constructs a FieldClassifier from the given descriptor YAML with a
// zero FrontmatterPlan.
func buildClassifier(t *testing.T, yaml string) descriptor.FieldClassifier {
	t.Helper()
	return buildClassifierWithPlan(t, yaml, domain.FrontmatterPlan{})
}

// ---------------------------------------------------------------------------
// Tests for Classify — ClassMosaic
// ---------------------------------------------------------------------------

// TestClassify_GenericVocabularyKey_IsMosaic verifies that every generic vocabulary key
// (the set from Catalog/SourceFilesFormat.md) classifies as ClassMosaic.
func TestClassify_GenericVocabularyKey_IsMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	genericVocab := []string{
		"id", "version", "name", "description", "role",
		"model", "tools", "recommended_tier", "tier_rationale", "required_skills",
	}
	for _, key := range genericVocab {
		got := clf.Classify(key)
		if got != descriptor.ClassMosaic {
			t.Errorf("Classify(%q) = %q, want %q; generic vocabulary keys must always classify as ClassMosaic",
				key, got, descriptor.ClassMosaic)
		}
	}
}

// TestClassify_LegacyMosaicDeployedKey_IsMosaic verifies that a MOSAIC-only deployed
// field under its legacy (unprefixed) name classifies as ClassMosaic.
func TestClassify_LegacyMosaicDeployedKey_IsMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	legacyKeys := []string{
		"transform_version",
		"injections_version",
		"tool_mappings_version",
		"bundle_version",
		"orchestrator_injections_version",
		"protocol_version",
	}
	for _, key := range legacyKeys {
		got := clf.Classify(key)
		if got != descriptor.ClassMosaic {
			t.Errorf("Classify(%q) = %q, want %q; legacy MOSAIC stamp keys must classify as ClassMosaic",
				key, got, descriptor.ClassMosaic)
		}
	}
}

// TestClassify_PrefixedMosaicDeployedKey_IsMosaic verifies that a MOSAIC-only deployed
// field under its mosaic_-prefixed name classifies as ClassMosaic.
// The list includes mosaic_role so that a regression in the classifier path for the role
// field (e.g. wrong predicate called, or a lookup miss) is caught at this boundary:
// a ClassUnknown result would cause the descriptor's unknown-field strip to silently remove
// mosaic_role from every deployed file on read.
func TestClassify_PrefixedMosaicDeployedKey_IsMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	prefixedKeys := []string{
		"mosaic_transform_version",
		"mosaic_injections_version",
		"mosaic_tool_mappings_version",
		"mosaic_bundle_version",
		"mosaic_orchestrator_injections_version",
		"mosaic_protocol_version",
		"mosaic_role",
	}
	for _, key := range prefixedKeys {
		got := clf.Classify(key)
		if got != descriptor.ClassMosaic {
			t.Errorf("Classify(%q) = %q, want %q; mosaic_-prefixed MOSAIC stamp keys must classify as ClassMosaic",
				key, got, descriptor.ClassMosaic)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for Classify — ClassHarness
// ---------------------------------------------------------------------------

// TestClassify_ModelKey_IsHarness verifies that the descriptor's declared ModelKey
// classifies as ClassHarness.
func TestClassify_ModelKey_IsHarness(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML) // ModelKey = "llm_model"

	got := clf.Classify("llm_model")
	if got != descriptor.ClassHarness {
		t.Errorf("Classify(%q) = %q, want %q; the descriptor's ModelKey must classify as ClassHarness",
			"llm_model", got, descriptor.ClassHarness)
	}
}

// TestClassify_ToolsKey_IsHarness verifies that the descriptor's declared ToolsKey
// classifies as ClassHarness.
func TestClassify_ToolsKey_IsHarness(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML) // ToolsKey = "tool_list"

	got := clf.Classify("tool_list")
	if got != descriptor.ClassHarness {
		t.Errorf("Classify(%q) = %q, want %q; the descriptor's ToolsKey must classify as ClassHarness",
			"tool_list", got, descriptor.ClassHarness)
	}
}

// TestClassify_PlanSetKey_IsHarness verifies that a key appearing in FrontmatterPlan.Set
// classifies as ClassHarness.
func TestClassify_PlanSetKey_IsHarness(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "harness_mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	got := clf.Classify("harness_mode")
	if got != descriptor.ClassHarness {
		t.Errorf("Classify(%q) = %q, want %q; a key in FrontmatterPlan.Set must classify as ClassHarness",
			"harness_mode", got, descriptor.ClassHarness)
	}
}

// TestClassify_SpecDropKey_IsHarness verifies that a key appearing in FrontmatterSpec.Drop
// classifies as ClassHarness. These are generic keys the harness removes at deploy time —
// they are "known to the harness" for the FR-3 question even though they are not added by it.
func TestClassify_SpecDropKey_IsHarness(t *testing.T) {
	// The divertedClassifierDescriptorYAML has Drop = ["recommended_tier", "tier_rationale", "required_skills"]
	// These are generic vocab keys, so ClassMosaic must beat ClassHarness — but we use a
	// non-MOSAIC key in the drop list to isolate the ClassHarness behaviour.
	clf := buildClassifier(t, `schema_version: "1"
id: "drop-test-harness"
display_name: "Drop Test Harness"
tools:
  shape: list
  universe: []
  mappings: []
frontmatter:
  model_key: "harness_model"
  tools_key: "harness_tools"
  drop:
    - "custom_generic_field"
`)

	got := clf.Classify("custom_generic_field")
	if got != descriptor.ClassHarness {
		t.Errorf("Classify(%q) = %q, want %q; a key in FrontmatterSpec.Drop is known to the harness and must classify as ClassHarness (FR-3)",
			"custom_generic_field", got, descriptor.ClassHarness)
	}
}

// TestClassify_PlanRemoveKey_IsHarness verifies that a key appearing in FrontmatterPlan.Remove
// classifies as ClassHarness. Same reasoning as SpecDropKey.
func TestClassify_PlanRemoveKey_IsHarness(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Remove: []string{"custom_removed_field"},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	got := clf.Classify("custom_removed_field")
	if got != descriptor.ClassHarness {
		t.Errorf("Classify(%q) = %q, want %q; a key in FrontmatterPlan.Remove is known to the harness and must classify as ClassHarness (FR-3)",
			"custom_removed_field", got, descriptor.ClassHarness)
	}
}

// TestClassify_KeyOrderKey_IsHarness verifies that a key appearing in FrontmatterSpec.KeyOrder
// classifies as ClassHarness. KeyOrder lists keys the harness expects to control ordering for —
// they are known to the harness and must not be treated as unknown fields.
func TestClassify_KeyOrderKey_IsHarness(t *testing.T) {
	// divertedClassifierDescriptorYAML has KeyOrder = ["model", "tools", "mcpServers", "mode"].
	// "mode" is not a MOSAIC vocabulary key, not a ModelKey/ToolsKey, not a DestField
	// destination, and not in any plan — it appears only in KeyOrder.
	clf := buildClassifier(t, divertedClassifierDescriptorYAML)

	got := clf.Classify("mode")
	if got != descriptor.ClassHarness {
		t.Errorf("Classify(%q) = %q, want %q; a key in FrontmatterSpec.KeyOrder is known to the harness and must classify as ClassHarness",
			"mode", got, descriptor.ClassHarness)
	}
}

// ---------------------------------------------------------------------------
// Tests for Classify — ClassDivertedTool
// ---------------------------------------------------------------------------

// TestClassify_DivertedDestinationKey_IsDivertedTool verifies that a key declared as a
// DestField destination classifies as ClassDivertedTool.
func TestClassify_DivertedDestinationKey_IsDivertedTool(t *testing.T) {
	clf := buildClassifier(t, divertedClassifierDescriptorYAML) // declares mcpServers as DestField

	got := clf.Classify("mcpServers")
	if got != descriptor.ClassDivertedTool {
		t.Errorf("Classify(%q) = %q, want %q; a DestField destination key must classify as ClassDivertedTool",
			"mcpServers", got, descriptor.ClassDivertedTool)
	}
}

// TestClassify_NoDestField_NeverProducesDivertedTool verifies that a descriptor without
// any DestField destinations never classifies any key as ClassDivertedTool.
func TestClassify_NoDestField_NeverProducesDivertedTool(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	// Try keys that would be diverted in other descriptors but not in this one.
	for _, key := range []string{"mcpServers", "extraField", "tool_field"} {
		got := clf.Classify(key)
		if got == descriptor.ClassDivertedTool {
			t.Errorf("Classify(%q) = ClassDivertedTool for a descriptor with no DestField destinations; only declared destinations may produce ClassDivertedTool",
				key)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for Classify — ClassUnknown
// ---------------------------------------------------------------------------

// TestClassify_UnknownKey_IsUnknown verifies that a key matching none of the known
// categories classifies as ClassUnknown.
func TestClassify_UnknownKey_IsUnknown(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	got := clf.Classify("completely_unknown_key")
	if got != descriptor.ClassUnknown {
		t.Errorf("Classify(%q) = %q, want %q; a key matching no known category must classify as ClassUnknown",
			"completely_unknown_key", got, descriptor.ClassUnknown)
	}
}

// ---------------------------------------------------------------------------
// Tests for Classify — precedence
// ---------------------------------------------------------------------------

// TestClassify_MosaicBeatsHarness_ProtectedKeyDeclaredAsModelKey verifies that when a
// harness declares a generic vocabulary key (e.g. "id") as its ModelKey, the key still
// classifies as ClassMosaic, not ClassHarness. This protects AD-4.
func TestClassify_MosaicBeatsHarness_ProtectedKeyDeclaredAsModelKey(t *testing.T) {
	d := loadDescriptor(t, `schema_version: "1"
id: "pathological-harness"
display_name: "Pathological Harness"
tools:
  shape: list
  universe: []
  mappings: []
frontmatter:
  model_key: "id"
  tools_key: "name"
`)
	clf := descriptor.NewFieldClassifier(d, domain.FrontmatterPlan{})

	for _, key := range []string{"id", "name"} {
		got := clf.Classify(key)
		if got != descriptor.ClassMosaic {
			t.Errorf("Classify(%q) = %q, want %q; ClassMosaic must beat ClassHarness — a harness declaring a MOSAIC vocabulary key as its own cannot reclassify it (AD-4)",
				key, got, descriptor.ClassMosaic)
		}
	}
}

// TestClassify_MosaicBeatesDivertedTool_VocabKeyAsDestField verifies that when a
// pathological harness uses a MOSAIC vocabulary key as a DestField destination, the key
// classifies as ClassMosaic, not ClassDivertedTool.
func TestClassify_MosaicBeatesDivertedTool_VocabKeyAsDestField(t *testing.T) {
	d := loadDescriptor(t, `schema_version: "1"
id: "pathological-field-harness"
display_name: "Pathological Field Harness"
tools:
  shape: list
  universe:
    - name: "some-tool"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: field
          field: tools
          format: list-block
          names:
            - "some-tool"
frontmatter:
  model_key: "harness_model"
  tools_key: "harness_tools"
`)
	clf := descriptor.NewFieldClassifier(d, domain.FrontmatterPlan{})

	// "tools" is both a generic vocabulary key and a DestField destination — ClassMosaic wins.
	got := clf.Classify("tools")
	if got != descriptor.ClassMosaic {
		t.Errorf("Classify(%q) = %q, want %q; ClassMosaic must beat ClassDivertedTool for MOSAIC vocabulary keys",
			"tools", got, descriptor.ClassMosaic)
	}
}

// TestClassify_DivertedToolBeatsHarness_DestFieldKeyInPlanSet verifies that when a key
// is both a DestField destination and appears in FrontmatterPlan.Set, ClassDivertedTool
// is returned (ClassDivertedTool beats ClassHarness).
func TestClassify_DivertedToolBeatsHarness_DestFieldKeyInPlanSet(t *testing.T) {
	plan := domain.FrontmatterPlan{
		// "mcpServers" is also a DestField destination in divertedClassifierDescriptorYAML.
		Set: []domain.FrontmatterField{
			{Key: "mcpServers", Value: domain.ScalarValue("x", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, divertedClassifierDescriptorYAML, plan)

	got := clf.Classify("mcpServers")
	if got != descriptor.ClassDivertedTool {
		t.Errorf("Classify(%q) = %q, want %q; ClassDivertedTool must beat ClassHarness when the key is both a DestField destination and a plan-set key",
			"mcpServers", got, descriptor.ClassDivertedTool)
	}
}

// ---------------------------------------------------------------------------
// Tests for DivertedField
// ---------------------------------------------------------------------------

// TestDivertedField_DivertedToolKey_ReturnsFieldAndTrue verifies that DivertedField
// returns the DivertedToolField declaration and true for a key that classifies as
// ClassDivertedTool.
func TestDivertedField_DivertedToolKey_ReturnsFieldAndTrue(t *testing.T) {
	clf := buildClassifier(t, divertedClassifierDescriptorYAML)

	f, ok := clf.DivertedField("mcpServers")
	if !ok {
		t.Fatal("DivertedField(\"mcpServers\") returned ok=false; must return true for a ClassDivertedTool key")
	}
	if f.Key != "mcpServers" {
		t.Errorf("DivertedField returned Key = %q, want %q", f.Key, "mcpServers")
	}
	if f.Format != domain.FormatListBlock {
		t.Errorf("DivertedField returned Format = %q, want %q", f.Format, domain.FormatListBlock)
	}
}

// TestDivertedField_NonDivertedKey_ReturnsFalse verifies that DivertedField returns
// false for a key that does not classify as ClassDivertedTool.
func TestDivertedField_NonDivertedKey_ReturnsFalse(t *testing.T) {
	clf := buildClassifier(t, divertedClassifierDescriptorYAML)

	_, ok := clf.DivertedField("completely_unknown_key")
	if ok {
		t.Error("DivertedField(\"completely_unknown_key\") returned ok=true; must return false for a non-diverted key")
	}
}

// ---------------------------------------------------------------------------
// Tests for HarnessKeys
// ---------------------------------------------------------------------------

// TestHarnessKeys_ContainsModelAndToolsKey verifies that HarnessKeys includes the
// descriptor's ModelKey and ToolsKey.
func TestHarnessKeys_ContainsModelAndToolsKey(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML) // ModelKey="llm_model", ToolsKey="tool_list"

	keys := clf.HarnessKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	if !keySet["llm_model"] {
		t.Errorf("HarnessKeys does not contain ModelKey %q; the harness's model key is always known to the harness", "llm_model")
	}
	if !keySet["tool_list"] {
		t.Errorf("HarnessKeys does not contain ToolsKey %q; the harness's tools key is always known to the harness", "tool_list")
	}
}

// TestHarnessKeys_ContainsPlanSetKeys verifies that keys from FrontmatterPlan.Set appear
// in HarnessKeys.
func TestHarnessKeys_ContainsPlanSetKeys(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "harness_mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
			{Key: "extra_field", Value: domain.ScalarValue("on", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	keys := clf.HarnessKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	for _, want := range []string{"harness_mode", "extra_field"} {
		if !keySet[want] {
			t.Errorf("HarnessKeys does not contain plan Set key %q; plan-added keys are known to the harness", want)
		}
	}
}

// TestHarnessKeys_ContainsSpecDropKeys verifies that keys from FrontmatterSpec.Drop appear
// in HarnessKeys. They are known to the harness because the harness removes them at deploy
// time (FR-3 "known to the harness" question).
func TestHarnessKeys_ContainsSpecDropKeys(t *testing.T) {
	clf := buildClassifier(t, `schema_version: "1"
id: "drop-keys-harness"
display_name: "Drop Keys Harness"
tools:
  shape: list
  universe: []
  mappings: []
frontmatter:
  model_key: "harness_model"
  tools_key: "harness_tools"
  drop:
    - "custom_generic_field"
    - "another_custom_field"
`)

	keys := clf.HarnessKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	for _, want := range []string{"custom_generic_field", "another_custom_field"} {
		if !keySet[want] {
			t.Errorf("HarnessKeys does not contain Spec.Drop key %q; Spec.Drop keys are known to the harness (FR-3)", want)
		}
	}
}

// TestHarnessKeys_ContainsPlanRemoveKeys verifies that keys from FrontmatterPlan.Remove
// appear in HarnessKeys. Same reasoning as SpecDropKeys.
func TestHarnessKeys_ContainsPlanRemoveKeys(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Remove: []string{"generic_field_removed_at_deploy"},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	keys := clf.HarnessKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	if !keySet["generic_field_removed_at_deploy"] {
		t.Errorf("HarnessKeys does not contain plan Remove key %q; Remove keys are known to the harness (FR-3)", "generic_field_removed_at_deploy")
	}
}

// TestHarnessKeys_ContainsKeyOrderKeys verifies that keys from FrontmatterSpec.KeyOrder
// appear in HarnessKeys. The harness controls ordering of these keys and therefore knows
// about them — they must not be treated as unknown fields during promote.
func TestHarnessKeys_ContainsKeyOrderKeys(t *testing.T) {
	// divertedClassifierDescriptorYAML has KeyOrder including "mode", which is not a
	// ModelKey, ToolsKey, DestField destination, or MOSAIC vocabulary key.
	clf := buildClassifier(t, divertedClassifierDescriptorYAML)

	keys := clf.HarnessKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	if !keySet["mode"] {
		t.Errorf("HarnessKeys does not contain KeyOrder key %q; keys in FrontmatterSpec.KeyOrder are known to the harness and must appear in HarnessKeys", "mode")
	}
}

// TestHarnessKeys_ExcludesDivertedToolKeys verifies that ClassDivertedTool keys are absent
// from HarnessKeys. A DestField destination key classifies as ClassDivertedTool — it lives
// in a separate class and must not also appear in HarnessKeys, as that would conflate two
// distinct accessor contracts.
func TestHarnessKeys_ExcludesDivertedToolKeys(t *testing.T) {
	// divertedClassifierDescriptorYAML has "mcpServers" as a DestField destination
	// (ClassDivertedTool). It is also listed in KeyOrder, but ClassDivertedTool
	// beats ClassHarness — HarnessKeys must not include it.
	clf := buildClassifier(t, divertedClassifierDescriptorYAML)

	keys := clf.HarnessKeys()
	for _, k := range keys {
		if k == "mcpServers" {
			t.Errorf("HarnessKeys contains ClassDivertedTool key %q; "+
				"diverted-tool keys must be excluded from HarnessKeys — they are accessible only via DivertedField()",
				k)
		}
	}
}

// TestHarnessKeys_IsSorted verifies that HarnessKeys returns its entries in alphabetical
// order, making the set's representation deterministic.
func TestHarnessKeys_IsSorted(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "zz_key", Value: domain.ScalarValue("x", domain.QuotePlain)},
			{Key: "aa_key", Value: domain.ScalarValue("y", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	keys := clf.HarnessKeys()
	if !sort.StringsAreSorted(keys) {
		t.Errorf("HarnessKeys is not sorted; got: %v", keys)
	}
}

// TestHarnessKeys_NoEmptyStrings verifies that HarnessKeys never includes empty strings,
// even when the descriptor or plan contains them.
func TestHarnessKeys_NoEmptyStrings(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "", Value: domain.ScalarValue("nokey", domain.QuotePlain)},
			{Key: "valid_key", Value: domain.ScalarValue("ok", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	for _, k := range clf.HarnessKeys() {
		if k == "" {
			t.Error("HarnessKeys contains an empty string; empty key names must be silently ignored")
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for DropCandidateKeys (AD-3 regression guard)
// ---------------------------------------------------------------------------

// TestDropCandidateKeys_ContainsModelAndToolsKey verifies that the descriptor's ModelKey
// and ToolsKey are included in DropCandidateKeys — they are harness-added fields.
func TestDropCandidateKeys_ContainsModelAndToolsKey(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML) // ModelKey="llm_model", ToolsKey="tool_list"

	keys := clf.DropCandidateKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	if !keySet["llm_model"] {
		t.Errorf("DropCandidateKeys does not contain ModelKey %q; the harness adds this key at deploy time", "llm_model")
	}
	if !keySet["tool_list"] {
		t.Errorf("DropCandidateKeys does not contain ToolsKey %q; the harness adds this key at deploy time", "tool_list")
	}
}

// TestDropCandidateKeys_ContainsPlanSetKeys verifies that keys from FrontmatterPlan.Set
// are included in DropCandidateKeys — the harness added them at deploy time.
func TestDropCandidateKeys_ContainsPlanSetKeys(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "harness_mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	keys := clf.DropCandidateKeys()
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	if !keySet["harness_mode"] {
		t.Errorf("DropCandidateKeys does not contain plan Set key %q; the harness adds Set keys at deploy time", "harness_mode")
	}
}

// TestDropCandidateKeys_SpecDropOnlyKey_NotIncluded is the AD-3 regression guard.
// A key exclusively in FrontmatterSpec.Drop names a generic field the harness removes
// at deploy time — exactly what promote is trying to restore. Including it in
// DropCandidateKeys would cause promote to remove the recovered field again.
func TestDropCandidateKeys_SpecDropOnlyKey_NotIncluded(t *testing.T) {
	clf := buildClassifier(t, `schema_version: "1"
id: "drop-only-harness"
display_name: "Drop Only Harness"
tools:
  shape: list
  universe: []
  mappings: []
frontmatter:
  model_key: "harness_model"
  tools_key: "harness_tools"
  drop:
    - "custom_generic_field"
`)

	dropCandidates := clf.DropCandidateKeys()
	dcSet := make(map[string]bool, len(dropCandidates))
	for _, k := range dropCandidates {
		dcSet[k] = true
	}

	// Guard: confirm the real drop-candidate keys are present so the assertion below
	// cannot pass vacuously if the implementation returns an empty set.
	for _, required := range []string{"harness_model", "harness_tools"} {
		if !dcSet[required] {
			t.Errorf("DropCandidateKeys guard: missing expected key %q; cannot trust the Spec.Drop exclusion assertion if the set is empty", required)
		}
	}

	// The Spec.Drop-only key must NOT appear — it names a generic field the harness
	// removes at deploy time, not a field the harness adds (AD-3).
	if dcSet["custom_generic_field"] {
		t.Errorf("DropCandidateKeys contains Spec.Drop-only key %q; AD-3 forbids this — "+
			"Spec.Drop names generic fields promote wants to restore, not harness-added fields",
			"custom_generic_field")
	}
}

// TestDropCandidateKeys_PlanRemoveOnlyKey_NotIncluded is the AD-3 regression guard
// for plan Remove. Same reasoning as SpecDropOnlyKey.
func TestDropCandidateKeys_PlanRemoveOnlyKey_NotIncluded(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Remove: []string{"generic_removed_field"},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	dropCandidates := clf.DropCandidateKeys()
	dcSet := make(map[string]bool, len(dropCandidates))
	for _, k := range dropCandidates {
		dcSet[k] = true
	}

	// Guard: confirm real keys are present.
	for _, required := range []string{"llm_model", "tool_list"} {
		if !dcSet[required] {
			t.Errorf("DropCandidateKeys guard: missing expected key %q", required)
		}
	}

	if dcSet["generic_removed_field"] {
		t.Errorf("DropCandidateKeys contains plan Remove-only key %q; AD-3 forbids this — "+
			"Remove names generic fields promote wants to restore (not harness-added fields)",
			"generic_removed_field")
	}
}

// TestDropCandidateKeys_KeyInBothSetAndDrop_IsIncluded verifies that a key appearing in
// BOTH FrontmatterPlan.Set and FrontmatterSpec.Drop is present in DropCandidateKeys,
// because the harness does add it (Set wins over Drop for the "does the harness add it?"
// question).
func TestDropCandidateKeys_KeyInBothSetAndDrop_IsIncluded(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			// "both_field" is added by the harness at deploy time (Set)...
			{Key: "both_field", Value: domain.ScalarValue("v", domain.QuotePlain)},
		},
		// ...and also listed in Remove (because the harness removes it for some agents).
		Remove: []string{"both_field"},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	dropCandidates := clf.DropCandidateKeys()
	dcSet := make(map[string]bool, len(dropCandidates))
	for _, k := range dropCandidates {
		dcSet[k] = true
	}

	if !dcSet["both_field"] {
		t.Errorf("DropCandidateKeys does not contain %q, which appears in both plan Set and Remove; "+
			"the harness does add this key, so it must be a drop candidate",
			"both_field")
	}
}

// TestDropCandidateKeys_IsSorted verifies that DropCandidateKeys returns entries in
// alphabetical order.
func TestDropCandidateKeys_IsSorted(t *testing.T) {
	plan := domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "zzz_key", Value: domain.ScalarValue("x", domain.QuotePlain)},
			{Key: "aaa_key", Value: domain.ScalarValue("y", domain.QuotePlain)},
		},
	}
	clf := buildClassifierWithPlan(t, noDestFieldClassifierDescriptorYAML, plan)

	keys := clf.DropCandidateKeys()
	if !sort.StringsAreSorted(keys) {
		t.Errorf("DropCandidateKeys is not sorted; got: %v", keys)
	}
}

// TestHarnessKeys_vs_DropCandidateKeys_DropOnlyKeyInHarnessButNotDrop verifies the
// complete set relationship: a key exclusively in Spec.Drop is in HarnessKeys but not
// in DropCandidateKeys. This is the direct AD-3 assertion.
func TestHarnessKeys_vs_DropCandidateKeys_DropOnlyKeyInHarnessButNotDrop(t *testing.T) {
	clf := buildClassifier(t, `schema_version: "1"
id: "ad3-test-harness"
display_name: "AD3 Test Harness"
tools:
  shape: list
  universe: []
  mappings: []
frontmatter:
  model_key: "harness_model"
  tools_key: "harness_tools"
  drop:
    - "generic_only_field"
`)

	harnessKeys := clf.HarnessKeys()
	hkSet := make(map[string]bool, len(harnessKeys))
	for _, k := range harnessKeys {
		hkSet[k] = true
	}

	dropCandidates := clf.DropCandidateKeys()
	dcSet := make(map[string]bool, len(dropCandidates))
	for _, k := range dropCandidates {
		dcSet[k] = true
	}

	// Guard: the real harness keys must be present so assertions are not vacuous.
	for _, required := range []string{"harness_model", "harness_tools"} {
		if !hkSet[required] {
			t.Errorf("HarnessKeys guard: missing expected key %q", required)
		}
		if !dcSet[required] {
			t.Errorf("DropCandidateKeys guard: missing expected key %q", required)
		}
	}

	// "generic_only_field" is known to the harness (it removes it) → in HarnessKeys.
	if !hkSet["generic_only_field"] {
		t.Errorf("HarnessKeys is missing Spec.Drop key %q; this key is known to the harness (FR-3) — "+
			"it must appear in HarnessKeys even though it is not a drop candidate",
			"generic_only_field")
	}
	// But it must NOT be in DropCandidateKeys — the harness does not ADD it (AD-3).
	if dcSet["generic_only_field"] {
		t.Errorf("DropCandidateKeys contains Spec.Drop-only key %q; AD-3 requires it to be absent "+
			"from DropCandidateKeys — only keys the harness ADDS are drop candidates",
			"generic_only_field")
	}
}

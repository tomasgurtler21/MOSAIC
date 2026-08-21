package descriptor_test

// Tests for loading a well-formed harness descriptor and verifying that all seven field
// groups defined in the schema are populated and accessible on the returned struct.
//
// Coverage:
//   - Load returns a non-nil descriptor for a valid file path.
//   - Identity fields (SchemaVersion, ID, DisplayName, TransformVersion, InjectionsVersion)
//     are populated from the YAML.
//   - The model catalog (IDs and FormatHint) is populated.
//   - The tool spec (Shape, Universe entries, Mappings) is populated.
//   - Deployment paths for agents and skills are populated with project paths.
//   - File extension rules per artifact kind are populated.
//   - Frontmatter shaping rules (Add, Drop, KeyOrder) are populated.
//   - Harness-level injection content (Name and Content) is populated.
//   - Hook support flag and variant key are populated.
//   - A minimal descriptor (only required fields present) loads without error.
//   - A non-existent file path returns an error.
//   - Parse with valid bytes produces an equivalent descriptor to Load.
//
//   value_by_role descriptor parsing (TDD RED - implementation pending):
//   - A descriptor with a valid value_by_role entry parses successfully; the entry
//     is loaded into FrontmatterSpec.RoleConditionalAdd, not into the static Add list.
//   - An entry with both value and value_by_role set is rejected with a validation error.
//   - An entry with neither value nor value_by_role set is rejected with a validation error.
//   - An entry with an unrecognized role key in value_by_role is rejected with a validation error.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// testdataDir is resolved relative to this package's test working directory.
const testdataDir = "../../../testdata/descriptors"

func loadFixture(t *testing.T, name string) *domain.HarnessDescriptor {
	t.Helper()
	path := filepath.Join(testdataDir, name)
	d, err := descriptor.Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", name, err)
	}
	if d == nil {
		t.Fatalf("Load(%q): returned nil descriptor without error", name)
	}
	return d
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// --- Non-existent file ---

func TestLoad_NonExistentFile_ReturnsError(t *testing.T) {
	_, err := descriptor.Load(filepath.Join(testdataDir, "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected error for a non-existent file, got nil")
	}
}

// --- Identity fields (field group 1) ---

func TestLoad_WellFormedDescriptor_SchemaVersionPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.SchemaVersion == "" {
		t.Error("SchemaVersion should be populated, got empty string")
	}
	if d.SchemaVersion != "1" {
		t.Errorf("SchemaVersion: want %q, got %q", "1", d.SchemaVersion)
	}
}

func TestLoad_WellFormedDescriptor_IDPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.ID == "" {
		t.Error("ID should be populated, got empty string")
	}
	if d.ID != "test-harness" {
		t.Errorf("ID: want %q, got %q", "test-harness", d.ID)
	}
}

func TestLoad_WellFormedDescriptor_DisplayNamePopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.DisplayName == "" {
		t.Error("DisplayName should be populated, got empty string")
	}
	if d.DisplayName != "Test Harness" {
		t.Errorf("DisplayName: want %q, got %q", "Test Harness", d.DisplayName)
	}
}

func TestLoad_WellFormedDescriptor_TransformVersionPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.TransformVersion == "" {
		t.Error("TransformVersion should be populated, got empty string")
	}
	if d.TransformVersion != "2.0.0" {
		t.Errorf("TransformVersion: want %q, got %q", "2.0.0", d.TransformVersion)
	}
}

func TestLoad_WellFormedDescriptor_InjectionsVersionPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.InjectionsVersion == "" {
		t.Error("InjectionsVersion should be populated, got empty string")
	}
	if d.InjectionsVersion != "1.1.0" {
		t.Errorf("InjectionsVersion: want %q, got %q", "1.1.0", d.InjectionsVersion)
	}
}

// --- Model catalog (field group 2) ---

func TestLoad_WellFormedDescriptor_ModelIDsPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	wantIDs := []string{"provider/model-fast", "provider/model-smart"}
	if len(d.Models.IDs) != len(wantIDs) {
		t.Fatalf("Models.IDs: want %d entries, got %d: %v", len(wantIDs), len(d.Models.IDs), d.Models.IDs)
	}
	for i, want := range wantIDs {
		if d.Models.IDs[i] != want {
			t.Errorf("Models.IDs[%d]: want %q, got %q", i, want, d.Models.IDs[i])
		}
	}
}

func TestLoad_WellFormedDescriptor_ModelFormatHintPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Models.FormatHint == "" {
		t.Error("Models.FormatHint should be populated, got empty string")
	}
	if d.Models.FormatHint != "provider/model-id" {
		t.Errorf("Models.FormatHint: want %q, got %q", "provider/model-id", d.Models.FormatHint)
	}
}

// --- Tool spec (field group 3) ---

func TestLoad_WellFormedDescriptor_ToolSpecShapePopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Tools.Shape == "" {
		t.Error("Tools.Shape should be populated, got empty string")
	}
	if d.Tools.Shape != domain.ShapeList {
		t.Errorf("Tools.Shape: want %q, got %q", domain.ShapeList, d.Tools.Shape)
	}
}

func TestLoad_WellFormedDescriptor_ToolUniversePopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	// The full fixture declares 5 harness tools.
	const wantCount = 5
	if len(d.Tools.Universe) != wantCount {
		t.Fatalf("Tools.Universe: want %d entries, got %d", wantCount, len(d.Tools.Universe))
	}
}

func TestLoad_WellFormedDescriptor_ToolUniverseEntriesHaveNames(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	for i, tool := range d.Tools.Universe {
		if tool.Name == "" {
			t.Errorf("Tools.Universe[%d].Name is empty", i)
		}
	}
}

func TestLoad_WellFormedDescriptor_ToolUniverseByConventionFlag(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	// "search/listDirectory" is declared with by_convention: true in the fixture.
	var found bool
	for _, tool := range d.Tools.Universe {
		if tool.Name == "search/listDirectory" {
			found = true
			if !tool.ByConvention {
				t.Error("search/listDirectory: expected ByConvention true, got false")
			}
		}
	}
	if !found {
		t.Error("search/listDirectory not found in Tools.Universe")
	}
}

func TestLoad_WellFormedDescriptor_ToolMappingsPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Tools.Mappings) == 0 {
		t.Fatal("Tools.Mappings should be non-empty")
	}
}

func TestLoad_WellFormedDescriptor_ToolMappingGenericNames(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	// The fixture declares mappings for file_read, file_write, content_search, file_search.
	genericNames := make(map[string]bool)
	for _, m := range d.Tools.Mappings {
		genericNames[m.Generic] = true
	}
	wantGenerics := []string{"file_read", "file_write", "content_search", "file_search"}
	for _, want := range wantGenerics {
		if !genericNames[want] {
			t.Errorf("expected mapping for generic tool %q, not found in %v", want, genericNamesSlice(d.Tools.Mappings))
		}
	}
}

func TestLoad_WellFormedDescriptor_OneToManyMappingPreservesAllNames(t *testing.T) {
	// file_write maps to two harness tool names via a DestMain destination in the fixture.
	// After loading, ToolMapping.Destinations must be populated with that destination,
	// and its Names slice must contain both names.
	// This test is RED until the loader populates ToolMapping.Destinations.
	d := loadFixture(t, "valid-full.yaml")

	for _, m := range d.Tools.Mappings {
		if m.Generic == "file_write" {
			if len(m.Destinations) != 1 {
				t.Fatalf("file_write mapping: want 1 Destination (one DestMain), got %d; "+
					"the loader must populate ToolMapping.Destinations from the destinations: YAML block",
					len(m.Destinations))
			}
			dest := m.Destinations[0]
			if dest.Kind != domain.DestMain {
				t.Errorf("file_write Destinations[0].Kind: want %q, got %q", domain.DestMain, dest.Kind)
			}
			const wantNames = 2
			if len(dest.Names) != wantNames {
				t.Fatalf("file_write Destinations[0].Names: want %d names, got %d: %v",
					wantNames, len(dest.Names), dest.Names)
			}
			return
		}
	}
	t.Fatal("mapping for file_write not found")
}

func TestLoad_WellFormedDescriptor_CustomToolTemplatePopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Tools.CustomToolTemplate != "mcp/%s" {
		t.Errorf("Tools.CustomToolTemplate: want %q, got %q", "mcp/%s", d.Tools.CustomToolTemplate)
	}
}

func TestLoad_WellFormedDescriptor_PlaceholderExpansionPopulated(t *testing.T) {
	// The full fixture declares placeholder_expansion: [read/readFile, write/createFile].
	// If the loader silently ignores this field the list will be empty and this test fails.
	d := loadFixture(t, "valid-full.yaml")

	wantExpansion := []string{"read/readFile", "write/createFile"}
	if len(d.Tools.PlaceholderExpansion) != len(wantExpansion) {
		t.Fatalf("Tools.PlaceholderExpansion: want %d entries, got %d: %v",
			len(wantExpansion), len(d.Tools.PlaceholderExpansion), d.Tools.PlaceholderExpansion)
	}
	for i, want := range wantExpansion {
		if d.Tools.PlaceholderExpansion[i] != want {
			t.Errorf("Tools.PlaceholderExpansion[%d]: want %q, got %q", i, want, d.Tools.PlaceholderExpansion[i])
		}
	}
}

// --- Permission tool shape ---

func TestLoad_PermissionShapeDescriptor_ShapeIsPermission(t *testing.T) {
	// A descriptor declaring shape: permission must load with Tools.Shape == ShapePermission.
	// An implementation that hard-codes ShapeList or misreads the field would fail this test.
	d := loadFixture(t, "valid-permission-shape.yaml")

	if d.Tools.Shape != domain.ShapePermission {
		t.Errorf("Tools.Shape: want %q, got %q", domain.ShapePermission, d.Tools.Shape)
	}
}

// --- Deployment paths (field group 4) ---

func TestLoad_WellFormedDescriptor_AgentPathsSupported(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if !d.Paths.Agents.Supported {
		t.Error("Paths.Agents.Supported should be true")
	}
}

func TestLoad_WellFormedDescriptor_AgentProjectPathPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Paths.Agents.Project == "" {
		t.Error("Paths.Agents.Project should be populated, got empty string")
	}
	if d.Paths.Agents.Project != ".test/agents" {
		t.Errorf("Paths.Agents.Project: want %q, got %q", ".test/agents", d.Paths.Agents.Project)
	}
}


func TestLoad_WellFormedDescriptor_SkillPathsSupported(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if !d.Paths.Skills.Supported {
		t.Error("Paths.Skills.Supported should be true")
	}
	if d.Paths.Skills.Project == "" {
		t.Error("Paths.Skills.Project should be populated")
	}
}

func TestLoad_WellFormedDescriptor_HookPathsNotSupported(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Paths.Hooks.Supported {
		t.Error("Paths.Hooks.Supported should be false for this fixture")
	}
}

// --- File extension rules (field group 5) ---

func TestLoad_WellFormedDescriptor_ExtensionsPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Extensions) == 0 {
		t.Fatal("Extensions should be non-empty")
	}
}

func TestLoad_WellFormedDescriptor_AgentExtensionPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	ext, ok := d.Extensions[domain.ArtifactAgent]
	if !ok {
		t.Fatal("Extensions[ArtifactAgent] should be present")
	}
	if ext != ".test.md" {
		t.Errorf("Extensions[ArtifactAgent]: want %q, got %q", ".test.md", ext)
	}
}

func TestLoad_WellFormedDescriptor_SkillExtensionPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	ext, ok := d.Extensions[domain.ArtifactSkill]
	if !ok {
		t.Fatal("Extensions[ArtifactSkill] should be present")
	}
	if ext != ".md" {
		t.Errorf("Extensions[ArtifactSkill]: want %q, got %q", ".md", ext)
	}
}

// --- Frontmatter shaping rules (field group 6) ---

func TestLoad_WellFormedDescriptor_FrontmatterModelKeyPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Frontmatter.ModelKey != "model" {
		t.Errorf("Frontmatter.ModelKey: want %q, got %q", "model", d.Frontmatter.ModelKey)
	}
}

func TestLoad_WellFormedDescriptor_FrontmatterToolsKeyPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if d.Frontmatter.ToolsKey != "tools" {
		t.Errorf("Frontmatter.ToolsKey: want %q, got %q", "tools", d.Frontmatter.ToolsKey)
	}
}

func TestLoad_WellFormedDescriptor_FrontmatterAddFieldsPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Frontmatter.Add) == 0 {
		t.Fatal("Frontmatter.Add should be non-empty")
	}
	if d.Frontmatter.Add[0].Key != "custom-field" {
		t.Errorf("Frontmatter.Add[0].Key: want %q, got %q", "custom-field", d.Frontmatter.Add[0].Key)
	}
}

func TestLoad_WellFormedDescriptor_FrontmatterAddFieldValue(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Frontmatter.Add) == 0 {
		t.Fatal("Frontmatter.Add should be non-empty")
	}
	v := d.Frontmatter.Add[0].Value
	if v.Kind != domain.KindScalar {
		t.Errorf("Frontmatter.Add[0].Value.Kind: want KindScalar, got %q", v.Kind)
	}
	if v.Scalar != "always-present" {
		t.Errorf("Frontmatter.Add[0].Value.Scalar: want %q, got %q", "always-present", v.Scalar)
	}
}

func TestLoad_WellFormedDescriptor_FrontmatterDropFieldsPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	wantDrop := []string{"recommended_tier", "tier_rationale"}
	if len(d.Frontmatter.Drop) != len(wantDrop) {
		t.Fatalf("Frontmatter.Drop: want %d entries, got %d: %v", len(wantDrop), len(d.Frontmatter.Drop), d.Frontmatter.Drop)
	}
	for i, want := range wantDrop {
		if d.Frontmatter.Drop[i] != want {
			t.Errorf("Frontmatter.Drop[%d]: want %q, got %q", i, want, d.Frontmatter.Drop[i])
		}
	}
}

func TestLoad_WellFormedDescriptor_FrontmatterKeyOrderPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Frontmatter.KeyOrder) == 0 {
		t.Fatal("Frontmatter.KeyOrder should be non-empty")
	}
	// "id" must appear first in the declared order.
	if d.Frontmatter.KeyOrder[0] != "id" {
		t.Errorf("Frontmatter.KeyOrder[0]: want %q, got %q", "id", d.Frontmatter.KeyOrder[0])
	}
}

// --- Injection content (field group 7) ---

func TestLoad_WellFormedDescriptor_InjectionsPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Injections) == 0 {
		t.Fatal("Injections should be non-empty")
	}
}

func TestLoad_WellFormedDescriptor_InjectionNamePopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Injections) == 0 {
		t.Fatal("Injections should be non-empty")
	}
	if d.Injections[0].Name != "HarnessConstraints" {
		t.Errorf("Injections[0].Name: want %q, got %q", "HarnessConstraints", d.Injections[0].Name)
	}
}

func TestLoad_WellFormedDescriptor_InjectionContentPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	if len(d.Injections) == 0 {
		t.Fatal("Injections should be non-empty")
	}
	if d.Injections[0].Content == "" {
		t.Error("Injections[0].Content should be non-empty")
	}
}

// --- Hook support ---

func TestLoad_WellFormedDescriptor_HookSupportPopulated(t *testing.T) {
	d := loadFixture(t, "valid-full.yaml")

	// The fixture declares hooks.supported: false.
	if d.Hooks.Supported {
		t.Error("Hooks.Supported should be false for the full fixture")
	}
}

// --- Minimal descriptor ---

func TestLoad_MinimalDescriptor_LoadsWithoutError(t *testing.T) {
	// A descriptor with only the required fields must load successfully.
	// All optional field groups default to zero values.
	d := loadFixture(t, "valid-minimal.yaml")

	if d.ID != "minimal-harness" {
		t.Errorf("ID: want %q, got %q", "minimal-harness", d.ID)
	}
}

func TestLoad_MinimalDescriptor_OptionalFieldsHaveZeroValues(t *testing.T) {
	d := loadFixture(t, "valid-minimal.yaml")

	if len(d.Models.IDs) != 0 {
		t.Errorf("Models.IDs should be empty for minimal descriptor, got %v", d.Models.IDs)
	}
	if len(d.Tools.Mappings) != 0 {
		t.Errorf("Tools.Mappings should be empty for minimal descriptor")
	}
	if len(d.Injections) != 0 {
		t.Errorf("Injections should be empty for minimal descriptor")
	}
}

// --- Parse vs Load equivalence ---

func TestParse_ValidBytes_SameIDAsLoad(t *testing.T) {
	// Parse must produce the same descriptor as Load for the same source bytes.
	path := filepath.Join(testdataDir, "valid-full.yaml")
	src := fixtureBytes(t, "valid-full.yaml")

	loaded, err := descriptor.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	parsed, err := descriptor.Parse(src, path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.ID != loaded.ID {
		t.Errorf("Parse and Load returned different IDs: %q vs %q", parsed.ID, loaded.ID)
	}
	if parsed.DisplayName != loaded.DisplayName {
		t.Errorf("Parse and Load returned different DisplayNames: %q vs %q", parsed.DisplayName, loaded.DisplayName)
	}
	if len(parsed.Models.IDs) != len(loaded.Models.IDs) {
		t.Errorf("Parse and Load returned different model ID counts: %d vs %d", len(parsed.Models.IDs), len(loaded.Models.IDs))
	}
}

// --- value_by_role parsing (TDD RED - implementation pending) ---

// TestLoad_ValueByRole_ParsesSuccessfully verifies that a descriptor containing a
// valid value_by_role entry loads without error. The entry declares user-invocable
// with distinct values per role (subagent: false, orchestrator/utility/standalone: true).
// RED: currently fails because wireFrontmatterField does not recognise value_by_role,
// causing yaml.DisallowUnknownField to reject the descriptor.
func TestLoad_ValueByRole_ParsesSuccessfully(t *testing.T) {
	_, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-valid.yaml"))
	if err != nil {
		t.Fatalf("Load(value-by-role-valid.yaml): unexpected error: %v", err)
	}
}

// TestLoad_ValueByRole_LoadedIntoRoleConditionalAdd verifies that a value_by_role entry
// is placed in FrontmatterSpec.RoleConditionalAdd rather than the static Add list.
// The static Add list must be empty; the role-conditional list must have one entry
// with key "user-invocable" and four role mappings.
// RED: currently fails because value_by_role is rejected at parse time as an unknown field.
func TestLoad_ValueByRole_LoadedIntoRoleConditionalAdd(t *testing.T) {
	d, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-valid.yaml"))
	if err != nil {
		t.Fatalf("Load(value-by-role-valid.yaml): %v", err)
	}

	if len(d.Frontmatter.Add) != 0 {
		t.Errorf("Frontmatter.Add: want empty (value_by_role entry must not appear in static Add), got %d entries: %v",
			len(d.Frontmatter.Add), d.Frontmatter.Add)
	}
	if len(d.Frontmatter.RoleConditionalAdd) == 0 {
		t.Fatal("Frontmatter.RoleConditionalAdd: want at least one entry, got empty")
	}
	rc := d.Frontmatter.RoleConditionalAdd[0]
	if rc.Key != "user-invocable" {
		t.Errorf("RoleConditionalAdd[0].Key: want %q, got %q", "user-invocable", rc.Key)
	}
	if len(rc.ValueByRole) != 4 {
		t.Errorf("RoleConditionalAdd[0].ValueByRole: want 4 entries (one per role), got %d", len(rc.ValueByRole))
	}
}

// TestLoad_ValueByRole_SubagentValueIsFalse verifies that the subagent role maps to
// the boolean false value in the parsed RoleConditionalField.
// RED: currently fails because value_by_role is rejected at parse time.
func TestLoad_ValueByRole_SubagentValueIsFalse(t *testing.T) {
	d, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-valid.yaml"))
	if err != nil {
		t.Fatalf("Load(value-by-role-valid.yaml): %v", err)
	}
	if len(d.Frontmatter.RoleConditionalAdd) == 0 {
		t.Fatal("Frontmatter.RoleConditionalAdd: want at least one entry, got empty")
	}
	rc := d.Frontmatter.RoleConditionalAdd[0]
	subagentVal, ok := rc.ValueByRole[domain.RoleSubagent]
	if !ok {
		t.Fatal("RoleConditionalAdd[0].ValueByRole: missing entry for RoleSubagent")
	}
	if subagentVal.Kind != domain.KindScalar {
		t.Errorf("subagent value kind: want KindScalar, got %q", subagentVal.Kind)
	}
	if subagentVal.Scalar != "false" {
		t.Errorf("subagent value scalar: want %q (false maps to string \"false\"), got %q", "false", subagentVal.Scalar)
	}
}

// TestLoad_ValueByRole_OrchestratorValueIsTrue verifies that the orchestrator role maps
// to the boolean true value in the parsed RoleConditionalField.
// RED: currently fails because value_by_role is rejected at parse time.
func TestLoad_ValueByRole_OrchestratorValueIsTrue(t *testing.T) {
	d, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-valid.yaml"))
	if err != nil {
		t.Fatalf("Load(value-by-role-valid.yaml): %v", err)
	}
	if len(d.Frontmatter.RoleConditionalAdd) == 0 {
		t.Fatal("Frontmatter.RoleConditionalAdd: want at least one entry, got empty")
	}
	rc := d.Frontmatter.RoleConditionalAdd[0]
	orchVal, ok := rc.ValueByRole[domain.RoleOrchestrator]
	if !ok {
		t.Fatal("RoleConditionalAdd[0].ValueByRole: missing entry for RoleOrchestrator")
	}
	if orchVal.Scalar != "true" {
		t.Errorf("orchestrator value scalar: want %q (true maps to string \"true\"), got %q", "true", orchVal.Scalar)
	}
}

// TestLoad_ValueByRole_BothValueAndValueByRole_ReturnsError verifies that an entry
// declaring both value and value_by_role is rejected with a validation error. The error
// must mention the violating field path (e.g. "frontmatter.add[0]").
// RED: currently fails — yaml.DisallowUnknownField rejects value_by_role as unknown
// rather than returning the expected mutual-exclusivity validation error.
func TestLoad_ValueByRole_BothValueAndValueByRole_ReturnsError(t *testing.T) {
	_, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-both-set.yaml"))
	if err == nil {
		t.Fatal("expected error for entry with both value and value_by_role set, got nil")
	}
	// The error must mention frontmatter to identify the violation site.
	errStr := err.Error()
	if !containsSubstring(errStr, "frontmatter") {
		t.Errorf("error message should mention %q to identify the violation site; got: %v", "frontmatter", err)
	}
}

// TestLoad_ValueByRole_NeitherValueNorValueByRole_ReturnsError verifies that an entry
// with only a key and no value declaration is rejected with a validation error.
// RED: currently the loader silently maps nil value to an empty scalar, producing no error.
func TestLoad_ValueByRole_NeitherValueNorValueByRole_ReturnsError(t *testing.T) {
	_, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-neither-set.yaml"))
	if err == nil {
		t.Fatal("expected error for entry with neither value nor value_by_role set, got nil")
	}
}

// TestLoad_ValueByRole_UnrecognizedRoleKey_ReturnsError verifies that an entry with an
// unrecognized role key (e.g. "not-a-valid-role") in value_by_role is rejected with a
// semantic validation error. The error must contain a phrase produced by the role-key
// validation path (e.g. "unrecognized role"), NOT just the role string literal — the
// role string can appear incidentally in yaml parse-error context windows. Asserting the
// validation phrase ensures this test only passes once the semantic validation logic is
// in place, not merely because yaml error output happens to mention the role name.
// RED: currently the yaml loader rejects value_by_role as an unknown field entirely,
// before role-key validation runs. The test will remain RED until both the YAML parsing
// of value_by_role AND the role-key validation are implemented.
func TestLoad_ValueByRole_UnrecognizedRoleKey_ReturnsError(t *testing.T) {
	_, err := descriptor.Load(filepath.Join(testdataDir, "value-by-role-unrecognized-role.yaml"))
	if err == nil {
		t.Fatal("expected error for entry with unrecognized role key, got nil")
	}
	errStr := err.Error()
	if !containsSubstring(errStr, "unrecognized role") {
		t.Errorf("error message should contain the validation phrase %q to confirm semantic validation ran; got: %v",
			"unrecognized role", err)
	}
}

// --- helpers ---

func genericNamesSlice(mappings []domain.ToolMapping) []string {
	names := make([]string, len(mappings))
	for i, m := range mappings {
		names[i] = m.Generic
	}
	return names
}

// containsSubstring reports whether s contains substr. Delegates to strings.Contains;
// exists as a named helper so test failure messages can name what was searched for.
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

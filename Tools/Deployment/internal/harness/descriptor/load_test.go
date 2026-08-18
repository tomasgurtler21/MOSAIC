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

import (
	"os"
	"path/filepath"
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

// --- helpers ---

func genericNamesSlice(mappings []domain.ToolMapping) []string {
	names := make([]string, len(mappings))
	for i, m := range mappings {
		names[i] = m.Generic
	}
	return names
}

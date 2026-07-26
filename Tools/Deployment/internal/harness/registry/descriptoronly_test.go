package registry_test

// Tests for the descriptor-only provision tier: a harness folder containing only harness.yaml
// resolves to a fully functional domain.HarnessModule with no code.
//
// The descriptor-only adapter is the zero-code runtime harness tier (AC6.2). These tests
// verify that it satisfies the domain.HarnessModule contract identically to a built-in,
// as required by AC6.1 (tier-agnostic from the consumer's perspective).
//
// Coverage:
//   T6.3  A folder with only harness.yaml resolves to a working HarnessModule:
//         - Tier and SourcePath provenance are correct.
//         - Descriptor fields from harness.yaml are accessible via Descriptor().
//         - Descriptor() returns the same pointer across calls (read-only contract).
//         - Close() returns nil (no resources to release).
//         - Tools() returns a result containing every requested generic tool, in order.
//         - Frontmatter() returns a plan that applies descriptor shaping rules.
//         - TargetPath() returns ErrArtifactUnsupported when the descriptor declares no
//           path support for the requested artifact kind.
//         - Injection() returns ok == false for names not declared in the descriptor.
//         - Injection() returns content declared in the descriptor.
//         - HookPlan() returns Supported == false with a reason when the descriptor has
//           hooks.supported: false.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Fixture builders for the descriptor-only tier tests
// ---------------------------------------------------------------------------

// makeDescriptorRoot builds a MosaicRoot with a single descriptor-only harness whose
// harness.yaml is written from the provided YAML content.
func makeDescriptorRoot(t *testing.T, id, yamlContent string) (root string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "MosaicDeploy", "harnesses", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create harness dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write harness.yaml: %v", err)
	}
	return root
}

// resolveDescriptorOnly is a test helper that calls Discover and Resolve for a
// descriptor-only harness, fatally failing if either step fails.
func resolveDescriptorOnly(t *testing.T, root, id string) domain.HarnessModule {
	t.Helper()
	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m == nil {
		t.Fatalf("Resolve(%q): returned nil module with nil error", id)
	}
	return m
}

// ---------------------------------------------------------------------------
// Identity and provenance
// ---------------------------------------------------------------------------

// TestDescriptorOnly_RefTierIsDescriptor verifies that a module resolved from an on-disk
// descriptor-only folder reports TierDescriptor — not TierBuiltin or TierExternal.
func TestDescriptorOnly_RefTierIsDescriptor(t *testing.T) {
	id := "descriptor-tier-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Tier Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Ref().Tier != domain.TierDescriptor {
		t.Errorf("Ref().Tier = %q, want %q", m.Ref().Tier, domain.TierDescriptor)
	}
}

// TestDescriptorOnly_RefIDMatchesDescriptorID verifies that the module's Ref().ID matches
// the id field declared in the harness.yaml.
func TestDescriptorOnly_RefIDMatchesDescriptorID(t *testing.T) {
	id := "descriptor-id-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"ID Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Ref().ID != id {
		t.Errorf("Ref().ID = %q, want %q", m.Ref().ID, id)
	}
}

// TestDescriptorOnly_RefSourcePathIsNonEmpty verifies that the module's Ref().SourcePath
// is set to a non-empty path. This is the on-disk location (harness.yaml or folder path)
// that the registry reports for logging and TUI display.
func TestDescriptorOnly_RefSourcePathIsNonEmpty(t *testing.T) {
	id := "descriptor-srcpath-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"SourcePath Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Ref().SourcePath == "" {
		t.Errorf("Ref().SourcePath is empty; expected path to the on-disk harness.yaml or folder")
	}
}

// TestDescriptorOnly_RefDisplayNameMatchesDescriptor verifies that the HarnessRef.DisplayName
// is taken from the descriptor's display_name field.
func TestDescriptorOnly_RefDisplayNameMatchesDescriptor(t *testing.T) {
	id := "descriptor-display-test"
	wantDisplayName := "My Display Name From YAML"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: %q\n", id, wantDisplayName)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Ref().DisplayName != wantDisplayName {
		t.Errorf("Ref().DisplayName = %q, want %q", m.Ref().DisplayName, wantDisplayName)
	}
}

// TestDescriptorOnly_RefUsableIsTrue verifies that a descriptor-only harness is marked as
// usable when its descriptor is valid.
func TestDescriptorOnly_RefUsableIsTrue(t *testing.T) {
	id := "descriptor-usable-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Usable Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if !m.Ref().Usable {
		t.Errorf("Ref().Usable = false, want true for a valid descriptor-only harness")
	}
}

// TestDescriptorOnly_RefRequiresOptInIsFalse verifies that descriptor-only harnesses do not
// require an explicit opt-in (that constraint applies only to the external tier).
func TestDescriptorOnly_RefRequiresOptInIsFalse(t *testing.T) {
	id := "descriptor-optin-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"OptIn Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Ref().RequiresOptIn {
		t.Errorf("Ref().RequiresOptIn = true for descriptor-only harness; want false")
	}
}

// TestDescriptorOnly_RefExecutablePathIsEmpty verifies that the ExecutablePath field is
// empty for descriptor-only modules (it is set only for the external tier).
func TestDescriptorOnly_RefExecutablePathIsEmpty(t *testing.T) {
	id := "descriptor-execpath-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"ExecPath Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Ref().ExecutablePath != "" {
		t.Errorf("Ref().ExecutablePath = %q for descriptor-only harness; want empty string", m.Ref().ExecutablePath)
	}
}

// ---------------------------------------------------------------------------
// Descriptor access
// ---------------------------------------------------------------------------

// TestDescriptorOnly_DescriptorIsNonNil verifies that Descriptor() never returns nil.
// The domain.HarnessModule contract requires Descriptor() to return a non-nil value.
func TestDescriptorOnly_DescriptorIsNonNil(t *testing.T) {
	id := "descriptor-nonnnil-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"NonNil Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if m.Descriptor() == nil {
		t.Errorf("Descriptor() returned nil; contract requires non-nil")
	}
}

// TestDescriptorOnly_DescriptorIDMatchesHarnessYAML verifies that the descriptor returned
// by Descriptor() carries the id field that was declared in harness.yaml.
func TestDescriptorOnly_DescriptorIDMatchesHarnessYAML(t *testing.T) {
	id := "descriptor-descid-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"DescID Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	desc := m.Descriptor()
	if desc.ID != id {
		t.Errorf("Descriptor().ID = %q, want %q", desc.ID, id)
	}
}

// TestDescriptorOnly_DescriptorIsStableAcrossCalls verifies that Descriptor() returns the
// same pointer on repeated calls (the returned value must be treated as read-only).
func TestDescriptorOnly_DescriptorIsStableAcrossCalls(t *testing.T) {
	id := "descriptor-stable-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Stable Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	first := m.Descriptor()
	second := m.Descriptor()
	if first != second {
		t.Errorf("Descriptor() returned different pointers across calls (%p vs %p); result must be stable", first, second)
	}
}

// TestDescriptorOnly_DescriptorModelIDsPopulated verifies that model IDs declared in
// harness.yaml are accessible via Descriptor().Models.IDs after loading.
func TestDescriptorOnly_DescriptorModelIDsPopulated(t *testing.T) {
	id := "descriptor-models-test"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "Models Test"
models:
  ids:
    - "provider/fast-model"
    - "provider/smart-model"
`, id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	desc := m.Descriptor()
	wantIDs := []string{"provider/fast-model", "provider/smart-model"}
	if len(desc.Models.IDs) != len(wantIDs) {
		t.Fatalf("Descriptor().Models.IDs: got %v, want %v", desc.Models.IDs, wantIDs)
	}
	for i, want := range wantIDs {
		if desc.Models.IDs[i] != want {
			t.Errorf("Models.IDs[%d] = %q, want %q", i, desc.Models.IDs[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

// TestDescriptorOnly_CloseReturnsNil verifies that Close() returns nil. The descriptor-only
// tier holds no resources (no child processes, no open files beyond startup).
func TestDescriptorOnly_CloseReturnsNil(t *testing.T) {
	id := "descriptor-close-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Close Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if err := m.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// TestDescriptorOnly_CloseIsIdempotent verifies that calling Close() twice returns nil both
// times. The contract requires idempotent Close().
func TestDescriptorOnly_CloseIsIdempotent(t *testing.T) {
	id := "descriptor-closeidempotent-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Idempotent Close Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)

	m := resolveDescriptorOnly(t, root, id)
	if err := m.Close(); err != nil {
		t.Fatalf("first Close() = %v, want nil", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil (Close must be idempotent)", err)
	}
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

// TestDescriptorOnly_Tools_AllGenericToolsHaveResolution verifies that Tools() returns
// exactly one ToolResolution per entry in ToolRequest.Generic, in the same order.
// This is the core invariant from domain.HarnessModule (AC9.3, AC9.4).
func TestDescriptorOnly_Tools_AllGenericToolsHaveResolution(t *testing.T) {
	id := "descriptor-tools-test"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "Tools Test"
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
    - name: "write/createFile"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read/readFile"
    - generic: "file_write"
      harness_tools:
        - "write/createFile"
`, id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	genericTools := []string{"file_read", "file_write"}
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  genericTools,
	}
	result, err := m.Tools(req)
	if err != nil {
		t.Fatalf("Tools(): %v", err)
	}
	if len(result.Resolutions) != len(genericTools) {
		t.Errorf("Tools(): got %d resolutions, want %d (one per generic tool)",
			len(result.Resolutions), len(genericTools))
	}
	for i, want := range genericTools {
		if i < len(result.Resolutions) && result.Resolutions[i].Generic != want {
			t.Errorf("Resolutions[%d].Generic = %q, want %q", i, result.Resolutions[i].Generic, want)
		}
	}
}

// TestDescriptorOnly_Tools_EmptyRequestReturnsEmptyResolutions verifies that Tools() with
// no generic tools requested returns an empty or nil Resolutions slice (no phantom entries).
func TestDescriptorOnly_Tools_EmptyRequestReturnsEmptyResolutions(t *testing.T) {
	id := "descriptor-toolsempty-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Empty Tools Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	result, err := m.Tools(domain.ToolRequest{AgentKey: "test-agent", Generic: nil})
	if err != nil {
		t.Fatalf("Tools() with empty request: %v", err)
	}
	if len(result.Resolutions) != 0 {
		t.Errorf("Tools() with empty Generic: got %d resolutions, want 0", len(result.Resolutions))
	}
}

// ---------------------------------------------------------------------------
// Frontmatter
// ---------------------------------------------------------------------------

// TestDescriptorOnly_Frontmatter_ReflectsDescriptorSpec verifies that Frontmatter() returns
// a FrontmatterPlan that reflects the descriptor's frontmatter shaping rules:
//   - fields in the descriptor's Add list appear in FrontmatterPlan.Set
//   - keys in the descriptor's Drop list appear in FrontmatterPlan.Remove
//
// The descriptor-only adapter must delegate to descriptor.ApplyFrontmatterSpec (CD-2). This
// test ensures that wiring is in place; without it, I6.3 could fail to connect
// ApplyFrontmatterSpec and no Stage 6 test would catch the omission (gap against AC6.1).
func TestDescriptorOnly_Frontmatter_ReflectsDescriptorSpec(t *testing.T) {
	id := "descriptor-frontmatter-test"
	wantAddKey := "harness_marker"
	wantAddValue := id
	wantDropKey := "generic_only_field"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "Frontmatter Test"
frontmatter:
  add:
    - key: %q
      value: %q
  drop:
    - %q
`, id, wantAddKey, wantAddValue, wantDropKey)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
		Source: []domain.FrontmatterField{
			{
				Key:   wantDropKey,
				Value: domain.FieldValue{Kind: domain.KindScalar, Scalar: "old-value"},
			},
		},
	}
	plan, err := m.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(): %v", err)
	}

	// The field declared in the descriptor's Add list must appear in FrontmatterPlan.Set.
	foundInSet := false
	for _, f := range plan.Set {
		if f.Key == wantAddKey {
			foundInSet = true
			break
		}
	}
	if !foundInSet {
		setKeys := make([]string, len(plan.Set))
		for i, f := range plan.Set {
			setKeys[i] = f.Key
		}
		t.Errorf("Frontmatter(): FrontmatterPlan.Set does not contain %q from descriptor frontmatter.add; got Set keys: %v",
			wantAddKey, setKeys)
	}

	// The key declared in the descriptor's Drop list must appear in FrontmatterPlan.Remove.
	foundInRemove := false
	for _, key := range plan.Remove {
		if key == wantDropKey {
			foundInRemove = true
			break
		}
	}
	if !foundInRemove {
		t.Errorf("Frontmatter(): FrontmatterPlan.Remove does not contain %q from descriptor frontmatter.drop; got Remove: %v",
			wantDropKey, plan.Remove)
	}
}

// TestDescriptorOnly_Frontmatter_EmptySpecReturnsEmptyPlan verifies that Frontmatter()
// returns an empty FrontmatterPlan when the descriptor declares no frontmatter shaping rules.
// This is the base-case: a minimal descriptor with no frontmatter block must produce no
// Set entries and no Remove entries.
func TestDescriptorOnly_Frontmatter_EmptySpecReturnsEmptyPlan(t *testing.T) {
	id := "descriptor-frontmatter-empty-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Frontmatter Empty Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	plan, err := m.Frontmatter(domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
	})
	if err != nil {
		t.Fatalf("Frontmatter() on empty spec: %v", err)
	}
	if len(plan.Set) != 0 {
		t.Errorf("Frontmatter() on empty spec: Set has %d entries, want 0; got: %v", len(plan.Set), plan.Set)
	}
	if len(plan.Remove) != 0 {
		t.Errorf("Frontmatter() on empty spec: Remove has %d entries, want 0; got: %v", len(plan.Remove), plan.Remove)
	}
}

// ---------------------------------------------------------------------------
// Injection
// ---------------------------------------------------------------------------

// TestDescriptorOnly_Injection_ReturnsFalseForUnknownNames verifies that Injection() returns
// ok == false for names not declared in the descriptor's injections list.
func TestDescriptorOnly_Injection_ReturnsFalseForUnknownNames(t *testing.T) {
	id := "descriptor-injection-test"
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"Injection Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	_, ok := m.Injection("HarnessConstraints")
	if ok {
		t.Errorf("Injection(\"HarnessConstraints\") returned ok = true for an empty descriptor; want false")
	}
}

// TestDescriptorOnly_Injection_ReturnsContentForDeclaredName verifies that Injection()
// returns the declared content and ok == true for names listed in the descriptor.
func TestDescriptorOnly_Injection_ReturnsContentForDeclaredName(t *testing.T) {
	id := "descriptor-injectioncontent-test"
	wantContent := "This is the harness-level content.\n"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "Injection Content Test"
injections:
  - name: "HarnessConstraints"
    content: %q
`, id, wantContent)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	content, ok := m.Injection("HarnessConstraints")
	if !ok {
		t.Errorf("Injection(\"HarnessConstraints\") returned ok = false; want true")
	}
	if content != wantContent {
		t.Errorf("Injection(\"HarnessConstraints\") content = %q, want %q", content, wantContent)
	}
}

// ---------------------------------------------------------------------------
// TargetPath
// ---------------------------------------------------------------------------

// TestDescriptorOnly_TargetPath_UnsupportedKindReturnsErrArtifactUnsupported verifies that
// TargetPath returns domain.ErrArtifactUnsupported when the descriptor declares no path
// support for the requested artifact kind.
func TestDescriptorOnly_TargetPath_UnsupportedKindReturnsErrArtifactUnsupported(t *testing.T) {
	id := "descriptor-targetpath-test"
	// Minimal descriptor with no paths block — all artifact kinds unsupported.
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"TargetPath Test\"\n", id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	_, err := m.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-agent",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	})
	if !errors.Is(err, domain.ErrArtifactUnsupported) {
		t.Errorf("TargetPath for unsupported kind: got %v, want ErrArtifactUnsupported", err)
	}
}

// TestDescriptorOnly_TargetPath_SupportedKindReturnsPath verifies that TargetPath returns
// a non-empty path for a supported artifact kind with a project scope.
func TestDescriptorOnly_TargetPath_SupportedKindReturnsPath(t *testing.T) {
	id := "descriptor-targetpathsupported-test"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "TargetPath Supported Test"
paths:
  agents:
    supported: true
    project: ".myharness/agents"
`, id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	path, err := m.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactAgent,
		Key:      "test-agent",
		FileName: "test-agent.md",
		Scope:    domain.ScopeProject,
		GOOS:     "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath for supported agent kind: %v", err)
	}
	if path == "" {
		t.Errorf("TargetPath returned empty string for a supported artifact kind")
	}
}

// ---------------------------------------------------------------------------
// HookPlan
// ---------------------------------------------------------------------------

// TestDescriptorOnly_HookPlan_UnsupportedDescriptorReturnsReason verifies that HookPlan
// returns Supported == false with a non-empty Reason when the descriptor declares
// hooks.supported: false. The HarnessModule contract requires a reason when unsupported.
func TestDescriptorOnly_HookPlan_UnsupportedDescriptorReturnsReason(t *testing.T) {
	id := "descriptor-hookplan-test"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "HookPlan Test"
hooks:
  supported: false
`, id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	plan, err := m.HookPlan(domain.HookPlanRequest{
		Bundle: domain.HookBundle{Key: "some-hook", Version: "1.0.0"},
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("HookPlan: unexpected error: %v", err)
	}
	if plan.Supported {
		t.Errorf("HookPlan.Supported = true for a descriptor declaring hooks.supported: false")
	}
	if plan.Reason == "" {
		t.Errorf("HookPlan.Reason is empty; a non-empty reason is required when Supported is false")
	}
	if len(plan.Files) != 0 {
		t.Errorf("HookPlan.Files is non-empty (%v) when Supported is false; expect empty", plan.Files)
	}
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

// TestDescriptorOnly_Tools_DeterministicAcrossCalls verifies that calling Tools() twice
// with identical input returns identical results. Descriptor-only modules must be
// deterministic and free of hidden state.
func TestDescriptorOnly_Tools_DeterministicAcrossCalls(t *testing.T) {
	id := "descriptor-determinism-test"
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: "Determinism Test"
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read/readFile"
`, id)
	root := makeDescriptorRoot(t, id, yaml)
	m := resolveDescriptorOnly(t, root, id)

	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}
	first, err := m.Tools(req)
	if err != nil {
		t.Fatalf("first Tools() call: %v", err)
	}
	second, err := m.Tools(req)
	if err != nil {
		t.Fatalf("second Tools() call: %v", err)
	}

	if len(first.Resolutions) != len(second.Resolutions) {
		t.Errorf("Tools() returned different resolution counts across calls: %d vs %d",
			len(first.Resolutions), len(second.Resolutions))
		return
	}
	for i := range first.Resolutions {
		if first.Resolutions[i].Generic != second.Resolutions[i].Generic {
			t.Errorf("Resolutions[%d].Generic differs across calls: %q vs %q",
				i, first.Resolutions[i].Generic, second.Resolutions[i].Generic)
		}
		if first.Resolutions[i].Outcome != second.Resolutions[i].Outcome {
			t.Errorf("Resolutions[%d].Outcome differs across calls: %q vs %q",
				i, first.Resolutions[i].Outcome, second.Resolutions[i].Outcome)
		}
	}
}

package descriptor_test

// multidest_permission_test.go verifies multi-destination mapping in the permission
// (ShapePermission) tool shape.
//
// In the permission shape the main tools field is a key-value mapping of harness tool names
// to their allowed/denied dispositions. The multi-destination rules for this shape are:
//   - Names contributed by DestMain destinations are marked "allow" in the permission mapping.
//   - Names contributed by DestField destinations are placed in separate FrontmatterFields
//     (same first-seen order as in the list shape), and must NOT appear in the permission map.
//   - ByConvention tools receive "allow" regardless of whether any generic tool resolves to them.
//   - The separate-field bucket rules (field name, format, merge across tools) are identical
//     to the list shape.
//
// All tests marked RED will fail until MapTools is updated to read ToolMapping.Destinations
// for the permission shape.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// makePermissionMultiDestDescriptor builds a permission-shape descriptor with:
//   - file_read  → DestMain: ["read"]
//   - user_interaction → DestMain: ["AskUserQuestion"] + DestField "mcpServers" (list-block): ["user-feedback"]
//   - write (universe tool, unused: deny, not mapped)
//
// Universe: read (deny), AskUserQuestion (deny), write (deny)
func makePermissionMultiDestDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "perm-multidest-harness",
		DisplayName:   "Permission Multi-Dest Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapePermission,
			Universe: []domain.HarnessTool{
				{Name: "read", Unused: domain.Deny, ByConvention: false},
				{Name: "AskUserQuestion", Unused: domain.Deny, ByConvention: false},
				{Name: "write", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read"}},
					},
				},
				{
					Generic: "user_interaction",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
						{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "permissions"},
	}
}

// permPairs extracts Pairs from the permission map field ("permissions" key).
// It fails the test if the field or its KindMapping value is absent.
func permPairs(t *testing.T, result domain.ToolResult) []domain.FieldPair {
	t.Helper()
	f := findField(t, result.Fields, "permissions")
	if f == nil {
		t.Fatal("permissions field absent from Fields; permission-shape descriptor must emit a KindMapping field")
		return nil
	}
	if f.Value.Kind != domain.KindMapping {
		t.Fatalf("permissions field value kind: want %q, got %q", domain.KindMapping, f.Value.Kind)
	}
	return f.Value.Pairs
}

// --- DestMain destinations contribute "allow" to the permission map ---

func TestMultiDestPermission_MainDest_NameReceivesAllowDisposition(t *testing.T) {
	// When a DestMain destination names a Universe tool, that tool must receive "allow"
	// in the permission mapping. "AskUserQuestion" is a DestMain name in user_interaction.
	// This test is RED until MapTools reads Destinations for the permission shape.
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)

	for _, p := range pairs {
		if p.Key == "AskUserQuestion" {
			if p.Value.Kind != domain.KindScalar {
				t.Errorf("AskUserQuestion pair value kind: want KindScalar, got %q", p.Value.Kind)
			}
			if p.Value.Scalar != string(domain.Allow) {
				t.Errorf("AskUserQuestion disposition: want %q (resolved via DestMain), got %q",
					domain.Allow, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("AskUserQuestion not found in permission pairs; got pairs: %v", pairs)
}

func TestMultiDestPermission_MainDest_MappedUniverseToolAllowed(t *testing.T) {
	// "read" is resolved through file_read's DestMain destination. It must be "allow".
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	for _, p := range pairs {
		if p.Key == "read" {
			if p.Value.Scalar != string(domain.Allow) {
				t.Errorf("read disposition: want %q (DestMain resolution), got %q", domain.Allow, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("read not found in permission pairs; got pairs: %v", pairs)
}

func TestMultiDestPermission_UnresolvedUniverseTool_RetainsUnusedDisposition(t *testing.T) {
	// "write" is in the Universe but no generic tool resolves to it via a DestMain destination.
	// Its disposition must be its declared Unused value ("deny").
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	for _, p := range pairs {
		if p.Key == "write" {
			if p.Value.Scalar != string(domain.Deny) {
				t.Errorf("write (unresolved): want disposition %q (Unused from descriptor), got %q",
					domain.Deny, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("write not found in permission pairs; got pairs: %v", pairs)
}

// --- DestField names must NOT enter the permission map ---

func TestMultiDestPermission_FieldDest_NamesAbsentFromPermissionMap(t *testing.T) {
	// "user-feedback" is contributed by a DestField destination. It must NOT appear
	// as a key in the permission mapping (only Universe tools form the permission map).
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	for _, p := range pairs {
		if p.Key == "user-feedback" {
			t.Errorf("DestField name %q must not appear as a key in the permission map; "+
				"only Universe tools form the permission mapping", "user-feedback")
		}
	}
}

func TestMultiDestPermission_PermissionMapContainsOnlyUniverseTools(t *testing.T) {
	// The permission map must have exactly one entry per Universe tool, no more and no fewer.
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)

	// Universe has 3 tools: read, AskUserQuestion, write.
	const wantCount = 3
	if len(pairs) != wantCount {
		t.Errorf("permission map pairs count: want %d (one per Universe tool), got %d: %v",
			wantCount, len(pairs), pairs)
	}
}

// --- DestField destinations produce a separate FrontmatterField in the permission shape ---

func TestMultiDestPermission_FieldDest_SeparateFieldExists(t *testing.T) {
	// A DestField destination must produce a separate FrontmatterField even in the
	// permission shape. The field is additional to the permission mapping, not part of it.
	// This test is RED until MapTools emits separate fields for DestField destinations in
	// the permission shape.
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if findField(t, result.Fields, "mcpServers") == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("mcpServers separate field absent from Fields in permission-shape output; "+
			"DestField destinations must produce a dedicated FrontmatterField in both list and permission shapes; "+
			"got keys: %v", keys)
	}
}

func TestMultiDestPermission_FieldDest_SeparateFieldContainsNames(t *testing.T) {
	// The separate field produced for a DestField destination in the permission shape
	// must contain the names declared in that destination.
	d := makePermissionMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		t.Fatal("mcpServers field absent from Fields")
	}
	if !fieldContainsName(mcpField, "user-feedback") {
		t.Errorf("mcpServers field does not contain %q; got: %v",
			"user-feedback", fieldItemNames(mcpField))
	}
}

// --- ByConvention tools are allow in permission shape even without DestMain resolution ---

func TestMultiDestPermission_ByConventionTool_ReceivesAllowEvenWithNoMatchingGenericTool(t *testing.T) {
	// A Universe tool marked ByConvention: true must receive "allow" in the permission map
	// regardless of whether any generic tool maps to it. This mirrors the list-shape rule
	// where by-convention tools always appear.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "perm-convention-harness",
		DisplayName:   "Permission ByConvention Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapePermission,
			Universe: []domain.HarnessTool{
				{Name: "alwaysAllowed", Unused: domain.Deny, ByConvention: true},
				{Name: "normalTool", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "some_tool",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"normalTool"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "permissions"},
	}
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"some_tool"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	for _, p := range pairs {
		if p.Key == "alwaysAllowed" {
			if p.Value.Scalar != string(domain.Allow) {
				t.Errorf("by-convention tool disposition: want %q (ByConvention: true), got %q",
					domain.Allow, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("by-convention tool %q not found in permission pairs; got: %v", "alwaysAllowed", pairs)
}

// =============================================================================
// Custom tool default destinations: permission-shape coverage
//
// When a descriptor declares a non-empty CustomToolDestinations list on a permission-shape
// harness, the same routing rules apply as in the list shape: DestMain names enter the
// resolved set (and receive "allow" in the permission map if they are Universe tools),
// while DestField names are routed to separate FrontmatterFields and must NOT appear in
// the permission map.
//
// Regression tests verify that permission-shape output is unchanged for descriptors that
// omit CustomToolDestinations, covering the ToolCustom fall-through and the ToolMapped path.
// =============================================================================

// makePermCustomDestDescriptor builds a permission-shape descriptor with:
//   - file_read → DestMain ["read"]
//   - write (universe tool, unused: deny, not mapped)
//   - CustomToolDestinations: [{DestField, "mcpServers", FormatListBlock}]
func makePermCustomDestDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "perm-custdest-harness",
		DisplayName:   "Permission CustomDest Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapePermission,
			Universe: []domain.HarnessTool{
				{Name: "read", Unused: domain.Deny, ByConvention: false},
				{Name: "write", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read"}},
					},
				},
			},
			CustomToolDestinations: []domain.ToolDestination{
				{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "permissions"},
	}
}

// TestMultiDestPermission_CustomDest_FieldDest_CustomNameAbsentFromPermissionMap asserts
// that a custom name routed through a DestField CustomToolDestination does not appear as
// a key in the permission map. The permission map contains only Universe tools.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestPermission_CustomDest_FieldDest_CustomNameAbsentFromPermissionMap(t *testing.T) {
	d := makePermCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	for _, p := range pairs {
		if p.Key == "my-server" {
			t.Errorf("custom name %q must not appear in the permission map; "+
				"DestField custom destinations must produce a separate FrontmatterField only; "+
				"the permission map contains only Universe tools",
				"my-server")
		}
	}
}

// TestMultiDestPermission_CustomDest_FieldDest_SeparateFieldEmitted asserts that the
// separate FrontmatterField is produced in permission-shape output when CustomToolDestinations
// declares a DestField destination.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestPermission_CustomDest_FieldDest_SeparateFieldEmitted(t *testing.T) {
	d := makePermCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if findField(t, result.Fields, "mcpServers") == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("mcpServers separate field absent from Fields in permission-shape output; "+
			"DestField CustomToolDestinations must produce a dedicated FrontmatterField in "+
			"both list and permission shapes; got keys: %v", keys)
	}
}

// TestMultiDestPermission_CustomDest_FieldDest_FieldContainsCustomName asserts that the
// separate field emitted for the DestField custom destination contains the custom name.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestPermission_CustomDest_FieldDest_FieldContainsCustomName(t *testing.T) {
	d := makePermCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		t.Fatal("mcpServers field absent from Fields")
	}
	if !fieldContainsName(mcpField, "my-server") {
		t.Errorf("mcpServers field does not contain %q; got: %v",
			"my-server", fieldItemNames(mcpField))
	}
}

// TestMultiDestPermission_CustomDest_PermissionMapContainsOnlyUniverseTools asserts
// that the permission map still contains exactly one entry per Universe tool when a
// ToolCustom resolution routes its name through a DestField custom destination.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestPermission_CustomDest_PermissionMapContainsOnlyUniverseTools(t *testing.T) {
	d := makePermCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	// Universe has 2 tools: read, write.
	if len(pairs) != 2 {
		t.Errorf("permission map pairs count: want 2 (Universe tools only), got %d: %v", len(pairs), pairs)
	}
}

// TestMultiDestPermission_CustomDest_MappedToolUnchanged asserts that the standard
// ToolMapped path (file_read → DestMain → "read" allowed in permission map) is not
// affected by CustomToolDestinations being declared on the same descriptor.
func TestMultiDestPermission_CustomDest_MappedToolUnchanged(t *testing.T) {
	d := makePermCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	for _, p := range pairs {
		if p.Key == "read" {
			if p.Value.Scalar != string(domain.Allow) {
				t.Errorf("read disposition: want %q (resolved via file_read ToolMapped, unaffected by CustomToolDestinations), got %q",
					domain.Allow, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("read not found in permission pairs; ToolMapped path must be unaffected by CustomToolDestinations; got: %v", pairs)
}

// --- Regression: permission-shape output unchanged for descriptors without CustomToolDestinations ---

// makePermNoCustomDestDescriptor builds a permission-shape descriptor with no
// CustomToolDestinations. Used for regression coverage.
func makePermNoCustomDestDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "perm-no-custdest-harness",
		DisplayName:   "Permission No CustomDest Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapePermission,
			Universe: []domain.HarnessTool{
				{Name: "read", Unused: domain.Deny, ByConvention: false},
				{Name: "write", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read"}},
					},
				},
			},
			// CustomToolDestinations is nil — not declared.
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "permissions"},
	}
}

// TestMultiDestPermission_Regression_NoCustomDest_CustomToolUsesDestMain asserts that
// a permission-shape descriptor without CustomToolDestinations produces the pre-change
// behaviour for a ToolCustom resolution: a single DestMain destination carrying the
// formatted custom name. This is the T2.6 regression for the permission shape.
func TestMultiDestPermission_Regression_NoCustomDest_CustomToolUsesDestMain(t *testing.T) {
	d := makePermNoCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolCustom {
		t.Fatalf("outcome: want %q, got %q", domain.ToolCustom, res.Outcome)
	}
	// Fall-through: single DestMain carrying the custom name.
	if len(res.Destinations) != 1 {
		t.Fatalf("Destinations count: want 1 (single DestMain fall-through without CustomToolDestinations), got %d", len(res.Destinations))
	}
	if res.Destinations[0].Kind != domain.DestMain {
		t.Errorf("Destinations[0].Kind: want %q (fall-through without CustomToolDestinations), got %q",
			domain.DestMain, res.Destinations[0].Kind)
	}
	if len(res.Destinations[0].Names) != 1 || res.Destinations[0].Names[0] != "my-server" {
		t.Errorf("Destinations[0].Names: want [%q], got %v", "my-server", res.Destinations[0].Names)
	}
}

// TestMultiDestPermission_Regression_NoCustomDest_PermissionMapContainsOnlyUniverse asserts
// that with no CustomToolDestinations, the permission map is Universe-only (the custom name
// does not appear as a pair key since it is not a Universe tool).
func TestMultiDestPermission_Regression_NoCustomDest_PermissionMapContainsOnlyUniverse(t *testing.T) {
	d := makePermNoCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"file_read", "terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permPairs(t, result)
	// Universe has 2 tools; the map must have exactly 2 pairs.
	if len(pairs) != 2 {
		t.Errorf("permission map pairs count: want 2 (Universe tools only), got %d: %v", len(pairs), pairs)
	}
	for _, p := range pairs {
		if p.Key == "my-server" {
			t.Errorf("custom name %q must not appear in the permission map (unchanged regression)", "my-server")
		}
	}
}

// TestMultiDestPermission_Regression_NoCustomDest_SkippedToolEmptyDestinations asserts
// that a skipped tool produces empty Destinations regardless of descriptor state.
func TestMultiDestPermission_Regression_NoCustomDest_SkippedToolEmptyDestinations(t *testing.T) {
	d := makePermNoCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:     "test-agent",
		Generic:      []string{"file_read"},
		SkippedTools: map[string]bool{"file_read": true},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolSkipped {
		t.Fatalf("outcome: want %q, got %q", domain.ToolSkipped, res.Outcome)
	}
	if len(res.Destinations) != 0 {
		t.Errorf("ToolSkipped Destinations: want empty (unchanged regression), got %d", len(res.Destinations))
	}
}

// TestMultiDestPermission_Regression_NoCustomDest_UnmappedToolEmptyDestinations asserts
// that an unmapped tool (no custom name) produces empty Destinations.
func TestMultiDestPermission_Regression_NoCustomDest_UnmappedToolEmptyDestinations(t *testing.T) {
	d := makePermNoCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"terminal"},
		// No CustomNames → ToolUnmapped, not ToolCustom.
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolUnmapped {
		t.Fatalf("outcome: want %q, got %q", domain.ToolUnmapped, res.Outcome)
	}
	if len(res.Destinations) != 0 {
		t.Errorf("ToolUnmapped Destinations: want empty (unchanged regression), got %d", len(res.Destinations))
	}
}

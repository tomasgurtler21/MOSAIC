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

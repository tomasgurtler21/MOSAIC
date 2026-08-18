package descriptor_test

// multidest_domain_test.go verifies that the new multi-destination domain types
// are correctly integrated into MapTools. Tests fall into two groups:
//
// Wire-format constant values — the ToolDestinationKind and ToolValueFormat string
// constants are used verbatim in YAML and JSON wire formats; their string values are
// behavioral contracts, not compiler details.
//
// MapTools integration — when a ToolMapping declares Destinations, MapTools must populate
// the returned ToolResolution.Destinations and must compute HarnessTools as the flattened,
// deduplicated, first-seen-order union of all destination Names. Both properties are
// RED until MapTools is updated to consume Destinations instead of the deprecated
// HarnessTools/Field fields.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- Wire-format constant values ---

func TestToolDestinationKind_MainStringValue(t *testing.T) {
	// "main" is the wire token for a destination that appends to the harness's own tools
	// field. The YAML and JSON parsers compare string values, not Go constants, so a change
	// here is an interoperability break.
	if domain.DestMain != "main" {
		t.Errorf("DestMain wire value: want %q, got %q", "main", domain.DestMain)
	}
}

func TestToolDestinationKind_FieldStringValue(t *testing.T) {
	// "field" is the wire token for a destination that writes to a separate frontmatter key.
	if domain.DestField != "field" {
		t.Errorf("DestField wire value: want %q, got %q", "field", domain.DestField)
	}
}

func TestToolDestinationKind_MainAndFieldAreDistinct(t *testing.T) {
	if domain.DestMain == domain.DestField {
		t.Error("DestMain and DestField must be distinct constants")
	}
}

func TestToolValueFormat_ListBlockStringValue(t *testing.T) {
	if domain.FormatListBlock != "list-block" {
		t.Errorf("FormatListBlock wire value: want %q, got %q", "list-block", domain.FormatListBlock)
	}
}

func TestToolValueFormat_ListFlowStringValue(t *testing.T) {
	if domain.FormatListFlow != "list-flow" {
		t.Errorf("FormatListFlow wire value: want %q, got %q", "list-flow", domain.FormatListFlow)
	}
}

func TestToolValueFormat_ScalarStringValue(t *testing.T) {
	if domain.FormatScalar != "scalar" {
		t.Errorf("FormatScalar wire value: want %q, got %q", "scalar", domain.FormatScalar)
	}
}

func TestDefaultToolValueFormat_IsListBlock(t *testing.T) {
	// The default format for an omitted Format field in a DestField destination is list-block.
	// Verifying this documents the fallback so callers do not need to guess.
	if domain.DefaultToolValueFormat != domain.FormatListBlock {
		t.Errorf("DefaultToolValueFormat: want %q, got %q", domain.FormatListBlock, domain.DefaultToolValueFormat)
	}
}

func TestDefaultScalarSeparator_IsCommaSpace(t *testing.T) {
	// The separator used when a FormatScalar destination omits Separator.
	if domain.DefaultScalarSeparator != ", " {
		t.Errorf("DefaultScalarSeparator: want %q, got %q", ", ", domain.DefaultScalarSeparator)
	}
}

// --- MapTools integration: Destinations on ToolResolution ---

// makeDestinationOnlyDescriptor builds a list-shape descriptor whose mapping for
// "user_interaction" declares two Destinations (one main, one field) without using
// the deprecated HarnessTools or Field fields. MapTools must read Destinations to
// produce the correct ToolResolution.
func makeDestinationOnlyDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "dest-only-harness",
		DisplayName:   "Destination-Only Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "AskUserQuestion", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "user_interaction",
					// Destinations is the new model; HarnessTools and Field are deliberately absent.
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
						{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
}

func TestMapTools_DestinationsModel_ResolutionHasDestinationsPopulated(t *testing.T) {
	// When a ToolMapping declares only Destinations (no deprecated HarnessTools/Field),
	// MapTools must populate ToolResolution.Destinations from the mapping's Destinations.
	// This test is RED until MapTools reads Destinations instead of HarnessTools/Field.
	d := makeDestinationOnlyDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if len(res.Destinations) == 0 {
		t.Fatal("ToolResolution.Destinations must be non-empty when the mapping declares destinations; " +
			"MapTools must copy the mapping's Destinations into the resolution")
	}
}

func TestMapTools_DestinationsModel_ResolutionDestinationCount(t *testing.T) {
	// The resolution must carry exactly as many Destinations as the mapping declared.
	// The fixture mapping declares two destinations (one main, one field).
	d := makeDestinationOnlyDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	const wantDests = 2
	if len(res.Destinations) != wantDests {
		t.Errorf("ToolResolution.Destinations count: want %d (one per declared destination), got %d",
			wantDests, len(res.Destinations))
	}
}

func TestMapTools_DestinationsModel_HarnessToolsIsUnionOfDestinationNames(t *testing.T) {
	// ToolResolution.HarnessTools must be the flattened, deduplicated, first-seen-order union
	// of all Destinations' Names slices. With two destinations carrying "AskUserQuestion" and
	// "user-feedback", HarnessTools must contain both names in declaration order.
	// This test is RED until MapTools derives HarnessTools from Destinations.
	d := makeDestinationOnlyDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]

	// Expect both names in declaration order: main dest first, then field dest.
	wantTools := []string{"AskUserQuestion", "user-feedback"}
	if len(res.HarnessTools) != len(wantTools) {
		t.Fatalf("HarnessTools count: want %d (union of all destination Names), got %d: %v",
			len(wantTools), len(res.HarnessTools), res.HarnessTools)
	}
	for i, want := range wantTools {
		if res.HarnessTools[i] != want {
			t.Errorf("HarnessTools[%d]: want %q, got %q", i, want, res.HarnessTools[i])
		}
	}
}

func TestMapTools_DestinationsModel_HarnessToolsDeduplicatesAcrossDestinations(t *testing.T) {
	// When two destinations name the same harness tool, HarnessTools must contain it only once.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "dedup-harness",
		DisplayName:   "Dedup Harness",
		Tools: domain.ToolSpec{
			Shape:    domain.ShapeList,
			Universe: []domain.HarnessTool{{Name: "SharedTool", Unused: domain.Deny}},
			Mappings: []domain.ToolMapping{
				{
					Generic: "some_tool",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"SharedTool"}},
						{Kind: domain.DestField, Field: "extra", Names: []string{"SharedTool", "OtherTool"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"some_tool"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]

	// SharedTool appears in both destinations; HarnessTools must contain it only once.
	seen := make(map[string]int)
	for _, ht := range res.HarnessTools {
		seen[ht]++
	}
	if count := seen["SharedTool"]; count > 1 {
		t.Errorf("HarnessTools contains %q %d times; must be deduplicated to appear at most once", "SharedTool", count)
	}
	// "OtherTool" should also be present once.
	if seen["OtherTool"] == 0 {
		t.Errorf("HarnessTools does not contain %q; it must be included from the DestField destination", "OtherTool")
	}
}

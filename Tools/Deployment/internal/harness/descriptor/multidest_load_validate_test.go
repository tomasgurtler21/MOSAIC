package descriptor_test

// multidest_load_validate_test.go verifies the new descriptor loading behaviour for the
// destinations: mapping schema and the standalone ValidateToolMappings function.
//
// Loader tests (using fixture files in testdata/descriptors/):
//   - A descriptor using the new destinations: schema parses without error.
//   - After loading, each ToolMapping.Destinations is populated with the declared destinations.
//   - An explicitly-unsupported mapping (destinations: []) loads with an empty Destinations slice.
//   - Descriptors that violate any of rules R3–R10 are rejected with a ValidationError.
//
// ValidateToolMappings unit tests (using domain structs directly):
//   - R1: duplicate generic name in the same mapping set
//   - R2: empty generic name
//   - R3: unknown destination kind
//   - R4: DestField without a field name
//   - R5: DestMain with a field name
//   - R6: unknown format value
//   - R7: DestMain with a format value
//   - R8: separator on a non-scalar format
//   - R9: duplicate (kind, field) pair within one mapping
//   - R10: non-empty destinations list with an empty Names slice
//   - fieldPrefix appears in every returned error's Field path
//   - A nil or empty mapping list returns no errors
//   - A valid mapping set returns no errors
//
// Loader tests are RED because descriptor.Parse rejects "destinations:" as an unknown field
// until the loader wire types are updated (I2.2). ValidateToolMappings tests are RED because
// the current stub returns nil for all inputs.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- Loader tests ---

func TestLoad_MultiDestDescriptor_ParsesWithoutError(t *testing.T) {
	// A descriptor that uses the new destinations: schema must load without error.
	// This test is RED until the wire types in load.go are updated to accept destinations:.
	d, err := descriptor.Load(testdataDir + "/valid-multidest-list.yaml")
	if err != nil {
		t.Fatalf("Load(valid-multidest-list.yaml): unexpected error: %v; "+
			"a descriptor using the destinations: schema must be accepted by the loader", err)
	}
	if d == nil {
		t.Fatal("Load returned nil descriptor without error")
	}
}

func TestLoad_MultiDestDescriptor_ToolMappingDestinationsPopulated(t *testing.T) {
	// After loading a valid-multidest-list.yaml descriptor, each mapping must have its
	// Destinations slice populated from the YAML destinations: block.
	// This test is RED until the loader populates ToolMapping.Destinations.
	d, err := descriptor.Load(testdataDir + "/valid-multidest-list.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, m := range d.Tools.Mappings {
		if m.Generic == "file_read" {
			if len(m.Destinations) == 0 {
				t.Errorf("file_read mapping: Destinations must be non-empty after loading; "+
					"the YAML declares one destination (to: main, names: [read/readFile])")
			}
			continue
		}
		if m.Generic == "user_interaction" {
			if len(m.Destinations) != 2 {
				t.Errorf("user_interaction mapping: want 2 Destinations, got %d; "+
					"the YAML declares one main and one field destination", len(m.Destinations))
			}
		}
	}
}

func TestLoad_MultiDestDescriptor_MainDestinationKindParsed(t *testing.T) {
	// A destinations entry with to: main must produce a ToolDestination with Kind == DestMain.
	d, err := descriptor.Load(testdataDir + "/valid-multidest-list.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, m := range d.Tools.Mappings {
		if m.Generic == "file_read" {
			if len(m.Destinations) == 0 {
				t.Fatal("file_read Destinations empty — cannot verify Kind")
			}
			if m.Destinations[0].Kind != domain.DestMain {
				t.Errorf("file_read Destinations[0].Kind: want %q, got %q", domain.DestMain, m.Destinations[0].Kind)
			}
			return
		}
	}
	t.Fatal("file_read mapping not found in loaded descriptor")
}

func TestLoad_MultiDestDescriptor_FieldDestinationParsed(t *testing.T) {
	// A destinations entry with to: field must produce a ToolDestination with Kind == DestField
	// and the declared Field and Format values.
	d, err := descriptor.Load(testdataDir + "/valid-multidest-list.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, m := range d.Tools.Mappings {
		if m.Generic == "user_interaction" {
			if len(m.Destinations) < 2 {
				t.Fatalf("user_interaction Destinations count: want ≥2, got %d", len(m.Destinations))
			}
			fieldDest := m.Destinations[1] // second destination is the DestField one
			if fieldDest.Kind != domain.DestField {
				t.Errorf("user_interaction Destinations[1].Kind: want %q, got %q", domain.DestField, fieldDest.Kind)
			}
			if fieldDest.Field != "mcpServers" {
				t.Errorf("user_interaction Destinations[1].Field: want %q, got %q", "mcpServers", fieldDest.Field)
			}
			if fieldDest.Format != domain.FormatListBlock {
				t.Errorf("user_interaction Destinations[1].Format: want %q, got %q", domain.FormatListBlock, fieldDest.Format)
			}
			return
		}
	}
	t.Fatal("user_interaction mapping not found in loaded descriptor")
}

func TestLoad_MultiDestDescriptor_EmptyDestinationsLoadsCorrectly(t *testing.T) {
	// A mapping with destinations: [] must load with an empty (not nil) Destinations slice,
	// expressing "declared unsupported by this harness".
	d, err := descriptor.Load(testdataDir + "/valid-multidest-list.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, m := range d.Tools.Mappings {
		if m.Generic == "unsupported_tool" {
			if m.Destinations == nil {
				t.Error("unsupported_tool Destinations must be an empty (non-nil) slice, not nil; " +
					"destinations: [] expresses explicit unsupport, which is distinct from an absent mapping")
			} else if len(m.Destinations) != 0 {
				t.Errorf("unsupported_tool Destinations: want empty slice, got %d entries", len(m.Destinations))
			}
			return
		}
	}
	t.Fatal("unsupported_tool mapping not found in loaded descriptor")
}

// --- Loader validation: invalid fixtures ---

func TestLoad_DestinationUnknownKind_ReturnsError(t *testing.T) {
	// A descriptor whose destinations entry declares an unknown "to" value must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-unknown-kind.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a destination with an unknown kind; got nil")
	}
}

func TestLoad_DestinationFieldWithoutName_ReturnsError(t *testing.T) {
	// A destination with to: field but no field name must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-field-without-name.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a to:field destination without a field name; got nil")
	}
}

func TestLoad_DestinationMainWithField_ReturnsError(t *testing.T) {
	// A destination with to: main that also declares a field name must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-main-with-field.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a to:main destination with a field name; got nil")
	}
}

func TestLoad_DestinationUnknownFormat_ReturnsError(t *testing.T) {
	// A destination with an unknown format value must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-unknown-format.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a destination with an unknown format; got nil")
	}
}

func TestLoad_DestinationMainWithFormat_ReturnsError(t *testing.T) {
	// A to:main destination with an explicit format must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-main-with-format.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a to:main destination with a format; got nil")
	}
}

func TestLoad_DestinationSeparatorOnNonScalar_ReturnsError(t *testing.T) {
	// A separator field on a non-scalar format must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-separator-nonscalar.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a separator on a non-scalar format; got nil")
	}
}

func TestLoad_DestinationDuplicate_ReturnsError(t *testing.T) {
	// Two destinations within the same mapping that share the same (to, field) pair must be rejected.
	_, err := descriptor.Load(testdataDir + "/invalid/dest-duplicate-destination.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for duplicate destinations within one mapping; got nil")
	}
}

func TestLoad_DestinationEmptyNames_ReturnsError(t *testing.T) {
	// A destination entry with an empty names list (inside a non-empty destinations slice)
	// must be rejected; to declare a tool unsupported, use destinations: [].
	_, err := descriptor.Load(testdataDir + "/invalid/dest-empty-names.yaml")
	if err == nil {
		t.Fatal("expected Load to return an error for a destination with empty names inside a non-empty destinations list; got nil")
	}
}

// --- ValidateToolMappings unit tests ---

// validMappingSet returns a slice of two valid ToolMappings for use in positive tests.
func validMappingSet() []domain.ToolMapping {
	return []domain.ToolMapping{
		{
			Generic: "file_read",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"read/readFile"}},
			},
		},
		{
			Generic: "user_interaction",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
			},
		},
	}
}

func TestValidateToolMappings_NilSlice_ReturnsNoErrors(t *testing.T) {
	// A nil mapping slice is valid (no mappings declared).
	errs := descriptor.ValidateToolMappings(nil, "tools.mappings")
	if len(errs) != 0 {
		t.Errorf("nil mappings: expected no errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateToolMappings_EmptySlice_ReturnsNoErrors(t *testing.T) {
	// An empty mapping slice is valid.
	errs := descriptor.ValidateToolMappings([]domain.ToolMapping{}, "tools.mappings")
	if len(errs) != 0 {
		t.Errorf("empty mappings: expected no errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateToolMappings_ValidMappingSet_ReturnsNoErrors(t *testing.T) {
	// A well-formed mapping set produces no errors.
	errs := descriptor.ValidateToolMappings(validMappingSet(), "tools.mappings")
	if len(errs) != 0 {
		t.Errorf("valid mapping set: expected no errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateToolMappings_DuplicateGeneric_ReturnsError(t *testing.T) {
	// R1: two mappings in the set share the same Generic name.
	// This test is RED until ValidateToolMappings implements rule R1.
	mappings := []domain.ToolMapping{
		{Generic: "file_read", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"read/readFile"}},
		}},
		{Generic: "file_read", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"read/openFile"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for duplicate generic name, got none")
	}
}

func TestValidateToolMappings_DuplicateGeneric_ErrorMentionsDuplicateName(t *testing.T) {
	mappings := []domain.ToolMapping{
		{Generic: "file_read", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"toolA"}},
		}},
		{Generic: "file_read", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"toolB"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected errors, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "file_read") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an error mentioning the duplicate name %q; got: %v", "file_read", errs)
	}
}

func TestValidateToolMappings_EmptyGeneric_ReturnsError(t *testing.T) {
	// R2: a mapping with an empty Generic field must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for empty generic name, got none")
	}
}

func TestValidateToolMappings_UnknownDestinationKind_ReturnsError(t *testing.T) {
	// R3: a destination with an unrecognised Kind string must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: "unknown-kind", Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for unknown destination kind, got none")
	}
}

func TestValidateToolMappings_UnknownDestinationKind_ErrorFieldPathContainsDestinationIndex(t *testing.T) {
	// The error's Field path must identify the offending destination by index.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"goodDest"}},
			{Kind: "bad-kind", Names: []string{"badDest"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected errors, got none")
	}
	foundIndexed := false
	for _, e := range errs {
		if strings.Contains(e.Field, "destinations[1]") {
			foundIndexed = true
			break
		}
	}
	if !foundIndexed {
		t.Errorf("expected error Field to contain destinations[1] for the second (bad) destination; got: %v", errs)
	}
}

func TestValidateToolMappings_FieldDestWithoutFieldName_ReturnsError(t *testing.T) {
	// R4: a DestField destination with an empty Field must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestField, Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for DestField without a field name, got none")
	}
}

func TestValidateToolMappings_MainDestWithFieldName_ReturnsError(t *testing.T) {
	// R5: a DestMain destination with a non-empty Field must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Field: "shouldNotBeSet", Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for DestMain with a field name, got none")
	}
}

func TestValidateToolMappings_UnknownFormat_ReturnsError(t *testing.T) {
	// R6: a destination with an unrecognised Format string must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestField, Field: "myField", Format: "not-a-format", Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for unknown format, got none")
	}
}

func TestValidateToolMappings_MainDestWithFormat_ReturnsError(t *testing.T) {
	// R7: a DestMain destination with a Format value must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Format: domain.FormatListFlow, Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for DestMain with a format, got none")
	}
}

func TestValidateToolMappings_SeparatorOnNonScalarFormat_ReturnsError(t *testing.T) {
	// R8: a non-scalar format destination with a non-empty Separator must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestField, Field: "myField", Format: domain.FormatListBlock, Separator: ",", Names: []string{"toolA"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for separator on non-scalar format, got none")
	}
}

func TestValidateToolMappings_DuplicateDestination_ReturnsError(t *testing.T) {
	// R9: two destinations within the same mapping with identical (Kind, Field) pairs must be rejected.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestField, Field: "myField", Names: []string{"toolA"}},
			{Kind: domain.DestField, Field: "myField", Names: []string{"toolB"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for duplicate (kind, field) pair, got none")
	}
}

func TestValidateToolMappings_DuplicateDestMain_ReturnsError(t *testing.T) {
	// R9 also applies to two DestMain destinations in the same mapping: they share the same
	// (Kind="main", Field="") pair and must be rejected. The existing R9 test uses two
	// DestField destinations with the same field name; this case exercises the same rule at
	// Kind==DestMain.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"toolA"}},
			{Kind: domain.DestMain, Names: []string{"toolB"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for two DestMain destinations in the same mapping, got none; " +
			"R9 applies to (Kind, Field) pairs: two DestMain destinations share (main, \"\") and must be rejected")
	}
}

func TestValidateToolMappings_EmptyNamesInNonEmptyDestinationsList_ReturnsError(t *testing.T) {
	// R10: a non-empty destinations list must not contain a destination with an empty Names slice.
	mappings := []domain.ToolMapping{
		{Generic: "some_tool", Destinations: []domain.ToolDestination{
			{Kind: domain.DestField, Field: "myField", Names: []string{}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected at least one ValidationError for destination with empty Names inside a non-empty destinations list, got none")
	}
}

// --- fieldPrefix appears in returned error Field paths ---

func TestValidateToolMappings_FieldPrefix_AppearsInErrorFieldPath(t *testing.T) {
	// The fieldPrefix argument must be prepended to every returned error's Field path.
	// A caller using "tool_destinations.claude-code" must see that prefix in the Field.
	const prefix = "tool_destinations.claude-code"
	mappings := []domain.ToolMapping{
		{Generic: "file_read", Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: []string{"read"}},
		}},
		{Generic: "file_read", Destinations: []domain.ToolDestination{ // duplicate
			{Kind: domain.DestMain, Names: []string{"read"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, prefix)

	if len(errs) == 0 {
		t.Fatal("expected errors for duplicate generic, got none")
	}
	for _, e := range errs {
		if !strings.HasPrefix(e.Field, prefix) {
			t.Errorf("error Field %q does not start with fieldPrefix %q; "+
				"ValidateToolMappings must prepend the fieldPrefix to every error Field", e.Field, prefix)
		}
	}
}

func TestValidateToolMappings_SameRulesApplyForDifferentPrefixes(t *testing.T) {
	// The same rule set must produce equivalent errors (same message, field structure)
	// regardless of the fieldPrefix value. Only the prefix portion of Field differs.
	invalidMappings := []domain.ToolMapping{
		{Generic: "tool", Destinations: []domain.ToolDestination{
			{Kind: "bad-kind", Names: []string{"toolA"}},
		}},
	}

	errs1 := descriptor.ValidateToolMappings(invalidMappings, "tools.mappings")
	errs2 := descriptor.ValidateToolMappings(invalidMappings, "tool_destinations.harness-x")

	if len(errs1) == 0 || len(errs2) == 0 {
		t.Fatalf("expected errors from both prefix variants; got len(errs1)=%d, len(errs2)=%d",
			len(errs1), len(errs2))
	}
	// Messages must be identical; only the Field prefix differs.
	if errs1[0].Message != errs2[0].Message {
		t.Errorf("same invalid mapping with different fieldPrefix produced different messages:\n  prefix1: %q\n  prefix2: %q",
			errs1[0].Message, errs2[0].Message)
	}
}

// --- ValidateToolMappings is callable from Validate via descriptor package ---

func TestValidate_DelegatesToolMappingValidationToValidateToolMappings(t *testing.T) {
	// Validate must call ValidateToolMappings for the descriptor's own mappings.
	// Verify by constructing a descriptor whose mappings are invalid (unknown kind) and
	// confirming that Validate returns an error mentioning the destination field path.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "validation-delegation-harness",
		DisplayName:   "Validation Delegation Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool",
					Destinations: []domain.ToolDestination{
						{Kind: "bad-kind", Names: []string{"toolA"}},
					},
				},
			},
		},
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected Validate to return errors for a mapping with an invalid destination kind; got none")
	}
	// At least one error must reference the destination path.
	found := false
	for _, e := range errs {
		if strings.Contains(e.Field, "destinations") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one Validate error referencing 'destinations' in its Field path; got: %v", errs)
	}
}

// TestValidateToolMappings_ReturnsStructuredErrors verifies that ValidateToolMappings
// errors satisfy the descriptor.ValidationError interface (for errors.As compatibility).
func TestValidateToolMappings_ReturnsStructuredErrors(t *testing.T) {
	mappings := []domain.ToolMapping{
		{Generic: "t", Destinations: []domain.ToolDestination{
			{Kind: "bad", Names: []string{"n"}},
		}},
	}

	errs := descriptor.ValidateToolMappings(mappings, "tools.mappings")

	if len(errs) == 0 {
		t.Fatal("expected errors, got none")
	}
	for i, e := range errs {
		var ve descriptor.ValidationError
		var errI error = e
		if !errors.As(errI, &ve) {
			t.Errorf("ValidateToolMappings error[%d] does not unwrap to descriptor.ValidationError; got %T", i, errI)
		}
		if e.Field == "" {
			t.Errorf("ValidateToolMappings error[%d].Field must not be empty", i)
		}
		if e.Message == "" {
			t.Errorf("ValidateToolMappings error[%d].Message must not be empty", i)
		}
	}
}

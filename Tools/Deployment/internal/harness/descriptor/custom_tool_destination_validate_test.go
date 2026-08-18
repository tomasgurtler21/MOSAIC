package descriptor_test

// Tests for the structural validation rules applied to custom_tool_destination entries.
//
// Coverage (one test per rule, mirroring the ValidateToolMappings R3–R9 destination rules):
//   - A valid custom_tool_destination list produces no validation errors.
//   - An entry with an unknown "to" value produces a validation error at
//     tools.custom_tool_destination[N].to and skips the remaining checks for that entry.
//   - A to:field entry with an empty field name produces a validation error at
//     tools.custom_tool_destination[N].field.
//   - A to:main entry that declares a field name produces a validation error at
//     tools.custom_tool_destination[N].field.
//   - An entry with an unknown format value produces a validation error at
//     tools.custom_tool_destination[N].format.
//   - A to:main entry that declares a format value produces a validation error at
//     tools.custom_tool_destination[N].format.
//   - An entry with a separator but without format:scalar produces a validation error at
//     tools.custom_tool_destination[N].separator.
//   - Two entries with the same (to, field) pair produce a validation error at
//     tools.custom_tool_destination[N] for the second occurrence.
//   - A nil or empty CustomToolDestinations slice produces no validation errors.
//   - Validation errors carry field paths of the form
//     tools.custom_tool_destination[N].<key>, matching the existing convention.

import (
	"fmt"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- Helpers ---

// descWithCustomDests constructs a minimal valid HarnessDescriptor whose
// CustomToolDestinations is set to the supplied slice. The other required fields are
// populated to prevent noise from unrelated validation errors.
func descWithCustomDests(dests []domain.ToolDestination) *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "ctd-validate-test",
		DisplayName:   "CTD Validate Test",
		Tools: domain.ToolSpec{
			CustomToolDestinations: dests,
		},
	}
}

// firstErrorForField returns the first ValidationError whose Field contains fieldSubstr,
// or nil if none exists.
func firstErrorForField(errs []descriptor.ValidationError, fieldSubstr string) *descriptor.ValidationError {
	for i := range errs {
		if strings.Contains(errs[i].Field, fieldSubstr) {
			return &errs[i]
		}
	}
	return nil
}

// --- Valid: no errors ---

// TestValidate_CustomToolDestination_ValidEntry_ProducesNoErrors asserts that a well-formed
// custom_tool_destination list (to:field with a non-empty field name and a known format)
// does not produce any validation errors.
func TestValidate_CustomToolDestination_ValidEntry_ProducesNoErrors(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
	})

	errs := descriptor.Validate(d)

	// Filter to only errors that concern custom_tool_destination.
	var ctdErrs []descriptor.ValidationError
	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination") {
			ctdErrs = append(ctdErrs, e)
		}
	}
	if len(ctdErrs) != 0 {
		t.Errorf("expected no validation errors for a valid custom_tool_destination list, got %d: %v", len(ctdErrs), ctdErrs)
	}
}

// TestValidate_CustomToolDestination_NilSlice_ProducesNoErrors asserts that a nil
// CustomToolDestinations slice (field absent / not set) is not an error.
func TestValidate_CustomToolDestination_NilSlice_ProducesNoErrors(t *testing.T) {
	d := descWithCustomDests(nil)

	errs := descriptor.Validate(d)

	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination") {
			t.Errorf("unexpected validation error for nil CustomToolDestinations: %v", e)
		}
	}
}

// TestValidate_CustomToolDestination_EmptySlice_ProducesNoErrors asserts that an empty
// CustomToolDestinations slice (custom_tool_destination: []) is not an error.
func TestValidate_CustomToolDestination_EmptySlice_ProducesNoErrors(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{})

	errs := descriptor.Validate(d)

	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination") {
			t.Errorf("unexpected validation error for empty CustomToolDestinations: %v", e)
		}
	}
}

// TestValidate_CustomToolDestination_ValidMainEntry_ProducesNoErrors asserts that a
// well-formed to:main entry (no field, no format) does not produce any validation errors.
// This covers the positive case for to:main, complementing the to:field positive case in
// TestValidate_CustomToolDestination_ValidEntry_ProducesNoErrors.
func TestValidate_CustomToolDestination_ValidMainEntry_ProducesNoErrors(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestMain, Names: []string{}},
	})

	errs := descriptor.Validate(d)

	var ctdErrs []descriptor.ValidationError
	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination") {
			ctdErrs = append(ctdErrs, e)
		}
	}
	if len(ctdErrs) != 0 {
		t.Errorf("expected no validation errors for a valid to:main custom_tool_destination entry "+
			"(no field, no format), got %d: %v", len(ctdErrs), ctdErrs)
	}
}

// --- Rule: unknown "to" value ---

// TestValidate_CustomToolDestination_UnknownKind_ProducesError asserts that an entry
// with a "to" value that is neither "main" nor "field" produces a validation error.
func TestValidate_CustomToolDestination_UnknownKind_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.ToolDestinationKind("unknown"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination")
	if found == nil {
		t.Fatal("expected a validation error for an unknown 'to' value, got none")
	}
}

// TestValidate_CustomToolDestination_UnknownKind_FieldPathContainsToDotKey asserts that
// the validation error for an unknown kind references the .to sub-key in its Field path.
func TestValidate_CustomToolDestination_UnknownKind_FieldPathContainsToDotKey(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.ToolDestinationKind("bogus"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].to")
	if found == nil {
		t.Errorf("expected a validation error with field path containing \"custom_tool_destination[0].to\"; got errors: %v", errs)
	}
}

// TestValidate_CustomToolDestination_UnknownKind_MessageNamesValidKinds asserts that the
// error message for an unknown kind names the valid alternatives (main, field).
func TestValidate_CustomToolDestination_UnknownKind_MessageNamesValidKinds(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.ToolDestinationKind("bogus"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].to")
	if found == nil {
		t.Fatalf("expected error at custom_tool_destination[0].to, got none; all errors: %v", errs)
	}
	if !strings.Contains(found.Message, "main") || !strings.Contains(found.Message, "field") {
		t.Errorf("error message should name valid kinds (main, field); got: %q", found.Message)
	}
}

// --- Rule: to:field without a field name ---

// TestValidate_CustomToolDestination_FieldKindEmptyFieldName_ProducesError asserts that
// a to:field entry without a field name produces a validation error.
func TestValidate_CustomToolDestination_FieldKindEmptyFieldName_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].field")
	if found == nil {
		t.Errorf("expected a validation error for to:field with empty field name; got errors: %v", errs)
	}
}

// TestValidate_CustomToolDestination_FieldKindEmptyFieldName_FieldPathIsCorrect asserts
// the error field path for a missing field name.
func TestValidate_CustomToolDestination_FieldKindEmptyFieldName_FieldPathIsCorrect(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	want := "tools.custom_tool_destination[0].field"
	found := firstErrorForField(errs, want)
	if found == nil {
		t.Errorf("expected error with Field containing %q; got errors: %v", want, errs)
	}
}

// --- Rule: to:main with a field name ---

// TestValidate_CustomToolDestination_MainKindWithFieldName_ProducesError asserts that
// a to:main entry that also declares a field name produces a validation error.
func TestValidate_CustomToolDestination_MainKindWithFieldName_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestMain, Field: "someKey", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].field")
	if found == nil {
		t.Errorf("expected a validation error for to:main with a field name; got errors: %v", errs)
	}
}

// --- Rule: unknown format value ---

// TestValidate_CustomToolDestination_UnknownFormat_ProducesError asserts that a format
// value that is not one of list-block, list-flow, or scalar produces a validation error.
func TestValidate_CustomToolDestination_UnknownFormat_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.ToolValueFormat("unknown-format"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].format")
	if found == nil {
		t.Errorf("expected a validation error for unknown format value; got errors: %v", errs)
	}
}

// TestValidate_CustomToolDestination_UnknownFormat_FieldPathIsCorrect asserts the error
// field path for an unknown format.
func TestValidate_CustomToolDestination_UnknownFormat_FieldPathIsCorrect(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.ToolValueFormat("bad"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	want := "tools.custom_tool_destination[0].format"
	found := firstErrorForField(errs, want)
	if found == nil {
		t.Errorf("expected error with Field containing %q; got errors: %v", want, errs)
	}
}

// TestValidate_CustomToolDestination_UnknownFormat_MessageNamesValidFormats asserts that
// the error message for an unknown format names the valid alternatives.
func TestValidate_CustomToolDestination_UnknownFormat_MessageNamesValidFormats(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.ToolValueFormat("bad"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].format")
	if found == nil {
		t.Fatalf("expected error at custom_tool_destination[0].format, got none")
	}
	if !strings.Contains(found.Message, "list-block") || !strings.Contains(found.Message, "list-flow") || !strings.Contains(found.Message, "scalar") {
		t.Errorf("error message should name valid formats (list-block, list-flow, scalar); got: %q", found.Message)
	}
}

// --- Rule: to:main with a format ---

// TestValidate_CustomToolDestination_MainKindWithFormat_ProducesError asserts that a
// to:main entry that also declares a format value produces a validation error.
func TestValidate_CustomToolDestination_MainKindWithFormat_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestMain, Format: domain.FormatListBlock, Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].format")
	if found == nil {
		t.Errorf("expected a validation error for to:main with a format value; got errors: %v", errs)
	}
}

// --- Rule: separator without format:scalar ---

// TestValidate_CustomToolDestination_SeparatorWithoutScalarFormat_ProducesError asserts
// that declaring a separator on an entry without format:scalar produces a validation error.
func TestValidate_CustomToolDestination_SeparatorWithoutScalarFormat_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Separator: "|", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[0].separator")
	if found == nil {
		t.Errorf("expected a validation error for separator without format:scalar; got errors: %v", errs)
	}
}

// TestValidate_CustomToolDestination_SeparatorWithoutScalarFormat_FieldPathIsCorrect
// asserts the error field path for an out-of-place separator.
func TestValidate_CustomToolDestination_SeparatorWithoutScalarFormat_FieldPathIsCorrect(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Separator: "|", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	want := "tools.custom_tool_destination[0].separator"
	found := firstErrorForField(errs, want)
	if found == nil {
		t.Errorf("expected error with Field containing %q; got errors: %v", want, errs)
	}
}

// TestValidate_CustomToolDestination_SeparatorWithScalarFormat_NoError asserts that
// a separator declared alongside format:scalar is accepted (separator is meaningful there).
func TestValidate_CustomToolDestination_SeparatorWithScalarFormat_NoError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatScalar, Separator: "|", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination") {
			t.Errorf("unexpected validation error for separator with format:scalar: %v", e)
		}
	}
}

// --- Rule: duplicate (to, field) pair ---

// TestValidate_CustomToolDestination_DuplicateDestination_ProducesError asserts that
// two entries with the same (Kind, Field) pair produce a validation error for the
// second occurrence.
func TestValidate_CustomToolDestination_DuplicateDestination_ProducesError(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListFlow, Names: []string{}},
	})

	errs := descriptor.Validate(d)

	found := firstErrorForField(errs, "custom_tool_destination[1]")
	if found == nil {
		t.Errorf("expected a validation error for duplicate (to:field, field:mcpServers) pair at index [1]; got errors: %v", errs)
	}
}

// TestValidate_CustomToolDestination_DuplicateDestination_FieldPathIsCorrect asserts
// that the duplicate destination error field path matches the convention.
func TestValidate_CustomToolDestination_DuplicateDestination_FieldPathIsCorrect(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListFlow, Names: []string{}},
	})

	errs := descriptor.Validate(d)

	// The field path for a duplicate destination is the entry itself (no sub-key),
	// matching the ValidateToolMappings R9 convention.
	want := "tools.custom_tool_destination[1]"
	found := firstErrorForField(errs, want)
	if found == nil {
		t.Errorf("expected error with Field containing %q; got errors: %v", want, errs)
	}
}

// TestValidate_CustomToolDestination_DuplicateDestination_FirstOccurrenceUnchanged asserts
// that the first occurrence of a duplicate pair does not itself gain a validation error;
// only the second (and later) occurrences are flagged.
func TestValidate_CustomToolDestination_DuplicateDestination_FirstOccurrenceUnchanged(t *testing.T) {
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
		{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListFlow, Names: []string{}},
	})

	errs := descriptor.Validate(d)

	// There must be no error at index [0] for this scenario — only [1] is the duplicate.
	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination[0]") {
			t.Errorf("unexpected error at custom_tool_destination[0] (the first, non-duplicate entry): %v", e)
		}
	}
}

// --- Rule: unknown kind skips remaining checks for that entry ---

// TestValidate_CustomToolDestination_UnknownKind_SkipsRemainingChecks asserts that when
// a destination has an unknown kind, the validator does not also report errors for the
// other attributes of that entry (which would be spurious given the invalid kind).
func TestValidate_CustomToolDestination_UnknownKind_SkipsRemainingChecks(t *testing.T) {
	// This entry has an unknown kind AND a missing field name, which would normally also
	// produce a field-name error. But the unknown-kind error should cause the rest of
	// this entry's checks to be skipped.
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.ToolDestinationKind("bogus"), Field: "", Names: []string{}},
	})

	errs := descriptor.Validate(d)

	var ctdErrs []descriptor.ValidationError
	for _, e := range errs {
		if strings.Contains(e.Field, "custom_tool_destination") {
			ctdErrs = append(ctdErrs, e)
		}
	}
	// Expect exactly one error (the unknown kind), not two.
	if len(ctdErrs) != 1 {
		t.Errorf("expected exactly 1 validation error (unknown kind) for entry with unknown kind; got %d: %v", len(ctdErrs), ctdErrs)
	}
}

// --- Field path format ---

// TestValidate_CustomToolDestination_FieldPathPrefix asserts that the prefix for all
// custom_tool_destination validation errors is exactly "tools.custom_tool_destination",
// matching the convention used by existing destination rules.
func TestValidate_CustomToolDestination_FieldPathPrefix(t *testing.T) {
	tests := []struct {
		name  string
		dests []domain.ToolDestination
	}{
		{
			name: "unknown kind",
			dests: []domain.ToolDestination{
				{Kind: domain.ToolDestinationKind("bogus"), Names: []string{}},
			},
		},
		{
			name: "field kind empty name",
			dests: []domain.ToolDestination{
				{Kind: domain.DestField, Field: "", Names: []string{}},
			},
		},
		{
			name: "main kind with field",
			dests: []domain.ToolDestination{
				{Kind: domain.DestMain, Field: "key", Names: []string{}},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := descWithCustomDests(tc.dests)
			errs := descriptor.Validate(d)

			const prefix = "tools.custom_tool_destination"
			for _, e := range errs {
				// Only check errors that are about custom_tool_destination at all.
				if !strings.Contains(e.Field, "custom_tool_destination") {
					continue
				}
				if !strings.HasPrefix(e.Field, prefix) {
					t.Errorf("[%s] error field %q does not start with the expected prefix %q",
						tc.name, e.Field, prefix)
				}
			}
		})
	}
}

// TestValidate_CustomToolDestination_IndexInFieldPath asserts that the entry index
// appears correctly in the field path as [N], matching the existing convention for
// tools.mappings[N].destinations[M].<key>.
func TestValidate_CustomToolDestination_IndexInFieldPath(t *testing.T) {
	// Put the invalid entry at index 2 to verify the index is accurate.
	d := descWithCustomDests([]domain.ToolDestination{
		{Kind: domain.DestField, Field: "a", Format: domain.FormatListBlock, Names: []string{}},
		{Kind: domain.DestField, Field: "b", Format: domain.FormatListBlock, Names: []string{}},
		{Kind: domain.ToolDestinationKind("bogus"), Names: []string{}},
	})

	errs := descriptor.Validate(d)

	want := fmt.Sprintf("tools.custom_tool_destination[%d]", 2)
	found := firstErrorForField(errs, want)
	if found == nil {
		t.Errorf("expected a validation error whose field path contains %q (to verify index accuracy); got errors: %v", want, errs)
	}
}

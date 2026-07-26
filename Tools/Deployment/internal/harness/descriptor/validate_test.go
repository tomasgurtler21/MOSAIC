package descriptor_test

// Tests for schema validation: each category of invalid descriptor produces an actionable,
// located error that names the file, the field, and what was wrong.
//
// Coverage:
//   - A valid descriptor produces no validation errors.
//   - A missing required field (id) produces a ValidationError naming the field.
//   - A missing required field (schema_version) produces a ValidationError naming the field.
//   - Duplicate tool mapping generic names produce a ValidationError identifying the duplicate.
//   - ValidationError.Error() matches the expected "<file>:<line>: <field>: <message>" format.
//   - ValidationError.Field and .Message are non-empty on every returned error.
//   - An unknown YAML field causes Parse to return an error (unknown fields are not silently ignored).
//   - A wrong-type YAML value causes Parse to return an error.
//   - Load returns an error for a file with a missing required field.
//   - Load returns an error for a file with duplicate tool mappings.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- Validate: valid descriptor ---

func TestValidate_ValidDescriptor_ReturnsEmptyErrors(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "valid-harness",
		DisplayName:   "Valid Harness",
	}

	errs := descriptor.Validate(d)

	if len(errs) != 0 {
		t.Errorf("expected no validation errors for a valid descriptor, got %d: %v", len(errs), errs)
	}
}

// --- Validate: missing required field — id ---

func TestValidate_MissingID_ReturnsAtLeastOneError(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		DisplayName:   "No ID Harness",
		// ID is intentionally left as zero value ("").
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected at least one validation error for a descriptor with an empty id, got none")
	}
}

func TestValidate_MissingID_ErrorNamesIDField(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		DisplayName:   "No ID Harness",
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	// At least one error must reference the "id" field.
	var found bool
	for _, e := range errs {
		if strings.Contains(strings.ToLower(e.Field), "id") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no validation error references the 'id' field; got errors: %v", errs)
	}
}

func TestValidate_MissingID_ErrorHasNonEmptyMessage(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		DisplayName:   "No ID Harness",
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	for i, e := range errs {
		if e.Message == "" {
			t.Errorf("ValidationError[%d].Message must not be empty", i)
		}
	}
}

// --- Validate: missing required field — schema_version ---

func TestValidate_MissingSchemaVersion_ReturnsAtLeastOneError(t *testing.T) {
	d := &domain.HarnessDescriptor{
		ID:          "no-version-harness",
		DisplayName: "No Version Harness",
		// SchemaVersion is intentionally left as zero value ("").
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected at least one validation error for a descriptor with an empty schema_version, got none")
	}
}

func TestValidate_MissingSchemaVersion_ErrorNamesSchemaVersionField(t *testing.T) {
	d := &domain.HarnessDescriptor{
		ID:          "no-version-harness",
		DisplayName: "No Version Harness",
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	var found bool
	for _, e := range errs {
		lower := strings.ToLower(e.Field)
		if strings.Contains(lower, "schema_version") || strings.Contains(lower, "schemaversion") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no validation error references the 'schema_version' field; got errors: %v", errs)
	}
}

// --- Validate: duplicate tool mappings ---

func TestValidate_DuplicateToolMappings_ReturnsAtLeastOneError(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "dup-harness",
		DisplayName:   "Duplicate Mappings Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{Generic: "file_read", HarnessTools: []string{"read/readFile"}},
				{Generic: "file_read", HarnessTools: []string{"read/openFile"}}, // duplicate generic
			},
		},
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected at least one validation error for duplicate tool mapping generic names, got none")
	}
}

func TestValidate_DuplicateToolMappings_ErrorMentionsDuplicateName(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "dup-harness",
		DisplayName:   "Duplicate Mappings Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{Generic: "file_read", HarnessTools: []string{"read/readFile"}},
				{Generic: "file_read", HarnessTools: []string{"read/openFile"}},
			},
		},
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	// The error must mention the duplicate generic name or the mappings field path.
	var found bool
	for _, e := range errs {
		if strings.Contains(e.Message, "file_read") || strings.Contains(e.Field, "mappings") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no validation error mentions 'file_read' or 'mappings'; got errors: %v", errs)
	}
}

func TestValidate_DuplicateToolMappings_FieldPathContainsIndex(t *testing.T) {
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "dup-harness",
		DisplayName:   "Duplicate Mappings Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{Generic: "file_read", HarnessTools: []string{"read/readFile"}},
				{Generic: "file_read", HarnessTools: []string{"read/openFile"}},
			},
		},
	}

	errs := descriptor.Validate(d)

	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	// The field path should identify the location, e.g. "tools.mappings[1].generic".
	var hasIndex bool
	for _, e := range errs {
		if strings.Contains(e.Field, "[") && strings.Contains(e.Field, "]") {
			hasIndex = true
			break
		}
	}
	if !hasIndex {
		t.Errorf("expected at least one error with an indexed field path (e.g. mappings[1].generic); got: %v", errs)
	}
}

// --- ValidationError.Error() format ---

func TestValidationError_ErrorString_MatchesExpectedFormat(t *testing.T) {
	// Contract format: "<file>:<line>: <field>: <message>"
	e := descriptor.ValidationError{
		File:    "harness.yaml",
		Field:   "id",
		Line:    5,
		Message: "required field is empty",
	}

	want := "harness.yaml:5: id: required field is empty"
	got := e.Error()

	if got != want {
		t.Errorf("Error() format:\n  want: %q\n   got: %q", want, got)
	}
}

func TestValidationError_ErrorString_ContainsFile(t *testing.T) {
	e := descriptor.ValidationError{
		File:    "path/to/harness.yaml",
		Field:   "tools.mappings[1].generic",
		Line:    42,
		Message: "duplicate generic tool name",
	}

	got := e.Error()

	if !strings.Contains(got, "path/to/harness.yaml") {
		t.Errorf("Error() should contain the file path, got: %q", got)
	}
}

func TestValidationError_ErrorString_ContainsFieldPath(t *testing.T) {
	e := descriptor.ValidationError{
		File:    "harness.yaml",
		Field:   "tools.mappings[1].generic",
		Line:    42,
		Message: "duplicate generic tool name",
	}

	got := e.Error()

	if !strings.Contains(got, "tools.mappings[1].generic") {
		t.Errorf("Error() should contain the field path, got: %q", got)
	}
}

func TestValidationError_ImplementsErrorInterface(t *testing.T) {
	// ValidationError must satisfy the error interface so it can be used with errors.As.
	var err error = descriptor.ValidationError{
		File:    "harness.yaml",
		Field:   "id",
		Line:    1,
		Message: "required field is empty",
	}

	var ve descriptor.ValidationError
	if !errors.As(err, &ve) {
		t.Error("ValidationError must be unwrappable via errors.As to a ValidationError")
	}
}

// --- Parse: YAML-structural errors ---

func TestParse_UserPathBlock_IsRejectedAsUnknownField(t *testing.T) {
	// A descriptor that declares a user: block inside its paths section must be rejected
	// by the loader. Once the User YAML wire field is removed from the paths struct,
	// the loader's existing unknown-field check produces an error for any descriptor
	// that still carries user: path entries.
	//
	// This test is RED in the TDD phase: the current loader still accepts user: blocks
	// because the User field exists in the wire struct. The test will pass once I1.2
	// removes that field and user: becomes an unknown field.
	src := readValidateFixture(t, "invalid/paths-user-block.yaml")

	_, err := descriptor.Parse(src, "invalid/paths-user-block.yaml")

	if err == nil {
		t.Fatal("expected Parse to return an error for a descriptor with a user: path block; " +
			"user-scope paths are not supported and must be treated as unknown fields")
	}
}

func TestParse_UnknownField_ReturnsError(t *testing.T) {
	// Unknown YAML fields must not be silently ignored; Parse must return an error.
	src := readValidateFixture(t, "invalid/unknown-field.yaml")

	_, err := descriptor.Parse(src, "invalid/unknown-field.yaml")

	if err == nil {
		t.Fatal("expected Parse to return an error for a descriptor with an unknown field, got nil")
	}
}

func TestParse_WrongType_ReturnsError(t *testing.T) {
	// A YAML type mismatch (string where sequence is expected) must return an error.
	src := readValidateFixture(t, "invalid/wrong-type.yaml")

	_, err := descriptor.Parse(src, "invalid/wrong-type.yaml")

	if err == nil {
		t.Fatal("expected Parse to return an error for a descriptor with a type mismatch, got nil")
	}
}

func TestParse_UnknownField_ErrorIsInformative(t *testing.T) {
	// The fixture contains the field "supersecret_undocumented_feature". A useful error
	// message must reference this field name so the author knows what to fix.
	// Asserting merely that the error is non-empty is not sufficient; the stub's
	// "not implemented" message also satisfies that. We require the actual unknown field
	// name to appear in the error.
	src := readValidateFixture(t, "invalid/unknown-field.yaml")

	_, err := descriptor.Parse(src, "invalid/unknown-field.yaml")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("error message must not be empty")
	}
	// The unknown field in invalid/unknown-field.yaml is "supersecret_undocumented_feature".
	// A genuinely informative error includes the field name so the author can fix it.
	if !strings.Contains(msg, "supersecret_undocumented_feature") {
		t.Errorf("error message should contain the unknown field name %q; got: %q",
			"supersecret_undocumented_feature", msg)
	}
}

// --- Load: file-level integration ---

func TestLoad_MissingRequiredField_ReturnsError(t *testing.T) {
	// A descriptor file with a missing required field (id) must cause Load to return an error.
	path := filepath.Join(testdataDir, "invalid/missing-id.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected Load to return an error for a descriptor missing the id field, got nil")
	}
}

func TestLoad_MissingRequiredField_ErrorIsStructuredValidationError(t *testing.T) {
	// Load must return a structured ValidationError, not a plain fmt.Errorf string.
	// The error must name the file (so the user knows which descriptor failed), the field
	// (so they know what to fix), and carry a non-empty message (AC5.2).
	path := filepath.Join(testdataDir, "invalid/missing-id.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve descriptor.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("expected error to unwrap to descriptor.ValidationError via errors.As; got %T: %v", err, err)
		return
	}
	if ve.File == "" {
		t.Error("ValidationError.File must not be empty; it should name the descriptor file that failed")
	}
	if ve.Field == "" {
		t.Error("ValidationError.Field must not be empty; it should name the field that was missing")
	}
	if ve.Message == "" {
		t.Error("ValidationError.Message must not be empty; it should describe what was wrong")
	}
}

func TestLoad_DuplicateToolMappings_ReturnsError(t *testing.T) {
	// A descriptor file with duplicate tool mapping generic names must cause Load to return an error.
	path := filepath.Join(testdataDir, "invalid/duplicate-tool-mappings.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected Load to return an error for a descriptor with duplicate tool mappings, got nil")
	}
}

func TestLoad_DuplicateToolMappings_ErrorIsStructuredValidationError(t *testing.T) {
	// Load must return a structured ValidationError with file, field, and message all populated.
	// A plain fmt.Errorf("invalid descriptor") would pass the existence check but fails here.
	path := filepath.Join(testdataDir, "invalid/duplicate-tool-mappings.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve descriptor.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("expected error to unwrap to descriptor.ValidationError via errors.As; got %T: %v", err, err)
		return
	}
	if ve.File == "" {
		t.Error("ValidationError.File must not be empty; it should name the descriptor file that failed")
	}
	if ve.Field == "" {
		t.Error("ValidationError.Field must not be empty; it should name the field with the duplicate entry")
	}
	if ve.Message == "" {
		t.Error("ValidationError.Message must not be empty; it should describe what was wrong")
	}
}

func TestLoad_WrongType_ReturnsError(t *testing.T) {
	path := filepath.Join(testdataDir, "invalid/wrong-type.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected Load to return an error for a descriptor with a type mismatch, got nil")
	}
}

// --- helper ---

func readValidateFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(testdataDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

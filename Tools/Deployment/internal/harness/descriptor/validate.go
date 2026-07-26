package descriptor

import (
	"fmt"

	"mosaic-deploy/internal/domain"
)

// SupportedSchemaVersions lists every descriptor schema_version value this build understands.
// A descriptor declaring any other value is rejected with ErrUnsupportedSchemaVersion.
var SupportedSchemaVersions = []string{"1"}

// ErrUnsupportedSchemaVersion is returned by Load and Parse when the descriptor's schema_version
// is not in SupportedSchemaVersions.
var ErrUnsupportedSchemaVersion = &unsupportedVersionError{}

// unsupportedVersionError is a distinct sentinel type so that errors.Is matches only this
// exact sentinel and not any plain errors.New with the same text.
type unsupportedVersionError struct{}

func (e *unsupportedVersionError) Error() string {
	return "descriptor schema version not supported"
}

// ValidationError is one located problem found in a harness descriptor. Multiple errors may be
// returned by Validate when a descriptor has several problems.
type ValidationError struct {
	// File is the descriptor's origin path (or the origin argument passed to Parse).
	File string
	// Field is the dotted path to the offending value, e.g. "tools.mappings[3].generic".
	Field string
	// Line is the YAML line number of the offending token, or 0 when not applicable.
	Line int
	// Message is a human-readable description of what was wrong.
	Message string
}

// Error returns a string of the form "<file>:<line>: <field>: <message>".
func (e ValidationError) Error() string {
	return fmt.Sprintf("%s:%d: %s: %s", e.File, e.Line, e.Field, e.Message)
}

// Validate checks the logical constraints of an already-parsed HarnessDescriptor and returns
// one ValidationError per problem found. A nil or empty return means the descriptor is valid.
// Validate does not re-check YAML structure (Parse handles that); it checks semantic rules:
// required fields are present, tool mapping generic values are unique, and so on.
func Validate(d *domain.HarnessDescriptor) []ValidationError {
	var errs []ValidationError

	if d.ID == "" {
		errs = append(errs, ValidationError{
			Field:   "id",
			Message: "required field is empty",
		})
	}

	if d.SchemaVersion == "" {
		errs = append(errs, ValidationError{
			Field:   "schema_version",
			Message: "required field is empty",
		})
	}

	if d.DisplayName == "" {
		errs = append(errs, ValidationError{
			Field:   "display_name",
			Message: "required field is empty",
		})
	}

	// Check for duplicate generic tool names in mappings.
	seen := make(map[string]int) // generic name → first-occurrence index
	for i, m := range d.Tools.Mappings {
		if prev, dup := seen[m.Generic]; dup {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("tools.mappings[%d].generic", i),
				Message: fmt.Sprintf("duplicate generic tool name %q (first seen at mappings[%d])", m.Generic, prev),
			})
		} else {
			seen[m.Generic] = i
		}
	}

	return errs
}

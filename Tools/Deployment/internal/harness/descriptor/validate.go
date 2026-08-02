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

// ValidateToolMappings checks one ordered set of tool mappings against the shared mapping
// and destination rules, independent of where the mappings were declared. It is the single
// definition of "a legal tool-mapping declaration" for the whole program: descriptors validate
// through it, and the config stores validate through it, so a declaration that is legal in a
// descriptor is legal in a config file and vice versa.
//
// fieldPrefix is prepended to every returned ValidationError.Field to locate the mappings
// within the caller's own document. Descriptors pass "tools.mappings", producing paths like
// "tools.mappings[2].destinations[1].field"; the config stores pass
// fmt.Sprintf("tool_destinations.%s", harnessID).
//
// ValidateToolMappings is pure: no I/O, no mutation of the argument. It never sets File or
// Line — the caller owns those. A nil or empty return means the mapping set is valid. Errors
// are returned in mapping order, and within a mapping in destination order.
func ValidateToolMappings(mappings []domain.ToolMapping, fieldPrefix string) []ValidationError {
	// TODO(I2.2): implement — checks R1 through R10 as defined in the design specification.
	return nil
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

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
	var errs []ValidationError
	seen := make(map[string]int) // R1: generic name → first-occurrence mapping index

	for i, m := range mappings {
		// R2: empty generic name.
		if m.Generic == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].generic", fieldPrefix, i),
				Message: "required field is empty",
			})
		} else {
			// R1: duplicate generic name (only meaningful when generic is non-empty).
			if prev, dup := seen[m.Generic]; dup {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].generic", fieldPrefix, i),
					Message: fmt.Sprintf("duplicate generic tool name %q (first seen at [%d])", m.Generic, prev),
				})
			} else {
				seen[m.Generic] = i
			}
		}

		// Destination-level rules: R3–R10.
		seenDests := make(map[string]int) // (kind:field) → first-occurrence destination index

		for j, d := range m.Destinations {
			// R3: unknown destination kind. Skip remaining destination checks when kind is
			// invalid to avoid spurious errors that assume a valid kind.
			if d.Kind != domain.DestMain && d.Kind != domain.DestField {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].to", fieldPrefix, i, j),
					Message: fmt.Sprintf("unknown destination kind %q; valid values: main, field", d.Kind),
				})
				continue
			}

			// R4: DestField without a field name.
			if d.Kind == domain.DestField && d.Field == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].field", fieldPrefix, i, j),
					Message: `destination of kind "field" requires a non-empty field name`,
				})
			}

			// R5: DestMain with a field name.
			if d.Kind == domain.DestMain && d.Field != "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].field", fieldPrefix, i, j),
					Message: `destination of kind "main" must not declare a field name`,
				})
			}

			// R6: unknown value format.
			if d.Format != "" && d.Format != domain.FormatListBlock && d.Format != domain.FormatListFlow && d.Format != domain.FormatScalar {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].format", fieldPrefix, i, j),
					Message: fmt.Sprintf("unknown value format %q; valid values: list-block, list-flow, scalar", d.Format),
				})
			}

			// R7: DestMain with a value format.
			if d.Kind == domain.DestMain && d.Format != "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].format", fieldPrefix, i, j),
					Message: `destination of kind "main" must not declare a value format; the harness's own tools format applies`,
				})
			}

			// R8: separator on a non-scalar format.
			if d.Separator != "" && d.Format != domain.FormatScalar {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].separator", fieldPrefix, i, j),
					Message: `separator is only meaningful for the "scalar" value format`,
				})
			}

			// R9: duplicate (kind, field) pair within one mapping.
			kindField := string(d.Kind) + ":" + d.Field
			if prevJ, dup := seenDests[kindField]; dup {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d]", fieldPrefix, i, j),
					Message: fmt.Sprintf("duplicate destination %q (first seen at destinations[%d])", kindField, prevJ),
				})
			} else {
				seenDests[kindField] = j
			}

			// R10: destination with empty names list inside a non-empty destinations list.
			if len(d.Names) == 0 {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s[%d].destinations[%d].names", fieldPrefix, i, j),
					Message: "destination contributes no names; omit the destination or declare an empty destinations list to mark the generic tool unsupported",
				})
			}
		}
	}
	return errs
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

	// Validate tool mappings (R1–R10 including duplicate generic name check).
	errs = append(errs, ValidateToolMappings(d.Tools.Mappings, "tools.mappings")...)

	// Validate custom_tool_destination entries (mirrors R3–R9, omits R10).
	errs = append(errs, validateCustomToolDestinations(d.Tools.CustomToolDestinations, "tools.custom_tool_destination")...)

	return errs
}

// validateCustomToolDestinations checks the harness-level default custom-tool
// destination list against the structural destination rules. It mirrors the
// destination rules of ValidateToolMappings (R3–R9) and deliberately omits the
// non-empty-names rule (R10), which is meaningless for this field.
//
// It is pure: no I/O, no mutation of the argument. It never sets File or Line —
// the caller owns those. A nil or empty return means the list is valid. Errors are
// returned in declaration order.
func validateCustomToolDestinations(dests []domain.ToolDestination, fieldPrefix string) []ValidationError {
	var errs []ValidationError
	seen := make(map[string]int) // (kind:field) → first-occurrence index

	for i, d := range dests {
		// Unknown destination kind. Skip remaining checks for this entry.
		if d.Kind != domain.DestMain && d.Kind != domain.DestField {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].to", fieldPrefix, i),
				Message: fmt.Sprintf("unknown destination kind %q; valid values: main, field", d.Kind),
			})
			continue
		}

		// to:field requires a non-empty field name.
		if d.Kind == domain.DestField && d.Field == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].field", fieldPrefix, i),
				Message: `destination of kind "field" requires a non-empty field name`,
			})
		}

		// to:main must not declare a field name.
		if d.Kind == domain.DestMain && d.Field != "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].field", fieldPrefix, i),
				Message: `destination of kind "main" must not declare a field name`,
			})
		}

		// Unknown value format.
		if d.Format != "" && d.Format != domain.FormatListBlock && d.Format != domain.FormatListFlow && d.Format != domain.FormatScalar {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].format", fieldPrefix, i),
				Message: fmt.Sprintf("unknown value format %q; valid values: list-block, list-flow, scalar", d.Format),
			})
		}

		// to:main must not declare a value format.
		if d.Kind == domain.DestMain && d.Format != "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].format", fieldPrefix, i),
				Message: `destination of kind "main" must not declare a value format; the harness's own tools format applies`,
			})
		}

		// Separator only meaningful with format:scalar.
		if d.Separator != "" && d.Format != domain.FormatScalar {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].separator", fieldPrefix, i),
				Message: `separator is only meaningful for the "scalar" value format`,
			})
		}

		// Duplicate (kind, field) pair.
		kindField := string(d.Kind) + ":" + d.Field
		if prevI, dup := seen[kindField]; dup {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d]", fieldPrefix, i),
				Message: fmt.Sprintf("duplicate destination %q (first seen at [%d])", kindField, prevI),
			})
		} else {
			seen[kindField] = i
		}
	}
	return errs
}

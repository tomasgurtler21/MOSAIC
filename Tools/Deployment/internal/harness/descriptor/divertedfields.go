package descriptor

// divertedfields.go implements the Stage 2 additions to internal/harness/descriptor:
//
//   DivertedToolField — describes one frontmatter key a harness routes tool entries to,
//   as declared by a ToolMapping destination with Kind == DestField.
//
//   DivertedToolFields — enumerates every distinct DestField destination declared in a
//   descriptor, in first-seen declaration order.
//
//   ExtractDivertedToolEntries — reads the harness-side tool names out of a diverted
//   field value using the field's declared Format and Separator.
//
//   RenderFieldValues — flattens a FieldValue into human-readable strings for reporting
//   a stripped field whose format is unknown.

import (
	"strings"

	"mosaic-deploy/internal/domain"
)

// DivertedToolField describes one frontmatter field a harness routes tool entries to,
// as declared by a ToolMapping destination with Kind == DestField.
type DivertedToolField struct {
	// Key is the frontmatter key, e.g. "mcpServers". Sourced verbatim from the destination's
	// domain.ToolDestination.Field — renamed here only because this struct is field-scoped
	// and "Field.Field" would read poorly.
	Key string
	// Format is the declared rendered shape of the value. Never inferred from the value.
	Format domain.ToolValueFormat
	// Separator joins names when Format == FormatScalar. Never empty in a returned
	// value: DefaultScalarSeparator is substituted for an omitted declaration.
	Separator string
	// byConvention is the set of by-convention tool names from the descriptor's Universe.
	// These are excluded from extracted entries (AD-14), matching ExtractToolEntries behaviour.
	// Populated by DivertedToolFields; zero when the struct is constructed directly.
	byConvention map[string]bool
}

// DivertedToolFields enumerates every distinct DestField destination declared anywhere in
// d.Tools.Mappings, in first-seen declaration order. The main tools key is never included.
//
// When two destinations declare the same Key with different Formats, the first-seen
// declaration wins and the later one is ignored — the deploy-time writer has the same
// first-wins behaviour, so read and write stay symmetric.
//
// Returns an empty slice for a descriptor with no DestField destinations. Pure.
func DivertedToolFields(d *domain.HarnessDescriptor) []DivertedToolField {
	// Build by-convention set from the universe for AD-14 exclusion in extraction.
	byConvention := make(map[string]bool)
	for _, t := range d.Tools.Universe {
		if t.ByConvention {
			byConvention[t.Name] = true
		}
	}

	var result []DivertedToolField
	seen := make(map[string]bool)

	for _, m := range d.Tools.Mappings {
		for _, dest := range m.Destinations {
			if dest.Kind != domain.DestField {
				continue
			}
			if dest.Field == "" || seen[dest.Field] {
				continue
			}
			seen[dest.Field] = true

			format := dest.Format
			if format == "" {
				format = domain.DefaultToolValueFormat
			}
			separator := dest.Separator
			if format == domain.FormatScalar && separator == "" {
				separator = domain.DefaultScalarSeparator
			}

			result = append(result, DivertedToolField{
				Key:          dest.Field,
				Format:       format,
				Separator:    separator,
				byConvention: byConvention,
			})
		}
	}

	return result
}

// ExtractDivertedToolEntries reads the harness-side tool names out of the value stored
// under a diverted field, using f's declared Format and Separator — never by sniffing the
// value's shape.
//
//   FormatListBlock, FormatListFlow: each list item's scalar text is one entry.
//   FormatScalar: the scalar is split on f.Separator; each part is trimmed.
//
// Entries are deduplicated first-seen in document order. By-convention tools (AD-14) are
// excluded, matching ExtractToolEntries. A zero-valued FieldValue yields an empty slice.
// Pure; never returns an error — an unreadable value yields no entries.
func ExtractDivertedToolEntries(f DivertedToolField, value domain.FieldValue) []string {
	seen := make(map[string]bool)
	var result []string

	addEntry := func(name string) {
		if name == "" || f.byConvention[name] || seen[name] {
			return
		}
		seen[name] = true
		result = append(result, name)
	}

	switch f.Format {
	case domain.FormatListBlock, domain.FormatListFlow:
		if value.Kind != domain.KindList {
			return nil
		}
		for _, item := range value.Items {
			if item.Kind == domain.KindScalar {
				addEntry(item.Scalar)
			}
		}
	case domain.FormatScalar:
		if value.Kind != domain.KindScalar {
			return nil
		}
		sep := f.Separator
		if sep == "" {
			sep = domain.DefaultScalarSeparator
		}
		parts := strings.Split(value.Scalar, sep)
		for _, part := range parts {
			addEntry(strings.TrimSpace(part))
		}
	default:
		return nil
	}

	return result
}

// RenderFieldValues flattens a FieldValue into human-readable strings for reporting a
// stripped field whose format is unknown. Scalars yield one entry; lists yield one entry
// per item; mappings yield "key: value" entries. Pure.
func RenderFieldValues(value domain.FieldValue) []string {
	switch value.Kind {
	case domain.KindScalar:
		return []string{value.Scalar}
	case domain.KindList:
		result := make([]string, 0, len(value.Items))
		for _, item := range value.Items {
			if item.Kind == domain.KindScalar {
				result = append(result, item.Scalar)
			}
		}
		return result
	case domain.KindMapping:
		result := make([]string, 0, len(value.Pairs))
		for _, pair := range value.Pairs {
			result = append(result, pair.Key+": "+pair.Value.Scalar)
		}
		return result
	default:
		return nil
	}
}

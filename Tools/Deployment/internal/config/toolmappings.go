package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ToolDestinationsByHarness maps a harness id to the tool-destination mappings declared
// for it in one configuration source.
type ToolDestinationsByHarness map[string][]domain.ToolMapping

// MappingDeclError reports a malformed tool-destination declaration in a config file.
// It names the file, the harness id, the offending mapping and destination, and what was
// wrong, so the user can locate and fix the entry without guesswork.
type MappingDeclError struct {
	File        string // absolute path of the config file, from the store's Path()
	HarnessID   string // harness key the declaration sits under
	MappingIdx  int    // index within that harness's mapping list
	Generic     string // generic tool name, when it could be read; empty when unreadable
	Destination int    // index within the mapping's destinations, or -1 when not applicable
	Message     string // human-readable description of what was wrong
}

// Error returns a single actionable line naming the file, the dotted path, and the
// message. When Destination >= 0 the path is
// tool_destinations.<harness>[<MappingIdx>].destinations[<Destination>]; when
// Destination == -1 it is tool_destinations.<harness>[<MappingIdx>].
func (e MappingDeclError) Error() string {
	var path string
	if e.Destination >= 0 {
		path = fmt.Sprintf("tool_destinations.%s[%d].destinations[%d]", e.HarnessID, e.MappingIdx, e.Destination)
	} else {
		path = fmt.Sprintf("tool_destinations.%s[%d]", e.HarnessID, e.MappingIdx)
	}
	return fmt.Sprintf("%s: %s: %s", e.File, path, e.Message)
}

// ---------------------------------------------------------------------------
// Wire types shared by tool.go and user.go
// ---------------------------------------------------------------------------

// wireToolMapping is the YAML wire form of a tool-destination mapping declaration in a
// config file. Field names and YAML tags are intentionally identical to the descriptor
// package's wireToolMapping so that a declaration can be moved between a descriptor and a
// config file unchanged.
type wireToolMapping struct {
	Generic      string                `yaml:"generic"`
	Destinations []wireToolDestination `yaml:"destinations"`
}

// wireToolDestination is the YAML wire form of one destination within a mapping.
type wireToolDestination struct {
	To        string   `yaml:"to"`
	Field     string   `yaml:"field"`
	Format    string   `yaml:"format"`
	Separator string   `yaml:"separator"`
	Names     []string `yaml:"names"`
}

// ---------------------------------------------------------------------------
// Wire conversion helpers
// ---------------------------------------------------------------------------

// fromWireToolMappings converts wire-form tool mappings to domain types. It performs
// string-to-typed-constant conversion only; no validation and no defaulting are applied,
// so an unknown "to" or "format" value reaches domain as-is and is caught by
// descriptor.ValidateToolMappings. Both directions deep copy Names.
func fromWireToolMappings(ms []wireToolMapping) []domain.ToolMapping {
	if len(ms) == 0 {
		return nil
	}
	result := make([]domain.ToolMapping, len(ms))
	for i, m := range ms {
		var dests []domain.ToolDestination
		if m.Destinations != nil {
			dests = make([]domain.ToolDestination, len(m.Destinations))
			for j, d := range m.Destinations {
				names := make([]string, len(d.Names))
				copy(names, d.Names)
				dests[j] = domain.ToolDestination{
					Kind:      domain.ToolDestinationKind(d.To),
					Field:     d.Field,
					Format:    domain.ToolValueFormat(d.Format),
					Separator: d.Separator,
					Names:     names,
				}
			}
		}
		result[i] = domain.ToolMapping{
			Generic:      m.Generic,
			Destinations: dests,
		}
	}
	return result
}

// toWireToolMappings converts domain tool mappings to wire form for YAML marshalling.
// Both directions deep copy Names.
func toWireToolMappings(ms []domain.ToolMapping) []wireToolMapping {
	if len(ms) == 0 {
		return nil
	}
	result := make([]wireToolMapping, len(ms))
	for i, m := range ms {
		var dests []wireToolDestination
		for _, d := range m.Destinations {
			names := make([]string, len(d.Names))
			copy(names, d.Names)
			dests = append(dests, wireToolDestination{
				To:        string(d.Kind),
				Field:     d.Field,
				Format:    string(d.Format),
				Separator: d.Separator,
				Names:     names,
			})
		}
		result[i] = wireToolMapping{
			Generic:      m.Generic,
			Destinations: dests,
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Validation helper
// ---------------------------------------------------------------------------

// validateAndConvertToolDestinations validates and converts wire-form harness-keyed tool
// mappings. Harnesses are processed in sorted key order for deterministic first-error
// reporting. It returns the converted ToolDestinationsByHarness and, when a declaration
// is malformed, a non-nil *MappingDeclError naming the file and offending entry.
func validateAndConvertToolDestinations(
	file string,
	rawDest map[string][]wireToolMapping,
) (ToolDestinationsByHarness, *MappingDeclError) {
	if len(rawDest) == 0 {
		return nil, nil
	}

	// Sort harness IDs for deterministic first-error ordering.
	harnessIDs := make([]string, 0, len(rawDest))
	for id := range rawDest {
		harnessIDs = append(harnessIDs, id)
	}
	sort.Strings(harnessIDs)

	dest := make(ToolDestinationsByHarness, len(rawDest))
	for _, harnessID := range harnessIDs {
		wireMs := rawDest[harnessID]
		mappings := fromWireToolMappings(wireMs)

		fieldPrefix := fmt.Sprintf("tool_destinations.%s", harnessID)
		valErrs := descriptor.ValidateToolMappings(mappings, fieldPrefix)
		if len(valErrs) > 0 {
			first := valErrs[0]
			mappingIdx, destIdx := parseIndicesFromField(first.Field, fieldPrefix)
			generic := ""
			if mappingIdx >= 0 && mappingIdx < len(mappings) {
				generic = mappings[mappingIdx].Generic
			}
			declErr := MappingDeclError{
				File:        file,
				HarnessID:   harnessID,
				MappingIdx:  mappingIdx,
				Generic:     generic,
				Destination: destIdx,
				Message:     first.Message,
			}
			return nil, &declErr
		}
		dest[harnessID] = mappings
	}
	return dest, nil
}

// parseIndicesFromField extracts the mapping index and destination index from a
// ValidateToolMappings-produced Field path, given the fieldPrefix used for that call.
// Returns -1 for destIdx when the error is mapping-scoped (no .destinations[N] segment).
// Returns -1 for both when the field cannot be parsed.
func parseIndicesFromField(field, fieldPrefix string) (mappingIdx, destIdx int) {
	rest := strings.TrimPrefix(field, fieldPrefix)
	if !strings.HasPrefix(rest, "[") {
		return -1, -1
	}
	rest = rest[1:] // skip "["
	closeBracket := strings.Index(rest, "]")
	if closeBracket < 0 {
		return -1, -1
	}
	idx, err := strconv.Atoi(rest[:closeBracket])
	if err != nil {
		return -1, -1
	}
	mappingIdx = idx
	rest = rest[closeBracket+1:] // skip "]<rest>"

	const destPrefix = ".destinations["
	if !strings.HasPrefix(rest, destPrefix) {
		return mappingIdx, -1
	}
	rest = rest[len(destPrefix):]
	closeBracket = strings.Index(rest, "]")
	if closeBracket < 0 {
		return mappingIdx, -1
	}
	destIdxVal, err := strconv.Atoi(rest[:closeBracket])
	if err != nil {
		return mappingIdx, -1
	}
	return mappingIdx, destIdxVal
}

// ---------------------------------------------------------------------------
// deepCopyMapping
// ---------------------------------------------------------------------------

// deepCopyMapping returns a deep copy of a ToolMapping, ensuring that the result's
// Destinations and Names slices are independent of the input's.
func deepCopyMapping(m domain.ToolMapping) domain.ToolMapping {
	result := domain.ToolMapping{
		Generic: m.Generic,
	}
	if m.Destinations != nil {
		dests := make([]domain.ToolDestination, len(m.Destinations))
		for i, d := range m.Destinations {
			names := make([]string, len(d.Names))
			copy(names, d.Names)
			dests[i] = domain.ToolDestination{
				Kind:      d.Kind,
				Field:     d.Field,
				Format:    d.Format,
				Separator: d.Separator,
				Names:     names,
			}
		}
		result.Destinations = dests
	}
	return result
}

// ---------------------------------------------------------------------------
// MergeToolMappings and EffectiveToolMappings
// ---------------------------------------------------------------------------

// MergeToolMappings computes the effective mapping set for one harness from the three
// declaration sources. Precedence per generic tool, highest first: user, project,
// descriptor. Precedence applies to a generic tool as a whole unit: a higher-precedence
// declaration replaces the lower-precedence destination set entirely and never merges
// into it.
//
// Ordering of the result is deterministic and independent of map iteration:
//  1. Every generic tool present in descriptorMappings, in descriptor order, carrying
//     its winning destination set.
//  2. Then generic tools that appear only in project (not in descriptor), in project
//     declaration order.
//  3. Then generic tools that appear only in user (not in descriptor and not in project),
//     in user declaration order.
//
// MergeToolMappings is pure: it performs no I/O, mutates none of its arguments, and deep
// copies every destination it places in the result. Passing nil for any argument is
// equivalent to passing an empty slice.
func MergeToolMappings(descriptorMappings, project, user []domain.ToolMapping) []domain.ToolMapping {
	// Build lookup maps for project and user declarations.
	projMap := make(map[string]domain.ToolMapping, len(project))
	for _, m := range project {
		projMap[m.Generic] = m
	}
	userMap := make(map[string]domain.ToolMapping, len(user))
	for _, m := range user {
		userMap[m.Generic] = m
	}

	seen := make(map[string]bool)
	var result []domain.ToolMapping

	// 1. Descriptor tools, in descriptor order, with winning destination set.
	// User beats project beats descriptor for the same generic tool.
	for _, dm := range descriptorMappings {
		seen[dm.Generic] = true
		winning := dm
		if pm, ok := projMap[dm.Generic]; ok {
			winning = pm
		}
		if um, ok := userMap[dm.Generic]; ok {
			winning = um
		}
		result = append(result, deepCopyMapping(winning))
	}

	// 2. Project-only tools (not in descriptor), in project declaration order.
	// A tool in both project and user (but not descriptor) appears here with user's
	// winning destinations.
	for _, pm := range project {
		if seen[pm.Generic] {
			continue
		}
		seen[pm.Generic] = true
		winning := pm
		if um, ok := userMap[pm.Generic]; ok {
			winning = um
		}
		result = append(result, deepCopyMapping(winning))
	}

	// 3. User-only tools (not in descriptor and not in project), in user declaration order.
	for _, um := range user {
		if seen[um.Generic] {
			continue
		}
		seen[um.Generic] = true
		result = append(result, deepCopyMapping(um))
	}

	return result
}

// EffectiveToolMappings is the per-harness convenience wrapper used at the composition
// point. It is equivalent to MergeToolMappings(descriptorMappings, project[harnessID],
// user[harnessID]).
func EffectiveToolMappings(
	harnessID string,
	descriptorMappings []domain.ToolMapping,
	project, user ToolDestinationsByHarness,
) []domain.ToolMapping {
	return MergeToolMappings(descriptorMappings, project[harnessID], user[harnessID])
}

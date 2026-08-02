package config

import (
	"mosaic-deploy/internal/domain"
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
	return "" // not implemented: returns empty string so tests that check content fail (RED)
}

// MergeToolMappings computes the effective mapping set for one harness from the three
// declaration sources. Precedence per generic tool, highest first: user, project,
// descriptor. Precedence applies to a generic tool as a whole unit: a higher-precedence
// declaration replaces the lower-precedence destination set entirely and never merges
// into it.
//
// Ordering of the result is deterministic and independent of map iteration:
//  1. Every generic tool present in descriptorMappings, in descriptor order, carrying
//     its winning destination set.
//  2. Then generic tools that appear only in project, in project declaration order.
//  3. Then generic tools that appear only in user, in user declaration order.
//
// MergeToolMappings is pure: it performs no I/O, mutates none of its arguments, and deep
// copies every destination it places in the result. Passing nil for any argument is
// equivalent to passing an empty slice.
func MergeToolMappings(descriptorMappings, project, user []domain.ToolMapping) []domain.ToolMapping {
	return nil // not implemented: returns nil so behavioral tests fail (RED)
}

// EffectiveToolMappings is the per-harness convenience wrapper used at the composition
// point. It is equivalent to MergeToolMappings(descriptorMappings, project[harnessID],
// user[harnessID]).
func EffectiveToolMappings(
	harnessID string,
	descriptorMappings []domain.ToolMapping,
	project, user ToolDestinationsByHarness,
) []domain.ToolMapping {
	return nil // not implemented: returns nil so behavioral tests fail (RED)
}

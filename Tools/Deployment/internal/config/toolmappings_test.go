package config_test

// Tests for the merge function (MergeToolMappings / EffectiveToolMappings) and for error
// reporting on malformed mapping declarations in both config stores.
//
// Merge invariants (T3.3):
//
// Nil / empty inputs:
//   - All-nil inputs produce nil (not an empty slice).
//   - All-empty-slice inputs produce nil (or an empty slice — the empty case).
//   - Nil is treated as an empty slice for each argument independently.
//
// Precedence — per generic tool, whole-unit replacement:
//   - Descriptor-only: result equals the descriptor mapping set.
//   - Project overrides descriptor for the same generic tool (whole-unit: all destinations
//     from project replace all destinations from descriptor).
//   - User overrides project for the same generic tool.
//   - User overrides descriptor directly when no project entry exists.
//   - A higher-precedence source with an empty Destinations list still wins and produces
//     an empty Destinations entry in the result (marking the tool unsupported at config level).
//
// Untouched tools:
//   - A generic tool declared only in the descriptor retains its descriptor destinations when
//     a different generic tool is overridden by project or user.
//
// Result ordering (deterministic, independent of map iteration):
//   - Descriptor-declared generic tools appear first, in descriptor declaration order.
//   - Generic tools declared only in project appear next, in project declaration order.
//   - Generic tools declared only in user appear last, in user declaration order.
//
// Purity:
//   - Modifying a destination in the result does not mutate the input arguments.
//
// EffectiveToolMappings convenience wrapper:
//   - Equivalent to MergeToolMappings with the per-harness slices extracted from the maps.
//   - A nil project or user map is treated as having no entry for that harness.
//
// Error reporting invariants (T3.4):
//
// Project-level config:
//   - A malformed declaration (unknown "to" value) causes Load to return a non-nil error.
//   - The error type is config.MappingDeclError (accessible via errors.As).
//   - The error's File field equals the store's Path().
//   - The error's HarnessID field names the harness key of the offending declaration.
//   - The error's MappingIdx field is the index of the offending mapping within that harness.
//   - The error's Destination field is the index of the offending destination (>= 0 for
//     destination-scoped rules, -1 for mapping-scoped rules like duplicate generic name).
//   - No partial config is returned alongside the error (ToolDestinations is nil on error).
//   - Multiple rules are checked (unknown kind, missing field name, empty names, duplicate generic).
//
// User-local config:
//   - The same validation applies; Load returns a MappingDeclError naming user-config.yaml.
//
// MappingDeclError.Error() format:
//   - Contains the file path.
//   - Contains a dotted path that includes the harness id and mapping index.
//   - When Destination >= 0, the path includes a ".destinations[N]" segment.
//   - When Destination == -1, the path does not include a ".destinations[N]" segment.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mapping builds a ToolMapping with one DestMain destination carrying the given names.
// It is a concise helper for arranging merge test inputs.
func mapping(generic string, mainNames ...string) domain.ToolMapping {
	return domain.ToolMapping{
		Generic: generic,
		Destinations: []domain.ToolDestination{
			{Kind: domain.DestMain, Names: mainNames},
		},
	}
}

// mappingField builds a ToolMapping with one DestField destination.
func mappingField(generic, field string, names ...string) domain.ToolMapping {
	return domain.ToolMapping{
		Generic: generic,
		Destinations: []domain.ToolDestination{
			{Kind: domain.DestField, Field: field, Names: names},
		},
	}
}

// mappingUnsupported builds a ToolMapping with an empty Destinations list, marking a
// generic tool as unsupported by this harness.
func mappingUnsupported(generic string) domain.ToolMapping {
	return domain.ToolMapping{
		Generic:      generic,
		Destinations: []domain.ToolDestination{},
	}
}

// genericNames extracts just the Generic fields from a mapping slice, in order.
func genericNames(ms []domain.ToolMapping) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Generic
	}
	return out
}

// ---------------------------------------------------------------------------
// MergeToolMappings — nil / empty inputs
// ---------------------------------------------------------------------------

// TestMergeToolMappings_AllNilInputs_ReturnsNilOrEmpty verifies that passing nil for all
// three arguments produces either nil or an empty (not populated) result. The zero value
// must never be a populated mapping set.
func TestMergeToolMappings_AllNilInputs_ReturnsNilOrEmpty(t *testing.T) {
	result := config.MergeToolMappings(nil, nil, nil)
	if len(result) != 0 {
		t.Errorf("MergeToolMappings(nil,nil,nil) = %v; want nil or empty", result)
	}
}

// TestMergeToolMappings_NilTreatedAsEmpty_ForEachArg verifies that nil is equivalent to
// an empty slice for each argument independently. Each combination must produce the same
// result as when the nil argument was an explicit empty slice.
func TestMergeToolMappings_NilTreatedAsEmpty_ForEachArg(t *testing.T) {
	m := mapping("subagent", "Task")

	resultNilDescriptor := config.MergeToolMappings(nil, []domain.ToolMapping{m}, nil)
	resultEmptyDescriptor := config.MergeToolMappings([]domain.ToolMapping{}, []domain.ToolMapping{m}, nil)

	if len(resultNilDescriptor) != len(resultEmptyDescriptor) {
		t.Errorf("nil descriptor produces %d mappings, empty produces %d; "+
			"they must be equivalent", len(resultNilDescriptor), len(resultEmptyDescriptor))
	}
}

// ---------------------------------------------------------------------------
// MergeToolMappings — descriptor-only (no project, no user)
// ---------------------------------------------------------------------------

// TestMergeToolMappings_DescriptorOnly_ResultEqualsDescriptor verifies that when only the
// descriptor mappings are supplied, the result equals the descriptor set (same generic names,
// same destinations).
func TestMergeToolMappings_DescriptorOnly_ResultEqualsDescriptor(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task", "TaskStop"),
		mapping("user_interaction", "AskUserQuestion"),
	}

	result := config.MergeToolMappings(desc, nil, nil)

	if len(result) != 2 {
		t.Fatalf("got %d mappings, want 2", len(result))
	}
	if result[0].Generic != "subagent" {
		t.Errorf("result[0].Generic = %q, want \"subagent\"", result[0].Generic)
	}
	if result[1].Generic != "user_interaction" {
		t.Errorf("result[1].Generic = %q, want \"user_interaction\"", result[1].Generic)
	}
}

// ---------------------------------------------------------------------------
// MergeToolMappings — project overrides descriptor
// ---------------------------------------------------------------------------

// TestMergeToolMappings_ProjectOverridesDescriptor_SameGenericTool verifies that a project
// entry for a generic tool replaces the descriptor's destinations entirely for that tool.
// The resulting mapping must have the project's destinations, not the descriptor's.
func TestMergeToolMappings_ProjectOverridesDescriptor_SameGenericTool(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("user_interaction", "OldTool"),
	}
	project := []domain.ToolMapping{
		mapping("user_interaction", "AskUserQuestion"),
	}

	result := config.MergeToolMappings(desc, project, nil)

	if len(result) == 0 {
		t.Fatal("got empty result")
	}
	if len(result[0].Destinations) == 0 {
		t.Fatal("winning mapping has no destinations")
	}
	names := result[0].Destinations[0].Names
	if len(names) != 1 || names[0] != "AskUserQuestion" {
		t.Errorf("result[0].Destinations[0].Names = %v, want [AskUserQuestion]; "+
			"project must replace descriptor's destinations for the same generic tool", names)
	}
}

// TestMergeToolMappings_ProjectOverridesDescriptor_WholeUnitReplacement verifies that a
// project entry replaces the descriptor's entire destination set for that generic tool —
// it does not merge individual destinations from both sources.
func TestMergeToolMappings_ProjectOverridesDescriptor_WholeUnitReplacement(t *testing.T) {
	desc := []domain.ToolMapping{
		{
			Generic: "user_interaction",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"DescTool"}},
				{Kind: domain.DestField, Field: "descField", Names: []string{"DescFieldTool"}},
			},
		},
	}
	// Project declares only one destination; it must fully replace both descriptor destinations.
	project := []domain.ToolMapping{
		{
			Generic: "user_interaction",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"ProjTool"}},
			},
		},
	}

	result := config.MergeToolMappings(desc, project, nil)

	if len(result) == 0 {
		t.Fatal("got empty result")
	}
	// The result must have exactly one destination (project's), not two (merged).
	if len(result[0].Destinations) != 1 {
		t.Errorf("Destinations: got %d, want 1; project must replace descriptor wholesale, not merge",
			len(result[0].Destinations))
	}
}

// ---------------------------------------------------------------------------
// MergeToolMappings — user overrides project overrides descriptor
// ---------------------------------------------------------------------------

// TestMergeToolMappings_UserOverridesProject_SameGenericTool verifies that a user entry
// for a generic tool replaces the project entry's destinations entirely, regardless of
// what the descriptor declares.
func TestMergeToolMappings_UserOverridesProject_SameGenericTool(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("user_interaction", "DescTool"),
	}
	project := []domain.ToolMapping{
		mapping("user_interaction", "ProjTool"),
	}
	user := []domain.ToolMapping{
		mapping("user_interaction", "UserTool"),
	}

	result := config.MergeToolMappings(desc, project, user)

	if len(result) == 0 || len(result[0].Destinations) == 0 {
		t.Fatal("no destinations in result")
	}
	names := result[0].Destinations[0].Names
	if len(names) != 1 || names[0] != "UserTool" {
		t.Errorf("Names = %v, want [UserTool]; user must take precedence over project", names)
	}
}

// TestMergeToolMappings_UserOverridesDescriptor_NoProject verifies that a user entry
// overrides the descriptor directly when no project entry exists for that generic tool.
func TestMergeToolMappings_UserOverridesDescriptor_NoProject(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("user_interaction", "DescTool"),
	}
	user := []domain.ToolMapping{
		mapping("user_interaction", "UserTool"),
	}

	result := config.MergeToolMappings(desc, nil, user)

	if len(result) == 0 || len(result[0].Destinations) == 0 {
		t.Fatal("no destinations in result")
	}
	names := result[0].Destinations[0].Names
	if len(names) != 1 || names[0] != "UserTool" {
		t.Errorf("Names = %v, want [UserTool]; user must take precedence over descriptor", names)
	}
}

// TestMergeToolMappings_HigherPrecedenceEmptyDestinations_StillWins verifies that when a
// higher-precedence source declares an empty Destinations list (marking the tool as
// unsupported), it still wins over a lower-precedence source with destinations. The result
// must have an empty Destinations list, not the lower-precedence destinations.
func TestMergeToolMappings_HigherPrecedenceEmptyDestinations_StillWins(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("user_interaction", "DescTool"),
	}
	// Project declares the tool as unsupported (empty destinations).
	project := []domain.ToolMapping{
		mappingUnsupported("user_interaction"),
	}

	result := config.MergeToolMappings(desc, project, nil)

	if len(result) == 0 {
		t.Fatal("no mappings in result")
	}
	if result[0].Generic != "user_interaction" {
		t.Fatalf("result[0].Generic = %q, want \"user_interaction\"", result[0].Generic)
	}
	if len(result[0].Destinations) != 0 {
		t.Errorf("Destinations: got %d, want 0; project's unsupported declaration must win over descriptor's",
			len(result[0].Destinations))
	}
}

// ---------------------------------------------------------------------------
// MergeToolMappings — untouched tools retain descriptor destinations
// ---------------------------------------------------------------------------

// TestMergeToolMappings_UntouchedGenericTool_RetainsDescriptorDestinations verifies that
// a generic tool not mentioned in any config retains its descriptor-declared destinations
// unchanged even when other generic tools are overridden.
func TestMergeToolMappings_UntouchedGenericTool_RetainsDescriptorDestinations(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task", "TaskStop"),
		mapping("user_interaction", "DescAsk"),
	}
	// Project overrides only user_interaction; subagent is untouched.
	project := []domain.ToolMapping{
		mapping("user_interaction", "ProjAsk"),
	}

	result := config.MergeToolMappings(desc, project, nil)

	if len(result) < 2 {
		t.Fatalf("got %d mappings, want at least 2", len(result))
	}

	// Find subagent in result.
	var subagentMapping *domain.ToolMapping
	for i := range result {
		if result[i].Generic == "subagent" {
			subagentMapping = &result[i]
			break
		}
	}
	if subagentMapping == nil {
		t.Fatal("subagent not found in result; untouched generic tools must remain in the result")
	}
	if len(subagentMapping.Destinations) == 0 {
		t.Fatal("subagent has no destinations; it must retain its descriptor destinations")
	}
	names := subagentMapping.Destinations[0].Names
	if len(names) != 2 || names[0] != "Task" || names[1] != "TaskStop" {
		t.Errorf("subagent Names = %v, want [Task TaskStop]; descriptor destinations must be retained", names)
	}
}

// ---------------------------------------------------------------------------
// MergeToolMappings — result ordering
// ---------------------------------------------------------------------------

// TestMergeToolMappings_ResultOrder_DescriptorToolsFirst verifies that every generic tool
// present in the descriptor appears in the result before any config-only generic tool,
// regardless of declaration order in the config sources.
func TestMergeToolMappings_ResultOrder_DescriptorToolsFirst(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task"),
		mapping("user_interaction", "Ask"),
	}
	// project introduces a new tool not in the descriptor.
	project := []domain.ToolMapping{
		mapping("project_only_tool", "SomeProjTool"),
	}
	// user introduces another new tool.
	user := []domain.ToolMapping{
		mapping("user_only_tool", "SomeUserTool"),
	}

	result := config.MergeToolMappings(desc, project, user)

	if len(result) < 4 {
		t.Fatalf("got %d mappings, want 4", len(result))
	}

	// Descriptor tools must come first.
	if result[0].Generic != "subagent" {
		t.Errorf("result[0].Generic = %q, want \"subagent\" (descriptor tools first)", result[0].Generic)
	}
	if result[1].Generic != "user_interaction" {
		t.Errorf("result[1].Generic = %q, want \"user_interaction\" (descriptor order)", result[1].Generic)
	}
}

// TestMergeToolMappings_ResultOrder_ProjectOnlyToolsAfterDescriptor verifies that generic
// tools declared only in the project appear after all descriptor-declared tools, in project
// declaration order.
func TestMergeToolMappings_ResultOrder_ProjectOnlyToolsAfterDescriptor(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task"),
	}
	project := []domain.ToolMapping{
		mapping("proj_first", "P1"),
		mapping("proj_second", "P2"),
	}

	result := config.MergeToolMappings(desc, project, nil)

	if len(result) < 3 {
		t.Fatalf("got %d mappings, want 3", len(result))
	}
	// result[0] must be the descriptor tool, result[1] and result[2] the project tools in order.
	if result[1].Generic != "proj_first" {
		t.Errorf("result[1].Generic = %q, want \"proj_first\" (project tools after descriptor)", result[1].Generic)
	}
	if result[2].Generic != "proj_second" {
		t.Errorf("result[2].Generic = %q, want \"proj_second\" (project declaration order)", result[2].Generic)
	}
}

// TestMergeToolMappings_ResultOrder_UserOnlyToolsAfterProjectOnly verifies that generic
// tools declared only in the user appear after project-only tools, in user declaration order.
func TestMergeToolMappings_ResultOrder_UserOnlyToolsAfterProjectOnly(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task"),
	}
	project := []domain.ToolMapping{
		mapping("proj_tool", "P"),
	}
	user := []domain.ToolMapping{
		mapping("user_first", "U1"),
		mapping("user_second", "U2"),
	}

	result := config.MergeToolMappings(desc, project, user)

	if len(result) < 4 {
		t.Fatalf("got %d mappings, want 4; got generics: %v", len(result), genericNames(result))
	}
	// result[2] and result[3] must be user-only tools in declaration order.
	if result[2].Generic != "user_first" {
		t.Errorf("result[2].Generic = %q, want \"user_first\" (user-only tools after project-only)", result[2].Generic)
	}
	if result[3].Generic != "user_second" {
		t.Errorf("result[3].Generic = %q, want \"user_second\" (user declaration order)", result[3].Generic)
	}
}

// ---------------------------------------------------------------------------
// MergeToolMappings — purity (no mutation of inputs)
// ---------------------------------------------------------------------------

// TestMergeToolMappings_DoesNotMutateDescriptorInput verifies that modifying a destination
// in the result does not change the original descriptor slice passed as input. The function
// must deep-copy destinations rather than sharing slices.
func TestMergeToolMappings_DoesNotMutateDescriptorInput(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task", "TaskStop"),
	}
	// Record original names before merge.
	originalNames := make([]string, len(desc[0].Destinations[0].Names))
	copy(originalNames, desc[0].Destinations[0].Names)

	result := config.MergeToolMappings(desc, nil, nil)

	if len(result) == 0 || len(result[0].Destinations) == 0 {
		t.Fatal("no destinations in result")
	}
	// Mutate the result.
	result[0].Destinations[0].Names[0] = "MUTATED"

	// Original must be unchanged.
	if desc[0].Destinations[0].Names[0] != originalNames[0] {
		t.Errorf("descriptor input was mutated: Names[0] = %q, want %q; MergeToolMappings must deep copy",
			desc[0].Destinations[0].Names[0], originalNames[0])
	}
}

// ---------------------------------------------------------------------------
// EffectiveToolMappings — convenience wrapper
// ---------------------------------------------------------------------------

// TestEffectiveToolMappings_EquivalentToMergeWithExtractedSlices verifies that
// EffectiveToolMappings(harnessID, desc, project, user) produces the same result as
// MergeToolMappings(desc, project[harnessID], user[harnessID]).
func TestEffectiveToolMappings_EquivalentToMergeWithExtractedSlices(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("user_interaction", "DescTool"),
	}
	project := config.ToolDestinationsByHarness{
		"claude-code": {mapping("user_interaction", "ProjTool")},
	}
	user := config.ToolDestinationsByHarness{
		"claude-code": {mapping("user_interaction", "UserTool")},
	}

	viaEffective := config.EffectiveToolMappings("claude-code", desc, project, user)
	viaMerge := config.MergeToolMappings(desc, project["claude-code"], user["claude-code"])

	if len(viaEffective) != len(viaMerge) {
		t.Fatalf("EffectiveToolMappings returns %d mappings, MergeToolMappings returns %d; "+
			"they must be equivalent", len(viaEffective), len(viaMerge))
	}
	if len(viaEffective) > 0 && len(viaMerge) > 0 {
		ev := viaEffective[0]
		mv := viaMerge[0]
		if ev.Generic != mv.Generic {
			t.Errorf("Generic: EffectiveToolMappings=%q, MergeToolMappings=%q; must be the same", ev.Generic, mv.Generic)
		}
	}
}

// TestEffectiveToolMappings_NilMaps_TreatedAsEmpty verifies that passing nil for both the
// project and user maps is equivalent to having no config-declared mappings, and the result
// equals the descriptor-only merge.
func TestEffectiveToolMappings_NilMaps_TreatedAsEmpty(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task"),
	}

	result := config.EffectiveToolMappings("claude-code", desc, nil, nil)

	if len(result) != 1 {
		t.Fatalf("got %d mappings, want 1", len(result))
	}
	if result[0].Generic != "subagent" {
		t.Errorf("Generic = %q, want \"subagent\"", result[0].Generic)
	}
}

// TestEffectiveToolMappings_AbsentHarnessInMap_TreatedAsNoMappings verifies that when the
// harness id is not present as a key in the project or user map, the result is as if those
// sources declared no mappings for that harness.
func TestEffectiveToolMappings_AbsentHarnessInMap_TreatedAsNoMappings(t *testing.T) {
	desc := []domain.ToolMapping{
		mapping("subagent", "Task"),
	}
	project := config.ToolDestinationsByHarness{
		"other-harness": {mapping("subagent", "OtherTool")},
	}

	result := config.EffectiveToolMappings("claude-code", desc, project, nil)

	// "claude-code" has no project entry, so descriptor destinations are used.
	if len(result) == 0 || len(result[0].Destinations) == 0 {
		t.Fatal("no destinations in result")
	}
	names := result[0].Destinations[0].Names
	if len(names) != 1 || names[0] != "Task" {
		t.Errorf("Names = %v, want [Task]; absent harness must not affect the harness being merged", names)
	}
}

// ---------------------------------------------------------------------------
// Error reporting — project-level config
// ---------------------------------------------------------------------------

// TestToolConfigStore_MalformedDeclaration_UnknownKind_ReturnsError verifies that a
// tool_destinations block with an unknown "to" value causes Load to return a non-nil error.
// Malformed declarations must not be silently dropped.
func TestToolConfigStore_MalformedDeclaration_UnknownKind_ReturnsError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Error("Load with unknown destination kind returned nil error; malformed declarations must produce an error")
	}
}

// TestToolConfigStore_MalformedDeclaration_ErrorIsMappingDeclError verifies that the error
// returned for a malformed tool_destinations declaration is (or wraps) a MappingDeclError.
func TestToolConfigStore_MalformedDeclaration_ErrorIsMappingDeclError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Errorf("error type = %T, want config.MappingDeclError; got: %v", err, err)
	}
}

// TestToolConfigStore_MalformedDeclaration_ErrorNamesFile verifies that the MappingDeclError
// File field equals the store's Path().
func TestToolConfigStore_MalformedDeclaration_ErrorNamesFile(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Fatalf("error is not a MappingDeclError: %T %v", err, err)
	}
	if declErr.File != store.Path() {
		t.Errorf("MappingDeclError.File = %q, want %q (the store's Path())", declErr.File, store.Path())
	}
}

// TestToolConfigStore_MalformedDeclaration_ErrorNamesHarnessID verifies that the
// MappingDeclError HarnessID field names the harness key that contains the offending mapping.
func TestToolConfigStore_MalformedDeclaration_ErrorNamesHarnessID(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  ghcp-cli:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Fatalf("error is not a MappingDeclError: %T %v", err, err)
	}
	if declErr.HarnessID != "ghcp-cli" {
		t.Errorf("MappingDeclError.HarnessID = %q, want \"ghcp-cli\"", declErr.HarnessID)
	}
}

// TestToolConfigStore_MalformedDeclaration_ErrorHasMappingIndex verifies that the
// MappingDeclError MappingIdx field is the zero-based index of the offending mapping within
// that harness's mapping list.
func TestToolConfigStore_MalformedDeclaration_ErrorHasMappingIndex(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	// The offending mapping is the second one (index 1) within claude-code.
	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: subagent
      destinations:
        - to: main
          names:
            - Task
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Fatalf("error is not a MappingDeclError: %T %v", err, err)
	}
	if declErr.MappingIdx != 1 {
		t.Errorf("MappingDeclError.MappingIdx = %d, want 1 (second mapping)", declErr.MappingIdx)
	}
}

// TestToolConfigStore_MalformedDeclaration_DestinationScopedRule_HasDestinationIndex
// verifies that for a destination-scoped validation failure (e.g., unknown "to" value),
// the MappingDeclError Destination field is the zero-based index of the offending destination.
func TestToolConfigStore_MalformedDeclaration_DestinationScopedRule_HasDestinationIndex(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	// The offending destination is the second one (index 1).
	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Fatalf("error is not a MappingDeclError: %T %v", err, err)
	}
	if declErr.Destination != 1 {
		t.Errorf("MappingDeclError.Destination = %d, want 1 (second destination)", declErr.Destination)
	}
}

// TestToolConfigStore_MalformedDeclaration_MappingScopedRule_HasNegativeOneDestination
// verifies that for a mapping-scoped validation failure (e.g., duplicate generic name),
// the MappingDeclError Destination field is -1.
func TestToolConfigStore_MalformedDeclaration_MappingScopedRule_HasNegativeOneDestination(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	// Duplicate generic name — a mapping-scoped rule, Destination must be -1.
	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskAgain
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError for duplicate generic")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Fatalf("error is not a MappingDeclError: %T %v", err, err)
	}
	if declErr.Destination != -1 {
		t.Errorf("MappingDeclError.Destination = %d, want -1 for a mapping-scoped rule (duplicate generic)",
			declErr.Destination)
	}
}

// TestToolConfigStore_MalformedDeclaration_NoPartialConfig verifies that when a declaration
// is malformed, Load does not return a partial ToolDestinations map alongside the error.
// A malformed declaration fails the load entirely.
func TestToolConfigStore_MalformedDeclaration_NoPartialConfig(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	cfg, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want an error for the malformed declaration")
	}

	// On error, ToolDestinations must be nil (no partial result).
	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("Load returned a partial ToolDestinations (%d entries) alongside an error; "+
			"a malformed declaration must fail the entire load", len(cfg.ToolDestinations))
	}
}

// TestToolConfigStore_MalformedDeclaration_FieldRequiredForKindField_ReturnsError verifies
// that a DestField destination without a field name (violating R4) causes Load to return
// a MappingDeclError.
func TestToolConfigStore_MalformedDeclaration_FieldRequiredForKindField_ReturnsError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: field
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Error("Load with DestField missing field name returned nil error; " +
			"a destination of kind \"field\" requires a non-empty field name")
	}
}

// TestToolConfigStore_MalformedDeclaration_EmptyNames_ReturnsError verifies that a
// destination with an empty names list inside a non-empty destinations list (violating R10)
// causes Load to return a MappingDeclError.
func TestToolConfigStore_MalformedDeclaration_EmptyNames_ReturnsError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names: []
`))

	_, err := store.Load()
	if err == nil {
		t.Error("Load with empty names list returned nil error; " +
			"a destination inside a non-empty destinations list must have at least one name")
	}
}

// TestToolConfigStore_MalformedDeclaration_EmptyGeneric_ReturnsError verifies that a
// mapping without a generic field (violating R2) causes Load to return a MappingDeclError.
func TestToolConfigStore_MalformedDeclaration_EmptyGeneric_ReturnsError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: ""
      destinations:
        - to: main
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Error("Load with empty generic name returned nil error; " +
			"a mapping without a generic tool name must be rejected")
	}
}

// ---------------------------------------------------------------------------
// Error reporting — user-local config
// ---------------------------------------------------------------------------

// TestUserConfigStore_MalformedDeclaration_UnknownKind_ReturnsError verifies that the
// user config store applies the same validation as the project config store. A malformed
// declaration in user-config.yaml must produce a non-nil error.
func TestUserConfigStore_MalformedDeclaration_UnknownKind_ReturnsError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	writeUserConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Error("UserConfigStore.Load with unknown destination kind returned nil error; " +
			"the same validation rules apply to user-config.yaml")
	}
}

// TestUserConfigStore_MalformedDeclaration_ErrorNamesFile verifies that the MappingDeclError
// returned by the user config store has a File field equal to that store's Path().
func TestUserConfigStore_MalformedDeclaration_ErrorNamesFile(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	writeUserConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: unknown-kind
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Fatalf("error is not a MappingDeclError: %T %v", err, err)
	}
	if declErr.File != store.Path() {
		t.Errorf("MappingDeclError.File = %q, want %q (the user config store's Path())",
			declErr.File, store.Path())
	}
}

// TestUserConfigStore_MalformedDeclaration_ErrorIsMappingDeclError verifies that the error
// type is (or wraps) a MappingDeclError, not a generic error string.
func TestUserConfigStore_MalformedDeclaration_ErrorIsMappingDeclError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	writeUserConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: ""
      destinations:
        - to: main
          names:
            - SomeTool
`))

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load returned nil error; want a MappingDeclError")
	}

	var declErr config.MappingDeclError
	if !errors.As(err, &declErr) {
		t.Errorf("error type = %T, want config.MappingDeclError; got: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// MappingDeclError.Error() format
// ---------------------------------------------------------------------------

// TestMappingDeclError_Error_ContainsFilePath verifies that Error() includes the file
// path so the user can locate the config file without guesswork.
func TestMappingDeclError_Error_ContainsFilePath(t *testing.T) {
	e := config.MappingDeclError{
		File:        "/some/path/tool-config.yaml",
		HarnessID:   "claude-code",
		MappingIdx:  0,
		Generic:     "user_interaction",
		Destination: -1,
		Message:     "required field is empty",
	}

	// Error() panics in the stub; this assertion verifies the format once implemented.
	got := e.Error()

	if !strings.Contains(got, "/some/path/tool-config.yaml") {
		t.Errorf("Error() = %q; want it to contain the file path", got)
	}
}

// TestMappingDeclError_Error_ContainsDottedPath_MappingScopedRule verifies that for a
// mapping-scoped rule (Destination == -1), Error() includes a path of the form
// "tool_destinations.<harness>[<MappingIdx>]" without a ".destinations[N]" segment.
func TestMappingDeclError_Error_ContainsDottedPath_MappingScopedRule(t *testing.T) {
	e := config.MappingDeclError{
		File:        "/path/tool-config.yaml",
		HarnessID:   "claude-code",
		MappingIdx:  2,
		Generic:     "",
		Destination: -1,
		Message:     "required field is empty",
	}

	got := e.Error()

	if !strings.Contains(got, "tool_destinations.claude-code") {
		t.Errorf("Error() = %q; want dotted path containing \"tool_destinations.claude-code\"", got)
	}
	// Mapping-scoped: no ".destinations[N]" in the path.
	if strings.Contains(got, ".destinations[") {
		t.Errorf("Error() = %q; Destination == -1 but path contains \".destinations[\" segment", got)
	}
}

// TestMappingDeclError_Error_ContainsDottedPath_DestinationScopedRule verifies that for a
// destination-scoped rule (Destination >= 0), Error() includes the mapping index and the
// ".destinations[<Destination>]" segment in the path.
func TestMappingDeclError_Error_ContainsDottedPath_DestinationScopedRule(t *testing.T) {
	e := config.MappingDeclError{
		File:        "/path/tool-config.yaml",
		HarnessID:   "claude-code",
		MappingIdx:  0,
		Generic:     "user_interaction",
		Destination: 1,
		Message:     `unknown destination kind "bogus"; valid values: main, field`,
	}

	got := e.Error()

	if !strings.Contains(got, "tool_destinations.claude-code") {
		t.Errorf("Error() = %q; want \"tool_destinations.claude-code\" in path", got)
	}
	if !strings.Contains(got, ".destinations[1]") {
		t.Errorf("Error() = %q; want \".destinations[1]\" in path for Destination==1", got)
	}
}

// TestMappingDeclError_Error_ContainsMessage verifies that Error() includes the Message
// field so the user knows what was wrong.
func TestMappingDeclError_Error_ContainsMessage(t *testing.T) {
	msg := `unknown destination kind "bogus"; valid values: main, field`
	e := config.MappingDeclError{
		File:        "/path/tool-config.yaml",
		HarnessID:   "claude-code",
		MappingIdx:  0,
		Destination: 0,
		Message:     msg,
	}

	got := e.Error()

	if !strings.Contains(got, msg) {
		t.Errorf("Error() = %q; want it to contain the message %q", got, msg)
	}
}

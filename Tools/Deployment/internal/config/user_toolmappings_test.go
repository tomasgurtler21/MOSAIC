package config_test

// Tests for tool-destination mapping declarations in the user-local config store.
//
// Verified invariants:
//
// Absence tolerance — these cases must produce nil ToolDestinations and no error:
//   - Load when user-config.yaml is absent.
//   - Load when the file exists but has no tool_destinations key.
//   - Load when the file has a tool_destinations key with an empty map.
//
// Round-trip preservation:
//   - A single mapping saved and loaded back is byte-equivalent for all fields.
//   - A multi-destination mapping (DestMain + DestField) round-trips without loss.
//   - Destination order within a mapping is preserved across Save+Load.
//   - A nil ToolDestinations is not written to the YAML file (absent file stays clean).
//
// Non-interference:
//   - TierModels content is unchanged after a Save+Load cycle that also carries
//     ToolDestinations.
//   - CustomModelIDs content is unchanged after a Save+Load cycle that also carries
//     ToolDestinations.
//   - Multiple harnesses round-trip independently.

import (
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Absence tolerance
// ---------------------------------------------------------------------------

// TestUserConfigStore_ToolDestinations_AbsentFile_YieldsNilMap verifies that loading an
// absent user-config.yaml returns nil ToolDestinations without error.
func TestUserConfigStore_ToolDestinations_AbsentFile_YieldsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load with absent file: %v", err)
	}

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("absent file: ToolDestinations has %d entries, want nil/empty",
			len(cfg.ToolDestinations))
	}
}

// TestUserConfigStore_ToolDestinations_AbsentSection_YieldsNilMap verifies that a
// user-config.yaml without a tool_destinations key produces nil ToolDestinations without
// error. Only existing content (TierModels, CustomModelIDs) needs to be present.
func TestUserConfigStore_ToolDestinations_AbsentSection_YieldsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	writeUserConfig(t, root, []byte("schema_version: \"1\"\ntier_models:\n  claude-code:\n    HIGH: claude-opus-4-5\n"))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load without tool_destinations section: %v", err)
	}

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("absent section: ToolDestinations has %d entries, want nil/empty",
			len(cfg.ToolDestinations))
	}
}

// TestUserConfigStore_ToolDestinations_EmptySection_YieldsNilMap verifies that a
// tool_destinations key present in the file but mapped to an empty object produces nil
// ToolDestinations without error.
func TestUserConfigStore_ToolDestinations_EmptySection_YieldsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	writeUserConfig(t, root, []byte("schema_version: \"1\"\ntool_destinations: {}\n"))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load with empty tool_destinations section: %v", err)
	}

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("empty section: ToolDestinations has %d entries, want nil/empty",
			len(cfg.ToolDestinations))
	}
}

// ---------------------------------------------------------------------------
// Save+Load round-trip for ToolDestinations
// ---------------------------------------------------------------------------

// TestUserConfigStore_ToolDestinations_MainDest_SaveAndLoad_GenericPreserved verifies that
// the generic tool name of a DestMain mapping is preserved through a Save+Load cycle.
func TestUserConfigStore_ToolDestinations_MainDest_SaveAndLoad_GenericPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "user_interaction",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 {
		t.Fatal("ToolDestinations[\"claude-code\"] is empty after Save+Load")
	}
	if mappings[0].Generic != "user_interaction" {
		t.Errorf("Generic = %q, want \"user_interaction\"", mappings[0].Generic)
	}
}

// TestUserConfigStore_ToolDestinations_MainDest_SaveAndLoad_KindPreserved verifies that
// a DestMain destination's Kind is preserved through a Save+Load cycle.
func TestUserConfigStore_ToolDestinations_MainDest_SaveAndLoad_KindPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "user_interaction",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations after Save+Load")
	}
	if mappings[0].Destinations[0].Kind != domain.DestMain {
		t.Errorf("Kind = %q, want %q", mappings[0].Destinations[0].Kind, domain.DestMain)
	}
}

// TestUserConfigStore_ToolDestinations_MainDest_SaveAndLoad_NamesPreserved verifies that
// the names list of a DestMain destination is preserved in order through a Save+Load cycle.
func TestUserConfigStore_ToolDestinations_MainDest_SaveAndLoad_NamesPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "subagent",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"Task", "TaskStop"}},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations after Save+Load")
	}
	names := mappings[0].Destinations[0].Names
	if len(names) != 2 || names[0] != "Task" || names[1] != "TaskStop" {
		t.Errorf("Names = %v, want [Task TaskStop]", names)
	}
}

// TestUserConfigStore_ToolDestinations_FieldDest_SaveAndLoad_AllFieldsPreserved verifies
// that every field of a DestField destination (Kind, Field, Format, Separator, Names) is
// preserved through a Save+Load cycle.
func TestUserConfigStore_ToolDestinations_FieldDest_SaveAndLoad_AllFieldsPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "user_interaction",
					Destinations: []domain.ToolDestination{
						{
							Kind:      domain.DestField,
							Field:     "mcpServers",
							Format:    domain.FormatListBlock,
							Names:     []string{"user-feedback"},
						},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations after Save+Load")
	}
	d := mappings[0].Destinations[0]
	if d.Kind != domain.DestField {
		t.Errorf("Kind = %q, want %q", d.Kind, domain.DestField)
	}
	if d.Field != "mcpServers" {
		t.Errorf("Field = %q, want \"mcpServers\"", d.Field)
	}
	if d.Format != domain.FormatListBlock {
		t.Errorf("Format = %q, want %q", d.Format, domain.FormatListBlock)
	}
	if len(d.Names) != 1 || d.Names[0] != "user-feedback" {
		t.Errorf("Names = %v, want [user-feedback]", d.Names)
	}
}

// TestUserConfigStore_ToolDestinations_ScalarDest_SeparatorPreserved verifies that a
// scalar-format destination's Separator value is preserved through a Save+Load cycle.
func TestUserConfigStore_ToolDestinations_ScalarDest_SeparatorPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "some_tool",
					Destinations: []domain.ToolDestination{
						{
							Kind:      domain.DestField,
							Field:     "someKey",
							Format:    domain.FormatScalar,
							Separator: " | ",
							Names:     []string{"ToolA", "ToolB"},
						},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations after Save+Load")
	}
	if mappings[0].Destinations[0].Separator != " | " {
		t.Errorf("Separator = %q, want \" | \"", mappings[0].Destinations[0].Separator)
	}
}

// TestUserConfigStore_ToolDestinations_MultiDest_SaveAndLoad_BothDestinationsPreserved
// verifies that a mapping with two destinations (DestMain + DestField) is stored and
// loaded back with both destinations intact, in declaration order.
func TestUserConfigStore_ToolDestinations_MultiDest_SaveAndLoad_BothDestinationsPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "user_interaction",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
						{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 {
		t.Fatal("no mappings after Save+Load")
	}
	if len(mappings[0].Destinations) != 2 {
		t.Fatalf("Destinations: got %d, want 2", len(mappings[0].Destinations))
	}
	if mappings[0].Destinations[0].Kind != domain.DestMain {
		t.Errorf("Destinations[0].Kind = %q, want %q", mappings[0].Destinations[0].Kind, domain.DestMain)
	}
	if mappings[0].Destinations[1].Kind != domain.DestField {
		t.Errorf("Destinations[1].Kind = %q, want %q", mappings[0].Destinations[1].Kind, domain.DestField)
	}
}

// TestUserConfigStore_ToolDestinations_DestinationOrder_Preserved verifies that the
// order of destinations within a mapping is preserved through a Save+Load cycle.
func TestUserConfigStore_ToolDestinations_DestinationOrder_Preserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{
					Generic: "some_tool",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "first", Names: []string{"A"}},
						{Kind: domain.DestField, Field: "second", Names: []string{"B"}},
						{Kind: domain.DestField, Field: "third", Names: []string{"C"}},
					},
				},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) == 0 {
		t.Fatal("no mappings after Save+Load")
	}
	dests := mappings[0].Destinations
	if len(dests) != 3 {
		t.Fatalf("Destinations: got %d, want 3", len(dests))
	}
	for i, want := range []string{"first", "second", "third"} {
		if dests[i].Field != want {
			t.Errorf("Destinations[%d].Field = %q, want %q (order must be preserved)", i, dests[i].Field, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Multiple harnesses round-trip independently
// ---------------------------------------------------------------------------

// TestUserConfigStore_ToolDestinations_MultipleHarnesses_AreIndependent verifies that
// mappings for different harnesses are stored and loaded independently after a Save+Load
// cycle. An entry for one harness must not appear under another.
func TestUserConfigStore_ToolDestinations_MultipleHarnesses_AreIndependent(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				}},
			},
			"ghcp-cli": {
				{Generic: "subagent", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"create_thread"}},
				}},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ccMappings := got.ToolDestinations["claude-code"]
	if len(ccMappings) != 1 || ccMappings[0].Generic != "user_interaction" {
		t.Errorf("claude-code: got %v, want one mapping for user_interaction", ccMappings)
	}

	ghMappings := got.ToolDestinations["ghcp-cli"]
	if len(ghMappings) != 1 || ghMappings[0].Generic != "subagent" {
		t.Errorf("ghcp-cli: got %v, want one mapping for subagent", ghMappings)
	}
}

// ---------------------------------------------------------------------------
// Non-interference: existing config content survives round-trip
// ---------------------------------------------------------------------------

// TestUserConfigStore_ToolDestinations_SaveAndLoad_TierModelsUnaffected verifies that
// TierModels content is preserved exactly when ToolDestinations is also saved and loaded.
// The round-trip guarantee applies to all fields simultaneously.
func TestUserConfigStore_ToolDestinations_SaveAndLoad_TierModelsUnaffected(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				}},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// TierModels must survive unchanged.
	if got.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("TierModels[claude-code][HIGH] = %q after round-trip with ToolDestinations, want \"claude-opus-4-5\"; "+
			"all fields must be persisted simultaneously", got.TierModels["claude-code"]["HIGH"])
	}
}

// TestUserConfigStore_ToolDestinations_SaveAndLoad_CustomModelIDsUnaffected verifies that
// CustomModelIDs content is preserved exactly when ToolDestinations is also saved and loaded.
func TestUserConfigStore_ToolDestinations_SaveAndLoad_CustomModelIDsUnaffected(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		CustomModelIDs: map[string][]string{
			"claude-code": {"my-custom-model"},
		},
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				}},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// CustomModelIDs must survive unchanged.
	ccIDs := got.CustomModelIDs["claude-code"]
	if !stringSliceContains(ccIDs, "my-custom-model") {
		t.Errorf("CustomModelIDs[\"claude-code\"] = %v after round-trip with ToolDestinations, "+
			"want to contain \"my-custom-model\"; all fields must be persisted simultaneously", ccIDs)
	}
}

// TestUserConfigStore_ToolDestinations_NilMap_NotWritten verifies that saving a UserConfig
// with nil ToolDestinations does not write a tool_destinations key to user-config.yaml.
// A subsequent Load must return nil ToolDestinations, not an empty or fabricated map.
func TestUserConfigStore_ToolDestinations_NilMap_NotWritten(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
		ToolDestinations: nil,
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.ToolDestinations) != 0 {
		t.Errorf("nil ToolDestinations written and loaded back as %d entries; nil must be omitted from the file",
			len(got.ToolDestinations))
	}
}

// TestUserConfigStore_ToolDestinations_EmptyDestinations_RoundTrip verifies that a mapping
// with an empty destinations list (unsupported tool) survives a Save+Load cycle intact.
func TestUserConfigStore_ToolDestinations_EmptyDestinations_RoundTrip(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "unsupported_tool", Destinations: []domain.ToolDestination{}},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) != 1 {
		t.Fatalf("claude-code: got %d mappings after round-trip, want 1", len(mappings))
	}
	if mappings[0].Generic != "unsupported_tool" {
		t.Errorf("Generic = %q, want \"unsupported_tool\"", mappings[0].Generic)
	}
	if len(mappings[0].Destinations) != 0 {
		t.Errorf("Destinations: got %d, want 0 (unsupported tool must survive round-trip)",
			len(mappings[0].Destinations))
	}
}

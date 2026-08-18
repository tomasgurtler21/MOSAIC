package config_test

// Tests for tool-destination mapping declarations in the project-level config store.
//
// Verified invariants:
//
// Absence tolerance — these cases must produce nil ToolDestinations and no error:
//   - Load when tool-config.yaml is absent.
//   - Load when the file exists but has no tool_destinations key.
//   - Load when the file has a tool_destinations key with an empty map.
//   - DefaultToolConfig() itself.
//
// Parsing — once the implementation is complete these must succeed:
//   - A single mapping with a DestMain destination is loaded with all fields correct.
//   - A multi-destination mapping (DestMain + DestField) is loaded with both destinations.
//   - A mapping with an empty destinations list (marking the tool unsupported) is loaded.
//   - Multiple harnesses in tool_destinations are stored independently.
//   - A DestField destination's Format, Separator, and Names are preserved.
//
// Non-interference:
//   - Other ToolConfig fields survive when tool_destinations is also present in the file.

import (
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Absence tolerance
// ---------------------------------------------------------------------------

// TestToolConfigStore_ToolDestinations_AbsentFile_YieldsNilMap verifies that loading an
// absent tool-config.yaml returns nil ToolDestinations without error. An absent file is
// equivalent to "no config-declared mappings" — not an error condition.
func TestToolConfigStore_ToolDestinations_AbsentFile_YieldsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load with absent file: %v", err)
	}

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("absent file: ToolDestinations has %d entries, want nil/empty",
			len(cfg.ToolDestinations))
	}
}

// TestToolConfigStore_ToolDestinations_AbsentSection_YieldsNilMap verifies that a
// tool-config.yaml without a tool_destinations key produces nil ToolDestinations without
// error. A missing section is treated identically to a missing file for this field.
func TestToolConfigStore_ToolDestinations_AbsentSection_YieldsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte("schema_version: \"1\"\nallow_external_modules: false\n"))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load without tool_destinations section: %v", err)
	}

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("absent section: ToolDestinations has %d entries, want nil/empty",
			len(cfg.ToolDestinations))
	}
}

// TestToolConfigStore_ToolDestinations_EmptySection_YieldsNilMap verifies that a
// tool_destinations map present in the file but containing no harness entries produces
// nil ToolDestinations without error.
func TestToolConfigStore_ToolDestinations_EmptySection_YieldsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte("schema_version: \"1\"\ntool_destinations: {}\n"))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load with empty tool_destinations section: %v", err)
	}

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("empty section: ToolDestinations has %d entries, want nil/empty",
			len(cfg.ToolDestinations))
	}
}

// TestDefaultToolConfig_ToolDestinationsIsNil verifies that DefaultToolConfig returns a
// nil ToolDestinations map. The default config has no config-declared mappings.
func TestDefaultToolConfig_ToolDestinationsIsNil(t *testing.T) {
	cfg := config.DefaultToolConfig()

	if len(cfg.ToolDestinations) != 0 {
		t.Errorf("DefaultToolConfig().ToolDestinations has %d entries, want nil",
			len(cfg.ToolDestinations))
	}
}

// ---------------------------------------------------------------------------
// Single mapping — DestMain destination
// ---------------------------------------------------------------------------

// TestToolConfigStore_ToolDestinations_MainDest_HarnessAndGenericPreserved verifies that
// the harness key and the generic tool name are preserved when loading a DestMain mapping.
func TestToolConfigStore_ToolDestinations_MainDest_HarnessAndGenericPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings, ok := cfg.ToolDestinations["claude-code"]
	if !ok {
		t.Fatal("ToolDestinations has no entry for harness \"claude-code\"")
	}
	if len(mappings) != 1 {
		t.Fatalf("claude-code: got %d mappings, want 1", len(mappings))
	}
	if mappings[0].Generic != "user_interaction" {
		t.Errorf("mappings[0].Generic = %q, want \"user_interaction\"", mappings[0].Generic)
	}
}

// TestToolConfigStore_ToolDestinations_MainDest_DestinationKindIsMain verifies that a
// destination declared with "to: main" is loaded with Kind == domain.DestMain.
func TestToolConfigStore_ToolDestinations_MainDest_DestinationKindIsMain(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations loaded")
	}
	if mappings[0].Destinations[0].Kind != domain.DestMain {
		t.Errorf("destination Kind = %q, want %q", mappings[0].Destinations[0].Kind, domain.DestMain)
	}
}

// TestToolConfigStore_ToolDestinations_MainDest_NamesPreserved verifies that the names
// list of a DestMain destination is loaded unchanged, in declaration order.
func TestToolConfigStore_ToolDestinations_MainDest_NamesPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: subagent
      destinations:
        - to: main
          names:
            - Task
            - TaskStop
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations loaded")
	}
	names := mappings[0].Destinations[0].Names
	if len(names) != 2 || names[0] != "Task" || names[1] != "TaskStop" {
		t.Errorf("Names = %v, want [Task TaskStop]", names)
	}
}

// ---------------------------------------------------------------------------
// Multi-destination mapping (DestMain + DestField)
// ---------------------------------------------------------------------------

// TestToolConfigStore_ToolDestinations_MultiDest_BothDestinationsLoaded verifies that a
// mapping with two destinations (DestMain and DestField) loads both, in declaration order.
func TestToolConfigStore_ToolDestinations_MultiDest_BothDestinationsLoaded(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
        - to: field
          field: mcpServers
          format: list-block
          names:
            - user-feedback
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) == 0 {
		t.Fatal("no mappings loaded")
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

// TestToolConfigStore_ToolDestinations_FieldDest_FieldNamePreserved verifies that the
// field name declared in a DestField destination survives the load.
func TestToolConfigStore_ToolDestinations_FieldDest_FieldNamePreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: field
          field: mcpServers
          format: list-block
          names:
            - user-feedback
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations loaded")
	}
	d := mappings[0].Destinations[0]
	if d.Field != "mcpServers" {
		t.Errorf("DestField.Field = %q, want \"mcpServers\"", d.Field)
	}
}

// TestToolConfigStore_ToolDestinations_FieldDest_FormatPreserved verifies that the format
// value of a DestField destination is loaded unchanged.
func TestToolConfigStore_ToolDestinations_FieldDest_FormatPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: field
          field: mcpServers
          format: list-flow
          names:
            - user-feedback
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations loaded")
	}
	if mappings[0].Destinations[0].Format != domain.FormatListFlow {
		t.Errorf("Format = %q, want %q", mappings[0].Destinations[0].Format, domain.FormatListFlow)
	}
}

// TestToolConfigStore_ToolDestinations_FieldDest_SeparatorPreserved verifies that a
// scalar-format destination's separator value survives the load.
func TestToolConfigStore_ToolDestinations_FieldDest_SeparatorPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: some_tool
      destinations:
        - to: field
          field: someKey
          format: scalar
          separator: " | "
          names:
            - ToolA
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) == 0 || len(mappings[0].Destinations) == 0 {
		t.Fatal("no destinations loaded")
	}
	if mappings[0].Destinations[0].Separator != " | " {
		t.Errorf("Separator = %q, want \" | \"", mappings[0].Destinations[0].Separator)
	}
}

// ---------------------------------------------------------------------------
// Empty destinations list (unsupported tool)
// ---------------------------------------------------------------------------

// TestToolConfigStore_ToolDestinations_EmptyDestinations_MappingIsLoaded verifies that a
// mapping with an empty destinations list (marking a generic tool as unsupported) is loaded
// with an empty Destinations slice rather than being dropped or causing an error.
func TestToolConfigStore_ToolDestinations_EmptyDestinations_MappingIsLoaded(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: unsupported_tool
      destinations: []
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) != 1 {
		t.Fatalf("got %d mappings, want 1", len(mappings))
	}
	if mappings[0].Generic != "unsupported_tool" {
		t.Errorf("Generic = %q, want \"unsupported_tool\"", mappings[0].Generic)
	}
	if len(mappings[0].Destinations) != 0 {
		t.Errorf("Destinations: got %d, want 0 (unsupported tool)", len(mappings[0].Destinations))
	}
}

// ---------------------------------------------------------------------------
// Multiple harnesses
// ---------------------------------------------------------------------------

// TestToolConfigStore_ToolDestinations_MultipleHarnesses_AreIndependent verifies that
// mappings declared under different harness keys are stored under separate entries and do
// not interfere with each other.
func TestToolConfigStore_ToolDestinations_MultipleHarnesses_AreIndependent(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
  ghcp-cli:
    - generic: subagent
      destinations:
        - to: main
          names:
            - create_thread
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ccMappings := cfg.ToolDestinations["claude-code"]
	if len(ccMappings) != 1 {
		t.Errorf("claude-code: got %d mappings, want 1", len(ccMappings))
	} else if ccMappings[0].Generic != "user_interaction" {
		t.Errorf("claude-code: Generic = %q, want \"user_interaction\"", ccMappings[0].Generic)
	}

	ghMappings := cfg.ToolDestinations["ghcp-cli"]
	if len(ghMappings) != 1 {
		t.Errorf("ghcp-cli: got %d mappings, want 1", len(ghMappings))
	} else if ghMappings[0].Generic != "subagent" {
		t.Errorf("ghcp-cli: Generic = %q, want \"subagent\"", ghMappings[0].Generic)
	}
}

// TestToolConfigStore_ToolDestinations_MultipleMappingsPerHarness_AllLoaded verifies that
// when a single harness declares multiple mappings, all are loaded in declaration order.
func TestToolConfigStore_ToolDestinations_MultipleMappingsPerHarness_AllLoaded(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

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
        - to: main
          names:
            - AskUserQuestion
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mappings := cfg.ToolDestinations["claude-code"]
	if len(mappings) != 2 {
		t.Fatalf("claude-code: got %d mappings, want 2", len(mappings))
	}
	if mappings[0].Generic != "subagent" {
		t.Errorf("mappings[0].Generic = %q, want \"subagent\"", mappings[0].Generic)
	}
	if mappings[1].Generic != "user_interaction" {
		t.Errorf("mappings[1].Generic = %q, want \"user_interaction\"", mappings[1].Generic)
	}
}

// ---------------------------------------------------------------------------
// Non-interference with other config fields
// ---------------------------------------------------------------------------

// TestToolConfigStore_ToolDestinations_OtherFieldsUnchanged verifies that the presence of
// a tool_destinations block in the config file does not alter other ToolConfig fields
// (UtilityAgentAllowList, AllowExternalModules, LogRetentionRuns).
func TestToolConfigStore_ToolDestinations_OtherFieldsUnchanged(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	writeToolConfig(t, root, []byte(`schema_version: "1"
utility_agent_allow_list:
  - anthropic-subagent-creator
allow_external_modules: true
log_retention_runs: 7
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names:
            - AskUserQuestion
`))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.AllowExternalModules {
		t.Error("AllowExternalModules changed after tool_destinations was added to the file; must be independent")
	}
	if cfg.LogRetentionRuns != 7 {
		t.Errorf("LogRetentionRuns = %d, want 7; must be independent of tool_destinations", cfg.LogRetentionRuns)
	}

	found := false
	for _, key := range cfg.UtilityAgentAllowList {
		if key == "anthropic-subagent-creator" {
			found = true
			break
		}
	}
	if !found {
		t.Error("UtilityAgentAllowList lost \"anthropic-subagent-creator\" after tool_destinations was added; must be independent")
	}
}

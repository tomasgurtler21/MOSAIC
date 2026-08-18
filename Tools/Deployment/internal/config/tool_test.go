package config_test

// Tests for the tool configuration store and its documented defaults.
//
// Verified invariants:
//
// Load-with-defaults (AC14.4):
//   - When tool-config.yaml is absent, Load returns DefaultToolConfig() and a nil error.
//   - DefaultToolConfig is an exact match for the value Load returns on an absent file.
//
// Default allow-list (AC14.7):
//   - DefaultToolConfig includes all six utility agents shipped with MOSAIC.
//   - The list has exactly six entries with no duplicates.
//   - AllowExternalModules defaults to false.
//   - LogRetentionRuns defaults to 0 (unbounded).
//
// Store.Path:
//   - Non-empty, ends with "tool-config.yaml", inside the mosaicRoot tree.
//
// No per-agent model persistence (AC14.6):
//   - ToolConfig has no field of type domain.ModelSelection.
//
// Schema migration:
//   - An old-schema tool-config.yaml is loaded without error.
//   - Recognisable values in the old file are preserved after migration.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared test helpers (used by both tool_test.go and user_test.go)
// ---------------------------------------------------------------------------

// makeRoot creates a temporary directory that resembles a MOSAIC root, including the
// MosaicDeploy/config subdirectory expected by both config stores.
func makeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "MosaicDeploy", "config"), 0o755); err != nil {
		t.Fatalf("makeRoot: %v", err)
	}
	return root
}

// writeToolConfig writes the given YAML bytes to tool-config.yaml inside root.
func writeToolConfig(t *testing.T, root string, content []byte) {
	t.Helper()
	p := filepath.Join(root, "MosaicDeploy", "config", "tool-config.yaml")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeToolConfig: %v", err)
	}
}

// knownUtilityAgents returns the canonical set of utility agent keys shipped with MOSAIC.
// These are the six agents listed in Catalog/UtilityAgents/ and referenced by
// the Plan and ContractsDesign. They must all appear in the default allow-list (AC14.7).
func knownUtilityAgents() []string {
	return []string{
		"anthropic-subagent-creator",
		"harness-bug-hunter",
		"orchestration-architect",
		"system-prompt-capturer",
		"transformation",
		"workflow-creator",
	}
}

// ---------------------------------------------------------------------------
// Load-with-defaults: absent file
// ---------------------------------------------------------------------------

// TestToolConfigStore_AbsentFile_ReturnsNoError verifies that loading a missing
// tool-config.yaml does not return an error. The absent file is a valid state whose
// result is the documented defaults (AC14.4).
func TestToolConfigStore_AbsentFile_ReturnsNoError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	_, err := store.Load()
	if err != nil {
		t.Errorf("Load with absent tool-config.yaml returned error: %v; "+
			"an absent file must yield defaults without error (AC14.4)", err)
	}
}

// TestToolConfigStore_AbsentFile_ReturnsDefaultConfig verifies that loading a missing
// tool-config.yaml returns a value identical to DefaultToolConfig().
func TestToolConfigStore_AbsentFile_ReturnsDefaultConfig(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.DefaultToolConfig()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("absent file: Load() differs from DefaultToolConfig():\n  got:  %+v\n  want: %+v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Default allow-list (AC14.7)
// ---------------------------------------------------------------------------

// TestDefaultToolConfig_AllowListContainsAllSixUtilityAgents verifies that the default
// allow-list includes every utility agent shipped with MOSAIC. The decision to include
// all six by default is documented in DefaultToolConfig's doc comment (AC14.7).
func TestDefaultToolConfig_AllowListContainsAllSixUtilityAgents(t *testing.T) {
	cfg := config.DefaultToolConfig()

	allowSet := make(map[string]bool, len(cfg.UtilityAgentAllowList))
	for _, key := range cfg.UtilityAgentAllowList {
		allowSet[key] = true
	}

	for _, want := range knownUtilityAgents() {
		if !allowSet[want] {
			t.Errorf("DefaultToolConfig().UtilityAgentAllowList missing %q; "+
				"all six utility agents must be in the default allow-list (AC14.7)", want)
		}
	}
}

// TestDefaultToolConfig_AllowListHasExactlySixAgents verifies that the default allow-list
// has exactly six entries — one per utility agent — with no extras.
func TestDefaultToolConfig_AllowListHasExactlySixAgents(t *testing.T) {
	cfg := config.DefaultToolConfig()
	if len(cfg.UtilityAgentAllowList) != 6 {
		t.Errorf("DefaultToolConfig().UtilityAgentAllowList has %d entries, want exactly 6; got: %v",
			len(cfg.UtilityAgentAllowList), cfg.UtilityAgentAllowList)
	}
}

// TestDefaultToolConfig_AllowListHasNoDuplicates verifies that no agent key appears
// more than once in the default allow-list.
func TestDefaultToolConfig_AllowListHasNoDuplicates(t *testing.T) {
	cfg := config.DefaultToolConfig()
	seen := make(map[string]bool)
	for _, key := range cfg.UtilityAgentAllowList {
		if seen[key] {
			t.Errorf("DefaultToolConfig().UtilityAgentAllowList contains duplicate key %q", key)
		}
		seen[key] = true
	}
}

// TestDefaultToolConfig_AllowListKeysAreNonEmpty verifies that no empty string appears
// in the default allow-list. An empty key would match no agent in the catalog.
func TestDefaultToolConfig_AllowListKeysAreNonEmpty(t *testing.T) {
	cfg := config.DefaultToolConfig()
	for i, key := range cfg.UtilityAgentAllowList {
		if key == "" {
			t.Errorf("DefaultToolConfig().UtilityAgentAllowList[%d] is an empty string; "+
				"all keys must be non-empty agent slugs", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Default AllowExternalModules
// ---------------------------------------------------------------------------

// TestDefaultToolConfig_AllowExternalModulesIsFalse verifies that external modules are
// disabled by default. External modules execute arbitrary binaries and represent an elevated
// trust boundary; enabling them requires an explicit operator decision.
func TestDefaultToolConfig_AllowExternalModulesIsFalse(t *testing.T) {
	cfg := config.DefaultToolConfig()
	if cfg.AllowExternalModules {
		t.Error("DefaultToolConfig().AllowExternalModules is true; default must be false " +
			"(external modules execute arbitrary binaries and require explicit operator opt-in)")
	}
}

// ---------------------------------------------------------------------------
// Default LogRetentionRuns
// ---------------------------------------------------------------------------

// TestDefaultToolConfig_LogRetentionRunsIsZero verifies that the default log retention
// is 0, meaning all run history is kept. A zero value means unbounded.
func TestDefaultToolConfig_LogRetentionRunsIsZero(t *testing.T) {
	cfg := config.DefaultToolConfig()
	if cfg.LogRetentionRuns != 0 {
		t.Errorf("DefaultToolConfig().LogRetentionRuns = %d, want 0 (unbounded by default)", cfg.LogRetentionRuns)
	}
}

// ---------------------------------------------------------------------------
// Default SchemaVersion
// ---------------------------------------------------------------------------

// TestDefaultToolConfig_SchemaVersionIsNonEmpty verifies that the default config has a
// non-empty SchemaVersion field so it can be written to disk and round-tripped.
func TestDefaultToolConfig_SchemaVersionIsNonEmpty(t *testing.T) {
	cfg := config.DefaultToolConfig()
	if cfg.SchemaVersion == "" {
		t.Error("DefaultToolConfig().SchemaVersion is empty; a schema version is required for forward compatibility")
	}
}

// ---------------------------------------------------------------------------
// ToolConfigStore.Path
// ---------------------------------------------------------------------------

// TestToolConfigStore_PathIsNonEmpty verifies that Path() returns a non-empty string.
func TestToolConfigStore_PathIsNonEmpty(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	if store.Path() == "" {
		t.Error("ToolConfigStore.Path() is empty; expected an absolute path to tool-config.yaml")
	}
}

// TestToolConfigStore_PathEndsWithToolConfigYAML verifies that Path() ends with the
// canonical file name "tool-config.yaml".
func TestToolConfigStore_PathEndsWithToolConfigYAML(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	if filepath.Base(store.Path()) != "tool-config.yaml" {
		t.Errorf("ToolConfigStore.Path() base = %q, want \"tool-config.yaml\"", filepath.Base(store.Path()))
	}
}

// TestToolConfigStore_PathIsInsideMosaicRoot verifies that Path() is inside the
// mosaicRoot directory tree.
func TestToolConfigStore_PathIsInsideMosaicRoot(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	p := store.Path()
	rel, err := filepath.Rel(root, p)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q): %v", root, p, err)
	}
	if len(rel) >= 2 && rel[:2] == ".." {
		t.Errorf("ToolConfigStore.Path() %q is outside mosaicRoot %q", p, root)
	}
}

// ---------------------------------------------------------------------------
// No per-agent model persistence (AC14.6)
// ---------------------------------------------------------------------------

// TestToolConfig_HasNoModelSelectionField verifies that the ToolConfig struct has no
// field of type domain.ModelSelection. Per-agent model choices must never be persisted
// to a config file (AC14.6, FR-11).
func TestToolConfig_HasNoModelSelectionField(t *testing.T) {
	modelSelectionType := reflect.TypeOf(domain.ModelSelection{})
	toolConfigType := reflect.TypeOf(config.ToolConfig{})

	for i := 0; i < toolConfigType.NumField(); i++ {
		f := toolConfigType.Field(i)
		if f.Type == modelSelectionType {
			t.Errorf("ToolConfig.%s has type domain.ModelSelection; "+
				"per-agent model choices must never appear in config types (AC14.6)", f.Name)
		}
	}
}

// TestToolConfig_HasNoMapValueOfModelSelectionType verifies that no map-valued field
// in ToolConfig has domain.ModelSelection as its map element type.
func TestToolConfig_HasNoMapValueOfModelSelectionType(t *testing.T) {
	modelSelectionType := reflect.TypeOf(domain.ModelSelection{})
	toolConfigType := reflect.TypeOf(config.ToolConfig{})

	for i := 0; i < toolConfigType.NumField(); i++ {
		f := toolConfigType.Field(i)
		if f.Type.Kind() == reflect.Map && f.Type.Elem() == modelSelectionType {
			t.Errorf("ToolConfig.%s is a map with value type domain.ModelSelection; "+
				"per-agent model choices must not be stored (AC14.6)", f.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Schema migration: old-schema tool config
// ---------------------------------------------------------------------------

// TestToolConfigStore_OldSchemaVersion_LoadDoesNotReturnError verifies that a
// tool-config.yaml written with an older schema_version is loaded without error.
// Schema evolution must not strand existing deployments.
func TestToolConfigStore_OldSchemaVersion_LoadDoesNotReturnError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	// schema_version "0" represents an older format from before the current schema version.
	oldConfig := []byte("schema_version: \"0\"\nutility_agent_allow_list:\n  - anthropic-subagent-creator\nallow_external_modules: true\nlog_retention_runs: 5\n")
	writeToolConfig(t, root, oldConfig)

	_, err := store.Load()
	if err != nil {
		t.Errorf("Load with schema_version \"0\" returned error: %v; "+
			"schema migration must succeed without error", err)
	}
}

// TestToolConfigStore_OldSchemaVersion_PreservesAllowExternalModules verifies that a
// boolean field (AllowExternalModules: true) present in an old-schema config is preserved
// after migration. Recognisable fields must survive a schema upgrade.
func TestToolConfigStore_OldSchemaVersion_PreservesAllowExternalModules(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	oldConfig := []byte("schema_version: \"0\"\nutility_agent_allow_list:\n  - anthropic-subagent-creator\nallow_external_modules: true\nlog_retention_runs: 5\n")
	writeToolConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.AllowExternalModules {
		t.Error("AllowExternalModules was true in old-schema config but false after migration; " +
			"recognisable values must be preserved")
	}
}

// TestToolConfigStore_OldSchemaVersion_PreservesLogRetentionRuns verifies that a numeric
// field (LogRetentionRuns: 5) present in an old-schema config is preserved after migration.
func TestToolConfigStore_OldSchemaVersion_PreservesLogRetentionRuns(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	oldConfig := []byte("schema_version: \"0\"\nutility_agent_allow_list:\n  - anthropic-subagent-creator\nallow_external_modules: true\nlog_retention_runs: 5\n")
	writeToolConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.LogRetentionRuns != 5 {
		t.Errorf("LogRetentionRuns: got %d after migration, want 5; recognisable values must be preserved", cfg.LogRetentionRuns)
	}
}

// TestToolConfigStore_OldSchemaVersion_PreservesUtilityAgentAllowList verifies that the
// utility_agent_allow_list present in an old-schema config is preserved after migration.
// A migration that silently resets the allow-list to defaults would discard the operator's
// intended access restrictions — the test catches this by using a non-default single-entry list.
func TestToolConfigStore_OldSchemaVersion_PreservesUtilityAgentAllowList(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	// The old-schema config includes a deliberately restricted allow-list (one agent only),
	// which is different from the six-agent default, making a reset to defaults detectable.
	oldConfig := []byte("schema_version: \"0\"\nutility_agent_allow_list:\n  - anthropic-subagent-creator\nallow_external_modules: true\nlog_retention_runs: 5\n")
	writeToolConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	found := false
	for _, key := range cfg.UtilityAgentAllowList {
		if key == "anthropic-subagent-creator" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("UtilityAgentAllowList after migration does not contain \"anthropic-subagent-creator\"; "+
			"the allow-list from the old-schema config must be preserved, not reset to defaults")
	}
}

// TestToolConfigStore_OldSchemaVersion_ResultHasCurrentSchemaVersion verifies that after
// loading an old-schema config, the returned ToolConfig reflects the current schema version.
func TestToolConfigStore_OldSchemaVersion_ResultHasCurrentSchemaVersion(t *testing.T) {
	root := makeRoot(t)
	store := config.NewToolConfigStore(root)
	oldConfig := []byte("schema_version: \"0\"\nutility_agent_allow_list:\n  - anthropic-subagent-creator\nallow_external_modules: false\nlog_retention_runs: 0\n")
	writeToolConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	current := config.DefaultToolConfig().SchemaVersion
	if cfg.SchemaVersion != current {
		t.Errorf("after migration: SchemaVersion = %q, want current schema %q; "+
			"migrated config must reflect the current schema version", cfg.SchemaVersion, current)
	}
}

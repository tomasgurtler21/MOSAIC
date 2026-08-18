package config_test

// Tests for the user configuration store.
//
// Verified invariants:
//
// Load when absent:
//   - An absent user-config.yaml returns a zero-value UserConfig with no error.
//
// Per-harness tier-to-model mappings (AC14.5):
//   - Tier-to-model mappings saved for one harness are loaded back unchanged.
//   - Mappings for different harnesses are stored independently and do not interfere.
//   - Tiers that no longer exist in source are stored and loaded verbatim; no validation
//     is performed against the current agent tier set.
//   - Saving a second time overwrites the first; the most recent save is what Load returns.
//   - A UserConfig with no tier mappings round-trips without fabricating entries.
//
// Store.Path:
//   - Non-empty, ends with "user-config.yaml", inside the mosaicRoot tree.
//
// No per-agent model persistence (AC14.6):
//   - UserConfig has no field of type domain.ModelSelection.
//   - UserConfig.TierModels maps to string model IDs, not to domain.ModelSelection.
//
// Schema migration:
//   - An old-schema user-config.yaml is loaded without error.
//   - Tier mappings present in the old file are preserved after migration.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeUserConfig writes the given YAML bytes to user-config.yaml inside root.
// makeRoot (defined in tool_test.go) creates the required MosaicDeploy/config directory.
func writeUserConfig(t *testing.T, root string, content []byte) {
	t.Helper()
	p := filepath.Join(root, "MosaicDeploy", "config", "user-config.yaml")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeUserConfig: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Load when absent
// ---------------------------------------------------------------------------

// TestUserConfigStore_AbsentFile_ReturnsNoError verifies that loading a missing
// user-config.yaml does not return an error. The absent file is a normal first-run state.
func TestUserConfigStore_AbsentFile_ReturnsNoError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	_, err := store.Load()
	if err != nil {
		t.Errorf("Load with absent user-config.yaml returned error: %v; "+
			"an absent file must return a zero-value UserConfig without error", err)
	}
}

// TestUserConfigStore_AbsentFile_ReturnsZeroValue verifies that loading a missing
// user-config.yaml returns a UserConfig with no tier model entries.
func TestUserConfigStore_AbsentFile_ReturnsZeroValue(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.TierModels) != 0 {
		t.Errorf("absent file: TierModels has %d entries, want 0", len(got.TierModels))
	}
}

// ---------------------------------------------------------------------------
// Per-harness tier-to-model mappings (AC14.5)
// ---------------------------------------------------------------------------

// TestUserConfigStore_SaveAndLoad_TierMappingPreserved verifies that a tier-to-model
// mapping saved for one harness is loaded back unchanged.
func TestUserConfigStore_SaveAndLoad_TierMappingPreserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {
				"HIGH":   "claude-opus-4-5",
				"MEDIUM": "claude-sonnet-4-5",
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

	harnessMap, ok := got.TierModels["claude-code"]
	if !ok {
		t.Fatal("loaded UserConfig has no mapping for harness \"claude-code\"")
	}
	if harnessMap["HIGH"] != "claude-opus-4-5" {
		t.Errorf("claude-code HIGH: got %q, want \"claude-opus-4-5\"", harnessMap["HIGH"])
	}
	if harnessMap["MEDIUM"] != "claude-sonnet-4-5" {
		t.Errorf("claude-code MEDIUM: got %q, want \"claude-sonnet-4-5\"", harnessMap["MEDIUM"])
	}
}

// TestUserConfigStore_SaveAndLoad_MultipleHarnessesAreIndependent verifies that tier
// mappings saved for different harnesses do not interfere with each other.
func TestUserConfigStore_SaveAndLoad_MultipleHarnessesAreIndependent(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {
				"HIGH": "claude-opus-4-5",
			},
			"ghcp-cli": {
				"HIGH": "gpt-4.1",
				"LOW":  "gpt-4o-mini",
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

	if got.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("claude-code HIGH: got %q, want \"claude-opus-4-5\"", got.TierModels["claude-code"]["HIGH"])
	}
	if got.TierModels["ghcp-cli"]["HIGH"] != "gpt-4.1" {
		t.Errorf("ghcp-cli HIGH: got %q, want \"gpt-4.1\"", got.TierModels["ghcp-cli"]["HIGH"])
	}
	if got.TierModels["ghcp-cli"]["LOW"] != "gpt-4o-mini" {
		t.Errorf("ghcp-cli LOW: got %q, want \"gpt-4o-mini\"", got.TierModels["ghcp-cli"]["LOW"])
	}
}

// TestUserConfigStore_VanishedTier_IsStoredAndLoadedVerbatim verifies that a tier key
// that no longer exists in the MOSAIC source is stored and returned without any validation
// or error (AC14.5). Tier values are open strings, never validated against the catalog.
func TestUserConfigStore_VanishedTier_IsStoredAndLoadedVerbatim(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	// "LEGACY-TIER" is a hypothetical tier that existed in an earlier MOSAIC version but
	// has since been removed. The store must preserve it without error.
	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {
				"HIGH":        "claude-opus-4-5",
				"LEGACY-TIER": "claude-opus-3",
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

	if got.TierModels["claude-code"]["LEGACY-TIER"] != "claude-opus-3" {
		t.Errorf("vanished tier: got %q for LEGACY-TIER, want \"claude-opus-3\"; "+
			"tier values must be stored verbatim without validation (AC14.5)",
			got.TierModels["claude-code"]["LEGACY-TIER"])
	}
}

// TestUserConfigStore_EmptyTierModels_RoundTrips verifies that saving a UserConfig with
// an empty TierModels map and loading it back produces an empty (or nil) map.
func TestUserConfigStore_EmptyTierModels_RoundTrips(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.TierModels) != 0 {
		t.Errorf("empty TierModels: got %d entries after round-trip, want 0", len(got.TierModels))
	}
}

// TestUserConfigStore_SaveOverwrite_SecondSaveWins verifies that saving twice overwrites
// the first save. Load always returns the state from the most recent Save call.
func TestUserConfigStore_SaveOverwrite_SecondSaveWins(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	first := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-3"},
		},
	}
	second := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
	}

	if err := store.Save(first); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := store.Save(second); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("second save should overwrite first: got %q, want \"claude-opus-4-5\"",
			got.TierModels["claude-code"]["HIGH"])
	}
}

// TestUserConfigStore_Save_CreatesDirIfMissing verifies that Save creates the
// MosaicDeploy/config directory if it does not exist, so callers do not need to pre-create it.
func TestUserConfigStore_Save_CreatesDirIfMissing(t *testing.T) {
	root := t.TempDir() // no MosaicDeploy/config pre-created
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
	}

	err := store.Save(cfg)
	if err != nil {
		t.Errorf("Save without pre-existing config directory returned error: %v; "+
			"Save must create the directory if needed", err)
	}
}

// ---------------------------------------------------------------------------
// UserConfigStore.Path
// ---------------------------------------------------------------------------

// TestUserConfigStore_PathIsNonEmpty verifies that Path() returns a non-empty string.
func TestUserConfigStore_PathIsNonEmpty(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)
	if store.Path() == "" {
		t.Error("UserConfigStore.Path() is empty; expected absolute path to user-config.yaml")
	}
}

// TestUserConfigStore_PathEndsWithUserConfigYAML verifies that Path() ends with the
// canonical file name "user-config.yaml".
func TestUserConfigStore_PathEndsWithUserConfigYAML(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)
	if filepath.Base(store.Path()) != "user-config.yaml" {
		t.Errorf("UserConfigStore.Path() base = %q, want \"user-config.yaml\"", filepath.Base(store.Path()))
	}
}

// TestUserConfigStore_PathIsInsideMosaicRoot verifies that Path() is inside the
// mosaicRoot directory tree.
func TestUserConfigStore_PathIsInsideMosaicRoot(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)
	p := store.Path()
	rel, err := filepath.Rel(root, p)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q): %v", root, p, err)
	}
	if len(rel) >= 2 && rel[:2] == ".." {
		t.Errorf("UserConfigStore.Path() %q is outside mosaicRoot %q", p, root)
	}
}

// ---------------------------------------------------------------------------
// No per-agent model persistence (AC14.6)
// ---------------------------------------------------------------------------

// TestUserConfig_HasNoModelSelectionField verifies that the UserConfig struct has no
// field of type domain.ModelSelection. Only tier-level mappings persist between runs;
// per-agent model choices have no representation in this type (AC14.6, FR-11).
func TestUserConfig_HasNoModelSelectionField(t *testing.T) {
	modelSelectionType := reflect.TypeOf(domain.ModelSelection{})
	userConfigType := reflect.TypeOf(config.UserConfig{})

	for i := 0; i < userConfigType.NumField(); i++ {
		f := userConfigType.Field(i)
		if f.Type == modelSelectionType {
			t.Errorf("UserConfig.%s has type domain.ModelSelection; "+
				"per-agent model choices must never appear in config types (AC14.6)", f.Name)
		}
	}
}

// TestUserConfig_HasNoMapValueOfModelSelectionType verifies that no map-valued field
// in UserConfig has domain.ModelSelection as its element type.
func TestUserConfig_HasNoMapValueOfModelSelectionType(t *testing.T) {
	modelSelectionType := reflect.TypeOf(domain.ModelSelection{})
	userConfigType := reflect.TypeOf(config.UserConfig{})

	for i := 0; i < userConfigType.NumField(); i++ {
		f := userConfigType.Field(i)
		if f.Type.Kind() == reflect.Map && f.Type.Elem() == modelSelectionType {
			t.Errorf("UserConfig.%s is a map with element type domain.ModelSelection; "+
				"per-agent model choices must not be stored (AC14.6)", f.Name)
		}
	}
}

// TestUserConfig_TierModels_LeafTypeIsString verifies that the TierModels field maps
// to string model IDs, not to domain.ModelSelection. ModelSelection carries origin
// metadata (OriginTierMapping, etc.) that must not persist in config files.
func TestUserConfig_TierModels_LeafTypeIsString(t *testing.T) {
	userConfigType := reflect.TypeOf(config.UserConfig{})
	field, ok := userConfigType.FieldByName("TierModels")
	if !ok {
		t.Skip("UserConfig has no TierModels field; type check skipped")
	}

	modelSelectionType := reflect.TypeOf(domain.ModelSelection{})
	ft := field.Type

	// TierModels is map[string]map[domain.Tier]string; the leaf must be string.
	if ft.Kind() == reflect.Map {
		innerType := ft.Elem()
		if innerType.Kind() == reflect.Map {
			leafType := innerType.Elem()
			if leafType == modelSelectionType {
				t.Errorf("UserConfig.TierModels leaf type is domain.ModelSelection; "+
					"it must be a plain string (model ID) — ModelSelection carries origin metadata "+
					"that must never be persisted (AC14.6)")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Schema migration
// ---------------------------------------------------------------------------

// TestUserConfigStore_OldSchemaVersion_LoadDoesNotReturnError verifies that a
// user-config.yaml written with an older schema_version is loaded without error.
func TestUserConfigStore_OldSchemaVersion_LoadDoesNotReturnError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	oldConfig := []byte("schema_version: \"0\"\ntier_models:\n  claude-code:\n    HIGH: claude-opus-4-5\n    MEDIUM: claude-sonnet-4-5\n")
	writeUserConfig(t, root, oldConfig)

	_, err := store.Load()
	if err != nil {
		t.Errorf("Load with schema_version \"0\" returned error: %v; "+
			"schema migration must succeed without error", err)
	}
}

// TestUserConfigStore_OldSchemaVersion_PreservesStoredMappings verifies that tier
// mappings present in an old-schema file are preserved in the loaded UserConfig.
func TestUserConfigStore_OldSchemaVersion_PreservesStoredMappings(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	oldConfig := []byte("schema_version: \"0\"\ntier_models:\n  claude-code:\n    HIGH: claude-opus-4-5\n")
	writeUserConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("old-schema migration: claude-code HIGH: got %q, want \"claude-opus-4-5\"; "+
			"tier mappings must survive schema migration", cfg.TierModels["claude-code"]["HIGH"])
	}
}

// TestUserConfigStore_OldSchemaVersion_ResultHasCurrentSchemaVersion verifies that after
// loading an old-schema user config, the returned UserConfig carries the current schema version.
func TestUserConfigStore_OldSchemaVersion_ResultHasCurrentSchemaVersion(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	oldConfig := []byte("schema_version: \"0\"\ntier_models:\n  claude-code:\n    HIGH: claude-opus-4-5\n")
	writeUserConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The current schema version is whatever DefaultToolConfig reports for schema version;
	// for UserConfig we use a parallel convention. The returned config must not carry "0".
	if cfg.SchemaVersion == "0" {
		t.Error("loaded config from old schema still has SchemaVersion \"0\"; " +
			"migration must update SchemaVersion to the current value")
	}
}

// TestUserConfigStore_OldSchemaVersion_PreservesMultipleHarnesses verifies that a
// multi-harness old-schema config migrates all harness mappings, not just the first.
func TestUserConfigStore_OldSchemaVersion_PreservesMultipleHarnesses(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	oldConfig := []byte("schema_version: \"0\"\ntier_models:\n  claude-code:\n    HIGH: claude-opus-4-5\n  ghcp-cli:\n    HIGH: gpt-4.1\n")
	writeUserConfig(t, root, oldConfig)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.TierModels["ghcp-cli"]["HIGH"] != "gpt-4.1" {
		t.Errorf("old-schema migration: ghcp-cli HIGH: got %q, want \"gpt-4.1\"; "+
			"all harness mappings must survive schema migration", cfg.TierModels["ghcp-cli"]["HIGH"])
	}
}

// ---------------------------------------------------------------------------
// SchemaVersion field in UserConfig
// ---------------------------------------------------------------------------

// TestUserConfig_SavedConfig_HasCurrentSchemaVersion verifies that a UserConfig written
// by Save carries the current schema version when loaded back, not an empty string.
func TestUserConfig_SavedConfig_HasCurrentSchemaVersion(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.SchemaVersion == "" {
		t.Error("loaded UserConfig has empty SchemaVersion; Save must write the current schema version")
	}
}

// TestUserConfig_TierKey_IsPreservedVerbatimWithUnusualCharacters verifies that tier
// strings containing hyphens, underscores, and mixed case are stored and loaded verbatim.
// Tier keys are open strings with no normalisation.
func TestUserConfig_TierKey_IsPreservedVerbatimWithUnusualCharacters(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	// Tier keys that exercise unusual characters: hyphens, mixed case, digits.
	unusualTiers := map[domain.Tier]string{
		"MEDIUM-HIGH":  "claude-sonnet-4-5",
		"LOW-MEDIUM":   "claude-haiku-3",
		"custom_tier1": "some-model",
	}

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": unusualTiers,
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for tier, wantModel := range unusualTiers {
		gotModel := got.TierModels["claude-code"][tier]
		if gotModel != wantModel {
			t.Errorf("tier %q: got model %q, want %q; tier keys must be stored verbatim", tier, gotModel, wantModel)
		}
	}
}

// TestUserConfig_Lookup_MissingHarness_ReturnsEmptyMap verifies that accessing a harness
// not present in TierModels returns a nil/empty inner map without panicking. This is normal
// Go map behaviour, but the test pins that Load does not fabricate entries for unknown harnesses.
func TestUserConfig_Lookup_MissingHarness_ReturnsEmptyMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// "ghcp-cli" was not saved; accessing it must not panic.
	inner := got.TierModels["ghcp-cli"]
	if len(inner) != 0 {
		t.Errorf("TierModels[\"ghcp-cli\"] has %d entries, want 0; Load must not fabricate entries", len(inner))
	}
}

// ---------------------------------------------------------------------------
// CustomModelIDs round-trip
// ---------------------------------------------------------------------------

// stringSliceContains reports whether s contains the target string.
func stringSliceContains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// TestUserConfigStore_CustomModelIDs_SaveAndLoad_Preserved verifies that custom model IDs
// saved for a harness are loaded back unchanged. This test will fail until the Save and Load
// implementations are updated to handle the CustomModelIDs field.
func TestUserConfigStore_CustomModelIDs_SaveAndLoad_Preserved(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		CustomModelIDs: map[string][]string{
			"claude-code": {"claude-opus-special", "my-custom-v2"},
			"ghcp-cli":    {"gpt-custom-1"},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ccIDs := got.CustomModelIDs["claude-code"]
	if len(ccIDs) < 2 {
		t.Errorf("CustomModelIDs[\"claude-code\"] has %d entries after round-trip, want 2; "+
			"Save+Load must preserve custom model IDs", len(ccIDs))
	}
	if !stringSliceContains(ccIDs, "claude-opus-special") {
		t.Errorf("CustomModelIDs[\"claude-code\"] = %v; want to contain \"claude-opus-special\"", ccIDs)
	}
	if !stringSliceContains(ccIDs, "my-custom-v2") {
		t.Errorf("CustomModelIDs[\"claude-code\"] = %v; want to contain \"my-custom-v2\"", ccIDs)
	}

	gcIDs := got.CustomModelIDs["ghcp-cli"]
	if !stringSliceContains(gcIDs, "gpt-custom-1") {
		t.Errorf("CustomModelIDs[\"ghcp-cli\"] = %v; want to contain \"gpt-custom-1\"", gcIDs)
	}
}

// TestUserConfigStore_CustomModelIDs_MultiHarness_IndependentSlices verifies that custom
// model IDs for one harness do not appear under another harness after a Save+Load cycle.
func TestUserConfigStore_CustomModelIDs_MultiHarness_IndependentSlices(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		CustomModelIDs: map[string][]string{
			"claude-code": {"special-model-for-cc"},
			"ghcp-cli":    {"special-model-for-ghcp"},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ccIDs := got.CustomModelIDs["claude-code"]
	// Positive assertion: the list must be non-empty before independence can be verified.
	// Without this, negative-only assertions trivially pass when Save/Load skip the field.
	if len(ccIDs) == 0 {
		t.Fatal("CustomModelIDs[\"claude-code\"] is empty after Save+Load; " +
			"Save and Load must persist and restore custom model IDs before independence can be verified")
	}
	if stringSliceContains(ccIDs, "special-model-for-ghcp") {
		t.Error("CustomModelIDs[\"claude-code\"] contains an ID that belongs to \"ghcp-cli\"; " +
			"custom model IDs for different harnesses must be stored independently")
	}
	gcIDs := got.CustomModelIDs["ghcp-cli"]
	// Positive assertion: same requirement for the second harness.
	if len(gcIDs) == 0 {
		t.Fatal("CustomModelIDs[\"ghcp-cli\"] is empty after Save+Load; " +
			"Save and Load must persist and restore custom model IDs before independence can be verified")
	}
	if stringSliceContains(gcIDs, "special-model-for-cc") {
		t.Error("CustomModelIDs[\"ghcp-cli\"] contains an ID that belongs to \"claude-code\"; " +
			"custom model IDs for different harnesses must be stored independently")
	}
}

// TestUserConfigStore_CustomModelIDs_AbsentFile_ReturnsNilMap verifies that loading an
// absent user-config.yaml does not fabricate any CustomModelIDs entries.
func TestUserConfigStore_CustomModelIDs_AbsentFile_ReturnsNilMap(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.CustomModelIDs) != 0 {
		t.Errorf("absent file: CustomModelIDs has %d harness entries, want 0; "+
			"Load must not fabricate custom model ID entries", len(got.CustomModelIDs))
	}
}

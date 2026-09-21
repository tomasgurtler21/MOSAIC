package config_test

// codex_config_test.go verifies that the user-configuration layer accepts "codex" as a
// harness key in every harness-keyed configuration section.
//
// T12.4 -- Config accepts Codex harness key:
//   - "codex" can be stored and loaded as a key in TierModels.
//   - "codex" can be stored and loaded as a key in CustomModelIDs.
//   - "codex" can be stored and loaded as a key in ToolDestinations.
//   - An unconfigured Codex harness (no "codex" key in TierModels) returns an empty
//     tier-model map for "codex", consistent with FR-17a: Codex contributes no default
//     tier models; an unconfigured Codex agent gets no model silently chosen by the
//     descriptor's tier catalog.
//
// Evidence of the "generic already" verdict for the Config surface (I12.5 AC12.10):
//   The config layer uses map[string]... for all harness-keyed sections; no allowlist
//   of harness IDs exists in user.go, toolmappings.go, or the store. Any harness ID
//   (including "codex") is a valid map key.

import (
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T12.4: Codex key in TierModels
// ---------------------------------------------------------------------------

// TestConfig_Codex_TierModels_SaveAndLoad verifies that a tier-model mapping stored for
// "codex" is loaded back unchanged. The config layer uses string-keyed maps; "codex"
// is a valid key without any allowlist enforcement.
func TestConfig_Codex_TierModels_SaveAndLoad(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"codex": {
				"HIGH":   "gpt-6-astra",
				"MEDIUM": "gpt-5.6-sol",
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

	codexMap, ok := got.TierModels["codex"]
	if !ok {
		t.Fatal("loaded UserConfig has no TierModels entry for \"codex\"; " +
			"the config layer must accept \"codex\" as a harness key in TierModels")
	}
	if codexMap["HIGH"] != "gpt-6-astra" {
		t.Errorf("codex HIGH: got %q, want %q", codexMap["HIGH"], "gpt-6-astra")
	}
	if codexMap["MEDIUM"] != "gpt-5.6-sol" {
		t.Errorf("codex MEDIUM: got %q, want %q", codexMap["MEDIUM"], "gpt-5.6-sol")
	}
}

// TestConfig_Codex_UnconfiguredHarness_HasNoTierModels verifies that when "codex" has
// no entry in TierModels, the loaded config returns a nil or empty map for that key.
// This is the FR-17a assertion: with no user configuration, Codex contributes no default
// tier models, so an unconfigured Codex agent gets no model silently chosen for it.
func TestConfig_Codex_UnconfiguredHarness_HasNoTierModels(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	// Save a config with tier models only for another harness, not for codex.
	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {
				"HIGH": "claude-opus-4-5",
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

	// codex must have no tier-model entry (FR-17a: no default tier models).
	if codexMap, ok := got.TierModels["codex"]; ok && len(codexMap) > 0 {
		t.Errorf("TierModels[\"codex\"] = %v, want absent or empty; "+
			"an unconfigured Codex harness must have no descriptor-supplied default tier models (FR-17a)",
			codexMap)
	}
}

// ---------------------------------------------------------------------------
// T12.4: Codex key in ToolDestinations
// ---------------------------------------------------------------------------

// TestConfig_Codex_ToolDestinations_SaveAndLoad verifies that a tool-destination mapping
// stored for "codex" is loaded back unchanged. ToolDestinations is also a harness-keyed
// map; "codex" is a valid key without any allowlist enforcement.
func TestConfig_Codex_ToolDestinations_SaveAndLoad(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"codex": {
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

	mappings, ok := got.ToolDestinations["codex"]
	if !ok || len(mappings) == 0 {
		t.Fatal("loaded UserConfig has no ToolDestinations entry for \"codex\"; " +
			"the config layer must accept \"codex\" as a harness key in ToolDestinations")
	}
	if mappings[0].Generic != "user_interaction" {
		t.Errorf("ToolDestinations[\"codex\"][0].Generic = %q, want \"user_interaction\"",
			mappings[0].Generic)
	}
	if len(mappings[0].Destinations) == 0 {
		t.Fatal("ToolDestinations[\"codex\"][0].Destinations is empty after round-trip")
	}
	if mappings[0].Destinations[0].Kind != domain.DestMain {
		t.Errorf("ToolDestinations[\"codex\"][0].Destinations[0].Kind = %q, want %q",
			mappings[0].Destinations[0].Kind, domain.DestMain)
	}
	if len(mappings[0].Destinations[0].Names) == 0 || mappings[0].Destinations[0].Names[0] != "AskUserQuestion" {
		t.Errorf("ToolDestinations[\"codex\"][0].Destinations[0].Names = %v, want [\"AskUserQuestion\"]",
			mappings[0].Destinations[0].Names)
	}
}

// ---------------------------------------------------------------------------
// T12.4: Codex key in CustomModelIDs
// ---------------------------------------------------------------------------

// TestConfig_Codex_CustomModelIDs_SaveAndLoad verifies that custom model IDs stored for
// "codex" are loaded back unchanged. CustomModelIDs is also a harness-keyed string map;
// "codex" is a valid key.
func TestConfig_Codex_CustomModelIDs_SaveAndLoad(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	wantIDs := []string{"gpt-6-astra", "my-fine-tuned-model"}
	cfg := config.UserConfig{
		CustomModelIDs: map[string][]string{
			"codex": wantIDs,
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	gotIDs, ok := got.CustomModelIDs["codex"]
	if !ok {
		t.Fatal("loaded UserConfig has no CustomModelIDs entry for \"codex\"; " +
			"the config layer must accept \"codex\" as a harness key in CustomModelIDs")
	}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("CustomModelIDs[\"codex\"] has %d entries, want %d", len(gotIDs), len(wantIDs))
	}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Errorf("CustomModelIDs[\"codex\"][%d] = %q, want %q", i, gotIDs[i], wantIDs[i])
		}
	}
}

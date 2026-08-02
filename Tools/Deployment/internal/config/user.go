package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"mosaic-deploy/internal/domain"
)

// userConfigSchemaVersion is the schema version written by this build into every
// user-config.yaml. Old-schema files are migrated to this version on load.
const userConfigSchemaVersion = "1"

// UserConfig is the per-user configuration stored in
// MosaicDeploy/config/user-config.yaml. It holds state that is personal to the operator
// and should NOT be committed to the MOSAIC repository.
//
// Per-agent model selections are not stored here (AC14.6, FR-11). Only tier-level mappings
// persist between runs. The distinction matters: tier mappings describe a user's preference
// for a class of agents (e.g. "use claude-opus for all HIGH-tier agents"), whereas per-agent
// selections describe a one-time override for a specific agent that should not persist.
type UserConfig struct {
	// SchemaVersion is the config schema version, used for forward compatibility.
	SchemaVersion string

	// TierModels maps harness id -> tier -> model id, recording the user's tier-to-model
	// selections from previous runs. Tier keys are stored verbatim and are never validated
	// against the tiers currently present in source (AC14.5): a tier that has since been
	// renamed or removed in the agent files does not cause an error on load.
	TierModels map[string]map[domain.Tier]string

	// CustomModelIDs maps harness id -> list of custom model IDs entered in previous
	// runs. These are offered as selectable options (not pre-answers) in future runs'
	// QTierModel and QAgentModel questions. The list is append-only across runs and
	// deduplicated by exact string match. This does not conflict with the existing
	// "no per-agent model persistence" design decision: these are option-pool entries,
	// not per-agent mappings.
	CustomModelIDs map[string][]string

	// ToolDestinations maps harness id -> tool-destination mappings declared by this
	// user. Entries here take precedence over the project-level declarations for the
	// same harness id and generic tool. Nil or empty means no config-declared mappings.
	ToolDestinations ToolDestinationsByHarness
}

// UserConfigStore loads and saves the per-user configuration.
type UserConfigStore interface {
	// Load reads MosaicDeploy/config/user-config.yaml. When the file is absent, Load
	// returns a zero-value UserConfig and a nil error. Other I/O or parse errors are
	// returned normally.
	Load() (UserConfig, error)

	// Save writes cfg to MosaicDeploy/config/user-config.yaml, creating the config
	// directory if it does not exist.
	Save(cfg UserConfig) error

	// Path returns the absolute path of user-config.yaml.
	Path() string
}

// ---------------------------------------------------------------------------
// YAML wire type
// ---------------------------------------------------------------------------

// userConfigYAML is the YAML-serializable form of UserConfig. TierModels uses plain
// string keys and values for YAML marshaling; conversion to/from domain.Tier happens
// in Load and Save.
type userConfigYAML struct {
	SchemaVersion    string                       `yaml:"schema_version"`
	TierModels       map[string]map[string]string `yaml:"tier_models"`
	CustomModelIDs   map[string][]string          `yaml:"custom_model_ids"`
	ToolDestinations map[string][]wireToolMapping `yaml:"tool_destinations"`
}

// ---------------------------------------------------------------------------
// userConfigStore implementation
// ---------------------------------------------------------------------------

type userConfigStore struct {
	filePath string
}

// NewUserConfigStore returns a UserConfigStore rooted at mosaicRoot.
// Config is read from and written to <mosaicRoot>/MosaicDeploy/config/user-config.yaml.
func NewUserConfigStore(mosaicRoot string) UserConfigStore {
	return &userConfigStore{
		filePath: filepath.Join(mosaicRoot, "MosaicDeploy", "config", "user-config.yaml"),
	}
}

// Path returns the absolute path of user-config.yaml.
func (s *userConfigStore) Path() string {
	return s.filePath
}

// Load reads user-config.yaml and returns the parsed UserConfig. When the file is absent,
// a zero-value UserConfig is returned without error. Old-schema files are migrated
// transparently: all tier mappings are preserved and SchemaVersion is updated to the current
// value. Tier keys are never validated against the current catalog (AC14.5).
func (s *userConfigStore) Load() (UserConfig, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return UserConfig{}, nil
		}
		return UserConfig{}, fmt.Errorf("read user config: %w", err)
	}

	var raw userConfigYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return UserConfig{}, fmt.Errorf("parse user config: %w", err)
	}

	// Migration: preserve all tier mappings and update SchemaVersion to current.
	cfg := UserConfig{
		SchemaVersion: userConfigSchemaVersion,
	}

	if len(raw.TierModels) > 0 {
		cfg.TierModels = make(map[string]map[domain.Tier]string, len(raw.TierModels))
		for harness, tiers := range raw.TierModels {
			if len(tiers) == 0 {
				continue
			}
			cfg.TierModels[harness] = make(map[domain.Tier]string, len(tiers))
			for tier, model := range tiers {
				cfg.TierModels[harness][domain.Tier(tier)] = model
			}
		}
	}

	if len(raw.CustomModelIDs) > 0 {
		cfg.CustomModelIDs = make(map[string][]string, len(raw.CustomModelIDs))
		for harness, ids := range raw.CustomModelIDs {
			if len(ids) == 0 {
				continue
			}
			copied := make([]string, len(ids))
			copy(copied, ids)
			cfg.CustomModelIDs[harness] = copied
		}
	}

	if len(raw.ToolDestinations) > 0 {
		dest, declErr := validateAndConvertToolDestinations(s.filePath, raw.ToolDestinations)
		if declErr != nil {
			return UserConfig{}, *declErr
		}
		cfg.ToolDestinations = dest
	}

	return cfg, nil
}

// Save writes cfg to user-config.yaml, creating the MosaicDeploy/config directory if
// it does not exist. A second Save call replaces the previous file entirely.
func (s *userConfigStore) Save(cfg UserConfig) error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create user config directory: %w", err)
	}

	raw := userConfigYAML{
		SchemaVersion: userConfigSchemaVersion,
	}

	if len(cfg.TierModels) > 0 {
		raw.TierModels = make(map[string]map[string]string, len(cfg.TierModels))
		for harness, tiers := range cfg.TierModels {
			if len(tiers) == 0 {
				continue
			}
			raw.TierModels[harness] = make(map[string]string, len(tiers))
			for tier, model := range tiers {
				raw.TierModels[harness][string(tier)] = model
			}
		}
	}

	if len(cfg.CustomModelIDs) > 0 {
		raw.CustomModelIDs = make(map[string][]string, len(cfg.CustomModelIDs))
		for harness, ids := range cfg.CustomModelIDs {
			if len(ids) == 0 {
				continue
			}
			copied := make([]string, len(ids))
			copy(copied, ids)
			raw.CustomModelIDs[harness] = copied
		}
	}

	if len(cfg.ToolDestinations) > 0 {
		wireMap := make(map[string][]wireToolMapping, len(cfg.ToolDestinations))
		for id, mappings := range cfg.ToolDestinations {
			wireMap[id] = toWireToolMappings(mappings)
		}
		raw.ToolDestinations = wireMap
	}

	data, err := yaml.Marshal(&raw)
	if err != nil {
		return fmt.Errorf("marshal user config: %w", err)
	}

	if err := os.WriteFile(s.filePath, data, 0o644); err != nil {
		return fmt.Errorf("write user config: %w", err)
	}
	return nil
}

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// toolConfigSchemaVersion is the schema version written by this build into every
// tool-config.yaml. Old-schema files are migrated to this version on load.
const toolConfigSchemaVersion = "1"

// ToolConfig is the project-level configuration stored in
// MosaicDeploy/config/tool-config.yaml. This file is typically committed to the MOSAIC
// repository and shared across the team.
//
// Per-agent model selections are not stored here and have no representation in this type.
// Only tier-level mappings persist between runs (AC14.6, FR-11).
type ToolConfig struct {
	// SchemaVersion is the config schema version, used for forward compatibility.
	SchemaVersion string

	// UtilityAgentAllowList holds the keys of utility agents that the user may select
	// during deployment. Only agents on this list appear in the utility-agent selection UI;
	// all others are omitted regardless of what is in the catalog. An empty list means no
	// utility agents are selectable (FR-8).
	UtilityAgentAllowList []string

	// AllowExternalModules controls whether external harness modules (binaries discovered
	// under MosaicDeploy/harnesses/) may be used. Defaults to false so that external
	// module execution requires an explicit operator decision.
	AllowExternalModules bool

	// LogRetentionRuns is the maximum number of run log entries to keep in the history
	// file. 0 means unbounded (keep all). Defaults to 0.
	LogRetentionRuns int

	// ToolDestinations maps harness id -> tool-destination mappings declared at the
	// project level. Nil or empty means no config-declared mappings.
	ToolDestinations ToolDestinationsByHarness
}

// ToolConfigStore loads the project-level tool configuration.
type ToolConfigStore interface {
	// Load reads MosaicDeploy/config/tool-config.yaml. When the file is absent, Load
	// returns DefaultToolConfig() and a nil error (AC14.4). Other I/O or parse errors
	// are returned normally.
	Load() (ToolConfig, error)

	// Path returns the absolute path of tool-config.yaml.
	Path() string
}

// ---------------------------------------------------------------------------
// YAML wire type
// ---------------------------------------------------------------------------

type toolConfigYAML struct {
	SchemaVersion        string   `yaml:"schema_version"`
	UtilityAgentAllowList []string `yaml:"utility_agent_allow_list"`
	AllowExternalModules bool     `yaml:"allow_external_modules"`
	LogRetentionRuns     int      `yaml:"log_retention_runs"`
}

// ---------------------------------------------------------------------------
// toolConfigStore implementation
// ---------------------------------------------------------------------------

type toolConfigStore struct {
	filePath string
}

// NewToolConfigStore returns a ToolConfigStore rooted at mosaicRoot.
// Config is read from <mosaicRoot>/MosaicDeploy/config/tool-config.yaml.
func NewToolConfigStore(mosaicRoot string) ToolConfigStore {
	return &toolConfigStore{
		filePath: filepath.Join(mosaicRoot, "MosaicDeploy", "config", "tool-config.yaml"),
	}
}

// Path returns the absolute path of tool-config.yaml.
func (s *toolConfigStore) Path() string {
	return s.filePath
}

// Load reads tool-config.yaml and returns the parsed ToolConfig. When the file is absent,
// DefaultToolConfig() is returned without error. Old-schema files are migrated transparently:
// all recognisable field values are preserved and SchemaVersion is updated to the current value.
func (s *toolConfigStore) Load() (ToolConfig, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultToolConfig(), nil
		}
		return ToolConfig{}, fmt.Errorf("read tool config: %w", err)
	}

	var raw toolConfigYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ToolConfig{}, fmt.Errorf("parse tool config: %w", err)
	}

	// Migration: preserve all recognisable field values and update SchemaVersion to current.
	cfg := ToolConfig{
		SchemaVersion:        toolConfigSchemaVersion,
		UtilityAgentAllowList: raw.UtilityAgentAllowList,
		AllowExternalModules: raw.AllowExternalModules,
		LogRetentionRuns:     raw.LogRetentionRuns,
	}
	return cfg, nil
}

// DefaultToolConfig returns the documented defaults applied when tool-config.yaml is absent.
//
// # Allow-list decision (AC14.7)
//
// The default allow-list includes all six utility agents shipped with MOSAIC. Rationale:
//
//   - Opting out of a specific agent is simpler UX than opting in: a user who never edits
//     the config still has access to every utility agent and does not need to discover the
//     allow-list mechanism before utility agents appear.
//   - The six utility agents are scoped to MOSAIC workflow operations; none of them perform
//     arbitrary user-defined actions, so the security risk of enabling them by default is low.
//   - Operators who want to restrict the set update UtilityAgentAllowList in tool-config.yaml.
//
// The six agents and their justification for default inclusion:
//   - anthropic-subagent-creator: common in agentic orchestration patterns
//   - harness-bug-hunter: self-service debugging; expected to be used during onboarding
//   - orchestration-architect: core workflow design capability
//   - system-prompt-capturer: diagnostic tool, low risk
//   - transformation: generic MOSAIC transformation capability
//   - workflow-creator: core workflow design capability
//
// # External-module default
//
// AllowExternalModules defaults to false. External modules execute an arbitrary binary,
// which represents an elevated trust boundary. Enabling them by default would be a security
// regression; the operator must set AllowExternalModules: true in tool-config.yaml.
//
// # Log retention default
//
// LogRetentionRuns defaults to 0 (unbounded). Log files are small; retaining all history is
// safer than silently losing it. Operators who need bounded history set LogRetentionRuns in
// tool-config.yaml.
func DefaultToolConfig() ToolConfig {
	return ToolConfig{
		SchemaVersion: toolConfigSchemaVersion,
		UtilityAgentAllowList: []string{
			"anthropic-subagent-creator",
			"harness-bug-hunter",
			"orchestration-architect",
			"system-prompt-capturer",
			"transformation",
			"workflow-creator",
		},
		AllowExternalModules: false,
		LogRetentionRuns:     0,
	}
}

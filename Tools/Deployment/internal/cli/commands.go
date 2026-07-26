package cli

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"mosaic-deploy/internal/domain"
)

// selectionsFile is the YAML schema for the --selections flag file. Fields are parsed
// leniently: unknown keys are silently ignored for forward compatibility (AC19.8).
type selectionsFile struct {
	Workflows     []string          `yaml:"workflows"`
	UtilityAgents []string          `yaml:"utility_agents"`
	Hooks         []string          `yaml:"hooks"`
	TierModels    map[string]string `yaml:"tier_models"`
}

// parseSelectionsFile reads and parses a YAML selections file at path.
// Returns (file, ExitUsage, err) when path does not exist.
// Returns (file, ExitFailure, err) when the file exists but contains invalid YAML.
// Returns (file, 0, nil) on success.
func parseSelectionsFile(path string) (selectionsFile, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return selectionsFile{}, ExitUsage, fmt.Errorf("--selections file not found: %w", err)
	}
	var sf selectionsFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return selectionsFile{}, ExitFailure, fmt.Errorf("invalid YAML in --selections file: %w", err)
	}
	return sf, 0, nil
}

// tierModelsFromFile converts the tier_models map from a selectionsFile into the
// map[domain.Tier]string type used by DeployRequest and UpdateRequest.
func tierModelsFromFile(sf selectionsFile) map[domain.Tier]string {
	if len(sf.TierModels) == 0 {
		return nil
	}
	m := make(map[domain.Tier]string, len(sf.TierModels))
	for k, v := range sf.TierModels {
		m[domain.Tier(k)] = v
	}
	return m
}

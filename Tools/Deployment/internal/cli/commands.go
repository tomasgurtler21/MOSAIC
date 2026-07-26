package cli

import (
	"fmt"
	"os"
	"strings"

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

// PreAnswersFromSelectionsFile reads the YAML selections file at path and converts it
// into a PreAnswers value encoding the file's workflow, utility-agent, hook, and
// tier-model selections in the question-ID / subject scheme that NewInteraction expects.
//
//   - Workflow, utility-agent, and hook IDs are comma-joined into a run-level (subject "")
//     answer for their respective QuestionIDs (QWorkflows, QUtilityAgents, QHooks).
//   - Tier-model entries produce one QTierModel answer per tier, keyed by the tier name as
//     the subject.
//   - Absent or empty sections produce no entry in the returned PreAnswers.Values map.
//
// Returns an error wrapping the underlying OS error when path does not exist (callers may
// use errors.Is to detect fs.ErrNotExist), and a plain error for invalid YAML content.
func PreAnswersFromSelectionsFile(path string) (PreAnswers, error) {
	sf, _, err := parseSelectionsFile(path)
	if err != nil {
		return PreAnswers{}, err
	}

	values := make(map[domain.QuestionID]map[string]string)

	if len(sf.Workflows) > 0 {
		values[domain.QWorkflows] = map[string]string{"": strings.Join(sf.Workflows, ",")}
	}
	if len(sf.UtilityAgents) > 0 {
		values[domain.QUtilityAgents] = map[string]string{"": strings.Join(sf.UtilityAgents, ",")}
	}
	if len(sf.Hooks) > 0 {
		values[domain.QHooks] = map[string]string{"": strings.Join(sf.Hooks, ",")}
	}
	if len(sf.TierModels) > 0 {
		tierMap := make(map[string]string, len(sf.TierModels))
		for tier, model := range sf.TierModels {
			tierMap[tier] = model
		}
		values[domain.QTierModel] = tierMap
	}

	if len(values) == 0 {
		return PreAnswers{}, nil
	}
	return PreAnswers{Values: values}, nil
}

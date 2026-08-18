package interceptor

import (
	"encoding/json"
	"fmt"
	"os"

	"mosaic-agent-test/internal/domain"
)

// LoadRegistry reads the active stub registry setup resolved and
// token-expanded into the control directory. A malformed document is
// returned as an error the caller contains, never silently treated as an
// empty registry — an empty registry would turn every stubbed call into an
// unmatched one and rewrite the run's meaning.
func LoadRegistry(controlDir string) (domain.StubRegistry, error) {
	sb := domain.Sandbox{ControlDir: controlDir}
	data, err := os.ReadFile(sb.ActiveRegistryPath())
	if err != nil {
		return domain.StubRegistry{}, fmt.Errorf("interceptor: reading active registry %s: %w", sb.ActiveRegistryPath(), err)
	}

	var reg domain.StubRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return domain.StubRegistry{}, fmt.Errorf("interceptor: parsing active registry %s: %w", sb.ActiveRegistryPath(), err)
	}
	return reg, nil
}

// LoadGroups reads the test's declared parallel groups from the control
// directory, so an interceptor can tag a record with its group without
// loading the whole test definition.
func LoadGroups(controlDir string) ([]domain.ParallelGroup, error) {
	sb := domain.Sandbox{ControlDir: controlDir}
	data, err := os.ReadFile(sb.ParallelGroupsPath())
	if err != nil {
		return nil, fmt.Errorf("interceptor: reading parallel groups %s: %w", sb.ParallelGroupsPath(), err)
	}

	var groups []domain.ParallelGroup
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, fmt.Errorf("interceptor: parsing parallel groups %s: %w", sb.ParallelGroupsPath(), err)
	}
	return groups, nil
}

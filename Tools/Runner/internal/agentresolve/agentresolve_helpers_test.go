package agentresolve_test

// Tests for agentresolve.ResolveAll.
//
// Coverage:
//
//   Happy path - single agent:
//   - Directory with one .md file; requesting that agent returns a non-nil map.
//   - AgentReference.Identifier matches the requested identifier.
//   - AgentReference.DefinitionPath ends with the expected filename.
//   - Returned map is keyed by the agent identifier.
//
//   Happy path - multiple agents:
//   - Directory with three .md files; requesting all three returns a map with
//     three entries.
//   - Each entry carries the correct Identifier.
//   - Each entry's DefinitionPath names a file that exists in the directory.
//   - Requesting a subset of identifiers returns only the requested subset.
//
//   Happy path - empty identifiers:
//   - ResolveAll with an empty identifiers slice returns an empty map and no error.
//
//   Refusal - missing agent:
//   - Requesting an identifier that has no matching .md file returns an error.
//   - The error wraps *domain.RefusalError.
//   - RefusalError.Component is "agentresolve".
//   - RefusalError.Resource or message names the missing identifier.
//   - RefusalError.Resource or message names the directory searched.
//
//   Note on duplicate-identifier guard: the matching rule (exact filename stem)
//   makes it impossible to have two files with the same stem in the same
//   directory. The guard exists as a safety net for future changes to the
//   matching rule and cannot be triggered by fixture-based tests.

import (
	"errors"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
)

const agentresolveTestdataDir = "../../testdata/agentresolve"

// agentsDir returns the path to the fixture directory containing agent files.
func agentsDir() string {
	return filepath.Join(agentresolveTestdataDir, "agents")
}

// asRefusalError asserts that err wraps a *domain.RefusalError and returns it.
// Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

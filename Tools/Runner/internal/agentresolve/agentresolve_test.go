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
	"strings"
	"testing"

	"mosaic-run/internal/agentresolve"
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

// --- Happy path: single agent ---

func TestResolveAll_SingleAgent_ReturnsNonNilMap(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})

	if err != nil {
		t.Fatalf("ResolveAll returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("ResolveAll returned nil map")
	}
}

func TestResolveAll_SingleAgent_MapContainsRequestedKey(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	if _, ok := result["test-writer-tdd"]; !ok {
		t.Errorf("result map must contain key %q", "test-writer-tdd")
	}
}

func TestResolveAll_SingleAgent_AgentReference_IdentifierMatches(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	ref := result["test-writer-tdd"]
	if ref.Identifier != "test-writer-tdd" {
		t.Errorf("AgentReference.Identifier: want %q, got %q", "test-writer-tdd", ref.Identifier)
	}
}

func TestResolveAll_SingleAgent_AgentReference_DefinitionPath_EndsWithFilename(t *testing.T) {
	// The resolved path must end with "test-writer-tdd.md" so that the harness
	// adapter can locate the agent definition file.
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	ref := result["test-writer-tdd"]
	if !strings.HasSuffix(ref.DefinitionPath, "test-writer-tdd.md") {
		t.Errorf("DefinitionPath must end with %q; got %q", "test-writer-tdd.md", ref.DefinitionPath)
	}
}

// --- Happy path: multiple agents ---

func TestResolveAll_MultipleAgents_ReturnsAllRequested(t *testing.T) {
	// The agents directory contains test-writer-tdd.md, implementation-tdd.md,
	// and build-review.md.
	ids := []string{"test-writer-tdd", "implementation-tdd", "build-review"}
	result, err := agentresolve.ResolveAll(agentsDir(), ids)

	if err != nil {
		t.Fatalf("ResolveAll returned unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("want 3 entries in result map, got %d", len(result))
	}
}

func TestResolveAll_MultipleAgents_EachIdentifierCorrect(t *testing.T) {
	ids := []string{"test-writer-tdd", "implementation-tdd", "build-review"}
	result, err := agentresolve.ResolveAll(agentsDir(), ids)
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	for _, id := range ids {
		ref, ok := result[id]
		if !ok {
			t.Errorf("result map missing key %q", id)
			continue
		}
		if ref.Identifier != id {
			t.Errorf("result[%q].Identifier: want %q, got %q", id, id, ref.Identifier)
		}
	}
}

func TestResolveAll_MultipleAgents_EachDefinitionPathNamesCorrectFile(t *testing.T) {
	ids := []string{"test-writer-tdd", "implementation-tdd", "build-review"}
	result, err := agentresolve.ResolveAll(agentsDir(), ids)
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	for _, id := range ids {
		ref := result[id]
		expectedSuffix := id + ".md"
		if !strings.HasSuffix(ref.DefinitionPath, expectedSuffix) {
			t.Errorf("result[%q].DefinitionPath must end with %q; got %q", id, expectedSuffix, ref.DefinitionPath)
		}
	}
}

func TestResolveAll_Subset_ReturnsOnlyRequestedEntries(t *testing.T) {
	// Requesting only one of the three available agents must return a map
	// with exactly one entry, not all agents in the directory.
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"build-review"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("requesting 1 identifier must return 1-entry map, got %d", len(result))
	}
	if _, ok := result["build-review"]; !ok {
		t.Error("result map must contain the requested key \"build-review\"")
	}
}

// --- Happy path: empty identifiers ---

func TestResolveAll_EmptyIdentifiers_ReturnsEmptyMap(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{})

	if err != nil {
		t.Fatalf("ResolveAll with empty identifiers must not return an error; got: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("want empty map, got %d entries", len(result))
	}
}

// --- Refusal: missing agent ---

func TestResolveAll_MissingAgent_ReturnsError(t *testing.T) {
	// "planner" does not exist in the agents directory.
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	if err == nil {
		t.Fatal("requesting a missing agent must return an error")
	}
}

func TestResolveAll_MissingAgent_ReturnsRefusalError(t *testing.T) {
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	asRefusalError(t, err)
}

func TestResolveAll_MissingAgent_RefusalError_ComponentIsAgentresolve(t *testing.T) {
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}

func TestResolveAll_MissingAgent_RefusalError_NamesIdentifier(t *testing.T) {
	// The error must name "planner" so the user knows which agent definition
	// is missing, without having to inspect the directory manually.
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "planner") {
		t.Errorf("RefusalError must name the missing identifier %q; got %q", "planner", re.Error())
	}
}

func TestResolveAll_MissingAgent_RefusalError_NamesDirectory(t *testing.T) {
	// The error must name the directory that was searched so the user knows
	// where to place the missing agent definition file.
	dir := agentsDir()
	_, err := agentresolve.ResolveAll(dir, []string{"planner"})

	re := asRefusalError(t, err)
	// The directory name or a recognisable part of it must appear in the error.
	if !strings.Contains(re.Error(), "agents") {
		t.Errorf("RefusalError must mention the searched directory; got %q", re.Error())
	}
}

func TestResolveAll_OneMissingOnePresent_ReturnsRefusalError(t *testing.T) {
	// Even when some identifiers resolve successfully, a single missing one
	// must produce a refusal error rather than silently returning a partial map.
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd", "planner"})

	if err == nil {
		t.Fatal("a single missing agent among multiple requested must return an error")
	}
	asRefusalError(t, err)
}

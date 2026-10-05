package testcatalog_test

// Tests for testcatalog.Load and the Catalog query methods.
//
// Coverage:
//   - Load succeeds for a valid catalog directory containing multiple workflow files.
//   - Load returns an error when the catalog root directory does not exist.
//   - Load returns an error when the catalog contains no .md files.
//   - Load returns an error when a .md file has no YAML frontmatter delimiters.
//   - Load returns an error when a .md file has malformed YAML in its frontmatter.
//   - Load returns an error when a .md file has valid YAML but is missing the modes field.
//   - Load returns an error when a .md file declares an empty modes list.
//   - Load returns an error when smoke_set contains a mode not present in modes.
//   - Load returns an error when the Fixtures/{id}/ directory does not exist.
//   - Load skips the Fixtures/ subdirectory (does not treat it as a workflow .md).
//   - Workflows() returns all entries sorted by workflow ID.
//   - Workflows() produces one entry per declared mode per workflow.
//   - FullSuite() returns all (workflow, mode) pairs sorted by ID then mode.
//   - SmokeSet() returns only entries whose InSmokeSet field is true.
//   - SmokeSet() excludes (workflow, mode) pairs not in the smoke_set declaration.
//   - WorkflowIDs() returns sorted list of all workflow IDs.
//   - WorkflowByID() returns all entries for a known ID with Mode and AllModes set.
//   - WorkflowByID() returns ErrWorkflowNotFound for an unknown ID.
//   - WorkflowModes() returns sorted modes for a multi-mode workflow.
//   - WorkflowModes() returns the single mode for a single-mode workflow.
//   - WorkflowModes() returns ErrWorkflowNotFound for an unknown ID.
//   - FixturePath is set to the absolute path of the workflow's fixture directory.
//   - InSmokeSet is true for each (workflow, mode) pair declared in smoke_set.
//   - InSmokeSet is false for (workflow, mode) pairs not declared in smoke_set.
//   - AllModes lists every mode the workflow declares, regardless of which mode
//     this entry represents.
//   - SidecarPath returns a path ending with {workflowID}-{mode}.expected.json
//     under the catalog root.

import (
	"path/filepath"
	"testing"
)

// validCatalog returns the path to the valid multi-workflow testdata fixture.
func validCatalog(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "valid-catalog"))
	if err != nil {
		t.Fatalf("failed to resolve valid-catalog fixture path: %v", err)
	}
	return p
}

// fixtureDir returns the absolute path to a named testdata subdirectory.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to resolve fixture dir %q: %v", name, err)
	}
	return p
}

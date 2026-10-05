package orchfile_test

// Tests for orchfile.EnumerateWorkflows and orchfile.GetWorkflow.
//
// Coverage:
//   - EnumerateWorkflows finds one region in a hand-assembled file with a bare
//     top-level <Workflow type="core" name="{id}" version="{ver}"> tag.
//   - The returned region carries the correct identifier extracted from the
//     tag's name attribute after the "Workflow:" compound-name reassembly.
//   - The returned region carries the version string from the tag's version attribute.
//   - The returned region Content contains the bytes between the boundary tags
//     but does not include the boundary tag lines themselves.
//   - EnumerateWorkflows finds a workflow nested at depth inside
//     <AvailableWorkflows type="project"> inside <Identity type="core">, the
//     structure of a deployed orchestrator agent file.
//   - EnumerateWorkflows returns two regions from a file with two workflow
//     sections, in declaration order.
//   - Refusal: missing file → *domain.RefusalError naming the path.
//   - Refusal: no workflow regions → *domain.RefusalError.
//   - Refusal: version attribute absent → *domain.RefusalError naming the region.
//   - Refusal: duplicate workflow identifiers → *domain.RefusalError naming
//     the duplicate identifier.
//   - Refusal: empty identifier (tag with empty name attribute value)
//     → *domain.RefusalError.
//   - GetWorkflow returns the correct region for a known identifier.
//   - GetWorkflow region Content is accessible.
//   - GetWorkflow: absent identifier → *domain.RefusalError.

import (
	"errors"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
)

const orchfileTestdataDir = "../../testdata/orchfile"

// orchfileFixture returns the absolute path to a named fixture file in the
// orchfile testdata directory.
func orchfileFixture(name string) string {
	return filepath.Join(orchfileTestdataDir, name)
}

// asRefusalError asserts that err is (or wraps) a *domain.RefusalError and
// returns it for further inspection. Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

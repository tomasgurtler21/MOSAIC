package testcatalog_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/testcatalog"
)

// ---- Load: success ----

func TestLoad_ValidCatalog_ReturnsNilError(t *testing.T) {
	_, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
}

// ---- Load: catalog-level errors ----

func TestLoad_NonExistentRoot_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for non-existent catalog root, got nil")
	}
}

func TestLoad_EmptyCatalog_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "empty-catalog"))
	if err == nil {
		t.Fatal("expected error for catalog with no workflow .md files, got nil")
	}
}

// ---- Load: per-file errors ----

func TestLoad_MissingFrontmatter_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "missing-frontmatter"))
	if err == nil {
		t.Fatal("expected error for workflow file with no frontmatter, got nil")
	}
}

func TestLoad_InvalidYAML_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "bad-yaml"))
	if err == nil {
		t.Fatal("expected error for workflow file with malformed YAML frontmatter, got nil")
	}
}

func TestLoad_MissingModesField_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "no-modes-field"))
	if err == nil {
		t.Fatal("expected error for workflow file missing the modes field, got nil")
	}
}

func TestLoad_EmptyModesList_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "empty-modes"))
	if err == nil {
		t.Fatal("expected error for workflow file with modes declared as an empty list, got nil")
	}
}

func TestLoad_SmokeSetEntryNotInModes_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "invalid-smoke-set"))
	if err == nil {
		t.Fatal("expected error when smoke_set contains a mode not in the modes list, got nil")
	}
}

func TestLoad_MissingFixtureDirectory_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "missing-fixture"))
	if err == nil {
		t.Fatal("expected error when a workflow's Fixtures/{id}/ directory is absent, got nil")
	}
}

func TestLoad_UnknownModeValue_ReturnsError(t *testing.T) {
	// The approved mode vocabulary is {auto, auto-review, orchestrated}.
	// A workflow file declaring a mode outside this set must cause Load to return an error.
	_, err := testcatalog.Load(fixtureDir(t, "unknown-mode"))
	if err == nil {
		t.Fatal("expected error for workflow file declaring a mode not in the approved vocabulary, got nil")
	}
}

// ---- Fixtures/ subdirectory is not treated as a workflow ----

func TestLoad_IgnoresFixturesSubdirectory(t *testing.T) {
	// valid-catalog contains a Fixtures/ subdirectory in Workflows/MosaicTest/.
	// The reader must not attempt to parse it as a .md file.
	// A successful Load without error proves the Fixtures/ dir was correctly skipped.
	_, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load failed, possibly treated Fixtures/ as a workflow: %v", err)
	}
}

func TestLoad_WorkflowCountExcludesFixturesDir(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := cat.WorkflowIDs()
	for _, id := range ids {
		if id == "Fixtures" || strings.EqualFold(id, "fixtures") {
			t.Errorf("WorkflowIDs() contains %q -- Fixtures/ directory was not skipped", id)
		}
	}
}

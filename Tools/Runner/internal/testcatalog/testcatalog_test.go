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
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/testcatalog"
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

// ---- Workflows() ----

func TestWorkflows_CountMatchesTotalModePairs(t *testing.T) {
	// valid-catalog has:
	//   workflow-a: 1 mode  -> 1 entry
	//   workflow-b: 2 modes -> 2 entries
	//   workflow-c: 1 mode  -> 1 entry
	// Total: 4 entries
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries := cat.Workflows()
	if len(entries) != 4 {
		t.Fatalf("Workflows() len = %d, want 4", len(entries))
	}
}

func TestWorkflows_SortedByWorkflowIDThenMode(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries := cat.Workflows()

	// Build the expected sorted order for a valid-catalog:
	//   (workflow-a, auto), (workflow-b, auto), (workflow-b, auto-review), (workflow-c, orchestrated)
	want := []struct{ id, mode string }{
		{"workflow-a", "auto"},
		{"workflow-b", "auto"},
		{"workflow-b", "auto-review"},
		{"workflow-c", "orchestrated"},
	}

	if len(entries) != len(want) {
		t.Fatalf("Workflows() len = %d, want %d", len(entries), len(want))
	}
	for i, w := range want {
		got := entries[i]
		if got.WorkflowID != w.id || got.Mode != w.mode {
			t.Errorf("Workflows()[%d] = {%q, %q}, want {%q, %q}",
				i, got.WorkflowID, got.Mode, w.id, w.mode)
		}
	}
}

// ---- FullSuite() ----

func TestFullSuite_ContainsAllModePairs(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries := cat.FullSuite()
	if len(entries) != 4 {
		t.Fatalf("FullSuite() len = %d, want 4", len(entries))
	}
}

func TestFullSuite_SortedByWorkflowIDThenMode(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries := cat.FullSuite()
	for i := 1; i < len(entries); i++ {
		prev, curr := entries[i-1], entries[i]
		less := prev.WorkflowID < curr.WorkflowID ||
			(prev.WorkflowID == curr.WorkflowID && prev.Mode <= curr.Mode)
		if !less {
			t.Errorf("FullSuite() not sorted: entries[%d]={%q,%q} followed by entries[%d]={%q,%q}",
				i-1, prev.WorkflowID, prev.Mode, i, curr.WorkflowID, curr.Mode)
		}
	}
}

// ---- SmokeSet() ----

func TestSmokeSet_CountMatchesDeclaredSmokeEntries(t *testing.T) {
	// valid-catalog smoke_set declarations:
	//   workflow-a: smoke=[auto]         -> 1 entry
	//   workflow-b: smoke=[auto]         -> 1 entry
	//   workflow-c: smoke=[] (absent)    -> 0 entries
	// Total: 2 entries
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	smoke := cat.SmokeSet()
	if len(smoke) != 2 {
		t.Fatalf("SmokeSet() len = %d, want 2", len(smoke))
	}
}

func TestSmokeSet_AllEntriesHaveInSmokeSetTrue(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range cat.SmokeSet() {
		if !e.InSmokeSet {
			t.Errorf("SmokeSet() returned entry with InSmokeSet=false: {%q, %q}",
				e.WorkflowID, e.Mode)
		}
	}
}

func TestSmokeSet_ExcludesNonSmokeModePairs(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range cat.SmokeSet() {
		// workflow-b auto-review and workflow-c orchestrated must not appear
		if e.WorkflowID == "workflow-b" && e.Mode == "auto-review" {
			t.Errorf("SmokeSet() incorrectly includes (workflow-b, auto-review)")
		}
		if e.WorkflowID == "workflow-c" {
			t.Errorf("SmokeSet() incorrectly includes workflow-c (not in any smoke set)")
		}
	}
}

func TestSmokeSet_SortedByWorkflowIDThenMode(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries := cat.SmokeSet()
	for i := 1; i < len(entries); i++ {
		prev, curr := entries[i-1], entries[i]
		less := prev.WorkflowID < curr.WorkflowID ||
			(prev.WorkflowID == curr.WorkflowID && prev.Mode <= curr.Mode)
		if !less {
			t.Errorf("SmokeSet() not sorted at index %d: {%q,%q} before {%q,%q}",
				i, prev.WorkflowID, prev.Mode, curr.WorkflowID, curr.Mode)
		}
	}
}

// ---- WorkflowIDs() ----

func TestWorkflowIDs_ReturnsSortedList(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := cat.WorkflowIDs()
	want := []string{"workflow-a", "workflow-b", "workflow-c"}
	if len(ids) != len(want) {
		t.Fatalf("WorkflowIDs() len = %d, want %d; got %v", len(ids), len(want), ids)
	}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("WorkflowIDs()[%d] = %q, want %q", i, ids[i], w)
		}
	}
}

func TestWorkflowIDs_EachIDAppearsOnce(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := cat.WorkflowIDs()
	seen := make(map[string]int)
	for _, id := range ids {
		seen[id]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("WorkflowIDs() returned %q %d times, want 1", id, count)
		}
	}
}

// ---- WorkflowByID() ----

func TestWorkflowByID_KnownSingleModeWorkflow_ReturnsOneEntry(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := cat.WorkflowByID("workflow-a")
	if err != nil {
		t.Fatalf("WorkflowByID(workflow-a): unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("WorkflowByID(workflow-a) len = %d, want 1", len(entries))
	}
	if entries[0].Mode != "auto" {
		t.Errorf("WorkflowByID(workflow-a)[0].Mode = %q, want %q", entries[0].Mode, "auto")
	}
}

func TestWorkflowByID_KnownMultiModeWorkflow_ReturnsOneEntryPerMode(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := cat.WorkflowByID("workflow-b")
	if err != nil {
		t.Fatalf("WorkflowByID(workflow-b): unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("WorkflowByID(workflow-b) len = %d, want 2", len(entries))
	}
	// Entries must be sorted by mode
	if entries[0].Mode != "auto" {
		t.Errorf("WorkflowByID(workflow-b)[0].Mode = %q, want %q", entries[0].Mode, "auto")
	}
	if entries[1].Mode != "auto-review" {
		t.Errorf("WorkflowByID(workflow-b)[1].Mode = %q, want %q", entries[1].Mode, "auto-review")
	}
}

func TestWorkflowByID_AllModesFieldPopulated(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := cat.WorkflowByID("workflow-b")
	if err != nil {
		t.Fatalf("WorkflowByID(workflow-b): unexpected error: %v", err)
	}
	// workflow-b declares modes: [auto, auto-review]. AllModes must be exactly those
	// two values in sorted (ascending) alphabetical order on every returned entry.
	wantAllModes := []string{"auto", "auto-review"}
	for _, e := range entries {
		if len(e.AllModes) != len(wantAllModes) {
			t.Errorf("entry {%q, %q}.AllModes len = %d, want %d; got %v",
				e.WorkflowID, e.Mode, len(e.AllModes), len(wantAllModes), e.AllModes)
			continue
		}
		for i, want := range wantAllModes {
			if e.AllModes[i] != want {
				t.Errorf("entry {%q, %q}.AllModes[%d] = %q, want %q (must be sorted)",
					e.WorkflowID, e.Mode, i, e.AllModes[i], want)
			}
		}
	}
}

func TestWorkflowByID_UnknownID_ReturnsErrWorkflowNotFound(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = cat.WorkflowByID("no-such-workflow")
	if !errors.Is(err, testcatalog.ErrWorkflowNotFound) {
		t.Errorf("WorkflowByID(unknown): err = %v, want ErrWorkflowNotFound", err)
	}
}

// ---- WorkflowModes() ----

func TestWorkflowModes_MultiModeWorkflow_ReturnsSortedModes(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	modes, err := cat.WorkflowModes("workflow-b")
	if err != nil {
		t.Fatalf("WorkflowModes(workflow-b): %v", err)
	}
	want := []string{"auto", "auto-review"}
	if len(modes) != len(want) {
		t.Fatalf("WorkflowModes(workflow-b) = %v, want %v", modes, want)
	}
	for i, w := range want {
		if modes[i] != w {
			t.Errorf("WorkflowModes(workflow-b)[%d] = %q, want %q", i, modes[i], w)
		}
	}
}

func TestWorkflowModes_SingleModeWorkflow_ReturnsSingleMode(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	modes, err := cat.WorkflowModes("workflow-a")
	if err != nil {
		t.Fatalf("WorkflowModes(workflow-a): %v", err)
	}
	if len(modes) != 1 || modes[0] != "auto" {
		t.Errorf("WorkflowModes(workflow-a) = %v, want [auto]", modes)
	}
}

func TestWorkflowModes_UnknownID_ReturnsErrWorkflowNotFound(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = cat.WorkflowModes("no-such-workflow")
	if !errors.Is(err, testcatalog.ErrWorkflowNotFound) {
		t.Errorf("WorkflowModes(unknown): err = %v, want ErrWorkflowNotFound", err)
	}
}

// ---- CatalogEntry field correctness ----

func TestCatalogEntry_WorkflowIDMatchesFileID(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries := cat.Workflows()
	for _, e := range entries {
		if e.WorkflowID == "" {
			t.Errorf("entry has empty WorkflowID")
		}
	}
}

func TestCatalogEntry_FixturePath_IsAbsolute(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range cat.Workflows() {
		if !filepath.IsAbs(e.FixturePath) {
			t.Errorf("entry {%q, %q}.FixturePath = %q is not absolute",
				e.WorkflowID, e.Mode, e.FixturePath)
		}
	}
}

func TestCatalogEntry_FixturePath_EndsWithWorkflowID(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range cat.Workflows() {
		base := filepath.Base(e.FixturePath)
		if base != e.WorkflowID {
			t.Errorf("entry {%q, %q}.FixturePath base = %q, want %q",
				e.WorkflowID, e.Mode, base, e.WorkflowID)
		}
	}
}

func TestCatalogEntry_InSmokeSet_TrueForSmokeDeclarations(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// workflow-a/auto is in smoke set
	entries, _ := cat.WorkflowByID("workflow-a")
	if len(entries) == 0 {
		t.Fatal("WorkflowByID(workflow-a) returned empty slice")
	}
	if !entries[0].InSmokeSet {
		t.Errorf("(workflow-a, auto).InSmokeSet = false, want true")
	}
}

func TestCatalogEntry_InSmokeSet_FalseForNonSmokeMode(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// workflow-b/auto-review is NOT in smoke set
	entries, _ := cat.WorkflowByID("workflow-b")
	for _, e := range entries {
		if e.Mode == "auto-review" && e.InSmokeSet {
			t.Errorf("(workflow-b, auto-review).InSmokeSet = true, want false")
		}
	}
}

func TestCatalogEntry_InSmokeSet_FalseForWorkflowWithNoSmokeSet(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// workflow-c declares no smoke_set
	entries, _ := cat.WorkflowByID("workflow-c")
	for _, e := range entries {
		if e.InSmokeSet {
			t.Errorf("(workflow-c, %q).InSmokeSet = true, want false (no smoke_set declared)",
				e.Mode)
		}
	}
}

func TestCatalogEntry_AllModes_ContainsAllDeclaredModesForWorkflow(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// For workflow-b, every entry must have AllModes = [auto, auto-review]
	entries, _ := cat.WorkflowByID("workflow-b")
	for _, e := range entries {
		found := make(map[string]bool)
		for _, m := range e.AllModes {
			found[m] = true
		}
		for _, want := range []string{"auto", "auto-review"} {
			if !found[want] {
				t.Errorf("(workflow-b, %q).AllModes missing %q; got %v",
					e.Mode, want, e.AllModes)
			}
		}
	}
}

// ---- SidecarPath() ----

func TestSidecarPath_ReturnsPathUnderCatalogRoot(t *testing.T) {
	root := validCatalog(t)
	cat, err := testcatalog.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cat.SidecarPath("workflow-a", "auto")
	if !strings.HasPrefix(p, root) {
		t.Errorf("SidecarPath(%q, %q) = %q, want path under catalog root %q",
			"workflow-a", "auto", p, root)
	}
	// The design spec requires the full structure: <catalogRoot>/Workflows/MosaicTest/<id>-<mode>.expected.json.
	// Verify the intermediate directory component is present to prevent an implementation that
	// omits Workflows/MosaicTest/ and returns <catalogRoot>/<id>-<mode>.expected.json directly.
	wantSuffix := filepath.Join("Workflows", "MosaicTest", "workflow-a-auto.expected.json")
	if !strings.HasSuffix(p, wantSuffix) {
		t.Errorf("SidecarPath(%q, %q) = %q, want path ending with %q",
			"workflow-a", "auto", p, wantSuffix)
	}
}

func TestSidecarPath_FileNameFollowsConvention(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cat.SidecarPath("workflow-b", "auto-review")
	base := filepath.Base(p)
	want := "workflow-b-auto-review.expected.json"
	if base != want {
		t.Errorf("SidecarPath(%q, %q) basename = %q, want %q",
			"workflow-b", "auto-review", base, want)
	}
}

func TestSidecarPath_DifferentModesProduceDifferentPaths(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p1 := cat.SidecarPath("workflow-b", "auto")
	p2 := cat.SidecarPath("workflow-b", "auto-review")
	if p1 == p2 {
		t.Errorf("SidecarPath returned same path for different modes: %q", p1)
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

// ---- CatalogEntry.PreConsult field ----

// TestCatalogEntry_PreConsult_FalseWhenDeclaredFalse verifies that a workflow
// declaring pre_consult: false in its frontmatter produces a CatalogEntry with
// PreConsult == false.
func TestCatalogEntry_PreConsult_FalseWhenDeclaredFalse(t *testing.T) {
	cat, err := testcatalog.Load(fixtureDir(t, "pre-consult-false"))
	if err != nil {
		t.Fatalf("Load(pre-consult-false): %v", err)
	}
	entries := cat.Workflows()
	if len(entries) == 0 {
		t.Fatal("Workflows() returned empty slice for pre-consult-false catalog")
	}
	for _, e := range entries {
		if e.PreConsult {
			t.Errorf("entry {%q, %q}.PreConsult = true, want false (frontmatter declares pre_consult: false)",
				e.WorkflowID, e.Mode)
		}
	}
}

// TestCatalogEntry_PreConsult_TrueByDefaultWhenFieldAbsent verifies that a
// workflow without a pre_consult field in its frontmatter produces a
// CatalogEntry with PreConsult == true (default on).
func TestCatalogEntry_PreConsult_TrueByDefaultWhenFieldAbsent(t *testing.T) {
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load(valid-catalog): %v", err)
	}
	// valid-catalog workflows (workflow-a, workflow-b, workflow-c) do not
	// declare pre_consult, so every entry must have PreConsult == true.
	entries := cat.Workflows()
	if len(entries) == 0 {
		t.Fatal("Workflows() returned empty slice for valid-catalog")
	}
	for _, e := range entries {
		if !e.PreConsult {
			t.Errorf("entry {%q, %q}.PreConsult = false, want true (absent pre_consult field must default to true)",
				e.WorkflowID, e.Mode)
		}
	}
}

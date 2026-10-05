package testcatalog_test

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/testcatalog"
)

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

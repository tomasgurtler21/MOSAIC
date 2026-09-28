package testcatalog_test

import (
	"testing"

	"mosaic-run/internal/testcatalog"
)

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

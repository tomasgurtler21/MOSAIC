package testcatalog_test

import (
	"errors"
	"testing"

	"mosaic-run/internal/testcatalog"
)

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

package orchfile_test

import (
	"bytes"
	"testing"

	"mosaic-run/internal/orchfile"
)

func TestGetWorkflow_KnownID_ReturnsRegion(t *testing.T) {
	region, err := orchfile.GetWorkflow(orchfileFixture("bare-single.md"), "bare-workflow")

	if err != nil {
		t.Fatalf("GetWorkflow returned unexpected error: %v", err)
	}
	got := string(region.Info.ID)
	if got != "bare-workflow" {
		t.Errorf("Info.ID: want %q, got %q", "bare-workflow", got)
	}
}

func TestGetWorkflow_KnownID_RegionVersion_IsCorrect(t *testing.T) {
	region, err := orchfile.GetWorkflow(orchfileFixture("bare-single.md"), "bare-workflow")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	if string(region.Info.Version) != "1.0" {
		t.Errorf("Info.Version: want %q, got %q", "1.0", string(region.Info.Version))
	}
}

func TestGetWorkflow_KnownID_Content_IsNonEmpty(t *testing.T) {
	region, err := orchfile.GetWorkflow(orchfileFixture("bare-single.md"), "bare-workflow")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	if len(bytes.TrimSpace(region.Content)) == 0 {
		t.Error("Content must not be empty for a workflow region with table content")
	}
}

func TestGetWorkflow_AbsentID_ReturnsRefusalError(t *testing.T) {
	// Requesting a workflow ID that does not appear in the file must return
	// a *domain.RefusalError.
	_, err := orchfile.GetWorkflow(orchfileFixture("bare-single.md"), "does-not-exist")

	if err == nil {
		t.Fatal("GetWorkflow must return an error for an absent identifier")
	}
	asRefusalError(t, err)
}

func TestGetWorkflow_AbsentID_RefusalError_ComponentIsOrchfile(t *testing.T) {
	// The RefusalError from GetWorkflow must name "orchfile" as the component
	// so callers can attribute the error to this package.
	_, err := orchfile.GetWorkflow(orchfileFixture("bare-single.md"), "does-not-exist")

	re := asRefusalError(t, err)
	if re.Component != "orchfile" {
		t.Errorf("RefusalError.Component: want %q, got %q", "orchfile", re.Component)
	}
}

func TestGetWorkflow_TwoWorkflowFile_SelectsCorrectOne(t *testing.T) {
	// GetWorkflow must return only the requested region, not the first one.
	region, err := orchfile.GetWorkflow(orchfileFixture("bare-two-workflows.md"), "workflow-beta")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	got := string(region.Info.ID)
	if got != "workflow-beta" {
		t.Errorf("Info.ID: want %q, got %q", "workflow-beta", got)
	}
	if string(region.Info.Version) != "2.0" {
		t.Errorf("Info.Version: want %q (beta's version), got %q", "2.0", string(region.Info.Version))
	}
}

func TestGetWorkflow_DeployManaged_SelectSecondByID_ContentContainsRoutingTable(t *testing.T) {
	// The routing table rows declared inside the greenfield-tdd workflow region must appear
	// in Content. This is the second-workflow counterpart of
	// TestGetWorkflow_DeployManaged_SelectFirstByID_ContentContainsRoutingTable.
	region, err := orchfile.GetWorkflow(orchfileFixture("deploy-managed.md"), "greenfield-tdd")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	// The greenfield-tdd workflow table in deploy-managed.md uses planner-tdd-soft.
	if !bytes.Contains(region.Content, []byte("planner-tdd-soft")) {
		t.Error("Content must contain the routing table rows from the greenfield-tdd workflow")
	}
}

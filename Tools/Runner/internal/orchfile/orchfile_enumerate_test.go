package orchfile_test

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-run/internal/orchfile"
)

func TestEnumerateWorkflows_BareFile_ReturnsOneRegion(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))

	if err != nil {
		t.Fatalf("EnumerateWorkflows returned unexpected error: %v", err)
	}
	if len(regions) != 1 {
		t.Errorf("want 1 workflow region, got %d", len(regions))
	}
}

func TestEnumerateWorkflows_BareFile_RegionID_MatchesSectionSuffix(t *testing.T) {
	// The identifier must be the name attribute of the Workflow tag.
	// <Workflow type="core" name="bare-workflow" version="1.0"> → ID = "bare-workflow".
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	got := string(regions[0].Info.ID)
	if got != "bare-workflow" {
		t.Errorf("Info.ID: want %q, got %q", "bare-workflow", got)
	}
}

func TestEnumerateWorkflows_BareFile_RegionVersion_ReadFromTag(t *testing.T) {
	// Version must be read from the version attribute on the opening tag.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	got := string(regions[0].Info.Version)
	if got != "1.0" {
		t.Errorf("Info.Version: want %q, got %q", "1.0", got)
	}
}

func TestEnumerateWorkflows_BareFile_Content_ExcludesBoundaryTagLines(t *testing.T) {
	// Content must be the raw bytes between the boundary tags, not including
	// the opening <Workflow ...> and closing </Workflow> lines themselves.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	content := regions[0].Content
	if bytes.Contains(content, []byte("<Workflow type=\"core\" name=\"bare-workflow\"")) {
		t.Error("Content must not contain the opening boundary tag line")
	}
	if bytes.Contains(content, []byte("</Workflow>")) {
		t.Error("Content must not contain the closing boundary tag line")
	}
}

// --- EnumerateWorkflows: deployed file (workflow nested in injection inside section) ---

func TestEnumerateWorkflows_DeployedFile_FindsWorkflow_NestedInInjection(t *testing.T) {
	// A deployed orchestrator agent embeds workflow sections inside
	// <AvailableWorkflows type="project"> inside <Identity type="core">.
	// EnumerateWorkflows must find the workflow at any nesting depth.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deployed.md"))

	if err != nil {
		t.Fatalf("EnumerateWorkflows returned unexpected error for deployed file: %v", err)
	}
	if len(regions) != 1 {
		t.Errorf("want 1 workflow region from deployed file, got %d", len(regions))
	}
}

func TestEnumerateWorkflows_DeployedFile_RegionID_IsCorrect(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deployed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	got := string(regions[0].Info.ID)
	if got != "deployed-workflow" {
		t.Errorf("Info.ID: want %q, got %q", "deployed-workflow", got)
	}
}

func TestEnumerateWorkflows_DeployedFile_RegionVersion_IsCorrect(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deployed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	got := string(regions[0].Info.Version)
	if got != "4.0" {
		t.Errorf("Info.Version: want %q, got %q", "4.0", got)
	}
}

// --- EnumerateWorkflows: multiple workflow sections ---

func TestEnumerateWorkflows_TwoWorkflows_ReturnsBoth(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-two-workflows.md"))

	if err != nil {
		t.Fatalf("EnumerateWorkflows returned unexpected error: %v", err)
	}
	if len(regions) != 2 {
		t.Errorf("want 2 workflow regions, got %d", len(regions))
	}
}

func TestEnumerateWorkflows_TwoWorkflows_PreservesDeclarationOrder(t *testing.T) {
	// Regions must be returned in the order they appear in the file.
	// bare-two-workflows.md declares workflow-alpha before workflow-beta.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-two-workflows.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}

	first := string(regions[0].Info.ID)
	second := string(regions[1].Info.ID)
	if first != "workflow-alpha" {
		t.Errorf("regions[0].Info.ID: want %q, got %q", "workflow-alpha", first)
	}
	if second != "workflow-beta" {
		t.Errorf("regions[1].Info.ID: want %q, got %q", "workflow-beta", second)
	}
}

func TestEnumerateWorkflows_TwoWorkflows_EachHasOwnVersion(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-two-workflows.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}

	if string(regions[0].Info.Version) != "1.0" {
		t.Errorf("regions[0].Info.Version: want %q, got %q", "1.0", string(regions[0].Info.Version))
	}
	if string(regions[1].Info.Version) != "2.0" {
		t.Errorf("regions[1].Info.Version: want %q, got %q", "2.0", string(regions[1].Info.Version))
	}
}

// --- EnumerateWorkflows: refusal cases ---

func TestEnumerateWorkflows_MissingFile_ReturnsError(t *testing.T) {
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("does-not-exist.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error for a missing file")
	}
}

func TestEnumerateWorkflows_MissingFile_ReturnsRefusalError(t *testing.T) {
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("does-not-exist.md"))

	asRefusalError(t, err) // fails if err is not *domain.RefusalError
}

func TestEnumerateWorkflows_MissingFile_RefusalError_ComponentIsOrchfile(t *testing.T) {
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("does-not-exist.md"))

	re := asRefusalError(t, err)
	if re.Component != "orchfile" {
		t.Errorf("RefusalError.Component: want %q, got %q", "orchfile", re.Component)
	}
}

func TestEnumerateWorkflows_MissingFile_RefusalError_ResourceNamesFile(t *testing.T) {
	// The refusal error for a missing file must name the file path so the user
	// can see which path failed to open.
	path := orchfileFixture("does-not-exist.md")
	_, err := orchfile.EnumerateWorkflows(path)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Resource, "does-not-exist.md") {
		t.Errorf("RefusalError.Resource must contain the file path %q; got %q", "does-not-exist.md", re.Resource)
	}
}

func TestEnumerateWorkflows_NoWorkflowRegions_ReturnsRefusalError(t *testing.T) {
	// A file that parses correctly but has no <Workflow type="core" ...> regions
	// must be refused.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("no-workflows.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error when no workflow regions are found")
	}
	asRefusalError(t, err)
}

func TestEnumerateWorkflows_MissingVersionAttribute_ReturnsRefusalError(t *testing.T) {
	// A workflow region whose opening tag carries no version attribute must be
	// refused.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("missing-version.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error when version attribute is absent")
	}
	asRefusalError(t, err)
}

func TestEnumerateWorkflows_MissingVersionAttribute_RefusalError_NamesRegion(t *testing.T) {
	// The refusal error must name the specific workflow region whose version
	// attribute is missing so the user can locate and fix it.
	// missing-version.md contains <Workflow type="core" name="no-version-workflow">.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("missing-version.md"))

	re := asRefusalError(t, err)
	if !strings.Contains(re.Resource, "no-version-workflow") {
		t.Errorf("RefusalError.Resource must contain the region identifier %q; got %q", "no-version-workflow", re.Resource)
	}
}

func TestEnumerateWorkflows_DuplicateIdentifiers_ReturnsRefusalError(t *testing.T) {
	// Two workflow sections with the same identifier must be refused because
	// the identifier is the row's stable identity.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("duplicate-ids.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error for duplicate workflow identifiers")
	}
	asRefusalError(t, err)
}

func TestEnumerateWorkflows_DuplicateIdentifiers_RefusalError_NamesDuplicate(t *testing.T) {
	// The error message or Resource must contain the specific duplicate identifier
	// so the user can locate it without reading the full file.
	// duplicate-ids.md contains two <Workflow type="core" name="shared-id"> tags.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("duplicate-ids.md"))

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "shared-id") {
		t.Errorf("RefusalError must name the duplicate identifier %q; got error: %s", "shared-id", re.Error())
	}
}

func TestEnumerateWorkflows_EmptyIdentifier_ReturnsRefusalError(t *testing.T) {
	// A <Workflow type="core" name="" ...> tag has an empty name attribute.
	// This is an unparseable identifier.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("empty-id.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error for an empty workflow identifier")
	}
	asRefusalError(t, err)
}

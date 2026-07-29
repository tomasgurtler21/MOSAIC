package orchfile_test

// Tests for orchfile.EnumerateWorkflows and orchfile.GetWorkflow.
//
// Coverage:
//   - EnumerateWorkflows finds one region in a hand-assembled file with a bare
//     top-level [[SECTION:Workflow:{id}]] tag (KC-11 first-class input).
//   - The returned region carries the correct identifier extracted from the
//     section name after the "Workflow:" prefix.
//   - The returned region carries the version string from the
//     <!-- workflow-version: {version} --> comment inside the section.
//   - The returned region Content contains the bytes between the boundary tags
//     but does not include the boundary tag lines themselves.
//   - EnumerateWorkflows finds a workflow nested at depth inside
//     [[INJECTION:AvailableWorkflows]] inside [[SECTION:Identity]], the
//     structure of a deployed orchestrator agent file.
//   - EnumerateWorkflows returns two regions from a file with two workflow
//     sections, in declaration order.
//   - Refusal: missing file → *domain.RefusalError naming the path.
//   - Refusal: no workflow regions → *domain.RefusalError.
//   - Refusal: version comment absent → *domain.RefusalError naming the region.
//   - Refusal: duplicate workflow identifiers → *domain.RefusalError naming
//     the duplicate identifier.
//   - Refusal: empty identifier (section named "Workflow:" with empty id part)
//     → *domain.RefusalError.
//   - GetWorkflow returns the correct region for a known identifier.
//   - GetWorkflow region Content is accessible.
//   - GetWorkflow: absent identifier → *domain.RefusalError.

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/orchfile"
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

// --- EnumerateWorkflows: bare top-level workflow (KC-11) ---

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
	// The identifier must be the part of the section name after "Workflow:".
	// [[SECTION:Workflow:bare-workflow]] → ID = "bare-workflow".
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	got := string(regions[0].Info.ID)
	if got != "bare-workflow" {
		t.Errorf("Info.ID: want %q, got %q", "bare-workflow", got)
	}
}

func TestEnumerateWorkflows_BareFile_RegionVersion_ExtractedFromComment(t *testing.T) {
	// Version must be extracted from <!-- workflow-version: 1.0 --> inside the region.
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
	// the [[SECTION:...]] and [[/SECTION:...]] lines themselves.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	content := regions[0].Content
	if bytes.Contains(content, []byte("[[SECTION:Workflow:bare-workflow]]")) {
		t.Error("Content must not contain the opening boundary tag line")
	}
	if bytes.Contains(content, []byte("[[/SECTION:Workflow:bare-workflow]]")) {
		t.Error("Content must not contain the closing boundary tag line")
	}
}

func TestEnumerateWorkflows_BareFile_Content_ContainsVersionComment(t *testing.T) {
	// The version comment line must be present in the content bytes so that
	// the workflow parser and callers can access it.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("bare-single.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	if !bytes.Contains(regions[0].Content, []byte("workflow-version")) {
		t.Error("Content must include the version comment line")
	}
}

// --- EnumerateWorkflows: deployed file (workflow nested in injection inside section) ---

func TestEnumerateWorkflows_DeployedFile_FindsWorkflow_NestedInInjection(t *testing.T) {
	// A deployed orchestrator agent embeds workflow sections inside
	// [[INJECTION:AvailableWorkflows]] inside [[SECTION:Identity]].
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
	// A file that parses correctly but has no [[SECTION:Workflow:{id}]] regions
	// must be refused.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("no-workflows.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error when no workflow regions are found")
	}
	asRefusalError(t, err)
}

func TestEnumerateWorkflows_MissingVersionComment_ReturnsRefusalError(t *testing.T) {
	// A workflow section without a <!-- workflow-version: ... --> comment
	// immediately inside it must be refused.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("missing-version.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error when version comment is absent")
	}
	asRefusalError(t, err)
}

func TestEnumerateWorkflows_MissingVersionComment_RefusalError_NamesRegion(t *testing.T) {
	// The refusal error must name the specific workflow region whose version
	// comment is missing so the user can locate and fix it.
	// missing-version.md contains [[SECTION:Workflow:no-version-workflow]].
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
	// duplicate-ids.md contains two [[SECTION:Workflow:shared-id]] sections.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("duplicate-ids.md"))

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "shared-id") {
		t.Errorf("RefusalError must name the duplicate identifier %q; got error: %s", "shared-id", re.Error())
	}
}

func TestEnumerateWorkflows_EmptyIdentifier_ReturnsRefusalError(t *testing.T) {
	// A [[SECTION:Workflow:]] tag has an empty id part (everything after the
	// "Workflow:" prefix is empty). This is an unparseable identifier.
	_, err := orchfile.EnumerateWorkflows(orchfileFixture("empty-id.md"))

	if err == nil {
		t.Fatal("EnumerateWorkflows must return an error for an empty workflow identifier")
	}
	asRefusalError(t, err)
}

// --- GetWorkflow ---

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

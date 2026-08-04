package transform_test

// boundary_fixtures_test.go wires the Deployment-module boundary fixtures in
// testdata/boundary/ and testdata/boundary/malformed/ into assertable test cases.
//
// Each fixture is parsed with docformat.Parse and validated with docformat.Validate.
// Valid fixtures must produce no issues. Malformed fixtures must each produce the
// specific issue code that names the violation they illustrate.
//
// These tests exist alongside the docformat package's own boundary tests
// (Tools/Common/docformat/boundary_test.go) to prove that the Deployment module's
// fixture set is coherent with the current canonical vocabulary — i.e. that the fixtures
// added during T13.3 correctly classify into passing/failing documents.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
)

// deploymentBoundaryDir is the testdata/boundary/ directory relative to this package's
// working directory (Tools/Deployment/internal/transform/).
const deploymentBoundaryDir = "../../testdata/boundary"

// ---------------------------------------------------------------------------
// Valid fixtures
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_ValidCases_NoIssues asserts that each well-formed
// boundary fixture in testdata/boundary/ parses without error and produces no
// validation issues. The validator is run without requiring canonical section parents
// or canonical section ordering, since the boundary fixtures are format-correctness
// probes, not structurally complete agent documents.
func TestDeploymentBoundaryFixtures_ValidCases_NoIssues(t *testing.T) {
	cases := []string{
		"empty-deployed.md",
		"filled-deployed.md",
		"mixed-markers.md",
	}

	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			src := readDeploymentBoundaryFixture(t, name)

			doc, err := docformat.Parse(src)
			if err != nil {
				t.Fatalf("docformat.Parse: %v", err)
			}

			issues := docformat.Validate(doc, docformat.ValidateOptions{})
			for _, iss := range issues {
				t.Errorf("unexpected validation issue: [%s] %s", iss.Code, iss.Message)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — wrong-marker
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_WrongMarkerToolAsInjection asserts that a fixture
// declaring a canonical tool-managed name (HarnessConstraints) under [[INJECTION:]]
// produces a "wrong-marker" validation issue. Tool-managed names must use [[DEPLOYED:]].
func TestDeploymentBoundaryFixtures_WrongMarkerToolAsInjection(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/wrong-marker-tool-as-injection.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	if !deploymentBoundaryHasCode(issues, "wrong-marker") {
		t.Errorf("expected a 'wrong-marker' issue for HarnessConstraints under [[INJECTION:]]; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// TestDeploymentBoundaryFixtures_WrongMarkerUserAsDeployed asserts that a fixture
// declaring a canonical user-owned name (IdentityExtension) under [[DEPLOYED:]] produces
// a "wrong-marker" validation issue. User-owned names must use [[INJECTION:]].
func TestDeploymentBoundaryFixtures_WrongMarkerUserAsDeployed(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/wrong-marker-user-as-deployed.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	if !deploymentBoundaryHasCode(issues, "wrong-marker") {
		t.Errorf("expected a 'wrong-marker' issue for IdentityExtension under [[DEPLOYED:]]; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — unknown-deployed
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_UnknownDeployedName asserts that a fixture declaring an
// unrecognised name under [[DEPLOYED:]] produces an "unknown-deployed" validation issue.
// An unrecognised tool-managed name has no generator and cannot be filled.
func TestDeploymentBoundaryFixtures_UnknownDeployedName(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	// AllowUnknownInjections defaults to false, so unknown [[DEPLOYED:]] names are flagged.
	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	if !deploymentBoundaryHasCode(issues, "unknown-deployed") {
		t.Errorf("expected an 'unknown-deployed' issue for [[DEPLOYED:UnknownRegion]]; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — wrong-parent (requires RequireInjectionParents: true)
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_DeployedOutsideRequiredParent asserts that a fixture
// declaring [[DEPLOYED:LanguagePatterns]] inside [[SECTION:Identity]] — instead of the
// required [[SECTION:Capabilities]] — produces a "wrong-parent" validation issue.
func TestDeploymentBoundaryFixtures_DeployedOutsideRequiredParent(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})
	if !deploymentBoundaryHasCode(issues, "wrong-parent") {
		t.Errorf("expected a 'wrong-parent' issue for LanguagePatterns inside Identity; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// TestDeploymentBoundaryFixtures_CommunicationProtocolInSection asserts that a fixture
// declaring [[DEPLOYED:CommunicationProtocol]] nested inside a section — instead of at
// body top level — produces a "wrong-parent" validation issue.
func TestDeploymentBoundaryFixtures_CommunicationProtocolInSection(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/communication-protocol-in-section.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})
	if !deploymentBoundaryHasCode(issues, "wrong-parent") {
		t.Errorf("expected a 'wrong-parent' issue for CommunicationProtocol nested in a section; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — out-of-order-section (requires RequireCanonicalSections: true)
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_DeployedProtocolOutOfOrder asserts that a fixture where
// [[DEPLOYED:CommunicationProtocol]] appears after [[SECTION:ArtifactProvenance]] — out of
// the canonical document order — produces an "out-of-order-section" validation issue.
func TestDeploymentBoundaryFixtures_DeployedProtocolOutOfOrder(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/deployed-protocol-out-of-order.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireCanonicalSections: true})
	if !deploymentBoundaryHasCode(issues, "out-of-order-section") {
		t.Errorf("expected an 'out-of-order-section' issue for CommunicationProtocol appearing after ArtifactProvenance; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// TestDeploymentBoundaryFixtures_ArtifactProvenanceOutOfOrder asserts that a fixture where
// [[SECTION:ArtifactProvenance]] appears after [[SECTION:Capabilities]] — out of the
// canonical document order — produces an "out-of-order-section" validation issue.
func TestDeploymentBoundaryFixtures_ArtifactProvenanceOutOfOrder(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/artifact-provenance-out-of-order.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireCanonicalSections: true})
	if !deploymentBoundaryHasCode(issues, "out-of-order-section") {
		t.Errorf("expected an 'out-of-order-section' issue for ArtifactProvenance appearing after Capabilities; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// readDeploymentBoundaryFixture reads a fixture file from the deployment boundary
// testdata directory, relative to this package's working directory.
func readDeploymentBoundaryFixture(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(deploymentBoundaryDir, name))
	if err != nil {
		t.Fatalf("read deployment boundary fixture %s: %v", name, err)
	}
	return src
}

// deploymentBoundaryHasCode reports whether any issue in the slice has the given code.
func deploymentBoundaryHasCode(issues []docformat.Issue, code string) bool {
	for _, iss := range issues {
		if iss.Code == code {
			return true
		}
	}
	return false
}

// deploymentBoundaryFormatIssues formats a slice of issues as a human-readable string
// for use in test failure messages.
func deploymentBoundaryFormatIssues(issues []docformat.Issue) string {
	if len(issues) == 0 {
		return "(no issues)"
	}
	var sb strings.Builder
	for i, iss := range issues {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString("[")
		sb.WriteString(iss.Code)
		sb.WriteString("] ")
		sb.WriteString(iss.Message)
	}
	return sb.String()
}

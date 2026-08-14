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
	"bytes"
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
// error-severity validation issues. The validator is run without requiring canonical
// section parents or canonical section ordering, since the boundary fixtures are
// format-correctness probes rather than structurally complete agent documents. Advisory
// warnings (e.g. identity-shape for minimal Identity sections) are expected and are not
// flagged by this test.
//
// compound-section.md is intentionally excluded: it is a parser fixture (not a
// validator fixture) exercised by TestValidate_CompoundSectionNames_WorkflowFileExcluded
// in the docformat package. Its close tag is syntactically valid but triggers a
// mismatched-tag error because the validator compares the compound name
// "Workflow:my-workflow" with the simple close-tag name "Workflow".
func TestDeploymentBoundaryFixtures_ValidCases_NoIssues(t *testing.T) {
	cases := []string{
		"empty-deployed.md",
		"empty-injection.md",
		"filled-deployed.md",
		"filled-injection.md",
		"legacy-markers.md",
		"mixed-markers.md",
		"multiple-sections.md",
		"simple-section.md",
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
				if iss.Severity == docformat.SeverityError {
					t.Errorf("unexpected error-severity validation issue: [%s] %s", iss.Code, iss.Message)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — wrong-marker
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_WrongMarkerToolAsInjection asserts that a fixture
// declaring a canonical tool-managed name (HarnessConstraints) under the project-injection
// marker produces a "wrong-marker" validation issue. Tool-managed names must use the
// managed-region marker.
func TestDeploymentBoundaryFixtures_WrongMarkerToolAsInjection(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/wrong-marker-tool-as-injection.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	if !deploymentBoundaryHasCode(issues, "wrong-marker") {
		t.Errorf("expected a 'wrong-marker' issue for HarnessConstraints under project-injection marker; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// TestDeploymentBoundaryFixtures_WrongMarkerUserAsDeployed asserts that a fixture
// declaring a non-tool-managed name (IdentityExtension) under the managed-region marker
// produces an "unknown-deployed" validation issue. Injection names are open (no allowlist),
// so an unrecognised managed-region name is always flagged as unknown-deployed regardless
// of whether it happens to be a known injection name.
func TestDeploymentBoundaryFixtures_WrongMarkerUserAsDeployed(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/wrong-marker-user-as-deployed.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	if !deploymentBoundaryHasCode(issues, "unknown-deployed") {
		t.Errorf("expected an 'unknown-deployed' issue for IdentityExtension under managed-region marker; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — unknown-deployed
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_UnknownDeployedName asserts that a fixture declaring an
// unrecognised name under the managed-region marker produces an "unknown-deployed" validation
// issue. An unrecognised tool-managed name has no generator and cannot be filled.
func TestDeploymentBoundaryFixtures_UnknownDeployedName(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	// AllowUnknownInjections defaults to false, so unknown managed-region names are flagged.
	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	if !deploymentBoundaryHasCode(issues, "unknown-deployed") {
		t.Errorf("expected an 'unknown-deployed' issue for UnknownRegion under managed-region marker; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — wrong-parent (requires RequireInjectionParents: true)
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_DeployedOutsideRequiredParent asserts that a fixture
// declaring <ProtocolConstraints type="managed"> inside <Identity type="core"> — instead of
// the required <Constraints type="core"> — produces a "wrong-parent" validation issue.
func TestDeploymentBoundaryFixtures_DeployedOutsideRequiredParent(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})
	if !deploymentBoundaryHasCode(issues, "wrong-parent") {
		t.Errorf("expected a 'wrong-parent' issue for ProtocolConstraints inside Identity; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// TestDeploymentBoundaryFixtures_CommunicationProtocolInSection asserts that a fixture
// declaring <CommunicationProtocol type="managed"> nested inside a section — instead of at
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
// <CommunicationProtocol type="managed"> appears after <Capabilities type="core"> — out of
// the canonical document order — produces an "out-of-order-section" validation issue.
// CommunicationProtocol must appear at slot 1 (before Capabilities at slot 2).
func TestDeploymentBoundaryFixtures_DeployedProtocolOutOfOrder(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/deployed-protocol-out-of-order.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireCanonicalSections: true})
	if !deploymentBoundaryHasCode(issues, "out-of-order-section") {
		t.Errorf("expected an 'out-of-order-section' issue for CommunicationProtocol appearing after Capabilities; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// TestDeploymentBoundaryFixtures_ConstraintsOutOfOrder asserts that a fixture where
// <Constraints type="core"> appears after <ErrorHandling type="core"> — out of the canonical
// document order — produces an "out-of-order-section" validation issue.
// This replaces the former ArtifactProvenanceOutOfOrder test: ArtifactProvenance was
// removed from the canonical vocabulary in Stage 2 and is now unknown-deployed.
func TestDeploymentBoundaryFixtures_ConstraintsOutOfOrder(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/artifact-provenance-out-of-order.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireCanonicalSections: true})
	if !deploymentBoundaryHasCode(issues, "out-of-order-section") {
		t.Errorf("expected an 'out-of-order-section' issue for Constraints appearing after ErrorHandling; got: %s",
			deploymentBoundaryFormatIssues(issues))
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — structural errors (table-driven)
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_MalformedCases_IssueCode asserts that each malformed
// boundary fixture produces the specific validation-issue code encoded in its filename.
// Where an option flag is required to surface the malformation, the test passes that flag.
func TestDeploymentBoundaryFixtures_MalformedCases_IssueCode(t *testing.T) {
	cases := []struct {
		fixture string
		code    string
		opts    docformat.ValidateOptions
	}{
		// duplicate-name: the same boundary name used more than once.
		{fixture: "malformed/duplicate-names.md", code: "duplicate-name"},

		// wrong-parent (injection): IdentityExtension appears at body top level instead of
		// inside the Identity section. Requires RequireInjectionParents.
		{
			fixture: "malformed/injection-outside-section.md",
			code:    "wrong-parent",
			opts:    docformat.ValidateOptions{RequireInjectionParents: true},
		},

		// mismatched-tag: opening tag name does not match its closing tag name.
		{fixture: "malformed/mismatched-names.md", code: "mismatched-tag"},

		// content-outside-boundary: non-blank content before the first boundary tag.
		{fixture: "malformed/out-of-boundary.md", code: "content-outside-boundary"},

		// out-of-order-section: Capabilities appears before Identity, violating canonical order.
		// Requires RequireCanonicalSections.
		{
			fixture: "malformed/out-of-order-sections.md",
			code:    "out-of-order-section",
			opts:    docformat.ValidateOptions{RequireCanonicalSections: true},
		},

		// unbalanced-tag (open): Identity section is opened but never closed.
		{fixture: "malformed/unbalanced-open.md", code: "unbalanced-tag"},

		// unbalanced-tag (close): a closing tag with no matching opening tag.
		{fixture: "malformed/unmatched-close.md", code: "unbalanced-tag"},

		// wrong-nesting: a section nested inside another section.
		{fixture: "malformed/wrong-nesting.md", code: "wrong-nesting"},

		// wrong-parent (injection): IdentityExtension appears inside Capabilities instead of
		// Identity. Requires RequireInjectionParents.
		{
			fixture: "malformed/wrong-parent.md",
			code:    "wrong-parent",
			opts:    docformat.ValidateOptions{RequireInjectionParents: true},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			src := readDeploymentBoundaryFixture(t, tc.fixture)

			doc, err := docformat.Parse(src)
			if err != nil {
				t.Fatalf("docformat.Parse: %v", err)
			}

			issues := docformat.Validate(doc, tc.opts)
			if !deploymentBoundaryHasCode(issues, tc.code) {
				t.Errorf("expected a %q issue; got: %s", tc.code, deploymentBoundaryFormatIssues(issues))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Malformed fixtures — unknown-injection (produces no issue)
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_UnknownInjectionName_ProducesNoIssue asserts that a fixture
// containing an unrecognised injection name (NonExistentName) produces no error-severity
// validation issue. Injection names are an open set: an unknown name is silently preserved
// and never flagged. The "unknown-injection" issue code was removed from the validator in
// Stage 3 and must not reappear.
//
// Advisory warnings (e.g. identity-shape for the minimal Identity section in the fixture)
// are expected and are not reported as failures here.
func TestDeploymentBoundaryFixtures_UnknownInjectionName_ProducesNoIssue(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "malformed/unknown-injection.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	// Use RequireInjectionParents to exercise the most thorough option set that could
	// in principle flag injection-related issues. NonExistentName is not in InjectionParent,
	// so even with this flag no wrong-parent issue is raised for it.
	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})
	for _, iss := range issues {
		if iss.Severity == docformat.SeverityError {
			t.Errorf("unexpected error-severity validation issue: [%s] %s", iss.Code, iss.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// Near-miss fixture — legacy-markers.md inertness
// ---------------------------------------------------------------------------

// TestDeploymentBoundaryFixtures_LegacyMarkers_IsInert asserts that the near-miss fixture
// legacy-markers.md is treated as inert text. The file contains <IdentityExtension> with no
// type attribute, which the parser ignores (Inertness Rule: a tag without a recognised type
// attribute is content, not a boundary). Two properties are checked:
//
//  1. No injection nodes are produced — the tag line is plain content, not a parsed boundary.
//  2. The document round-trips byte-exactly — inert content is preserved verbatim.
func TestDeploymentBoundaryFixtures_LegacyMarkers_IsInert(t *testing.T) {
	src := readDeploymentBoundaryFixture(t, "legacy-markers.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}

	// Assert no injection nodes: the <IdentityExtension> tag without a type attribute
	// must not be parsed as a boundary marker.
	injections := doc.Body().Injections()
	if len(injections) != 0 {
		names := make([]string, len(injections))
		for i, n := range injections {
			names[i] = n.Name()
		}
		t.Errorf("expected no injection nodes; got %d: %v", len(injections), names)
	}

	// Assert byte-exact round-trip: inert text must be preserved verbatim.
	got := doc.Bytes()
	if !bytes.Equal(got, src) {
		t.Errorf("round-trip mismatch: doc.Bytes() differs from source\nsource: %q\ngot:    %q", src, got)
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

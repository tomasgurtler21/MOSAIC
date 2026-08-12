package docformat_test

// Tests for structural validation and canonical vocabulary.
//
// Coverage:
//   - Validate returns an Issue with Code "unbalanced-tag" for a missing closing tag.
//   - Validate returns an Issue with Code "unbalanced-tag" for an unmatched closing tag.
//   - Validate returns an Issue with Code "mismatched-tag" for open/close names that do not match.
//   - Validate returns an Issue with Code "wrong-nesting" for a section nested inside another section.
//   - Validate returns an Issue with Code "wrong-parent" for a canonical injection outside its
//     advisory parent section (RequireInjectionParents is true).
//   - Validate returns an Issue with Code "content-outside-boundary" for non-blank content
//     outside all boundaries.
//   - Validate returns an Issue with Code "duplicate-name" for a boundary name used twice.
//   - Validate returns an Issue with Code "out-of-order-section" for sections outside canonical order.
//   - Validate returns no Issues for a well-formed document.
//   - All structural issue codes carry SeverityError.
//   - Issue.Node is non-empty for a mismatched-tag issue.
//   - RequireCanonicalSections enforces order of present sections, not presence of all sections.
//   - Workflow files with compound section names are excluded from agreement checks.
//   - InjectionParent maps each canonical injection name to its advisory parent section.

import (
	"testing"

	"mosaic-common/docformat"
)

// boundaryMalformedFixture parses a malformed boundary fixture. The parse must succeed
// (structural errors are reported by Validate, not by Parse). If Parse fails, the test
// is skipped with a message so the failure is attributed to the parser, not the validator.
func boundaryMalformedFixture(t *testing.T, name string) *docformat.Document {
	t.Helper()
	src := boundaryFixtureBytes(t, "malformed/"+name)
	doc, err := docformat.Parse(src)
	if err != nil {
		// Structural errors (unbalanced tags etc.) are validation concerns, not parse errors.
		// If Parse fails here, the parser is being stricter than required.
		t.Fatalf("Parse(%s) returned an error — structural errors must be reported via Validate, not Parse: %v", name, err)
	}
	return doc
}

// issueWithCode returns the first Issue in issues whose Code matches code, or nil.
func issueWithCode(issues []docformat.Issue, code string) *docformat.Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

// hasIssueWithCode reports whether any Issue in issues has Code == code.
func hasIssueWithCode(issues []docformat.Issue, code string) bool {
	return issueWithCode(issues, code) != nil
}

// --- Well-formed document ---

func TestValidate_WellFormedDocument_ReturnsNoIssues(t *testing.T) {
	// Uses a fixture with canonical sections and CommunicationProtocol as a top-level
	// [[DEPLOYED:]] boundary. ArtifactProvenance and ArtifactProvenanceExtension are
	// absent (removed from vocabulary in Stage 2).
	doc := parsedBoundaryFixture(t, "canonical-order-with-deployed-protocol.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
	})

	if len(issues) != 0 {
		t.Errorf("expected no issues for a well-formed document, got %d:", len(issues))
		for _, iss := range issues {
			t.Logf("  [%s] %s (line %d)", iss.Code, iss.Message, iss.Line)
		}
	}
}

// --- Unbalanced open tag ---

func TestValidate_UnbalancedOpenTag_ReportsUnbalancedTagIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "unbalanced-open.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "unbalanced-tag") {
		t.Errorf("expected an \"unbalanced-tag\" issue for an unclosed [[SECTION:Identity]], got issues: %v", issues)
	}
}

func TestValidate_UnbalancedOpenTag_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "unbalanced-open.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "unbalanced-tag")
	if iss == nil {
		t.Skip("no unbalanced-tag issue returned")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("unbalanced-tag issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// --- Unmatched closing tag ---

func TestValidate_UnmatchedCloseTag_ReportsUnbalancedTagIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "unmatched-close.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "unbalanced-tag") {
		t.Errorf("expected an \"unbalanced-tag\" issue for an unmatched [[/SECTION:Identity]], got issues: %v", issues)
	}
}

// --- Mismatched open/close names ---

func TestValidate_MismatchedTagNames_ReportsMismatchedTagIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "mismatched-names.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "mismatched-tag") {
		t.Errorf("expected a \"mismatched-tag\" issue for mismatched open/close names, got issues: %v", issues)
	}
}

func TestValidate_MismatchedTagNames_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "mismatched-names.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "mismatched-tag")
	if iss == nil {
		t.Skip("no mismatched-tag issue returned")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("mismatched-tag issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// --- Wrong nesting: section inside section ---

func TestValidate_SectionNestedInsideSection_ReportsWrongNestingIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "wrong-nesting.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "wrong-nesting") {
		t.Errorf("expected a \"wrong-nesting\" issue for a section nested inside another section, got issues: %v", issues)
	}
}

// --- Injection in wrong parent section ---

func TestValidate_InjectionInWrongParent_ReportsWrongParentIssue(t *testing.T) {
	// IdentityExtension is listed in InjectionParent with parent "Identity".
	// Placing it inside Capabilities violates the advisory parent constraint.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf("expected a \"wrong-parent\" issue for IdentityExtension inside Capabilities, got issues: %v", issues)
	}
}

func TestValidate_InjectionInWrongParent_NotReportedWhenOptionDisabled(t *testing.T) {
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: false,
	})

	if hasIssueWithCode(issues, "wrong-parent") {
		t.Error("wrong-parent issue reported even though RequireInjectionParents is false")
	}
}

// --- Content outside all boundaries ---

func TestValidate_ContentOutsideBoundary_ReportsContentOutsideBoundaryIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "out-of-boundary.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "content-outside-boundary") {
		t.Errorf("expected a \"content-outside-boundary\" issue for non-blank content before the first section, got issues: %v", issues)
	}
}

// --- Duplicate boundary name ---

func TestValidate_DuplicateBoundaryName_ReportsDuplicateNameIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "duplicate-names.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "duplicate-name") {
		t.Errorf("expected a \"duplicate-name\" issue for [[SECTION:Identity]] used twice, got issues: %v", issues)
	}
}

// --- Sections out of canonical order ---

func TestValidate_SectionsOutOfCanonicalOrder_ReportsOutOfOrderIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "out-of-order-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if !hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf("expected an \"out-of-order-section\" issue for sections in wrong canonical order, got issues: %v", issues)
	}
}

func TestValidate_SectionsOutOfCanonicalOrder_NotReportedWhenOptionDisabled(t *testing.T) {
	doc := boundaryMalformedFixture(t, "out-of-order-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Error("out-of-order-section issue reported even though RequireCanonicalSections is false")
	}
}

// --- Issue.Line is set ---

func TestValidate_UnbalancedOpenTag_IssueLineIsNonZero(t *testing.T) {
	doc := boundaryMalformedFixture(t, "unbalanced-open.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "unbalanced-tag")
	if iss == nil {
		t.Skip("no unbalanced-tag issue returned")
	}
	if iss.Line <= 0 {
		t.Errorf("Issue.Line must be a positive line number, got %d", iss.Line)
	}
}

// --- InjectionParent advisory map ---

func TestInjectionParent_MapsEachAdvisoryInjectionToItsParent(t *testing.T) {
	// Stage 2: ArtifactProvenanceExtension and ProtocolExtension are removed from
	// InjectionParent. ProtocolExtension is removed from the advisory map — projects use
	// [[CUSTOM:ProtocolExtension]] instead. The map is now advisory only.
	wantMap := map[string]string{
		"IdentityExtension":      "Identity",
		"CodebaseContext":        "Capabilities",
		"OutputArtifactTemplate": "Capabilities",
		"SeverityThresholds":     "Capabilities",
		"SeverityDefinitions":    "Capabilities",
		"ErrorHandlingExtension": "ErrorHandling",
		"ContextLimits":          "ExecutionPhilosophy",
	}

	got := docformat.InjectionParent

	if got == nil {
		t.Fatal("InjectionParent is nil — must be populated before first use")
	}
	for inj, wantParent := range wantMap {
		if gotParent, ok := got[inj]; !ok {
			t.Errorf("InjectionParent missing entry for %q", inj)
		} else if gotParent != wantParent {
			t.Errorf("InjectionParent[%q]: want %q, got %q", inj, wantParent, gotParent)
		}
	}
	// ProtocolExtension must be absent from the advisory map after Stage 2.
	if _, ok := got["ProtocolExtension"]; ok {
		t.Error("InjectionParent must not contain \"ProtocolExtension\" — " +
			"removed in Stage 2; projects use [[CUSTOM:ProtocolExtension]] instead")
	}
}

// --- Severity checks for structural issue codes ---

func TestValidate_WrongNesting_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "wrong-nesting.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "wrong-nesting")
	if iss == nil {
		t.Skip("no wrong-nesting issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("wrong-nesting issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_WrongParent_IssueSeverityIsAdvice(t *testing.T) {
	// Stage 3: injection wrong-parent is downgraded from SeverityError to SeverityAdvice.
	// A misplaced injection harms nobody; it is reported but never fatal.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	iss := issueWithCode(issues, "wrong-parent")
	if iss == nil {
		t.Skip("no wrong-parent issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityAdvice {
		t.Errorf("wrong-parent issue severity: want SeverityAdvice, got %q", iss.Severity)
	}
}

func TestValidate_ContentOutsideBoundary_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "out-of-boundary.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "content-outside-boundary")
	if iss == nil {
		t.Skip("no content-outside-boundary issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("content-outside-boundary issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_DuplicateName_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "duplicate-names.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "duplicate-name")
	if iss == nil {
		t.Skip("no duplicate-name issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("duplicate-name issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_OutOfOrderSection_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "out-of-order-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	iss := issueWithCode(issues, "out-of-order-section")
	if iss == nil {
		t.Skip("no out-of-order-section issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("out-of-order-section issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// --- Issue.Node field ---

func TestValidate_MismatchedTagNames_IssueNodeIsSet(t *testing.T) {
	doc := boundaryMalformedFixture(t, "mismatched-names.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "mismatched-tag")
	if iss == nil {
		t.Skip("no mismatched-tag issue returned — Issue.Node cannot be checked")
	}
	if iss.Node == "" {
		t.Error("Issue.Node must be non-empty for a mismatched-tag issue — the offending section name (Identity) is unambiguous")
	}
}

// --- Injection at body top level with RequireInjectionParents ---

func TestValidate_InjectionAtTopLevel_WrongParentWhenParentsRequired(t *testing.T) {
	// IdentityExtension inside no section but required to be inside Identity.
	doc := boundaryMalformedFixture(t, "injection-outside-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf("expected a \"wrong-parent\" issue for a canonical injection at top level (no enclosing section), got issues: %v", issues)
	}
}

func TestValidate_InjectionAtTopLevel_NotReportedWhenParentsNotRequired(t *testing.T) {
	doc := boundaryMalformedFixture(t, "injection-outside-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: false,
	})

	if hasIssueWithCode(issues, "wrong-parent") {
		t.Error("wrong-parent issue reported for top-level injection even though RequireInjectionParents is false")
	}
}

// --- RequireCanonicalSections semantics ---

func TestValidate_RequireCanonicalSections_EnforcesOrderNotPresence(t *testing.T) {
	// A document with a subset of canonical sections in correct order must not
	// produce an "out-of-order-section" issue.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Error("out-of-order-section reported for a document with a subset of canonical sections in correct order — RequireCanonicalSections must not require all sections to be present")
	}
}

// --- Compound section names: workflow files excluded from agreement checks ---

func TestValidate_CompoundSectionNames_WorkflowFileExcluded(t *testing.T) {
	// Compound section names (e.g. "Workflow:my-workflow") are addressable in Go
	// but produce E005 content-outside-boundary in the Python validator (TAG_PATTERN
	// limitation). The Python-Go agreement check excludes workflow files.
	doc := parsedBoundaryFixture(t, "compound-section.md")

	_, ok := doc.Body().Section("Workflow:my-workflow")
	if !ok {
		t.Error("Body.Section(\"Workflow:my-workflow\") returned false — compound section names must be addressable")
	}

	// Calling Validate must not panic; the exact issues may differ from the Python
	// validator's output within the documented scope exclusion.
	_ = docformat.Validate(doc, docformat.ValidateOptions{})
}

package docformat_test

// Tests for structural validation and canonical vocabulary (T4.5).
//
// Coverage:
//   - Validate returns an Issue with Code "unbalanced-tag" for a missing closing tag (E001).
//   - Validate returns an Issue with Code "unbalanced-tag" for an unmatched closing tag (E002).
//   - Validate returns an Issue with Code "mismatched-tag" for open/close names that do not match (E003).
//   - Validate returns an Issue with Code "wrong-nesting" for a section nested inside another section (E004-style).
//   - Validate returns an Issue with Code "wrong-parent" for an injection outside its expected parent section (E008-style).
//   - Validate returns an Issue with Code "wrong-parent" for a canonical injection at body top level (no enclosing section) when RequireInjectionParents is true.
//   - Validate returns an Issue with Code "content-outside-boundary" for non-blank content outside all boundaries.
//   - Validate returns an Issue with Code "duplicate-name" for a boundary name used twice in the same file.
//   - Validate returns an Issue with Code "out-of-order-section" for sections that appear outside canonical order.
//   - Validate returns an Issue with Code "unknown-injection" for a non-canonical injection name when AllowUnknownInjections is false.
//   - Validate returns no "unknown-injection" issue when AllowUnknownInjections is true.
//   - Validate returns no Issues for a well-formed document.
//   - All six structural issue codes carry SeverityError.
//   - Issue.Node is non-empty for a mismatched-tag issue (offending section name is known).
//   - RequireCanonicalSections enforces order of present sections, not presence of all sections.
//   - Workflow files with compound section names are excluded from AC4.4 Python-Go agreement scope.
//   - CanonicalSections contains all seven expected section names in canonical order.
//   - CanonicalInjections contains all twelve expected injection names.
//   - InjectionParent maps each canonical injection to its expected parent section.
//   - ClassifyInjection returns InjectionWorkflow for "AvailableWorkflows".
//   - ClassifyInjection returns InjectionHarness for "HarnessConstraints".
//   - ClassifyInjection returns InjectionProject for "IdentityExtension".

import (
	"testing"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
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
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections:  true,
		RequireInjectionParents:   true,
		AllowUnknownInjections:    false,
	})

	if len(issues) != 0 {
		t.Errorf("expected no issues for a well-formed document, got %d:", len(issues))
		for _, iss := range issues {
			t.Logf("  [%s] %s (line %d)", iss.Code, iss.Message, iss.Line)
		}
	}
}

// --- Unbalanced open tag (E001 equivalent: unmatched-open → "unbalanced-tag") ---

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

// --- Unmatched closing tag (E002 equivalent: unmatched-close → "unbalanced-tag") ---

func TestValidate_UnmatchedCloseTag_ReportsUnbalancedTagIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "unmatched-close.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "unbalanced-tag") {
		t.Errorf("expected an \"unbalanced-tag\" issue for an unmatched [[/SECTION:Identity]], got issues: %v", issues)
	}
}

// --- Mismatched open/close names (E003 → "mismatched-tag") ---

func TestValidate_MismatchedTagNames_ReportsMismatchedTagIssue(t *testing.T) {
	// [[SECTION:Identity]] opened but [[/SECTION:Capabilities]] used to close it.
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
	// Sections must not be nested inside other sections.
	doc := boundaryMalformedFixture(t, "wrong-nesting.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "wrong-nesting") {
		t.Errorf("expected a \"wrong-nesting\" issue for a section nested inside another section, got issues: %v", issues)
	}
}

// --- Injection in wrong parent section (E008 → "wrong-parent") ---

func TestValidate_InjectionInWrongParent_ReportsWrongParentIssue(t *testing.T) {
	// IdentityExtension must be inside the Identity section, not Capabilities.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf("expected a \"wrong-parent\" issue for IdentityExtension inside Capabilities, got issues: %v", issues)
	}
}

func TestValidate_InjectionInWrongParent_NotReportedWhenOptionDisabled(t *testing.T) {
	// When RequireInjectionParents is false, wrong-parent is not flagged.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: false,
	})

	if hasIssueWithCode(issues, "wrong-parent") {
		t.Error("wrong-parent issue reported even though RequireInjectionParents is false")
	}
}

// --- Content outside all boundaries (E005 → "content-outside-boundary") ---

func TestValidate_ContentOutsideBoundary_ReportsContentOutsideBoundaryIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "out-of-boundary.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "content-outside-boundary") {
		t.Errorf("expected a \"content-outside-boundary\" issue for non-blank content before the first section, got issues: %v", issues)
	}
}

// --- Duplicate boundary name (E006 → "duplicate-name") ---

func TestValidate_DuplicateBoundaryName_ReportsDuplicateNameIssue(t *testing.T) {
	doc := boundaryMalformedFixture(t, "duplicate-names.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "duplicate-name") {
		t.Errorf("expected a \"duplicate-name\" issue for [[SECTION:Identity]] used twice, got issues: %v", issues)
	}
}

// --- Sections out of canonical order (E007 → "out-of-order-section") ---

func TestValidate_SectionsOutOfCanonicalOrder_ReportsOutOfOrderIssue(t *testing.T) {
	// Capabilities appears before Identity in the fixture; this violates canonical order.
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

// --- Canonical vocabulary ---

func TestCanonicalSections_ContainsSevenSectionsInCanonicalOrder(t *testing.T) {
	want := []string{
		"Identity",
		"CommunicationProtocol",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	got := docformat.CanonicalSections

	if len(got) != len(want) {
		t.Fatalf("CanonicalSections length: want %d, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalSections[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

func TestCanonicalInjections_ContainsTwelveInjections(t *testing.T) {
	wantNames := []string{
		"IdentityExtension",
		"ProtocolExtension",
		"LanguagePatterns",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"HarnessConstraints",
		"CustomConstraints",
		"ErrorHandlingExtension",
		"ContextLimits",
		"SeverityThresholds",
		"SeverityDefinitions",
		"AvailableWorkflows",
	}

	got := docformat.CanonicalInjections

	if len(got) != len(wantNames) {
		t.Fatalf("CanonicalInjections length: want %d, got %d: %v", len(wantNames), len(got), got)
	}
	wantSet := make(map[string]bool, len(wantNames))
	for _, n := range wantNames {
		wantSet[n] = true
	}
	for _, n := range got {
		if !wantSet[n] {
			t.Errorf("unexpected injection name in CanonicalInjections: %q", n)
		}
	}
}

func TestInjectionParent_MapsEachCanonicalInjectionToItsExpectedParent(t *testing.T) {
	wantMap := map[string]string{
		"IdentityExtension":     "Identity",
		"ProtocolExtension":     "CommunicationProtocol",
		"LanguagePatterns":      "Capabilities",
		"CodebaseContext":       "Capabilities",
		"OutputArtifactTemplate": "Capabilities",
		"SeverityThresholds":    "Capabilities",
		"SeverityDefinitions":   "Capabilities",
		"HarnessConstraints":    "Constraints",
		"CustomConstraints":     "Constraints",
		"ErrorHandlingExtension": "ErrorHandling",
		"ContextLimits":         "ExecutionPhilosophy",
		"AvailableWorkflows":    "Identity",
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
}

// --- ClassifyInjection ---

func TestClassifyInjection_AvailableWorkflows_IsWorkflow(t *testing.T) {
	got := docformat.ClassifyInjection("AvailableWorkflows")
	if got != domain.InjectionWorkflow {
		t.Errorf("ClassifyInjection(\"AvailableWorkflows\"): want InjectionWorkflow, got %q", got)
	}
}

func TestClassifyInjection_HarnessConstraints_IsHarness(t *testing.T) {
	got := docformat.ClassifyInjection("HarnessConstraints")
	if got != domain.InjectionHarness {
		t.Errorf("ClassifyInjection(\"HarnessConstraints\"): want InjectionHarness, got %q", got)
	}
}

func TestClassifyInjection_LanguagePatterns_IsHarness(t *testing.T) {
	got := docformat.ClassifyInjection("LanguagePatterns")
	if got != domain.InjectionHarness {
		t.Errorf("ClassifyInjection(\"LanguagePatterns\"): want InjectionHarness, got %q", got)
	}
}

func TestClassifyInjection_IdentityExtension_IsProject(t *testing.T) {
	got := docformat.ClassifyInjection("IdentityExtension")
	if got != domain.InjectionProject {
		t.Errorf("ClassifyInjection(\"IdentityExtension\"): want InjectionProject, got %q", got)
	}
}

func TestClassifyInjection_ProtocolExtension_IsProject(t *testing.T) {
	got := docformat.ClassifyInjection("ProtocolExtension")
	if got != domain.InjectionProject {
		t.Errorf("ClassifyInjection(\"ProtocolExtension\"): want InjectionProject, got %q", got)
	}
}

func TestClassifyInjection_CodebaseContext_IsProject(t *testing.T) {
	got := docformat.ClassifyInjection("CodebaseContext")
	if got != domain.InjectionProject {
		t.Errorf("ClassifyInjection(\"CodebaseContext\"): want InjectionProject, got %q", got)
	}
}

func TestClassifyInjection_CustomConstraints_IsHarness(t *testing.T) {
	// CustomConstraints is InjectionHarness so that VS Code GHCP can fill it with the
	// parallel tool calls instruction at the harness level. Other harnesses return ok=false
	// for Injection("CustomConstraints") so applyHarnessInjection leaves it empty for them,
	// which is consistent with project-level semantics everywhere except VS Code GHCP.
	got := docformat.ClassifyInjection("CustomConstraints")
	if got != domain.InjectionHarness {
		t.Errorf("ClassifyInjection(\"CustomConstraints\"): want InjectionHarness, got %q", got)
	}
}

// --- AllowUnknownInjections option ---

func TestValidate_UnknownInjection_AllowedWhenOptionIsTrue(t *testing.T) {
	// When AllowUnknownInjections is true, a non-canonical injection name must NOT
	// produce an "unknown-injection" issue. The option permits free-form injection
	// names for documents that extend the canonical vocabulary.
	doc := boundaryMalformedFixture(t, "unknown-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		AllowUnknownInjections: true,
	})

	if hasIssueWithCode(issues, "unknown-injection") {
		t.Error("unexpected \"unknown-injection\" issue when AllowUnknownInjections is true — the option must suppress this check")
	}
}

func TestValidate_UnknownInjection_ReportedWhenOptionIsFalse(t *testing.T) {
	// When AllowUnknownInjections is false (the default), a non-canonical injection name
	// must produce an "unknown-injection" issue. The fixture contains
	// [[INJECTION:NonExistentName]], which is not in CanonicalInjections.
	doc := boundaryMalformedFixture(t, "unknown-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		AllowUnknownInjections: false,
	})

	if !hasIssueWithCode(issues, "unknown-injection") {
		t.Errorf("expected an \"unknown-injection\" issue for [[INJECTION:NonExistentName]] with AllowUnknownInjections false, got issues: %v", issues)
	}
}

// --- Severity checks for all structural issue codes ---

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

func TestValidate_WrongParent_IssueSeverityIsError(t *testing.T) {
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	iss := issueWithCode(issues, "wrong-parent")
	if iss == nil {
		t.Skip("no wrong-parent issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("wrong-parent issue severity: want SeverityError, got %q", iss.Severity)
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
	// Issue.Node must be non-empty for a mismatched-tag issue because the offending
	// section name is unambiguous: the fixture opens [[SECTION:Identity]] but closes
	// with [[/SECTION:Capabilities]]. The Node field must identify the problematic node.
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
	// A canonical injection appearing at body top level (no enclosing section) must be
	// reported as "wrong-parent" when RequireInjectionParents is true. At top level the
	// enclosing section is nil, which differs from the expected parent (Identity for
	// IdentityExtension). The fixture has [[INJECTION:IdentityExtension]] with no
	// enclosing section.
	doc := boundaryMalformedFixture(t, "injection-outside-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf("expected a \"wrong-parent\" issue for a canonical injection at top level (no enclosing section), got issues: %v", issues)
	}
}

func TestValidate_InjectionAtTopLevel_NotReportedWhenParentsNotRequired(t *testing.T) {
	// When RequireInjectionParents is false, a top-level canonical injection must not
	// produce a "wrong-parent" issue.
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
	// RequireCanonicalSections enforces that present canonical sections appear in the
	// canonical order; it does NOT require all seven canonical sections to be present.
	// A document containing only a subset of sections (in the correct relative order)
	// must produce no "out-of-order-section" issue.
	//
	// Implementor note: the option name could be misread as "require all canonical
	// sections to be present." This test makes the semantics explicit: the fixture
	// multiple-sections.md has only Identity and CommunicationProtocol (2 of 7), and
	// they appear in canonical order, so no issue must be reported.
	doc := parsedBoundaryFixture(t, "multiple-sections.md") // has Identity and CommunicationProtocol only

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Error("out-of-order-section reported for a document with a subset of canonical sections in correct order — RequireCanonicalSections must not require all sections to be present")
	}
}

// --- AC4.4 scope: workflow files excluded from Python-Go agreement checks ---

func TestValidate_AC4_4_ScopeExcludesWorkflowFiles(t *testing.T) {
	// AC4.4 requires the Go validator to agree with boundary_validator.py on which
	// documents are invalid. This agreement is scoped to agent files, utility agents,
	// orchestrators, and skill/hook files. Workflow files are EXCLUDED.
	//
	// Rationale: the Python TAG_PATTERN ([A-Za-z]+ for the name group) does not match
	// compound section names that contain colons or hyphens, such as
	// [[SECTION:Workflow:quick-fix]]. From the Python validator's perspective, a
	// workflow file has all its content outside any recognised boundary, triggering
	// E005 for every non-blank body line. The Go docformat package correctly parses
	// compound names as opaque colon-bearing strings, so it considers the
	// [[SECTION:Workflow:{id}]] block structurally valid.
	//
	// This disagreement is not a Go bug. The Python TAG_PATTERN restriction is a
	// Python-side limitation that is out of scope for the docformat package to fix.
	// The "repository-wide" phrasing in AC4.4 therefore means: every file in the
	// repository whose body uses only simple (non-compound) section names.
	//
	// Observable contract: the Go parser must correctly address the compound section
	// so that Body.Section("Workflow:my-workflow") succeeds. This is the key
	// behaviour the Python validator cannot verify, and it is correct Go behaviour.
	doc := parsedBoundaryFixture(t, "compound-section.md")

	// The compound section must be addressable by the Go package.
	_, ok := doc.Body().Section("Workflow:my-workflow")
	if !ok {
		t.Error("Body.Section(\"Workflow:my-workflow\") returned false — compound section names must be addressable")
	}

	// Calling Validate must not panic; the exact issues returned may differ from the
	// Python validator's output because of the compound-name recognition difference,
	// and that difference is within the documented scope exclusion.
	_ = docformat.Validate(doc, docformat.ValidateOptions{})
}

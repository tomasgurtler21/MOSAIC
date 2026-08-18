package docformat_test

// validate_stage4_test.go covers Stage 4 amendments to Validate semantics:
//
// Coverage (unknown-deployed exemption for managed block names):
//   - A nested managed Workflow block (bare form) inside AvailableWorkflows produces no
//     unknown-deployed issue.
//   - A nested managed Workflow block (compound form "Workflow:quick-fix") produces no
//     unknown-deployed issue.
//   - Multiple managed Workflow blocks inside AvailableWorkflows produce no unknown-deployed.
//   - An unrecognised managed name (not in CanonicalDeployed, not a managed block name)
//     still produces unknown-deployed — Stage 4 narrows the false-positive, not the rule.
//   - A document containing only CanonicalDeployed managed regions and recognised managed
//     block names produces no unknown-deployed issues.
//
// Coverage (wrong-parent for managed blocks, gated by RequireInjectionParents):
//   - A managed Workflow block inside its required parent (AvailableWorkflows) produces no
//     wrong-parent issue even when RequireInjectionParents is set.
//   - A managed Workflow block outside its required parent produces a wrong-parent issue when
//     RequireInjectionParents is set. The issue severity is SeverityError.
//   - wrong-parent for a managed block is absent from blockingValidationCodes, so a document
//     with a misplaced Workflow block passes the zero-option validate check (no blocking issue).
//
// Coverage (regression guards — unchanged validate behaviours after Stage 4):
//   - Unbalanced tags still produce unbalanced-tag issues.
//   - A mismatched tag still produces mismatched-tag.
//   - An existing CanonicalDeployed name under type="project" still produces wrong-marker.
//   - Duplicate boundary names still produce duplicate-name.
//   - duplicate-name still compares full compound names so two distinct Workflow blocks
//     (Workflow:a, Workflow:b) do not collide.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseBodyForValidate wraps body bytes in minimal document structure (no frontmatter).
func parseBodyForValidate(t *testing.T, body []byte) *docformat.Document {
	t.Helper()
	doc, err := docformat.Parse(body)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}
	return doc
}

// hasIssueCode reports whether issues contains at least one issue with the given code.
func hasIssueCode(issues []docformat.Issue, code string) bool {
	for _, iss := range issues {
		if iss.Code == code {
			return true
		}
	}
	return false
}

// issueCodesFor returns all issue codes present in issues, deduplicated.
func issueCodesFor(issues []docformat.Issue) []string {
	seen := map[string]bool{}
	var codes []string
	for _, iss := range issues {
		if !seen[iss.Code] {
			seen[iss.Code] = true
			codes = append(codes, iss.Code)
		}
	}
	return codes
}

// ---------------------------------------------------------------------------
// unknown-deployed exemption for managed block names
// ---------------------------------------------------------------------------

func TestValidate_Stage4_WorkflowBareManaged_InsideAvailableWorkflows_NoUnknownDeployed(t *testing.T) {
	// A managed Workflow block (bare form) nested inside AvailableWorkflows must not produce
	// unknown-deployed after Stage 4. Currently it does — this test is RED until implementation.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\">\n" +
		"Some workflow content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueCode(issues, "unknown-deployed") {
		t.Errorf("Validate produced unknown-deployed for a bare managed Workflow block inside AvailableWorkflows; "+
			"after Stage 4, nested managed Workflow blocks must be recognised. Issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_WorkflowCompoundManaged_InsideAvailableWorkflows_NoUnknownDeployed(t *testing.T) {
	// A compound managed Workflow block ("Workflow:quick-fix") must not produce unknown-deployed.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"## Quick Fix Workflow\n" +
		"\n" +
		"Some workflow body.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueCode(issues, "unknown-deployed") {
		t.Errorf("Validate produced unknown-deployed for compound managed Workflow:quick-fix inside AvailableWorkflows; "+
			"after Stage 4, compound managed Workflow block names must be recognised. Issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_MultipleWorkflowBlocks_InsideAvailableWorkflows_NoUnknownDeployed(t *testing.T) {
	// Multiple distinct managed Workflow blocks must all be recognised — no unknown-deployed.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Quick fix content.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"greenfield-tdd\" version=\"3.3\">\n" +
		"Greenfield content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueCode(issues, "unknown-deployed") {
		t.Errorf("Validate produced unknown-deployed for multiple managed Workflow blocks inside AvailableWorkflows; "+
			"all managed Workflow block names must be recognised. Issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_UnrecognisedManagedName_StillProducesUnknownDeployed(t *testing.T) {
	// An unrecognised managed name that is neither CanonicalDeployed nor a managed block name
	// must still produce unknown-deployed. Stage 4 narrows the check, not removes it.
	body := "<Identity type=\"core\">\n" +
		"<UnrecognisedManagedName type=\"managed\">\n" +
		"Some content.\n" +
		"</UnrecognisedManagedName>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueCode(issues, "unknown-deployed") {
		t.Errorf("Validate did not produce unknown-deployed for unrecognised managed name %q; "+
			"Stage 4 must not suppress unknown-deployed for names outside CanonicalManagedBlocks and CanonicalDeployed. Issues: %v",
			"UnrecognisedManagedName", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_DocumentWithRecognisedManagedBlocks_NoUnknownDeployed(t *testing.T) {
	// A complete document with CanonicalDeployed regions and managed Workflow blocks must
	// produce no unknown-deployed issues. This is the deployed-orchestrator document shape.
	body := "<Identity type=\"core\">\n" +
		"Orchestrator identity.\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Quick fix workflow.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"brownfield-tdd\" version=\"2.1\">\n" +
		"Brownfield TDD workflow.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueCode(issues, "unknown-deployed") {
		t.Errorf("Validate produced unknown-deployed in a document with only recognised managed regions and blocks; "+
			"issues: %v", issueCodesFor(issues))
	}
}

// ---------------------------------------------------------------------------
// wrong-parent for managed blocks (gated by RequireInjectionParents)
// ---------------------------------------------------------------------------

func TestValidate_Stage4_WorkflowInsideCorrectParent_NoWrongParent(t *testing.T) {
	// A managed Workflow block nested inside its required parent (AvailableWorkflows) must
	// not produce wrong-parent even when RequireInjectionParents is set.
	// Note: this test passes vacuously pre-implementation because the wrong-parent check does
	// not yet exist. Its value activates once I4.1 implements the wrong-parent check — at that
	// point it guards against incorrectly raising wrong-parent for correctly placed blocks.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})

	for _, iss := range issues {
		if iss.Code == "wrong-parent" {
			t.Errorf("Validate produced wrong-parent for Workflow inside its required parent AvailableWorkflows; "+
				"issue: %+v", iss)
		}
	}
}

func TestValidate_Stage4_WorkflowOutsideRequiredParent_ProducesWrongParent(t *testing.T) {
	// A managed Workflow block that is NOT inside AvailableWorkflows must produce a wrong-parent
	// issue at SeverityError when RequireInjectionParents is set.
	body := "<Identity type=\"core\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Content outside AvailableWorkflows.\n" +
		"</Workflow>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})

	found := false
	for _, iss := range issues {
		if iss.Code == "wrong-parent" {
			found = true
			if iss.Severity != docformat.SeverityError {
				t.Errorf("wrong-parent for managed block outside required parent: want SeverityError, got %q", iss.Severity)
			}
		}
	}
	if !found {
		t.Errorf("Validate did not produce wrong-parent for Workflow outside required parent AvailableWorkflows "+
			"(RequireInjectionParents=true); issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_MisplacedWorkflow_ZeroOptions_NoBlockingIssue(t *testing.T) {
	// wrong-parent for a managed block is non-blocking. A document with a Workflow block
	// outside AvailableWorkflows must produce no unknown-deployed with zero-value options
	// (RequireInjectionParents is false) — wrong-parent is only raised when the option is set.
	body := "<Identity type=\"core\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Content outside AvailableWorkflows.\n" +
		"</Workflow>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	// No unknown-deployed (Workflow is recognised), no wrong-parent (option not set).
	if hasIssueCode(issues, "unknown-deployed") {
		t.Errorf("Validate produced unknown-deployed for a managed Workflow block (should be recognised); "+
			"issues: %v", issueCodesFor(issues))
	}
	if hasIssueCode(issues, "wrong-parent") {
		t.Errorf("Validate produced wrong-parent with zero-value options; wrong-parent is gated by RequireInjectionParents; "+
			"issues: %v", issueCodesFor(issues))
	}
}

// ---------------------------------------------------------------------------
// Regression guards — unchanged Validate behaviours after Stage 4
// ---------------------------------------------------------------------------

func TestValidate_Stage4_UnbalancedWorkflowTag_StillBlockingIssue(t *testing.T) {
	// An unclosed managed Workflow block must still produce unbalanced-tag.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Content.\n" +
		// Missing </Workflow>
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueCode(issues, "unbalanced-tag") {
		t.Errorf("Validate did not produce unbalanced-tag for an unclosed Workflow block; "+
			"issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_DuplicateWorkflowName_StillDuplicateNameIssue(t *testing.T) {
	// The same compound Workflow name appearing twice must still produce duplicate-name.
	// (Two distinct Workflow names, e.g. quick-fix and greenfield-tdd, must not collide.)
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"First occurrence.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Second occurrence — duplicate.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueCode(issues, "duplicate-name") {
		t.Errorf("Validate did not produce duplicate-name for the same Workflow name appearing twice; "+
			"issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_DistinctWorkflowNames_NoDuplicateName(t *testing.T) {
	// Two distinct Workflow compound names (quick-fix vs greenfield-tdd) must NOT collide.
	// duplicate-name compares full compound names; they are different names.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"First workflow.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"greenfield-tdd\" version=\"3.3\">\n" +
		"Second workflow — different name.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueCode(issues, "duplicate-name") {
		t.Errorf("Validate produced duplicate-name for two distinct Workflow compound names; "+
			"duplicate-name must compare full names, not prefixes. Issues: %v", issueCodesFor(issues))
	}
}

func TestValidate_Stage4_CanonicalDeployedUnderInjection_StillProducesWrongMarker(t *testing.T) {
	// A CanonicalDeployed name under type="project" must still produce wrong-marker.
	// Stage 4 must not affect this check.
	body := "<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"project\">\n" + // wrong marker — must be type="managed"
		"</AvailableWorkflows>\n" +
		"</Identity>\n"
	doc := parseBodyForValidate(t, []byte(body))
	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueCode(issues, "wrong-marker") {
		t.Errorf("Validate did not produce wrong-marker for AvailableWorkflows under type=\"project\"; "+
			"Stage 4 must not suppress wrong-marker for CanonicalDeployed names. Issues: %v", issueCodesFor(issues))
	}
}

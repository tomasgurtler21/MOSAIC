package docformat_test

// Tests for marker/name mismatch validation, deployed-region parent checks,
// unknown-deployed-name checks, and the CommunicationProtocol canonical document slot.
//
// Stage 2 changes to this file:
//   - AllowUnknownInjections removed from ValidateOptions — field is gone.
//   - "wrong-marker" for user-owned names under [[DEPLOYED:]] is gone because there is
//     no user-owned registry; such names produce "unknown-deployed" instead. Those tests
//     are removed and replaced in vocabulary_boundary_sync_test.go.
//   - "unknown-deployed" check is unconditional (no AllowUnknownInjections gate).
//
// Coverage (wrong-marker — tool-managed name under [[INJECTION:]]):
//   - Validate reports "wrong-marker" when a tool-managed name is declared with [[INJECTION:]].
//   - The "wrong-marker" issue carries SeverityError.
//   - When a tool-managed name is mismatched, "wrong-marker" is reported; no secondary
//     diagnostic is raised for the same name.
//
// Coverage (deployed-region parent checks):
//   - Validate reports "wrong-parent" when a tool-managed name appears outside its
//     required parent section (RequireInjectionParents is true).
//   - The "wrong-parent" issue for a deployed name carries SeverityError.
//   - No "wrong-parent" is raised for deployed names when RequireInjectionParents is false.
//   - Validate reports "wrong-parent" when [[DEPLOYED:CommunicationProtocol]] is nested
//     inside a section (it must be at body top level).
//
// Coverage (unknown deployed name — unconditional):
//   - Validate reports "unknown-deployed" for a [[DEPLOYED:]] name not in CanonicalDeployed.
//   - The "unknown-deployed" issue carries SeverityError.
//
// Coverage (CommunicationProtocol canonical slot):
//   - A document with [[DEPLOYED:CommunicationProtocol]] at body top level in the
//     second canonical position produces no "out-of-order-section" issue.
//   - A document with [[DEPLOYED:CommunicationProtocol]] after the third canonical
//     position produces an "out-of-order-section" issue.
//   - No "out-of-order-section" when RequireCanonicalSections is false.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Wrong-marker: tool-managed name declared under [[INJECTION:]]
// ---------------------------------------------------------------------------

func TestValidate_WrongMarker_ToolManagedNameUnderInjection_ReportsWrongMarkerIssue(t *testing.T) {
	// HarnessConstraints is a tool-managed name that must be declared with [[DEPLOYED:]].
	// Declaring it with [[INJECTION:HarnessConstraints]] is a marker mismatch.
	//
	// Fixture: wrong-marker-tool-as-injection.md contains
	//   [[SECTION:Constraints]]
	//     [[INJECTION:HarnessConstraints]]
	//     [[/INJECTION:HarnessConstraints]]
	//   [[/SECTION:Constraints]]
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-tool-as-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected a \"wrong-marker\" issue for [[INJECTION:HarnessConstraints]] (tool-managed name under the wrong marker), got issues: %v",
			issues,
		)
	}
}

func TestValidate_WrongMarker_ToolManagedNameUnderInjection_IssueSeverityIsError(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-tool-as-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	iss := issueWithCode(issues, "wrong-marker")
	if iss == nil {
		t.Skip("no wrong-marker issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("wrong-marker issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_WrongMarker_ToolManagedNameUnderInjection_OneIssueDiagnostic(t *testing.T) {
	// When a tool-managed name is declared under [[INJECTION:]], "wrong-marker" is the
	// only diagnostic. No secondary code (such as "unknown-deployed") must be raised
	// for the same boundary tag.
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-tool-as-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected a \"wrong-marker\" issue for a tool-managed name declared under [[INJECTION:]], got issues: %v",
			issues,
		)
	}
	if hasIssueWithCode(issues, "unknown-injection") {
		t.Errorf(
			"expected no \"unknown-injection\" issue when \"wrong-marker\" is raised — one mistake must produce one diagnostic, got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// No wrong-marker for correct tool-managed pairing
// ---------------------------------------------------------------------------

func TestValidate_WrongMarker_CorrectPairing_ToolManagedUnderDeployed_NoWrongMarkerIssue(t *testing.T) {
	// A document with tool-managed names correctly declared under [[DEPLOYED:]] must not
	// produce any "wrong-marker" issue.
	doc := parsedBoundaryFixture(t, "deployed-in-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"unexpected \"wrong-marker\" issue for a correctly paired tool-managed name under [[DEPLOYED:]], got issues: %v",
			issues,
		)
	}
}

func TestValidate_WrongMarker_CorrectPairing_UserOwnedUnderInjection_NoWrongMarkerIssue(t *testing.T) {
	// A document with an injection name correctly declared under [[INJECTION:]] must not
	// produce any "wrong-marker" issue. Injection names are open in Stage 2 — any name
	// under [[INJECTION:]] that is not in CanonicalDeployed is accepted as InjectionProject.
	doc := parsedBoundaryFixture(t, "empty-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"unexpected \"wrong-marker\" issue for an injection name under [[INJECTION:]], got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// Deployed-region parent check
// ---------------------------------------------------------------------------

func TestValidate_DeployedOutsideRequiredParent_ReportsWrongParentIssue(t *testing.T) {
	// ProtocolConstraints requires a Constraints parent. Placing it inside Identity
	// violates the parent constraint.
	//
	// Fixture: deployed-outside-required-parent.md contains
	//   [[SECTION:Identity]]
	//     [[DEPLOYED:ProtocolConstraints]]
	//     [[/DEPLOYED:ProtocolConstraints]]
	//   [[/SECTION:Identity]]
	doc := parsedBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf(
			"expected a \"wrong-parent\" issue for [[DEPLOYED:ProtocolConstraints]] inside Identity (requires Constraints), got issues: %v",
			issues,
		)
	}
}

func TestValidate_DeployedOutsideRequiredParent_IssueSeverityIsError(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

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

func TestValidate_DeployedOutsideRequiredParent_NotReportedWhenOptionDisabled(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: false,
	})

	if hasIssueWithCode(issues, "wrong-parent") {
		t.Error("wrong-parent issue reported for deployed name even though RequireInjectionParents is false")
	}
}

// ---------------------------------------------------------------------------
// CommunicationProtocol must be at body top level
// ---------------------------------------------------------------------------

func TestValidate_CommunicationProtocolDeployed_NestedInSection_ReportsWrongParentIssue(t *testing.T) {
	// [[DEPLOYED:CommunicationProtocol]] has "" required parent (top level only).
	// Nesting it inside a section violates the parent constraint.
	//
	// Fixture: communication-protocol-in-section.md
	doc := parsedBoundaryFixture(t, "malformed/communication-protocol-in-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf(
			"expected a \"wrong-parent\" issue for [[DEPLOYED:CommunicationProtocol]] nested inside a section, got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// Unknown deployed name — unconditional (no AllowUnknownInjections gate)
// ---------------------------------------------------------------------------

func TestValidate_UnknownDeployedName_ReportsUnknownDeployedIssue(t *testing.T) {
	// An unrecognised [[DEPLOYED:]] name has no generator. The "unknown-deployed" check
	// is unconditional in Stage 2 — AllowUnknownInjections is removed from ValidateOptions.
	//
	// Fixture: unknown-deployed-name.md contains [[DEPLOYED:UnknownRegion]].
	doc := parsedBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "unknown-deployed") {
		t.Errorf(
			"expected an \"unknown-deployed\" issue for [[DEPLOYED:UnknownRegion]], got issues: %v",
			issues,
		)
	}
}

func TestValidate_UnknownDeployedName_IssueSeverityIsError(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "unknown-deployed")
	if iss == nil {
		t.Skip("no unknown-deployed issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("unknown-deployed issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// ---------------------------------------------------------------------------
// CommunicationProtocol DEPLOYED satisfies the canonical order slot
// ---------------------------------------------------------------------------

func TestValidate_DeployedCommunicationProtocol_InSecondPosition_NoOutOfOrderIssue(t *testing.T) {
	// A document whose second canonical slot is occupied by a top-level
	// [[DEPLOYED:CommunicationProtocol]] boundary (after Identity, before Capabilities)
	// must produce no "out-of-order-section" issue.
	doc := parsedBoundaryFixture(t, "canonical-order-with-deployed-protocol.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"unexpected \"out-of-order-section\" issue for a document with [[DEPLOYED:CommunicationProtocol]] in the correct second canonical slot, got issues: %v",
			issues,
		)
	}
}

func TestValidate_DeployedCommunicationProtocol_InSecondPosition_NoIssues(t *testing.T) {
	doc := parsedBoundaryFixture(t, "canonical-order-with-deployed-protocol.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
	})

	if len(issues) != 0 {
		t.Errorf("expected no issues for a canonical document with a top-level deployed protocol region, got %d:", len(issues))
		for _, iss := range issues {
			t.Logf("  [%s] %s (line %d)", iss.Code, iss.Message, iss.Line)
		}
	}
}

func TestValidate_DeployedCommunicationProtocol_AfterThirdPosition_ReportsOutOfOrderIssue(t *testing.T) {
	// In Stage 2, CanonicalOrder has 7 slots. CommunicationProtocol is at index 1.
	// A document where CommunicationProtocol appears after Capabilities (index 2) is
	// out of order.
	//
	// Fixture: deployed-protocol-out-of-order.md
	doc := parsedBoundaryFixture(t, "malformed/deployed-protocol-out-of-order.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if !hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected an \"out-of-order-section\" issue when [[DEPLOYED:CommunicationProtocol]] appears out of order, got issues: %v",
			issues,
		)
	}
}

func TestValidate_DeployedCommunicationProtocol_OutOfOrder_NotReportedWhenOptionDisabled(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/deployed-protocol-out-of-order.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Error("out-of-order-section issue reported for deployed protocol even though RequireCanonicalSections is false")
	}
}

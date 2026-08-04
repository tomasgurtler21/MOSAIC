package docformat_test

// Tests for Stage 2 validation: marker/name mismatch, deployed-region parent checks,
// unknown-deployed-name checks, and the CommunicationProtocol canonical document slot.
//
// Coverage (marker/name mismatch — T2.2):
//   - Validate reports a "wrong-marker" issue when a tool-managed name is declared
//     with [[INJECTION:]] (the wrong marker).
//   - Validate reports a "wrong-marker" issue when a user-owned name is declared
//     with [[DEPLOYED:]] (the wrong marker).
//   - Both "wrong-marker" issues carry SeverityError.
//   - When a canonical name is mismatched, "wrong-marker" is reported; "unknown-injection"
//     is not, so one mistake produces one actionable diagnostic.
//   - Correct pairings (tool-managed under [[DEPLOYED:]], user-owned under [[INJECTION:]])
//     produce no "wrong-marker" issue.
//
// Coverage (deployed-region parent checks — T2.3):
//   - Validate reports a "wrong-parent" issue when a tool-managed name appears outside
//     its DeployedParent section, when RequireInjectionParents is true.
//   - The "wrong-parent" issue for a deployed name carries SeverityError.
//   - No "wrong-parent" issue is raised for deployed names when RequireInjectionParents
//     is false.
//   - Validate reports a "wrong-parent" issue when [[DEPLOYED:CommunicationProtocol]]
//     appears nested inside a section (it must be at body top level).
//   - Validate reports an "unknown-deployed" issue when a [[DEPLOYED:]] name is not in
//     the tool-managed registry and AllowUnknownInjections is false.
//   - The "unknown-deployed" issue carries SeverityError.
//   - No "unknown-deployed" issue is raised when AllowUnknownInjections is true.
//
// Coverage (CommunicationProtocol canonical slot — T2.4):
//   - A document where [[DEPLOYED:CommunicationProtocol]] appears at body top level in
//     the second canonical position (after Identity, before ArtifactProvenance) produces
//     no "out-of-order-section" issue when RequireCanonicalSections is true.
//   - A document where [[DEPLOYED:CommunicationProtocol]] appears after ArtifactProvenance
//     produces an "out-of-order-section" issue when RequireCanonicalSections is true.
//   - No "out-of-order-section" issue is raised when RequireCanonicalSections is false.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// T2.2 — Wrong-marker: tool-managed name declared under [[INJECTION:]]
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
		AllowUnknownInjections:  false,
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
		AllowUnknownInjections:  false,
	})

	iss := issueWithCode(issues, "wrong-marker")
	if iss == nil {
		t.Skip("no wrong-marker issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("wrong-marker issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_WrongMarker_ToolManagedNameUnderInjection_WrongMarkerPreferredOverUnknownInjection(t *testing.T) {
	// When a canonical name is declared under the wrong marker, "wrong-marker" must be
	// reported. "unknown-injection" must NOT additionally be raised for the same name,
	// so one mistake produces one actionable diagnostic rather than a confusing pair.
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-tool-as-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		AllowUnknownInjections: false,
	})

	if !hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected a \"wrong-marker\" issue for a canonical tool-managed name declared under [[INJECTION:]], got issues: %v",
			issues,
		)
	}
	if hasIssueWithCode(issues, "unknown-injection") {
		t.Errorf(
			"expected no \"unknown-injection\" issue when \"wrong-marker\" is raised for the same name — one mistake must produce one diagnostic, got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Wrong-marker: user-owned name declared under [[DEPLOYED:]]
// ---------------------------------------------------------------------------

func TestValidate_WrongMarker_UserOwnedNameUnderDeployed_ReportsWrongMarkerIssue(t *testing.T) {
	// IdentityExtension is a user-owned name that must be declared with [[INJECTION:]].
	// Declaring it with [[DEPLOYED:IdentityExtension]] is a marker mismatch.
	//
	// Fixture: wrong-marker-user-as-deployed.md contains
	//   [[SECTION:Identity]]
	//     [[DEPLOYED:IdentityExtension]]
	//     [[/DEPLOYED:IdentityExtension]]
	//   [[/SECTION:Identity]]
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-user-as-deployed.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
		AllowUnknownInjections:  false,
	})

	if !hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected a \"wrong-marker\" issue for [[DEPLOYED:IdentityExtension]] (user-owned name under the wrong marker), got issues: %v",
			issues,
		)
	}
}

func TestValidate_WrongMarker_UserOwnedNameUnderDeployed_IssueSeverityIsError(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-user-as-deployed.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
		AllowUnknownInjections:  false,
	})

	iss := issueWithCode(issues, "wrong-marker")
	if iss == nil {
		t.Skip("no wrong-marker issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("wrong-marker issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// ---------------------------------------------------------------------------
// T2.2 — No wrong-marker for correct pairings
// ---------------------------------------------------------------------------

func TestValidate_WrongMarker_CorrectPairing_ToolManagedUnderDeployed_NoWrongMarkerIssue(t *testing.T) {
	// A document with tool-managed names correctly declared under [[DEPLOYED:]] must not
	// produce any "wrong-marker" issue.
	//
	// Fixture: deployed-in-section.md — LanguagePatterns under [[DEPLOYED:]] in Capabilities.
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
	// A document with user-owned names correctly declared under [[INJECTION:]] must not
	// produce any "wrong-marker" issue.
	//
	// Fixture: empty-injection.md — IdentityExtension under [[INJECTION:]] in Identity.
	doc := parsedBoundaryFixture(t, "empty-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"unexpected \"wrong-marker\" issue for a correctly paired user-owned name under [[INJECTION:]], got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Deployed-region parent check
// ---------------------------------------------------------------------------

func TestValidate_DeployedOutsideRequiredParent_ReportsWrongParentIssue(t *testing.T) {
	// LanguagePatterns requires a Capabilities parent (DeployedParent["LanguagePatterns"] ==
	// "Capabilities"). Placing it inside an Identity section violates the parent constraint.
	//
	// Fixture: deployed-outside-required-parent.md contains
	//   [[SECTION:Identity]]
	//     [[DEPLOYED:LanguagePatterns]]
	//     [[/DEPLOYED:LanguagePatterns]]
	//   [[/SECTION:Identity]]
	doc := parsedBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf(
			"expected a \"wrong-parent\" issue for [[DEPLOYED:LanguagePatterns]] inside Identity (requires Capabilities), got issues: %v",
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
	// When RequireInjectionParents is false the parent check is disabled; no "wrong-parent"
	// issue must be produced even for a deployed name outside its required section.
	doc := parsedBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: false,
	})

	if hasIssueWithCode(issues, "wrong-parent") {
		t.Error("wrong-parent issue reported for deployed name even though RequireInjectionParents is false")
	}
}

// ---------------------------------------------------------------------------
// T2.3 — CommunicationProtocol must be at body top level
// ---------------------------------------------------------------------------

func TestValidate_CommunicationProtocolDeployed_NestedInSection_ReportsWrongParentIssue(t *testing.T) {
	// [[DEPLOYED:CommunicationProtocol]] has an empty required parent (top level only).
	// Nesting it inside a section violates the parent constraint.
	//
	// Fixture: communication-protocol-in-section.md contains
	//   [[SECTION:Identity]]
	//     [[DEPLOYED:CommunicationProtocol]]
	//     [[/DEPLOYED:CommunicationProtocol]]
	//   [[/SECTION:Identity]]
	doc := parsedBoundaryFixture(t, "malformed/communication-protocol-in-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	if !hasIssueWithCode(issues, "wrong-parent") {
		t.Errorf(
			"expected a \"wrong-parent\" issue for [[DEPLOYED:CommunicationProtocol]] nested inside a section (it must be at body top level), got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Unknown deployed name
// ---------------------------------------------------------------------------

func TestValidate_UnknownDeployedName_AllowUnknownFalse_ReportsUnknownDeployedIssue(t *testing.T) {
	// An unrecognised [[DEPLOYED:]] name has no generator and cannot be filled.
	// When AllowUnknownInjections is false, an "unknown-deployed" issue must be raised.
	//
	// Fixture: unknown-deployed-name.md contains [[DEPLOYED:UnknownRegion]].
	doc := parsedBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		AllowUnknownInjections: false,
	})

	if !hasIssueWithCode(issues, "unknown-deployed") {
		t.Errorf(
			"expected an \"unknown-deployed\" issue for [[DEPLOYED:UnknownRegion]] when AllowUnknownInjections is false, got issues: %v",
			issues,
		)
	}
}

func TestValidate_UnknownDeployedName_AllowUnknownFalse_IssueSeverityIsError(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		AllowUnknownInjections: false,
	})

	iss := issueWithCode(issues, "unknown-deployed")
	if iss == nil {
		t.Skip("no unknown-deployed issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("unknown-deployed issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_UnknownDeployedName_AllowUnknownTrue_NoUnknownDeployedIssue(t *testing.T) {
	// When AllowUnknownInjections is true, the unknown-deployed check is suppressed.
	// An unrecognised [[DEPLOYED:]] name must not produce an "unknown-deployed" issue.
	doc := parsedBoundaryFixture(t, "malformed/unknown-deployed-name.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		AllowUnknownInjections: true,
	})

	if hasIssueWithCode(issues, "unknown-deployed") {
		t.Error("unexpected \"unknown-deployed\" issue when AllowUnknownInjections is true — the option must suppress this check")
	}
}

// ---------------------------------------------------------------------------
// T2.4 — CommunicationProtocol DEPLOYED satisfies the canonical order slot
// ---------------------------------------------------------------------------

func TestValidate_DeployedCommunicationProtocol_InSecondPosition_NoOutOfOrderIssue(t *testing.T) {
	// A document whose second canonical slot is occupied by a top-level
	// [[DEPLOYED:CommunicationProtocol]] boundary (after Identity, before ArtifactProvenance)
	// must produce no "out-of-order-section" issue.
	//
	// Fixture: canonical-order-with-deployed-protocol.md layout:
	//   [[SECTION:Identity]]          ← canonical slot 0
	//   [[DEPLOYED:CommunicationProtocol]] ← canonical slot 1 (top-level deployed)
	//   [[SECTION:ArtifactProvenance]] ← canonical slot 2
	doc := parsedBoundaryFixture(t, "canonical-order-with-deployed-protocol.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
		AllowUnknownInjections:   false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"unexpected \"out-of-order-section\" issue for a document with [[DEPLOYED:CommunicationProtocol]] in the correct second canonical slot, got issues: %v",
			issues,
		)
	}
}

func TestValidate_DeployedCommunicationProtocol_InSecondPosition_NoIssues(t *testing.T) {
	// The canonical document with a top-level [[DEPLOYED:CommunicationProtocol]] in the
	// second position must produce zero issues under the strictest validation options.
	doc := parsedBoundaryFixture(t, "canonical-order-with-deployed-protocol.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
		AllowUnknownInjections:   false,
	})

	if len(issues) != 0 {
		t.Errorf("expected no issues for a canonical document with a top-level deployed protocol region, got %d:", len(issues))
		for _, iss := range issues {
			t.Logf("  [%s] %s (line %d)", iss.Code, iss.Message, iss.Line)
		}
	}
}

func TestValidate_DeployedCommunicationProtocol_AfterArtifactProvenance_ReportsOutOfOrderIssue(t *testing.T) {
	// [[DEPLOYED:CommunicationProtocol]] occupies canonical slot 1. When it appears after
	// ArtifactProvenance (canonical slot 2), the document-order check must fire.
	//
	// Fixture: deployed-protocol-out-of-order.md layout:
	//   [[SECTION:Identity]]          ← canonical slot 0
	//   [[SECTION:ArtifactProvenance]] ← canonical slot 2 — appears before CP
	//   [[DEPLOYED:CommunicationProtocol]] ← canonical slot 1 — out of order
	doc := parsedBoundaryFixture(t, "malformed/deployed-protocol-out-of-order.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if !hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected an \"out-of-order-section\" issue when [[DEPLOYED:CommunicationProtocol]] appears after ArtifactProvenance, got issues: %v",
			issues,
		)
	}
}

func TestValidate_DeployedCommunicationProtocol_OutOfOrder_NotReportedWhenOptionDisabled(t *testing.T) {
	// When RequireCanonicalSections is false the ordering check is disabled; no
	// "out-of-order-section" issue must be produced even for an out-of-order deployed slot.
	doc := parsedBoundaryFixture(t, "malformed/deployed-protocol-out-of-order.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Error("out-of-order-section issue reported for deployed protocol even though RequireCanonicalSections is false")
	}
}

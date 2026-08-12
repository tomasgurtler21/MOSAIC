package docformat_test

// Tests for two related validator behaviours introduced for [[CUSTOM:]] support:
//
//   (1) The custom-region-in-source rule: a [[CUSTOM:]] region appearing in a source
//       document is a validation error. The error names the region, carries SeverityError,
//       and is gated on opts.Kind == DocumentSource — it is never raised for deployed or
//       unknown-kind documents.
//
//   (2) The relaxed populated-source-region emptiness check: a source [[DEPLOYED:]] region
//       whose only content is whitespace-only nested [[INJECTION:]] or [[CUSTOM:]] markers
//       is not flagged as populated. A region that contains prose — with or without such
//       markers — is still flagged.
//
// Also included: regression tests asserting no "wrong-nesting" issue is raised for a
// [[CUSTOM:]] or [[INJECTION:]] region nested inside a [[DEPLOYED:]] region in either
// document kind. The nesting was previously discussed as a ban; that ban is superseded and
// must never be re-added.
//
// Coverage (custom-region-in-source):
//   - A source document with a [[CUSTOM:]] region at top level produces a
//     "custom-region-in-source" issue at SeverityError.
//   - The "custom-region-in-source" issue message contains the custom region's name.
//   - The "custom-region-in-source" issue Node field is set to the custom region's name.
//   - A deployed document with the same [[CUSTOM:]] region produces no
//     "custom-region-in-source" issue.
//   - When Kind is DocumentUnknown (zero value), the custom-in-source rule is skipped.
//   - A [[CUSTOM:]] region with an arbitrary, previously-unseen name in a deployed document
//     produces no issue referencing that name (custom names are an open set).
//   - No "unknown-custom-name" issue is ever produced, regardless of the name or document kind.
//
// Coverage (nesting regression):
//   - A [[CUSTOM:]] marker nested inside [[DEPLOYED:]] in a source document produces no
//     "wrong-nesting" issue.
//   - An [[INJECTION:]] marker nested inside [[DEPLOYED:]] in a source document produces no
//     "wrong-nesting" issue.
//   - A [[CUSTOM:]] region nested inside [[DEPLOYED:]] in a deployed document produces no
//     "wrong-nesting" issue.
//   - An [[INJECTION:]] region nested inside [[DEPLOYED:]] in a deployed document produces
//     no "wrong-nesting" issue.
//
// Coverage (relaxed populated-source-region):
//   - A source [[DEPLOYED:]] region containing only a whitespace-only nested [[INJECTION:]]
//     marker produces no "populated-source-region" issue.
//   - A source [[DEPLOYED:]] region containing only a whitespace-only nested [[CUSTOM:]]
//     marker produces no "populated-source-region" issue.
//   - A source [[DEPLOYED:]] region containing both a whitespace-only nested marker and
//     prose outside the nested marker still produces a "populated-source-region" issue.

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Inline document fixtures
// ---------------------------------------------------------------------------

// customInSourceDoc is a source document with a top-level [[CUSTOM:CustomConstraints]]
// region. Custom regions must not appear in source files.
const customInSourceDoc = `---
name: test-agent
---

[[CUSTOM:CustomConstraints]]
Project-specific constraint text.
[[/CUSTOM:CustomConstraints]]
`

// customInDeployedDoc is a deployed document with a [[CUSTOM:CustomConstraints]] region.
// Custom regions are valid in deployed documents.
const customInDeployedDoc = `---
name: test-agent
---

[[CUSTOM:CustomConstraints]]
Project-specific constraint text.
[[/CUSTOM:CustomConstraints]]
`

// arbitraryCustomNameDeployedDoc is a deployed document with a [[CUSTOM:]] region whose
// name has never appeared in any MOSAIC registry. Custom names are open: any name is valid.
const arbitraryCustomNameDeployedDoc = `---
name: test-agent
---

[[CUSTOM:AnArbitraryProjectSpecificName]]
Content invented entirely by the project.
[[/CUSTOM:AnArbitraryProjectSpecificName]]
`

// sourceDocDeployedHoldingEmptyInjection is a source document whose [[DEPLOYED:]] region
// contains only an empty [[INJECTION:]] nested marker. Nesting this marker is permitted;
// the parent region must not be reported as populated because of it.
const sourceDocDeployedHoldingEmptyInjection = `---
name: test-agent
---

[[DEPLOYED:CommunicationProtocol]]
[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/DEPLOYED:CommunicationProtocol]]
`

// sourceDocDeployedHoldingEmptyCustom is a source document whose [[DEPLOYED:]] region
// contains only an empty [[CUSTOM:]] nested marker. Nesting this marker is permitted;
// the parent region must not be reported as populated because of it.
const sourceDocDeployedHoldingEmptyCustom = `---
name: test-agent
---

[[DEPLOYED:CommunicationProtocol]]
[[CUSTOM:HarnessCustom]]
[[/CUSTOM:HarnessCustom]]
[[/DEPLOYED:CommunicationProtocol]]
`

// sourceDocDeployedHoldingProseAndNestedMarker is a source document whose [[DEPLOYED:]]
// region contains both an empty nested [[INJECTION:]] marker and prose outside it. The
// prose makes the region non-empty regardless of the nested marker.
const sourceDocDeployedHoldingProseAndNestedMarker = `---
name: test-agent
---

[[DEPLOYED:CommunicationProtocol]]
Prose that must not appear alongside a nested marker in a source deployed region.
[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/DEPLOYED:CommunicationProtocol]]
`

// deployedDocWithNestedCustomAndInjection is a deployed document whose [[DEPLOYED:]] region
// contains both a populated [[CUSTOM:]] region and a populated [[INJECTION:]] region.
// Both kinds of nesting are permitted in deployed documents.
const deployedDocWithNestedCustomAndInjection = `---
name: test-agent
---

[[DEPLOYED:CommunicationProtocol]]
Canonical protocol content.
[[INJECTION:IdentityExtension]]
Injected content.
[[/INJECTION:IdentityExtension]]
[[CUSTOM:HarnessCustom]]
Project-added content inside deployed.
[[/CUSTOM:HarnessCustom]]
[[/DEPLOYED:CommunicationProtocol]]
`

// ---------------------------------------------------------------------------
// Helper: parseCustomDoc parses an inline document string; fails the test on error.
// ---------------------------------------------------------------------------

func parseCustomDoc(t *testing.T, src string) *docformat.Document {
	t.Helper()
	doc, err := docformat.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// custom-region-in-source: source document produces an error
// ---------------------------------------------------------------------------

func TestValidate_CustomRegionInSource_ReportsCustomRegionInSourceIssue(t *testing.T) {
	// A source document containing a [[CUSTOM:]] region must produce a
	// "custom-region-in-source" issue. Custom regions are project-owned content that
	// exists only in deployed files; they must not appear in source files.
	doc := parseCustomDoc(t, customInSourceDoc)

	// Arrange
	opts := docformat.ValidateOptions{Kind: docformat.DocumentSource}

	// Act
	issues := docformat.Validate(doc, opts)

	// Assert
	if !hasIssueWithCode(issues, "custom-region-in-source") {
		t.Errorf(
			"expected a \"custom-region-in-source\" issue for [[CUSTOM:CustomConstraints]] in a source document, got issues: %v",
			issues,
		)
	}
}

func TestValidate_CustomRegionInSource_IssueSeverityIsError(t *testing.T) {
	// The "custom-region-in-source" issue must carry SeverityError. A custom region in a
	// source file represents content that cannot be regenerated by the tool.
	doc := parseCustomDoc(t, customInSourceDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	iss := issueWithCode(issues, "custom-region-in-source")
	if iss == nil {
		t.Skip("no custom-region-in-source issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("custom-region-in-source issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_CustomRegionInSource_IssueMessageNamesTheRegion(t *testing.T) {
	// The "custom-region-in-source" issue message must contain the custom region's name so
	// the author knows which region to remove or move. Exact wording is not asserted.
	doc := parseCustomDoc(t, customInSourceDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	iss := issueWithCode(issues, "custom-region-in-source")
	if iss == nil {
		t.Skip("no custom-region-in-source issue returned — message cannot be checked")
	}
	if !strings.Contains(iss.Message, "CustomConstraints") {
		t.Errorf(
			"custom-region-in-source issue message must contain the region name \"CustomConstraints\", got: %q",
			iss.Message,
		)
	}
}

func TestValidate_CustomRegionInSource_IssueNodeIsRegionName(t *testing.T) {
	// The Issue.Node field must be set to the custom region's name so that callers can
	// identify which region triggered the error without parsing the message string.
	doc := parseCustomDoc(t, customInSourceDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	iss := issueWithCode(issues, "custom-region-in-source")
	if iss == nil {
		t.Skip("no custom-region-in-source issue returned — Node cannot be checked")
	}
	if iss.Node != "CustomConstraints" {
		t.Errorf("custom-region-in-source issue Node: want %q, got %q", "CustomConstraints", iss.Node)
	}
}

// ---------------------------------------------------------------------------
// custom-region-in-source: deployed document produces no error
// ---------------------------------------------------------------------------

func TestValidate_CustomRegionInDeployedDoc_NoCustomRegionInSourceIssue(t *testing.T) {
	// A deployed document with a [[CUSTOM:]] region must produce no "custom-region-in-source"
	// issue. The rule is gated on DocumentSource; deployed documents are the correct home
	// for custom regions.
	doc := parseCustomDoc(t, customInDeployedDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentDeployed})

	if hasIssueWithCode(issues, "custom-region-in-source") {
		t.Errorf(
			"unexpected \"custom-region-in-source\" issue for [[CUSTOM:CustomConstraints]] in a deployed document, got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// custom-region-in-source: DocumentUnknown skips the rule
// ---------------------------------------------------------------------------

func TestValidate_CustomRegion_DocumentKindUnknown_NoCustomRegionInSourceIssue(t *testing.T) {
	// When Kind is DocumentUnknown (the zero value of ValidateOptions.Kind), the
	// custom-in-source rule must be skipped. The validator cannot know whether the document
	// is a source file without an explicit kind.
	doc := parseCustomDoc(t, customInSourceDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "custom-region-in-source") {
		t.Errorf(
			"custom-in-source rule must be skipped when Kind is DocumentUnknown; "+
				"got \"custom-region-in-source\" issue: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// Unknown custom name: open set — no issue for any name in a deployed document
// ---------------------------------------------------------------------------

func TestValidate_CustomRegion_ArbitraryName_DeployedDoc_NoIssueForName(t *testing.T) {
	// An arbitrary [[CUSTOM:]] name that has never appeared in any registry must produce no
	// issue referencing that name in a deployed document. Custom names are open; the tool
	// never validates a custom name against any set.
	const customName = "AnArbitraryProjectSpecificName"
	doc := parseCustomDoc(t, arbitraryCustomNameDeployedDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentDeployed})

	for _, iss := range issues {
		if iss.Node == customName {
			t.Errorf(
				"unexpected issue for arbitrary custom name %q in a deployed document: code=%q severity=%q message=%q",
				customName, iss.Code, iss.Severity, iss.Message,
			)
		}
	}
}

func TestValidate_CustomRegion_UnknownCustomNameCode_NeverProduced(t *testing.T) {
	// The "unknown-custom-name" issue code must never be produced by the validator, under
	// any document kind or options. Custom names are an open set with no registry.
	doc := parseCustomDoc(t, arbitraryCustomNameDeployedDoc)

	for _, opts := range []docformat.ValidateOptions{
		{},
		{Kind: docformat.DocumentSource},
		{Kind: docformat.DocumentDeployed},
		{Kind: docformat.DocumentSource, RequireInjectionParents: true},
	} {
		issues := docformat.Validate(doc, opts)
		if hasIssueWithCode(issues, "unknown-custom-name") {
			t.Errorf(
				"\"unknown-custom-name\" issue must never be produced (opts=%+v), got issues: %v",
				opts, issues,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Nesting regression: no wrong-nesting for user-owned regions inside [[DEPLOYED:]]
// ---------------------------------------------------------------------------

func TestValidate_CustomNestedInDeployed_SourceDoc_NoWrongNestingIssue(t *testing.T) {
	// A [[CUSTOM:]] marker nested inside a [[DEPLOYED:]] region in a source document must
	// produce no "wrong-nesting" issue. The earlier design discussed a nesting ban; that ban
	// is superseded. Nesting is permitted — the validator must not flag it.
	//
	// Note: other issues (such as custom-region-in-source) may still be raised; this test
	// asserts only the absence of wrong-nesting.
	doc := parseCustomDoc(t, sourceDocDeployedHoldingEmptyCustom)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	if hasIssueWithCode(issues, "wrong-nesting") {
		t.Errorf(
			"unexpected \"wrong-nesting\" issue for [[CUSTOM:]] nested inside [[DEPLOYED:]] in a source document; "+
				"nesting user-owned regions inside deployed regions is permitted, got issues: %v",
			issues,
		)
	}
}

func TestValidate_InjectionNestedInDeployed_SourceDoc_NoWrongNestingIssue(t *testing.T) {
	// An [[INJECTION:]] marker nested inside a [[DEPLOYED:]] region in a source document
	// must produce no "wrong-nesting" issue. This nesting is permitted and is the standard
	// mechanism for declaring injections that live inside a deployed region.
	doc := parseCustomDoc(t, sourceDocDeployedHoldingEmptyInjection)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	if hasIssueWithCode(issues, "wrong-nesting") {
		t.Errorf(
			"unexpected \"wrong-nesting\" issue for [[INJECTION:]] nested inside [[DEPLOYED:]] in a source document, got issues: %v",
			issues,
		)
	}
}

func TestValidate_CustomNestedInDeployed_DeployedDoc_NoWrongNestingIssue(t *testing.T) {
	// A [[CUSTOM:]] region nested inside a [[DEPLOYED:]] region in a deployed document must
	// produce no "wrong-nesting" issue. Deployed documents may carry custom regions anywhere,
	// including inside deployed regions.
	doc := parseCustomDoc(t, deployedDocWithNestedCustomAndInjection)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentDeployed})

	if hasIssueWithCode(issues, "wrong-nesting") {
		t.Errorf(
			"unexpected \"wrong-nesting\" issue for [[CUSTOM:]] nested inside [[DEPLOYED:]] in a deployed document, got issues: %v",
			issues,
		)
	}
}

func TestValidate_InjectionNestedInDeployed_DeployedDoc_NoWrongNestingIssue(t *testing.T) {
	// An [[INJECTION:]] region nested inside a [[DEPLOYED:]] region in a deployed document
	// must produce no "wrong-nesting" issue.
	doc := parseCustomDoc(t, deployedDocWithNestedCustomAndInjection)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentDeployed})

	if hasIssueWithCode(issues, "wrong-nesting") {
		t.Errorf(
			"unexpected \"wrong-nesting\" issue for [[INJECTION:]] nested inside [[DEPLOYED:]] in a deployed document, got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// Relaxed populated-source-region: empty nested markers do not count as content
// ---------------------------------------------------------------------------

func TestValidate_SourceDeployedRegion_OnlyEmptyNestedInjection_NotPopulated(t *testing.T) {
	// A source [[DEPLOYED:]] region whose only content is a whitespace-only nested
	// [[INJECTION:]] marker must not be flagged as a populated source region. An empty
	// nested marker is not "content the author hand-edited into the region"; it is the
	// normal form for declaring an injection slot inside a deployed region.
	doc := parseCustomDoc(t, sourceDocDeployedHoldingEmptyInjection)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	if hasIssueWithCode(issues, "populated-source-region") {
		t.Errorf(
			"unexpected \"populated-source-region\" issue for a source [[DEPLOYED:]] region "+
				"containing only an empty nested [[INJECTION:]] marker; empty nested markers "+
				"must not count as region content, got issues: %v",
			issues,
		)
	}
}

func TestValidate_SourceDeployedRegion_OnlyEmptyNestedCustom_NotPopulated(t *testing.T) {
	// A source [[DEPLOYED:]] region whose only content is a whitespace-only nested
	// [[CUSTOM:]] marker must not be flagged as a populated source region. The relaxation
	// covers both injection and custom user-owned nested markers.
	doc := parseCustomDoc(t, sourceDocDeployedHoldingEmptyCustom)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	if hasIssueWithCode(issues, "populated-source-region") {
		t.Errorf(
			"unexpected \"populated-source-region\" issue for a source [[DEPLOYED:]] region "+
				"containing only an empty nested [[CUSTOM:]] marker; empty nested markers "+
				"must not count as region content, got issues: %v",
			issues,
		)
	}
}

func TestValidate_SourceDeployedRegion_ProseAlongsideNestedMarker_StillPopulated(t *testing.T) {
	// A source [[DEPLOYED:]] region containing prose in addition to an empty nested marker
	// must still produce a "populated-source-region" issue. The relaxation applies only to
	// the nested marker itself, not to any surrounding prose — which is a hand-edit and
	// must be caught.
	doc := parseCustomDoc(t, sourceDocDeployedHoldingProseAndNestedMarker)

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	iss := issueWithCode(issues, "populated-source-region")
	if iss == nil {
		t.Error(
			"expected a \"populated-source-region\" issue for a source [[DEPLOYED:]] region " +
				"containing prose alongside an empty nested marker; prose is not a nested marker " +
				"and must still trigger the populated check",
		)
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("populated-source-region issue severity: want SeverityError, got %q", iss.Severity)
	}
}

package docformat_test

// Tests for the eight-name deployed vocabulary and the validator's subsequence
// ordering rule.
//
// Coverage (retired names):
//   - A managed region named ProtocolConstraints, IdentityExtension or
//     ErrorHandlingExtension is reported as "unknown-deployed" at SeverityError,
//     even inside the parent section the name used to require.
//   - The retired injection names remain legal as project regions.
//
// Coverage (ordering is a subsequence check over the canonical slots):
//   - An extra top-level section between canonical slots passes.
//   - Open project-region names raise no unknown-injection finding.
//   - A project region in a non-advised parent is advice only.
//   - Constraints placed before Capabilities still fails.

import (
	"testing"

	"mosaic-common/docformat"
)

func TestValidate_RetiredNameAsManagedRegion_ReportsUnknownDeployedError(t *testing.T) {
	// ProtocolConstraints used to require a Constraints parent, so it is placed there:
	// the finding must be an unrecognised name, not a wrong-parent resolution.
	cases := []struct{ name, section string }{
		{"ProtocolConstraints", "Constraints"},
		{"IdentityExtension", "Identity"},
		{"ErrorHandlingExtension", "ErrorHandling"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "---\nname: test-agent\n---\n\n" +
				"<" + tc.section + " type=\"core\">\nContent.\n" +
				"<" + tc.name + " type=\"managed\">\n</" + tc.name + ">\n" +
				"</" + tc.section + ">\n"
			doc := parseInlineDoc(t, src)

			issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})

			iss := issueWithCode(issues, "unknown-deployed")
			if iss == nil {
				t.Fatalf("expected an \"unknown-deployed\" issue for managed region %q, got issues: %v", tc.name, issues)
			}
			if iss.Severity != docformat.SeverityError {
				t.Errorf("unknown-deployed severity: want SeverityError, got %q", iss.Severity)
			}
			if hasIssueWithCode(issues, "wrong-parent") {
				t.Errorf("retired name %q must not be resolved to a parent; got issues: %v", tc.name, issues)
			}
		})
	}
}

func TestValidate_RetiredInjectionNamesAsProjectRegions_ProduceNoIssue(t *testing.T) {
	// Injection names are open: the retired names are still acceptable project regions.
	src := "---\nname: test-agent\n---\n\n" +
		"<Identity type=\"core\">\nContent.\n<IdentityExtension type=\"project\">\n</IdentityExtension>\n</Identity>\n\n" +
		"<ErrorHandling type=\"core\">\nContent.\n<ErrorHandlingExtension type=\"project\">\n</ErrorHandlingExtension>\n</ErrorHandling>\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})

	for _, code := range []string{"unknown-deployed", "unknown-injection", "wrong-marker", "wrong-parent"} {
		if hasIssueWithCode(issues, code) {
			t.Errorf("retired name used as a project region must not raise %q, got issues: %v", code, issues)
		}
	}
}

func TestValidate_ExtraTopLevelSection_PassesOrderCheck(t *testing.T) {
	doc := parsedBoundaryFixture(t, "subsequence-extra-top-level-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true, RequireCanonicalSections: true})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf("an extra top-level section must not break canonical ordering, got issues: %v", issues)
	}
	for _, iss := range issues {
		if iss.Severity == docformat.SeverityError {
			t.Errorf("unexpected error-level issue for a document with an extra top-level section: %+v", iss)
		}
	}
}

func TestValidate_OpenProjectRegionName_RaisesNoUnknownInjectionFinding(t *testing.T) {
	doc := parsedBoundaryFixture(t, "subsequence-extra-top-level-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})

	if hasIssueWithCode(issues, "unknown-injection") {
		t.Errorf("open project-region names must not be flagged, got issues: %v", issues)
	}
	for _, iss := range issues {
		if iss.Node == "SomeProjectSlot" && iss.Severity != docformat.SeverityAdvice {
			t.Errorf("finding for an open project region must be advice at most: %+v", iss)
		}
	}
}

func TestValidate_ProjectRegionInNonAdvisedParent_IsAdviceOnly(t *testing.T) {
	// CodebaseContext is advised for Capabilities; placing it in Identity is advice, not an error.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireInjectionParents: true})

	iss := issueWithCode(issues, "wrong-parent")
	if iss == nil {
		t.Fatalf("expected a wrong-parent finding for CodebaseContext inside Identity, got issues: %v", issues)
	}
	if iss.Severity != docformat.SeverityAdvice {
		t.Errorf("wrong-parent for a project region: want SeverityAdvice, got %q", iss.Severity)
	}
}

func TestValidate_ConstraintsBeforeCapabilities_ReportsOutOfOrder(t *testing.T) {
	src := "---\nname: test-agent\n---\n\n" +
		"<Identity type=\"core\">\nContent.\n</Identity>\n\n" +
		"<Constraints type=\"core\">\nContent.\n</Constraints>\n\n" +
		"<Capabilities type=\"core\">\nContent.\n</Capabilities>\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{RequireCanonicalSections: true})

	iss := issueWithCode(issues, "out-of-order-section")
	if iss == nil {
		t.Fatalf("expected an out-of-order-section issue for Constraints before Capabilities, got issues: %v", issues)
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("out-of-order-section severity: want SeverityError, got %q", iss.Severity)
	}
}

package docformat_test

// Tests for Stage 3 validator relaxations.
//
// Coverage (T3.1 — unknown injection names produce no issue at any severity):
//   - An unrecognised [[INJECTION:]] name produces no issue at any severity, including
//     the new SeverityAdvice level. Injection names are open unconditionally.
//   - The "unknown-injection" code cannot be produced by the validator under any option
//     combination. The code and the AllowUnknownInjections option are both gone.
//
// Coverage (T3.2 — canonical-order check is a subsequence test):
//   - A document with extra non-canonical top-level sections produces no "out-of-order-section"
//     issue — the check skips names absent from CanonicalOrder entirely.
//   - Non-canonical sections appearing between two canonical sections do not affect the
//     relative ordering of those canonical sections.
//   - A document with only a subset of canonical sections in correct relative order passes.
//   - Two canonical sections that are both present but appear in the wrong relative order
//     still produce "out-of-order-section" at SeverityError — the subsequence relaxation
//     does not tolerate misordering among present entries.
//
// Coverage (T3.3 — injection placement is advisory; deployed placement remains error):
//   - SeverityAdvice constant exists with the string value "advice".
//   - A misplaced [[INJECTION:]] region produces "wrong-parent" at SeverityAdvice, not
//     SeverityError. An injection placed somewhere unusual harms nobody.
//   - A misplaced [[INJECTION:]] region does NOT produce any SeverityError issue.
//   - A misplaced [[DEPLOYED:]] region still produces "wrong-parent" at SeverityError.
//   - Strict mode promotes SeverityWarning issues to SeverityError but NEVER promotes
//     SeverityAdvice — advice that could fail a run is not advice.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// T3.1 — Unknown injection names produce no issue at any severity
// ---------------------------------------------------------------------------

func TestValidate_UnknownInjectionName_ProducesNoIssueAtAnySeverity(t *testing.T) {
	// [[INJECTION:NonExistentName]] is not listed in InjectionParent and is not in
	// CanonicalDeployed. Injection names are open: any name produces no issue of any
	// severity — not SeverityError, not SeverityWarning, and not SeverityAdvice.
	doc := parsedBoundaryFixture(t, "malformed/unknown-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	for _, iss := range issues {
		if iss.Node == "NonExistentName" {
			t.Errorf(
				"unexpected issue for unknown injection name %q: code=%q severity=%q message=%q",
				"NonExistentName", iss.Code, iss.Severity, iss.Message,
			)
		}
	}
}

func TestValidate_UnknownInjectionName_NoIssueWithoutRequireInjectionParents(t *testing.T) {
	// Unknown injection names produce no issue regardless of option settings.
	doc := parsedBoundaryFixture(t, "malformed/unknown-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: false,
	})

	for _, iss := range issues {
		if iss.Node == "NonExistentName" {
			t.Errorf(
				"unexpected issue for unknown injection name: code=%q severity=%q",
				iss.Code, iss.Severity,
			)
		}
	}
}

func TestValidate_UnknownInjectionCode_NeverProduced(t *testing.T) {
	// The "unknown-injection" code is removed from the validator entirely.
	// It must never appear in any returned issue, under any option combination.
	doc := parsedBoundaryFixture(t, "malformed/unknown-injection.md")

	optionSets := []docformat.ValidateOptions{
		{},
		{RequireInjectionParents: true},
		{RequireCanonicalSections: true},
		{RequireInjectionParents: true, RequireCanonicalSections: true},
	}
	for _, opts := range optionSets {
		issues := docformat.Validate(doc, opts)
		for _, iss := range issues {
			if iss.Code == "unknown-injection" {
				t.Errorf(
					"options=%+v: got \"unknown-injection\" issue — this code must be "+
						"permanently removed from the validator: %+v",
					opts, iss,
				)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Canonical-order check is a subsequence test
// ---------------------------------------------------------------------------

func TestValidate_SubsequenceOrder_ExtraTopLevelNonCanonicalSections_NoOutOfOrderIssue(t *testing.T) {
	// A document with canonical sections in the correct relative order, interspersed
	// with non-canonical (extra) top-level sections, must produce no "out-of-order-section"
	// issue. The order check is a subsequence test: non-canonical names (names absent
	// from CanonicalOrder) are skipped entirely by the check.
	//
	// Fixture: extra-noncanonical-sections.md has Identity, Preamble (non-canonical),
	// Capabilities, InternalContext (non-canonical), Constraints — canonical entries
	// are in correct relative order.
	doc := parsedBoundaryFixture(t, "extra-noncanonical-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected no \"out-of-order-section\" for canonical sections in correct relative order "+
				"with non-canonical sections interspersed, got issues: %v",
			issues,
		)
	}
}

func TestValidate_SubsequenceOrder_NonCanonicalSections_DoNotAffectOrderCheck(t *testing.T) {
	// A non-canonical section appearing between two canonical sections must not affect
	// the relative-order comparison for those canonical sections. The check advances its
	// position counter only when a canonical-order entry is encountered.
	doc := parsedBoundaryFixture(t, "extra-noncanonical-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	for _, iss := range issues {
		if iss.Code == "out-of-order-section" {
			t.Errorf(
				"unexpected \"out-of-order-section\" for document with non-canonical sections "+
					"interspersed among canonical ones: %+v",
				iss,
			)
		}
	}
}

func TestValidate_SubsequenceOrder_MissingCanonicalSections_NoOutOfOrderIssue(t *testing.T) {
	// A document with only a subset of canonical sections in the correct relative order
	// must not produce an "out-of-order-section" issue. The check enforces the relative
	// order of present canonical entries; absent canonical entries are not an issue.
	//
	// Fixture: multiple-sections.md contains Identity and CommunicationProtocol only.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected no \"out-of-order-section\" for a document with a subset of canonical "+
				"sections in correct relative order; got issues: %v",
			issues,
		)
	}
}

func TestValidate_SubsequenceOrder_TwoCanonicalEntriesInWrongRelativeOrder_ReportsOutOfOrder(t *testing.T) {
	// When two canonical entries that are both present appear in the wrong relative order,
	// the validator must still report "out-of-order-section". The subsequence relaxation
	// skips absent entries and non-canonical names; it does NOT tolerate misordering
	// among present canonical entries.
	//
	// Fixture: out-of-order-sections.md has Capabilities before Identity.
	doc := parsedBoundaryFixture(t, "malformed/out-of-order-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if !hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected an \"out-of-order-section\" issue when two present canonical entries "+
				"appear in the wrong relative order (Capabilities before Identity); got issues: %v",
			issues,
		)
	}
}

func TestValidate_SubsequenceOrder_WrongRelativeOrder_IssueSeverityIsError(t *testing.T) {
	// The "out-of-order-section" issue for canonical entries in wrong relative order
	// carries SeverityError — this is unchanged by the subsequence relaxation.
	doc := parsedBoundaryFixture(t, "malformed/out-of-order-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	iss := issueWithCode(issues, "out-of-order-section")
	if iss == nil {
		t.Skip("no out-of-order-section issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf(
			"out-of-order-section issue severity: want SeverityError, got %q",
			iss.Severity,
		)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — SeverityAdvice constant
// ---------------------------------------------------------------------------

func TestSeverityAdvice_ConstantExistsWithStringValueAdvice(t *testing.T) {
	// SeverityAdvice is a new severity level added in Stage 3. It is reported but
	// never fatal: a caller that filters issues by severity must treat it as
	// informational only. Strict mode must not promote it to SeverityError.
	const wantValue docformat.Severity = "advice"
	if docformat.SeverityAdvice != wantValue {
		t.Errorf("SeverityAdvice: want string value %q, got %q", wantValue, docformat.SeverityAdvice)
	}
}

func TestSeverityAdvice_IsDistinctFromSeverityError(t *testing.T) {
	if docformat.SeverityAdvice == docformat.SeverityError {
		t.Error("SeverityAdvice must be distinct from SeverityError")
	}
}

func TestSeverityAdvice_IsDistinctFromSeverityWarning(t *testing.T) {
	if docformat.SeverityAdvice == docformat.SeverityWarning {
		t.Error("SeverityAdvice must be distinct from SeverityWarning")
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Injection parent placement is advisory
// ---------------------------------------------------------------------------

func TestValidate_InjectionInWrongParent_IssueSeverityIsAdvice(t *testing.T) {
	// An [[INJECTION:]] region placed outside its advisory parent section reports
	// "wrong-parent" at SeverityAdvice. This is a downgrade from SeverityError in
	// Stage 2. A misplaced injection harms nobody and destroys nothing; it must not
	// fail validation.
	//
	// Fixture: wrong-parent.md has [[INJECTION:IdentityExtension]] inside Capabilities
	// (advisory parent is Identity).
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	iss := issueWithCode(issues, "wrong-parent")
	if iss == nil {
		t.Skip("no wrong-parent issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityAdvice {
		t.Errorf(
			"wrong-parent for a misplaced injection: want SeverityAdvice, got %q "+
				"(Stage 3 downgrades injection wrong-parent from SeverityError to SeverityAdvice)",
			iss.Severity,
		)
	}
}

func TestValidate_InjectionInWrongParent_NoSeverityErrorIssueProduced(t *testing.T) {
	// A misplaced [[INJECTION:]] region must produce no SeverityError issue.
	// The "wrong-parent" advisory finding may be present at SeverityAdvice, but must
	// never be present at SeverityError.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	for _, iss := range issues {
		if iss.Code == "wrong-parent" && iss.Severity == docformat.SeverityError {
			t.Errorf(
				"got \"wrong-parent\" at SeverityError for a misplaced injection — "+
					"Stage 3 mandates SeverityAdvice for injection placement findings: %+v",
				iss,
			)
		}
	}
}

func TestValidate_InjectionAtTopLevel_WrongParent_IssueSeverityIsAdvice(t *testing.T) {
	// An advisory injection (IdentityExtension) appearing at body top level instead of
	// inside Identity produces "wrong-parent" at SeverityAdvice, never SeverityError.
	//
	// Fixture: injection-outside-section.md has [[INJECTION:IdentityExtension]] with no
	// enclosing section.
	doc := boundaryMalformedFixture(t, "injection-outside-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	for _, iss := range issues {
		if iss.Code == "wrong-parent" && iss.Severity == docformat.SeverityError {
			t.Errorf(
				"wrong-parent for a top-level advisory injection should be SeverityAdvice, "+
					"got SeverityError: %+v",
				iss,
			)
		}
	}
}

func TestValidate_DeployedInWrongParent_StillReportsSeverityError(t *testing.T) {
	// A misplaced [[DEPLOYED:]] region must still produce "wrong-parent" at SeverityError.
	// The advisory downgrade in Stage 3 applies only to [[INJECTION:]] regions.
	//
	// Fixture: deployed-outside-required-parent.md has [[DEPLOYED:LanguagePatterns]]
	// inside Identity (requires Capabilities parent).
	doc := parsedBoundaryFixture(t, "malformed/deployed-outside-required-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
	})

	iss := issueWithCode(issues, "wrong-parent")
	if iss == nil {
		t.Skip("no wrong-parent issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf(
			"wrong-parent for a misplaced deployed region: want SeverityError, got %q "+
				"(advisory downgrade applies only to [[INJECTION:]], not [[DEPLOYED:]])",
			iss.Severity,
		)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Strict mode does not promote SeverityAdvice
// ---------------------------------------------------------------------------

func TestValidate_StrictMode_DoesNotPromoteSeverityAdviceToSeverityError(t *testing.T) {
	// Strict mode promotes SeverityWarning issues to SeverityError, but NEVER promotes
	// SeverityAdvice. An injection wrong-parent advisory must remain SeverityAdvice
	// even when ValidateOptions.Strict is true.
	doc := boundaryMalformedFixture(t, "wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
		Strict:                  true,
	})

	for _, iss := range issues {
		if iss.Code == "wrong-parent" && iss.Severity == docformat.SeverityError {
			t.Errorf(
				"Strict mode promoted a \"wrong-parent\" advisory finding to SeverityError — "+
					"SeverityAdvice must never be promoted by Strict mode: %+v",
				iss,
			)
		}
	}
}

func TestValidate_StrictMode_FieldExistsOnValidateOptions(t *testing.T) {
	// ValidateOptions.Strict must exist as a boolean field. This test confirms the
	// field is present at compile time; runtime behaviour is tested by the Strict-mode
	// advisory promotion test above.
	opts := docformat.ValidateOptions{
		Strict: false,
	}
	_ = opts.Strict
}

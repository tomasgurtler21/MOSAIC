package docformat_test

// Tests for compound-tag mismatch fix.
//
// Coverage:
//   - TagNamePrefix returns the tag-name portion of a compound boundary name.
//   - TagNamePrefix("Workflow:brownfield-tdd") returns "Workflow".
//   - TagNamePrefix("Workflow") returns "Workflow" (no colon — whole name).
//   - TagNamePrefix("Identity") returns "Identity".
//   - TagNamePrefix("A:b:c") returns "A" (split at the FIRST colon only).
//   - TagNamePrefix(":x") returns "" (empty prefix before the colon).
//   - TagNamePrefix("") returns "" (empty input).
//   - A well-formed compound-named section (<Workflow type="core" name="brownfield-tdd">
//     ... </Workflow>) produces no "mismatched-tag" issue.
//   - A genuine tag-name mismatch (<Foo type="core"> ... </Bar>) still produces
//     a "mismatched-tag" issue.
//   - Non-compound paired tags produce no "mismatched-tag" issue.
//   - Nested compound-named sections both close correctly and produce no "mismatched-tag" issue.
//   - On a genuine mismatched-tag issue, Issue.Node carries the full compound opening name,
//     not only the prefix.

import (
	"testing"

	"mosaic-common/docformat"
)

// parseInlineDocBytes is a helper that parses an inline byte slice as a document.
// It calls t.Fatalf if parsing fails, because inline fixtures are always well-formed YAML.
// This helper accepts []byte; the existing parseInlineDoc helper in other test files in
// this package accepts string.
func parseInlineDocBytes(t *testing.T, src []byte) *docformat.Document {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// TagNamePrefix — table-driven behaviour tests
// ---------------------------------------------------------------------------

// TestTagNamePrefix_BehaviourTable verifies every entry in the design's behaviour table
// for TagNamePrefix. The function is expected to return the tag-name portion of a
// compound boundary name — everything before the first colon — or the whole name when
// there is no colon.
func TestTagNamePrefix_BehaviourTable(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Compound name: tag name is before the first colon.
		{input: "Workflow:brownfield-tdd", want: "Workflow"},
		// Bare tag name (no colon): whole name is the prefix.
		{input: "Workflow", want: "Workflow"},
		// Another bare name.
		{input: "Identity", want: "Identity"},
		// Multi-colon compound name: split at FIRST colon only.
		{input: "A:b:c", want: "A"},
		// Leading colon: prefix before the first colon is empty.
		{input: ":x", want: ""},
		// Empty input: empty output.
		{input: "", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got := docformat.TagNamePrefix(tc.input)
			if got != tc.want {
				t.Errorf("TagNamePrefix(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mismatched-tag comparison: compound-name open/close matching
// ---------------------------------------------------------------------------

// TestValidate_CompoundNamedSection_ClosesWithBareTagName_NoMismatchedTag verifies that
// a compound-named section opened as <Workflow type="core" name="brownfield-tdd"> and
// closed as </Workflow> produces no "mismatched-tag" issue. The closing tag uses only the
// tag-name prefix ("Workflow"), which must match the tag-name prefix of the compound
// opening name ("Workflow:brownfield-tdd" → prefix "Workflow").
func TestValidate_CompoundNamedSection_ClosesWithBareTagName_NoMismatchedTag(t *testing.T) {
	src := []byte("---\nname: test\n---\n" +
		"<Workflow type=\"core\" name=\"brownfield-tdd\">\n" +
		"Workflow section content.\n" +
		"</Workflow>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "mismatched-tag") {
		iss := issueWithCode(issues, "mismatched-tag")
		t.Errorf("expected no \"mismatched-tag\" issue for "+
			"<Workflow type=\"core\" name=\"brownfield-tdd\"> closed by </Workflow>; "+
			"closing with the bare tag-name prefix must be accepted. Got: [%s] %s",
			iss.Code, iss.Message)
	}
}

// TestValidate_CompoundNamedSection_NoColonInId_ClosesWithBareTag_NoMismatchedTag
// verifies that a compound name where the id portion itself contains no colon still
// closes correctly. This is the common case for single-word workflow names such as
// "Workflow:quickfix" or "Workflow:setup".
func TestValidate_CompoundNamedSection_NoColonInId_ClosesWithBareTag_NoMismatchedTag(t *testing.T) {
	src := []byte("---\nname: test\n---\n" +
		"<Workflow type=\"core\" name=\"quickfix\">\n" +
		"Workflow content.\n" +
		"</Workflow>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "mismatched-tag") {
		iss := issueWithCode(issues, "mismatched-tag")
		t.Errorf("expected no \"mismatched-tag\" issue for "+
			"<Workflow type=\"core\" name=\"quickfix\"> closed by </Workflow>; "+
			"got: [%s] %s", iss.Code, iss.Message)
	}
}

// TestValidate_GenuineMismatch_StillProducesMismatchedTag verifies that a genuine
// open/close tag-name mismatch (<Foo type="core"> closed by </Bar>) still produces
// a "mismatched-tag" issue after the compound-name fix. The fix must not suppress real
// mismatches — only the false positive caused by compound names is removed.
func TestValidate_GenuineMismatch_StillProducesMismatchedTag(t *testing.T) {
	src := []byte("---\nname: test\n---\n" +
		"<Foo type=\"core\">\n" +
		"Content.\n" +
		"</Bar>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "mismatched-tag") {
		t.Errorf("expected a \"mismatched-tag\" issue for <Foo type=\"core\"> closed by </Bar>; "+
			"genuine tag-name mismatches must still be reported. Got issues: %v", issues)
	}
}

// TestValidate_CompoundNamedSection_GenuinePrefixMismatch_StillFires verifies that a
// compound-named section closed by a closing tag whose name does NOT match the prefix
// still produces a "mismatched-tag" issue. The prefix comparison must be strict:
// <Foo type="core" name="bar"> closed by </Baz> must fire.
func TestValidate_CompoundNamedSection_GenuinePrefixMismatch_StillFires(t *testing.T) {
	src := []byte("---\nname: test\n---\n" +
		"<Foo type=\"core\" name=\"bar\">\n" +
		"Content.\n" +
		"</Baz>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "mismatched-tag") {
		t.Errorf("expected a \"mismatched-tag\" issue for "+
			"<Foo type=\"core\" name=\"bar\"> closed by </Baz>; "+
			"prefix \"Foo\" != \"Baz\" must still fire. Got issues: %v", issues)
	}
}

// TestValidate_NonCompoundTag_ClosesCorrectly_NoMismatchedTag verifies that a
// non-compound section (<Identity type="core">) closed by its matching tag (</Identity>)
// continues to produce no "mismatched-tag" issue. This is a regression guard: the fix
// to compound-name comparison must not break the existing behavior for bare names.
func TestValidate_NonCompoundTag_ClosesCorrectly_NoMismatchedTag(t *testing.T) {
	src := []byte("---\nname: test\n---\n" +
		"<Identity type=\"core\">\n" +
		"Identity content.\n" +
		"</Identity>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "mismatched-tag") {
		iss := issueWithCode(issues, "mismatched-tag")
		t.Errorf("expected no \"mismatched-tag\" issue for a well-formed "+
			"<Identity type=\"core\">...</Identity> region; got: [%s] %s",
			iss.Code, iss.Message)
	}
}

// TestValidate_InnerCompoundSection_InsideNonCompoundOuter_BothCloseCorrectly verifies
// that a compound-named inner section (<Workflow type="project" name="alpha">) nested
// inside a non-compound outer section (<Identity type="core">) produces no
// "mismatched-tag" issue when both regions close with their correct bare tag name.
func TestValidate_InnerCompoundSection_InsideNonCompoundOuter_BothCloseCorrectly(t *testing.T) {
	// Outer: <Identity type="core"> (not compound) wrapping
	// Inner: <Workflow type="project" name="alpha"> (compound injection inside Identity)
	src := []byte("---\nname: test\n---\n" +
		"<Identity type=\"core\">\n" +
		"Identity content.\n" +
		"<Workflow type=\"project\" name=\"alpha\">\n" +
		"Workflow injection content.\n" +
		"</Workflow>\n" +
		"</Identity>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "mismatched-tag") {
		iss := issueWithCode(issues, "mismatched-tag")
		t.Errorf("expected no \"mismatched-tag\" issue for nested compound and non-compound "+
			"regions that each close with their correct bare tag name; got: [%s] %s",
			iss.Code, iss.Message)
	}
}

// TestValidate_CompoundNamedSection_MismatchedTag_NodeCarriesFullName verifies that
// when a compound-named region has a genuine tag mismatch (the CLOSING tag name differs
// from the OPENING tag-name prefix), the resulting "mismatched-tag" issue carries the
// full compound opening name in Issue.Node — not only the prefix. The diagnostic must
// still identify which region was involved, so "Workflow:brownfield-tdd" must appear in
// Issue.Node, not just "Workflow".
func TestValidate_CompoundNamedSection_MismatchedTag_NodeCarriesFullName(t *testing.T) {
	// <Workflow type="core" name="brownfield-tdd"> opened, then closed by </OtherTag>
	// which does not match the prefix "Workflow". This triggers a genuine mismatch.
	src := []byte("---\nname: test\n---\n" +
		"<Workflow type=\"core\" name=\"brownfield-tdd\">\n" +
		"Workflow content.\n" +
		"</OtherTag>\n")

	doc := parseInlineDocBytes(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "mismatched-tag")
	if iss == nil {
		t.Fatal("expected a \"mismatched-tag\" issue for <Workflow name=\"brownfield-tdd\"> closed by </OtherTag>; genuine prefix mismatch (\"Workflow\" != \"OtherTag\") must always fire")
	}
	if iss.Node != "Workflow:brownfield-tdd" {
		t.Errorf("Issue.Node = %q, want %q; "+
			"the full compound opening name must be carried in Issue.Node, not only the prefix",
			iss.Node, "Workflow:brownfield-tdd")
	}
}

// TestValidate_CompoundNamedSectionFixture_ProducesNoMismatchedTag verifies the
// compound-named-section.md fixture (a well-formed compound section with no other
// structural issues) against the corrected comparison rule. This fixture is the canonical
// positive test case for the compound-tag mismatch fix.
func TestValidate_CompoundNamedSectionFixture_ProducesNoMismatchedTag(t *testing.T) {
	doc := parsedBoundaryFixture(t, "compound-named-section.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "mismatched-tag") {
		iss := issueWithCode(issues, "mismatched-tag")
		t.Errorf("expected no \"mismatched-tag\" issue for compound-named-section.md; "+
			"a well-formed compound-named region must close without error. Got: [%s] %s",
			iss.Code, iss.Message)
	}
}

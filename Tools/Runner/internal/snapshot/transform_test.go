package snapshot_test

// Tests for TransformationsFor and TransformFile.
//
// TransformationsFor coverage:
//   - opencode harness returns exactly one rule: {Field:"mode", OldValue:"subagent", NewValue:"primary"}
//   - claude-code harness returns nil (no rules)
//   - ghcp-cli harness returns nil (no rules)
//   - fake harness returns nil (no rules)
//   - unknown / empty harness ID returns nil (no rules)
//
// TransformFile coverage:
//   - File with matching frontmatter field is rewritten correctly
//   - Only the targeted field value is replaced; other frontmatter lines are unchanged
//   - Content after the closing frontmatter delimiter is unchanged
//   - File without a matching field value is returned unchanged
//   - File without any YAML frontmatter (no leading "---") is returned unchanged
//   - File with frontmatter but no closing "---" delimiter: frontmatter block
//     is never closed, so no transformation is applied and content is returned unchanged
//   - Multiple rules applied in one pass: each rule transforms its targeted field
//   - Field matching ignores leading and trailing whitespace around the value
//   - Non-.md-style content (plain text, no frontmatter) passes through unchanged

import (
	"bytes"
	"testing"

	"mosaic-run/internal/snapshot"
)

// ---------------------------------------------------------------------------
// TransformationsFor
// ---------------------------------------------------------------------------

func TestTransformationsFor_OpenCode(t *testing.T) {
	rules := snapshot.TransformationsFor("opencode")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule for opencode, got %d", len(rules))
	}
	r := rules[0]
	if r.Field != "mode" {
		t.Errorf("Field: got %q, want %q", r.Field, "mode")
	}
	if r.OldValue != "subagent" {
		t.Errorf("OldValue: got %q, want %q", r.OldValue, "subagent")
	}
	if r.NewValue != "primary" {
		t.Errorf("NewValue: got %q, want %q", r.NewValue, "primary")
	}
}

func TestTransformationsFor_ClaudeCode_ReturnsNil(t *testing.T) {
	rules := snapshot.TransformationsFor("claude-code")
	if rules != nil {
		t.Errorf("expected nil rules for claude-code, got %v", rules)
	}
}

func TestTransformationsFor_GhcpCLI_ReturnsNil(t *testing.T) {
	rules := snapshot.TransformationsFor("ghcp-cli")
	if rules != nil {
		t.Errorf("expected nil rules for ghcp-cli, got %v", rules)
	}
}

func TestTransformationsFor_Fake_ReturnsNil(t *testing.T) {
	rules := snapshot.TransformationsFor("fake")
	if rules != nil {
		t.Errorf("expected nil rules for fake harness, got %v", rules)
	}
}

func TestTransformationsFor_UnknownHarness_ReturnsNil(t *testing.T) {
	rules := snapshot.TransformationsFor("not-a-real-harness")
	if rules != nil {
		t.Errorf("expected nil rules for unknown harness, got %v", rules)
	}
}

func TestTransformationsFor_EmptyID_ReturnsNil(t *testing.T) {
	rules := snapshot.TransformationsFor("")
	if rules != nil {
		t.Errorf("expected nil rules for empty harness ID, got %v", rules)
	}
}

// ---------------------------------------------------------------------------
// TransformFile
// ---------------------------------------------------------------------------

// openCodeRule is the single transformation applied to opencode agent files.
var openCodeRule = snapshot.TransformRule{
	Field:    "mode",
	OldValue: "subagent",
	NewValue: "primary",
}

func TestTransformFile_ReplacesMatchingField(t *testing.T) {
	input := []byte("---\nmode: subagent\ntitle: My Agent\n---\n\n# Body\n")
	want := []byte("---\nmode: primary\ntitle: My Agent\n---\n\n# Body\n")

	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Equal(got, want) {
		t.Errorf("TransformFile output mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTransformFile_OnlyTargetedFieldIsReplaced(t *testing.T) {
	input := []byte("---\nmode: subagent\nauthor: someone\n---\n\nBody text.\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Contains(got, []byte("author: someone")) {
		t.Errorf("TransformFile altered a non-targeted frontmatter field; got:\n%s", got)
	}
	if !bytes.Contains(got, []byte("mode: primary")) {
		t.Errorf("TransformFile did not rewrite the targeted field; got:\n%s", got)
	}
}

func TestTransformFile_BodyAfterFrontmatterIsUnchanged(t *testing.T) {
	body := "\n# Section\n\nmode: subagent is mentioned in the body.\n"
	input := []byte("---\nmode: subagent\n---\n" + body)
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Contains(got, []byte(body)) {
		t.Errorf("TransformFile changed content outside frontmatter; got:\n%s", got)
	}
}

func TestTransformFile_NoMatchingFieldValue_Unchanged(t *testing.T) {
	input := []byte("---\nmode: primary\ntitle: Already Primary\n---\n\nBody.\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Equal(got, input) {
		t.Errorf("TransformFile modified content when no field matched;\ngot:\n%s\nwant:\n%s", got, input)
	}
}

func TestTransformFile_NoFrontmatter_Unchanged(t *testing.T) {
	input := []byte("# No Frontmatter\n\nJust plain markdown.\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Equal(got, input) {
		t.Errorf("TransformFile modified content when there is no frontmatter; got:\n%s", got)
	}
}

func TestTransformFile_UnclosedFrontmatter_Unchanged(t *testing.T) {
	// Frontmatter block has no closing "---"; the block is never terminated,
	// so no transformation should be applied.
	input := []byte("---\nmode: subagent\ntitle: Missing Close\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Equal(got, input) {
		t.Errorf("TransformFile modified content with unclosed frontmatter; got:\n%s", got)
	}
}

func TestTransformFile_MultipleRules_AllApplied(t *testing.T) {
	rules := []snapshot.TransformRule{
		{Field: "mode", OldValue: "subagent", NewValue: "primary"},
		{Field: "role", OldValue: "worker", NewValue: "orchestrator"},
	}
	input := []byte("---\nmode: subagent\nrole: worker\n---\n\nBody.\n")
	got := snapshot.TransformFile(input, rules)

	if !bytes.Contains(got, []byte("mode: primary")) {
		t.Errorf("TransformFile did not apply mode rule; got:\n%s", got)
	}
	if !bytes.Contains(got, []byte("role: orchestrator")) {
		t.Errorf("TransformFile did not apply role rule; got:\n%s", got)
	}
}

func TestTransformFile_LeadingWhitespaceOnFieldLine_MatchedAndPreserved(t *testing.T) {
	// The field line has leading whitespace; the match should still apply,
	// and the leading whitespace should be preserved in the output.
	input := []byte("---\n  mode: subagent\ntitle: Indented\n---\n\nBody.\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	// The rewritten line should keep its leading whitespace.
	if !bytes.Contains(got, []byte("  mode: primary")) {
		t.Errorf("TransformFile did not match/preserve leading whitespace on field line; got:\n%s", got)
	}
}

func TestTransformFile_NoRules_ContentUnchanged(t *testing.T) {
	input := []byte("---\nmode: subagent\n---\n\nBody.\n")
	got := snapshot.TransformFile(input, nil)

	if !bytes.Equal(got, input) {
		t.Errorf("TransformFile with nil rules modified content; got:\n%s", got)
	}
}

func TestTransformFile_EmptyContent_ReturnsEmpty(t *testing.T) {
	got := snapshot.TransformFile([]byte{}, []snapshot.TransformRule{openCodeRule})
	if len(got) != 0 {
		t.Errorf("TransformFile with empty input should return empty, got %d bytes", len(got))
	}
}

// ---------------------------------------------------------------------------
// CRLF line-ending handling
// ---------------------------------------------------------------------------

// TestTransformFile_CRLF_RuleAppliesAndPreservesEnding verifies that a file
// whose lines end with \r\n is transformed correctly: the target field is
// rewritten and the replaced line keeps its \r\n ending.
func TestTransformFile_CRLF_RuleAppliesAndPreservesEnding(t *testing.T) {
	input := []byte("---\r\nmode: subagent\r\ntitle: My Agent\r\n---\r\n\r\n# Body\r\n")
	want := []byte("---\r\nmode: primary\r\ntitle: My Agent\r\n---\r\n\r\n# Body\r\n")

	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Equal(got, want) {
		t.Errorf("TransformFile CRLF output mismatch.\ngot:  %q\nwant: %q", got, want)
	}
}

// TestTransformFile_CRLF_NonTargetedLinesPreserveEnding verifies that lines
// not matched by any rule keep their \r\n ending intact after a transform.
func TestTransformFile_CRLF_NonTargetedLinesPreserveEnding(t *testing.T) {
	input := []byte("---\r\nmode: subagent\r\nauthor: someone\r\n---\r\n\r\nBody.\r\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	// The non-targeted line must retain its \r\n ending.
	if !bytes.Contains(got, []byte("author: someone\r\n")) {
		t.Errorf("TransformFile altered the line ending of a non-targeted field; got: %q", got)
	}
	if !bytes.Contains(got, []byte("mode: primary\r\n")) {
		t.Errorf("TransformFile did not rewrite the targeted field with preserved \\r\\n; got: %q", got)
	}
}

// TestTransformFile_LFvsCRLF_SameLogicalTransform verifies that a CRLF file
// and an otherwise-identical LF file produce the same logical transformation
// (same fields changed), and that each output preserves its own line endings.
func TestTransformFile_LFvsCRLF_SameLogicalTransform(t *testing.T) {
	lfInput := []byte("---\nmode: subagent\ntitle: Agent\n---\n\n# Body\n")
	crlfInput := []byte("---\r\nmode: subagent\r\ntitle: Agent\r\n---\r\n\r\n# Body\r\n")

	lfGot := snapshot.TransformFile(lfInput, []snapshot.TransformRule{openCodeRule})
	crlfGot := snapshot.TransformFile(crlfInput, []snapshot.TransformRule{openCodeRule})

	// LF output must not contain any \r.
	if bytes.Contains(lfGot, []byte("\r")) {
		t.Errorf("LF output unexpectedly contains \\r: %q", lfGot)
	}
	// CRLF output must still have \r\n on each line.
	if !bytes.Contains(crlfGot, []byte("mode: primary\r\n")) {
		t.Errorf("CRLF output missing expected \\r\\n on transformed line: %q", crlfGot)
	}
	// Both must have replaced the field.
	if !bytes.Contains(lfGot, []byte("mode: primary")) {
		t.Errorf("LF output did not replace the targeted field: %q", lfGot)
	}
	if !bytes.Contains(crlfGot, []byte("mode: primary")) {
		t.Errorf("CRLF output did not replace the targeted field: %q", crlfGot)
	}
}

// TestTransformFile_MixedLineEndings_EachPreservedIndependently verifies that
// within a single file some lines with \r\n and some with \n each retain their
// own ending after a transform.  The transformed line must keep its original
// ending (whichever it had), and untouched lines keep theirs.
func TestTransformFile_MixedLineEndings_EachPreservedIndependently(t *testing.T) {
	// Frontmatter delimiters use LF; frontmatter fields use CRLF; body uses LF.
	// "mode: subagent" line has \r\n; "title" line has plain \n.
	input := []byte("---\nmode: subagent\r\ntitle: Mixed\n---\n\n# Body\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	// Transformed line must keep \r\n.
	if !bytes.Contains(got, []byte("mode: primary\r\n")) {
		t.Errorf("mixed endings: transformed line lost \\r\\n ending; got: %q", got)
	}
	// Untouched LF line must keep \n (not gain \r).
	if bytes.Contains(got, []byte("title: Mixed\r\n")) {
		t.Errorf("mixed endings: LF line gained unexpected \\r; got: %q", got)
	}
	if !bytes.Contains(got, []byte("title: Mixed\n")) {
		t.Errorf("mixed endings: LF line lost its \\n ending; got: %q", got)
	}
}

// TestTransformFile_CRLF_NilRules_Unchanged verifies that TransformFile with
// nil rules leaves CRLF content byte-identical to the input.
func TestTransformFile_CRLF_NilRules_Unchanged(t *testing.T) {
	input := []byte("---\r\nmode: subagent\r\n---\r\n\r\nBody.\r\n")
	got := snapshot.TransformFile(input, nil)

	if !bytes.Equal(got, input) {
		t.Errorf("TransformFile with nil rules modified CRLF content; got: %q", got)
	}
}

// TestTransformFile_CRLF_EmptyRules_Unchanged verifies that TransformFile with
// an empty rule slice leaves CRLF content byte-identical to the input.
func TestTransformFile_CRLF_EmptyRules_Unchanged(t *testing.T) {
	input := []byte("---\r\nmode: subagent\r\n---\r\n\r\nBody.\r\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{})

	if !bytes.Equal(got, input) {
		t.Errorf("TransformFile with empty rules modified CRLF content; got: %q", got)
	}
}

// TestTransformFile_TrailingWhitespaceBeforeCR_MatchesAndPreservesCR verifies
// that trailing spaces or tabs before \r do not prevent rule matching, that
// the replacement line drops the trailing spaces (existing behavior), and that
// the \r\n ending is preserved.
//
// Input field line: "mode: subagent  \r\n" (two trailing spaces before \r\n)
// Expected output:  "mode: primary\r\n"    (trailing spaces dropped, \r preserved)
func TestTransformFile_TrailingWhitespaceBeforeCR_MatchesAndPreservesCR(t *testing.T) {
	input := []byte("---\r\nmode: subagent  \r\ntitle: Agent\r\n---\r\n\r\nBody.\r\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	// Must have replaced the field.
	if !bytes.Contains(got, []byte("mode: primary")) {
		t.Errorf("trailing whitespace before \\r prevented rule from matching; got: %q", got)
	}
	// Replaced line must end with \r\n (CR preserved).
	if !bytes.Contains(got, []byte("mode: primary\r\n")) {
		t.Errorf("replaced line lost \\r\\n ending; got: %q", got)
	}
	// Trailing spaces must not appear in the output for this line.
	if bytes.Contains(got, []byte("mode: primary  \r\n")) {
		t.Errorf("trailing spaces were unexpectedly preserved in output; got: %q", got)
	}
}

// TestTransformFile_TrailingTabBeforeCR_MatchesAndPreservesCR is a variant of
// the trailing-whitespace test using a tab character instead of spaces.
func TestTransformFile_TrailingTabBeforeCR_MatchesAndPreservesCR(t *testing.T) {
	input := []byte("---\r\nmode: subagent\t\r\ntitle: Agent\r\n---\r\n\r\nBody.\r\n")
	got := snapshot.TransformFile(input, []snapshot.TransformRule{openCodeRule})

	if !bytes.Contains(got, []byte("mode: primary\r\n")) {
		t.Errorf("trailing tab before \\r prevented rule from matching or lost \\r\\n; got: %q", got)
	}
}

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

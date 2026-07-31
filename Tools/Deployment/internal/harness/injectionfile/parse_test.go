package injectionfile_test

// Tests for ParseInjections: extraction of [[INJECTION:Name]] regions from
// MOSAIC-formatted Markdown files into a name-to-content map.
//
// Coverage:
//   - A well-formed Markdown file with multiple [[INJECTION:Name]] regions returns
//     a map with all injection names as keys and their trimmed content as values.
//   - A Markdown file with no injection regions returns a non-nil empty map with no error.
//   - An injection region with empty content between its boundary tags returns an entry
//     with an empty-string value, distinguishable from an absent key.
//   - Content fidelity: the returned content matches the inner text between the boundary
//     tags (excluding the tag lines themselves), with only leading and trailing whitespace
//     stripped; inner structure is preserved byte-for-byte.
//   - Malformed input (unclosed frontmatter block) causes docformat.Parse to return an
//     error, which ParseInjections propagates as a non-nil error.
//   - Injections nested inside [[SECTION:Name]] blocks are found by the depth-first
//     traversal in Body().Injections() and appear in the returned map.
//   - A file with valid YAML frontmatter followed by injection regions parses correctly;
//     the frontmatter content does not appear in the injection map values.
//   - The returned map size equals the number of distinct [[INJECTION:Name]] regions in
//     the source; no duplicate or phantom entries are added.
//   - Leading and trailing whitespace (newlines and spaces) surrounding the injection
//     content are stripped; non-whitespace characters inside the content are preserved.
//   - A file with a single injection region returns a map with exactly one entry.
//   - An empty byte slice input returns a non-nil empty map with no error.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/harness/injectionfile"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// multiInjectionFixture is a well-formed Markdown body with three top-level injection
// regions. Used by tests that verify correct name-to-content mapping.
const multiInjectionFixture = `# Harness Injections

[[INJECTION:HarnessConstraints]]
Use only approved tools.
Do not access external APIs without explicit permission.
[[/INJECTION:HarnessConstraints]]

[[INJECTION:CustomConstraints]]
Follow the project coding standards.
Submit work incrementally.
[[/INJECTION:CustomConstraints]]

[[INJECTION:LanguagePatterns]]
Prefer table-driven tests over individual test functions.
Use errors.Is for error comparison.
[[/INJECTION:LanguagePatterns]]
`

// noInjectionFixture is a well-formed Markdown body with no injection regions. Used by
// tests that verify an empty map is returned when no injection sections are present.
const noInjectionFixture = `# Harness Injections

This file intentionally contains no injection regions.

## Design Rationale

- **HarnessConstraints:** Not applicable for this harness.
- **LanguagePatterns:** Not applicable for this harness.
`

// emptyContentInjectionFixture has one injection region with no content between the tags.
// Used to verify that "declared but empty" injections produce a map entry with value "".
const emptyContentInjectionFixture = `# Harness Injections

[[INJECTION:HarnessConstraints]]
[[/INJECTION:HarnessConstraints]]
`

// contentFidelityFixture has an injection with distinctive multi-line content that
// includes special characters, blank lines, and indented code. Used to verify that
// ParseInjections preserves the inner text faithfully and strips only the surrounding
// boundary tag lines and leading/trailing whitespace.
const contentFidelityFixture = `# Harness Injections

[[INJECTION:LanguagePatterns]]
Use idiomatic Go:

  - Return errors explicitly; do not panic on recoverable conditions.
  - Prefer early returns to deep nesting.
  - Use table-driven tests with t.Run for parameterised cases.

Example:

    func foo(x int) (int, error) {
        if x < 0 {
            return 0, fmt.Errorf("negative input: %d", x)
        }
        return x * 2, nil
    }
[[/INJECTION:LanguagePatterns]]
`

// malformedFrontmatterFixture causes docformat.Parse to return an error because the
// opening "---" delimiter has no matching closing "---" delimiter.
const malformedFrontmatterFixture = `---
harness: test-harness
injections_version: "1.0.0"
`

// sectionWrappedInjectionFixture has an injection nested inside a [[SECTION:Name]] block.
// Used to verify that Body().Injections() traverses all nesting depths.
const sectionWrappedInjectionFixture = `[[SECTION:Identity]]
# Identity

[[INJECTION:IdentityExtension]]
This is orchestrator-only identity content.
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]
`

// withFrontmatterFixture is a complete file with valid YAML frontmatter followed by
// injection regions. Mirrors the actual HarnessInjections.md file format.
const withFrontmatterFixture = `---
version: "1.0.0"
harness: test-harness
---

# Harness Injections — Test Harness

[[INJECTION:HarnessConstraints]]
Do not modify system files.
[[/INJECTION:HarnessConstraints]]

[[INJECTION:CustomConstraints]]
Project-specific rules apply.
[[/INJECTION:CustomConstraints]]
`

// whitespaceTrimmingFixture has an injection whose content has leading blank lines,
// trailing blank lines, and internal structure. Used to verify that only the outer
// whitespace is stripped and the internal structure is preserved.
const whitespaceTrimmingFixture = `[[INJECTION:HarnessConstraints]]

First line of content.

Second line of content.

[[/INJECTION:HarnessConstraints]]
`

// singleInjectionFixture has exactly one injection region. Used to verify a map with
// exactly one entry is returned.
const singleInjectionFixture = `[[INJECTION:CustomConstraints]]
Single constraint.
[[/INJECTION:CustomConstraints]]
`

// ---------------------------------------------------------------------------
// Tests: well-formed multi-injection file
// ---------------------------------------------------------------------------

func TestParseInjections_MultipleInjections_AllNamesPresent(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(multiInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error parsing well-formed multi-injection fixture: %v", err)
	}

	wantNames := []string{"HarnessConstraints", "CustomConstraints", "LanguagePatterns"}
	for _, name := range wantNames {
		if _, ok := result[name]; !ok {
			t.Errorf("map is missing expected injection name %q", name)
		}
	}
}

func TestParseInjections_MultipleInjections_MapSizeMatchesCount(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(multiInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const wantCount = 3
	if len(result) != wantCount {
		t.Errorf("map size: want %d, got %d (entries: %v)", wantCount, len(result), result)
	}
}

func TestParseInjections_MultipleInjections_HarnessConstraintsContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(multiInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}

	if !strings.Contains(content, "Use only approved tools.") {
		t.Errorf("HarnessConstraints content missing expected text; got: %q", content)
	}
	if !strings.Contains(content, "Do not access external APIs without explicit permission.") {
		t.Errorf("HarnessConstraints content missing expected text; got: %q", content)
	}
}

func TestParseInjections_MultipleInjections_CustomConstraintsContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(multiInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["CustomConstraints"]
	if !ok {
		t.Fatal("map does not contain CustomConstraints")
	}

	if !strings.Contains(content, "Follow the project coding standards.") {
		t.Errorf("CustomConstraints content missing expected text; got: %q", content)
	}
}

func TestParseInjections_MultipleInjections_LanguagePatternsContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(multiInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["LanguagePatterns"]
	if !ok {
		t.Fatal("map does not contain LanguagePatterns")
	}

	if !strings.Contains(content, "Prefer table-driven tests over individual test functions.") {
		t.Errorf("LanguagePatterns content missing expected text; got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: no injection regions
// ---------------------------------------------------------------------------

func TestParseInjections_NoInjections_ReturnsNilError(t *testing.T) {
	_, err := injectionfile.ParseInjections([]byte(noInjectionFixture))

	if err != nil {
		t.Errorf("expected nil error for valid file with no injections, got: %v", err)
	}
}

func TestParseInjections_NoInjections_ReturnsNonNilEmptyMap(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(noInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for file with no injections, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map for file with no injections, got %d entries: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// Tests: empty content between boundary tags
// ---------------------------------------------------------------------------

func TestParseInjections_EmptyContentInjection_KeyPresentInMap(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(emptyContentInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["HarnessConstraints"]; !ok {
		t.Error("map does not contain HarnessConstraints; empty-content injections must still produce a map entry")
	}
}

func TestParseInjections_EmptyContentInjection_ValueIsEmptyString(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(emptyContentInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}
	if content != "" {
		t.Errorf("expected empty string for empty-content injection, got: %q", content)
	}
}

func TestParseInjections_EmptyContentInjection_MapHasExactlyOneEntry(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(emptyContentInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected exactly 1 entry for fixture with one empty-content injection, got %d: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// Tests: content fidelity
// ---------------------------------------------------------------------------

func TestParseInjections_ContentFidelity_InnerTextPreserved(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(contentFidelityFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["LanguagePatterns"]
	if !ok {
		t.Fatal("map does not contain LanguagePatterns")
	}

	// Inner prose and structure must be preserved.
	if !strings.Contains(content, "Return errors explicitly; do not panic on recoverable conditions.") {
		t.Errorf("inner text not preserved; content: %q", content)
	}
	if !strings.Contains(content, "Prefer early returns to deep nesting.") {
		t.Errorf("inner text not preserved; content: %q", content)
	}
	if !strings.Contains(content, "func foo(x int) (int, error) {") {
		t.Errorf("indented code block not preserved; content: %q", content)
	}
}

func TestParseInjections_ContentFidelity_BoundaryTagLinesExcluded(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(contentFidelityFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["LanguagePatterns"]
	if !ok {
		t.Fatal("map does not contain LanguagePatterns")
	}

	// The boundary tag lines must not appear in the content.
	if strings.Contains(content, "[[INJECTION:LanguagePatterns]]") {
		t.Errorf("content must not include the opening boundary tag line; content: %q", content)
	}
	if strings.Contains(content, "[[/INJECTION:LanguagePatterns]]") {
		t.Errorf("content must not include the closing boundary tag line; content: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: whitespace trimming
// ---------------------------------------------------------------------------

func TestParseInjections_WhitespaceTrimming_LeadingBlankLineStripped(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(whitespaceTrimmingFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}

	// Leading whitespace (the blank line after the opening tag) must be stripped.
	if strings.HasPrefix(content, "\n") {
		t.Errorf("content must not begin with a leading newline after trimming; got: %q", content)
	}
	if strings.HasPrefix(content, " ") {
		t.Errorf("content must not begin with a leading space after trimming; got: %q", content)
	}
}

func TestParseInjections_WhitespaceTrimming_TrailingBlankLineStripped(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(whitespaceTrimmingFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}

	// Trailing whitespace (the blank line before the closing tag) must be stripped.
	if strings.HasSuffix(content, "\n") {
		t.Errorf("content must not end with a trailing newline after trimming; got: %q", content)
	}
}

func TestParseInjections_WhitespaceTrimming_InternalStructurePreserved(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(whitespaceTrimmingFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}

	// The blank line between the two content lines must still be present.
	if !strings.Contains(content, "First line of content.\n\nSecond line of content.") {
		t.Errorf("internal blank line between content lines must be preserved; got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: malformed / unparseable input
// ---------------------------------------------------------------------------

func TestParseInjections_UnclosedFrontmatter_ReturnsError(t *testing.T) {
	_, err := injectionfile.ParseInjections([]byte(malformedFrontmatterFixture))

	if err == nil {
		t.Error("expected non-nil error for input with an unclosed frontmatter block, got nil")
	}
}

func TestParseInjections_UnclosedFrontmatter_ErrorMessageDescriptive(t *testing.T) {
	_, err := injectionfile.ParseInjections([]byte(malformedFrontmatterFixture))

	if err == nil {
		t.Fatal("expected non-nil error for input with an unclosed frontmatter block, got nil")
	}

	// The error should mention "frontmatter" so callers can diagnose the problem.
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error message should reference 'frontmatter'; got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: injections nested inside sections
// ---------------------------------------------------------------------------

func TestParseInjections_InjectionInSection_Found(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(sectionWrappedInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["IdentityExtension"]; !ok {
		t.Error("ParseInjections must find injections nested inside section blocks; IdentityExtension not found")
	}
}

func TestParseInjections_InjectionInSection_ContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(sectionWrappedInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["IdentityExtension"]
	if !ok {
		t.Fatal("IdentityExtension not found in map")
	}
	if !strings.Contains(content, "This is orchestrator-only identity content.") {
		t.Errorf("nested injection content incorrect; got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: file with valid YAML frontmatter
// ---------------------------------------------------------------------------

func TestParseInjections_WithFrontmatter_ReturnsNilError(t *testing.T) {
	_, err := injectionfile.ParseInjections([]byte(withFrontmatterFixture))

	if err != nil {
		t.Errorf("unexpected error for file with valid frontmatter and injections: %v", err)
	}
}

func TestParseInjections_WithFrontmatter_InjectionsExtracted(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(withFrontmatterFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["HarnessConstraints"]; !ok {
		t.Error("HarnessConstraints not found in map for file with frontmatter")
	}
	if _, ok := result["CustomConstraints"]; !ok {
		t.Error("CustomConstraints not found in map for file with frontmatter")
	}
}

func TestParseInjections_WithFrontmatter_FrontmatterNotInContentValues(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(withFrontmatterFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for name, content := range result {
		if strings.Contains(content, "version:") || strings.Contains(content, "harness:") {
			t.Errorf("injection %q content contains frontmatter text; got: %q", name, content)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: single injection
// ---------------------------------------------------------------------------

func TestParseInjections_SingleInjection_MapHasOneEntry(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(singleInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected exactly 1 entry for single-injection fixture, got %d: %v", len(result), result)
	}
}

func TestParseInjections_SingleInjection_ContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte(singleInjectionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["CustomConstraints"]
	if !ok {
		t.Fatal("map does not contain CustomConstraints")
	}
	if !strings.Contains(content, "Single constraint.") {
		t.Errorf("content missing expected text; got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: empty input
// ---------------------------------------------------------------------------

func TestParseInjections_EmptyInput_ReturnsNilError(t *testing.T) {
	_, err := injectionfile.ParseInjections([]byte{})

	if err != nil {
		t.Errorf("expected nil error for empty input, got: %v", err)
	}
}

func TestParseInjections_EmptyInput_ReturnsNonNilEmptyMap(t *testing.T) {
	result, err := injectionfile.ParseInjections([]byte{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for empty input, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map for empty input, got %d entries: %v", len(result), result)
	}
}

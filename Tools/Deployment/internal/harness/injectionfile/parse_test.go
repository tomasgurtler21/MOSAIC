package injectionfile_test

// Tests for ParseDeployedRegions: extraction of [[DEPLOYED:Name]] regions from
// MOSAIC-formatted Markdown harness content files into a name-to-content map.
//
// After the Stage 8 migration, harness content files (HarnessInjections.md,
// HarnessInjectionsOrchestrator.md) use [[DEPLOYED:Name]] regions for tool-managed
// content. ParseDeployedRegions is the canonical parser for these files.
//
// Comprehensive coverage lives in deployed_test.go. This file adds integration-style
// tests that verify the parser behaves correctly with the complete set of harness region
// names, whitespace handling, and error conditions expected from real harness content files.
// Functions that duplicate assertions already present in deployed_test.go were removed
// to prevent build errors from redeclared identifiers.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/harness/injectionfile"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// multiDeployedRegionFixture is a well-formed harness content file with three
// [[DEPLOYED:Name]] regions. Mirrors the format of a real HarnessInjections.md.
const multiDeployedRegionFixture = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Use only approved tools.
Do not access external APIs without explicit permission.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:CustomConstraints]]
Follow the project coding standards.
Submit work incrementally.
[[/DEPLOYED:CustomConstraints]]

[[DEPLOYED:LanguagePatterns]]
Prefer table-driven tests over individual test functions.
Use errors.Is for error comparison.
[[/DEPLOYED:LanguagePatterns]]
`

// emptyContentDeployedFixture has one [[DEPLOYED:]] region with no content between its tags.
// Used to verify that "declared but empty" produces a map entry with value "".
const emptyContentDeployedFixture = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`

// contentFidelityDeployedFixture has a region with distinctive multi-line content that
// includes special characters, blank lines, and indented code. Used to verify that
// ParseDeployedRegions preserves the inner text faithfully and strips only the surrounding
// boundary tag lines and leading/trailing whitespace.
const contentFidelityDeployedFixture = `# Harness Injections

[[DEPLOYED:LanguagePatterns]]
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
[[/DEPLOYED:LanguagePatterns]]
`

// malformedFrontmatterDeployedFixture causes docformat.Parse to return an error because
// the opening "---" delimiter has no matching closing "---" delimiter.
const malformedFrontmatterDeployedFixture = `---
harness: test-harness
injections_version: "1.0.0"
`

// sectionWrappedDeployedFixture has a [[DEPLOYED:]] region nested inside a [[SECTION:]]
// block. Used to verify that ParseDeployedRegions traverses all nesting depths.
const sectionWrappedDeployedFixture = `[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:HarnessConstraints]]
This is harness-level constraint content.
[[/DEPLOYED:HarnessConstraints]]

[[/SECTION:Constraints]]
`

// withFrontmatterDeployedFixture is a complete file with valid YAML frontmatter followed
// by [[DEPLOYED:]] regions. Mirrors the actual HarnessInjections.md file format.
const withFrontmatterDeployedFixture = `---
version: "1.0.0"
harness: test-harness
---

# Harness Injections — Test Harness

[[DEPLOYED:HarnessConstraints]]
Do not modify system files.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:CustomConstraints]]
Project-specific rules apply.
[[/DEPLOYED:CustomConstraints]]
`

// whitespaceTrimmingDeployedFixture has a region whose content has leading blank lines,
// trailing blank lines, and internal structure. Used to verify that only the outer
// whitespace is stripped and the internal structure is preserved.
const whitespaceTrimmingDeployedFixture = `[[DEPLOYED:HarnessConstraints]]

First line of content.

Second line of content.

[[/DEPLOYED:HarnessConstraints]]
`

// singleDeployedRegionFixture has exactly one [[DEPLOYED:]] region.
const singleDeployedRegionFixture = `[[DEPLOYED:CustomConstraints]]
Single constraint.
[[/DEPLOYED:CustomConstraints]]
`

// ---------------------------------------------------------------------------
// Tests: well-formed multi-region file
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_MultipleRegions_HarnessConstraintsContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(multiDeployedRegionFixture))

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

func TestParseDeployedRegions_MultipleRegions_CustomConstraintsContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(multiDeployedRegionFixture))

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

func TestParseDeployedRegions_MultipleRegions_LanguagePatternsContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(multiDeployedRegionFixture))

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
// Tests: empty content between boundary tags
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_EmptyContentRegion_MapHasExactlyOneEntry(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(emptyContentDeployedFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected exactly 1 entry for fixture with one empty-content region, got %d: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// Tests: content fidelity
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_ContentFidelity_InnerTextPreserved(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(contentFidelityDeployedFixture))

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

func TestParseDeployedRegions_ContentFidelity_BoundaryTagLinesExcluded(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(contentFidelityDeployedFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["LanguagePatterns"]
	if !ok {
		t.Fatal("map does not contain LanguagePatterns")
	}

	// The boundary tag lines must not appear in the content.
	if strings.Contains(content, "[[DEPLOYED:LanguagePatterns]]") {
		t.Errorf("content must not include the opening boundary tag line; content: %q", content)
	}
	if strings.Contains(content, "[[/DEPLOYED:LanguagePatterns]]") {
		t.Errorf("content must not include the closing boundary tag line; content: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: whitespace trimming
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_WhitespaceTrimming_LeadingBlankLineStripped(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(whitespaceTrimmingDeployedFixture))

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

func TestParseDeployedRegions_WhitespaceTrimming_TrailingBlankLineStripped(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(whitespaceTrimmingDeployedFixture))

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

func TestParseDeployedRegions_WhitespaceTrimming_InternalStructurePreserved(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(whitespaceTrimmingDeployedFixture))

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

func TestParseDeployedRegions_UnclosedFrontmatter_ReturnsError(t *testing.T) {
	_, err := injectionfile.ParseDeployedRegions([]byte(malformedFrontmatterDeployedFixture))

	if err == nil {
		t.Error("expected non-nil error for input with an unclosed frontmatter block, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: regions nested inside sections
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_RegionInSection_Found(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(sectionWrappedDeployedFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["HarnessConstraints"]; !ok {
		t.Error("ParseDeployedRegions must find regions nested inside section blocks; HarnessConstraints not found")
	}
}

func TestParseDeployedRegions_RegionInSection_ContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(sectionWrappedDeployedFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("HarnessConstraints not found in map")
	}
	if !strings.Contains(content, "This is harness-level constraint content.") {
		t.Errorf("nested region content incorrect; got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: file with valid YAML frontmatter
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_WithFrontmatter_ReturnsNilError(t *testing.T) {
	_, err := injectionfile.ParseDeployedRegions([]byte(withFrontmatterDeployedFixture))

	if err != nil {
		t.Errorf("unexpected error for file with valid frontmatter and regions: %v", err)
	}
}

func TestParseDeployedRegions_WithFrontmatter_RegionsExtracted(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(withFrontmatterDeployedFixture))

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

func TestParseDeployedRegions_WithFrontmatter_FrontmatterNotInContentValues(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(withFrontmatterDeployedFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for name, content := range result {
		if strings.Contains(content, "version:") || strings.Contains(content, "harness:") {
			t.Errorf("region %q content contains frontmatter text; got: %q", name, content)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: single region
// ---------------------------------------------------------------------------

func TestParseDeployedRegions_SingleRegion_MapHasOneEntry(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(singleDeployedRegionFixture))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected exactly 1 entry for single-region fixture, got %d: %v", len(result), result)
	}
}

func TestParseDeployedRegions_SingleRegion_ContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(singleDeployedRegionFixture))

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

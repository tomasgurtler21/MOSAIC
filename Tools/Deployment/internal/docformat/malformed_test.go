package docformat_test

// Tests for malformed frontmatter error reporting (T3.5).
//
// Coverage:
//   - SplitFrontmatter returns an error for an unclosed frontmatter block.
//   - Parse returns an error for invalid YAML inside a frontmatter block.
//   - Parse returns an error for YAML that uses tab indentation (disallowed by the spec).
//   - Errors are non-nil and contain a non-empty, human-readable description.
//   - Errors from Parse include positional context (line or construct information).
//   - Parse never produces a non-nil Document alongside a non-nil error.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/docformat"
)

// --- Unclosed frontmatter ---

func TestSplitFrontmatter_UnclosedBlock_FromFixture_ReturnsError(t *testing.T) {
	src := fixtureBytes(t, "malformed-unclosed.md")

	_, _, err := docformat.SplitFrontmatter(src)

	if err == nil {
		t.Fatal("expected error for unclosed frontmatter block, got nil")
	}
}

func TestParse_UnclosedFrontmatter_ReturnsError(t *testing.T) {
	src := fixtureBytes(t, "malformed-unclosed.md")

	doc, err := docformat.Parse(src)

	if err == nil {
		t.Fatal("expected error for unclosed frontmatter, got nil")
	}
	if doc != nil {
		t.Error("Parse must return a nil Document when it returns an error")
	}
}

func TestParse_UnclosedFrontmatter_ErrorIsInformative(t *testing.T) {
	src := fixtureBytes(t, "malformed-unclosed.md")

	_, err := docformat.Parse(src)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if strings.TrimSpace(msg) == "" {
		t.Error("error message must not be empty or whitespace-only")
	}
}

// --- Invalid YAML content ---

func TestParse_InvalidYAML_UnclosedBracket_ReturnsError(t *testing.T) {
	src := fixtureBytes(t, "malformed-invalid-yaml.md")

	doc, err := docformat.Parse(src)

	if err == nil {
		t.Fatal("expected error for invalid YAML (unclosed bracket), got nil")
	}
	if doc != nil {
		t.Error("Parse must return a nil Document when it returns an error")
	}
}

func TestParse_InvalidYAML_UnclosedBracket_ErrorDescribesConstruct(t *testing.T) {
	// The error must name the offending construct or include its position (line number).
	// AC3.5 requires that malformed frontmatter yields an error naming the offending construct.
	// goccy/go-yaml includes line information in its parse errors; the test asserts that
	// something concrete is present (a digit indicating a line number, or a recognised YAML
	// token name), rather than just checking that the error is non-empty.
	src := fixtureBytes(t, "malformed-invalid-yaml.md")

	_, err := docformat.Parse(src)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if strings.TrimSpace(msg) == "" {
		t.Error("error message must not be empty")
	}
	// The error must contain at least one digit (a line number) or a structural YAML term.
	// A plain "error" or "parse error" string carries no actionable information for a caller
	// trying to locate and fix the problem.
	containsPositionalInfo := false
	for _, ch := range msg {
		if ch >= '0' && ch <= '9' {
			containsPositionalInfo = true
			break
		}
	}
	if !containsPositionalInfo {
		// Fallback: accept structural YAML keywords that also identify the construct.
		structuralTerms := []string{"line", "column", "token", "bracket", "flow", "scalar", "mapping"}
		for _, term := range structuralTerms {
			if strings.Contains(strings.ToLower(msg), term) {
				containsPositionalInfo = true
				break
			}
		}
	}
	if !containsPositionalInfo {
		t.Errorf("error message lacks positional or structural context (no line number, no YAML term): %q", msg)
	}
}

// --- Tab-indented YAML ---

func TestParse_TabIndentedYAML_ReturnsError(t *testing.T) {
	// YAML disallows tab characters for indentation. An agent file with tab-indented
	// frontmatter should produce a clear error rather than a silent misparse.
	src := fixtureBytes(t, "malformed-tab-indented.md")

	doc, err := docformat.Parse(src)

	if err == nil {
		t.Fatal("expected error for tab-indented YAML, got nil")
	}
	if doc != nil {
		t.Error("Parse must return a nil Document when it returns an error")
	}
}

// --- Inline malformed inputs ---

func TestParse_DuplicateKey_ReturnsError(t *testing.T) {
	// YAML technically allows duplicate keys (last-wins), but for MOSAIC frontmatter a
	// duplicate key is a data error that should be caught.
	src := []byte("---\nname: first\nname: second\n---\n")

	_, err := docformat.Parse(src)

	if err == nil {
		t.Error("expected error for duplicate frontmatter key, got nil")
	}
}

func TestParse_ValidDocument_DoesNotReturnError(t *testing.T) {
	// Sanity check: a well-formed document must not trigger malformed-input error paths.
	src := []byte("---\nname: valid-agent\nversion: 1.0.0\n---\n\nBody.\n")

	doc, err := docformat.Parse(src)

	if err != nil {
		t.Errorf("unexpected error for a well-formed document: %v", err)
	}
	if doc == nil {
		t.Error("expected non-nil Document for a well-formed input")
	}
}

func TestParse_NilDocumentOnError_NeverWithNonNilDoc(t *testing.T) {
	// Exhaustive invariant: for every malformed input, (doc, err) satisfies
	// either (non-nil, nil) or (nil, non-nil) — never (non-nil, non-nil).
	malformedInputs := []struct {
		name string
		src  []byte
	}{
		{
			name: "unclosed frontmatter",
			src:  []byte("---\nname: test\n"),
		},
		{
			name: "invalid yaml - unclosed bracket",
			src:  []byte("---\ntools: [a, b\n---\n"),
		},
	}

	for _, tc := range malformedInputs {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := docformat.Parse(tc.src)
			if doc != nil && err != nil {
				t.Errorf("Parse returned both a non-nil Document and a non-nil error: doc=%v, err=%v", doc, err)
			}
		})
	}
}

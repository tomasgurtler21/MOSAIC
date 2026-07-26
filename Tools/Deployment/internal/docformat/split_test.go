package docformat_test

// Tests for the frontmatter/body splitter (SplitFrontmatter).
//
// Coverage:
//   - Files with no frontmatter delimiter return nil frontmatter and the full source as body.
//   - Files with an empty frontmatter block return an empty (non-nil) frontmatter slice.
//   - LF and CRLF line endings are both handled and preserved exactly.
//   - A "---" that appears in the body after the closing delimiter is not consumed.
//   - A file whose opening "---" has no closing delimiter produces an error.
//   - A file with only a frontmatter block and no body is handled.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-deploy/internal/docformat"
)

func TestSplitFrontmatter_NoFrontmatter_ReturnsNilFrontmatterAndFullBody(t *testing.T) {
	input := []byte("# Just a Markdown File\n\nNo frontmatter here.\n")

	fm, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter for a file without delimiters, got %q", fm)
	}
	if !bytes.Equal(body, input) {
		t.Errorf("body should equal the full input when there is no frontmatter\ngot:  %q\nwant: %q", body, input)
	}
}

func TestSplitFrontmatter_FileStartsWithHorizontalRule_NotTreatedAsFrontmatter(t *testing.T) {
	// A Markdown horizontal rule that is not at the very first byte of a "---\n" opening line
	// must not trigger frontmatter detection. This tests the edge case where the file does NOT
	// begin with "---" on line 1.
	input := []byte("# Heading\n\n---\n\nSome text.\n")

	fm, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != nil {
		t.Errorf("mid-file horizontal rule must not be detected as frontmatter, got %q", fm)
	}
	if !bytes.Equal(body, input) {
		t.Errorf("body should equal the full input\ngot:  %q\nwant: %q", body, input)
	}
}

func TestSplitFrontmatter_EmptyFrontmatterBlock_ReturnsEmptyNonNilSlice(t *testing.T) {
	bodyContent := "\nBody text.\n"
	input := []byte("---\n---\n" + bodyContent)

	fm, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter slice for an empty frontmatter block")
	}
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter bytes, got %q", fm)
	}
	if string(body) != bodyContent {
		t.Errorf("body mismatch\ngot:  %q\nwant: %q", body, bodyContent)
	}
}

func TestSplitFrontmatter_NormalContent_LF_SplitsCorrectly(t *testing.T) {
	frontmatterContent := "name: test\nversion: 1.0.0\n"
	bodyContent := "\nBody content.\n"
	input := []byte("---\n" + frontmatterContent + "---\n" + bodyContent)

	fm, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(fm) != frontmatterContent {
		t.Errorf("frontmatter mismatch\ngot:  %q\nwant: %q", fm, frontmatterContent)
	}
	if string(body) != bodyContent {
		t.Errorf("body mismatch\ngot:  %q\nwant: %q", body, bodyContent)
	}
}

func TestSplitFrontmatter_NormalContent_CRLF_PreservesLineEndings(t *testing.T) {
	// CRLF line endings must be treated as content and never normalised to LF.
	frontmatterContent := "name: test\r\nversion: 1.0.0\r\n"
	bodyContent := "\r\nBody content.\r\n"
	input := []byte("---\r\n" + frontmatterContent + "---\r\n" + bodyContent)

	fm, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(fm) != frontmatterContent {
		t.Errorf("CRLF frontmatter not preserved exactly\ngot:  %q\nwant: %q", fm, frontmatterContent)
	}
	if string(body) != bodyContent {
		t.Errorf("CRLF body not preserved exactly\ngot:  %q\nwant: %q", body, bodyContent)
	}
}

func TestSplitFrontmatter_DashesInBody_AreNotConsumedAsDelimiter(t *testing.T) {
	// A "---" line that appears in the body after the closing delimiter must pass through
	// unchanged. Only the first "---" after the opening one closes the frontmatter.
	bodyContent := "---\nThis is body content with a horizontal rule.\n---\n"
	input := []byte("---\nname: test\n---\n" + bodyContent)

	_, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != bodyContent {
		t.Errorf("body dashes were wrongly consumed\ngot:  %q\nwant: %q", body, bodyContent)
	}
}

func TestSplitFrontmatter_OnlyFrontmatterNoBody_ReturnsEmptyBody(t *testing.T) {
	frontmatterContent := "name: test\n"
	input := []byte("---\n" + frontmatterContent + "---\n")

	fm, body, err := docformat.SplitFrontmatter(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(fm) != frontmatterContent {
		t.Errorf("frontmatter mismatch\ngot:  %q\nwant: %q", fm, frontmatterContent)
	}
	if len(body) != 0 {
		t.Errorf("expected empty body when there is no content after the closing delimiter, got %q", body)
	}
}

func TestSplitFrontmatter_UnclosedFrontmatter_ReturnsError(t *testing.T) {
	// A file that starts with "---" but never reaches a second "---" on its own line is
	// malformed. Silent treatment as "no frontmatter" would hide real errors.
	input := []byte("---\nname: test\nversion: 1.0.0\n\nNo closing delimiter here.\n")

	_, _, err := docformat.SplitFrontmatter(input)

	if err == nil {
		t.Fatal("expected error for unclosed frontmatter, got nil")
	}
}

func TestSplitFrontmatter_UnclosedFrontmatter_ErrorDescribesTheProblem(t *testing.T) {
	input := []byte("---\nname: test\n")

	_, _, err := docformat.SplitFrontmatter(input)

	if err == nil {
		t.Fatal("expected error for unclosed frontmatter, got nil")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("error message must not be empty")
	}
	// The error must describe the missing closing delimiter, not just be a generic word.
	// A caller reading this error needs to understand what is wrong with the file.
	describesProblem := strings.Contains(msg, "unclosed") ||
		strings.Contains(msg, "closing") ||
		strings.Contains(msg, "delimiter") ||
		strings.Contains(msg, "frontmatter") ||
		strings.Contains(msg, "---")
	if !describesProblem {
		t.Errorf("error does not describe the unclosed-frontmatter problem clearly: %q", msg)
	}
}

func TestSplitFrontmatter_EmptyInput_ReturnsNilFrontmatterAndEmptyBody(t *testing.T) {
	fm, body, err := docformat.SplitFrontmatter([]byte{})

	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter for empty input, got %q", fm)
	}
	if len(body) != 0 {
		t.Errorf("expected empty body for empty input, got %q", body)
	}
}

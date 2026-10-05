package docformat_test

import (
	"errors"
	"testing"

	"mosaic-common/docformat"
)

const bomPrefix = "\xEF\xBB\xBF"

// requireFormatError asserts err is (or wraps) a *docformat.FormatError and returns it.
func requireFormatError(t *testing.T, err error) *docformat.FormatError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a format error, got nil")
	}
	var fe *docformat.FormatError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *docformat.FormatError via errors.As, got %T: %v", err, err)
	}
	return fe
}

// requireASCII fails when s contains a non-ASCII byte.
func requireASCII(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			t.Fatalf("message must be ASCII, found byte 0x%02X at %d: %q", s[i], i, s)
		}
	}
}

// requireFormatProblem asserts that both SplitFrontmatter and Parse reject src with the
// given problem and line, and returns the error from SplitFrontmatter.
func requireFormatProblem(t *testing.T, src string, problem docformat.FormatProblem, line int) *docformat.FormatError {
	t.Helper()
	_, _, splitErr := docformat.SplitFrontmatter([]byte(src))
	fe := requireFormatError(t, splitErr)
	if fe.Problem != problem {
		t.Errorf("SplitFrontmatter problem = %q, want %q", fe.Problem, problem)
	}
	if fe.Line != line {
		t.Errorf("SplitFrontmatter line = %d, want %d", fe.Line, line)
	}
	requireASCII(t, fe.Error())

	doc, parseErr := docformat.Parse([]byte(src))
	pe := requireFormatError(t, parseErr)
	if doc != nil {
		t.Error("Parse must not return a Document alongside an error")
	}
	if pe.Problem != problem || pe.Line != line {
		t.Errorf("Parse error = {%q line %d}, want {%q line %d}", pe.Problem, pe.Line, problem, line)
	}
	return fe
}

package domain_test

// Tests for the Execution Log error marker helpers: formatting a BLOCKED row's
// error code as a trailing Summary marker and reading it back.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestFormatErrorMarker_CodeIsWrappedInErrorMarker(t *testing.T) {
	if got := domain.FormatErrorMarker(domain.ErrorTOOL_UNAVAILABLE); got != "[error:E501]" {
		t.Errorf("FormatErrorMarker(E501): want %q, got %q", "[error:E501]", got)
	}
}

func TestFormatErrorMarker_NoCodeYieldsEmptyString(t *testing.T) {
	if got := domain.FormatErrorMarker(domain.ErrorNone); got != "" {
		t.Errorf("FormatErrorMarker(none): want empty, got %q", got)
	}
}

func TestAppendErrorMarker(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		code    domain.ErrorCode
		want    string
	}{
		{"code appended after one space", "harness failed", domain.ErrorTOOL_UNAVAILABLE, "harness failed [error:E501]"},
		{"empty summary yields marker alone", "", domain.ErrorPERMISSION_DENIED, "[error:E502]"},
		{"no code leaves summary unchanged", "harness failed", domain.ErrorNone, "harness failed"},
		{"no code and empty summary stay empty", "", domain.ErrorNone, ""},
		{"existing bracket markers are kept before the error marker", "done [commit:abc]", domain.ErrorTOOL_UNAVAILABLE, "done [commit:abc] [error:E501]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.AppendErrorMarker(tc.summary, tc.code); got != tc.want {
				t.Errorf("AppendErrorMarker(%q, %q): want %q, got %q", tc.summary, tc.code, tc.want, got)
			}
		})
	}
}

func TestSplitErrorMarker(t *testing.T) {
	tests := []struct {
		name        string
		cell        string
		wantSummary string
		wantCode    domain.ErrorCode
	}{
		{"marker after text", "harness failed [error:E501]", "harness failed", domain.ErrorTOOL_UNAVAILABLE},
		{"marker alone", "[error:E501]", "", domain.ErrorTOOL_UNAVAILABLE},
		{"trailing whitespace after marker is ignored", "harness failed [error:E501]  ", "harness failed", domain.ErrorTOOL_UNAVAILABLE},
		{"no marker yields no code", "harness failed", "harness failed", domain.ErrorNone},
		{"empty cell", "", "", domain.ErrorNone},
		{"other bracket marker is kept and code still found", "done [commit:abc] [error:E501]", "done [commit:abc]", domain.ErrorTOOL_UNAVAILABLE},
		{"other bracket marker last is not consumed", "done [error:E501] [commit:abc]", "done [error:E501] [commit:abc]", domain.ErrorNone},
		{"marker not at the end is not recognised", "[error:E501] and more text", "[error:E501] and more text", domain.ErrorNone},
		{"only the final marker is consumed", "saw [error:E100] earlier [error:E501]", "saw [error:E100] earlier", domain.ErrorTOOL_UNAVAILABLE},
		{"unknown but well-formed code is preserved as written", "odd [error:X9z]", "odd", domain.ErrorCode("X9z")},
		{"empty code is malformed", "odd [error:]", "odd [error:]", domain.ErrorNone},
		{"code with a space is malformed", "odd [error:E5 01]", "odd [error:E5 01]", domain.ErrorNone},
		{"code with punctuation is malformed", "odd [error:E-1]", "odd [error:E-1]", domain.ErrorNone},
		{"missing closing bracket is malformed", "odd [error:E501", "odd [error:E501", domain.ErrorNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSummary, gotCode := domain.SplitErrorMarker(tc.cell)
			if gotSummary != tc.wantSummary || gotCode != tc.wantCode {
				t.Errorf("SplitErrorMarker(%q): want (%q, %q), got (%q, %q)",
					tc.cell, tc.wantSummary, tc.wantCode, gotSummary, gotCode)
			}
		})
	}
}

func TestSplitErrorMarker_InvertsAppendErrorMarker(t *testing.T) {
	summaries := []string{"", "harness failed", "done [commit:abc]", "[branch:x] [commit:y]", "ends with brackets [x]"}
	codes := []domain.ErrorCode{
		domain.ErrorINVALID_INVOCATION, domain.ErrorINPUT_NOT_FOUND, domain.ErrorDEPENDENCY_MISSING,
		domain.ErrorTOOL_UNAVAILABLE, domain.ErrorPERMISSION_DENIED, domain.ErrorUSER_CONTACT,
	}
	for _, summary := range summaries {
		for _, code := range codes {
			cell := domain.AppendErrorMarker(summary, code)
			gotSummary, gotCode := domain.SplitErrorMarker(cell)
			if gotSummary != summary || gotCode != code {
				t.Errorf("Split(Append(%q, %q)) = (%q, %q), want the inputs back", summary, code, gotSummary, gotCode)
			}
		}
	}
}

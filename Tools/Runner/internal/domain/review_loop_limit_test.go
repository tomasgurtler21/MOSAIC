package domain_test

// Tests for ParseReviewLoopLimit: the single parser behind the CLI flag and the
// TUI prompt. A positive integer is a limit; "none" or "no limit" means no
// limit (0); everything else is a refusal listing the accepted forms.

import (
	"errors"
	"testing"

	"mosaic-run/internal/domain"
)

func TestParseReviewLoopLimit_AcceptedForms(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1", 1},
		{"3", 3},
		{"25", 25},
		{" 4 ", 4},
		{"none", 0},
		{"None", 0},
		{"NONE", 0},
		{"no limit", 0},
		{"No Limit", 0},
		{"  no limit  ", 0},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := domain.ParseReviewLoopLimit(tc.input)

			if err != nil {
				t.Fatalf("ParseReviewLoopLimit(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseReviewLoopLimit(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseReviewLoopLimit_RejectedForms(t *testing.T) {
	for _, input := range []string{"", "   ", "0", "-1", "-5", "abc", "3.5", "1e3", "unlimited", "no", "3 attempts", "+", "999999999999999999999999"} {
		t.Run(input, func(t *testing.T) {
			_, err := domain.ParseReviewLoopLimit(input)

			var refusal *domain.RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("ParseReviewLoopLimit(%q) error = %v, want *domain.RefusalError", input, err)
			}
		})
	}
}

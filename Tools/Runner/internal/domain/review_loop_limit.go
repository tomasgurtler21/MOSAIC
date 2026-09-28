package domain

import (
	"strconv"
	"strings"
)

// ParseReviewLoopLimit maps user input to a review loop limit. A decimal
// positive integer yields that value; "none" or "no limit" (case-insensitive,
// trimmed) yields 0 (no limit). Anything else, including "", "0" and
// negatives, yields a *RefusalError listing the accepted forms.
func ParseReviewLoopLimit(s string) (int, error) {
	trimmed := strings.TrimSpace(s)
	switch strings.ToLower(trimmed) {
	case "none", "no limit":
		return 0, nil
	}
	if n, err := strconv.Atoi(trimmed); err == nil && n > 0 && !strings.HasPrefix(trimmed, "+") {
		return n, nil
	}
	return 0, &RefusalError{
		Component: "run configuration",
		Resource:  "review loop limit",
		Reason:    "invalid review loop limit " + strconv.Quote(s) + "; accepted forms: a positive integer, \"none\" or \"no limit\"",
	}
}

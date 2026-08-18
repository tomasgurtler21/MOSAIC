package transform_test

// enclosing_section_changed_detail_test.go covers the EnclosingSectionChangedDetail
// pure formatter.
//
// The formatter renders the Detail text for a GapEnclosingSectionChanged gap. It takes the
// section name, a pre-sorted list of custom region names, and an optional timestamp.
//
// Key invariants under test:
//
//   - The rendered text names the section whose own content changed.
//   - The rendered text names all enclosed custom region names in the order supplied.
//   - When a timestamp is supplied, the "(detected ...)" clause is present.
//   - When the timestamp is empty, the "(detected ...)" clause is omitted.
//   - The output is non-empty for any valid input.
//   - The output does not end with a trailing space when no timestamp is supplied.
//   - Two calls with different timestamps produce different output.
//   - The output communicates a review action to the user.
//
// These tests fail in the TDD RED phase (EnclosingSectionChangedDetail panics) and
// become regression guards once the Stage 2 implementation (I2.3) provides the body.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/transform"
)

// TestEnclosingSectionChangedDetail_WithTimestamp_ContainsSectionName verifies that the
// formatter includes the section name in the rendered text when a timestamp is supplied.
func TestEnclosingSectionChangedDetail_WithTimestamp_ContainsSectionName(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail(
		"Identity",
		[]string{"IdentityNote"},
		"2026-08-17T22:41:49Z",
	)

	if !strings.Contains(detail, "Identity") {
		t.Errorf("detail does not name the changed section %q: %s", "Identity", detail)
	}
}

// TestEnclosingSectionChangedDetail_WithTimestamp_ContainsCustomName verifies that the
// formatter includes the custom region name in the rendered text.
func TestEnclosingSectionChangedDetail_WithTimestamp_ContainsCustomName(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail(
		"Identity",
		[]string{"IdentityNote"},
		"2026-08-17T22:41:49Z",
	)

	if !strings.Contains(detail, "IdentityNote") {
		t.Errorf("detail does not name the custom region %q: %s", "IdentityNote", detail)
	}
}

// TestEnclosingSectionChangedDetail_WithTimestamp_ContainsTimestamp verifies that when
// a timestamp is supplied, it appears in the rendered text.
func TestEnclosingSectionChangedDetail_WithTimestamp_ContainsTimestamp(t *testing.T) {
	const ts = "2026-08-17T22:41:49Z"
	detail := transform.EnclosingSectionChangedDetail(
		"Identity",
		[]string{"IdentityNote"},
		ts,
	)

	if !strings.Contains(detail, ts) {
		t.Errorf("detail does not contain the caller-supplied timestamp %q: %s", ts, detail)
	}
}

// TestEnclosingSectionChangedDetail_EmptyTimestamp_OmitsClause verifies that when the
// timestamp is empty, the "(detected ...)" clause is absent. The section name and custom
// region names must still be present.
func TestEnclosingSectionChangedDetail_EmptyTimestamp_OmitsClause(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail(
		"Identity",
		[]string{"IdentityNote"},
		"",
	)

	if !strings.Contains(detail, "Identity") {
		t.Errorf("detail does not name the section with empty timestamp: %s", detail)
	}
	if strings.Contains(detail, "(detected") {
		t.Errorf("detail contains a timestamp clause when timestamp is empty; the clause must be omitted: %s", detail)
	}
}

// TestEnclosingSectionChangedDetail_MultipleCustomNames_AllPresent verifies that the
// formatter includes all custom region names in the rendered text.
func TestEnclosingSectionChangedDetail_MultipleCustomNames_AllPresent(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail(
		"Identity",
		[]string{"AlphaExt", "BetaExt", "ZetaExt"},
		"",
	)

	if !strings.Contains(detail, "AlphaExt") {
		t.Errorf("detail does not contain %q: %s", "AlphaExt", detail)
	}
	if !strings.Contains(detail, "BetaExt") {
		t.Errorf("detail does not contain %q: %s", "BetaExt", detail)
	}
	if !strings.Contains(detail, "ZetaExt") {
		t.Errorf("detail does not contain %q: %s", "ZetaExt", detail)
	}
}

// TestEnclosingSectionChangedDetail_MultipleCustomNames_RelativeOrderPreserved verifies
// that the formatter renders custom names in the order supplied. Names must be pre-sorted
// by the caller; the formatter renders them as given and must not silently re-sort or drop.
func TestEnclosingSectionChangedDetail_MultipleCustomNames_RelativeOrderPreserved(t *testing.T) {
	names := []string{"AlphaExt", "BetaExt", "ZetaExt"}
	detail := transform.EnclosingSectionChangedDetail("Identity", names, "")

	alphaIdx := strings.Index(detail, "AlphaExt")
	betaIdx := strings.Index(detail, "BetaExt")
	zetaIdx := strings.Index(detail, "ZetaExt")

	if alphaIdx < 0 {
		t.Errorf("detail does not contain %q: %s", "AlphaExt", detail)
	}
	if betaIdx < 0 {
		t.Errorf("detail does not contain %q: %s", "BetaExt", detail)
	}
	if zetaIdx < 0 {
		t.Errorf("detail does not contain %q: %s", "ZetaExt", detail)
	}
	if alphaIdx >= 0 && betaIdx >= 0 && alphaIdx > betaIdx {
		t.Errorf("AlphaExt at index %d appears after BetaExt at %d; must appear in the supplied order",
			alphaIdx, betaIdx)
	}
	if betaIdx >= 0 && zetaIdx >= 0 && betaIdx > zetaIdx {
		t.Errorf("BetaExt at index %d appears after ZetaExt at %d; must appear in the supplied order",
			betaIdx, zetaIdx)
	}
}

// TestEnclosingSectionChangedDetail_NonEmpty verifies that the formatter returns a
// non-empty string for valid inputs.
func TestEnclosingSectionChangedDetail_NonEmpty(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail("Identity", []string{"IdentityNote"}, "")
	if len(detail) == 0 {
		t.Error("EnclosingSectionChangedDetail returned empty string for valid inputs; must return non-empty detail text")
	}
}

// TestEnclosingSectionChangedDetail_EmptyTimestamp_NoTrailingSpace verifies that when
// the timestamp is empty, the detail text does not end with a trailing space or an
// incomplete clause.
func TestEnclosingSectionChangedDetail_EmptyTimestamp_NoTrailingSpace(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail("Identity", []string{"IdentityNote"}, "")
	if len(detail) == 0 {
		t.Fatal("EnclosingSectionChangedDetail returned empty string")
	}
	last := detail[len(detail)-1]
	if last == ' ' {
		t.Errorf("detail ends with a trailing space when timestamp is empty; detail: %q", detail)
	}
}

// TestEnclosingSectionChangedDetail_DifferentTimestamps_ProduceDifferentOutputs verifies
// that two calls differing only in timestamp produce different output.
func TestEnclosingSectionChangedDetail_DifferentTimestamps_ProduceDifferentOutputs(t *testing.T) {
	ts1 := "2026-08-17T22:41:49Z"
	ts2 := "2026-08-17T23:00:00Z"

	d1 := transform.EnclosingSectionChangedDetail("Identity", []string{"IdentityNote"}, ts1)
	d2 := transform.EnclosingSectionChangedDetail("Identity", []string{"IdentityNote"}, ts2)

	if !strings.Contains(d1, ts1) {
		t.Errorf("detail with timestamp %q does not contain the timestamp: %s", ts1, d1)
	}
	if !strings.Contains(d2, ts2) {
		t.Errorf("detail with timestamp %q does not contain the timestamp: %s", ts2, d2)
	}
	if d1 == d2 {
		t.Errorf("EnclosingSectionChangedDetail produced identical output for different timestamps:\nd1: %s\nd2: %s", d1, d2)
	}
}

// TestEnclosingSectionChangedDetail_StatesReviewRequired verifies that the detail text
// communicates a review action to the user. The exact wording is flexible; the test checks
// for key semantic indicators that tell the user what to do.
func TestEnclosingSectionChangedDetail_StatesReviewRequired(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail("Identity", []string{"IdentityNote"}, "")

	hasMeaningfulContent := strings.Contains(detail, "changed") ||
		strings.Contains(detail, "review") ||
		strings.Contains(detail, "Review") ||
		strings.Contains(detail, "contradiction") ||
		strings.Contains(detail, "Contradiction")
	if !hasMeaningfulContent {
		t.Errorf("detail does not communicate a section change or review instruction; "+
			"users need actionable guidance from the detail text: %s", detail)
	}
}

// TestEnclosingSectionChangedDetail_SingleCustomName_Renders verifies the common case:
// a single custom region name is included in the rendered text.
func TestEnclosingSectionChangedDetail_SingleCustomName_Renders(t *testing.T) {
	detail := transform.EnclosingSectionChangedDetail("Constraints", []string{"TeamPolicy"}, "")

	if !strings.Contains(detail, "Constraints") {
		t.Errorf("detail does not name the section %q: %s", "Constraints", detail)
	}
	if !strings.Contains(detail, "TeamPolicy") {
		t.Errorf("detail does not name the custom region %q: %s", "TeamPolicy", detail)
	}
}

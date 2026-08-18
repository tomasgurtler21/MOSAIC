package domain_test

// Tests for the run configuration value types introduced in Stage 1:
// ExecutionMode and CommitBranchVariant.
//
// Coverage:
//
//   ExecutionModes:
//   - Returns a non-empty slice.
//   - Contains ExecutionModeOrchestrated, ExecutionModeAuto, ExecutionModeAutoReview.
//   - Does NOT contain ExecutionModeUnset (the empty-string sentinel is never
//     a selectable run mode).
//
//   ParseExecutionMode:
//   - Empty string yields ExecutionModeUnset with nil error (absence is not a
//     parse failure; the caller decides whether unset is acceptable).
//   - "orchestrated" yields ExecutionModeOrchestrated with nil error.
//   - "auto" yields ExecutionModeAuto with nil error.
//   - "auto-review" yields ExecutionModeAutoReview with nil error.
//   - An unrecognised value yields a non-nil error.
//   - The error for an unrecognised value is a *RefusalError.
//   - The *RefusalError message contains the offending value.
//   - The *RefusalError message contains each valid mode name.
//
//   CommitBranchVariants:
//   - Returns a non-empty slice.
//   - CommitBranchMOSAICOwned is the first element (the recommended variant).
//   - Contains both CommitBranchMOSAICOwned and CommitBranchUserOwn.
//
//   ParseCommitBranchVariant:
//   - Empty string yields CommitBranchMOSAICOwned with nil error (documented
//     default: absence resolves to the recommended variant).
//   - "mosaic-owned" yields CommitBranchMOSAICOwned with nil error.
//   - "user-own" yields CommitBranchUserOwn with nil error.
//   - An unrecognised value yields a non-nil error.
//   - The error for an unrecognised value is a *RefusalError.
//   - The *RefusalError message contains the offending value.
//
//   MOSAICRunBranchName:
//   - Returns "mosaic/run/{runID}" for any non-empty runID.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// asRunConfigRefusalError asserts that err is (or wraps) a *domain.RefusalError
// and returns it. Calls t.Fatal on failure.
func asRunConfigRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// ---- ExecutionModes ----

func TestExecutionModes_NonEmpty(t *testing.T) {
	// Arrange / Act
	modes := domain.ExecutionModes()

	// Assert
	if len(modes) == 0 {
		t.Error("ExecutionModes: want non-empty slice, got empty")
	}
}

func TestExecutionModes_ContainsOrchestrated(t *testing.T) {
	modes := domain.ExecutionModes()

	for _, m := range modes {
		if m == domain.ExecutionModeOrchestrated {
			return
		}
	}
	t.Errorf("ExecutionModes: want slice to contain %q", domain.ExecutionModeOrchestrated)
}

func TestExecutionModes_ContainsAuto(t *testing.T) {
	modes := domain.ExecutionModes()

	for _, m := range modes {
		if m == domain.ExecutionModeAuto {
			return
		}
	}
	t.Errorf("ExecutionModes: want slice to contain %q", domain.ExecutionModeAuto)
}

func TestExecutionModes_ContainsAutoReview(t *testing.T) {
	modes := domain.ExecutionModes()

	for _, m := range modes {
		if m == domain.ExecutionModeAutoReview {
			return
		}
	}
	t.Errorf("ExecutionModes: want slice to contain %q", domain.ExecutionModeAutoReview)
}

func TestExecutionModes_DoesNotContainUnset(t *testing.T) {
	// ExecutionModeUnset ("") is a sentinel, never a selectable mode.
	modes := domain.ExecutionModes()

	for _, m := range modes {
		if m == domain.ExecutionModeUnset {
			t.Errorf("ExecutionModes: want slice to NOT contain the unset sentinel %q", domain.ExecutionModeUnset)
			return
		}
	}
}

// ---- ParseExecutionMode ----

func TestParseExecutionMode_EmptyString_ReturnsUnsetWithNilError(t *testing.T) {
	// Arrange / Act
	got, err := domain.ParseExecutionMode("")

	// Assert
	if err != nil {
		t.Fatalf("ParseExecutionMode(%q): want nil error, got %v", "", err)
	}
	if got != domain.ExecutionModeUnset {
		t.Errorf("ParseExecutionMode(%q): want %q, got %q", "", domain.ExecutionModeUnset, got)
	}
}

func TestParseExecutionMode_Orchestrated_ReturnsCorrectValue(t *testing.T) {
	// Arrange / Act
	got, err := domain.ParseExecutionMode("orchestrated")

	// Assert
	if err != nil {
		t.Fatalf("ParseExecutionMode(%q): want nil error, got %v", "orchestrated", err)
	}
	if got != domain.ExecutionModeOrchestrated {
		t.Errorf("ParseExecutionMode(%q): want %q, got %q", "orchestrated", domain.ExecutionModeOrchestrated, got)
	}
}

func TestParseExecutionMode_Auto_ReturnsCorrectValue(t *testing.T) {
	// Arrange / Act
	got, err := domain.ParseExecutionMode("auto")

	// Assert
	if err != nil {
		t.Fatalf("ParseExecutionMode(%q): want nil error, got %v", "auto", err)
	}
	if got != domain.ExecutionModeAuto {
		t.Errorf("ParseExecutionMode(%q): want %q, got %q", "auto", domain.ExecutionModeAuto, got)
	}
}

func TestParseExecutionMode_AutoReview_ReturnsCorrectValue(t *testing.T) {
	// Arrange / Act
	got, err := domain.ParseExecutionMode("auto-review")

	// Assert
	if err != nil {
		t.Fatalf("ParseExecutionMode(%q): want nil error, got %v", "auto-review", err)
	}
	if got != domain.ExecutionModeAutoReview {
		t.Errorf("ParseExecutionMode(%q): want %q, got %q", "auto-review", domain.ExecutionModeAutoReview, got)
	}
}

func TestParseExecutionMode_UnknownValue_ReturnsError(t *testing.T) {
	// Arrange / Act
	_, err := domain.ParseExecutionMode("quick")

	// Assert
	if err == nil {
		t.Fatal("ParseExecutionMode(\"quick\"): want non-nil error for unrecognised value, got nil")
	}
}

func TestParseExecutionMode_UnknownValue_ReturnsRefusalError(t *testing.T) {
	// Arrange / Act
	_, err := domain.ParseExecutionMode("quick")

	// Assert
	asRunConfigRefusalError(t, err)
}

func TestParseExecutionMode_UnknownValue_ErrorContainsOffendingValue(t *testing.T) {
	// Arrange
	offending := "quick"

	// Act
	_, err := domain.ParseExecutionMode(offending)

	// Assert
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), offending) {
		t.Errorf("ParseExecutionMode(%q): want error message to contain the offending value %q, got: %q",
			offending, offending, err.Error())
	}
}

func TestParseExecutionMode_UnknownValue_ErrorListsValidModes(t *testing.T) {
	// The error message must list every valid mode so the user knows what to supply.
	// Each mode string must appear in the error message.
	_, err := domain.ParseExecutionMode("invalid-mode")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, m := range []string{"orchestrated", "auto", "auto-review"} {
		if !strings.Contains(msg, m) {
			t.Errorf("ParseExecutionMode: want error message to contain valid mode %q, got: %q", m, msg)
		}
	}
}

// ---- CommitBranchVariants ----

func TestCommitBranchVariants_NonEmpty(t *testing.T) {
	// Arrange / Act
	variants := domain.CommitBranchVariants()

	// Assert
	if len(variants) == 0 {
		t.Error("CommitBranchVariants: want non-empty slice, got empty")
	}
}

func TestCommitBranchVariants_MOSAICOwnedIsFirst(t *testing.T) {
	// CommitBranchMOSAICOwned is recommended and must appear first in the
	// presentation order so UIs can select it as the default preselection.
	variants := domain.CommitBranchVariants()

	if len(variants) == 0 {
		t.Fatal("CommitBranchVariants: want non-empty slice, got empty")
	}
	if variants[0] != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariants[0]: want %q (recommended, first), got %q",
			domain.CommitBranchMOSAICOwned, variants[0])
	}
}

func TestCommitBranchVariants_ContainsMOSAICOwned(t *testing.T) {
	variants := domain.CommitBranchVariants()

	for _, v := range variants {
		if v == domain.CommitBranchMOSAICOwned {
			return
		}
	}
	t.Errorf("CommitBranchVariants: want slice to contain %q", domain.CommitBranchMOSAICOwned)
}

func TestCommitBranchVariants_ContainsUserOwn(t *testing.T) {
	variants := domain.CommitBranchVariants()

	for _, v := range variants {
		if v == domain.CommitBranchUserOwn {
			return
		}
	}
	t.Errorf("CommitBranchVariants: want slice to contain %q", domain.CommitBranchUserOwn)
}

// ---- ParseCommitBranchVariant ----

func TestParseCommitBranchVariant_EmptyString_ReturnsMOSAICOwnedWithNilError(t *testing.T) {
	// Absence of the key in frontmatter resolves to the recommended default.
	// Arrange / Act
	got, err := domain.ParseCommitBranchVariant("")

	// Assert
	if err != nil {
		t.Fatalf("ParseCommitBranchVariant(%q): want nil error, got %v", "", err)
	}
	if got != domain.CommitBranchMOSAICOwned {
		t.Errorf("ParseCommitBranchVariant(%q): want %q (default), got %q",
			"", domain.CommitBranchMOSAICOwned, got)
	}
}

func TestParseCommitBranchVariant_MOSAICOwned_ReturnsCorrectValue(t *testing.T) {
	// Arrange / Act
	got, err := domain.ParseCommitBranchVariant("mosaic-owned")

	// Assert
	if err != nil {
		t.Fatalf("ParseCommitBranchVariant(%q): want nil error, got %v", "mosaic-owned", err)
	}
	if got != domain.CommitBranchMOSAICOwned {
		t.Errorf("ParseCommitBranchVariant(%q): want %q, got %q",
			"mosaic-owned", domain.CommitBranchMOSAICOwned, got)
	}
}

func TestParseCommitBranchVariant_UserOwn_ReturnsCorrectValue(t *testing.T) {
	// Arrange / Act
	got, err := domain.ParseCommitBranchVariant("user-own")

	// Assert
	if err != nil {
		t.Fatalf("ParseCommitBranchVariant(%q): want nil error, got %v", "user-own", err)
	}
	if got != domain.CommitBranchUserOwn {
		t.Errorf("ParseCommitBranchVariant(%q): want %q, got %q",
			"user-own", domain.CommitBranchUserOwn, got)
	}
}

func TestParseCommitBranchVariant_UnknownValue_ReturnsError(t *testing.T) {
	// Arrange / Act
	_, err := domain.ParseCommitBranchVariant("weekly")

	// Assert
	if err == nil {
		t.Fatal("ParseCommitBranchVariant(\"weekly\"): want non-nil error for unrecognised value, got nil")
	}
}

func TestParseCommitBranchVariant_UnknownValue_ReturnsRefusalError(t *testing.T) {
	// Arrange / Act
	_, err := domain.ParseCommitBranchVariant("weekly")

	// Assert
	asRunConfigRefusalError(t, err)
}

func TestParseCommitBranchVariant_UnknownValue_ErrorContainsOffendingValue(t *testing.T) {
	// Arrange
	offending := "weekly"

	// Act
	_, err := domain.ParseCommitBranchVariant(offending)

	// Assert
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), offending) {
		t.Errorf("ParseCommitBranchVariant(%q): want error message to contain offending value %q, got: %q",
			offending, offending, err.Error())
	}
}

func TestParseCommitBranchVariant_UnknownValue_ErrorListsValidVariants(t *testing.T) {
	// The error message must list every valid variant so the user knows what to supply.
	// Each variant string must appear in the error message.
	_, err := domain.ParseCommitBranchVariant("invalid-variant")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, v := range []string{"mosaic-owned", "user-own"} {
		if !strings.Contains(msg, v) {
			t.Errorf("ParseCommitBranchVariant: want error message to contain valid variant %q, got: %q", v, msg)
		}
	}
}

// ---- MOSAICRunBranchName ----

func TestMOSAICRunBranchName_ReturnsExpectedFormat(t *testing.T) {
	// Arrange
	runID := "20260816T000724Z-3b68"

	// Act
	got := domain.MOSAICRunBranchName(runID)

	// Assert
	want := "mosaic/run/" + runID
	if got != want {
		t.Errorf("MOSAICRunBranchName(%q): want %q, got %q", runID, want, got)
	}
}

func TestMOSAICRunBranchName_PreservesRunIDExactly(t *testing.T) {
	// Arrange
	runID := "20260101T120000Z-abcd"

	// Act
	got := domain.MOSAICRunBranchName(runID)

	// Assert
	if !strings.HasSuffix(got, runID) {
		t.Errorf("MOSAICRunBranchName(%q): want result to end with the run ID, got %q", runID, got)
	}
	if !strings.HasPrefix(got, "mosaic/run/") {
		t.Errorf("MOSAICRunBranchName(%q): want result to start with \"mosaic/run/\", got %q", runID, got)
	}
}

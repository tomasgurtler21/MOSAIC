package domain_test

// Tests for Assertions.ExpandRunID: the pure copy-returning method that
// replaces RunIDPlaceholder with a real run identifier in every path-bearing
// field. The tests cover:
//
//   - ArtifactCreated and ArtifactNotCreated (the top-level path slices)
//   - The four artifact sets on each TaskMessageAssertion
//   - The invariant that fields containing no placeholder are copied unchanged
//   - The invariant that the original Assertions value is never mutated
//   - The nil-versus-empty distinction is preserved exactly
//   - An empty runID performs no substitution

import (
	"testing"

	"mosaic-agent-test/internal/domain"
)

const testRunID = "20260816T092714Z-df8b"

// expanded returns the expected expansion of a single path that contains the
// placeholder, or the path verbatim when it contains none.
func expanded(path string) string {
	// ExpandRunID replaces every occurrence of RunIDPlaceholder with the run
	// id. This helper mirrors that replacement so test expectations are
	// expressed without hard-coding the exact placeholder string.
	result := ""
	placeholder := domain.RunIDPlaceholder
	for i := 0; i < len(path); {
		if i+len(placeholder) <= len(path) && path[i:i+len(placeholder)] == placeholder {
			result += testRunID
			i += len(placeholder)
		} else {
			result += string(path[i])
			i++
		}
	}
	return result
}

// --- ArtifactCreated ---

func TestAssertions_ExpandRunID_ArtifactCreated_PlaceholderIsReplaced(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md"
	a := domain.Assertions{
		ArtifactCreated: []string{declared},
	}

	got := a.ExpandRunID(testRunID)

	want := "Orchestration-" + testRunID + "/Plan.md"
	if len(got.ArtifactCreated) != 1 {
		t.Fatalf("ArtifactCreated len = %d, want 1", len(got.ArtifactCreated))
	}
	if got.ArtifactCreated[0] != want {
		t.Errorf("ArtifactCreated[0] = %q, want %q", got.ArtifactCreated[0], want)
	}
}

func TestAssertions_ExpandRunID_ArtifactCreated_NoPlaceholder_Unchanged(t *testing.T) {
	plain := "Research.md"
	a := domain.Assertions{
		ArtifactCreated: []string{plain},
	}

	got := a.ExpandRunID(testRunID)

	if len(got.ArtifactCreated) != 1 {
		t.Fatalf("ArtifactCreated len = %d, want 1", len(got.ArtifactCreated))
	}
	if got.ArtifactCreated[0] != plain {
		t.Errorf("ArtifactCreated[0] = %q, want %q (unchanged)", got.ArtifactCreated[0], plain)
	}
}

func TestAssertions_ExpandRunID_ArtifactCreated_MixedPaths(t *testing.T) {
	placeholder := "Orchestration-" + domain.RunIDPlaceholder + "/output.md"
	plain := "Requirements.md"
	a := domain.Assertions{
		ArtifactCreated: []string{placeholder, plain},
	}

	got := a.ExpandRunID(testRunID)

	if len(got.ArtifactCreated) != 2 {
		t.Fatalf("ArtifactCreated len = %d, want 2", len(got.ArtifactCreated))
	}
	wantExpanded := "Orchestration-" + testRunID + "/output.md"
	if got.ArtifactCreated[0] != wantExpanded {
		t.Errorf("ArtifactCreated[0] = %q, want %q", got.ArtifactCreated[0], wantExpanded)
	}
	if got.ArtifactCreated[1] != plain {
		t.Errorf("ArtifactCreated[1] = %q, want %q (unchanged)", got.ArtifactCreated[1], plain)
	}
}

// --- ArtifactNotCreated ---

func TestAssertions_ExpandRunID_ArtifactNotCreated_PlaceholderIsReplaced(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/scratch.tmp"
	a := domain.Assertions{
		ArtifactNotCreated: []string{declared},
	}

	got := a.ExpandRunID(testRunID)

	want := "Orchestration-" + testRunID + "/scratch.tmp"
	if len(got.ArtifactNotCreated) != 1 {
		t.Fatalf("ArtifactNotCreated len = %d, want 1", len(got.ArtifactNotCreated))
	}
	if got.ArtifactNotCreated[0] != want {
		t.Errorf("ArtifactNotCreated[0] = %q, want %q", got.ArtifactNotCreated[0], want)
	}
}

func TestAssertions_ExpandRunID_ArtifactNotCreated_NoPlaceholder_Unchanged(t *testing.T) {
	plain := "ephemeral.log"
	a := domain.Assertions{
		ArtifactNotCreated: []string{plain},
	}

	got := a.ExpandRunID(testRunID)

	if len(got.ArtifactNotCreated) != 1 {
		t.Fatalf("ArtifactNotCreated len = %d, want 1", len(got.ArtifactNotCreated))
	}
	if got.ArtifactNotCreated[0] != plain {
		t.Errorf("ArtifactNotCreated[0] = %q, want %q (unchanged)", got.ArtifactNotCreated[0], plain)
	}
}

// --- TaskMessageAssertion artifact sets ---

func TestAssertions_ExpandRunID_TaskMessage_RequiredInputArtifacts_PlaceholderIsReplaced(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/input.md"
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{At: 1, RequiredInputArtifacts: []string{declared}},
		},
	}

	got := a.ExpandRunID(testRunID)

	want := "Orchestration-" + testRunID + "/input.md"
	if len(got.TaskMessages) != 1 {
		t.Fatalf("TaskMessages len = %d, want 1", len(got.TaskMessages))
	}
	if len(got.TaskMessages[0].RequiredInputArtifacts) != 1 {
		t.Fatalf("RequiredInputArtifacts len = %d, want 1", len(got.TaskMessages[0].RequiredInputArtifacts))
	}
	if got.TaskMessages[0].RequiredInputArtifacts[0] != want {
		t.Errorf("RequiredInputArtifacts[0] = %q, want %q", got.TaskMessages[0].RequiredInputArtifacts[0], want)
	}
}

func TestAssertions_ExpandRunID_TaskMessage_OptionalInputArtifacts_PlaceholderIsReplaced(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/context.md"
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{At: 1, OptionalInputArtifacts: []string{declared}},
		},
	}

	got := a.ExpandRunID(testRunID)

	want := "Orchestration-" + testRunID + "/context.md"
	if len(got.TaskMessages) != 1 {
		t.Fatalf("TaskMessages len = %d, want 1", len(got.TaskMessages))
	}
	if len(got.TaskMessages[0].OptionalInputArtifacts) != 1 {
		t.Fatalf("OptionalInputArtifacts len = %d, want 1", len(got.TaskMessages[0].OptionalInputArtifacts))
	}
	if got.TaskMessages[0].OptionalInputArtifacts[0] != want {
		t.Errorf("OptionalInputArtifacts[0] = %q, want %q", got.TaskMessages[0].OptionalInputArtifacts[0], want)
	}
}

func TestAssertions_ExpandRunID_TaskMessage_RequiredOutputArtifacts_PlaceholderIsReplaced(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/output.md"
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{At: 1, RequiredOutputArtifacts: []string{declared}},
		},
	}

	got := a.ExpandRunID(testRunID)

	want := "Orchestration-" + testRunID + "/output.md"
	if len(got.TaskMessages) != 1 {
		t.Fatalf("TaskMessages len = %d, want 1", len(got.TaskMessages))
	}
	if len(got.TaskMessages[0].RequiredOutputArtifacts) != 1 {
		t.Fatalf("RequiredOutputArtifacts len = %d, want 1", len(got.TaskMessages[0].RequiredOutputArtifacts))
	}
	if got.TaskMessages[0].RequiredOutputArtifacts[0] != want {
		t.Errorf("RequiredOutputArtifacts[0] = %q, want %q", got.TaskMessages[0].RequiredOutputArtifacts[0], want)
	}
}

func TestAssertions_ExpandRunID_TaskMessage_OptionalOutputArtifacts_PlaceholderIsReplaced(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/notes.md"
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{At: 1, OptionalOutputArtifacts: []string{declared}},
		},
	}

	got := a.ExpandRunID(testRunID)

	want := "Orchestration-" + testRunID + "/notes.md"
	if len(got.TaskMessages) != 1 {
		t.Fatalf("TaskMessages len = %d, want 1", len(got.TaskMessages))
	}
	if len(got.TaskMessages[0].OptionalOutputArtifacts) != 1 {
		t.Fatalf("OptionalOutputArtifacts len = %d, want 1", len(got.TaskMessages[0].OptionalOutputArtifacts))
	}
	if got.TaskMessages[0].OptionalOutputArtifacts[0] != want {
		t.Errorf("OptionalOutputArtifacts[0] = %q, want %q", got.TaskMessages[0].OptionalOutputArtifacts[0], want)
	}
}

func TestAssertions_ExpandRunID_TaskMessage_AllFourSets_ExpandedIndependently(t *testing.T) {
	// Exercise all four path-bearing sets in a single entry to catch an
	// implementation that expands only some of them.
	mkPath := func(field string) string {
		return "Orchestration-" + domain.RunIDPlaceholder + "/" + field + ".md"
	}
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{
				At:                      1,
				RequiredInputArtifacts:  []string{mkPath("req-in")},
				OptionalInputArtifacts:  []string{mkPath("opt-in")},
				RequiredOutputArtifacts: []string{mkPath("req-out")},
				OptionalOutputArtifacts: []string{mkPath("opt-out")},
			},
		},
	}

	got := a.ExpandRunID(testRunID)

	entry := got.TaskMessages[0]
	check := func(field, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	check("RequiredInputArtifacts[0]", entry.RequiredInputArtifacts[0], "Orchestration-"+testRunID+"/req-in.md")
	check("OptionalInputArtifacts[0]", entry.OptionalInputArtifacts[0], "Orchestration-"+testRunID+"/opt-in.md")
	check("RequiredOutputArtifacts[0]", entry.RequiredOutputArtifacts[0], "Orchestration-"+testRunID+"/req-out.md")
	check("OptionalOutputArtifacts[0]", entry.OptionalOutputArtifacts[0], "Orchestration-"+testRunID+"/opt-out.md")
}

func TestAssertions_ExpandRunID_TaskMessage_NonPathFields_Untouched(t *testing.T) {
	// At, Identity, HumanInTheLoop, and TaskDescriptionContains are not path
	// fields. They must survive the copy unchanged even if they happen to
	// contain the placeholder text.
	wantHITL := true
	placeholderText := "see " + domain.RunIDPlaceholder + " for context"
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{
				At:                      3,
				HumanInTheLoop:          &wantHITL,
				TaskDescriptionContains: []string{placeholderText},
			},
		},
	}

	got := a.ExpandRunID(testRunID)

	entry := got.TaskMessages[0]
	if entry.At != 3 {
		t.Errorf("At = %d, want 3", entry.At)
	}
	if entry.HumanInTheLoop == nil || !*entry.HumanInTheLoop {
		t.Error("HumanInTheLoop was changed or lost")
	}
	// TaskDescriptionContains is not a path field; the placeholder text
	// must be copied verbatim, not expanded.
	if len(entry.TaskDescriptionContains) != 1 || entry.TaskDescriptionContains[0] != placeholderText {
		t.Errorf("TaskDescriptionContains = %v, want [%q] (unchanged)", entry.TaskDescriptionContains, placeholderText)
	}
}

// --- Empty runID is a no-op ---

func TestAssertions_ExpandRunID_EmptyRunID_NoSubstitution(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md"
	a := domain.Assertions{
		ArtifactCreated: []string{declared},
	}

	got := a.ExpandRunID("")

	// With an empty run ID, the placeholder must be left as-is.
	if len(got.ArtifactCreated) != 1 {
		t.Fatalf("ArtifactCreated len = %d, want 1", len(got.ArtifactCreated))
	}
	if got.ArtifactCreated[0] != declared {
		t.Errorf("ArtifactCreated[0] = %q, want %q (unchanged with empty run id)", got.ArtifactCreated[0], declared)
	}
}

// --- Original is never mutated ---

func TestAssertions_ExpandRunID_DoesNotMutateOriginal(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md"
	a := domain.Assertions{
		ArtifactCreated:    []string{declared},
		ArtifactNotCreated: []string{declared},
		TaskMessages: []domain.TaskMessageAssertion{
			{
				At:                      1,
				RequiredInputArtifacts:  []string{declared},
				OptionalInputArtifacts:  []string{declared},
				RequiredOutputArtifacts: []string{declared},
				OptionalOutputArtifacts: []string{declared},
			},
		},
	}

	_ = a.ExpandRunID(testRunID)

	// The original must be unchanged in every field.
	if a.ArtifactCreated[0] != declared {
		t.Errorf("original ArtifactCreated[0] was mutated: got %q, want %q", a.ArtifactCreated[0], declared)
	}
	if a.ArtifactNotCreated[0] != declared {
		t.Errorf("original ArtifactNotCreated[0] was mutated: got %q, want %q", a.ArtifactNotCreated[0], declared)
	}
	entry := a.TaskMessages[0]
	for field, val := range map[string]string{
		"RequiredInputArtifacts[0]":  entry.RequiredInputArtifacts[0],
		"OptionalInputArtifacts[0]":  entry.OptionalInputArtifacts[0],
		"RequiredOutputArtifacts[0]": entry.RequiredOutputArtifacts[0],
		"OptionalOutputArtifacts[0]": entry.OptionalOutputArtifacts[0],
	} {
		if val != declared {
			t.Errorf("original TaskMessages[0].%s was mutated: got %q, want %q", field, val, declared)
		}
	}
}

// --- Nil-versus-empty preservation ---

func TestAssertions_ExpandRunID_NilSlice_CopiedAsNil(t *testing.T) {
	// A nil ArtifactCreated means "not declared"; it must not become an empty
	// non-nil slice, which would mean "assert the empty set".
	a := domain.Assertions{} // all slices nil

	got := a.ExpandRunID(testRunID)

	if got.ArtifactCreated != nil {
		t.Errorf("ArtifactCreated = %v, want nil (nil means not evaluated)", got.ArtifactCreated)
	}
	if got.ArtifactNotCreated != nil {
		t.Errorf("ArtifactNotCreated = %v, want nil (nil means not evaluated)", got.ArtifactNotCreated)
	}
	if got.TaskMessages != nil {
		t.Errorf("TaskMessages = %v, want nil", got.TaskMessages)
	}
}

func TestAssertions_ExpandRunID_EmptyNonNilSlice_CopiedAsEmptyNonNil(t *testing.T) {
	// An empty non-nil ArtifactCreated means "assert the empty set" (trivially
	// passes). It must remain non-nil after expansion.
	a := domain.Assertions{
		ArtifactCreated:    []string{},
		ArtifactNotCreated: []string{},
	}

	got := a.ExpandRunID(testRunID)

	if got.ArtifactCreated == nil {
		t.Error("ArtifactCreated = nil, want non-nil empty slice (empty non-nil means assert empty set)")
	}
	if len(got.ArtifactCreated) != 0 {
		t.Errorf("ArtifactCreated len = %d, want 0", len(got.ArtifactCreated))
	}
	if got.ArtifactNotCreated == nil {
		t.Error("ArtifactNotCreated = nil, want non-nil empty slice (empty non-nil means assert empty set)")
	}
	if len(got.ArtifactNotCreated) != 0 {
		t.Errorf("ArtifactNotCreated len = %d, want 0", len(got.ArtifactNotCreated))
	}
}

// --- Multiple TaskMessageAssertions ---

func TestAssertions_ExpandRunID_MultipleTaskMessages_AllExpanded(t *testing.T) {
	// Two entries: both must be expanded. A bug that only expands the first
	// entry would be caught here.
	mkPath := func(n int) string {
		return "Orchestration-" + domain.RunIDPlaceholder + "/task-" + string(rune('0'+n)) + ".md"
	}
	a := domain.Assertions{
		TaskMessages: []domain.TaskMessageAssertion{
			{At: 1, RequiredOutputArtifacts: []string{mkPath(1)}},
			{At: 2, RequiredOutputArtifacts: []string{mkPath(2)}},
		},
	}

	got := a.ExpandRunID(testRunID)

	if len(got.TaskMessages) != 2 {
		t.Fatalf("TaskMessages len = %d, want 2", len(got.TaskMessages))
	}
	want1 := "Orchestration-" + testRunID + "/task-1.md"
	want2 := "Orchestration-" + testRunID + "/task-2.md"
	if got.TaskMessages[0].RequiredOutputArtifacts[0] != want1 {
		t.Errorf("TaskMessages[0].RequiredOutputArtifacts[0] = %q, want %q", got.TaskMessages[0].RequiredOutputArtifacts[0], want1)
	}
	if got.TaskMessages[1].RequiredOutputArtifacts[0] != want2 {
		t.Errorf("TaskMessages[1].RequiredOutputArtifacts[0] = %q, want %q", got.TaskMessages[1].RequiredOutputArtifacts[0], want2)
	}
}

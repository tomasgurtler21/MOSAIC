package preflight_test

// Tests for the cross-reference check and CLI filter behavior after the
// domain rename: registry.TestID is compared against Definition.Name, and
// Overrides.TestIDs filters by Definition.Name.
//
// Coverage:
//   - The test-id-mismatch diagnostic message says "name" not "id", so
//     users know the field they must align.
//   - Overrides.TestIDs filters the returned plan to tests whose
//     Definition.Name appears in the set, preserving order.
//   - A filter that matches only one test in a two-test suite excludes the
//     other, confirming the filter acts on Name and not on something else
//     (e.g. the numeric id field or the suite-entry position).
//   - An empty Overrides.TestIDs leaves all tests in the plan.
//   - A filter supplying a numeric id string does not match any test,
//     confirming the filter compares Name (string) not NumericID (int).

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// TestValidate_RegistryTestIDMismatch_DiagnosticMessageNamesDefinitionName
// asserts that when the stub registry's test_id does not match the test
// definition's Name, the produced diagnostic message explicitly refers to the
// definition's "name" field, not the old "id" field. This pins the user-facing
// wording so a regression from "name" back to "id" is caught.
func TestValidate_RegistryTestIDMismatch_DiagnosticMessageNamesDefinitionName(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":         validSuite,
		"happy-path.test.yaml": validDefinition,
		"happy-path.stubs.json": `{
  "schema_version": 1,
  "test_id": "wrong-name",
  "on_unmatched": "halt",
  "stubs": []
}`,
		"agents/researcher.md": "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	d, found := findDiag(report, "test-id-mismatch")
	if !found {
		t.Fatalf("expected a %q diagnostic but none found; got: %v", "test-id-mismatch", report.Diagnostics)
	}
	if !strings.Contains(d.Message, "name") {
		t.Errorf("test-id-mismatch diagnostic message %q does not contain the word %q; "+
			"the message must say 'name' so users know to align the registry's test_id "+
			"with the definition's name field, not with any other identifier",
			d.Message, "name")
	}
	if strings.Contains(strings.ToLower(d.Message), "definition id") ||
		strings.Contains(strings.ToLower(d.Message), "definition's id") {
		t.Errorf("test-id-mismatch diagnostic message %q mentions the old 'id' phrasing; "+
			"it must refer to the definition's 'name' field after the rename",
			d.Message)
	}
}

// TestValidate_FilterByTestIDs_MatchesByDefinitionName asserts that
// Overrides.TestIDs restricts the returned plan to tests whose
// Definition.Name appears in the filter set. When the filter names exactly
// one test from a two-test suite, only that test appears in the plan.
func TestValidate_FilterByTestIDs_MatchesByDefinitionName(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			TestIDs: []string{"test-a"},
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) != 1 {
		t.Fatalf("len(plan.Tests) = %d, want 1; "+
			"Overrides.TestIDs = [\"test-a\"] must restrict the plan to the single test "+
			"whose Definition.Name is \"test-a\"",
			len(plan.Tests))
	}
	if plan.Tests[0].Definition.Name != "test-a" {
		t.Errorf("plan.Tests[0].Definition.Name = %q, want %q; "+
			"the retained test must be the one whose Name matches the filter",
			plan.Tests[0].Definition.Name, "test-a")
	}
}

// TestValidate_FilterByTestIDs_ExcludesNonMatchingByName asserts that a test
// whose Definition.Name does not appear in Overrides.TestIDs is excluded from
// the plan. This is the complementary assertion to
// TestValidate_FilterByTestIDs_MatchesByDefinitionName: together they confirm
// that the filter both includes the matching test and excludes the
// non-matching one.
func TestValidate_FilterByTestIDs_ExcludesNonMatchingByName(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			TestIDs: []string{"test-b"},
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}

	for i, rt := range plan.Tests {
		if rt.Definition.Name == "test-a" {
			t.Errorf("plan.Tests[%d].Definition.Name = %q; "+
				"test-a must not appear in the plan when Overrides.TestIDs = [\"test-b\"]",
				i, rt.Definition.Name)
		}
	}
	if len(plan.Tests) != 1 || plan.Tests[0].Definition.Name != "test-b" {
		t.Errorf("plan.Tests names = %v; want exactly one test with Definition.Name \"test-b\"",
			planTestNames(plan.Tests))
	}
}

// TestValidate_FilterByTestIDs_EmptyFilter_IncludesAllTests asserts that an
// empty Overrides.TestIDs leaves all tests in the returned plan. An empty
// filter means "no restriction" — every test in the suite runs.
func TestValidate_FilterByTestIDs_EmptyFilter_IncludesAllTests(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides:   preflight.Overrides{}, // TestIDs intentionally nil/empty
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) != 2 {
		t.Fatalf("len(plan.Tests) = %d, want 2; "+
			"an empty Overrides.TestIDs must include all tests from the suite",
			len(plan.Tests))
	}
}

// TestValidate_FilterByTestIDs_MultipleMatchingNames_RetainsBoth asserts that
// when Overrides.TestIDs contains both names present in the suite, both tests
// appear in the returned plan and their order is preserved. This is the
// all-match boundary: every test in the suite matches the filter, so the
// plan must be identical to the unfiltered result.
func TestValidate_FilterByTestIDs_MultipleMatchingNames_RetainsBoth(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			TestIDs: []string{"test-a", "test-b"},
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) != 2 {
		t.Fatalf("len(plan.Tests) = %d, want 2; "+
			"Overrides.TestIDs = [\"test-a\", \"test-b\"] must retain all tests "+
			"when every suite test matches a filter entry; got names: %v",
			len(plan.Tests), planTestNames(plan.Tests))
	}
	// Order must be preserved: test-a first, test-b second.
	if plan.Tests[0].Definition.Name != "test-a" {
		t.Errorf("plan.Tests[0].Definition.Name = %q, want %q; "+
			"filter must preserve suite order, not reorder tests by filter position",
			plan.Tests[0].Definition.Name, "test-a")
	}
	if plan.Tests[1].Definition.Name != "test-b" {
		t.Errorf("plan.Tests[1].Definition.Name = %q, want %q; "+
			"filter must preserve suite order, not reorder tests by filter position",
			plan.Tests[1].Definition.Name, "test-b")
	}
}

// TestValidate_FilterByTestIDs_MatchesByName_NotByNumericID asserts that the
// filter matches on the string Definition.Name, not on the integer numeric id.
// A filter that supplies the string representation of the numeric id ("1") must
// NOT retain a test whose Definition.Name is "test-a" (even though test-a has
// numeric id 1). This distinguishes the Name-based filter from a hypothetical
// ID-based filter and pins the correct field access after the ID->Name rename.
func TestValidate_FilterByTestIDs_MatchesByName_NotByNumericID(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			// "1" is the numeric id of test-a, but it is not its Name.
			// A Name-based filter must not match it.
			TestIDs: []string{"1"},
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) != 0 {
		t.Errorf("len(plan.Tests) = %d, want 0; "+
			"Overrides.TestIDs = [\"1\"] must not match any test by numeric id — "+
			"the filter compares against Definition.Name (the string), not the numeric identifier; "+
			"retained tests: %v",
			len(plan.Tests), planTestNames(plan.Tests))
	}
}

// TestValidate_CrossRef_RegistryTestIDMatchesDefinitionName_NoMismatchDiagnostic
// asserts the happy path: when the stub registry's test_id equals the test
// definition's Name field, no test-id-mismatch diagnostic is produced. This
// is the pass-case complement of
// TestValidate_RegistryTestIDMismatch_DiagnosticMessageNamesDefinitionName and
// makes AC3.1's pass branch explicitly auditable.
func TestValidate_CrossRef_RegistryTestIDMatchesDefinitionName_NoMismatchDiagnostic(t *testing.T) {
	// validDefinition has name: happy-path; validRegistry has test_id: "happy-path".
	// They match, so no test-id-mismatch diagnostic should appear.
	root := writeTree(t, map[string]string{
		"s.suite.yaml":         validSuite,
		"happy-path.test.yaml": validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md": "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if _, found := findDiag(report, "test-id-mismatch"); found {
		t.Errorf("unexpected test-id-mismatch diagnostic when registry.test_id == definition.Name; "+
			"cross-reference check must produce no diagnostic when the names match. "+
			"Report diagnostics: %v", report.Diagnostics)
	}
}

// planTestNames returns the Definition.Name of each test in ts, for use in
// error messages.
func planTestNames(ts []preflight.ResolvedTest) []string {
	names := make([]string, len(ts))
	for i, rt := range ts {
		names[i] = rt.Definition.Name
	}
	return names
}

// findDiag returns the first diagnostic in report with the given code and
// whether one was found. It is the message-inspecting counterpart of
// hasDiagnosticCode, which only tests presence.
func findDiag(report authoring.Report, code string) (authoring.Diagnostic, bool) {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return d, true
		}
	}
	return authoring.Diagnostic{}, false
}

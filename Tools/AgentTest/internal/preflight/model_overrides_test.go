package preflight_test

// Tests that Overrides.SubjectModel and Overrides.StubModel are copied into
// every ResolvedTest.Models in the plan returned by Validate (Stage 10).
//
// Model selection is run-level: the same selection applies to every test in
// the plan. Validate is the only place the override travels from the Overrides
// struct to the per-test ResolvedTest, so these tests pin that path.
//
// Coverage (T10.1):
//   - Overrides.SubjectModel reaches every ResolvedTest.Models.Subject.
//   - Overrides.StubModel reaches every ResolvedTest.Models.Stub.
//
// Coverage (T10.2):
//   - Empty overrides leave ResolvedTest.Models at its zero value, so a run
//     with no frontend model choice falls through to today's authored-field
//     behaviour rather than to an empty override that silently replaces it.
//
// All assertions are in the TDD RED phase: they compile against the current
// Validate implementation and fail because Validate does not yet copy
// SubjectModel/StubModel into ResolvedTest.Models.

import (
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/preflight"
)

// twoTestSuite is a minimal suite fixture with two test entries, used to
// verify that the model override propagates to all resolved tests, not just
// the first one.
const twoTestSuite = `
schema_version: 1
id: s
tests:
  - path: test-a.test.yaml
  - path: test-b.test.yaml
`

const testADefinition = `
schema_version: 1
name: test-a
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: test-a.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`

const testBDefinition = `
schema_version: 1
name: test-b
id: 2
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: test-b.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`

const testARegistry = `{
  "schema_version": 1,
  "test_id": "test-a",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

const testBRegistry = `{
  "schema_version": 1,
  "test_id": "test-b",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

// twoTestTree builds a suite with two tests, sharing a researcher stub agent
// file, under t.TempDir(). It returns the root directory.
func twoTestTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"s.suite.yaml":       twoTestSuite,
		"test-a.test.yaml":   testADefinition,
		"test-b.test.yaml":   testBDefinition,
		"test-a.stubs.json":  testARegistry,
		"test-b.stubs.json":  testBRegistry,
		"agents/researcher.md": "# stub researcher placeholder",
	})
}

// TestValidate_SubjectModelOverride_ReachesAllResolvedTests asserts that when
// Overrides.SubjectModel is non-empty, every ResolvedTest.Models.Subject in
// the returned plan carries that value. The override must not be applied to
// only the first test or silently dropped.
func TestValidate_SubjectModelOverride_ReachesAllResolvedTests(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			SubjectModel: "runtime-subject-model",
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("Plan contains no tests; want at least one test from the two-test suite")
	}

	for i, rt := range plan.Tests {
		if rt.Models.Subject != "runtime-subject-model" {
			t.Errorf("plan.Tests[%d].Models.Subject = %q, want %q; "+
				"Overrides.SubjectModel must be copied into every ResolvedTest.Models.Subject "+
				"so the runtime model reaches the runner's tier-model map through the existing "+
				"per-attempt channel",
				i, rt.Models.Subject, "runtime-subject-model")
		}
	}
}

// TestValidate_StubModelOverride_ReachesAllResolvedTests asserts that when
// Overrides.StubModel is non-empty, every ResolvedTest.Models.Stub in the
// returned plan carries that value. Both model overrides must propagate;
// neither is optional.
func TestValidate_StubModelOverride_ReachesAllResolvedTests(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			SubjectModel: "runtime-subject-model",
			StubModel:    "runtime-stub-model",
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("Plan contains no tests; want at least one test from the two-test suite")
	}

	for i, rt := range plan.Tests {
		if rt.Models.Stub != "runtime-stub-model" {
			t.Errorf("plan.Tests[%d].Models.Stub = %q, want %q; "+
				"Overrides.StubModel must be copied into every ResolvedTest.Models.Stub "+
				"so the runtime stub model reaches the runner's tier-model map",
				i, rt.Models.Stub, "runtime-stub-model")
		}
	}
}

// TestValidate_BothModelOverrides_ReachEveryResolvedTest asserts that both
// SubjectModel and StubModel overrides propagate to every test in the plan in
// a single Validate call, confirming that the copy is unconditional and covers
// all tests regardless of their position in the plan.
func TestValidate_BothModelOverrides_ReachEveryResolvedTest(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Overrides: preflight.Overrides{
			SubjectModel: "runtime-subject-model",
			StubModel:    "runtime-stub-model",
		},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) < 2 {
		t.Fatalf("Plan contains %d test(s), want 2; "+
			"this test needs both tests to verify the override reaches every resolved test",
			len(plan.Tests))
	}

	for i, rt := range plan.Tests {
		if rt.Models.Subject != "runtime-subject-model" {
			t.Errorf("plan.Tests[%d].Models.Subject = %q, want %q",
				i, rt.Models.Subject, "runtime-subject-model")
		}
		if rt.Models.Stub != "runtime-stub-model" {
			t.Errorf("plan.Tests[%d].Models.Stub = %q, want %q",
				i, rt.Models.Stub, "runtime-stub-model")
		}
	}
}

// TestValidate_EmptyModelOverrides_LeaveModelsZeroValued asserts that when
// neither Overrides.SubjectModel nor Overrides.StubModel is set, every
// ResolvedTest.Models remains zero-valued. A zero-valued ModelSelection means
// "no run-time choice was made" and the runner falls through to the authored
// fields — today's behaviour, which must remain intact.
func TestValidate_EmptyModelOverrides_LeaveModelsZeroValued(t *testing.T) {
	root := twoTestTree(t)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		// Overrides intentionally has no SubjectModel or StubModel.
		Overrides: preflight.Overrides{},
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("Plan contains no tests; want at least one test from the two-test suite")
	}

	for i, rt := range plan.Tests {
		if rt.Models.Subject != "" {
			t.Errorf("plan.Tests[%d].Models.Subject = %q, want empty string; "+
				"absent model overrides must not inject a non-empty value into Models.Subject — "+
				"the runner must see the zero value and fall through to today's authored-field behaviour",
				i, rt.Models.Subject)
		}
		if rt.Models.Stub != "" {
			t.Errorf("plan.Tests[%d].Models.Stub = %q, want empty string; "+
				"absent model overrides must not inject a non-empty value into Models.Stub",
				i, rt.Models.Stub)
		}
	}
}

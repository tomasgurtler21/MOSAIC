package preflight_test

// Tests for Validate: cross-format validation composing internal/authoring
// and internal/fixtures into one aggregated, deterministically ordered
// error report before any process is spawned.
//
// Coverage follows T4.4: a suite referencing a missing test definition, a
// test definition referencing a missing stub registry, an assertion naming
// a collaborator absent from the registry, an unresolvable $ref, and a
// declared parallel group naming an invocation that never appears in the
// expected sequence. The decisive case is accumulation: a document with
// several unrelated errors must report all of them in one pass, ordered
// deterministically, not stop at the first (AC4.2).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// writeTree materializes a fixture project under t.TempDir(): a suite file,
// a set of test definitions, stub registries and fixture files, each keyed
// by its path relative to the root. It returns the root directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}
	return root
}

const validSuite = `
schema_version: 1
id: s
tests:
  - path: happy-path.test.yaml
`

const validDefinition = `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
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

const validRegistry = `{
  "schema_version": 1,
  "test_id": "happy-path",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

func TestValidate_HappyPath_NoErrors(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected the reference example tree to pass pre-flight cleanly, got: %v", report.Diagnostics)
	}
}

// TestValidate_ReferenceExamples_PassCleanly is the direct coverage for
// AC4.6: "The reference example files under testdata pass pre-flight
// cleanly." I4.4 authors that tree — one suite, one full-workflow test
// definition, one single-decision test definition, one stub registry —
// under Tools/AgentTest/testdata/examples/, doubling as this format's
// documentation and as pre-flight's happy-path fixture.
//
// This test is a valid RED today: the tree does not exist yet, so it fails
// on a missing suite file rather than on a "not-implemented" diagnostic —
// exactly as valid a RED per this module's own convention (a missing
// directory is exactly as much evidence of unfinished work as a stub
// diagnostic). Unlike TestValidate_HappyPath_NoErrors, which builds its own
// ad-hoc tree in a temp dir and only exercises the same *logic*, this test
// is the one in this package that verifies I4.4's actual deliverable: an
// implementation that completes I4.1-I4.3 but ships a broken or missing
// testdata/examples/ tree must fail this test even though every other test
// in this package is green.
func TestValidate_ReferenceExamples_PassCleanly(t *testing.T) {
	root := referenceExamplesRoot(t)

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "happy-path.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected the reference example tree under %q to pass pre-flight cleanly, got: %v", root, report.Diagnostics)
	}
}

// referenceExamplesRoot locates Tools/AgentTest/testdata/examples relative
// to this test file's own location, independent of the working directory
// `go test` happens to be invoked from.
func referenceExamplesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's path")
	}
	// This file lives at internal/preflight/preflight_test.go.
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "examples")
}

func TestValidate_SuiteReferencesMissingTestDefinition_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: does-not-exist.test.yaml
`,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !hasDiagnosticCode(report, "missing-test-definition") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "missing-test-definition", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a suite entry naming a missing test definition to be reported")
	}
}

func TestValidate_TestDefinitionReferencesMissingStubRegistry_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": validSuite,
		"happy-path.test.yaml": `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: does-not-exist.stubs.json
`,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !hasDiagnosticCode(report, "missing-stub-registry") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "missing-stub-registry", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a test definition naming a missing stub registry to be reported")
	}
}

func TestValidate_AssertionNamesCollaboratorAbsentFromRegistry_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": validSuite,
		"happy-path.test.yaml": `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
assertions:
  task_messages:
    - at: 1
      identity: { tool: Task, agent: nobody-in-the-registry }
`,
		"happy-path.stubs.json": validRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !hasDiagnosticCode(report, "unknown-collaborator") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "unknown-collaborator", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected an assertion naming a collaborator absent from the registry to be reported")
	}
}

func TestValidate_UnresolvableRef_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": validSuite,
		"happy-path.test.yaml": `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
seed_files:
  - path: Orchestration-{run_id}/Orchestration.md
    ref: does-not-exist.md
`,
		"happy-path.stubs.json": validRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !hasDiagnosticCode(report, "unresolvable-ref") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "unresolvable-ref", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected an unresolvable $ref to be reported")
	}
}

func TestValidate_ParallelGroupNamesInvocationNeverInSequence_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": validSuite,
		"happy-path.test.yaml": `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
parallel_groups:
  - name: research-fanout
    members:
      - { tool: Task, agent: researcher }
      - { tool: Task, agent: library-researcher }
assertions:
  invocation_sequence:
    exact: false
    steps:
      - { tool: Task, agent: researcher }
`,
		"happy-path.stubs.json": validRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !hasDiagnosticCode(report, "unknown-parallel-group-member") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "unknown-parallel-group-member", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a parallel group naming a member absent from the expected sequence to be reported")
	}
}

// TestValidate_RegistryTestIDMismatchesDefinitionID_ReportsError covers one
// of ContractsDesign.md's seven documented preflight.Validate cross-checks:
// "the registry's test_id matches the definition's id."
func TestValidate_RegistryTestIDMismatchesDefinitionID_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":         validSuite,
		"happy-path.test.yaml": validDefinition,
		"happy-path.stubs.json": `{
  "schema_version": 1,
  "test_id": "some-other-test",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`,
		"agents/researcher.md": "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !hasDiagnosticCode(report, "test-id-mismatch") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "test-id-mismatch", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a stub registry whose test_id does not match its owning definition's id to be reported")
	}
}

// TestValidate_SettingsOutOfLegalRange_ReportsError covers the last of
// ContractsDesign.md's seven documented preflight.Validate cross-checks:
// "repetitions/pass rate, timeout and turn limit are within legal ranges."
func TestValidate_SettingsOutOfLegalRange_ReportsError(t *testing.T) {
	cases := []struct {
		name       string
		definition string
	}{
		{
			name: "negative repetitions",
			definition: `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
repetitions: -1
`,
		},
		{
			name: "pass_rate above 1.0",
			definition: `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
pass_rate: 1.5
`,
		},
		{
			name: "negative timeout",
			definition: `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
timeout: -15m
`,
		},
		{
			name: "negative turn_limit",
			definition: `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
turn_limit: -1
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				"s.suite.yaml":          validSuite,
				"happy-path.test.yaml":  tc.definition,
				"happy-path.stubs.json": validRegistry,
			})

			_, report := preflight.Validate(preflight.Input{
				SuitePath:   filepath.Join(root, "s.suite.yaml"),
				FixtureRoot: filepath.Join(root, "fixtures"),
				HarnessID:   "claude-code",
			})

			if !hasDiagnosticCode(report, "setting-out-of-range") {
				t.Errorf("expected a diagnostic with code %q, got: %v", "setting-out-of-range", report.Diagnostics)
			}
			if !report.HasErrors() {
				t.Fatalf("expected %s to be reported as out of its legal range", tc.name)
			}
		})
	}
}

// hasDiagnosticCode reports whether any diagnostic in report carries code.
func hasDiagnosticCode(report authoring.Report, code string) bool {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// TestValidate_MultipleUnrelatedErrors_AccumulatedInOnePass is the decisive
// case named in the plan (AC4.2): a document with several unrelated errors
// must report all of them in one pass, not stop at the first.
func TestValidate_MultipleUnrelatedErrors_AccumulatedInOnePass(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: happy-path.test.yaml
  - path: also-missing.test.yaml
`,
		"happy-path.test.yaml": `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: does-not-exist.stubs.json
seed_files:
  - path: Orchestration-{run_id}/Orchestration.md
    ref: also-does-not-exist.md
assertions:
  task_messages:
    - at: 1
      identity: { tool: Task, agent: nobody-in-the-registry }
`,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	// Four independent problems were seeded above: a missing test
	// definition, a missing stub registry, an unresolvable $ref, and an
	// assertion naming an absent collaborator (the last two are only
	// reachable once the registry-missing case is not treated as fatal).
	// A pass that stops at the first problem reports one diagnostic; a
	// pass that accumulates reports several.
	if len(report.Diagnostics) < 2 {
		t.Fatalf("len(Diagnostics) = %d, want several — a single unrelated error must not suppress the rest", len(report.Diagnostics))
	}
}

// TestValidate_DeterministicOrdering proves a report is diff-stable: the
// same input produces diagnostics in the same order across runs, sorted by
// path, then line, then pointer, then code.
func TestValidate_DeterministicOrdering(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: missing-one.test.yaml
  - path: missing-two.test.yaml
`,
	})

	in := preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	}

	_, first := preflight.Validate(in)
	_, second := preflight.Validate(in)

	firstSorted := first.Sorted()
	secondSorted := second.Sorted()

	// Two unrelated missing-test-definition problems were seeded above; a
	// report of only one diagnostic cannot demonstrate ordering at all.
	if len(firstSorted) < 2 {
		t.Fatalf("len(Sorted()) = %d, want at least 2 — ordering needs more than one diagnostic to be meaningful", len(firstSorted))
	}
	if len(firstSorted) != len(secondSorted) {
		t.Fatalf("diagnostic count differs across runs: %d vs %d", len(firstSorted), len(secondSorted))
	}
	for i := range firstSorted {
		if firstSorted[i] != secondSorted[i] {
			t.Errorf("Sorted()[%d] differs across runs: %+v vs %+v", i, firstSorted[i], secondSorted[i])
		}
	}
	for i := 1; i < len(firstSorted); i++ {
		prev, cur := firstSorted[i-1], firstSorted[i]
		if sortKey(prev) > sortKey(cur) {
			t.Errorf("Sorted() out of order at index %d: %+v then %+v", i, prev, cur)
		}
	}
}

// TestValidate_DeterministicOrdering_TiesBrokenByLineAndPointer strengthens
// the ordering guarantee beyond what TestValidate_DeterministicOrdering
// alone can prove. That test's two diagnostics already differ by Path (two
// distinct suite entries), so an implementation that always left
// Line/Pointer at their zero values would still pass it undetected — the
// "diff-stable" claim behind AC4.2 and Sorted()'s documented order (path,
// then line, then pointer, then code) is not actually forced by any
// assertion until two diagnostics sharing the same Path are ordered
// correctly by Line/Pointer alone. This test seeds two unresolvable $refs in
// two different seed_files entries of the *same* test definition, so both
// diagnostics carry the same Path and can only be told apart, and sorted, by
// Line/Pointer.
func TestValidate_DeterministicOrdering_TiesBrokenByLineAndPointer(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": validSuite,
		"happy-path.test.yaml": `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: happy-path.stubs.json
seed_files:
  - path: Orchestration-{run_id}/First.md
    ref: missing-one.md
  - path: Orchestration-{run_id}/Second.md
    ref: missing-two.md
`,
		"happy-path.stubs.json": validRegistry,
	})

	in := preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	}

	_, report := preflight.Validate(in)

	var unresolved []authoring.Diagnostic
	for _, d := range report.Sorted() {
		if d.Code == "unresolvable-ref" {
			unresolved = append(unresolved, d)
		}
	}
	if len(unresolved) != 2 {
		t.Fatalf("len(unresolvable-ref diagnostics) = %d, want 2 — two seed_files entries in the same document each reference a missing file", len(unresolved))
	}
	if unresolved[0].Path != unresolved[1].Path {
		t.Fatalf("expected both diagnostics to share the same Path (same document): got %q and %q", unresolved[0].Path, unresolved[1].Path)
	}
	if unresolved[0].Line == unresolved[1].Line && unresolved[0].Pointer == unresolved[1].Pointer {
		t.Fatal("expected the two diagnostics to differ by Line or Pointer — identical Path, Line and Pointer would make this test unable to prove tie-breaking")
	}
	if sortKey(unresolved[0]) > sortKey(unresolved[1]) {
		t.Errorf("Sorted() ordered the same-Path diagnostics as %+v then %+v, want ascending by Line/Pointer/Code", unresolved[0], unresolved[1])
	}
}

// sortKey renders the fields Sorted() must order by (path, line, pointer,
// code) into one comparable string.
func sortKey(d authoring.Diagnostic) string {
	return fmt.Sprintf("%s\x00%08d\x00%s\x00%s", d.Path, d.Line, d.Pointer, d.Code)
}

// TestValidate_PerformsNoProcessSpawningOrWorkspaceCreation is AC4.7: a
// pre-flight run must leave the filesystem exactly as it found it — no
// sandbox, no new directory, no new file anywhere outside the report it
// returns in memory.
//
// It also asserts the happy-path tree resolves with no errors: a Validate
// that performs no I/O at all (never reads the suite, never writes
// anything) would satisfy "no side effects" trivially, so this test only
// counts as covering AC4.7 once it is combined with a positive assertion
// that Validate actually did its job.
func TestValidate_PerformsNoProcessSpawningOrWorkspaceCreation(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	before := snapshot(t, filepath.Dir(root))

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected the happy-path tree to resolve with no errors, got: %v", report.Diagnostics)
	}

	after := snapshot(t, filepath.Dir(root))
	if len(after) != len(before) {
		t.Fatalf("filesystem entries under the temp root changed during Validate: before=%d after=%d", len(before), len(after))
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("entry %q present before Validate is gone after it", path)
		}
	}
}

func snapshot(t *testing.T, root string) map[string]bool {
	t.Helper()
	entries := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		entries[path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("Walk(%q): %v", root, err)
	}
	return entries
}

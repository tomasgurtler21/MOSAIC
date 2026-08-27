package evaluate_test

// Tests verifying that evaluate.Evaluate threads the test definition's Version
// and NumericID fields through to the returned domain.TestResult.
//
// The design specifies that both fields must be populated at BOTH return sites
// in evaluate.go: the normal return path and the spawn-failed early return path
// (~line 37). This mirrors the identical carry-through pattern already used for
// SubjectVersion, SubjectModel, StubModel, HarnessID, and RetainedSandboxPath.
//
// These tests compile against the current code (Version and NumericID fields
// exist on domain.TestResult after the Stage 2 contract additions) but FAIL
// because evaluate.Evaluate does not yet populate them. They will pass once I2.9
// threads the fields at both return sites.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// versionedDefinition builds a test definition with a specific NumericID and
// Version so threading tests can verify the exact values appear on TestResult.
func versionedDefinition(numericID, version int) domain.TestDefinition {
	def := baseDefinition()
	def.NumericID = numericID
	def.Version = version
	return def
}

// TestEvaluate_NormalPath_CarriesVersionFromDefinition verifies that
// evaluate.Evaluate populates TestResult.Version from
// RunEvidence.Definition.Version on the normal (non-spawn-failed) return path.
// A caller that stores RunReport.TestVersion — which suite.go copies from
// TestResult.Version — must receive the correct content version, not zero.
func TestEvaluate_NormalPath_CarriesVersionFromDefinition(t *testing.T) {
	ev := baseEvidence()
	ev.Definition = versionedDefinition(42, 3)

	got := evaluate.Evaluate(ev)

	if got.Version != 3 {
		t.Errorf("TestResult.Version = %d, want 3 — evaluate.Evaluate must carry Definition.Version through to the returned TestResult on the normal return path", got.Version)
	}
}

// TestEvaluate_NormalPath_CarriesNumericIDFromDefinition verifies that
// evaluate.Evaluate populates TestResult.NumericID from
// RunEvidence.Definition.NumericID on the normal return path. The numeric ID
// is the stable identity carried into the report JSON's per-run test_id field.
func TestEvaluate_NormalPath_CarriesNumericIDFromDefinition(t *testing.T) {
	ev := baseEvidence()
	ev.Definition = versionedDefinition(42, 3)

	got := evaluate.Evaluate(ev)

	if got.NumericID != 42 {
		t.Errorf("TestResult.NumericID = %d, want 42 — evaluate.Evaluate must carry Definition.NumericID through to the returned TestResult on the normal return path", got.NumericID)
	}
}

// TestEvaluate_SpawnFailedPath_CarriesVersionFromDefinition verifies that
// evaluate.Evaluate populates TestResult.Version on the spawn-failed early
// return path as well. The early return at ~line 37 of evaluate.go must carry
// all the same fields as the normal return — omitting Version on this path
// would silently zero-out the field for any run whose harness exited non-zero.
func TestEvaluate_SpawnFailedPath_CarriesVersionFromDefinition(t *testing.T) {
	ev := baseEvidence()
	ev.Definition = versionedDefinition(42, 3)
	ev.SubjectResult = domain.SubjectResult{
		Disposition: domain.DispositionSpawnFailed,
		ExitCode:    1,
		Stderr:      "harness: exec: exit status 1",
	}

	got := evaluate.Evaluate(ev)

	// Confirm we are exercising the spawn-failed path, not the normal path.
	if got.SubjectResult.Disposition != domain.DispositionSpawnFailed {
		t.Fatal("fixture did not produce a spawn-failed result — the test setup is wrong")
	}

	if got.Version != 3 {
		t.Errorf("TestResult.Version = %d, want 3 — evaluate.Evaluate must carry Definition.Version on the spawn-failed early return path, not only on the normal path", got.Version)
	}
}

// TestEvaluate_SpawnFailedPath_CarriesNumericIDFromDefinition verifies that
// evaluate.Evaluate populates TestResult.NumericID on the spawn-failed early
// return path. A spawn-failed run still produces a TestResult that is
// aggregated and reported; its NumericID must be correct.
func TestEvaluate_SpawnFailedPath_CarriesNumericIDFromDefinition(t *testing.T) {
	ev := baseEvidence()
	ev.Definition = versionedDefinition(42, 3)
	ev.SubjectResult = domain.SubjectResult{
		Disposition: domain.DispositionSpawnFailed,
		ExitCode:    1,
		Stderr:      "harness: exec: exit status 1",
	}

	got := evaluate.Evaluate(ev)

	if got.SubjectResult.Disposition != domain.DispositionSpawnFailed {
		t.Fatal("fixture did not produce a spawn-failed result — the test setup is wrong")
	}

	if got.NumericID != 42 {
		t.Errorf("TestResult.NumericID = %d, want 42 — evaluate.Evaluate must carry Definition.NumericID on the spawn-failed early return path, not only on the normal path", got.NumericID)
	}
}

// TestEvaluate_ZeroNumericIDDefinition_CarriesZero verifies the zero-value
// contract: a definition with NumericID=0 (constructed directly, bypassing the
// parser's positive-integer validation, as test helpers do) produces a
// TestResult with NumericID=0. The threading must not substitute a default.
func TestEvaluate_ZeroNumericIDDefinition_CarriesZero(t *testing.T) {
	ev := baseEvidence()
	// baseDefinition() leaves NumericID at 0 (parser-bypassing helper).
	ev.Definition = baseDefinition()

	got := evaluate.Evaluate(ev)

	if got.NumericID != 0 {
		t.Errorf("TestResult.NumericID = %d, want 0 — a definition without a NumericID must produce a zero NumericID on the result, not a substitute", got.NumericID)
	}
}

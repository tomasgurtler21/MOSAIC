package main

// Tests for the importcheck boundary guard.
//
// TestImportBoundaries_NoViolations runs runChecks against the actual module
// source tree and asserts there are no violations — a build-time regression
// guard.
//
// The remaining tests each drive runChecks against a fixture directory under
// testdata/: one conforming graph that must pass, and one violating graph
// per rule class in the checker's doc comment. A checker that accepts
// everything is worse than no checker, so the failing cases are the point.

import (
	"strings"
	"testing"
)

// TestImportBoundaries_NoViolations verifies that all import-boundary rules
// pass with zero violations against the current source tree.
func TestImportBoundaries_NoViolations(t *testing.T) {
	moduleRoot := "../.."

	violations, errs := runChecks(moduleRoot)

	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("tool error: %s", e)
		}
		t.Fatal("import-check tool encountered errors; fix the errors above")
	}

	if len(violations) > 0 {
		t.Errorf("import boundary violations (%d):", len(violations))
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
		t.Fatal("fix the imports above to restore the dependency direction")
	}
}

// TestConformingFixture_NoViolations verifies that a fixture import graph
// respecting every rule passes cleanly.
func TestConformingFixture_NoViolations(t *testing.T) {
	violations, errs := runChecks("../../testdata/conforming")

	if len(errs) > 0 {
		t.Fatalf("unexpected tool errors: %v", errs)
	}
	if len(violations) > 0 {
		t.Fatalf("expected no violations for the conforming fixture, got: %v", violations)
	}
}

// violationCase describes one rule-class violation fixture.
type violationCase struct {
	name        string
	fixtureDir  string
	wantSubstrs []string // every one of these must appear in some violation message
}

// TestViolationFixtures_EachRuleClassReported verifies that each rule class
// documented in main.go is violated by its own fixture graph and reported —
// distinct fixtures per class prevent one overly permissive rule from
// silently passing the others.
func TestViolationFixtures_EachRuleClassReported(t *testing.T) {
	cases := []violationCase{
		{
			name:        "domain imports another module package",
			fixtureDir:  "../../testdata/violation_domain",
			wantSubstrs: []string{"internal/domain/bad.go", "domain is the base layer"},
		},
		{
			name:        "pure core imports an adapter",
			fixtureDir:  "../../testdata/violation_purecore_layer",
			wantSubstrs: []string{"internal/intercept/bad.go", "pure cores may import only domain"},
		},
		{
			name:        "pure core imports a banned I/O package",
			fixtureDir:  "../../testdata/violation_purecore_io",
			wantSubstrs: []string{"internal/stubmatch/bad.go", "free of I/O packages"},
		},
		{
			name:        "pure core reads the ambient clock",
			fixtureDir:  "../../testdata/violation_purecore_clock",
			wantSubstrs: []string{"internal/evaluate/bad.go", "calls time.Now"},
		},
		{
			name:        "adapter imports a use case",
			fixtureDir:  "../../testdata/violation_adapter",
			wantSubstrs: []string{"internal/workspace/bad.go", "adapters and stores may import only domain"},
		},
		{
			name:        "use case imports a frontend",
			fixtureDir:  "../../testdata/violation_usecase",
			wantSubstrs: []string{"internal/runner/bad.go", "use cases must not import a frontend"},
		},
		{
			name:        "frontend imports the sibling frontend",
			fixtureDir:  "../../testdata/violation_frontend",
			wantSubstrs: []string{"internal/cli/bad.go", "the two frontends must not import each other"},
		},
		{
			name:        "non-harness package imports a concrete harness adapter",
			fixtureDir:  "../../testdata/violation_harness_isolation",
			wantSubstrs: []string{"internal/suite/bad.go", "concrete harness adapter"},
		},
		{
			name:        "unclassified package",
			fixtureDir:  "../../testdata/violation_unknown_package",
			wantSubstrs: []string{"internal/mystery", "not classified"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, errs := runChecks(tc.fixtureDir)
			if len(errs) > 0 {
				t.Fatalf("unexpected tool errors: %v", errs)
			}
			if len(violations) == 0 {
				t.Fatalf("expected at least one violation for fixture %q, got none", tc.fixtureDir)
			}

			joined := strings.Join(violations, "\n")
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(joined, want) {
					t.Errorf("expected a violation containing %q, got:\n%s", want, joined)
				}
			}
		})
	}
}

// TestClassify_LaunchIsAdapter_InterceptorIsUseCase verifies the two
// classifications the plan calls out explicitly: launch performs process
// control and is an adapter even though it names no harness; interceptor is
// a use case.
func TestClassify_LaunchIsAdapter_InterceptorIsUseCase(t *testing.T) {
	if got := classify("internal/launch"); got != layerAdapter {
		t.Errorf("classify(internal/launch) = %q, want %q", got, layerAdapter)
	}
	if got := classify("internal/interceptor"); got != layerUseCase {
		t.Errorf("classify(internal/interceptor) = %q, want %q", got, layerUseCase)
	}
}

// TestClassify_UnknownPackage verifies an unclassified package path yields
// layerUnknown rather than defaulting to something permitted.
func TestClassify_UnknownPackage(t *testing.T) {
	if got := classify("internal/mystery"); got != layerUnknown {
		t.Errorf("classify(internal/mystery) = %q, want %q", got, layerUnknown)
	}
}

// ---------------------------------------------------------------------------
// Composition-root exemption (Stage 17)
//
// The gate has asserted the harness-isolation rule since the adapter stage;
// this stage is where the exemption for the binary composition root
// (cmd/mosaic-agent-test) becomes real. The exemption must be narrow: it
// covers exactly that one package, never every package beneath cmd/, and it
// does not exempt that package from being classified at all — an
// unclassified package is still a violation regardless of where it sits.
// ---------------------------------------------------------------------------

// TestCompositionRoot_ImportingConcreteAdapter_PassesTheGate verifies that
// cmd/mosaic-agent-test — and only that package — may import a concrete
// harness adapter package directly.
func TestCompositionRoot_ImportingConcreteAdapter_PassesTheGate(t *testing.T) {
	violations, errs := runChecks("../../testdata/composition_root_exempt")

	if len(errs) > 0 {
		t.Fatalf("unexpected tool errors: %v", errs)
	}
	if len(violations) > 0 {
		t.Fatalf("expected no violations for the composition root importing a concrete adapter, got: %v", violations)
	}
}

// TestNonCompositionRootCmdPackage_ImportingConcreteAdapter_FailsTheGate
// verifies the exemption is narrow: a cmd/ package that is not the module's
// binary composition root gets no benefit from cmd/mosaic-agent-test's
// exemption and fails harness isolation exactly as an internal/ package
// would.
func TestNonCompositionRootCmdPackage_ImportingConcreteAdapter_FailsTheGate(t *testing.T) {
	violations, errs := runChecks("../../testdata/violation_cmd_not_composition_root")

	if len(errs) > 0 {
		t.Fatalf("unexpected tool errors: %v", errs)
	}
	if len(violations) == 0 {
		t.Fatal("expected a harness-isolation violation for a non-composition-root cmd/ package, got none")
	}

	joined := strings.Join(violations, "\n")
	for _, want := range []string{"cmd/othertool", "concrete harness adapter"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a violation containing %q, got:\n%s", want, joined)
		}
	}
}

// TestCmdSubpackage_Unclassified_FailsTheGate verifies the exemption covers
// exactly the composition-root package path and nothing that merely shares
// its prefix: a package beneath cmd/mosaic-agent-test/ that is not the
// composition root itself must still be classified, and an unclassified
// package is a violation just as it would be under internal/.
func TestCmdSubpackage_Unclassified_FailsTheGate(t *testing.T) {
	violations, errs := runChecks("../../testdata/violation_unclassified_cmd")

	if len(errs) > 0 {
		t.Fatalf("unexpected tool errors: %v", errs)
	}
	if len(violations) == 0 {
		t.Fatal("expected an unclassified-package violation for cmd/mosaic-agent-test/helper, got none")
	}

	joined := strings.Join(violations, "\n")
	for _, want := range []string{"cmd/mosaic-agent-test/helper", "not classified"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a violation containing %q, got:\n%s", want, joined)
		}
	}
}

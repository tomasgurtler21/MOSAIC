package opencode_test

// Tests for the adapter's environment check honesty, per Stage 3's
// per-adapter obligation table: opencode "Reports honestly: declares no
// interpreter (InterpreterApplicable: false), no neutralization". This
// harness runs no interpreter of its own, so claiming one would be exactly
// the dishonesty the port's conformance suite exists to catch.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

// TestCheckEnvironment_DeclaresNoInterpreter asserts the adapter reports
// InterpreterApplicable false and an empty InterpreterCmd: it must not
// invent a resolved interpreter it never runs.
func TestCheckEnvironment_DeclaresNoInterpreter(t *testing.T) {
	a := opencode.New(opencode.Options{})

	report, err := a.CheckEnvironment(context.Background())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if report.InterpreterApplicable {
		t.Errorf("CheckEnvironment: InterpreterApplicable = true, want false — this harness runs no interpreter")
	}
	if report.InterpreterCmd != "" {
		t.Errorf("CheckEnvironment: InterpreterCmd = %q, want empty — declaring one would be a resolved value this adapter does not actually use", report.InterpreterCmd)
	}
}

// TestCheckEnvironment_CompetingOutsideScopeRewriterFails asserts that a
// non-sandbox scope reporting RewritesInput without neutralization fails the
// environment check with ProblemCompetingRewriter, exercising the loop over
// InspectScopes findings CheckEnvironment's glue performs. claudecode's
// equivalent path is directly tested
// (TestCheckEnvironment_CompetingOutsideScopeRewriterFails); this closes the
// same gap for opencode so the two adapters' coverage stays symmetric, even
// though the underlying rewriter detection itself is already proven by
// scopes_test.go.
func TestCheckEnvironment_CompetingOutsideScopeRewriterFails(t *testing.T) {
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user-config": readScopeFixture(t, "user-config-with-plugin.json"),
		},
	}

	a := opencode.New(opencode.Options{ScopeProbe: probe})

	report, err := a.CheckEnvironment(context.Background())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if report.OK() {
		t.Fatalf("CheckEnvironment: OK() = true, want false when a non-sandbox scope has a competing input-rewriting hook")
	}

	var found bool
	for _, p := range report.Problems {
		if p.Kind != domain.ProblemCompetingRewriter {
			continue
		}
		found = true
		if p.Detail == "" {
			t.Errorf("CheckEnvironment: competing-rewriter problem has empty Detail; must name what was found")
		}
	}
	if !found {
		t.Errorf("CheckEnvironment: Problems = %+v, want a ProblemCompetingRewriter entry", report.Problems)
	}
}

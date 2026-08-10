package report_test

// Stage 7 (Sandbox Retention), T7.3 / AC7.3: the report prints the retained
// sandbox's path. report.RunReport.RetainedSandboxPath is the model field
// ContractsDesign.md's "report model additions" section adds; these tests
// pin the rendering obligation both text_test.go's text-rendering table and
// AC7.3 itself describe: "Printed as an openable path whenever
// RetainedSandboxPath is non-empty."
//
// No golden file is used here: the exact line format and the JSON field
// name are an implementation choice this stage's tests deliberately leave
// open, per ContractsDesign.md's additive-only wire-shape rule — only that
// the path itself appears in both renderings, verbatim, is pinned.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// retainedPathResult builds a minimal, single-test, single-run Result whose
// run carries RetainedSandboxPath, so a test can assert on its presence
// (or, with path left empty, its absence) without depending on the full
// fixtureFullResult shape.
func retainedPathResult(path string) report.Result {
	return report.Build("retention-suite", time.Time{}, time.Time{}, []report.TestReport{
		{
			TestID: "unspawnable-subject",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestID:                "unspawnable-subject",
				Verdict:               domain.VerdictFail,
				Reasons:               []domain.FailureReason{domain.ReasonInfrastructure},
				InfrastructureFailure: true,
			},
			Runs: []report.RunReport{
				{
					Key:                 domain.RunKey{RunID: "unspawnable-subject-1", TestID: "unspawnable-subject", RunNumber: 1},
					Verdict:             domain.VerdictFail,
					Reasons:             []domain.FailureReason{domain.ReasonInfrastructure},
					RetainedSandboxPath: path,
				},
			},
		},
	})
}

func TestRenderText_PrintsTheRetainedSandboxPath_WhenSet(t *testing.T) {
	const path = `C:\workspace-root\unspawnable-subject-1-unspawnable-subject-1`
	out, err := renderText(t, retainedPathResult(path))
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	if !strings.Contains(out, path) {
		t.Errorf("terminal report does not mention the retained sandbox path %q; a retention feature whose path a user has to guess is not a feature.\n--- got ---\n%s", path, out)
	}
}

func TestRenderText_OmitsARetainedSandboxLine_WhenNotSet(t *testing.T) {
	out, err := renderText(t, retainedPathResult(""))
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Guards against a rendering that always writes a "retained sandbox"
	// line, empty or not — a run that retained nothing must read as such.
	if strings.Contains(strings.ToLower(out), "retained sandbox") {
		t.Errorf("terminal report mentions a retained sandbox for a run that retained none.\n--- got ---\n%s", out)
	}
}

func TestRenderJSON_IncludesTheRetainedSandboxPath_WhenSet(t *testing.T) {
	const path = `/var/agent-test/workspace-root/unspawnable-subject-1-unspawnable-subject-1`
	out, err := renderJSON(t, retainedPathResult(path))
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	if !strings.Contains(string(out), path) {
		t.Errorf("machine-readable report does not include the retained sandbox path %q.\n--- got ---\n%s", path, out)
	}
}

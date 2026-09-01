package report_test

// Stage 8 (Harness Credentials and Subject Diagnostics), T8.4 / AC8.6: a
// non-zero subject exit surfaces its exit code and stderr in the report.
// report.RunReport.Subject / report.SubjectFailure are the model fields
// ContractsDesign.md's "report model additions" section adds; these tests
// pin the rendering obligation both renderings must honour and the
// dedicated outcome class an infrastructure fault must exhibit, following
// the same no-golden-file approach retention_test.go established for
// RetainedSandboxPath: only that the exit code and stderr text appear in
// both renderings, verbatim, is pinned.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// subjectFailureResult builds a minimal, single-test, single-run Result
// whose run carries a Subject failure, so a test can assert on its
// presence (or, with a zero-valued SubjectFailure, its absence) without
// depending on the full fixtureFullResult shape.
func subjectFailureResult(subject report.SubjectFailure) report.Result {
	return report.Build("subject-failure-suite", time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "subject-exits-nonzero",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:  "subject-exits-nonzero",
				Verdict: domain.VerdictFail,
				Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
			},
			Runs: []report.RunReport{
				{
					Key:     domain.RunKey{RunID: "subject-exits-nonzero-1", TestName: "subject-exits-nonzero", RunNumber: 1},
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
					Subject: subject,
				},
			},
		},
	}, "")
}

func TestRenderText_PrintsSubjectExitCodeAndStderr_OnNonZeroExit(t *testing.T) {
	const stderrText = "Not logged in. Run `claude login` to authenticate."
	out, err := renderText(t, subjectFailureResult(report.SubjectFailure{ExitCode: 1, Stderr: stderrText}))
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	if !strings.Contains(out, strconv.Itoa(1)) {
		t.Errorf("terminal report does not mention the subject's exit code 1.\n--- got ---\n%s", out)
	}
	if !strings.Contains(out, stderrText) {
		t.Errorf("terminal report does not mention the subject's stderr %q; a non-zero exit must be diagnosable without reproducing outside the tool.\n--- got ---\n%s", stderrText, out)
	}
}

func TestRenderText_OmitsSubjectFailure_WhenExitCodeZero(t *testing.T) {
	out, err := renderText(t, subjectFailureResult(report.SubjectFailure{}))
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Guards against a rendering that always writes a subject-failure line,
	// zero exit code or not — a run whose subject exited cleanly must not
	// read as though it failed.
	if strings.Contains(strings.ToLower(out), "stderr") {
		t.Errorf("terminal report mentions subject stderr for a run whose subject exited zero.\n--- got ---\n%s", out)
	}
}

func TestRenderJSON_IncludesSubjectStderr_OnNonZeroExit(t *testing.T) {
	const stderrText = "Not logged in. Run `claude login` to authenticate."
	out, err := renderJSON(t, subjectFailureResult(report.SubjectFailure{ExitCode: 1, Stderr: stderrText}))
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	if !strings.Contains(string(out), stderrText) {
		t.Errorf("machine-readable report does not include the subject's stderr %q.\n--- got ---\n%s", stderrText, out)
	}
}

func TestRenderJSON_OmitsSubjectStderr_WhenExitCodeZero(t *testing.T) {
	out, err := renderJSON(t, subjectFailureResult(report.SubjectFailure{}))
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	if strings.Contains(strings.ToLower(string(out)), "stderr") {
		t.Errorf("machine-readable report mentions subject stderr for a run whose subject exited zero.\n--- got ---\n%s", out)
	}
}

// TestClassify_NonZeroSubjectExit_IncludesClassSubjectFailure pins
// report.Classify's single derivation of the dedicated outcome class: an
// infrastructure fault must not read like a subject regression, and here
// the reverse must also hold — a genuine subject failure must be visibly
// distinguishable as its own class.
func TestClassify_NonZeroSubjectExit_IncludesClassSubjectFailure(t *testing.T) {
	run := report.RunReport{
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
		Subject: report.SubjectFailure{ExitCode: 1, Stderr: "Not logged in."},
	}

	classes := report.Classify(run)

	found := false
	for _, c := range classes {
		if c == report.ClassSubjectFailure {
			found = true
		}
	}
	if !found {
		t.Errorf("Classify(%+v) = %v, want it to include ClassSubjectFailure for a non-zero subject exit", run, classes)
	}
}

func TestClassify_ZeroSubjectExit_NeverIncludesClassSubjectFailure(t *testing.T) {
	run := report.RunReport{Verdict: domain.VerdictPass}

	classes := report.Classify(run)

	for _, c := range classes {
		if c == report.ClassSubjectFailure {
			t.Errorf("Classify(%+v) = %v, want it never to include ClassSubjectFailure for a subject that exited zero", run, classes)
		}
	}
}

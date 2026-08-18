package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"mosaic-agent-test/internal/domain"
)

// renderText writes the human-readable terminal report: a header, one line
// per test naming its verdict and the outcome classes it exhibits (so an
// infrastructure fault never reads like a subject regression), one line per
// failed assertion, and a summary.
func renderText(w io.Writer, r Result) error {
	if err := writeHeader(w, r); err != nil {
		return err
	}
	if len(r.Tests) > 0 {
		for _, t := range r.Tests {
			if err := writeTestLine(w, t); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return writeSummary(w, r)
}

func writeHeader(w io.Writer, r Result) error {
	if _, err := fmt.Fprintf(w, "Suite: %s\n", r.SuiteID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Started: %s\n", r.StartedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Finished: %s\n", r.FinishedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeTestLine(w io.Writer, t TestReport) error {
	achievedPct := "n/a"
	if t.Aggregate.Counted > 0 {
		achievedPct = fmt.Sprintf("%.0f%%", t.Aggregate.PassRate*100)
	}
	requiredPct := fmt.Sprintf("%.0f%%", t.Aggregate.RequiredPassRate*100)
	stats := fmt.Sprintf("%d/%d passed (%s, required %s)",
		t.Aggregate.Passed, t.Aggregate.Counted, achievedPct, requiredPct)
	line := fmt.Sprintf("%s: %s %s", t.TestID, t.Aggregate.Verdict, stats)
	if classes := testClasses(t); len(classes) > 0 {
		names := make([]string, len(classes))
		for i, c := range classes {
			names[i] = string(c)
		}
		line += fmt.Sprintf(" [%s]", strings.Join(names, ","))
	}
	if t.Aggregate.InfrastructureFailure {
		line += " (infrastructure failure)"
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}
	for _, run := range t.Runs {
		for _, a := range run.Assertions {
			switch a.Outcome {
			case domain.AssertionFail:
				detail := ""
				if a.Detail != "" {
					detail = fmt.Sprintf(" (%s)", a.Detail)
				}
				if _, err := fmt.Fprintf(w, "  - %s: expected %q, got %q%s\n", a.Class, a.Expected, a.Actual, detail); err != nil {
					return err
				}
			case domain.AssertionNotEvaluated:
				if _, err := fmt.Fprintf(w, "  ~ %s: not_evaluated (%s)\n", a.Class, a.Detail); err != nil {
					return err
				}
			}
		}
		for _, c := range run.Conditions {
			if _, err := fmt.Fprintf(w, "  ! %s: %s\n", c.Kind, c.Detail); err != nil {
				return err
			}
		}
		if run.RetainedSandboxPath != "" {
			if _, err := fmt.Fprintf(w, "  retained sandbox: %s\n", run.RetainedSandboxPath); err != nil {
				return err
			}
		}
		if run.Subject.ExitCode != 0 {
			stderr := run.Subject.Stderr
			const maxStderrDisplay = 500
			if len(stderr) > maxStderrDisplay {
				stderr = stderr[:maxStderrDisplay] + "..."
			}
			if _, err := fmt.Fprintf(w, "  subject exited %d: %s\n", run.Subject.ExitCode, stderr); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  subject version: %s\n", subjectVersionOrUnknown(run.SubjectVersion)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  subject model: %s\n", subjectVersionOrUnknown(run.SubjectModel)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  stub model: %s\n", subjectVersionOrUnknown(run.StubModel)); err != nil {
			return err
		}
	}
	return nil
}

func writeSummary(w io.Writer, r Result) error {
	totals := formatTotals(r.Counts)
	var err error
	if totals == "" {
		_, err = fmt.Fprintln(w, "Totals:")
	} else {
		_, err = fmt.Fprintf(w, "Totals: %s\n", totals)
	}
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Infrastructure failures: %d\n", r.InfrastructureFailures); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Total cost: $%.2f (%s)\n", r.TotalCost.TotalUSD, r.TotalCost.Attribution)
	return err
}

// formatTotals renders r's verdict counts as "KEY=n" entries, sorted
// alphabetically so the line is diff-stable across runs.
func formatTotals(counts map[domain.Verdict]int) string {
	keys := make([]string, 0, len(counts))
	for v := range counts {
		keys = append(keys, string(v))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[domain.Verdict(k)]))
	}
	return strings.Join(parts, " ")
}

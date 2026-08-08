package tui

// equivalence_test.go verifies AC16.5: driving both frontends from one
// scripted event sequence and one result model, they report the same
// verdicts, the same counts and the same totals. This is the executable
// form of the claim that the CLI and the TUI cannot disagree — both fold
// the identical progress-event stream (internal/domain.ProgressEvent)
// rather than maintaining parallel logic, following the same test shape as
// Tools/Deployment/internal/tui/equivalence_test.go.
//
// Import-boundary note: internal/cli is imported here only because this is
// a _test.go file, which tools/importcheck exempts from the frontend
// import-boundary gate; internal/tui's production code (app.go) imports no
// sibling frontend.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// TestEquivalence_SameEventSequence_SameVerdictsCountsAndTotals drives the
// CLI's FormatEvent and the TUI's Fold from one identical scripted event
// sequence and asserts both report the same verdict counts and the same
// total cost — the numbers a user compares when deciding whether the two
// frontends agree about a run.
func TestEquivalence_SameEventSequence_SameVerdictsCountsAndTotals(t *testing.T) {
	seq := scriptedSuite{
		suiteID: "suite-under-test",
		tests: []scriptedTest{
			{testID: "test-a", verdict: domain.VerdictPass, duration: time.Second, cost: domain.CostReport{TotalUSD: 0.10, Attribution: domain.AttributionAttributed}},
			{testID: "test-b", verdict: domain.VerdictFail, duration: 2 * time.Second, cost: domain.CostReport{TotalUSD: 0.25, Attribution: domain.AttributionAttributed}, failed: []string{"assertion"}},
			{testID: "test-c", verdict: domain.VerdictTimeout, duration: 3 * time.Second},
		},
	}.events()

	// The CLI's rendering: one FormatEvent line per non-empty event,
	// exactly what the non-interactive frontend streams as it runs.
	var cliLines []string
	for _, ev := range seq {
		if line := cli.FormatEvent(ev); line != "" {
			cliLines = append(cliLines, line)
		}
	}

	// The TUI's folding: the same events, folded into a Model.
	m := foldAll(t, newFoldModel(), seq)

	final := seq[len(seq)-1]
	if final.Kind != domain.ProgressSuiteFinished {
		t.Fatalf("test setup: last scripted event is %q, want %q", final.Kind, domain.ProgressSuiteFinished)
	}

	// The TUI's structured counts and total cost must equal what the
	// terminal event itself carries.
	tuiCounts := m.Counts()
	for verdict, want := range final.Counts {
		if got := tuiCounts[verdict]; got != want {
			t.Errorf("TUI Counts()[%q] = %d, want %d", verdict, got, want)
		}
	}
	if got := m.TotalCost().TotalUSD; got != final.TotalCost.TotalUSD {
		t.Errorf("TUI TotalCost().TotalUSD = %v, want %v", got, final.TotalCost.TotalUSD)
	}

	// The CLI's rendered suite-finished line must mention every verdict's
	// count from the same terminal event — the same numbers asserted above
	// against the TUI's structured accessors. Neither frontend may derive a
	// different number from the identical event.
	if len(cliLines) == 0 {
		t.Fatalf("CLI rendered no lines from the scripted sequence")
	}
	last := cliLines[len(cliLines)-1]
	for verdict, count := range final.Counts {
		wantCount := fmt.Sprintf("%d", count)
		if !strings.Contains(last, string(verdict)) || !strings.Contains(last, wantCount) {
			t.Errorf("CLI's suite-finished line %q does not mention %d %s test(s), which the TUI reports via Counts()[%q] = %d",
				last, count, verdict, verdict, tuiCounts[verdict])
		}
	}
}

// TestEquivalence_BothConsumeIdenticalProgressSinkInterface is a structural
// check that both frontends are built to consume domain.ProgressSink —
// neither constructs its own progress channel. If either frontend's sink
// adapter drifted from the shared interface, this file fails to compile.
func TestEquivalence_BothConsumeIdenticalProgressSinkInterface(t *testing.T) {
	var _ domain.ProgressSink = cli.NewLineSink(nil)
	var _ domain.ProgressSink = NewProgramSink(nil)
}

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
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
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

// ---------------------------------------------------------------------------
// Model selection equivalence: CLI-TUI cross-frontend pinning
// ---------------------------------------------------------------------------

// TestEquivalence_ModelSelection_SubjectModel_CLIAndTUIArriveAtSameValue drives
// the CLI with --subject-model X and the TUI to select X on the subject phase,
// and asserts both arrive at identical preflight.Input.Overrides.SubjectModel
// values. This is the cross-frontend pinning the design calls out: "the existing
// frontend-equivalence tests are the place that is pinned" for
// preflight.Input.Overrides.SubjectModel, preventing frontend drift.
func TestEquivalence_ModelSelection_SubjectModel_CLIAndTUIArriveAtSameValue(t *testing.T) {
	catalog := twoModelCatalog()
	const harnessID = "claude-code"
	// Second model in the catalog ("claude-sonnet-4-5"), reached on the TUI via
	// a single Down key on the subject phase (cursor starts at position 0).
	wantModel := catalog[0].IDs[1]

	// CLI path: validate with --harness harnessID --subject-model wantModel and
	// capture whatever preflight.Input.Overrides.SubjectModel the flag parser
	// produced. "validate" is used so no SuiteFactory is needed.
	var cliCaptured preflight.Input
	cliOptions := cli.Options{
		Preflight: func(in preflight.Input) (preflight.Plan, authoring.Report) {
			cliCaptured = in
			return preflight.Plan{}, authoring.Report{}
		},
		Harnesses:      twoHarnessCatalog(),
		Models:         twoModelCatalog(),
		DefaultHarness: harnessID,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	}
	cli.Execute(context.Background(),
		[]string{"validate", "suite.yaml", "--harness", harnessID, "--subject-model", wantModel},
		cliOptions)

	// TUI path: select wantModel on the subject phase and capture the preflight
	// input produced when the suite starts.
	var tuiCaptured preflight.Input
	runner := newFakeSuiteRunner()
	tuiOptions := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	tuiOptions.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		tuiCaptured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(tuiOptions)
	m = advanceToModelSelect(t, m)                  // harness-select → model-select (subject phase)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))   // move cursor to second model (wantModel)
	m, _ = safeUpdate(t, m, keyMsg("\r"))            // confirm subject model → stub phase
	m, _ = safeUpdate(t, m, keyMsg("\r"))            // stub phase: accept "same as subject" → suite-select
	m, cmd := startSuiteFromSuiteSelect(t, m)        // suite-select: navigate settings and start the suite
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if cliCaptured.Overrides.SubjectModel != tuiCaptured.Overrides.SubjectModel {
		t.Errorf("SubjectModel diverged between frontends: CLI=%q TUI=%q (both must arrive at %q — the same choice must reach the same field regardless of which frontend collected it)",
			cliCaptured.Overrides.SubjectModel, tuiCaptured.Overrides.SubjectModel, wantModel)
	}
}

// TestEquivalence_ModelSelection_StubModel_CLIAndTUIArriveAtSameValue drives
// the CLI with --stub-model Y and the TUI to select Y on the stub phase, and
// asserts both arrive at identical preflight.Input.Overrides.StubModel values.
// Companion to TestEquivalence_ModelSelection_SubjectModel_CLIAndTUIArriveAtSameValue:
// covers the second field in the pair the design requires to be equivalent across
// both frontends.
func TestEquivalence_ModelSelection_StubModel_CLIAndTUIArriveAtSameValue(t *testing.T) {
	catalog := twoModelCatalog()
	const harnessID = "claude-code"
	// First catalog model ("claude-opus-4-5"), reached on the TUI stub phase
	// by pressing Down once (past the "same as subject" entry at position 0).
	wantStub := catalog[0].IDs[0]

	// CLI path: validate with --harness harnessID --stub-model wantStub.
	var cliCaptured preflight.Input
	cliOptions := cli.Options{
		Preflight: func(in preflight.Input) (preflight.Plan, authoring.Report) {
			cliCaptured = in
			return preflight.Plan{}, authoring.Report{}
		},
		Harnesses:      twoHarnessCatalog(),
		Models:         twoModelCatalog(),
		DefaultHarness: harnessID,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	}
	cli.Execute(context.Background(),
		[]string{"validate", "suite.yaml", "--harness", harnessID, "--stub-model", wantStub},
		cliOptions)

	// TUI path: accept the default subject, then select wantStub on the stub phase.
	var tuiCaptured preflight.Input
	runner := newFakeSuiteRunner()
	tuiOptions := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	tuiOptions.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		tuiCaptured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(tuiOptions)
	m = advanceToModelSelect(t, m)                  // harness-select → model-select (subject phase)
	m, _ = safeUpdate(t, m, keyMsg("\r"))            // subject phase: accept default (first model, cursor 0)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))   // stub phase: move past "same as subject" to wantStub
	m, _ = safeUpdate(t, m, keyMsg("\r"))            // confirm stub model → suite-select
	m, cmd := startSuiteFromSuiteSelect(t, m)        // suite-select: navigate settings and start the suite
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if cliCaptured.Overrides.StubModel != tuiCaptured.Overrides.StubModel {
		t.Errorf("StubModel diverged between frontends: CLI=%q TUI=%q (both must arrive at %q — the same choice must reach the same field regardless of which frontend collected it)",
			cliCaptured.Overrides.StubModel, tuiCaptured.Overrides.StubModel, wantStub)
	}
}

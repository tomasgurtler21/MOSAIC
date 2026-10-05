package app_test

// Runner-shaped OrchestrationLogs trees must be analysed exactly like native
// ones: per-agent totals and priced cost for every invocation that carries
// usage, each invocation counted once, and no data-quality findings that the
// equivalent native tree would not get. Trees are built in temporary
// directories and read through the real scanner and reader.

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/logread"
	"mosaic-log-analyzer/internal/logscan"
)

func runnerShapeSpecs() map[string]shapeSpec {
	return map[string]shapeSpec{
		"claude-code": claudeCodeShape(),
		"ghcp-cli":    ghcpCLIShape(),
		"opencode":    openCodeShape(),
	}
}

// analyzeShapePath analyses path (a logs root or run folder) with the real
// scanner and reader, pricing the spec's model with a flat rate card.
func analyzeShapePath(t *testing.T, spec shapeSpec, path string) domain.Report {
	t.Helper()
	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(domain.ModelID(spec.model))})
	svc := app.New(app.Deps{
		Source:      logscan.NewScanner(),
		Reader:      logread.NewReader(),
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     t.TempDir(),
	})
	result, err := svc.Analyze(context.Background(), app.Request{ExplicitPath: path})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}
	return result.Report
}

// costNanos prices tokens with flatPricing's rate card, independently of the
// code under test: nanodollars per token are 3000 / 300 / 3750 / 15000 for
// input / cache read / cache creation / output.
func costNanos(u [4]int64) int64 {
	return u[0]*3000 + u[1]*300 + u[2]*3750 + u[3]*15000
}

func tokenValues(u domain.TokenUsage) [4]int64 {
	return [4]int64{u.Input.ValueOr(0), u.CacheRead.ValueOr(0), u.CacheCreation.ValueOr(0), u.Output.ValueOr(0)}
}

func assertTotals(t *testing.T, label string, got domain.Totals, want [4]int64) {
	t.Helper()
	if v := tokenValues(got.Tokens); v != want {
		t.Errorf("%s tokens = %v, want %v", label, v, want)
	}
	if got.Money.Total.State != domain.MoneyKnown || got.Money.Total.Amount.Nanos() != costNanos(want) {
		t.Errorf("%s cost = %+v, want known %d nanodollars", label, got.Money.Total, costNanos(want))
	}
}

func findAgent(run domain.RunReport, id string) (domain.ActorReport, bool) {
	for _, a := range run.Agents {
		if string(a.Actor.Instance) == id {
			return a, true
		}
	}
	return domain.ActorReport{}, false
}

func sumArrays(parts ...[4]int64) [4]int64 {
	var s [4]int64
	for _, p := range parts {
		for i := range s {
			s[i] += p[i]
		}
	}
	return s
}

func TestRunnerShape_ReportsPerAgentTotalsAndPricedCost(t *testing.T) {
	// Every invocation that carries usage must get its own totals and a
	// priced cost, whichever harness usage shape the Runner-shaped run has.
	for name, spec := range runnerShapeSpecs() {
		t.Run(name, func(t *testing.T) {
			report := analyzeShapePath(t, spec, writeShapeTree(t, spec, true))

			run, ok := report.FindRun(shapeRunID)
			if !ok || len(report.Runs) != 1 {
				t.Fatalf("want exactly the run %s, got %d runs", shapeRunID, len(report.Runs))
			}
			if len(run.Agents) != len(spec.agents) {
				t.Fatalf("len(Agents) = %d, want %d", len(run.Agents), len(spec.agents))
			}
			parts := [][4]int64{spec.wantOrch}
			for _, want := range spec.agents {
				agent, found := findAgent(run, want.id)
				if !found {
					t.Fatalf("agent %s missing from report", want.id)
				}
				assertTotals(t, want.id, agent.Totals, want.want)
				parts = append(parts, want.want)
			}
			assertTotals(t, "orchestrator", run.Orchestrator.Totals, spec.wantOrch)
			assertTotals(t, "run", run.Totals, sumArrays(parts...))
			if report.HasUnpricedModels() {
				t.Errorf("UnpricedModels = %v, want none", report.UnpricedModels)
			}
		})
	}
}

func TestRunnerShape_CostIsCompleteForEveryActor(t *testing.T) {
	// Cost is complete when every contributing invocation is priced. User
	// turns carry no model or usage by the log format and must not make an
	// otherwise fully priced run incomplete.
	for name, spec := range runnerShapeSpecs() {
		t.Run(name, func(t *testing.T) {
			report := analyzeShapePath(t, spec, writeShapeTree(t, spec, true))

			run, ok := report.FindRun(shapeRunID)
			if !ok {
				t.Fatalf("run %s missing", shapeRunID)
			}
			if !run.Orchestrator.Totals.Money.Complete {
				t.Error("orchestrator cost is not complete, want complete")
			}
			for _, agent := range run.Agents {
				if !agent.Totals.Money.Complete {
					t.Errorf("%s cost is not complete, want complete", agent.Actor.Label())
				}
			}
			if !run.Totals.Money.Complete {
				t.Error("run cost is not complete, want complete")
			}
		})
	}
}

func TestRunnerShape_MultipleLifecyclePairsCompleteTheRun(t *testing.T) {
	// Several run_start/run_end and session_start/session_end pairs must
	// leave the run complete rather than provisional.
	for name, spec := range runnerShapeSpecs() {
		t.Run(name, func(t *testing.T) {
			report := analyzeShapePath(t, spec, writeShapeTree(t, spec, true))

			run, ok := report.FindRun(shapeRunID)
			if !ok {
				t.Fatalf("run %s missing", shapeRunID)
			}
			if run.Provisional {
				t.Error("run is provisional, want complete after run_end")
			}
		})
	}
}

func TestRunnerShape_RaisesNoFindingsBeyondNativeTree(t *testing.T) {
	// The same usage events laid out as a native tree define which findings
	// are expected; the Runner shape must add none and drop none.
	for name, spec := range runnerShapeSpecs() {
		t.Run(name, func(t *testing.T) {
			runner := analyzeShapePath(t, spec, writeShapeTree(t, spec, true))
			native := analyzeShapePath(t, spec, writeShapeTree(t, spec, false))

			if !reflect.DeepEqual(runner.Quality.Counts, native.Quality.Counts) {
				t.Errorf("finding counts = %v, native tree has %v", runner.Quality.Counts, native.Quality.Counts)
			}
		})
	}
}

func TestRunnerShape_ClaudeCodeRunIsFullyClean(t *testing.T) {
	// A Claude Code-like Runner run has a recognised harness and complete
	// usage everywhere, so it must produce no findings at all.
	spec := claudeCodeShape()

	report := analyzeShapePath(t, spec, writeShapeTree(t, spec, true))

	if !report.Quality.IsClean() {
		t.Errorf("findings = %+v, want none", report.Quality.Findings)
	}
}

func TestRunnerShape_MatchesNativeTreeTotals(t *testing.T) {
	// Run-level totals of a Runner-shaped tree equal those of the native tree
	// holding the same usage events.
	for name, spec := range runnerShapeSpecs() {
		t.Run(name, func(t *testing.T) {
			runner := analyzeShapePath(t, spec, writeShapeTree(t, spec, true))
			native := analyzeShapePath(t, spec, writeShapeTree(t, spec, false))

			if !reflect.DeepEqual(runner.AllRuns, native.AllRuns) {
				t.Errorf("AllRuns = %+v, native tree has %+v", runner.AllRuns, native.AllRuns)
			}
		})
	}
}

func TestRunnerShape_AnalysisFromSingleRunFolderMatchesLogsRoot(t *testing.T) {
	// Pointing the scanner at the run folder itself must report the same
	// totals as pointing it at the logs root.
	for name, spec := range runnerShapeSpecs() {
		t.Run(name, func(t *testing.T) {
			logsRoot := writeShapeTree(t, spec, true)

			fromRoot := analyzeShapePath(t, spec, logsRoot)
			fromRun := analyzeShapePath(t, spec, filepath.Join(logsRoot, shapeRunID))

			if len(fromRun.Runs) != 1 || !reflect.DeepEqual(fromRun.AllRuns, fromRoot.AllRuns) {
				t.Errorf("single-run totals = %+v (%d runs), logs-root totals = %+v",
					fromRun.AllRuns, len(fromRun.Runs), fromRoot.AllRuns)
			}
		})
	}
}

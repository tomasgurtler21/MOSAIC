package app

import (
	"context"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// RunTotal is the minimal per-run figure behind the stable CLI JSON contract.
// Keep this shape small and additive-only: it is the committed integration point
// the future Test Suite phase will shell out to.
type RunTotal struct {
	RunID       string
	Provisional bool
	Tokens      domain.TokenUsage
	Money       domain.MoneyValue
	// Complete is false when any model contributing to this run was unpriced.
	Complete bool

	// UnpricedModels names the model(s) contributing to this run that had no
	// price entry. Empty when Complete is true. Populated from the run entry's
	// own UnpricedModels, which already carries them.
	UnpricedModels []domain.ModelID

	// PartialAmount is the amount attributable to priced models when the run is
	// partially priced (some models were priced and some were not). Zero-valued
	// when fully priced or when no models were priced at all.
	PartialAmount domain.MoneyValue

	// UnknownRunMerged is the number of usage records from the unknown-run
	// sibling bucket that were successfully merged into this run's totals
	// during TotalForRun's pre-aggregation re-attribution. Zero when:
	//   - No unknown-run bucket exists in the source.
	//   - The source has multiple named runs (merge guard rejected).
	//   - The unknown-run bucket was empty.
	//   - All unknown-run records were duplicates of named-run records.
	UnknownRunMerged int

	// UnknownRunResidual is the number of unknown-run records that could
	// not be attributed to this run after merging. In a single-run-isolated
	// source this is always 0. Non-zero signals an unexpected condition.
	UnknownRunResidual int
}

// RunTotalFor extracts the basic total for one named run from a report.
// Returns (zero, false) when the run ID is not present in the report.
func RunTotalFor(r domain.Report, runID string) (RunTotal, bool) {
	run, found := r.FindRun(runID)
	if !found {
		return RunTotal{}, false
	}
	partial := run.Totals.Money.Total
	if run.Totals.Money.Complete || partial.State != domain.MoneyKnown {
		partial = domain.MoneyValue{}
	}

	return RunTotal{
		RunID:          run.Run.ID,
		Provisional:    run.Provisional,
		Tokens:         run.Totals.Tokens,
		Money:          run.Totals.Money.Total,
		Complete:       run.Totals.Money.Complete,
		UnpricedModels: run.UnpricedModels,
		PartialAmount:  partial,
	}, true
}

// TotalForRun is the use-case entry point for the basic query: it runs the
// full analysis flow with optional unknown-run merge and returns just the
// requested run's total.
//
// Merge behavior: when the source inventory contains exactly one named run
// and an unattributable bucket (the single-run-isolated case), the
// unknown-run streams are re-attributed to the named run before aggregation.
// This causes the aggregator to fold the unknown-run usage into the named
// run's totals, with record_id-based deduplication handled by the
// aggregator's existing mechanism. When the source has multiple named runs,
// no merge is performed (safe degradation to current behavior).
//
// The general Analyze path is unaffected: Aggregate.Unattributable and
// Report.Unattributable remain structurally separate there. This dedicated
// flow is the ONLY place the merge occurs.
func (s *Service) TotalForRun(ctx context.Context, req Request, runID string) (RunTotal, Outcome, error) {
	// Step 1: Resolve source using the same logic as Analyze.
	src, outcome, err := s.resolveSource(ctx, req)
	if err != nil {
		return RunTotal{}, OutcomeReport, err
	}
	if outcome != outcomeContinue {
		return RunTotal{}, outcome, nil
	}

	// Step 2: Load pricing table.
	table, pricingFindings, err := s.deps.Pricing.Load(ctx)
	if err != nil {
		return RunTotal{}, OutcomeReport, err
	}

	// Step 3: Enumerate the source.
	inv, enumFindings := s.deps.Source.Enumerate(src)

	// Collect findings from infrastructure phases.
	carried := make([]domain.Finding, 0, len(pricingFindings)+len(enumFindings))
	carried = append(carried, pricingFindings...)
	carried = append(carried, enumFindings...)

	// Step 4: A structurally valid source with no run data is a named no-data outcome.
	if inv.IsEmpty() {
		return RunTotal{}, OutcomeNoData, nil
	}

	// Step 5: Read all streams.
	streams, readFindings, err := s.readAllStreams(ctx, inv)
	if err != nil {
		return RunTotal{}, OutcomeReport, err
	}
	carried = append(carried, readFindings...)

	// Step 6: Optionally merge unknown-run streams into the named run.
	var mergePerformed bool
	var totalUnknownRecords int
	var targetRun domain.RunRef

	if mergeEligible(inv) {
		mergePerformed = true
		targetRun = inv.Runs[0].Run
		// Count unknown-run usage records before re-attribution.
		totalUnknownRecords = countUnknownRunRecords(streams)
		// Re-attribute unknown-run streams to the named run.
		streams = reattributeStreams(streams, targetRun)
	}

	// Step 7: Aggregate.
	in := analysis.Input{Streams: streams}
	agg, aggFindings := analysis.Aggregate(in)
	carried = append(carried, aggFindings...)

	// Step 8: Price.
	report, priceFindings := analysis.Price(agg, table, src)
	carried = append(carried, priceFindings...)

	// Step 9: Attach quality summary.
	report.Quality = domain.NewQualitySummary(carried)

	// Step 10: Extract the requested run's total.
	total, found := RunTotalFor(report, runID)
	if !found {
		// Analysis succeeded but the requested run was not among the results.
		return RunTotal{}, OutcomeReport, nil
	}

	// Step 11: Populate merge metadata when a merge was performed.
	if mergePerformed {
		crossStreamDups := countCrossStreamDups(aggFindings, targetRun)
		merged, residual := mergeMetadata(totalUnknownRecords, crossStreamDups)
		total.UnknownRunMerged = merged
		total.UnknownRunResidual = residual
	}

	return total, OutcomeReport, nil
}

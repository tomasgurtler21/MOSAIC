package app

import (
	"context"

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

// TotalForRun is the use-case entry point for the basic query: it runs the full
// analysis flow and returns just the requested run's total. The outcome reflects
// the analysis result; an OutcomeReport outcome with an empty RunTotal signals
// that the analysis succeeded but the requested run was not in the result.
func (s *Service) TotalForRun(ctx context.Context, req Request, runID string) (RunTotal, Outcome, error) {
	result, err := s.Analyze(ctx, req)
	if err != nil {
		return RunTotal{}, OutcomeReport, err
	}
	if result.Outcome != OutcomeReport {
		return RunTotal{}, result.Outcome, nil
	}

	total, found := RunTotalFor(result.Report, runID)
	if !found {
		// Analysis succeeded but the requested run was not among the results.
		return RunTotal{}, OutcomeReport, nil
	}
	return total, OutcomeReport, nil
}

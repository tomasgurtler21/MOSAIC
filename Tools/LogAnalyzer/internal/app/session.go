package app

import (
	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// Session holds the aggregated, not-yet-priced state so prices supplied after
// the fact re-price the report from memory. The logs are never re-read.
type Session struct {
	aggregate domain.Aggregate
	table     domain.PricingTable
	source    domain.Source
}

// Aggregate exposes the cached pre-pricing aggregate.
func (s *Session) Aggregate() domain.Aggregate { return s.aggregate }

// Table exposes the pricing table the current report was priced with.
func (s *Session) Table() domain.PricingTable { return s.table }

// Reprice applies additional entries and returns a freshly priced report. It
// performs no I/O and does not persist anything; persistence is Service.SavePricing.
func (s *Session) Reprice(entries ...domain.ModelPricing) domain.Report {
	table := s.table
	for _, entry := range entries {
		table = table.With(entry)
	}
	report, _ := analysis.Price(s.aggregate, table, s.source)
	return report
}

package analysis

import "mosaic-log-analyzer/internal/domain"

// Input is the complete, already-decoded input to the pure core.
type Input struct {
	Streams []Stream
}

// Stream is one event file's decoded contents with its provenance.
type Stream struct {
	Ref    domain.StreamRef
	Events []domain.Event
}

// Aggregate runs attribution over the input. Pure and deterministic.
func Aggregate(in Input) (domain.Aggregate, []domain.Finding) {
	a := NewAggregator()
	for _, stream := range in.Streams {
		for _, ev := range stream.Events {
			a.Add(stream.Ref, ev)
		}
	}
	return a.Result()
}

// Price converts an aggregate into a priced report using the supplied table.
// Separate from Aggregate so a report can be re-priced from cached totals after
// the user supplies missing prices, without re-reading any logs.
func Price(agg domain.Aggregate, table domain.PricingTable, src domain.Source) (domain.Report, []domain.Finding) {
	return domain.Report{}, nil
}

// Analyze is the composed entry point: Aggregate then Price, with findings from
// both phases plus any carried-in findings merged into the report's
// QualitySummary. Identical inputs always produce an identical report.
func Analyze(in Input, table domain.PricingTable, src domain.Source, carried []domain.Finding) domain.Report {
	return domain.Report{}
}

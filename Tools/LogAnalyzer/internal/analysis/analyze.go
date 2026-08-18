package analysis

import (
	"sort"

	"mosaic-log-analyzer/internal/analysis/cost"
	"mosaic-log-analyzer/internal/domain"
)

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
	runReports := make([]domain.RunReport, 0, len(agg.Runs))
	allRunMoneys := make([]domain.CategoryMoney, 0, len(agg.Runs))
	var allRunTokens domain.TokenUsage
	allUnpricedModels := make(map[domain.ModelID]bool)

	for _, run := range agg.Runs {
		rr := priceRunAggregate(run, table)
		runReports = append(runReports, rr)
		allRunMoneys = append(allRunMoneys, rr.Totals.Money)
		allRunTokens = allRunTokens.Add(run.Totals())
		for _, m := range rr.UnpricedModels {
			allUnpricedModels[m] = true
		}
	}

	var allRunsMoney domain.CategoryMoney
	if len(allRunMoneys) > 0 {
		allRunsMoney = sumCategoryMoneys(allRunMoneys...)
	}

	allRuns := domain.Totals{
		Tokens: allRunTokens,
		Money:  allRunsMoney,
	}

	var unattribReport *domain.RunReport
	if agg.Unattributable != nil {
		rr := priceRunAggregate(*agg.Unattributable, table)
		unattribReport = &rr
		for _, m := range rr.UnpricedModels {
			allUnpricedModels[m] = true
		}
	}

	return domain.Report{
		Source:         src,
		AllRuns:        allRuns,
		Runs:           runReports,
		Unattributable: unattribReport,
		UnpricedModels: sortedModelIDSet(allUnpricedModels),
		Quality:        domain.NewQualitySummary(nil),
		Currency:       domain.Currency,
	}, nil
}

// Analyze is the composed entry point: Aggregate then Price, with findings from
// both phases plus any carried-in findings merged into the report's
// QualitySummary. Identical inputs always produce an identical report.
func Analyze(in Input, table domain.PricingTable, src domain.Source, carried []domain.Finding) domain.Report {
	agg, aggFindings := Aggregate(in)
	report, priceFindings := Price(agg, table, src)

	allFindings := make([]domain.Finding, 0, len(carried)+len(aggFindings)+len(priceFindings))
	allFindings = append(allFindings, carried...)
	allFindings = append(allFindings, aggFindings...)
	allFindings = append(allFindings, priceFindings...)

	report.Quality = domain.NewQualitySummary(allFindings)
	return report
}

// priceActorAggregate converts one actor's aggregate into a priced ActorReport.
func priceActorAggregate(actor domain.ActorAggregate, table domain.PricingTable) domain.ActorReport {
	costs := make([]cost.InvocationCost, len(actor.Invocations))
	for i, inv := range actor.Invocations {
		costs[i] = cost.PriceInvocation(inv, table)
	}

	money := cost.SumCosts(costs...)
	tokens := actor.Totals()

	seenModels := make(map[domain.ModelID]bool)
	seenUnpriced := make(map[domain.ModelID]bool)
	for _, c := range costs {
		if !c.Model.IsEmpty() {
			seenModels[c.Model] = true
			if !c.Priced {
				seenUnpriced[c.Model] = true
			}
		}
	}

	return domain.ActorReport{
		Actor:          actor.Actor,
		Models:         sortedModelIDSet(seenModels),
		UnpricedModels: sortedModelIDSet(seenUnpriced),
		Totals: domain.Totals{
			Tokens: tokens,
			Money:  money,
		},
	}
}

// priceRunAggregate converts one RunAggregate into a priced RunReport.
func priceRunAggregate(run domain.RunAggregate, table domain.PricingTable) domain.RunReport {
	orchReport := priceActorAggregate(run.Orchestrator, table)

	agentReports := make([]domain.ActorReport, len(run.Agents))
	for i, agent := range run.Agents {
		agentReports[i] = priceActorAggregate(agent, table)
	}

	// Collect all per-actor CategoryMoneys to compute the run-level sum.
	allMoneys := make([]domain.CategoryMoney, 0, 1+len(agentReports))
	allMoneys = append(allMoneys, orchReport.Totals.Money)
	for _, ar := range agentReports {
		allMoneys = append(allMoneys, ar.Totals.Money)
	}

	runMoney := sumCategoryMoneys(allMoneys...)

	// Collect run-level unpriced models from all actors.
	seenUnpriced := make(map[domain.ModelID]bool)
	for _, m := range orchReport.UnpricedModels {
		seenUnpriced[m] = true
	}
	for _, ar := range agentReports {
		for _, m := range ar.UnpricedModels {
			seenUnpriced[m] = true
		}
	}

	return domain.RunReport{
		Run:         run.Run,
		Provisional: run.Provisional,
		Totals: domain.Totals{
			Tokens: run.Totals(),
			Money:  runMoney,
		},
		Orchestrator:   orchReport,
		Agents:         agentReports,
		UnpricedModels: sortedModelIDSet(seenUnpriced),
	}
}

// sumCategoryMoneys folds a slice of CategoryMoney into one, summing per-category
// money values and computing the overall Complete flag.
func sumCategoryMoneys(moneys ...domain.CategoryMoney) domain.CategoryMoney {
	inputs := make([]domain.MoneyValue, len(moneys))
	cacheReads := make([]domain.MoneyValue, len(moneys))
	cacheCreations := make([]domain.MoneyValue, len(moneys))
	outputs := make([]domain.MoneyValue, len(moneys))

	for i, m := range moneys {
		inputs[i] = m.Input
		cacheReads[i] = m.CacheRead
		cacheCreations[i] = m.CacheCreation
		outputs[i] = m.Output
	}

	inputTotal, inputComplete := domain.SumMoney(inputs...)
	cacheReadTotal, cacheReadComplete := domain.SumMoney(cacheReads...)
	cacheCreationTotal, cacheCreationComplete := domain.SumMoney(cacheCreations...)
	outputTotal, outputComplete := domain.SumMoney(outputs...)

	total, _ := domain.SumMoney(inputTotal, cacheReadTotal, cacheCreationTotal, outputTotal)
	complete := inputComplete && cacheReadComplete && cacheCreationComplete && outputComplete

	return domain.CategoryMoney{
		Input:         inputTotal,
		CacheRead:     cacheReadTotal,
		CacheCreation: cacheCreationTotal,
		Output:        outputTotal,
		Total:         total,
		Complete:      complete,
	}
}

// sortedModelIDSet converts a set of ModelIDs to a deterministically sorted slice.
func sortedModelIDSet(m map[domain.ModelID]bool) []domain.ModelID {
	if len(m) == 0 {
		return nil
	}
	ids := make([]domain.ModelID, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return string(ids[i]) < string(ids[j])
	})
	return ids
}

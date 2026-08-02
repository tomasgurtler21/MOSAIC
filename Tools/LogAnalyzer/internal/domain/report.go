package domain

// CategoryMoney is the money side of a totals cell, per category plus a total.
type CategoryMoney struct {
	Input         MoneyValue
	CacheRead     MoneyValue
	CacheCreation MoneyValue
	Output        MoneyValue
	Total         MoneyValue
	// Complete is false when any contributing model was unpriced. A false value
	// means Total is a partial sum and must be rendered as such.
	Complete bool
}

// Totals pairs tokens and money so frontends can render them side by side from
// one value rather than correlating two.
type Totals struct {
	Tokens TokenUsage
	Money  CategoryMoney
}

// ActorReport is one actor's priced breakdown within a run.
type ActorReport struct {
	Actor          ActorRef
	Models         []ModelID // distinct, sorted
	UnpricedModels []ModelID // subset of Models with no pricing entry
	Totals         Totals
}

// RunReport is one run's priced breakdown.
type RunReport struct {
	Run            RunRef
	Provisional    bool
	Totals         Totals
	Orchestrator   ActorReport
	Agents         []ActorReport // sorted by instance id
	UnpricedModels []ModelID
}

// Report is the complete analysis output rendered by both frontends.
type Report struct {
	Source Source
	// AllRuns totals NAMED runs only. The unattributable bucket is excluded.
	AllRuns        Totals
	Runs           []RunReport
	Unattributable *RunReport // nil when absent; held separately by contract
	UnpricedModels []ModelID  // distinct, sorted, across the whole report
	Quality        QualitySummary
	// Currency is always domain.Currency; emitted so machine consumers need not assume.
	Currency string
}

// IsEmpty reports whether the report contains no named runs and no unattributable bucket.
func (r Report) IsEmpty() bool {
	return len(r.Runs) == 0 && r.Unattributable == nil
}

// HasUnpricedModels reports whether any models lacked pricing.
func (r Report) HasUnpricedModels() bool { return len(r.UnpricedModels) > 0 }

// FindRun returns the named run report with the given id.
// It searches named runs only; the unattributable bucket is never returned.
func (r Report) FindRun(id string) (RunReport, bool) {
	for _, run := range r.Runs {
		if run.Run.Kind == RunNamed && run.Run.ID == id {
			return run, true
		}
	}
	return RunReport{}, false
}

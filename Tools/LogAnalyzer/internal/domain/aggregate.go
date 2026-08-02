package domain

import "sort"

// InvocationUsage is the pricing unit. Tier selection is per invocation, never
// per run, so per-invocation records are retained rather than pre-summed.
type InvocationUsage struct {
	Model   ModelID // empty when the event carried no model
	Harness Harness
	Usage   TokenUsage
}

// ActorAggregate holds one actor's invocation records within one run.
type ActorAggregate struct {
	Actor       ActorRef
	Invocations []InvocationUsage // file order preserved
}

// Totals returns the absent-preserving per-category sum of all invocations.
func (a ActorAggregate) Totals() TokenUsage {
	usages := make([]TokenUsage, len(a.Invocations))
	for i, inv := range a.Invocations {
		usages[i] = inv.Usage
	}
	return SumUsage(usages...)
}

// Models returns the distinct, sorted model IDs; empty ids excluded.
func (a ActorAggregate) Models() []ModelID {
	seen := make(map[ModelID]bool)
	for _, inv := range a.Invocations {
		if !inv.Model.IsEmpty() {
			seen[inv.Model] = true
		}
	}
	return sortedModelIDs(seen)
}

// RunAggregate holds one run's (or the unattributable bucket's) records.
type RunAggregate struct {
	Run RunRef
	// Provisional is true when neither run_end nor session_end was observed.
	Provisional  bool
	Orchestrator ActorAggregate
	Agents       []ActorAggregate // sorted by instance id
}

// Totals returns the combined token usage across all actors in the run.
func (r RunAggregate) Totals() TokenUsage {
	result := r.Orchestrator.Totals()
	for _, agent := range r.Agents {
		result = result.Add(agent.Totals())
	}
	return result
}

// Models returns the distinct, sorted model IDs across all actors.
func (r RunAggregate) Models() []ModelID {
	seen := make(map[ModelID]bool)
	for _, id := range r.Orchestrator.Models() {
		seen[id] = true
	}
	for _, agent := range r.Agents {
		for _, id := range agent.Models() {
			seen[id] = true
		}
	}
	return sortedModelIDs(seen)
}

// Aggregate is the complete pre-pricing result and the re-pricing input. Holding
// it lets the app re-price after interactive price entry without re-reading logs.
type Aggregate struct {
	Runs           []RunAggregate // named runs, sorted by run id
	Unattributable *RunAggregate  // nil when absent; NEVER folded into Runs
}

// Models returns the distinct model IDs across everything, sorted.
func (a Aggregate) Models() []ModelID {
	seen := make(map[ModelID]bool)
	for _, run := range a.Runs {
		for _, id := range run.Models() {
			seen[id] = true
		}
	}
	if a.Unattributable != nil {
		for _, id := range a.Unattributable.Models() {
			seen[id] = true
		}
	}
	return sortedModelIDs(seen)
}

// IsEmpty reports whether there are no runs and no unattributable bucket.
func (a Aggregate) IsEmpty() bool {
	return len(a.Runs) == 0 && a.Unattributable == nil
}

// sortedModelIDs converts a set of ModelID keys to a sorted slice.
func sortedModelIDs(seen map[ModelID]bool) []ModelID {
	if len(seen) == 0 {
		return nil
	}
	models := make([]ModelID, 0, len(seen))
	for id := range seen {
		models = append(models, id)
	}
	sort.Slice(models, func(i, j int) bool {
		return string(models[i]) < string(models[j])
	})
	return models
}

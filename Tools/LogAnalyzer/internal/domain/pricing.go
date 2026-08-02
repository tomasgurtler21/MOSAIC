package domain

import (
	"errors"
	"sort"
)

// ModelPricing is the five-rate entry for one model, plus its optional
// long-context threshold.
type ModelPricing struct {
	Model                ModelID
	Input                Rate
	CachedInput          Rate // cache read
	CacheWrite           Rate // cache creation
	OutputUnderThreshold Rate
	OutputAtThreshold    Rate  // unused when HasThreshold is false
	// ContextLengthThreshold is the total-context size at or above which the
	// whole invocation's output is repriced at OutputAtThreshold.
	ContextLengthThreshold int64
	HasThreshold           bool // false => the model has no long-context tier
}

// HasLongContextTier reports whether tier selection applies to this model.
func (p ModelPricing) HasLongContextTier() bool { return p.HasThreshold }

// Validate reports why an entry is unusable. An entry with no threshold is valid;
// an entry missing any of the four always-required rates is not. OutputAtThreshold
// is required only when HasThreshold is true.
func (p ModelPricing) Validate() error {
	if p.Model.IsEmpty() {
		return errors.New("pricing entry has empty model ID")
	}
	if p.Input == 0 {
		return errors.New("pricing entry for " + string(p.Model) + " has zero Input rate")
	}
	if p.CachedInput == 0 {
		return errors.New("pricing entry for " + string(p.Model) + " has zero CachedInput rate")
	}
	if p.CacheWrite == 0 {
		return errors.New("pricing entry for " + string(p.Model) + " has zero CacheWrite rate")
	}
	if p.OutputUnderThreshold == 0 {
		return errors.New("pricing entry for " + string(p.Model) + " has zero OutputUnderThreshold rate")
	}
	if p.HasThreshold && p.OutputAtThreshold == 0 {
		return errors.New("pricing entry for " + string(p.Model) + " has HasThreshold=true but zero OutputAtThreshold rate")
	}
	return nil
}

// PricingTable is an immutable, model-keyed lookup.
type PricingTable struct {
	entries map[ModelID]ModelPricing
}

// NewPricingTable builds a PricingTable from a slice of entries.
func NewPricingTable(entries []ModelPricing) PricingTable {
	m := make(map[ModelID]ModelPricing, len(entries))
	for _, e := range entries {
		m[e.Model] = e
	}
	return PricingTable{entries: m}
}

// EmptyPricingTable returns a table with no entries.
func EmptyPricingTable() PricingTable {
	return PricingTable{entries: make(map[ModelID]ModelPricing)}
}

// Lookup resolves a model. found=false is the explicit not-configured outcome and
// is never represented by a zero-valued ModelPricing.
func (t PricingTable) Lookup(m ModelID) (entry ModelPricing, found bool) {
	entry, found = t.entries[m]
	return
}

// Models returns all model IDs in sorted order.
func (t PricingTable) Models() []ModelID {
	if len(t.entries) == 0 {
		return nil
	}
	models := make([]ModelID, 0, len(t.entries))
	for id := range t.entries {
		models = append(models, id)
	}
	sort.Slice(models, func(i, j int) bool {
		return string(models[i]) < string(models[j])
	})
	return models
}

// Len returns the number of entries in the table.
func (t PricingTable) Len() int { return len(t.entries) }

// With returns a copy of the table with the entry added or replaced. The table is
// never mutated in place, so a re-price cannot race a render.
func (t PricingTable) With(entry ModelPricing) PricingTable {
	m := make(map[ModelID]ModelPricing, len(t.entries)+1)
	for id, e := range t.entries {
		m[id] = e
	}
	m[entry.Model] = entry
	return PricingTable{entries: m}
}

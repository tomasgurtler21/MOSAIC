package todo

import (
	"sort"

	"mosaic-deploy/internal/domain"
)

// categoryOrder defines the canonical display order for all todo categories. Items and groups
// are always sorted in this order regardless of insertion sequence.
var categoryOrder = []domain.TodoCategory{
	domain.TodoModels,
	domain.TodoToolMappings,
	domain.TodoInjections,
	domain.TodoSkippedFiles,
	domain.TodoRegistration,
	domain.TodoManual,
	domain.TodoEnvironment,
}

// categoryIndex maps each category to its position in categoryOrder for O(1) lookup during sort.
var categoryIndex = func() map[domain.TodoCategory]int {
	m := make(map[domain.TodoCategory]int, len(categoryOrder))
	for i, c := range categoryOrder {
		m[c] = i
	}
	return m
}()

// gapToCategory is the single canonical mapping from GapKind to TodoCategory. Every gap kind
// in domain.gaps.go appears here exactly once; the mapping is not duplicated anywhere.
var gapToCategory = map[domain.GapKind]domain.TodoCategory{
	domain.GapNoModel:                  domain.TodoModels,
	domain.GapUnmappedTool:             domain.TodoToolMappings,
	domain.GapEmptyInjection:           domain.TodoInjections,
	domain.GapRemovedInjection:         domain.TodoInjections,
	domain.GapParkedCustomRegion:       domain.TodoInjections,
	domain.GapCustomRegionFallthrough:      domain.TodoInjections,
	domain.GapInjectionReparented:          domain.TodoInjections,
	domain.GapDeployedRegionContentChanged: domain.TodoInjections,
	domain.GapDefaultContentDeployed:       domain.TodoInjections,
	domain.GapEnclosingSectionChanged:      domain.TodoInjections,
	domain.GapSkippedFile:                  domain.TodoSkippedFiles,
	domain.GapHookRegistration:         domain.TodoRegistration,
	domain.GapManualStep:               domain.TodoManual,
	domain.GapFallbackLocation:         domain.TodoEnvironment,
	domain.GapUnsupportedArtifact:      domain.TodoEnvironment,
}

// collector is the concrete Collector implementation.
type collector struct {
	items []domain.TodoItem
}

// NewCollector creates a new, empty Collector.
func NewCollector() Collector {
	return &collector{}
}

// Add stores a TodoItem directly. All fields are preserved verbatim.
func (c *collector) Add(item domain.TodoItem) {
	c.items = append(c.items, item)
}

// AddGap converts a Gap into a TodoItem using gapToCategory and stores it.
func (c *collector) AddGap(g domain.Gap) {
	cat, ok := gapToCategory[g.Kind]
	if !ok {
		cat = domain.TodoManual
	}
	c.Add(domain.TodoItem{
		Category: cat,
		Subject:  g.Subject,
		Owner:    g.Owner,
		Detail:   g.Detail,
		Fragment: g.Fragment,
	})
}

// Empty reports whether no items have been collected.
func (c *collector) Empty() bool {
	return len(c.items) == 0
}

// sorted returns a copy of all items sorted first by category declaration order, then by
// ascending Subject within each category.
func (c *collector) sorted() []domain.TodoItem {
	out := make([]domain.TodoItem, len(c.items))
	copy(out, c.items)
	sort.SliceStable(out, func(i, j int) bool {
		ci := categoryIndex[out[i].Category]
		cj := categoryIndex[out[j].Category]
		if ci != cj {
			return ci < cj
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}

// Items returns all collected items in a deterministic order: category declaration order,
// then ascending Subject within each category.
func (c *collector) Items() []domain.TodoItem {
	return c.sorted()
}

// Reset discards all previously accumulated items, returning the collector to its initial
// empty state. Subsequent calls to Empty() return true and Items()/Groups() return empty
// results until new items are added.
func (c *collector) Reset() {
	c.items = c.items[:0]
}

// Groups returns items partitioned by category in the same order as Items. Only categories
// that have at least one item are included in the result.
func (c *collector) Groups() []Group {
	sorted := c.sorted()
	var groups []Group
	for _, cat := range categoryOrder {
		var catItems []domain.TodoItem
		for _, item := range sorted {
			if item.Category == cat {
				catItems = append(catItems, item)
			}
		}
		if len(catItems) > 0 {
			groups = append(groups, Group{Category: cat, Items: catItems})
		}
	}
	return groups
}

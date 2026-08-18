package todo

import (
	"sort"

	"mosaic-deploy/internal/domain"
)

// OwnerGroup is the set of todo items within one category that share one owner.
type OwnerGroup struct {
	Owner string // artifact key the items belong to; empty when the item is not attributable
	Items []domain.TodoItem
}

// GroupByOwner partitions items by their Owner field. Named owners come first in ascending
// owner order; items with an empty Owner form a final group with an empty Owner value.
// Within a group, items keep the order they arrived in (already Subject-ascending from the
// Collector). It is a pure function: it copies, never mutates its argument.
func GroupByOwner(items []domain.TodoItem) []OwnerGroup {
	if len(items) == 0 {
		return nil
	}

	// Collect unique named owners in insertion order, and accumulate items per owner.
	ownerOrder := make([]string, 0)
	ownerSeen := make(map[string]bool)
	byOwner := make(map[string][]domain.TodoItem)

	var unowned []domain.TodoItem

	for _, item := range items {
		if item.Owner == "" {
			// Copy into unowned; do not mutate the input.
			unowned = append(unowned, item)
			continue
		}
		if !ownerSeen[item.Owner] {
			ownerSeen[item.Owner] = true
			ownerOrder = append(ownerOrder, item.Owner)
		}
		byOwner[item.Owner] = append(byOwner[item.Owner], item)
	}

	// Sort named owners ascending.
	sort.Strings(ownerOrder)

	result := make([]OwnerGroup, 0, len(ownerOrder)+1)
	for _, owner := range ownerOrder {
		result = append(result, OwnerGroup{
			Owner: owner,
			Items: byOwner[owner],
		})
	}

	// Unowned items form the final group (omitted when empty).
	if len(unowned) > 0 {
		result = append(result, OwnerGroup{
			Owner: "",
			Items: unowned,
		})
	}

	return result
}

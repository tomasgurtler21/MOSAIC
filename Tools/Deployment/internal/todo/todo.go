package todo

import (
	"time"

	"mosaic-deploy/internal/domain"
)

// Collector accumulates todo items produced by gaps across all deployment components. It is the
// single source for both RenderMarkdown and RenderSummary; neither renderer derives its content
// from the other (CD-9, AC15.6).
type Collector interface {
	// Add stores a TodoItem directly. The item's fields are preserved verbatim.
	Add(domain.TodoItem)
	// AddGap converts a Gap into a TodoItem via the single canonical Gap→Category mapping and
	// stores it. Every gap kind maps to exactly one category; the mapping is not duplicated.
	AddGap(domain.Gap)
	// Items returns all collected items in a deterministic order: category declaration order,
	// then ascending Subject within each category.
	Items() []domain.TodoItem
	// Groups returns items partitioned by category in the same deterministic order as Items.
	// Only categories that have at least one item are included.
	Groups() []Group
	// Empty reports whether no items have been collected.
	Empty() bool
}

// Group is the set of todo items sharing one category.
type Group struct {
	Category domain.TodoCategory
	Items    []domain.TodoItem
}

// Meta is run-level metadata embedded in the generated markdown checklist.
type Meta struct {
	Harness        string
	WorkspacePath  string
	DeploymentRoot string
	GeneratedAt    time.Time
	Mode           domain.RunMode
}

// SummaryLine is one rendered entry in the interactive run summary.
type SummaryLine struct {
	Category domain.TodoCategory
	Text     string
}

// FileName is the name of the todo checklist file written to the workspace root.
const FileName = "MOSAIC-DEPLOYMENT-TODO.md"

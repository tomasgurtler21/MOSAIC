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
	// Reset discards all previously accumulated items, returning the collector to its initial
	// empty state. Subsequent calls to Empty() return true and Items()/Groups() return empty
	// results until new items are added.
	//
	// Intended for use when a deployment attempt is abandoned (e.g., the user declines the
	// plan review, triggering ErrPlanNotConfirmed) so that gaps from the abandoned attempt do
	// not leak into the final report.
	Reset()
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

// FileNamePrefix and FileNameExt compose the deployment TODO checklist's file name.
const (
	FileNamePrefix = "MOSAIC-DEPLOYMENT-TODO"
	FileNameExt    = ".md"
)

// FileNameTimestampLayout is the time layout used for the run-identifying suffix.
const FileNameTimestampLayout = "20060102-150405"

// FileNameAt returns the checklist file name for a run generated at t, with the run's
// timestamp as a suffix before the extension — e.g. "MOSAIC-DEPLOYMENT-TODO-20260814-142405.md".
// t is converted to UTC before formatting, so the same instant always yields the same name
// regardless of the caller's location. A zero t yields the zero instant's formatting; callers
// pass the run's Meta.GeneratedAt, which the service layer always populates.
func FileNameAt(t time.Time) string {
	return FileNamePrefix + "-" + t.UTC().Format(FileNameTimestampLayout) + FileNameExt
}

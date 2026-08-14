// Package domain defines all value types, enumerations, and port interfaces that
// every other runner package depends on. It imports only standard library and
// mosaic-common packages -- never any internal runner package.
package domain

// WorkflowID is the identifier from the name attribute of a
// <Workflow type="core" name="{id}"> region tag.
type WorkflowID string

// WorkflowVersion is the version string from the version attribute of a
// <Workflow type="core" name="{id}" version="{version}"> region tag.
type WorkflowVersion string

// WorkflowInfo identifies a workflow definition.
type WorkflowInfo struct {
	ID      WorkflowID      // identifier from the region's name attribute
	Version WorkflowVersion // from the region's version attribute
}

// ExecutionGroupsHeading is the reserved line that locates a workflow's
// approach-to-group-sequence table inside its SECTION content.
// An exact trimmed full-line match is required.
const ExecutionGroupsHeading = "**Execution Groups:**"

// PhaseParsed is the structured decomposition of a phase string.
type PhaseParsed struct {
	Name     string    // e.g. "RESEARCH", "PLANNING", "DESIGN", "EXECUTION", "REVIEW"
	IsStaged bool      // true when the phase is "EXECUTION.[StageNumber]"
	Group    GroupName // group segment of a staged phase; "" when none declared
}

// OptionalHint represents an optional routing column value.
// ColumnPresent is false when the column does not exist in the table at all.
// When ColumnPresent is true but the value is "-" or empty, Value is "".
// Raw carries the original text when it contains free-form qualification.
type OptionalHint struct {
	ColumnPresent bool
	Value         string // "" means "no hint"; non-empty means the raw column text
}

// RoutingRow is one row of a workflow routing table. The row, not the agent,
// is the unit of execution.
type RoutingRow struct {
	Index           int          // zero-based position in the table; the row's identity
	Phase           string       // literal phase string from the Phase column
	PhaseParsed     PhaseParsed  // structured parse of the phase
	Agent           string       // agent identifier from Subagent column, verbatim
	HITL            bool         // decoded from checkbox emoji
	InputArtifacts  []string     // comma-separated paths, trimmed, verbatim (templates preserved)
	OutputArtifacts []string     // same
	OnSuccess       OptionalHint // from On Success column, if present
	OnFindings      OptionalHint // from On Findings column, if present
}

// RoutingTable is the complete routing model for one workflow.
type RoutingTable struct {
	Info          WorkflowInfo
	Rows          []RoutingRow  // declaration order, zero-based indexed
	ApproachTable ApproachTable // zero value when the workflow declares none
}

// WorkflowRegion is one workflow definition found in an orchestrator agent file.
type WorkflowRegion struct {
	Info    WorkflowInfo // identifier and version
	Content []byte       // raw bytes between the boundary tags (for workflow.Parse)
}

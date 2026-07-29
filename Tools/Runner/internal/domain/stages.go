package domain

// StageNumber is a positive integer stage number starting at 1.
type StageNumber int

// StageEntry is one row from the plan artifact's stage table.
type StageEntry struct {
	Number    StageNumber
	HITL      bool
	DependsOn []StageNumber // empty means no dependencies
	Approach  Approach      // zero value when not read (single-group workflow)
}

// StageSet is the validated, ordered collection of stages for a workflow run.
// Validation guarantees: consecutive numbering starting at 1, no forward
// dependencies, and (when required) valid approach values.
type StageSet struct {
	Entries []StageEntry // ascending stage-number order, consecutive from 1
}

// Count returns the number of stages.
func (s StageSet) Count() int {
	return len(s.Entries)
}

// Entry returns the stage entry for the given number, or false if not found.
func (s StageSet) Entry(n StageNumber) (StageEntry, bool) {
	for _, e := range s.Entries {
		if e.Number == n {
			return e, true
		}
	}
	return StageEntry{}, false
}

// Numbers returns all stage numbers in order.
func (s StageSet) Numbers() []StageNumber {
	out := make([]StageNumber, len(s.Entries))
	for i, e := range s.Entries {
		out[i] = e.Number
	}
	return out
}

// Approach determines how execution groups are ordered within a stage.
type Approach string

const (
	ApproachTDD                 Approach = "TDD"
	ApproachImplementationFirst Approach = "Implementation-First"
	ApproachImplementationOnly  Approach = "Implementation-Only"
	ApproachTestsOnly           Approach = "Tests-Only"
)

// GroupKind distinguishes the two kinds of execution group.
type GroupKind string

const (
	GroupTest           GroupKind = "test"
	GroupImplementation GroupKind = "implementation"
)

// ExecutionGroup is a contiguous range of routing table rows that form
// one execution group within the EXECUTION phase.
type ExecutionGroup struct {
	Kind     GroupKind
	StartRow int // inclusive, zero-based row index into RoutingTable.Rows
	EndRow   int // exclusive, zero-based row index
}

// AdmittedWorkflow is the output of compat admission: a routing table that has
// been validated as inside the supported subset, with resolved execution groups.
type AdmittedWorkflow struct {
	Table    RoutingTable
	Groups   []ExecutionGroup // 1 or 2 groups; empty for non-staged workflows
	TwoGroup bool             // true when the workflow declares both test and implementation groups

	// PreExecutionRows and PostExecutionRows identify the row ranges
	// before and after the staged EXECUTION phase (empty for non-staged or
	// when the phase is at the boundary of the table).
	PreExecutionStartRow  int // inclusive
	PreExecutionEndRow    int // exclusive (equals first EXECUTION row index)
	PostExecutionStartRow int // inclusive (equals first post-EXECUTION row index)
	PostExecutionEndRow   int // exclusive

	HasStagedPhase bool
}

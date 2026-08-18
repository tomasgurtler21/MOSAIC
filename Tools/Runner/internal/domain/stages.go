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

// GroupName is a workflow-defined execution group token, taken verbatim from
// the Phase column's group segment and from the Execution Groups table.
// The empty value means "no group declared".
type GroupName string

// Approach is a workflow-defined, opaque token read verbatim from the plan
// artifact's Approach column and matched against the workflow's Execution
// Groups table. The runner defines no Approach constants.
// The zero value "" means "no approach read" and is only legal when the
// workflow declares no groups.
type Approach string

// ApproachSequence is one row of the Execution Groups table: an approach token
// and the ordered group sequence it selects.
type ApproachSequence struct {
	Approach Approach
	Groups   []GroupName // ordered; non-empty for a valid table row
}

// ApproachTable is a workflow's Execution Groups table in declaration order.
// The zero value means the workflow declares no approach table.
type ApproachTable struct {
	Rows []ApproachSequence
}

// Present reports whether the workflow declares an approach table.
func (t ApproachTable) Present() bool {
	return len(t.Rows) > 0
}

// Sequence returns the ordered group sequence for the given approach token,
// and false when no row matches. Matching is exact and case-sensitive.
func (t ApproachTable) Sequence(a Approach) ([]GroupName, bool) {
	for _, row := range t.Rows {
		if row.Approach == a {
			return row.Groups, true
		}
	}
	return nil, false
}

// Approaches returns every declared approach token in table order.
func (t ApproachTable) Approaches() []Approach {
	out := make([]Approach, len(t.Rows))
	for i, row := range t.Rows {
		out[i] = row.Approach
	}
	return out
}

// GroupNames returns every group named anywhere in the table, deduplicated,
// in order of first appearance.
func (t ApproachTable) GroupNames() []GroupName {
	seen := make(map[GroupName]bool)
	var out []GroupName
	for _, row := range t.Rows {
		for _, g := range row.Groups {
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

// ExecutionGroup is a contiguous range of routing table rows that form
// one execution group within the EXECUTION phase.
type ExecutionGroup struct {
	Name     GroupName // workflow-defined; "" for the implicit no-groups range
	StartRow int       // inclusive, zero-based row index into RoutingTable.Rows
	EndRow   int       // exclusive, zero-based row index
}

// AdmittedWorkflow is the output of compat admission: a routing table that has
// been validated as inside the supported subset, with resolved execution groups.
type AdmittedWorkflow struct {
	Table RoutingTable

	// Groups holds the EXECUTION row partition. When GroupsDeclared is true it
	// holds one entry per declared group, in first-appearance order. When
	// GroupsDeclared is false it holds exactly one entry with an empty Name
	// covering every staged row. Empty for non-staged workflows.
	Groups         []ExecutionGroup
	GroupsDeclared bool          // true iff at least one EXECUTION row carries a group segment
	ApproachTable  ApproachTable // carried through from the routing table

	// PreExecutionRows and PostExecutionRows identify the row ranges
	// before and after the staged EXECUTION phase (empty for non-staged or
	// when the phase is at the boundary of the table).
	PreExecutionStartRow  int // inclusive
	PreExecutionEndRow    int // exclusive (equals first EXECUTION row index)
	PostExecutionStartRow int // inclusive (equals first post-EXECUTION row index)
	PostExecutionEndRow   int // exclusive

	HasStagedPhase bool
}

// GroupByName returns the group with the given name and false when absent.
func (w AdmittedWorkflow) GroupByName(n GroupName) (ExecutionGroup, bool) {
	for _, g := range w.Groups {
		if g.Name == n {
			return g, true
		}
	}
	return ExecutionGroup{}, false
}

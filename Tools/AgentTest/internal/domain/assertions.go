package domain

import "strings"

// Assertions is the full assertion vocabulary a test definition can declare.
// Every class the evaluator supports is expressible here; a nil pointer or
// nil slice means the class is not evaluated for this test, which is
// distinct from an empty non-nil slice meaning "assert the empty set".
type Assertions struct {
	InvocationSequence    *SequenceAssertion
	ExecutionLogAgentIDs  *[]string // ordered agent instance ids, e.g. "researcher#3"
	ExecutionLogAllStatus *string   // every recorded status equals this
	FinalPhase            *string
	FinalStatus           *string
	ProtocolViolations    *int
	ArtifactCreated       []string
	ArtifactNotCreated    []string
	// MinConcurrency maps a declared parallel group name to the minimum peak
	// concurrency that group must have achieved.
	MinConcurrency map[string]int
	TaskMessages   []TaskMessageAssertion
}

// SequenceAssertion matches the observed invocation sequence.
// Order between steps decides the verdict; order within a parallel step does
// not.
type SequenceAssertion struct {
	Steps []SequenceStep
	// Exact requires the observed sequence to contain nothing beyond Steps.
	// When false, the assertion is a subsequence match.
	Exact bool
}

// SequenceStep is one step of a SequenceAssertion: either a single ordered
// collaborator, or a parallel group whose members must all appear
// contiguously in any order.
type SequenceStep struct {
	// Identity is set for an ordered single-member step.
	Identity *CollaboratorIdentity
	// Group and Members are set for a parallel step.
	Group   string
	Members []CollaboratorIdentity
}

// TaskMessageAssertion asserts on the Communication Protocol message one
// invocation carried.
type TaskMessageAssertion struct {
	// At is the 1-based global sequence number of the invocation asserted on.
	At int
	// Identity, when set, is cross-checked against the invocation at At, so a
	// sequence drift is reported as such rather than as a message mismatch.
	Identity *CollaboratorIdentity

	// Artifact set semantics: every Required entry must be present; each
	// Optional entry may be. Anything present that is in neither set fails
	// the assertion — that is what gives Optional meaning as a permission
	// rather than as decoration.
	RequiredInputArtifacts  []string
	OptionalInputArtifacts  []string
	RequiredOutputArtifacts []string
	OptionalOutputArtifacts []string

	HumanInTheLoop          *bool
	TaskDescriptionContains []string
}

// ExpandRunID returns a deep copy of a in which every path-bearing field has
// each occurrence of RunIDPlaceholder replaced by runID. The receiver and
// every slice it points at are left untouched, so evidence can be
// re-evaluated repeatedly and in any order and still yield the same verdict.
//
// Path-bearing fields, and the complete set this method covers:
//   - ArtifactCreated
//   - ArtifactNotCreated
//   - each TaskMessages entry's RequiredInputArtifacts, OptionalInputArtifacts,
//     RequiredOutputArtifacts and OptionalOutputArtifacts
//
// Every other field is copied through unchanged: a non-path field containing
// the placeholder text is not a path and is not rewritten.
//
// An empty runID returns the deep copy with no substitution performed, so a
// caller with no run identity degrades to today's verbatim comparison rather
// than to an empty-string substitution.
//
// Nil-versus-empty is preserved exactly: a nil slice copies as nil and an
// empty non-nil slice copies as empty non-nil, because Assertions documents
// that distinction as the difference between "not evaluated" and "assert the
// empty set".
func (a Assertions) ExpandRunID(runID string) Assertions {
	expandSlice := func(paths []string) []string {
		if paths == nil {
			return nil
		}
		out := make([]string, len(paths))
		for i, p := range paths {
			if runID != "" {
				out[i] = strings.ReplaceAll(p, RunIDPlaceholder, runID)
			} else {
				out[i] = p
			}
		}
		return out
	}

	result := a
	result.ArtifactCreated = expandSlice(a.ArtifactCreated)
	result.ArtifactNotCreated = expandSlice(a.ArtifactNotCreated)

	if a.TaskMessages != nil {
		tms := make([]TaskMessageAssertion, len(a.TaskMessages))
		for i, tm := range a.TaskMessages {
			tms[i] = tm
			tms[i].RequiredInputArtifacts = expandSlice(tm.RequiredInputArtifacts)
			tms[i].OptionalInputArtifacts = expandSlice(tm.OptionalInputArtifacts)
			tms[i].RequiredOutputArtifacts = expandSlice(tm.RequiredOutputArtifacts)
			tms[i].OptionalOutputArtifacts = expandSlice(tm.OptionalOutputArtifacts)
		}
		result.TaskMessages = tms
	}

	return result
}

// AssertionClass names one class of assertion the evaluator supports.
type AssertionClass string

const (
	ClassInvocationSequence    AssertionClass = "invocation_sequence"
	ClassExecutionLogAgentIDs  AssertionClass = "execution_log.agent_ids"
	ClassExecutionLogAllStatus AssertionClass = "execution_log.all_status"
	ClassFinalPhase            AssertionClass = "final_state.phase"
	ClassFinalStatus           AssertionClass = "final_state.last_status"
	ClassProtocolViolations    AssertionClass = "protocol_violations"
	ClassArtifactCreated       AssertionClass = "artifact_created"
	ClassArtifactNotCreated    AssertionClass = "artifact_not_created"
	ClassMinConcurrency        AssertionClass = "min_concurrency"
	ClassTaskMessage           AssertionClass = "task_message"
	// Always evaluated, never declared, never suppressed, never inverted.
	ClassEchoFidelity AssertionClass = "echo_fidelity"
)

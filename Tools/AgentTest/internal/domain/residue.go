package domain

// StateResidue is what a run's correlation state still held when the run
// ended. Captured from the persisted run state at snapshot time and carried
// on the evidence, so the verdict engine can raise a condition about it
// without reading a file.
type StateResidue struct {
	// PendingStubs are dispatches whose echo check never resolved.
	PendingStubs []ResidualDispatch
	// InFlight are dispatches whose in-flight entry was never released.
	InFlight []ResidualDispatch

	// Unreadable is set when the run state could not be read back at
	// snapshot time, so an unreadable state is never folded into the
	// evidence as an empty one that reads like "nothing leaked". Empty
	// otherwise.
	Unreadable string
}

// ResidualDispatch identifies one dispatch left outstanding at end of run.
type ResidualDispatch struct {
	// Seq is the invocation sequence number, or zero when the entry carried
	// none.
	Seq int
	// Identity is the collaborator the dispatch was for.
	Identity CollaboratorIdentity
}

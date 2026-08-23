package domain

// ExclusionReason names why a run was kept out of the pass-rate denominator.
// The empty string means the run was counted.
type ExclusionReason string

const (
	// ExclusionStateIntegrity: a lock was reclaimed, so a state update may
	// have been lost and every verdict computed afterwards is suspect.
	ExclusionStateIntegrity ExclusionReason = "state_integrity"

	// ExclusionSpawnFailed: the harness process exited non-zero, so the
	// subject may never have run at all. Evidence about the tool's environment,
	// not about the subject.
	ExclusionSpawnFailed ExclusionReason = "spawn_failed"
)

// ExcludedRun is one run kept out of the pass-rate denominator, and why.
//
// It exists because the exclusion count alone reaches the wire today and the
// reason does not: a reader can see that a run did not count but not why,
// which leaves inspecting a retained sandbox as the only way to find out.
type ExcludedRun struct {
	Key    RunKey
	Reason ExclusionReason

	// TerminationReason is the run's subject disposition, carried through
	// unchanged. Empty when none was recorded, which the renderings show as
	// "unknown" rather than blank.
	TerminationReason string

	// Detail is enough to explain the exclusion without re-reading the
	// invocation log. Never the empty string.
	Detail string
}

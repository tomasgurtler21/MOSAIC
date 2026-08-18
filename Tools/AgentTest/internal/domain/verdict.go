package domain

// Verdict is the outcome of evaluating one run against a test definition.
type Verdict string

const (
	VerdictPass    Verdict = "PASS"
	VerdictFail    Verdict = "FAIL"
	VerdictTimeout Verdict = "TIMEOUT"
)

// FailureReason names why a verdict was not VerdictPass.
type FailureReason string

const (
	// An assertion the test declared did not hold.
	ReasonAssertion FailureReason = "ASSERTION"
	// A stubbed collaborator did not echo its stub faithfully. Always
	// evaluated, unconditional, never inverted.
	ReasonEchoMismatch FailureReason = "ECHO_MISMATCH"
	// A lock was reclaimed, so a state update may have been lost and the
	// verdict rests on possibly-wrong state. Reported distinctly so an
	// infrastructure fault is never mistaken for a subject regression.
	ReasonStateIntegrity FailureReason = "STATE_INTEGRITY"
	// The run exceeded its timeout. A distinct verdict, not a species of FAIL.
	ReasonTimeout FailureReason = "TIMEOUT"
	// The tool itself could not complete the run.
	ReasonInfrastructure FailureReason = "INFRASTRUCTURE"
)

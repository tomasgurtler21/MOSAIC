package domain

// EnvironmentProblemKind names one class of pre-run environment failure, in
// a vocabulary no harness name enters.
type EnvironmentProblemKind string

const (
	ProblemInterpreterUnresolved EnvironmentProblemKind = "interpreter_unresolved"
	ProblemBundleUnreadable      EnvironmentProblemKind = "bundle_unreadable"
	ProblemBundleMisshapen       EnvironmentProblemKind = "bundle_misshapen"
	ProblemCostToolUnavailable   EnvironmentProblemKind = "cost_tool_unavailable"
	ProblemBinaryPathUnresolved  EnvironmentProblemKind = "binary_path_unresolved"
	ProblemCompetingRewriter     EnvironmentProblemKind = "competing_rewriter"
)

// EnvironmentProblem is one reason the environment is unusable.
type EnvironmentProblem struct {
	Kind EnvironmentProblemKind

	// Detail names what was tried and what to do about it. For a bundle
	// problem it names the expected layout, not a bare file-not-found, and
	// it names the path that was searched and the override that would
	// change it.
	Detail string
}

// EnvironmentReport is what an adapter's environment check yields: the
// values the neutral layers need from it, plus every problem that would
// make a run meaningless.
//
// It is harness-neutral by construction. An adapter that resolves no
// interpreter says so through InterpreterApplicable rather than by
// inventing one, which is what keeps the report honest for an adapter whose
// harness has no interpreter to resolve.
type EnvironmentReport struct {
	// InterpreterCmd is the resolved command the logger bundle's
	// registration fragments must run. Empty when InterpreterApplicable is
	// false.
	InterpreterCmd string

	// InterpreterApplicable declares whether this adapter runs an
	// interpreter at all. False makes an empty InterpreterCmd an honest
	// answer rather than an unresolved one.
	InterpreterApplicable bool

	// InterpreterTried lists every candidate examined, in order, so a
	// failure diagnostic can name what was looked for rather than only what
	// was missed.
	InterpreterTried []string

	// BinaryPath is the resolved absolute path of the running binary, which
	// the bridge registers as the interceptor command.
	BinaryPath string

	// Scopes is what InspectScopes found, carried here so preflight sees
	// the configuration picture and the interpreter in one value.
	Scopes []ScopeFinding

	// Problems is the authority on usability. Empty means usable.
	Problems []EnvironmentProblem
}

// OK reports whether the environment is usable: no problem was found.
func (r EnvironmentReport) OK() bool {
	return len(r.Problems) == 0
}

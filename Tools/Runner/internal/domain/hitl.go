package domain

// HumanApproval is the result of reading an artifact's human_approved
// frontmatter field. Every failure mode is a distinct value rather than an
// error, so the compliance decision is total.
type HumanApproval int

const (
	// ApprovalTrue: the field is present and reads "true".
	ApprovalTrue HumanApproval = iota

	// ApprovalFalse: the field is present and reads "false".
	ApprovalFalse

	// ApprovalAbsent: a frontmatter block exists but carries no
	// human_approved key. NOT equivalent to ApprovalTrue.
	ApprovalAbsent

	// ApprovalNoFrontmatter: the file exists but has no frontmatter block.
	ApprovalNoFrontmatter

	// ApprovalMalformed: a frontmatter block exists but could not be parsed,
	// or human_approved holds a value that is neither true nor false.
	ApprovalMalformed

	// ApprovalFileMissing: no file exists at the path.
	ApprovalFileMissing

	// ApprovalUnreadable: the file exists but could not be read (permissions,
	// I/O error).
	ApprovalUnreadable
)

// IsApproved reports whether the value means the human gate was closed.
// Only ApprovalTrue does. Every other value — including ApprovalAbsent and
// ApprovalMalformed — reports false, so a degraded read never passes the gate.
func (a HumanApproval) IsApproved() bool {
	return a == ApprovalTrue
}

// HITLComplianceInput is everything the compliance decision needs. It holds no
// state and touches no filesystem: the caller performs the reads and passes
// the results in.
type HITLComplianceInput struct {
	// EffectiveHITL is the HITL value actually sent on the dispatch, after
	// override resolution.
	EffectiveHITL bool

	// Status is the status code the agent returned.
	Status StatusCode

	// Approvals holds one entry per artifact in the dispatch's effective
	// output_artifacts list, in dispatch order. An empty or nil slice means
	// the agent was tasked with no output artifact for this step.
	Approvals []ArtifactApproval

	// RedispatchUsed reports whether this step has already consumed its one
	// HITL redispatch. The caller owns this flag and scopes it to the step;
	// the decision function never carries state between calls.
	RedispatchUsed bool
}

// ArtifactApproval pairs a dispatched output artifact path with its read result.
type ArtifactApproval struct {
	Path     string
	Approval HumanApproval
}

// HITLComplianceDecision is the outcome of the verification.
type HITLComplianceDecision struct {
	Outcome HITLOutcome

	// NonCompliant lists the artifacts that failed the check, in dispatch
	// order. Empty when Outcome is HITLAccept. It is the material for the
	// operator-facing message and the debug log entry.
	NonCompliant []ArtifactApproval
}

// HITLOutcome is the action the dispatch loop must take.
type HITLOutcome int

const (
	// HITLAccept: the step is final; the loop proceeds to recording.
	HITLAccept HITLOutcome = iota

	// HITLRedispatch: re-dispatch the same agent with identical task
	// description, constraints, and artifact lists. Allowed at most once per
	// step.
	HITLRedispatch

	// HITLEscalate: the single redispatch is spent and the artifacts are still
	// non-compliant. Feed the result back into the routing decision as a
	// deviation, the same path a non-SUCCESS result takes.
	HITLEscalate
)

// DecideHITLCompliance is pure: no I/O, no clock, no state.
//
// Decision table:
//
//	EffectiveHITL false                       -> HITLAccept
//	Status != StatusSUCCESS                   -> HITLAccept (the non-SUCCESS
//	                                             path already deviates)
//	len(Approvals) == 0                       -> HITLAccept (nothing to verify)
//	every Approval.IsApproved()               -> HITLAccept
//	any !IsApproved() && !RedispatchUsed      -> HITLRedispatch
//	any !IsApproved() && RedispatchUsed       -> HITLEscalate
//
// ApprovalFileMissing is treated identically to ApprovalFalse: a SUCCESS whose
// declared output artifact does not exist has not closed its gate either.
func DecideHITLCompliance(in HITLComplianceInput) HITLComplianceDecision {
	// Short-circuit: HITL was not required for this dispatch.
	if !in.EffectiveHITL {
		return HITLComplianceDecision{Outcome: HITLAccept}
	}

	// Short-circuit: non-SUCCESS results are handled by the deviation path;
	// HITL verification only applies to SUCCESS.
	if in.Status != StatusSUCCESS {
		return HITLComplianceDecision{Outcome: HITLAccept}
	}

	// Short-circuit: nothing to verify when no output artifacts were declared.
	if len(in.Approvals) == 0 {
		return HITLComplianceDecision{Outcome: HITLAccept}
	}

	// Collect non-compliant artifacts in dispatch order.
	var nonCompliant []ArtifactApproval
	for _, a := range in.Approvals {
		if !a.Approval.IsApproved() {
			nonCompliant = append(nonCompliant, a)
		}
	}

	// All artifacts approved.
	if len(nonCompliant) == 0 {
		return HITLComplianceDecision{Outcome: HITLAccept}
	}

	// Non-compliant artifacts exist: redispatch or escalate.
	if !in.RedispatchUsed {
		return HITLComplianceDecision{Outcome: HITLRedispatch, NonCompliant: nonCompliant}
	}
	return HITLComplianceDecision{Outcome: HITLEscalate, NonCompliant: nonCompliant}
}

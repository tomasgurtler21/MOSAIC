package domain

// RunContext carries the run facts that are settled during the run-start
// sequence and needed by consultants constructed before it. It is immutable
// once handed over: a consultant stores the values and never mutates them.
type RunContext struct {
	// Orchestrator is the resolved orchestrator agent reference. Its
	// InvocationKind is always InvocationOrchestrator. It is the reference
	// every consultation is issued against.
	Orchestrator AgentReference

	// Table is the selected workflow's parsed routing table. It resolves a
	// dispatch instruction's agent identifier to a row index and supplies the
	// available-agent list for an unknown-agent failure and the option list
	// for the manual resolver.
	Table RoutingTable
}

// RunContextBinder is the seam by which the session hands a consultant the
// two facts that are not known when the composition root constructs it: the
// resolved orchestrator reference and the selected workflow's routing table.
//
// The composition root builds consultants with neither. The session calls
// BindRunContext exactly once per run, on the run-start path, after the
// routing table is parsed and the orchestrator is resolved and before the
// artifact is created or any consultation is issued.
//
// Implementing this interface is optional. A consultant that needs neither
// fact — a test double, a future consultant with its own source of truth —
// simply does not implement it, and the session skips it. A consultant that
// does implement it must not be consulted before it has been bound; the
// session guarantees this ordering.
//
// BindRunContext is called from the run-start goroutine only, before any
// consultation, so implementations need no synchronisation.
type RunContextBinder interface {
	BindRunContext(rc RunContext)
}

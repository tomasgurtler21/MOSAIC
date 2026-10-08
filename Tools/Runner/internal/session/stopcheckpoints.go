package session

// Graceful-stop checkpoint identifiers. Emitted as the "checkpoint" field on
// domain.EventSessionStopObserved so the debug log states which dispatch path
// observed the stop signal. These are the only six checkpoints; adding one
// requires extending this list.
//
// Exported so tests can assert on the constant rather than on a string literal,
// from either an in-package or an external test package.
const (
	StopCheckpointEngineStep            = "engine.step"
	StopCheckpointEngineHITLRedispatch  = "engine.hitl_redispatch"
	StopCheckpointConsultDispatch       = "consult.dispatch"
	StopCheckpointConsultHITLRedispatch = "consult.hitl_redispatch"
	StopCheckpointInfraDispatch         = "infra.dispatch"

	// StopCheckpointConsultEntry is observed at the start of every routing
	// consultation, before any consultant is called.
	StopCheckpointConsultEntry = "consult.entry"
)

// DiscardedRoutingDecisionNote is the fixed phrase in a RunStopped outcome
// message when a routing decision completed after a stop request was
// discarded.
const DiscardedRoutingDecisionNote = "a completed routing decision was discarded"

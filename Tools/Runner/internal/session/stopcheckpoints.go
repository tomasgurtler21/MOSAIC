package session

// Graceful-stop checkpoint identifiers. Emitted as the "checkpoint" field on
// domain.EventSessionStopObserved so the debug log states which dispatch path
// observed the stop signal. These are the only five checkpoints; adding one
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
)

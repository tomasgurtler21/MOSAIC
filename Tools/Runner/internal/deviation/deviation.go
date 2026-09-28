// Package deviation implements the two-action orchestrator consultation contract.
//
// Two implementations are provided:
//   - OrchestratorConsultant: implements the two-action consultation contract
//     (domain.RoutingConsultant and domain.PreConsultant) by carrying a raw JSON
//     payload to the orchestrator over the RawInvoker transport.
//   - ManualResolver: drives the Interaction port to produce a RoutingInstruction
//     from user input.
//
// Neither OrchestratorConsultant nor ManualResolver originates a HITL override.
// HITLOverride is carried only when explicitly set by the orchestrator's response
// or when the user explicitly asks for one.
package deviation

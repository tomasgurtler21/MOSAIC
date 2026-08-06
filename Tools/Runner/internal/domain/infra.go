package domain

// DeclaredInfraTrigger represents one trigger in an infrastructure agent's
// declaration (parsed from the deployed orchestrator file) or in an
// infrastructure_overrides entry (parsed from Orchestration.md frontmatter).
//
// Trigger is drawn from the closed vocabulary: "STAGE_END",
// "INVOCATION_INTERVAL", "PHASE_END", "MANUAL".
//
// Param holds the trigger's parameter value (e.g. an interval count for
// INVOCATION_INTERVAL). Empty string when the trigger takes no parameter.
// The YAML field name for Param is "trigger_param".
type DeclaredInfraTrigger struct {
	Trigger string // e.g. "STAGE_END", "INVOCATION_INTERVAL", "PHASE_END", "MANUAL"
	Param   string // trigger parameter; empty when the trigger takes no parameter
}

// DeclaredInfraAgent represents one infrastructure agent parsed from the
// deployed orchestrator's [[INJECTION:InfrastructureAgents]] region.
type DeclaredInfraAgent struct {
	Name      string                // agent name (from section identifier)
	Class     string                // "checkpoint", "commit", "review", "restore"
	Triggers  []DeclaredInfraTrigger
	OnFailure string                // "halt" or "continue"
	Version   string                // from <!-- infra-version: {version} --> comment; reserved for future use
}

// InfraDispatchResult carries the outcome of an infrastructure agent dispatch.
type InfraDispatchResult struct {
	Agent    DeclaredInfraAgent
	Response ProtocolResponse
	Step     CompletedStep
}

// NopDebugLogger is the named no-op DebugLogger. It is the default whenever no
// logger is injected, so consumers never hold a nil DebugLogger and never need
// a nil check at a call site.
//
// The zero value is usable; NopDebugLogger{} may be copied freely and shared.
type NopDebugLogger struct{}

// Log implements DebugLogger. It discards the entry.
func (NopDebugLogger) Log(event string, message string, fields ...DebugField) {}

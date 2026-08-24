package domain

import "strings"

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
// deployed orchestrator's <InfrastructureAgents type="project"> region.
type DeclaredInfraAgent struct {
	Name      string                // agent name (from section identifier)
	Class     string                // "checkpoint", "commit", "review", "restore"
	Triggers  []DeclaredInfraTrigger
	OnFailure string                // "halt" or "continue"
	Version   string                // from the region's version attribute; reserved for future use
}

// InfraDispatchResult carries the outcome of an infrastructure agent dispatch.
type InfraDispatchResult struct {
	Agent    DeclaredInfraAgent
	Response ProtocolResponse
	Step     CompletedStep
}

// InfraAgentSet reports whether an agent instance recorded in an artifact
// belongs to an infrastructure agent declared for this run.
//
// The argument is an agent instance id in "{AgentName}#{Seq}" form, or a
// bare agent name; implementations must accept both and compare on the
// name part only.
//
// A nil InfraAgentSet is valid and means "no infrastructure agents are
// declared for this run": callers must treat nil as always-false rather
// than nil-checking at every site. NewInfraAgentSet returns a value that is
// safe to call in this way, including for an empty or nil declared slice.
type InfraAgentSet interface {
	IsInfrastructureAgent(agentInstance string) bool
}

// infraAgentSet is the InfraAgentSet implementation built by NewInfraAgentSet.
// Its zero value (including a nil *infraAgentSet) is usable: IsInfrastructureAgent
// reports false for a nil receiver, so a caller need not nil-check the set itself.
type infraAgentSet struct {
	names map[string]struct{}
}

// NewInfraAgentSet builds an InfraAgentSet from the run's declared
// infrastructure agents. extraNames registers additional agent identifiers
// (e.g. "orchestrator-script") so that consultation rows recorded under
// those identifiers are classified as infrastructure steps on resume.
// An empty or nil declared slice with no extraNames yields a set that
// reports false for every agent.
func NewInfraAgentSet(declared []DeclaredInfraAgent, extraNames ...string) InfraAgentSet {
	if len(declared) == 0 && len(extraNames) == 0 {
		return (*infraAgentSet)(nil)
	}
	names := make(map[string]struct{}, len(declared)+len(extraNames))
	for _, d := range declared {
		names[d.Name] = struct{}{}
	}
	for _, n := range extraNames {
		names[n] = struct{}{}
	}
	return &infraAgentSet{names: names}
}

// IsInfrastructureAgent implements InfraAgentSet. agentInstance may be a bare
// agent name or an "{AgentName}#{Seq}" instance id; only the name part is
// compared.
func (s *infraAgentSet) IsInfrastructureAgent(agentInstance string) bool {
	if s == nil {
		return false
	}
	name := agentInstance
	if i := strings.LastIndex(agentInstance, "#"); i >= 0 {
		name = agentInstance[:i]
	}
	_, ok := s.names[name]
	return ok
}

// InfraAgentsOfClass returns the subset of declared infrastructure agents whose
// Class field equals the given class string. The order of elements in the
// returned slice matches their order in the declared slice. Returns nil when no
// agents match (not an empty non-nil slice), so callers can distinguish "none
// declared" from an allocated slice with len zero.
func InfraAgentsOfClass(declared []DeclaredInfraAgent, class string) []DeclaredInfraAgent {
	var result []DeclaredInfraAgent
	for _, a := range declared {
		if a.Class == class {
			result = append(result, a)
		}
	}
	return result
}

// NopDebugLogger is the named no-op DebugLogger. It is the default whenever no
// logger is injected, so consumers never hold a nil DebugLogger and never need
// a nil check at a call site.
//
// The zero value is usable; NopDebugLogger{} may be copied freely and shared.
type NopDebugLogger struct{}

// Log implements DebugLogger. It discards the entry.
func (NopDebugLogger) Log(event string, message string, fields ...DebugField) {}

// NopDispatchLogger is the named no-op DispatchLogger. It is the default
// whenever no logger is injected, so consumers never hold a nil
// DispatchLogger and never need a nil check at a call site.
//
// The zero value is usable; NopDispatchLogger{} may be copied freely and
// shared.
type NopDispatchLogger struct{}

// LogRequest implements DispatchLogger. It discards the entry.
func (NopDispatchLogger) LogRequest(req ProtocolRequest) {}

// LogResponse implements DispatchLogger. It discards the entry.
func (NopDispatchLogger) LogResponse(resp ProtocolResponse) {}

// LogError implements DispatchLogger. It discards the entry.
func (NopDispatchLogger) LogError(agentInstanceID string, errText string) {}

// SetRunID implements DispatchLogger. It is a no-op.
func (NopDispatchLogger) SetRunID(runID string) {}

// Close implements DispatchLogger. It is a no-op.
func (NopDispatchLogger) Close() {}

// Path implements DispatchLogger. It always returns "".
func (NopDispatchLogger) Path() string { return "" }

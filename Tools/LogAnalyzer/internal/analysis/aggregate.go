package analysis

import (
	"sort"

	"mosaic-log-analyzer/internal/domain"
)

// runState holds mutable state for one run (or the unattributable bucket)
// as events are accumulated.
type runState struct {
	run          domain.RunRef
	orchestrator actorState
	agents       map[domain.AgentInstanceID]*actorState
	// provisional starts true; set false when run_end or session_end is seen.
	provisional bool
}

// actorState holds mutable state for one actor within a run.
type actorState struct {
	actor       domain.ActorRef
	invocations []domain.InvocationUsage
}

// Aggregator accumulates decoded events into a domain.Aggregate.
//
// Binding attribution rules, enforced here and nowhere else:
//   - A subagent instance's usage comes from its invocation_end events ONLY.
//     Subagent turn events also carry token_usage for the same work and are
//     deliberately excluded; summing both double-counts.
//   - The orchestrator's usage comes from turn events on the orchestrator stream
//     ONLY. Orchestrator turns are not wrapped in invocation pairs.
//   - invocation_start supplies agent_type and carries no usage.
//   - Where a folder-derived instance hint disagrees with the agent_instance_id on
//     the event, the EVENT FIELD WINS.
//   - The unattributable bucket accumulates through the same code path but lands
//     in Aggregate.Unattributable, never in Aggregate.Runs.
type Aggregator struct {
	runs           map[string]*runState // keyed by run ID for named runs
	unattributable *runState            // nil until first unattributable event
	findings       []domain.Finding
}

// NewAggregator returns a new Aggregator ready to receive events.
func NewAggregator() *Aggregator {
	return &Aggregator{
		runs: make(map[string]*runState),
	}
}

// Add feeds one decoded event with its stream provenance. Events that cannot be
// attributed to an actor produce a Finding rather than being dropped silently.
func (a *Aggregator) Add(ref domain.StreamRef, ev domain.Event) {
	rs := a.getOrCreateRunState(ref.Run)

	// Handle run-completion markers before any other processing.
	switch ev.Type {
	case domain.EventRunEnd, domain.EventSessionEnd:
		rs.provisional = false
		return
	}

	// Benign event types carry no attribution-relevant field blocks.
	// They must be silently discarded with no finding and no totals change.
	switch ev.Type {
	case domain.EventOther, domain.EventRunStart, domain.EventSessionStart:
		return
	}

	// Check harness quality for events we actually process.
	if ev.Harness != "" && !ev.Harness.IsRecognised() {
		a.findings = append(a.findings, domain.Finding{
			Kind:     domain.FindingUnrecognisedHarness,
			Severity: domain.SeverityWarning,
			Run:      ref.Run,
			Detail:   "unrecognised harness: " + string(ev.Harness),
		})
	}

	switch ref.Kind {
	case domain.StreamOrchestrator:
		a.handleOrchestratorEvent(rs, ref, ev)
	case domain.StreamAgentInstance:
		a.handleAgentEvent(rs, ref, ev)
	}
}

// Result returns the accumulated aggregate and every finding raised during
// attribution. Calling Result does not reset the aggregator.
func (a *Aggregator) Result() (domain.Aggregate, []domain.Finding) {
	// Sort named runs by run ID for deterministic output.
	runIDs := make([]string, 0, len(a.runs))
	for id := range a.runs {
		runIDs = append(runIDs, id)
	}
	sort.Strings(runIDs)

	runs := make([]domain.RunAggregate, 0, len(runIDs))
	for _, id := range runIDs {
		runs = append(runs, buildRunAggregate(a.runs[id]))
	}

	agg := domain.Aggregate{Runs: runs}

	if a.unattributable != nil {
		ua := buildRunAggregate(a.unattributable)
		agg.Unattributable = &ua
	}

	return agg, a.findings
}

// handleOrchestratorEvent processes an event on the orchestrator stream.
// Only turn events are attributed to the orchestrator; all others are ignored.
func (a *Aggregator) handleOrchestratorEvent(rs *runState, ref domain.StreamRef, ev domain.Event) {
	if ev.Type != domain.EventTurn {
		// invocation_start and invocation_end are subagent constructs and must
		// not be attributed to the orchestrator even when they arrive on its stream.
		return
	}

	if ev.Turn == nil {
		return
	}

	rs.orchestrator.invocations = append(rs.orchestrator.invocations, domain.InvocationUsage{
		Model:   ev.Turn.Model,
		Harness: ev.Harness,
		Usage:   ev.Turn.Usage,
	})
}

// handleAgentEvent processes an event on a subagent instance stream.
func (a *Aggregator) handleAgentEvent(rs *runState, ref domain.StreamRef, ev domain.Event) {
	switch ev.Type {
	case domain.EventInvocationStart:
		a.handleInvocationStart(rs, ref, ev)

	case domain.EventInvocationEnd:
		a.handleInvocationEnd(rs, ref, ev)

	case domain.EventTurn:
		// Subagent turn events carry the same token_usage as the surrounding
		// invocation_end and MUST NOT contribute to instance totals.
		return

	default:
		// Any other recognised event type that is not attribution-relevant is ignored.
		return
	}
}

// handleInvocationStart processes an invocation_start event.
// It supplies agent_type but carries no token usage.
func (a *Aggregator) handleInvocationStart(rs *runState, ref domain.StreamRef, ev domain.Event) {
	if ev.InvocationStart == nil {
		return
	}

	id := ev.InvocationStart.AgentInstanceID
	if id == "" {
		id = ref.InstanceHint
	}
	if id == "" {
		return
	}

	as := a.getOrCreateActorState(rs, id)
	if ev.InvocationStart.AgentType != "" {
		as.actor.Type = ev.InvocationStart.AgentType
	}
}

// handleInvocationEnd processes an invocation_end event, determining the actor
// identity, raising quality findings, and recording the invocation.
func (a *Aggregator) handleInvocationEnd(rs *runState, ref domain.StreamRef, ev domain.Event) {
	if ev.InvocationEnd == nil {
		return
	}

	end := ev.InvocationEnd

	// Determine the agent identity: event field wins over folder hint.
	id := end.AgentInstanceID
	if id == "" {
		id = ref.InstanceHint
	}
	if id == "" {
		// No usable identity — produce a finding rather than silently dropping.
		a.findings = append(a.findings, domain.Finding{
			Kind:     domain.FindingUnidentifiedEvent,
			Severity: domain.SeverityWarning,
			Run:      ref.Run,
			Detail:   "invocation_end with no usable agent_instance_id",
		})
		return
	}

	actor := domain.AgentInstance(id)

	// Quality findings do not halt aggregation; the invocation is still recorded.
	if !end.HasUsage {
		a.findings = append(a.findings, domain.Finding{
			Kind:     domain.FindingMissingTokenUsage,
			Severity: domain.SeverityWarning,
			Run:      ref.Run,
			Actor:    actor,
			Detail:   "invocation_end has no token_usage",
		})
	}

	if end.Model.IsEmpty() {
		a.findings = append(a.findings, domain.Finding{
			Kind:     domain.FindingMissingModel,
			Severity: domain.SeverityWarning,
			Run:      ref.Run,
			Actor:    actor,
			Detail:   "invocation_end has no model",
		})
	}

	as := a.getOrCreateActorState(rs, id)
	// Preserve the actor instance set at creation; only add the invocation record.
	as.invocations = append(as.invocations, domain.InvocationUsage{
		Model:   end.Model,
		Harness: ev.Harness,
		Usage:   end.Usage,
	})
}

// getOrCreateRunState returns the runState for the given RunRef, creating it if absent.
func (a *Aggregator) getOrCreateRunState(run domain.RunRef) *runState {
	if run.IsUnattributable() {
		if a.unattributable == nil {
			a.unattributable = newRunState(run)
		}
		return a.unattributable
	}

	rs, ok := a.runs[run.ID]
	if !ok {
		rs = newRunState(run)
		a.runs[run.ID] = rs
	}
	return rs
}

// newRunState constructs a fresh runState, initialized as provisional.
func newRunState(run domain.RunRef) *runState {
	rs := &runState{
		run:         run,
		agents:      make(map[domain.AgentInstanceID]*actorState),
		provisional: true,
	}
	rs.orchestrator = actorState{actor: domain.Orchestrator()}
	return rs
}

// getOrCreateActorState returns the actorState for the given agent ID within
// the run, creating it if absent.
func (a *Aggregator) getOrCreateActorState(rs *runState, id domain.AgentInstanceID) *actorState {
	as, ok := rs.agents[id]
	if !ok {
		as = &actorState{actor: domain.AgentInstance(id)}
		rs.agents[id] = as
	}
	return as
}

// buildRunAggregate converts a runState into a domain.RunAggregate.
// Agents are sorted by instance ID for deterministic output.
func buildRunAggregate(rs *runState) domain.RunAggregate {
	agentIDs := make([]domain.AgentInstanceID, 0, len(rs.agents))
	for id := range rs.agents {
		agentIDs = append(agentIDs, id)
	}
	sort.Slice(agentIDs, func(i, j int) bool {
		return string(agentIDs[i]) < string(agentIDs[j])
	})

	agents := make([]domain.ActorAggregate, 0, len(agentIDs))
	for _, id := range agentIDs {
		as := rs.agents[id]
		agents = append(agents, domain.ActorAggregate{
			Actor:       as.actor,
			Invocations: as.invocations,
		})
	}

	return domain.RunAggregate{
		Run:         rs.run,
		Provisional: rs.provisional,
		Orchestrator: domain.ActorAggregate{
			Actor:       rs.orchestrator.actor,
			Invocations: rs.orchestrator.invocations,
		},
		Agents: agents,
	}
}

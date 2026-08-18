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
	// recordOwners maps record_id to the actor that first presented it within
	// this run. Consulted before per-actor supersede to enforce the
	// single-stream invariant: a record_id may only count toward one stream.
	recordOwners map[string]domain.ActorRef
	// crossStreamReported is the set of record_ids for which exactly one
	// FindingCrossStreamRecord has already been raised. Subsequent foreign
	// observations of the same record_id produce no additional finding.
	crossStreamReported map[string]bool
}

// actorState holds mutable state for one actor within a run.
type actorState struct {
	actor       domain.ActorRef
	invocations []domain.InvocationUsage // legacy: turn / invocation_end sampled usage
	records     []domain.InvocationUsage // per-record usage, file order preserved
	recordIndex map[string]int           // record_id → index into records; last-wins supersede
}

// effective returns the invocation records that count toward this actor's
// totals: the per-record set when non-empty, otherwise the legacy set. The
// two are never combined — this is decision D2, the per-actor precedence
// rule between usage_record and the legacy sampled usage.
func (s *actorState) effective() []domain.InvocationUsage {
	if len(s.records) > 0 {
		return s.records
	}
	return s.invocations
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
//   - A usage_record event is attributed by STREAM PROVENANCE alone: on the
//     orchestrator stream it contributes to the orchestrator total, on an agent
//     instance stream to that instance's total.
//   - Single-stream invariant (now verified, not trusted): a given record_id may
//     belong to exactly one stream per run. When a record_id is first observed
//     its owning actor is recorded in the run-scoped registry. Re-observation
//     on the SAME stream is ordinary deduplication (last-wins supersede) and
//     raises no finding. Observation from a DIFFERENT stream is a producer-side
//     contract violation: exactly one FindingCrossStreamRecord warning is raised
//     per duplicated record, and the duplicate is not added to the later actor's
//     total. First in feed order retains the record.
//   - Where an actor has at least one usage_record, its totals come from those
//     records ONLY and the sampled token_usage on its turn / invocation_end
//     events is ignored. Where it has none, the legacy rules above apply
//     unchanged. The two sources are never summed together.
//   - When the same record_id appears more than once on one actor's stream, the
//     LAST (most recent) observation supersedes all earlier ones. The record's
//     ordinal position among that actor's records is preserved and the record
//     count does not increase on re-observation. Re-observation is the expected
//     case and raises no finding. Records with an empty record_id cannot be
//     deduplicated and are each counted once, raising FindingUnidentifiedEvent.
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
// Only turn and usage_record events are attributed to the orchestrator; all
// others are ignored.
func (a *Aggregator) handleOrchestratorEvent(rs *runState, ref domain.StreamRef, ev domain.Event) {
	switch ev.Type {
	case domain.EventUsageRecord:
		a.handleUsageRecord(rs, ref, ev, &rs.orchestrator)
		return

	case domain.EventTurn:
		if ev.Turn == nil {
			return
		}
		rs.orchestrator.invocations = append(rs.orchestrator.invocations, domain.InvocationUsage{
			Model:   ev.Turn.Model,
			Harness: ev.Harness,
			Usage:   ev.Turn.Usage,
		})

	default:
		// invocation_start and invocation_end are subagent constructs and must
		// not be attributed to the orchestrator even when they arrive on its stream.
		return
	}
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

	case domain.EventUsageRecord:
		a.handleAgentUsageRecord(rs, ref, ev)

	default:
		// Any other recognised event type that is not attribution-relevant is ignored.
		return
	}
}

// handleAgentUsageRecord processes a usage_record event on a subagent instance
// stream, resolving the actor identity (event field wins over folder hint,
// same as invocation_end) and routing it into that actor's per-record total.
func (a *Aggregator) handleAgentUsageRecord(rs *runState, ref domain.StreamRef, ev domain.Event) {
	if ev.UsageRecord == nil {
		return
	}

	rec := ev.UsageRecord

	id := rec.AgentInstanceID
	if id == "" {
		id = ref.InstanceHint
	}
	if id == "" {
		a.findings = append(a.findings, domain.Finding{
			Kind:     domain.FindingUnidentifiedEvent,
			Severity: domain.SeverityWarning,
			Run:      ref.Run,
			Detail:   "usage_record with no usable agent_instance_id",
		})
		return
	}

	as := a.getOrCreateActorState(rs, id)
	a.handleUsageRecord(rs, ref, ev, as)
}

// handleUsageRecord applies two rules in order for each usage_record event:
//
//  1. Single-stream check (run-scoped): for a non-empty record_id, consult
//     the run's ownership registry. If the record is absent, register this
//     actor as owner and proceed to rule 2. If the record is already owned by
//     this same actor, it is ordinary re-observation — proceed to rule 2. If
//     the record is owned by a different actor, raise exactly one
//     FindingCrossStreamRecord (the first time only) and return without
//     contributing the record to this actor's total.
//
//  2. Supersede (per-actor): for a non-empty record_id already held by this
//     actor, replace the stored entry in place (last observation wins),
//     preserving file order and record count. For a record_id not yet held,
//     append in file order. For an empty record_id, append and raise
//     FindingUnidentifiedEvent.
func (a *Aggregator) handleUsageRecord(rs *runState, ref domain.StreamRef, ev domain.Event, as *actorState) {
	rec := ev.UsageRecord
	if rec == nil {
		return
	}

	entry := domain.InvocationUsage{
		Model:   rec.Model,
		Harness: ev.Harness,
		Usage:   rec.Usage,
	}

	if rec.RecordID == "" {
		// Empty record_id: cannot enter either registry or supersede map.
		a.findings = append(a.findings, domain.Finding{
			Kind:     domain.FindingUnidentifiedEvent,
			Severity: domain.SeverityWarning,
			Run:      ref.Run,
			Actor:    as.actor,
			Detail:   "usage_record with no record_id",
		})
		as.records = append(as.records, entry)
		return
	}

	// Rule 1: single-stream invariant check.
	if owner, owned := rs.recordOwners[rec.RecordID]; owned {
		if !sameActorIdentity(owner, as.actor) {
			// Cross-stream violation: raise at most one finding per record.
			if !rs.crossStreamReported[rec.RecordID] {
				rs.crossStreamReported[rec.RecordID] = true
				a.findings = append(a.findings, domain.Finding{
					Kind:     domain.FindingCrossStreamRecord,
					Severity: domain.SeverityWarning,
					Path:     ref.Path,
					Line:     0,
					Run:      ref.Run,
					Actor:    as.actor,
					Detail: "usage_record " + rec.RecordID +
						" already attributed to " + owner.Label() +
						"; duplicate on " + as.actor.Label() + " ignored",
				})
			}
			return
		}
		// Same actor: fall through to per-actor supersede.
	} else {
		// First time this record_id is seen in this run: register ownership.
		rs.recordOwners[rec.RecordID] = as.actor
	}

	// Rule 2: per-actor supersede.
	if as.recordIndex == nil {
		as.recordIndex = make(map[string]int)
	}
	if idx, seen := as.recordIndex[rec.RecordID]; seen {
		// Last observation wins: replace in place, preserving file order and count.
		as.records[idx] = entry
		return
	}

	// First observation for this actor: append and record its position.
	as.recordIndex[rec.RecordID] = len(as.records)
	as.records = append(as.records, entry)
}

// sameActorIdentity reports whether two ActorRefs identify the same stream
// owner. It compares only Kind and Instance, deliberately ignoring Type (the
// agent_type hint from invocation_start) which is not part of stream identity.
func sameActorIdentity(a, b domain.ActorRef) bool {
	return a.Kind == b.Kind && a.Instance == b.Instance
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
		run:                 run,
		agents:              make(map[domain.AgentInstanceID]*actorState),
		provisional:         true,
		recordOwners:        make(map[string]domain.ActorRef),
		crossStreamReported: make(map[string]bool),
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
			Invocations: as.effective(),
		})
	}

	return domain.RunAggregate{
		Run:         rs.run,
		Provisional: rs.provisional,
		Orchestrator: domain.ActorAggregate{
			Actor:       rs.orchestrator.actor,
			Invocations: rs.orchestrator.effective(),
		},
		Agents: agents,
	}
}

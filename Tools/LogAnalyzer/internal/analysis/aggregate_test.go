package analysis_test

// Tests for the analysis core: event attribution, per-category token rollup,
// model tracking, provisional-run detection, the unattributable bucket, and
// quality-finding accumulation.
//
// All tests exercise the pure Aggregate function using in-memory event streams.
// No filesystem, network, clock or randomness is involved anywhere in these tests.

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared fixtures and helpers
// ---------------------------------------------------------------------------

const testRunID = "20260101T000000Z-abcd"

var testRunRef = domain.NamedRun(testRunID)

// agentStream builds a StreamRef for a subagent instance on a named run.
func agentStream(run domain.RunRef, hint domain.AgentInstanceID) domain.StreamRef {
	return domain.StreamRef{
		Run:          run,
		Kind:         domain.StreamAgentInstance,
		InstanceHint: hint,
	}
}

// orchStream builds a StreamRef for the orchestrator stream on a named run.
func orchStream(run domain.RunRef) domain.StreamRef {
	return domain.StreamRef{
		Run:  run,
		Kind: domain.StreamOrchestrator,
	}
}

// invEndEvent constructs an invocation_end event with a given agent identity,
// model and usage. HasUsage is set to true when any category is present.
func invEndEvent(id domain.AgentInstanceID, model domain.ModelID, usage domain.TokenUsage) domain.Event {
	return domain.Event{
		Type: domain.EventInvocationEnd,
		InvocationEnd: &domain.InvocationEndFields{
			AgentInstanceID: id,
			Model:           model,
			Usage:           usage,
			HasUsage:        !usage.IsEmpty(),
		},
	}
}

// invEndEventNoUsage constructs an invocation_end whose token_usage key was
// entirely absent (HasUsage == false).
func invEndEventNoUsage(id domain.AgentInstanceID, model domain.ModelID) domain.Event {
	return domain.Event{
		Type: domain.EventInvocationEnd,
		InvocationEnd: &domain.InvocationEndFields{
			AgentInstanceID: id,
			Model:           model,
			Usage:           domain.TokenUsage{},
			HasUsage:        false,
		},
	}
}

// turnEvent constructs a turn event (assistant role by default).
func turnEvent(model domain.ModelID, usage domain.TokenUsage) domain.Event {
	return domain.Event{
		Type: domain.EventTurn,
		Turn: &domain.TurnFields{
			Role:     domain.TurnAssistant,
			Model:    model,
			Usage:    usage,
			HasUsage: !usage.IsEmpty(),
		},
	}
}

// runEndEvent constructs a run_end event.
func runEndEvent() domain.Event { return domain.Event{Type: domain.EventRunEnd} }

// sessionEndEvent constructs a session_end event.
func sessionEndEvent() domain.Event { return domain.Event{Type: domain.EventSessionEnd} }

// oneAgentStream is a shorthand for a single-agent analysis input.
func oneAgentStream(run domain.RunRef, id domain.AgentInstanceID, events ...domain.Event) analysis.Input {
	return analysis.Input{
		Streams: []analysis.Stream{
			{Ref: agentStream(run, id), Events: events},
		},
	}
}

// requireOneRun fails the test if agg does not contain exactly one named run and
// returns that run.
func requireOneRun(t *testing.T, agg domain.Aggregate) domain.RunAggregate {
	t.Helper()
	if len(agg.Runs) != 1 {
		t.Fatalf("got %d named runs, want 1", len(agg.Runs))
	}
	return agg.Runs[0]
}

// requireOneAgent fails the test if the run does not contain exactly one agent
// and returns that agent aggregate.
func requireOneAgent(t *testing.T, run domain.RunAggregate) domain.ActorAggregate {
	t.Helper()
	if len(run.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(run.Agents))
	}
	return run.Agents[0]
}

// assertTokenPresent asserts that tc is present and equal to want.
func assertTokenPresent(t *testing.T, name string, tc domain.TokenCount, want int64) {
	t.Helper()
	v, ok := tc.Value()
	if !ok {
		t.Errorf("%s: absent, want present(%d)", name, want)
		return
	}
	if v != want {
		t.Errorf("%s = %d, want %d", name, v, want)
	}
}

// assertTokenAbsent asserts that tc is absent.
func assertTokenAbsent(t *testing.T, name string, tc domain.TokenCount) {
	t.Helper()
	if tc.IsPresent() {
		v, _ := tc.Value()
		t.Errorf("%s = %d, want absent", name, v)
	}
}

// hasFindingKind reports whether findings contains at least one entry with the
// given kind.
func hasFindingKind(findings []domain.Finding, kind domain.FindingKind) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Attribution — invocation_end events
// ---------------------------------------------------------------------------

func TestAggregate_InvocationEnd_AttributesToNamedAgentInstance(t *testing.T) {
	agentID := domain.AgentInstanceID("TestAgent#1")
	usage := domain.TokenUsage{
		Input:  domain.Tokens(100),
		Output: domain.Tokens(50),
	}

	agg, findings := analysis.Aggregate(oneAgentStream(
		testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", usage),
	))

	if len(findings) > 0 {
		t.Errorf("got %d unexpected findings: %v", len(findings), findings)
	}

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	if agent.Actor.Instance != agentID {
		t.Errorf("agent instance = %q, want %q", agent.Actor.Instance, agentID)
	}

	totals := agent.Totals()
	assertTokenPresent(t, "Input", totals.Input, 100)
	assertTokenPresent(t, "Output", totals.Output, 50)
}

func TestAggregate_EventFieldWinsOverFolderHint(t *testing.T) {
	// Where the folder-derived hint and the agent_instance_id in the event
	// disagree, the event field is authoritative.
	hintID := domain.AgentInstanceID("FolderDerivedHint#1")
	actualID := domain.AgentInstanceID("ActualEventID#99")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref:    agentStream(testRunRef, hintID), // hint says FolderDerivedHint#1
				Events: []domain.Event{
					invEndEvent(actualID, "claude-sonnet", // event says ActualEventID#99
						domain.TokenUsage{Input: domain.Tokens(10)}),
				},
			},
		},
	})

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	if agent.Actor.Instance != actualID {
		t.Errorf("agent instance = %q, want %q (event field must win over folder hint)",
			agent.Actor.Instance, actualID)
	}
}

func TestAggregate_OrchestratorTurnAttributesToOrchestrator(t *testing.T) {
	// Turn events on the orchestrator stream are attributed to the orchestrator,
	// not to any agent instance.
	usage := domain.TokenUsage{
		Input:  domain.Tokens(200),
		Output: domain.Tokens(100),
	}

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref:    orchStream(testRunRef),
				Events: []domain.Event{turnEvent("claude-sonnet", usage)},
			},
		},
	})

	run := requireOneRun(t, agg)

	orchTotals := run.Orchestrator.Totals()
	assertTokenPresent(t, "Orchestrator Input", orchTotals.Input, 200)
	assertTokenPresent(t, "Orchestrator Output", orchTotals.Output, 100)

	if len(run.Agents) != 0 {
		t.Errorf("got %d agents, want 0 — orchestrator turns must not create agent entries",
			len(run.Agents))
	}
}

func TestAggregate_SubagentTurnExcludedFromInstanceTotal(t *testing.T) {
	// A subagent's turn event carries the same token_usage as its invocation_end.
	// The turn must NOT be summed into instance totals; only invocation_end counts.
	agentID := domain.AgentInstanceID("Subagent#1")

	turnUsage := domain.TokenUsage{
		Input:  domain.Tokens(999),
		Output: domain.Tokens(999),
	}
	endUsage := domain.TokenUsage{
		Input:  domain.Tokens(100),
		Output: domain.Tokens(50),
	}

	agg, _ := analysis.Aggregate(oneAgentStream(
		testRunRef, agentID,
		turnEvent("claude-sonnet", turnUsage),          // must NOT be counted
		invEndEvent(agentID, "claude-sonnet", endUsage), // ONLY this counts
	))

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)
	totals := agent.Totals()

	assertTokenPresent(t, "Input (no double-count)", totals.Input, 100)
	assertTokenPresent(t, "Output (no double-count)", totals.Output, 50)
}

func TestAggregate_MultipleSubagentTurnsDoNotDoubleCount(t *testing.T) {
	// Multiple turn events (both user and assistant) in a subagent stream —
	// none may contribute to the instance total.
	agentID := domain.AgentInstanceID("Subagent#2")

	in := oneAgentStream(testRunRef, agentID,
		domain.Event{
			Type: domain.EventTurn,
			Turn: &domain.TurnFields{
				Role:     domain.TurnUser,
				Model:    "claude-sonnet",
				Usage:    domain.TokenUsage{Input: domain.Tokens(500)},
				HasUsage: true,
			},
		},
		domain.Event{
			Type: domain.EventTurn,
			Turn: &domain.TurnFields{
				Role:     domain.TurnAssistant,
				Model:    "claude-sonnet",
				Usage:    domain.TokenUsage{Output: domain.Tokens(500)},
				HasUsage: true,
			},
		},
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{
			Input:  domain.Tokens(200),
			Output: domain.Tokens(80),
		}),
	)

	agg, _ := analysis.Aggregate(in)

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)
	totals := agent.Totals()

	assertTokenPresent(t, "Input (no double-count)", totals.Input, 200)
	assertTokenPresent(t, "Output (no double-count)", totals.Output, 80)
}

func TestAggregate_MultipleInvocationEnds_AreAllCounted(t *testing.T) {
	// An agent can have multiple invocation_end events; all must be included
	// in the instance total (each is a separate invocation, not a duplicate).
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(200)}),
	))

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	assertTokenPresent(t, "Input", agent.Totals().Input, 300)

	if len(agent.Invocations) != 2 {
		t.Errorf("got %d invocations, want 2", len(agent.Invocations))
	}
}

func TestAggregate_InvocationStart_SuppliesAgentTypeWithNoUsage(t *testing.T) {
	// invocation_start carries agent_type but no token_usage and must not
	// contribute to instance totals.
	agentID := domain.AgentInstanceID("Agent#1")

	in := oneAgentStream(testRunRef, agentID,
		domain.Event{
			Type: domain.EventInvocationStart,
			InvocationStart: &domain.InvocationStartFields{
				AgentInstanceID: agentID,
				AgentType:       "planner",
			},
		},
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
	)

	agg, _ := analysis.Aggregate(in)

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	// Only the invocation_end contributes to the total.
	assertTokenPresent(t, "Input", agent.Totals().Input, 100)

	if len(agent.Invocations) != 1 {
		t.Errorf("got %d invocations, want 1 (invocation_start must not add an invocation)",
			len(agent.Invocations))
	}
}

func TestAggregate_UnidentifiableEvent_ProducesFinding(t *testing.T) {
	// An invocation_end with an empty agent_instance_id and no stream-level hint
	// cannot be attributed to any actor. It must produce a finding rather than
	// being silently dropped.
	in := analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: domain.StreamRef{
					Run:          testRunRef,
					Kind:         domain.StreamAgentInstance,
					InstanceHint: "", // no folder-derived hint
				},
				Events: []domain.Event{
					invEndEvent("", "claude-sonnet", // empty agent_instance_id
						domain.TokenUsage{Input: domain.Tokens(50)}),
				},
			},
		},
	}

	_, findings := analysis.Aggregate(in)

	if !hasFindingKind(findings, domain.FindingUnidentifiedEvent) {
		t.Errorf("expected FindingUnidentifiedEvent for event with no usable identity; got: %v",
			findings)
	}
}

func TestAggregate_EmptyEventField_FallsBackToInstanceHint(t *testing.T) {
	// When agent_instance_id on the event is empty but the stream-level
	// InstanceHint (derived from the folder name) is present, the hint must be
	// used for attribution. The hint exists precisely to recover identity from
	// folder structure when the event field is absent. Raising
	// FindingUnidentifiedEvent in this case would silently discard attributable
	// usage, which is the misattribution hazard this stage is designed to prevent.
	hintID := domain.AgentInstanceID("FolderDerivedHint#1")

	in := analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: domain.StreamRef{
					Run:          testRunRef,
					Kind:         domain.StreamAgentInstance,
					InstanceHint: hintID, // folder-derived hint is present
				},
				Events: []domain.Event{
					invEndEvent("", "claude-sonnet", // event field is empty
						domain.TokenUsage{Input: domain.Tokens(75)}),
				},
			},
		},
	}

	agg, findings := analysis.Aggregate(in)

	// The hint must supply the identity; no FindingUnidentifiedEvent should appear.
	if hasFindingKind(findings, domain.FindingUnidentifiedEvent) {
		t.Error("got FindingUnidentifiedEvent, but event should be attributed via InstanceHint when AgentInstanceID is empty and hint is present")
	}

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	if agent.Actor.Instance != hintID {
		t.Errorf("agent instance = %q, want %q (InstanceHint must be used when event field is empty)",
			agent.Actor.Instance, hintID)
	}

	assertTokenPresent(t, "Input", agent.Totals().Input, 75)
}

func TestAggregate_BenignEventTypes_AreIgnored(t *testing.T) {
	// EventOther, EventRunStart and EventSessionStart carry no attribution-relevant
	// field block and must be silently discarded: no finding, no totals change, no
	// crash. An implementer's type-switch could plausibly mis-route these unhandled
	// types into FindingUnidentifiedEvent when they should produce no finding at all.
	//
	// A real invocation_end is included alongside the benign events to confirm the
	// aggregator processed the stream at all. This anchors the test so it fails in
	// RED (the stub returns no runs) and verifies that benign events do not inflate
	// or corrupt the real event's totals.
	agentID := domain.AgentInstanceID("Agent#1")

	in := analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(testRunRef, agentID),
				Events: []domain.Event{
					{Type: domain.EventOther},
					{Type: domain.EventRunStart},
					{Type: domain.EventSessionStart},
					invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
				},
			},
		},
	}

	agg, findings := analysis.Aggregate(in)

	// Benign event types must not produce FindingUnidentifiedEvent.
	if hasFindingKind(findings, domain.FindingUnidentifiedEvent) {
		t.Error("benign event types (EventOther/RunStart/SessionStart) must not produce FindingUnidentifiedEvent")
	}

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	// Only the invocation_end may contribute; the total must equal exactly its usage.
	assertTokenPresent(t, "Input", agent.Totals().Input, 100)
}

func TestAggregate_InvocationEndOnOrchestratorStream_DoesNotMisAttributeToOrchestrator(t *testing.T) {
	// An invocation_end arriving on the orchestrator stream is an unexpected input
	// shape (invocation pairs are a subagent construct). It must not be silently
	// folded into the orchestrator actor's usage totals, because that would distort
	// the orchestrator's cost figures. The implementation may ignore it or raise a
	// finding, but must not attribute it to Orchestrator().
	//
	// A turn event is included to establish that the orchestrator stream is processed
	// at all (anchors RED-phase failure) and to give the test a known baseline total
	// to compare against.
	agentID := domain.AgentInstanceID("SomeAgent#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: orchStream(testRunRef),
				Events: []domain.Event{
					turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(10)}), // legitimate orchestrator usage
					invEndEvent(agentID, "claude-sonnet",
						domain.TokenUsage{Input: domain.Tokens(500)}), // must NOT be attributed to orchestrator
				},
			},
		},
	})

	run := requireOneRun(t, agg)
	orchTotals := run.Orchestrator.Totals()

	// The orchestrator total must reflect only the turn event (10 tokens), not
	// the invocation_end (500 tokens).
	if v, ok := orchTotals.Input.Value(); ok && v >= 500 {
		t.Errorf("orchestrator Input = %d, but an invocation_end on the orchestrator stream must not be attributed to the orchestrator (only the turn's 10 tokens belong here)",
			v)
	}
}

// ---------------------------------------------------------------------------
// Per-category rollup — absent preservation
// ---------------------------------------------------------------------------

func TestAggregate_AllFourCategories_TotalsCorrect(t *testing.T) {
	agentID := domain.AgentInstanceID("Agent#1")
	usage := domain.TokenUsage{
		Input:         domain.Tokens(100),
		CacheRead:     domain.Tokens(50),
		CacheCreation: domain.Tokens(25),
		Output:        domain.Tokens(200),
	}

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", usage),
	))

	totals := requireOneAgent(t, requireOneRun(t, agg)).Totals()
	assertTokenPresent(t, "Input", totals.Input, 100)
	assertTokenPresent(t, "CacheRead", totals.CacheRead, 50)
	assertTokenPresent(t, "CacheCreation", totals.CacheCreation, 25)
	assertTokenPresent(t, "Output", totals.Output, 200)
}

func TestAggregate_AbsentCategoryInEveryInvocation_StaysAbsent(t *testing.T) {
	// A category absent from every contributing event must remain absent in the
	// instance total — it must never collapse to zero.
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{
			Input:  domain.Tokens(100),
			Output: domain.Tokens(50),
			// CacheRead and CacheCreation absent
		}),
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{
			Input:  domain.Tokens(200),
			Output: domain.Tokens(100),
			// CacheRead and CacheCreation absent
		}),
	))

	totals := requireOneAgent(t, requireOneRun(t, agg)).Totals()

	assertTokenAbsent(t, "CacheRead", totals.CacheRead)
	assertTokenAbsent(t, "CacheCreation", totals.CacheCreation)
	assertTokenPresent(t, "Input", totals.Input, 300)
	assertTokenPresent(t, "Output", totals.Output, 150)
}

func TestAggregate_MixedAbsentPresent_TotalsOnlyThePresentOnes(t *testing.T) {
	// When one invocation supplies a category and another omits it, the total
	// must include only the present value (absent treated as contributing nothing,
	// not as zero).
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{
			Input:     domain.Tokens(100),
			CacheRead: domain.Tokens(30), // present in this invocation
			Output:    domain.Tokens(50),
		}),
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{
			Input:  domain.Tokens(200),
			Output: domain.Tokens(100),
			// CacheRead absent in this invocation
		}),
	))

	totals := requireOneAgent(t, requireOneRun(t, agg)).Totals()

	// present(30) + absent = present(30), never zero.
	assertTokenPresent(t, "CacheRead", totals.CacheRead, 30)
	assertTokenPresent(t, "Input", totals.Input, 300)
	assertTokenPresent(t, "Output", totals.Output, 150)
	assertTokenAbsent(t, "CacheCreation", totals.CacheCreation)
}

func TestAggregate_RunTotals_SumAcrossOrchestratorAndAgents(t *testing.T) {
	agentA := domain.AgentInstanceID("AgentA#1")
	agentB := domain.AgentInstanceID("AgentB#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: orchStream(testRunRef),
				Events: []domain.Event{
					turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(50)}),
				},
			},
			{
				Ref: agentStream(testRunRef, agentA),
				Events: []domain.Event{
					invEndEvent(agentA, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
				},
			},
			{
				Ref: agentStream(testRunRef, agentB),
				Events: []domain.Event{
					invEndEvent(agentB, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(200)}),
				},
			},
		},
	})

	run := requireOneRun(t, agg)
	runTotals := run.Totals()

	assertTokenPresent(t, "run Input", runTotals.Input, 350)
}

func TestAggregate_PerAgentTotals_AreIndependent(t *testing.T) {
	agentA := domain.AgentInstanceID("AgentA#1")
	agentB := domain.AgentInstanceID("AgentB#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(testRunRef, agentA),
				Events: []domain.Event{
					invEndEvent(agentA, "claude-sonnet", domain.TokenUsage{
						Input:  domain.Tokens(100),
						Output: domain.Tokens(40),
					}),
				},
			},
			{
				Ref: agentStream(testRunRef, agentB),
				Events: []domain.Event{
					invEndEvent(agentB, "claude-sonnet", domain.TokenUsage{
						Input:  domain.Tokens(200),
						Output: domain.Tokens(80),
					}),
				},
			},
		},
	})

	run := requireOneRun(t, agg)

	if len(run.Agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(run.Agents))
	}

	// Verify per-agent independence by checking total.
	var totalInput int64
	for _, agent := range run.Agents {
		v, ok := agent.Totals().Input.Value()
		if !ok {
			t.Errorf("agent %q: Input absent", agent.Actor.Instance)
			continue
		}
		totalInput += v
	}
	if totalInput != 300 {
		t.Errorf("sum of per-agent Input = %d, want 300", totalInput)
	}
}

// ---------------------------------------------------------------------------
// Model attribution — per instance and per run
// ---------------------------------------------------------------------------

func TestAggregate_SingleAgent_MultipleModels_AllTracked(t *testing.T) {
	// A single agent instance that used different models across invocations must
	// retain per-invocation records so pricing (which is per model) can be applied.
	agentID := domain.AgentInstanceID("Agent#1")
	model1 := domain.ModelID("claude-sonnet")
	model2 := domain.ModelID("claude-haiku")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, model1, domain.TokenUsage{Input: domain.Tokens(100)}),
		invEndEvent(agentID, model2, domain.TokenUsage{Input: domain.Tokens(200)}),
		invEndEvent(agentID, model1, domain.TokenUsage{Input: domain.Tokens(50)}),
	))

	agent := requireOneAgent(t, requireOneRun(t, agg))

	if len(agent.Invocations) != 3 {
		t.Errorf("got %d invocations, want 3 (per-invocation records must be retained)",
			len(agent.Invocations))
	}

	models := agent.Models()
	if len(models) != 2 {
		t.Errorf("got %d distinct models, want 2: %v", len(models), models)
	}
}

func TestAggregate_AgentModels_AreSorted(t *testing.T) {
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "z-model", domain.TokenUsage{Input: domain.Tokens(1)}),
		invEndEvent(agentID, "a-model", domain.TokenUsage{Input: domain.Tokens(1)}),
		invEndEvent(agentID, "m-model", domain.TokenUsage{Input: domain.Tokens(1)}),
	))

	models := requireOneAgent(t, requireOneRun(t, agg)).Models()

	for i := 1; i < len(models); i++ {
		if string(models[i-1]) > string(models[i]) {
			t.Errorf("models not sorted: %v", models)
			break
		}
	}
}

func TestAggregate_EmptyModelID_ExcludedFromModelList(t *testing.T) {
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "", domain.TokenUsage{Input: domain.Tokens(100)}), // no model
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(50)}),
	))

	models := requireOneAgent(t, requireOneRun(t, agg)).Models()

	for _, m := range models {
		if m.IsEmpty() {
			t.Error("empty model ID must be excluded from the model list")
		}
	}

	if len(models) != 1 {
		t.Errorf("got %d distinct non-empty models, want 1: %v", len(models), models)
	}
}

func TestAggregate_RunModels_DistinctAcrossAllActors(t *testing.T) {
	agentA := domain.AgentInstanceID("AgentA#1")
	agentB := domain.AgentInstanceID("AgentB#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref:    orchStream(testRunRef),
				Events: []domain.Event{turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)})},
			},
			{
				Ref:    agentStream(testRunRef, agentA),
				Events: []domain.Event{invEndEvent(agentA, "claude-haiku", domain.TokenUsage{Input: domain.Tokens(50)})},
			},
			{
				Ref:    agentStream(testRunRef, agentB),
				Events: []domain.Event{invEndEvent(agentB, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(75)})},
			},
		},
	})

	// Run-level model set: claude-sonnet (orchestrator + agentB) and claude-haiku (agentA)
	// Distinct means only 2 entries even though claude-sonnet appears twice.
	runModels := requireOneRun(t, agg).Models()

	if len(runModels) != 2 {
		t.Errorf("got %d distinct run models, want 2: %v", len(runModels), runModels)
	}
}

func TestAggregate_AggregateModels_DistinctAcrossAllRuns(t *testing.T) {
	run1 := domain.NamedRun("20260101T000000Z-aaaa")
	run2 := domain.NamedRun("20260101T000000Z-bbbb")
	agent1 := domain.AgentInstanceID("Agent#1")
	agent2 := domain.AgentInstanceID("Agent#1") // same name, different run

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref:    agentStream(run1, agent1),
				Events: []domain.Event{invEndEvent(agent1, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(1)})},
			},
			{
				Ref:    agentStream(run2, agent2),
				Events: []domain.Event{invEndEvent(agent2, "claude-haiku", domain.TokenUsage{Input: domain.Tokens(1)})},
			},
		},
	})

	allModels := agg.Models()

	if len(allModels) != 2 {
		t.Errorf("got %d distinct aggregate models, want 2: %v", len(allModels), allModels)
	}
}

// ---------------------------------------------------------------------------
// Provisional-run detection
// ---------------------------------------------------------------------------

func TestAggregate_NoRunEnd_NoSessionEnd_IsProvisional(t *testing.T) {
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
		// no run_end or session_end
	))

	run := requireOneRun(t, agg)
	if !run.Provisional {
		t.Error("run without run_end or session_end must be flagged Provisional")
	}
}

func TestAggregate_WithRunEnd_IsNotProvisional(t *testing.T) {
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
		runEndEvent(),
	))

	run := requireOneRun(t, agg)
	if run.Provisional {
		t.Error("run with run_end must not be flagged Provisional")
	}
}

func TestAggregate_WithSessionEnd_IsNotProvisional(t *testing.T) {
	agentID := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
		sessionEndEvent(),
	))

	run := requireOneRun(t, agg)
	if run.Provisional {
		t.Error("run with session_end must not be flagged Provisional")
	}
}

func TestAggregate_RunEndOnOrchestratorStream_IsNotProvisional(t *testing.T) {
	// run_end may appear on the orchestrator stream, not just agent streams.
	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: orchStream(testRunRef),
				Events: []domain.Event{
					turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
					runEndEvent(),
				},
			},
		},
	})

	run := requireOneRun(t, agg)
	if run.Provisional {
		t.Error("run with run_end on orchestrator stream must not be flagged Provisional")
	}
}

func TestAggregate_SessionEndOnOrchestratorStream_IsNotProvisional(t *testing.T) {
	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: orchStream(testRunRef),
				Events: []domain.Event{
					turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
					sessionEndEvent(),
				},
			},
		},
	})

	run := requireOneRun(t, agg)
	if run.Provisional {
		t.Error("run with session_end on orchestrator stream must not be flagged Provisional")
	}
}

func TestAggregate_MultipleRuns_ProvisionalFlagIsPerRun(t *testing.T) {
	// Provisional detection is per-run; a run_end in one run must not affect
	// another run's provisional flag.
	run1 := domain.NamedRun("20260101T000000Z-aaaa")
	run2 := domain.NamedRun("20260101T000000Z-bbbb")
	agent1 := domain.AgentInstanceID("Agent#1")
	agent2 := domain.AgentInstanceID("Agent#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(run1, agent1),
				Events: []domain.Event{
					invEndEvent(agent1, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(1)}),
					runEndEvent(), // run1 is complete
				},
			},
			{
				Ref: agentStream(run2, agent2),
				Events: []domain.Event{
					invEndEvent(agent2, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(1)}),
					// no run_end — run2 is provisional
				},
			},
		},
	})

	if len(agg.Runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(agg.Runs))
	}

	for _, run := range agg.Runs {
		switch run.Run.ID {
		case "20260101T000000Z-aaaa":
			if run.Provisional {
				t.Error("run1 (has run_end) must not be provisional")
			}
		case "20260101T000000Z-bbbb":
			if !run.Provisional {
				t.Error("run2 (no run_end or session_end) must be provisional")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Unattributable (unknown-run) bucket
// ---------------------------------------------------------------------------

func TestAggregate_UnattributableStream_LandsInSeparateBucket(t *testing.T) {
	unattribRef := domain.UnattributableRun()
	namedRef := domain.NamedRun(testRunID)
	namedAgent := domain.AgentInstanceID("Named#1")
	unattribAgent := domain.AgentInstanceID("Unknown#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(namedRef, namedAgent),
				Events: []domain.Event{
					invEndEvent(namedAgent, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
				},
			},
			{
				Ref: agentStream(unattribRef, unattribAgent),
				Events: []domain.Event{
					invEndEvent(unattribAgent, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(999)}),
				},
			},
		},
	})

	if len(agg.Runs) != 1 {
		t.Errorf("got %d named runs, want 1", len(agg.Runs))
	}

	if agg.Unattributable == nil {
		t.Fatal("expected non-nil Unattributable bucket")
	}
}

func TestAggregate_UnattributableNotFoldedIntoNamedRunTotals(t *testing.T) {
	// Unattributable usage must be completely isolated from named-run totals.
	namedRef := domain.NamedRun(testRunID)
	unattribRef := domain.UnattributableRun()
	namedAgent := domain.AgentInstanceID("Named#1")
	unattribAgent := domain.AgentInstanceID("Unattrib#1")

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(namedRef, namedAgent),
				Events: []domain.Event{
					invEndEvent(namedAgent, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}),
				},
			},
			{
				Ref: agentStream(unattribRef, unattribAgent),
				Events: []domain.Event{
					invEndEvent(unattribAgent, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(99999)}),
				},
			},
		},
	})

	if len(agg.Runs) != 1 {
		t.Fatalf("got %d named runs, want 1", len(agg.Runs))
	}

	namedTotal := agg.Runs[0].Totals()
	if v, ok := namedTotal.Input.Value(); !ok || v != 100 {
		t.Errorf("named-run Input = (%d, %v), want (100, true) — unattributable must not be included",
			v, ok)
	}

	if agg.Unattributable == nil {
		t.Fatal("expected non-nil Unattributable bucket")
	}

	unattribTotal := agg.Unattributable.Totals()
	assertTokenPresent(t, "Unattributable Input", unattribTotal.Input, 99999)
}

func TestAggregate_OnlyUnattributableStream_NoNamedRuns(t *testing.T) {
	unattribRef := domain.UnattributableRun()
	agentID := domain.AgentInstanceID("Unknown#1")

	agg, _ := analysis.Aggregate(oneAgentStream(unattribRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(50)}),
	))

	if len(agg.Runs) != 0 {
		t.Errorf("got %d named runs, want 0", len(agg.Runs))
	}

	if agg.Unattributable == nil {
		t.Fatal("expected non-nil Unattributable bucket")
	}

	assertTokenPresent(t, "Unattributable Input", agg.Unattributable.Totals().Input, 50)
}

func TestAggregate_UnattributableRef_IsNotRunNamed(t *testing.T) {
	// The unattributable bucket's RunRef must have Kind == RunUnattributable,
	// never RunNamed.
	unattribRef := domain.UnattributableRun()
	agentID := domain.AgentInstanceID("Unknown#1")

	agg, _ := analysis.Aggregate(oneAgentStream(unattribRef, agentID,
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(10)}),
	))

	if agg.Unattributable == nil {
		t.Fatal("expected non-nil Unattributable bucket")
	}

	if !agg.Unattributable.Run.IsUnattributable() {
		t.Errorf("Unattributable bucket RunRef.Kind = %v, want RunUnattributable",
			agg.Unattributable.Run.Kind)
	}
}

func TestAggregate_OrchestratorStreamOnUnattributableRun(t *testing.T) {
	// The orchestrator stream can also be unattributable.
	unattribRef := domain.UnattributableRun()

	agg, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: orchStream(unattribRef),
				Events: []domain.Event{
					turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(75)}),
				},
			},
		},
	})

	if len(agg.Runs) != 0 {
		t.Errorf("got %d named runs, want 0", len(agg.Runs))
	}

	if agg.Unattributable == nil {
		t.Fatal("expected non-nil Unattributable bucket for unattributable orchestrator stream")
	}
}

// ---------------------------------------------------------------------------
// Quality-finding accumulation
// ---------------------------------------------------------------------------

func TestAggregate_MissingTokenUsage_ProducesFinding(t *testing.T) {
	// An invocation_end where the token_usage key was entirely absent
	// (HasUsage == false) must produce a FindingMissingTokenUsage.
	agentID := domain.AgentInstanceID("Agent#1")

	_, findings := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEventNoUsage(agentID, "claude-sonnet"),
	))

	if !hasFindingKind(findings, domain.FindingMissingTokenUsage) {
		t.Errorf("expected FindingMissingTokenUsage for invocation_end with absent token_usage; got: %v",
			findings)
	}
}

func TestAggregate_MissingModel_ProducesFinding(t *testing.T) {
	// An invocation_end with an empty model field must produce a
	// FindingMissingModel.
	agentID := domain.AgentInstanceID("Agent#1")

	_, findings := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "", domain.TokenUsage{Input: domain.Tokens(100)}),
	))

	if !hasFindingKind(findings, domain.FindingMissingModel) {
		t.Errorf("expected FindingMissingModel for invocation_end with empty model; got: %v",
			findings)
	}
}

func TestAggregate_MissingModel_UsageStillCountedInTotals(t *testing.T) {
	// An invocation_end with an empty model field raises FindingMissingModel, but
	// the invocation's token usage must still be included in the instance totals.
	// This pins "findings don't drop data" for the missing-model case, consistent
	// with the missing-usage case covered by TestAggregate_FindingsDoNotHaltAggregation.
	agentID := domain.AgentInstanceID("Agent#1")

	agg, findings := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, "", domain.TokenUsage{Input: domain.Tokens(100)}),
	))

	if !hasFindingKind(findings, domain.FindingMissingModel) {
		t.Errorf("expected FindingMissingModel for invocation_end with empty model; got: %v",
			findings)
	}

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)

	// The finding must not suppress the invocation's token count.
	assertTokenPresent(t, "Input", agent.Totals().Input, 100)
}

func TestAggregate_UnrecognisedHarness_ProducesFinding(t *testing.T) {
	// An event carrying an unrecognised harness value must produce a
	// FindingUnrecognisedHarness. The event is still processed; the finding
	// communicates the data-quality issue without halting aggregation.
	agentID := domain.AgentInstanceID("Agent#1")

	in := oneAgentStream(testRunRef, agentID,
		domain.Event{
			Type:    domain.EventInvocationEnd,
			Harness: domain.Harness("totally-unknown-harness"),
			InvocationEnd: &domain.InvocationEndFields{
				AgentInstanceID: agentID,
				Model:           "claude-sonnet",
				Usage:           domain.TokenUsage{Input: domain.Tokens(100)},
				HasUsage:        true,
			},
		},
	)

	_, findings := analysis.Aggregate(in)

	if !hasFindingKind(findings, domain.FindingUnrecognisedHarness) {
		t.Errorf("expected FindingUnrecognisedHarness for unrecognised harness; got: %v",
			findings)
	}
}

func TestAggregate_FindingsDoNotHaltAggregation(t *testing.T) {
	// A bad event (missing usage) must not prevent subsequent good events from
	// being included in the aggregate. Findings accumulate; aggregation continues.
	agentID := domain.AgentInstanceID("Agent#1")

	agg, findings := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEventNoUsage(agentID, "claude-sonnet"),                                           // bad — no usage
		invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)}), // good
	))

	if len(findings) == 0 {
		t.Error("expected at least one finding for the event with missing usage")
	}

	run := requireOneRun(t, agg)
	agent := requireOneAgent(t, run)
	assertTokenPresent(t, "Input (aggregation continued)", agent.Totals().Input, 100)
}

func TestAggregate_EachBadEvent_ProducesItsOwnFinding(t *testing.T) {
	// Every event that cannot be fully used must produce exactly one finding.
	// Multiple bad events must produce multiple findings, not just one.
	agentID := domain.AgentInstanceID("Agent#1")

	_, findings := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEventNoUsage(agentID, "claude-sonnet"), // finding 1
		invEndEventNoUsage(agentID, "claude-sonnet"), // finding 2
	))

	missingUsageCount := 0
	for _, f := range findings {
		if f.Kind == domain.FindingMissingTokenUsage {
			missingUsageCount++
		}
	}

	if missingUsageCount < 2 {
		t.Errorf("got %d FindingMissingTokenUsage entries, want at least 2 (one per bad event)",
			missingUsageCount)
	}
}

func TestAggregate_FindingCarriesRunAndActor(t *testing.T) {
	// A finding must be attributable: it must carry the run and actor so the
	// frontend can show which run and which agent was affected.
	agentID := domain.AgentInstanceID("Agent#1")

	_, findings := analysis.Aggregate(oneAgentStream(testRunRef, agentID,
		invEndEventNoUsage(agentID, "claude-sonnet"),
	))

	if !hasFindingKind(findings, domain.FindingMissingTokenUsage) {
		t.Skip("no MissingTokenUsage finding to inspect")
	}

	for _, f := range findings {
		if f.Kind != domain.FindingMissingTokenUsage {
			continue
		}

		if f.Run.Kind != domain.RunNamed || f.Run.ID != testRunID {
			t.Errorf("finding Run = %v, want NamedRun(%q)", f.Run, testRunID)
		}

		if f.Actor.Kind != domain.ActorAgentInstance {
			t.Errorf("finding Actor.Kind = %v, want ActorAgentInstance", f.Actor.Kind)
		}

		if f.Actor.Instance != agentID {
			t.Errorf("finding Actor.Instance = %q, want %q", f.Actor.Instance, agentID)
		}
	}
}

// ---------------------------------------------------------------------------
// Aggregator stateful API
// ---------------------------------------------------------------------------

func TestAggregator_Add_ThenResult_EquivalentToAggregate(t *testing.T) {
	// The Aggregator API must produce the same aggregate as the pure Aggregate
	// function for the same sequence of events.
	agentID := domain.AgentInstanceID("Agent#1")
	ref := agentStream(testRunRef, agentID)
	ev := invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)})

	// Via Aggregator
	agg := analysis.NewAggregator()
	agg.Add(ref, ev)
	aggResult, _ := agg.Result()

	// Via Aggregate function
	funcResult, _ := analysis.Aggregate(analysis.Input{
		Streams: []analysis.Stream{{Ref: ref, Events: []domain.Event{ev}}},
	})

	// Both should have 1 run with 1 agent with 100 input tokens.
	if len(aggResult.Runs) != 1 || len(funcResult.Runs) != 1 {
		t.Fatalf("run count mismatch: Aggregator=%d, Aggregate=%d",
			len(aggResult.Runs), len(funcResult.Runs))
	}

	aggTotals := aggResult.Runs[0].Agents[0].Totals()
	funcTotals := funcResult.Runs[0].Agents[0].Totals()

	aggV, aggOk := aggTotals.Input.Value()
	funcV, funcOk := funcTotals.Input.Value()

	if aggOk != funcOk || aggV != funcV {
		t.Errorf("Aggregator Input=(%d,%v), Aggregate Input=(%d,%v); must match",
			aggV, aggOk, funcV, funcOk)
	}
}

func TestAggregator_ResultDoesNotResetState(t *testing.T) {
	// Calling Result must not reset the aggregator; a second call must return
	// the same aggregate with the same data as the first call.
	agentID := domain.AgentInstanceID("Agent#1")
	ref := agentStream(testRunRef, agentID)
	ev := invEndEvent(agentID, "claude-sonnet", domain.TokenUsage{Input: domain.Tokens(100)})

	a := analysis.NewAggregator()
	a.Add(ref, ev)

	agg1, _ := a.Result()
	agg2, _ := a.Result()

	// First, both calls must return a non-empty aggregate (verifies the stub fails).
	if len(agg1.Runs) != 1 {
		t.Fatalf("first Result(): got %d runs, want 1", len(agg1.Runs))
	}
	if len(agg2.Runs) != 1 {
		t.Fatalf("second Result(): got %d runs, want 1", len(agg2.Runs))
	}

	v1, ok1 := agg1.Runs[0].Agents[0].Totals().Input.Value()
	v2, ok2 := agg2.Runs[0].Agents[0].Totals().Input.Value()

	if ok1 != ok2 || v1 != v2 {
		t.Errorf("second Result() differs from first: first=(%d,%v), second=(%d,%v)",
			v1, ok1, v2, ok2)
	}
}

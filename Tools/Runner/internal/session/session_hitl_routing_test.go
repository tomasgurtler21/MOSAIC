package session_test

// Session-level tests for the two special outcomes of the HITL gate, on both
// dispatch paths (auto: engine-routed; consult: consultant-routed):
//
//   - A BLOCKED response carrying E503 (user contact unavailable) is accepted
//     directly: no gate verification, no re-dispatch, one Execution Log row,
//     current_state at BLOCKED/E503. It is then routed as any BLOCKED result.
//     Any other BLOCKED code with an unapproved written output is re-dispatched.
//
//   - After a gate-discharging re-dispatch that returns SUCCESS, routing,
//     current_state.last_status/error_code and the trigger evaluation follow the
//     ORIGINAL attempt's status, while current_state.last_agent names the
//     re-dispatch. A re-dispatch returning any other status is applied as
//     returned.
//
// Routing is observed through the consultant: in the auto path a non-SUCCESS
// routed status becomes a deviation carrying the routed response, and in the
// consult path the consultation request carries the routed response's
// status_message.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

const hitlReviewWorkflow = "hitl-review"

// reviewRig builds a detecting rig for the three-agent HITL review workflow.
func reviewRig(t *testing.T) *detectingRig {
	t.Helper()
	return newDetectingRig(t, "hitl-review-orch.md", "agent-a", "agent-b", "agent-c")
}

// reviewConfig returns the run config for the review workflow on the given
// path. The auto path runs in Mode 2 (auto) unless mode overrides it.
func reviewConfig(rig *detectingRig, kind dispatchPathKind, mode domain.ExecutionMode, loopLimit int) domain.RunConfig {
	cfg := rig.config(kind, hitlReviewWorkflow)
	cfg.RunSettings.Mode = mode
	cfg.RunSettings.ReviewLoopLimit = loopLimit
	return cfg
}

func modeFor(kind dispatchPathKind) domain.ExecutionMode {
	if kind == pathConsult {
		return domain.ExecutionModeOrchestrated
	}
	return domain.ExecutionModeAuto
}

// queueReviewPrefix queues agent-a's success and, on the consult path, the
// consultant's dispatches of agent-a and then agent-b.
func queueReviewPrefix(rig *detectingRig, kind dispatchPathKind) {
	rig.adapter.Queue("agent-a", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "a done")})
	if kind == pathConsult {
		rig.consult.queueDispatch("agent-a", "do the work", 0)
		rig.consult.queueDispatch("agent-b", "review the work", 1)
	}
}

// reviewAttempt is one scripted reviewer invocation writing review.md.
func reviewAttempt(status domain.StatusCode, code domain.ErrorCode, msg, stamp, tag string) harness.ScriptedEntry {
	r := resp(status, msg)
	r.ErrorCode = code
	if code != domain.ErrorNone {
		r.ErrorReason = "reason: " + msg
	}
	return harness.ScriptedEntry{
		Response: r,
		Writes:   []harness.ScriptedWrite{{Path: dispatchPath("review.md"), Content: stamped(stamp, tag)}},
	}
}

// stepsOf returns every applied step (rejected attempts included) whose agent
// instance belongs to the agent.
func (r *detectingRig) stepsOf(agent string) []domain.CompletedStep {
	var out []domain.CompletedStep
	for _, s := range r.store.Applied {
		if len(s.AgentInstance) > len(agent) && s.AgentInstance[:len(agent)+1] == agent+"#" {
			out = append(out, s)
		}
	}
	return out
}

func deviationOf(t *testing.T, c *scriptedRoutingConsultant) *domain.DeviationInfo {
	t.Helper()
	for _, req := range c.Requests {
		if req.Deviation != nil {
			return req.Deviation
		}
	}
	t.Fatalf("no consultation carried a deviation (%d consultation(s))", len(c.Requests))
	return nil
}

func lastRequest(t *testing.T, c *scriptedRoutingConsultant) domain.ConsultationRequest {
	t.Helper()
	if len(c.Requests) == 0 {
		t.Fatalf("no consultation happened")
	}
	return c.Requests[len(c.Requests)-1]
}

// requireRoutedResponse asserts that the run routed on a response with the
// given status, error code and status message: as a deviation on the auto path,
// as the last consultation request on the consult path.
func requireRoutedResponse(t *testing.T, rig *detectingRig, kind dispatchPathKind,
	status domain.StatusCode, code domain.ErrorCode, msg string) {
	t.Helper()
	if kind == pathAuto {
		dev := deviationOf(t, rig.consult)
		if dev.Kind != domain.DeviationNonSuccess {
			t.Errorf("deviation kind: want %q, got %q", domain.DeviationNonSuccess, dev.Kind)
		}
		if dev.Response.StatusCode != status || dev.Response.ErrorCode != code {
			t.Errorf("deviation response: want %s/%q, got %s/%q",
				status, code, dev.Response.StatusCode, dev.Response.ErrorCode)
		}
		if dev.Response.StatusMessage != msg {
			t.Errorf("deviation status_message: want %q, got %q", msg, dev.Response.StatusMessage)
		}
		return
	}
	req := lastRequest(t, rig.consult)
	if req.LastStatusMessage == nil {
		t.Errorf("consultation last_status_message: want %q, got nil", msg)
	} else if *req.LastStatusMessage != msg {
		t.Errorf("consultation last_status_message: want %q, got %q", msg, *req.LastStatusMessage)
	}
}

func requireCurrentState(t *testing.T, rig *detectingRig, status domain.StatusCode, code domain.ErrorCode, agent string) {
	t.Helper()
	cs := rig.store.state.CurrentState
	if cs.LastStatus != status || cs.ErrorCode != code {
		t.Errorf("current_state status: want %s/%q, got %s/%q", status, code, cs.LastStatus, cs.ErrorCode)
	}
	if cs.LastAgent != agent {
		t.Errorf("current_state last_agent: want %q, got %q", agent, cs.LastAgent)
	}
}

// ===== BLOCKED / E503 exemption =====

// TestSession_HITLGate_E503_AcceptedWithoutRedispatchAndRoutedAsBlocked verifies
// that an HITL dispatch returning BLOCKED/E503 with a written output stamped
// false is not gated: one row, no re-dispatch, current_state at BLOCKED/E503,
// the written output registered, and the result routed as BLOCKED.
func TestSession_HITLGate_E503_AcceptedWithoutRedispatchAndRoutedAsBlocked(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		t.Run(string(kind), func(t *testing.T) {
			rig := reviewRig(t)
			queueReviewPrefix(rig, kind)
			rig.adapter.Queue("agent-b", reviewAttempt(domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, "no user", "false", "v1"))
			rig.consult.queueStop("done")
			rig.consult.queueStop("done")

			rig.ses.Start(context.Background(), reviewConfig(rig, kind, modeFor(kind), 0)) //nolint:errcheck

			if got := rig.invocationsOf("agent-b"); got != 1 {
				t.Errorf("agent-b invocations: want 1 (E503 is not re-dispatched), got %d", got)
			}
			steps := rig.stepsOf("agent-b")
			if len(steps) != 1 {
				t.Fatalf("agent-b rows: want exactly 1, got %d", len(steps))
			}
			if steps[0].HITLRejected || steps[0].IsInfrastructure {
				t.Errorf("the E503 row must be an accepted workflow row, got %+v", steps[0])
			}
			if len(steps[0].WrittenArtifacts) != 1 || steps[0].WrittenArtifacts[0] != dispatchPath("review.md") {
				t.Errorf("written outputs must be registered: got %v", steps[0].WrittenArtifacts)
			}
			requireCurrentState(t, rig, domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, steps[0].AgentInstance)
			requireRoutedResponse(t, rig, kind, domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, "no user")
			if got := rig.invocationsOf("agent-c"); got != 0 {
				t.Errorf("a BLOCKED result must not advance to agent-c, got %d invocation(s)", got)
			}
		})
	}
}

// TestSession_HITLGate_OtherBlockedCode_StillRedispatched verifies that the
// exemption is specific to E503: BLOCKED with another code and an unapproved
// written output is re-dispatched.
func TestSession_HITLGate_OtherBlockedCode_StillRedispatched(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		t.Run(string(kind), func(t *testing.T) {
			rig := reviewRig(t)
			queueReviewPrefix(rig, kind)
			rig.adapter.Queue("agent-b",
				reviewAttempt(domain.StatusBLOCKED, domain.ErrorPERMISSION_DENIED, "denied", "false", "v1"),
				reviewAttempt(domain.StatusBLOCKED, domain.ErrorPERMISSION_DENIED, "denied again", "true", "v2"),
			)
			rig.consult.queueStop("done")
			rig.consult.queueStop("done")

			rig.ses.Start(context.Background(), reviewConfig(rig, kind, modeFor(kind), 0)) //nolint:errcheck

			if got := rig.invocationsOf("agent-b"); got != 2 {
				t.Errorf("agent-b invocations: want 2 (original + re-dispatch), got %d", got)
			}
		})
	}
}

// TestSession_HITLGate_RedispatchReturningE503_RoutedAsBlocked verifies that a
// re-dispatch returning BLOCKED/E503 is accepted as returned and routed on
// BLOCKED, not on the original attempt's status.
func TestSession_HITLGate_RedispatchReturningE503_RoutedAsBlocked(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		t.Run(string(kind), func(t *testing.T) {
			rig := reviewRig(t)
			queueReviewPrefix(rig, kind)
			rig.adapter.Queue("agent-b",
				reviewAttempt(domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, "findings", "false", "v1"),
				reviewAttempt(domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, "no user", "false", "v2"),
			)
			rig.consult.queueStop("done")
			rig.consult.queueStop("done")

			rig.ses.Start(context.Background(), reviewConfig(rig, kind, modeFor(kind), 0)) //nolint:errcheck

			if got := rig.invocationsOf("agent-b"); got != 2 {
				t.Errorf("agent-b invocations: want 2 (no second re-dispatch), got %d", got)
			}
			steps := rig.stepsOf("agent-b")
			if len(steps) != 2 {
				t.Fatalf("agent-b rows: want 2 (rejected original + E503), got %d", len(steps))
			}
			if !steps[0].HITLRejected {
				t.Errorf("the original attempt must be recorded as rejected")
			}
			if steps[1].Routed != nil {
				t.Errorf("a BLOCKED re-dispatch is applied as returned, got Routed=%+v", steps[1].Routed)
			}
			requireCurrentState(t, rig, domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, steps[1].AgentInstance)
			requireRoutedResponse(t, rig, kind, domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, "no user")
		})
	}
}

// ===== Original-status routing after a gate-discharging SUCCESS re-dispatch =====

// TestSession_HITLGate_RepairedNonSuccess_RecordsOriginalStatusUnderRedispatchAgent
// verifies, for the original statuses that route as findings, that after a
// SUCCESS re-dispatch: both rows are logged as returned, the re-dispatch step
// carries the original status as its routed outcome, current_state keeps the
// original status while naming the re-dispatch as last_agent, and the routing
// uses the original response.
func TestSession_HITLGate_RepairedNonSuccess_RecordsOriginalStatusUnderRedispatchAgent(t *testing.T) {
	statuses := []domain.StatusCode{domain.StatusCOMPLETED_NEEDS_ACTION, domain.StatusPARTIALLY_DONE}
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		for _, orig := range statuses {
			t.Run(string(kind)+"/"+string(orig), func(t *testing.T) {
				rig := reviewRig(t)
				queueReviewPrefix(rig, kind)
				rig.adapter.Queue("agent-b",
					reviewAttempt(orig, domain.ErrorNone, "original findings", "false", "v1"),
					reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "repaired", "true", "v2"),
				)
				rig.consult.queueStop("done")
				rig.consult.queueStop("done")

				rig.ses.Start(context.Background(), reviewConfig(rig, kind, modeFor(kind), 0)) //nolint:errcheck

				steps := rig.stepsOf("agent-b")
				if len(steps) != 2 {
					t.Fatalf("agent-b rows: want 2, got %d", len(steps))
				}
				if steps[0].Status != orig || !steps[0].HITLRejected {
					t.Errorf("original row: want %s recorded as rejected, got %+v", orig, steps[0])
				}
				re := steps[1]
				if re.Status != domain.StatusSUCCESS {
					t.Errorf("re-dispatch row is logged as returned (SUCCESS), got %s", re.Status)
				}
				if re.Routed == nil || re.Routed.Status != orig {
					t.Errorf("re-dispatch step must carry the original status %s as its routed outcome, got %+v", orig, re.Routed)
				}
				if re.RoutedStatus() != orig {
					t.Errorf("RoutedStatus: want %s, got %s", orig, re.RoutedStatus())
				}
				if len(re.WrittenArtifacts) != 1 || re.WrittenArtifacts[0] != dispatchPath("review.md") {
					t.Errorf("Artifacts must be upserted from the re-dispatch, got %v", re.WrittenArtifacts)
				}
				requireCurrentState(t, rig, orig, domain.ErrorNone, re.AgentInstance)
				requireRoutedResponse(t, rig, kind, orig, domain.ErrorNone, "original findings")
				if got := rig.invocationsOf("agent-c"); got != 0 {
					t.Errorf("a repaired %s must not advance as SUCCESS to agent-c, got %d invocation(s)", orig, got)
				}
			})
		}
	}
}

// TestSession_HITLGate_RepairedSuccess_RoutesAsSuccess verifies that when the
// original attempt was SUCCESS, the repaired step is recorded and routed as
// SUCCESS, with no routed override needed.
func TestSession_HITLGate_RepairedSuccess_RoutesAsSuccess(t *testing.T) {
	rig := reviewRig(t)
	queueReviewPrefix(rig, pathAuto)
	rig.adapter.Queue("agent-b",
		reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "first", "false", "v1"),
		reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "second", "true", "v2"),
	)
	rig.adapter.Queue("agent-c", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "c done")})

	got, err := rig.ses.Start(context.Background(), reviewConfig(rig, pathAuto, domain.ExecutionModeAuto, 0))

	requireRunStatus(t, got, err, domain.RunCompleted)
	if n := rig.invocationsOf("agent-c"); n != 1 {
		t.Errorf("agent-c invocations: want 1 after a repaired SUCCESS, got %d", n)
	}
	if len(rig.consult.Requests) != 0 {
		t.Errorf("a repaired SUCCESS must not deviate, got %d consultation(s)", len(rig.consult.Requests))
	}
}

// TestSession_HITLGate_RepairedCompletedNeedsAction_AutoReviewRoutesToOnFindings
// verifies that a reviewer's COMPLETED_NEEDS_ACTION followed by a repaired
// SUCCESS is routed to the On Findings target, exactly as if the original had
// been accepted.
func TestSession_HITLGate_RepairedCompletedNeedsAction_AutoReviewRoutesToOnFindings(t *testing.T) {
	rig := reviewRig(t)
	rig.adapter.Queue("agent-a",
		harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "a done")},
		harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "a fixed")},
	)
	rig.adapter.Queue("agent-b",
		reviewAttempt(domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, "findings", "false", "v1"),
		reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "repaired", "true", "v2"),
		reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "clean", "true", "v3"),
	)
	rig.adapter.Queue("agent-c", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "c done")})

	got, err := rig.ses.Start(context.Background(), reviewConfig(rig, pathAuto, domain.ExecutionModeAutoReview, 0))

	requireRunStatus(t, got, err, domain.RunCompleted)
	var order []string
	for _, inv := range rig.adapter.Invocations() {
		order = append(order, inv.Agent.Identifier)
	}
	want := []string{"agent-a", "agent-b", "agent-b", "agent-a", "agent-b", "agent-c"}
	if len(order) != len(want) {
		t.Fatalf("dispatch order: want %v, got %v", want, order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("dispatch order: want %v, got %v", want, order)
		}
	}
}

// TestSession_HITLGate_ReviewLoopLimit_CountsRepairedOriginalOnce verifies that
// the review loop limit sees a rejected COMPLETED_NEEDS_ACTION and its repaired
// SUCCESS re-dispatch as one findings iteration: limit 1 is reached by it,
// limit 2 is not.
func TestSession_HITLGate_ReviewLoopLimit_CountsRepairedOriginalOnce(t *testing.T) {
	setup := func(t *testing.T, limit int) *detectingRig {
		rig := reviewRig(t)
		rig.adapter.Queue("agent-a",
			harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "a done")},
			harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "a fixed")},
		)
		rig.adapter.Queue("agent-b",
			reviewAttempt(domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, "findings", "false", "v1"),
			reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "repaired", "true", "v2"),
			reviewAttempt(domain.StatusSUCCESS, domain.ErrorNone, "clean", "true", "v3"),
		)
		rig.adapter.Queue("agent-c", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "c done")})
		rig.consult.queueStop("done")
		rig.ses.Start(context.Background(), reviewConfig(rig, pathAuto, domain.ExecutionModeAutoReview, limit)) //nolint:errcheck
		return rig
	}

	t.Run("limit 1 is reached", func(t *testing.T) {
		rig := setup(t, 1)
		dev := deviationOf(t, rig.consult)
		if dev.Kind != domain.DeviationReviewLoopLimit {
			t.Errorf("deviation kind: want %q, got %q", domain.DeviationReviewLoopLimit, dev.Kind)
		}
		if n := rig.invocationsOf("agent-a"); n != 1 {
			t.Errorf("agent-a must not be re-dispatched once the limit is reached, got %d invocation(s)", n)
		}
	})
	t.Run("limit 2 is not reached", func(t *testing.T) {
		rig := setup(t, 2)
		if len(rig.consult.Requests) != 0 {
			t.Errorf("one repaired findings iteration must count once (no deviation under limit 2), got %d consultation(s)", len(rig.consult.Requests))
		}
		if n := rig.invocationsOf("agent-a"); n != 2 {
			t.Errorf("agent-a must be re-dispatched via On Findings, got %d invocation(s)", n)
		}
	})
}

// TestSession_HITLGate_ResumeAfterRepairedReview_RoutesOnRecordedStatus verifies
// that resuming a run whose last review was repaired (log ends with the
// re-dispatch's SUCCESS row, current_state records COMPLETED_NEEDS_ACTION)
// routes on the recorded status: On Findings in auto-review mode, not the
// trailing row's SUCCESS.
func TestSession_HITLGate_ResumeAfterRepairedReview_RoutesOnRecordedStatus(t *testing.T) {
	rig := reviewRig(t)
	rig.store.exists = true
	rig.store.state = domain.ArtifactState{
		Workflow:        hitlReviewWorkflow,
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  3,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusCOMPLETED_NEEDS_ACTION,
			LastAgent:  "agent-b#3",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-b#2", Phase: "PLANNING", Status: domain.StatusCOMPLETED_NEEDS_ACTION},
			{Seq: 3, Agent: "agent-b#3", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	rig.adapter.Queue("agent-a", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "a fixed")})
	rig.consult.queueStop("done")

	cfg := reviewConfig(rig, pathAuto, domain.ExecutionModeAutoReview, 0)
	cfg.IsNewRun = false
	cfg.RunID = testRunID
	rig.ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := rig.adapter.Invocations()
	if len(invs) == 0 || invs[0].Agent.Identifier != "agent-a" {
		t.Fatalf("resume must route the recorded COMPLETED_NEEDS_ACTION to On Findings (agent-a first), got %v", invs)
	}
}

// ===== Triggers follow the routed status =====

const hitlStageEndPlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`

// stageEndRig builds a rig where the stage's last step is a HITL reviewer and a
// commit agent fires on STAGE_END.
func stageEndRig(t *testing.T, kind dispatchPathKind, review ...harness.ScriptedEntry) *detectingRig {
	t.Helper()
	rig := newDetectingRig(t, "hitl-stage-end-staged-orch.md",
		"implementation-tdd", "implementation-review", "commit-manager-git")
	writeRunFile(t, rig.runFolder, "Plan.md", hitlStageEndPlan)
	// The run is resumed with Stage-1's implementation step already done, so
	// the reviewer (the last workflow step of Stage-1) is the next dispatch.
	rig.store.exists = true
	rig.store.state = domain.ArtifactState{
		Workflow:        "hitl-stage-end",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: modeFor(kind)},
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Stage-1",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "implementation-tdd#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "implementation-tdd#1", Phase: "EXECUTION.Stage-1", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}
	rig.adapter.Queue("implementation-review", review...)
	rig.adapter.Queue("commit-manager-git", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "committed")})
	if kind == pathConsult {
		rig.consult.queueDispatch("implementation-review", "review", 1)
	}
	rig.consult.queueStop("done")
	rig.consult.queueStop("done")
	return rig
}

func stageReviewAttempt(status domain.StatusCode, stamp, tag string) harness.ScriptedEntry {
	return harness.ScriptedEntry{
		Response: resp(status, "review "+string(status)),
		Writes: []harness.ScriptedWrite{{
			Path:    dispatchPath("Stage-1/implementation-review.md"),
			Content: stamped(stamp, tag),
		}},
	}
}

func startStageEnd(rig *detectingRig, kind dispatchPathKind) {
	cfg := rig.config(kind, "hitl-stage-end")
	cfg.IsNewRun = false
	cfg.RunID = testRunID
	rig.ses.Start(context.Background(), cfg) //nolint:errcheck
}

// TestSession_HITLGate_RepairedNonSuccess_FiresNoStageEnd verifies that a
// repaired COMPLETED_NEEDS_ACTION or PARTIALLY_DONE that is the last workflow
// step of a stage does not fire STAGE_END.
func TestSession_HITLGate_RepairedNonSuccess_FiresNoStageEnd(t *testing.T) {
	statuses := []domain.StatusCode{domain.StatusCOMPLETED_NEEDS_ACTION, domain.StatusPARTIALLY_DONE}
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		for _, orig := range statuses {
			t.Run(string(kind)+"/"+string(orig), func(t *testing.T) {
				rig := stageEndRig(t, kind,
					stageReviewAttempt(orig, "false", "v1"),
					stageReviewAttempt(domain.StatusSUCCESS, "true", "v2"),
				)

				startStageEnd(rig, kind)

				if n := rig.invocationsOf("implementation-review"); n != 2 {
					t.Fatalf("implementation-review invocations: want 2, got %d", n)
				}
				if n := rig.invocationsOf("commit-manager-git"); n != 0 {
					t.Errorf("STAGE_END must follow the routed %s status and not fire, got %d commit dispatch(es)", orig, n)
				}
			})
		}
	}
}

// TestSession_HITLGate_RepairedSuccess_FiresStageEndOnce verifies that a
// gate-discharged original SUCCESS fires STAGE_END exactly once, not once per
// attempt.
func TestSession_HITLGate_RepairedSuccess_FiresStageEndOnce(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		t.Run(string(kind), func(t *testing.T) {
			rig := stageEndRig(t, kind,
				stageReviewAttempt(domain.StatusSUCCESS, "false", "v1"),
				stageReviewAttempt(domain.StatusSUCCESS, "true", "v2"),
			)

			startStageEnd(rig, kind)

			if n := rig.invocationsOf("implementation-review"); n != 2 {
				t.Fatalf("implementation-review invocations: want 2, got %d", n)
			}
			if n := rig.invocationsOf("commit-manager-git"); n != 1 {
				t.Errorf("STAGE_END must fire exactly once, got %d commit dispatch(es)", n)
			}
		})
	}
}

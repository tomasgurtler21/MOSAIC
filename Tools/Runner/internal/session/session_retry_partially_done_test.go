package session_test

// Tests for the session's handling of PARTIALLY_DONE: in the auto modes the
// same row is dispatched again without a consultation, bounded by the engine's
// re-dispatch limit, and the re-dispatch carries the previous output as
// context. In orchestrated mode every PARTIALLY_DONE leads to a consultation.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/harness"
)

func TestSession_PartiallyDone_ThenSuccess_RedispatchesSameRowWithoutConsultation(t *testing.T) {
	for _, mode := range autoModes {
		t.Run(string(mode), func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)
			f.Queue("agent-a",
				statusEntry(domain.StatusPARTIALLY_DONE, domain.ErrorNone, "half done"),
				statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"),
			)
			f.Queue("agent-b", statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"))

			got, err := ses.Start(context.Background(), configForMode(orchPath, mode))

			requireRunStatus(t, got, err, domain.RunCompleted)
			if consultant.CallCount != 0 {
				t.Errorf("want no consultation for PARTIALLY_DONE then SUCCESS, got %d", consultant.CallCount)
			}
			rows := agentRows(store, "agent-a")
			if len(rows) != 2 {
				t.Fatalf("want 2 agent-a rows (PARTIALLY_DONE, then SUCCESS), got %d: %+v", len(rows), rows)
			}
			if rows[0].Status != domain.StatusPARTIALLY_DONE || rows[1].Status != domain.StatusSUCCESS {
				t.Errorf("want statuses PARTIALLY_DONE then SUCCESS, got %s then %s", rows[0].Status, rows[1].Status)
			}
			if rows[0].WorkflowRow != rows[1].WorkflowRow || rows[0].Stage != rows[1].Stage {
				t.Errorf("want both dispatches on the same row and stage, got row %d/%q and row %d/%q",
					rows[0].WorkflowRow, rows[0].Stage, rows[1].WorkflowRow, rows[1].Stage)
			}
		})
	}
}

func TestSession_PartiallyDone_AlwaysPartiallyDone_ConsultsAfterTheBound(t *testing.T) {
	for _, mode := range autoModes {
		t.Run(string(mode), func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			consultant.queueStop("agent keeps returning PARTIALLY_DONE")
			ses, f, _, orchPath := newAutoSessionWithConsultant(t, consultant)
			// More replies than the bound allows, so the bound, not the queue, ends the loop.
			queueRepeated(f, "agent-a", engine.PartiallyDoneRedispatchLimit+3,
				statusEntry(domain.StatusPARTIALLY_DONE, domain.ErrorNone, "half done"))

			got, err := ses.Start(context.Background(), configForMode(orchPath, mode))

			requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
			if want := 1 + engine.PartiallyDoneRedispatchLimit; countInvocationsFor(f.Invocations(), "agent-a") != want {
				t.Errorf("want %d agent-a dispatches (original + re-dispatches), got %d",
					want, countInvocationsFor(f.Invocations(), "agent-a"))
			}
			if consultant.CallCount != 1 {
				t.Fatalf("want exactly one consultation after the bound, got %d", consultant.CallCount)
			}
			dev := consultant.Requests[0].Deviation
			if dev == nil || dev.Response.StatusCode != domain.StatusPARTIALLY_DONE {
				t.Errorf("want the consultation to carry the PARTIALLY_DONE deviation, got %+v", dev)
			}
		})
	}
}

func TestSession_PartiallyDone_OrchestratedMode_ConsultsInsteadOfRedispatching(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)
	consultant.queueStop("decided after PARTIALLY_DONE")
	ses, f, _, orchPath := newOrchestratedSession(t, consultant)
	queueRepeated(f, "agent-a", 3, statusEntry(domain.StatusPARTIALLY_DONE, domain.ErrorNone, "half done"))

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 1 {
		t.Errorf("want 1 agent-a dispatch (no mechanical re-dispatch), got %d", n)
	}
	if consultant.CallCount != 2 {
		t.Errorf("want 2 consultations (initial dispatch, then after PARTIALLY_DONE), got %d", consultant.CallCount)
	}
}

// TestSession_PartiallyDone_Redispatch_CarriesPreviousOutputAsContext verifies
// that the request sent to the harness for the re-dispatch holds the context
// the engine chose: the previous status message in the task description and
// the previous output among the input artifacts. The first request holds
// neither.
func TestSession_PartiallyDone_Redispatch_CarriesPreviousOutputAsContext(t *testing.T) {
	const statusMessage = "drafted sections one and two only"
	rig := newDetectingRig(t, "written-outputs-orch.md", "agent-a", "agent-b")
	output := dispatchPath("design.md")
	rig.adapter.Queue("agent-a",
		harness.ScriptedEntry{
			Response: resp(domain.StatusPARTIALLY_DONE, statusMessage),
			Writes:   []harness.ScriptedWrite{{Path: output, Content: stamped("true", "v1")}},
		},
		harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "finished")},
	)
	rig.adapter.Queue("agent-b", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "b done")})

	got, err := rig.ses.Start(context.Background(), rig.config(pathAuto, "written-outputs"))

	requireRunStatus(t, got, err, domain.RunCompleted)
	var aReqs []domain.ProtocolRequest
	for _, inv := range rig.adapter.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			aReqs = append(aReqs, inv.Request)
		}
	}
	if len(aReqs) != 2 {
		t.Fatalf("want 2 agent-a requests, got %d", len(aReqs))
	}
	first, second := aReqs[0], aReqs[1]
	if strings.Contains(first.TaskDescription, engine.PartiallyDoneContextPrefix) || containsInput(first.InputArtifacts, output) {
		t.Errorf("the first request must carry no PARTIALLY_DONE context, got task %q inputs %v",
			first.TaskDescription, first.InputArtifacts)
	}
	if !strings.Contains(second.TaskDescription, engine.PartiallyDoneContextPrefix) ||
		!strings.Contains(second.TaskDescription, statusMessage) {
		t.Errorf("the re-dispatch task description must carry the PARTIALLY_DONE context and the previous status message, got %q",
			second.TaskDescription)
	}
	if !containsInput(second.InputArtifacts, output) {
		t.Errorf("the re-dispatch inputs must include the previous output %q, got %v", output, second.InputArtifacts)
	}
}

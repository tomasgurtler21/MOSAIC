package session_test

// Tests that a consultation-routed step that uses a row's defaults gets
// exactly the inputs, outputs and HITL value an engine-routed dispatch of the
// same row and stage gets, that explicit overrides reach the agent verbatim,
// and that a failure to resolve the defaults is surfaced instead of swallowed.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// defaultsSeed is a run whose last step is the build-review of stage 1, so the
// engine's next dispatch is the test-writer of stage 2 (plan stage HITL true).
func defaultsSeed() domain.ArtifactState {
	return rowStageSeed("build-review", 4, "EXECUTION", "Implementation.1")
}

// engineRoutedTestWriter returns the request the engine-routed dispatch of the
// test-writer at stage 2 sends to the harness.
func engineRoutedTestWriter(t *testing.T) domain.ProtocolRequest {
	t.Helper()
	rig := newDefaultsRig(t, stoppingConsultant(3), domain.ExecutionModeAuto, defaultsSeed())
	queueSuccess(rig.f, "test-writer", "test-writer#3")

	_, err := rig.ses.Start(context.Background(), rig.cfg)

	if err != nil {
		t.Fatalf("engine-routed run: %v", err)
	}
	return firstRequestTo(t, rig.f, "test-writer")
}

// consultRoutedTestWriter returns the request the consultation-routed dispatch
// of the test-writer at stage 2 sends to the harness; shape queues the
// instruction (with any overrides) before the run.
func consultRoutedTestWriter(t *testing.T, shape func(c *scriptedRoutingConsultant)) domain.ProtocolRequest {
	t.Helper()
	consultant := &scriptedRoutingConsultant{}
	shape(consultant)
	consultant.queueStop("done")
	rig := newDefaultsRig(t, consultant, domain.ExecutionModeOrchestrated, defaultsSeed())
	queueSuccess(rig.f, "test-writer", "test-writer#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	return firstRequestTo(t, rig.f, "test-writer")
}

func TestSession_ConsultRoute_NoOverrides_GetsSameInputsOutputsAndHITLAsEngineRouting(t *testing.T) {
	engineReq := engineRoutedTestWriter(t)

	consultReq := consultRoutedTestWriter(t, func(c *scriptedRoutingConsultant) {
		c.queueStagedDispatch("test-writer", "write the tests for stage 2", cdTestWriter, 2)
	})

	if !engineReq.HumanInTheLoop {
		t.Fatalf("fixture check: the engine-routed stage 2 dispatch should be HITL (plan stage HITL), got false")
	}
	if !reflect.DeepEqual(consultReq.InputArtifacts, engineReq.InputArtifacts) {
		t.Errorf("inputs differ:\n consult %v\n engine  %v", consultReq.InputArtifacts, engineReq.InputArtifacts)
	}
	if !reflect.DeepEqual(consultReq.OutputArtifacts, engineReq.OutputArtifacts) {
		t.Errorf("outputs differ:\n consult %v\n engine  %v", consultReq.OutputArtifacts, engineReq.OutputArtifacts)
	}
	if consultReq.HumanInTheLoop != engineReq.HumanInTheLoop {
		t.Errorf("HITL differs: consult %v, engine %v", consultReq.HumanInTheLoop, engineReq.HumanInTheLoop)
	}
}

// For a non-staged row the consultation path used to leave a Stage-* default
// input literal, while the engine expands it per plan stage.
func TestSession_ConsultRoute_NonStagedRowWithStageWildcardDefault_GetsSameInputsAsEngineRouting(t *testing.T) {
	seed := func() domain.ArtifactState {
		return rowStageSeed("build-review", 4, "EXECUTION", "Implementation.2")
	}
	engineRig := newDefaultsRig(t, stoppingConsultant(3), domain.ExecutionModeAuto, seed())
	queueSuccess(engineRig.f, "final-review", "final-review#3")
	if _, err := engineRig.ses.Start(context.Background(), engineRig.cfg); err != nil {
		t.Fatalf("engine-routed run: %v", err)
	}
	engineReq := firstRequestTo(t, engineRig.f, "final-review")

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("final-review", "final review", cdFinalReview)
	consultant.queueStop("done")
	consultRig := newDefaultsRig(t, consultant, domain.ExecutionModeOrchestrated, seed())
	queueSuccess(consultRig.f, "final-review", "final-review#3")
	got, err := consultRig.ses.Start(context.Background(), consultRig.cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	consultReq := firstRequestTo(t, consultRig.f, "final-review")

	if len(engineReq.InputArtifacts) < 2 {
		t.Fatalf("fixture check: engine-routed Stage-* default should expand per stage, got %v", engineReq.InputArtifacts)
	}
	if !reflect.DeepEqual(consultReq.InputArtifacts, engineReq.InputArtifacts) {
		t.Errorf("inputs differ:\n consult %v\n engine  %v", consultReq.InputArtifacts, engineReq.InputArtifacts)
	}
	if !reflect.DeepEqual(consultReq.OutputArtifacts, engineReq.OutputArtifacts) {
		t.Errorf("outputs differ:\n consult %v\n engine  %v", consultReq.OutputArtifacts, engineReq.OutputArtifacts)
	}
}

func TestSession_ConsultRoute_ExplicitStageWildcardInput_ReachesAgentUnchanged(t *testing.T) {
	override := []string{"Stage-*/Plan.md"}

	req := consultRoutedTestWriter(t, func(c *scriptedRoutingConsultant) {
		c.queueDispatchWithInputs("test-writer", "write the tests for stage 2", cdTestWriter, &override)
		c.stageLastDispatch(2)
	})

	if got := bare(req, req.InputArtifacts); !reflect.DeepEqual(got, override) {
		t.Errorf("explicit inputs = %v, want %v verbatim", got, override)
	}
}

func TestSession_ConsultRoute_ExplicitEmptyInputs_AreNotReplacedByDefaults(t *testing.T) {
	override := []string{}

	req := consultRoutedTestWriter(t, func(c *scriptedRoutingConsultant) {
		c.queueDispatchWithInputs("test-writer", "write the tests for stage 2", cdTestWriter, &override)
		c.stageLastDispatch(2)
	})

	if len(req.InputArtifacts) != 0 {
		t.Errorf("explicit empty inputs: want none, got %v", req.InputArtifacts)
	}
}

func TestSession_ConsultRoute_ExplicitOutputsWithStageToken_ReachAgentUnresolved(t *testing.T) {
	override := []string{"Stage-{StageNumber}/custom.md", "Stage-*/Plan.md"}

	req := consultRoutedTestWriter(t, func(c *scriptedRoutingConsultant) {
		c.queueDispatchWithOutputs("test-writer", "write the tests for stage 2", cdTestWriter, &override)
		c.stageLastDispatch(2)
	})

	if got := bare(req, req.OutputArtifacts); !reflect.DeepEqual(got, override) {
		t.Errorf("explicit outputs = %v, want %v verbatim", got, override)
	}
}

// Guards against an implementation that ORs an explicit HITL override with the
// plan stage HITL: the stage is HITL, the explicit override says no.
func TestSession_ConsultRoute_ExplicitHITLFalse_WinsOverPlanStageHITL(t *testing.T) {
	off := false

	req := consultRoutedTestWriter(t, func(c *scriptedRoutingConsultant) {
		c.queueDispatchWithHITL("test-writer", "write the tests for stage 2", cdTestWriter, &off)
		c.stageLastDispatch(2)
	})

	if req.HumanInTheLoop {
		t.Errorf("explicit HITL override false should win over the plan stage HITL, got true")
	}
}

// Guards the verbatim rule in the other direction: neither the plan stage nor
// the row is HITL, the explicit override says yes.
func TestSession_ConsultRoute_ExplicitHITLTrue_WinsOverNonHITLStageAndRow(t *testing.T) {
	on := true

	req := consultRoutedTestWriter(t, func(c *scriptedRoutingConsultant) {
		c.queueDispatchWithHITL("test-writer", "write the tests for stage 1", cdTestWriter, &on)
		c.stageLastDispatch(1)
	})

	if !req.HumanInTheLoop {
		t.Errorf("explicit HITL override true should win over a non-HITL stage and row, got false")
	}
}

func TestSession_ConsultRoute_UnresolvableDefaults_StopsRunWithoutDispatchOrRecord(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("impl-review", "final review", cdImplReview)
	consultant.queueStop("done")
	rig := newDefaultsRig(t, consultant, domain.ExecutionModeOrchestrated,
		rowStageSeed("test-writer", 2, "EXECUTION", "Test.1"))

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if n := len(rig.f.Invocations()); n != 0 {
		t.Errorf("want no agent dispatched when the defaults cannot be resolved, got %d invocation(s)", n)
	}
	if steps := stepsByAgent(rig.store, "impl-review"); len(steps) != 0 {
		t.Errorf("want nothing recorded for the failed dispatch, got %+v", steps)
	}
	if got.Cause == nil {
		t.Fatalf("want the resolution error as the outcome cause, got nil (message %q)", got.Message)
	}
	text := got.Message + " " + got.Cause.Error()
	for _, want := range []string{"impl-review", "{StageNumber}"} {
		if !strings.Contains(text, want) {
			t.Errorf("outcome %q should name %q", text, want)
		}
	}
}

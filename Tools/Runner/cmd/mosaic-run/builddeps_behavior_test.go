package main

import (
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---------------------------------------------------------------------------
// Behavioral session tests driven via buildDeps
//
// These tests verify that a session constructed from buildDeps output behaves
// correctly end-to-end. They exercise the wiring path that both frontends use,
// establishing that the interactive path produces a session that consults the
// OrchestratorConsultant for routing and pre-consultation.
//
// Infrastructure shared by these tests lives below the test functions:
// fakeRawInvoker, mainTestMemStore, mainTestClock, mainTestNoopInteraction,
// and the file-helper functions.
// ---------------------------------------------------------------------------

// mainTestOrchestratorDir is the session testdata directory relative to the
// cmd/mosaic-run package. It contains the orchestrator fixture files shared
// with internal/session tests.
const mainTestOrchestratorDir = "../../testdata/session"

// TestBuildDeps_PreConsultation_AdviceAppliedToDispatch_ViaBuildDeps verifies
// that when buildDeps is called with PreConsultation=true, the resulting session
// invokes its PreConsult field (the OrchestratorConsultant wired by buildDeps)
// and that the pre-consultation advice is applied to the first auto-routed
// dispatch's task description.
//
// This mirrors TestSession_Start_PreConsultation_AdviceAppliedToDispatch in the
// session package but is driven through buildDeps, establishing that the
// interactive wiring path (which calls buildDeps with the completed configuration)
// produces a session that actually uses pre-consultation advice.
func TestBuildDeps_PreConsultation_AdviceAppliedToDispatch_ViaBuildDeps(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFileForMain(t, dir, "linear-orch.md")
	writeAgentFileForMain(t, dir, "agent-a")
	writeAgentFileForMain(t, dir, "agent-b")

	// Sentinel text that must appear in the first dispatch's TaskDescription
	// when pre-consultation advice is correctly applied.
	const adviceText = "pre-consultation-advice-buildDeps-sentinel-r7q"

	// In auto mode, PreConsult is called once at run start (one InvokeRaw call).
	// Routing is handled automatically; the fakeRawInvoker serves only the
	// pre-consultation request.
	fakeInvoker := &fakeRawInvoker{
		responses: [][]byte{
			[]byte(`{"task_description":"` + adviceText + `","constraints":""}`),
		},
	}

	settings := domain.RunSettings{
		Mode:            domain.ExecutionModeAuto,
		PreConsultation: true,
	}

	// Build session deps through the shared builder (same path both frontends use).
	deps := buildDepsFromTransports(fakeInvoker, &mainTestNoopInteraction{}, nil, nil)

	// Supply the infrastructure deps that buildDeps deliberately leaves empty
	// (Harness, Store, Clock, Interact are frontend-supplied, not part of the
	// routing/consultation wiring that buildDeps owns).
	f := harness.NewMockAdapter()
	deps.Harness = f
	deps.Store = &mainTestMemStore{}
	deps.Clock = mainTestClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	deps.Interact = &mainTestNoopInteraction{}

	ses := session.New(deps)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          settings,
	}

	startSessionGuarded(t, ses, cfg)

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation after the session run, got none")
	}

	// The pre-consultation advice text must appear in the first dispatch's
	// TaskDescription, confirming that the OrchestratorConsultant wired by
	// buildDeps was invoked and its advice was applied.
	if !strings.Contains(invs[0].Request.TaskDescription, adviceText) {
		t.Errorf("want first dispatch TaskDescription to contain pre-consultation advice %q, got %q",
			adviceText, invs[0].Request.TaskDescription)
	}
}

// TestBuildDeps_OrchestratedMode_RoutingConsultantIsInvoked verifies that a
// session built from buildDeps(Mode=orchestrated) routes every dispatch
// decision through the OrchestratorConsultant. The fakeRawInvoker scripted
// into buildDeps records every InvokeRaw call, so a non-zero call count
// confirms that routing decisions flowed through the consultant wired by
// buildDeps rather than being resolved internally.
//
// This is the integration-style behavioral test for the wiring that both
// frontends rely on: establishing that the interactive path, which calls
// buildDeps with the completed configuration, produces a session that actually
// consults the orchestrator for each routing decision.
func TestBuildDeps_OrchestratedMode_RoutingConsultantIsInvoked(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFileForMain(t, dir, "linear-orch.md")
	writeAgentFileForMain(t, dir, "agent-a")
	writeAgentFileForMain(t, dir, "agent-b")

	// Three routing consultations for the linear 2-agent workflow in
	// orchestrated mode: dispatch agent-a, dispatch agent-b, then stop.
	fakeInvoker := &fakeRawInvoker{
		responses: [][]byte{
			[]byte(`{"action":"dispatch","agent":"agent-a","row":1,"task_description":"orchestrated task for agent-a"}`),
			[]byte(`{"action":"dispatch","agent":"agent-b","row":2,"task_description":"orchestrated task for agent-b"}`),
			[]byte(`{"action":"stop","reason":"workflow complete"}`),
		},
	}

	settings := domain.RunSettings{
		Mode: domain.ExecutionModeOrchestrated,
	}

	deps := buildDepsFromTransports(fakeInvoker, nil, nil, nil)

	f := harness.NewMockAdapter()
	deps.Harness = f
	deps.Store = &mainTestMemStore{}
	deps.Clock = mainTestClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	deps.Interact = &mainTestNoopInteraction{}

	ses := session.New(deps)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          settings,
	}

	startSessionGuarded(t, ses, cfg)

	// A non-zero InvokeRaw call count proves that routing decisions flowed
	// through the OrchestratorConsultant that buildDeps wired. If the session
	// had dispatched without consulting the orchestrator, callCount would be 0.
	fakeInvoker.mu.Lock()
	callCount := fakeInvoker.callCount
	fakeInvoker.mu.Unlock()
	if callCount == 0 {
		t.Error("buildDeps(orchestrated) routing consultant was never invoked; " +
			"want InvokeRaw called at least once for routing decisions")
	}
}

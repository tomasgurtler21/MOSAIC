package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/mosaiclog"
)

// nativeInvocationEndKeys is the complete key set native OpenCode
// invocation_end carries (envelope plus the three invocation fields).
var nativeInvocationEndKeys = map[string]bool{
	"schema_version": true, "event": true, "timestamp": true, "harness": true, "run_id": true,
	"agent_instance_id": true, "status_code": true, "response": true,
}

const mlAgentInstance = "Research#3"

// mlSafetyNet builds a safety net over mock, writing under a fresh workspace.
func mlSafetyNet(t *testing.T, mock *MockOpenCodeHarness) (openCodeHarness, string) {
	t.Helper()
	ws := t.TempDir()
	w := mosaiclog.NewWriter(ws, "opencode", mosaiclog.WithClock(mainTestClock{t: mlFixedNow}))
	return newOpenCodeSafetyNet(mock, w), ws
}

func mlInvocationDir(ws, runID, folder string) string {
	return filepath.Join(ws, "OrchestrationLogs", runID, folder)
}

func mlMakeInvocationDir(t *testing.T, ws, runID, folder string) string {
	t.Helper()
	dir := mlInvocationDir(ws, runID, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "03_events.jsonl")
}

func mlProtocolRequest(runID string) domain.ProtocolRequest {
	return domain.ProtocolRequest{AgentInstanceID: mlAgentInstance, RunID: runID, TaskDescription: "do it"}
}

func mlSuccessResponse() domain.ProtocolResponse {
	return domain.ProtocolResponse{
		AgentInstanceID: mlAgentInstance, RunID: mlRunID,
		StatusCode: domain.StatusCode("SUCCESS"), StatusMessage: "all work finished",
	}
}

func TestOpenCodeSafetyNet_AppendsInvocationEndWhenFolderExistsWithoutOne(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	events := mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)

	got, err := net.Invoke(context.Background(), domain.AgentReference{Identifier: "Research"}, mlProtocolRequest(mlRunID))

	if err != nil || got != mock.Response {
		t.Fatalf("returned (%+v, %v), want the wrapped adapter's result unchanged", got, err)
	}
	evs := mlReadEvents(t, events)
	if len(evs) != 1 || evs[0]["event"] != "invocation_end" {
		t.Fatalf("events = %v, want exactly one invocation_end", evs)
	}
	ev := evs[0]
	if ev["agent_instance_id"] != mlAgentInstance || ev["status_code"] != "SUCCESS" || ev["run_id"] != mlRunID ||
		ev["harness"] != "opencode" || ev["schema_version"] != "1.1.0" {
		t.Errorf("event = %v, want the request's agent instance and the response's status code", ev)
	}
	if resp, _ := ev["response"].(string); !strings.Contains(resp, "all work finished") {
		t.Errorf("response = %q, want text carrying the agent's protocol response", resp)
	}
	for k := range ev {
		if !nativeInvocationEndKeys[k] {
			t.Errorf("unexpected field %q: only native OpenCode invocation_end fields are allowed", k)
		}
	}
}

func TestOpenCodeSafetyNet_DoesNotAddASecondEndWhenTheHookWroteOne(t *testing.T) {
	var events string
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	events = filepath.Join(mlInvocationDir(ws, mlRunID, mlAgentInstance), "03_events.jsonl")
	mock.OnInvoke = func(domain.ProtocolRequest) {
		hookLine := `{"event":"invocation_end","agent_instance_id":"` + mlAgentInstance + `","status_code":"SUCCESS"}` + "\n"
		if err := os.MkdirAll(filepath.Dir(events), 0o755); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(events, []byte(hookLine), 0o644); err != nil {
			t.Error(err)
		}
	}

	_, _ = net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(mlRunID))

	evs := mlReadEvents(t, events)
	if len(evs) != 1 {
		t.Fatalf("events = %v, want exactly the hook's single invocation_end", mlEventNames(evs))
	}
	if _, wrote := evs[0]["harness"]; wrote {
		t.Error("the surviving invocation_end is the Runner's, not the hook's")
	}
}

func TestOpenCodeSafetyNet_FindsFolderCreatedWhileTheProcessRan(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	mock.OnInvoke = func(domain.ProtocolRequest) { mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance) }

	_, _ = net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(mlRunID))

	evs := mlReadEvents(t, filepath.Join(mlInvocationDir(ws, mlRunID, mlAgentInstance), "03_events.jsonl"))
	if len(evs) != 1 {
		t.Fatalf("events = %v, want one invocation_end written after the process exited", evs)
	}
}

func TestOpenCodeSafetyNet_WritesNothingWhenFolderIsAbsent(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)

	got, err := net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(mlRunID))

	if err != nil || got != mock.Response {
		t.Fatalf("returned (%+v, %v), want the wrapped result unchanged", got, err)
	}
	if _, statErr := os.Stat(filepath.Join(ws, "OrchestrationLogs")); statErr == nil {
		t.Fatal("OrchestrationLogs was created, want nothing written without an invocation folder")
	}
}

func TestOpenCodeSafetyNet_RunIDFallsBackToTheAdapterRunID(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	events := mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)

	_, _ = net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(""))

	if evs := mlReadEvents(t, events); len(evs) != 1 {
		t.Fatalf("events = %v, want one invocation_end under the adapter's run id", evs)
	}
}

func TestOpenCodeSafetyNet_RequestRunIDWinsOverTheAdapterRunID(t *testing.T) {
	const otherRun = "20261003T190000Z-aaaa"
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	own := mlMakeInvocationDir(t, ws, otherRun, mlAgentInstance)
	adapters := mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)

	_, _ = net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(otherRun))

	if len(mlReadEvents(t, own)) != 1 || len(mlReadEvents(t, adapters)) != 0 {
		t.Fatal("the invocation_end must go to the request's run, not the adapter's")
	}
}

func TestOpenCodeSafetyNet_NoUsableRunIDWritesNothing(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: "", Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)

	_, _ = net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(""))

	if evs := mlReadEvents(t, filepath.Join(mlInvocationDir(ws, mlRunID, mlAgentInstance), "03_events.jsonl")); len(evs) != 0 {
		t.Fatalf("events = %v, want none without a run id", evs)
	}
}

func TestOpenCodeSafetyNet_ErrorReturnStillClosesInvocationWithoutResponseFields(t *testing.T) {
	boom := errors.New("opencode exited abnormally")
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Err: boom}
	net, ws := mlSafetyNet(t, mock)
	events := mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)

	_, err := net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(mlRunID))

	if err != boom {
		t.Fatalf("error = %v, want the wrapped adapter's error unchanged", err)
	}
	evs := mlReadEvents(t, events)
	if len(evs) != 1 || evs[0]["event"] != "invocation_end" || evs[0]["agent_instance_id"] != mlAgentInstance {
		t.Fatalf("events = %v, want one invocation_end for the instance", evs)
	}
	for _, k := range []string{"status_code", "response"} {
		if _, ok := evs[0][k]; ok {
			t.Errorf("%s must be omitted after an error return", k)
		}
	}
}

func TestOpenCodeSafetyNet_WriteFailureNeverChangesTheResult(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, Response: mlSuccessResponse()}
	net, ws := mlSafetyNet(t, mock)
	// 03_events.jsonl is a directory: the existence check and the append both fail.
	mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)
	if err := os.MkdirAll(filepath.Join(mlInvocationDir(ws, mlRunID, mlAgentInstance), "03_events.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := net.Invoke(context.Background(), domain.AgentReference{}, mlProtocolRequest(mlRunID))

	if err != nil || got != mock.Response {
		t.Fatalf("returned (%+v, %v), want the wrapped adapter's result unchanged", got, err)
	}
}

func TestOpenCodeSafetyNet_InvokeRawIsForwardedUnchangedWithoutLogging(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, RawReply: []byte("raw reply bytes"), RawErr: errors.New("raw failure")}
	net, ws := mlSafetyNet(t, mock)
	events := mlMakeInvocationDir(t, ws, mlRunID, mlAgentInstance)
	agent := domain.AgentReference{Identifier: "Orchestrator", DefinitionPath: "x.md", InvocationKind: domain.InvocationOrchestrator}
	payload := []byte(`{"consult":"payload"}`)

	reply, err := net.InvokeRaw(context.Background(), agent, payload)

	if string(reply) != "raw reply bytes" || err != mock.RawErr {
		t.Fatalf("returned (%q, %v), want the wrapped bytes and error unchanged", reply, err)
	}
	if len(mock.RawAgents) != 1 || mock.RawAgents[0] != agent || string(mock.RawPayloads[0]) != string(payload) {
		t.Fatalf("wrapped adapter saw agent %v payload %q, want them unchanged", mock.RawAgents, mock.RawPayloads)
	}
	if len(mock.InvokeRequests) != 0 {
		t.Error("InvokeRaw must not route through Invoke")
	}
	if evs := mlReadEvents(t, events); len(evs) != 0 {
		t.Fatalf("events = %v, want no safety-net write for a raw call", evs)
	}
}

func TestOpenCodeSafetyNet_ForwardsExecutablePathAndRunID(t *testing.T) {
	mock := &MockOpenCodeHarness{RunIDValue: mlRunID, ExecPath: "/usr/bin/opencode"}
	net, _ := mlSafetyNet(t, mock)

	if net.ExecutablePath() != "/usr/bin/opencode" || net.RunID() != mlRunID {
		t.Fatalf("ExecutablePath=%q RunID=%q, want the wrapped adapter's values", net.ExecutablePath(), net.RunID())
	}
}

func TestOpenCodeSafetyNet_SatisfiesEveryOptionalCapabilityByTypeAssertion(t *testing.T) {
	net, _ := mlSafetyNet(t, &MockOpenCodeHarness{})
	var h domain.HarnessAdapter = net

	if _, ok := h.(domain.RawInvoker); !ok {
		t.Error("decorated adapter lost domain.RawInvoker")
	}
	if _, ok := h.(domain.ExecutableRevealer); !ok {
		t.Error("decorated adapter lost domain.ExecutableRevealer")
	}
	if _, ok := h.(runIDReporter); !ok {
		t.Error("decorated adapter lost runIDReporter")
	}
}

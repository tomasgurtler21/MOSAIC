package interceptor_test

// Shell-level coverage of the subagent hand-back completion on the Claude
// Code adapter. These tests exchange real Claude Code hook payloads with the
// interceptor shell (pre-dispatch, agent-start, hand-back PostToolUse,
// SubagentStop) and assert on what a run leaves behind: invocation-log
// records, the neutral reply, the early-exit sentinel and committed state.
//
// The hand-back PostToolUse is the completion moment; a later SubagentStop or
// a second hand-back for the same agent adds nothing; with no hand-back the
// SubagentStop stays the completion. Sentinel ordering tests drive the
// hand-back firing to completion before the next dispatch is issued, so none
// of them depends on timing.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/interceptor"
)

const handbackWorker = "Worker"

// --- payload builders -------------------------------------------------------

func hbJSON(t *testing.T, v map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func hbPre(t *testing.T, instance, toolUseID string) []byte {
	t.Helper()
	prompt := hbJSON(t, map[string]any{
		"agent_instance_id": instance, "run_id": "run-1", "task_description": "do the work",
		"input_artifacts": []string{}, "output_artifacts": []string{},
		"include_result_summary": false, "human_in_the_loop": false,
	})
	return hbJSON(t, map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "sess-1", "cwd": "/somewhere",
		"tool_name": "Agent", "tool_use_id": toolUseID,
		"tool_input": map[string]any{"subagent_type": handbackWorker, "description": "do the work", "prompt": string(prompt)},
	})
}

func hbStart(t *testing.T, agentID string) []byte {
	t.Helper()
	return hbJSON(t, map[string]any{
		"hook_event_name": "SubagentStart", "session_id": "sess-1", "agent_id": agentID, "agent_type": handbackWorker,
	})
}

func hbHandback(t *testing.T, event, agentID string, toolInput any) []byte {
	t.Helper()
	p := map[string]any{
		"hook_event_name": event, "session_id": "sess-1", "agent_id": agentID, "agent_type": handbackWorker,
		"permission_mode": "auto", "tool_name": "SubagentHandback", "tool_use_id": "toolu_handback_" + agentID,
	}
	if toolInput != nil {
		p["tool_input"] = toolInput
	}
	if event == "PostToolUse" {
		p["tool_response"] = map[string]any{"success": true, "message": "Report delivered to your caller."}
	}
	return hbJSON(t, p)
}

func hbMessage(t *testing.T, agentID, message string) []byte {
	return hbHandback(t, "PostToolUse", agentID, map[string]any{"message": message})
}

func hbStop(t *testing.T, agentID, last string) []byte {
	t.Helper()
	return hbJSON(t, map[string]any{
		"hook_event_name": "SubagentStop", "session_id": "sess-1", "agent_id": agentID,
		"last_assistant_message": last, "stop_hook_active": false,
	})
}

// --- harness assembly -------------------------------------------------------

func hbRegistry(responses ...string) domain.StubRegistry {
	reg := domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}
	for i, r := range responses {
		reg.Stubs = append(reg.Stubs, domain.Stub{
			Match:    domain.StubMatch{Identity: domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: handbackWorker}, Invocation: i + 1},
			Response: json.RawMessage(r),
		})
	}
	return reg
}

func hbHarness(t *testing.T, state domain.RunState, reg domain.StubRegistry) testHarness {
	t.Helper()
	h := newHarness(t, state, reg, claudecode.Capabilities(), nil)
	h.Config.Adapter = claudecode.New(claudecode.Options{})
	return h
}

// hbDispatch performs the pre-dispatch and the agent-start of one subagent.
func hbDispatch(t *testing.T, h testHarness, instance, toolUseID, agentID string) (preReply []byte) {
	t.Helper()
	reply, code, _ := run(t, h.Config, domain.PhasePre, hbPre(t, instance, toolUseID))
	if code != 0 {
		t.Fatalf("pre-dispatch exit code %d", code)
	}
	if _, code, _ := run(t, h.Config, domain.PhaseAgentStart, hbStart(t, agentID)); code != 0 {
		t.Fatalf("agent-start exit code %d", code)
	}
	return reply
}

func hbComplete(t *testing.T, h testHarness, native []byte) (reply []byte, diag []byte) {
	t.Helper()
	reply, code, diag := run(t, h.Config, domain.PhaseCompletion, native)
	if code != 0 {
		t.Fatalf("completion exit code %d, want 0", code)
	}
	if !json.Valid(reply) {
		t.Fatalf("completion reply is not valid JSON: %q", reply)
	}
	if strings.Contains(string(reply), "deny") {
		t.Errorf("completion reply denies: %s", reply)
	}
	return reply, diag
}

func hbRecords(t *testing.T, h testHarness, kind domain.RecordKind) []domain.LogRecord {
	t.Helper()
	var out []domain.LogRecord
	for _, r := range readLog(t, h.Log) {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func hbRunEvents(t *testing.T, h testHarness, ev domain.RunEventKind) int {
	t.Helper()
	n := 0
	for _, r := range hbRecords(t, h, domain.RecordRun) {
		if r.Event == ev {
			n++
		}
	}
	return n
}

func hbSentinel(h testHarness) bool {
	_, err := os.Stat(filepath.Join(h.ControlDir, domain.EarlyExitSentinelName))
	return err == nil
}

const okReply = `{"status_code":"SUCCESS"}`

// --- completion ---------------------------------------------------------------

func TestRun_Handback_RecordsOneEndWithEchoComparedAgainstTheMessage(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	hbComplete(t, h, hbMessage(t, "agent-1", okReply))

	ends := hbRecords(t, h, domain.RecordEnd)
	if len(ends) != 1 {
		t.Fatalf("end records = %d, want exactly 1", len(ends))
	}
	if ends[0].Echo == nil || !ends[0].Echo.Match {
		t.Errorf("end record echo = %+v, want a match against the hand-back message", ends[0].Echo)
	}
	if ends[0].CorrelationToken != "tu-1" {
		t.Errorf("end record token = %q, want the dispatch's %q (resolved through agent_id)", ends[0].CorrelationToken, "tu-1")
	}
}

func TestRun_Handback_MessageThatDiffersFromTheStubIsAnEchoMismatch(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	hbComplete(t, h, hbMessage(t, "agent-1", `{"status_code":"FAILED"}`))

	ends := hbRecords(t, h, domain.RecordEnd)
	if len(ends) != 1 || ends[0].Echo == nil || ends[0].Echo.Match {
		t.Fatalf("end records = %+v, want one with an echo mismatch", ends)
	}
}

func TestRun_Handback_LaterSubagentStopCommentaryAddsNoRecordsAndNoMismatch(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")
	hbComplete(t, h, hbMessage(t, "agent-1", okReply))
	before := len(readLog(t, h.Log))

	hbComplete(t, h, hbStop(t, "agent-1", "Report delivered. Let me know if you need anything else."))

	if after := len(readLog(t, h.Log)); after != before {
		t.Errorf("log grew from %d to %d records on the SubagentStop after a hand-back; it must add nothing", before, after)
	}
	ends := hbRecords(t, h, domain.RecordEnd)
	if len(ends) != 1 || ends[0].Echo == nil || !ends[0].Echo.Match {
		t.Errorf("end records = %+v, want the single hand-back completion with its matching echo", ends)
	}
	if n := hbRunEvents(t, h, domain.RunEventUncorrelatedCompletion); n != 0 {
		t.Errorf("uncorrelated-completion events = %d, want 0", n)
	}
}

func TestRun_Handback_SecondHandbackFromTheSameAgentAddsNothing(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")
	hbComplete(t, h, hbMessage(t, "agent-1", okReply))
	before := len(readLog(t, h.Log))

	hbComplete(t, h, hbMessage(t, "agent-1", `{"status_code":"FAILED"}`))

	if after := len(readLog(t, h.Log)); after != before {
		t.Errorf("log grew from %d to %d records on a second hand-back; it must add nothing", before, after)
	}
	if ends := hbRecords(t, h, domain.RecordEnd); len(ends) != 1 || !ends[0].Echo.Match {
		t.Errorf("end records = %+v, want the first hand-back's single matching completion", ends)
	}
}

func TestRun_NoHandback_SubagentStopRemainsTheCompletion(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	hbComplete(t, h, hbStop(t, "agent-1", okReply))

	ends := hbRecords(t, h, domain.RecordEnd)
	if len(ends) != 1 || ends[0].Echo == nil || !ends[0].Echo.Match {
		t.Errorf("end records = %+v, want one completion from last_assistant_message with a matching echo", ends)
	}
}

func TestRun_Handback_TwoSubagentsInFlightAreEachAttributedByAgentID(t *testing.T) {
	replyA, replyB := `{"status_code":"SUCCESS","who":"A"}`, `{"status_code":"SUCCESS","who":"B"}`
	h := hbHarness(t, baseState(), hbRegistry(replyA, replyB))
	hbDispatch(t, h, "Worker#1", "tu-A", "agent-A")
	hbDispatch(t, h, "Worker#2", "tu-B", "agent-B")

	hbComplete(t, h, hbMessage(t, "agent-B", replyB))
	hbComplete(t, h, hbMessage(t, "agent-A", replyA))

	ends := hbRecords(t, h, domain.RecordEnd)
	if len(ends) != 2 {
		t.Fatalf("end records = %d, want 2", len(ends))
	}
	for _, e := range ends {
		if e.Echo == nil || !e.Echo.Match {
			t.Errorf("end record for token %q echo = %+v, want a match: each hand-back must be compared with its own dispatch's stub", e.CorrelationToken, e.Echo)
		}
	}
	tokens := map[string]bool{ends[0].CorrelationToken: true, ends[1].CorrelationToken: true}
	if !tokens["tu-A"] || !tokens["tu-B"] {
		t.Errorf("end record tokens = %v, want both dispatches completed once each", tokens)
	}
}

// --- early exit ---------------------------------------------------------------

func TestRun_Handback_AtNthDispatchWritesSentinelAfterTheReplyAndHaltsTheNextDispatch(t *testing.T) {
	state := baseState()
	state.EarlyExitThreshold = 1
	h := hbHarness(t, state, hbRegistry(okReply, okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")
	if hbSentinel(h) {
		t.Fatalf("sentinel exists before any completion")
	}

	// Drive the hand-back firing to completion, observing sentinel state at
	// the instant the reply is first written.
	out := &orderCheckWriter{sentinelPath: filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)}
	cfg := h.Config
	cfg.Phase = domain.PhaseCompletion
	cfg.In = bytes.NewReader(hbMessage(t, "agent-1", okReply))
	cfg.Out = out
	cfg.Diag = &bytes.Buffer{}
	if code := interceptor.Run(context.Background(), cfg); code != 0 {
		t.Fatalf("hand-back completion exit code %d", code)
	}

	if out.buf.Len() == 0 {
		t.Errorf("no reply was written by the hand-back firing")
	}
	if !hbSentinel(h) {
		t.Fatalf("the Nth hand-back did not write the early-exit sentinel")
	}
	if out.sentinelExisted {
		t.Errorf("sentinel already existed when the reply was written; the reply must come first")
	}
	if n := hbRunEvents(t, h, domain.RunEventEarlyExitTriggered); n != 1 {
		t.Errorf("early-exit-triggered events = %d, want 1", n)
	}

	// A dispatch made after the sentinel is halted on entry: no start record.
	startsBefore := len(hbRecords(t, h, domain.RecordStart))
	reply, code, _ := run(t, h.Config, domain.PhasePre, hbPre(t, "Worker#2", "tu-2"))
	if code != 0 {
		t.Fatalf("pre-dispatch after sentinel exit code %d", code)
	}
	if !strings.Contains(string(reply), "deny") {
		t.Errorf("pre-dispatch after the sentinel reply = %s, want a denial", reply)
	}
	if startsAfter := len(hbRecords(t, h, domain.RecordStart)); startsAfter != startsBefore {
		t.Errorf("start records went from %d to %d; a halted dispatch must leave none", startsBefore, startsAfter)
	}
}

func TestRun_Handback_BelowTheThresholdWritesNoSentinel(t *testing.T) {
	state := baseState()
	state.EarlyExitThreshold = 2
	h := hbHarness(t, state, hbRegistry(okReply, okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	hbComplete(t, h, hbMessage(t, "agent-1", okReply))

	if hbSentinel(h) {
		t.Errorf("sentinel written after the 1st of 2 dispatches")
	}
}

func TestRun_Handback_LaterSubagentStopAfterTheNthHandbackAddsNoSecondCutoff(t *testing.T) {
	state := baseState()
	state.EarlyExitThreshold = 1
	h := hbHarness(t, state, hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")
	hbComplete(t, h, hbMessage(t, "agent-1", okReply))
	before := len(readLog(t, h.Log))

	hbComplete(t, h, hbStop(t, "agent-1", "bye"))

	if after := len(readLog(t, h.Log)); after != before {
		t.Errorf("log grew from %d to %d on SubagentStop after the cutting hand-back", before, after)
	}
	if n := hbRunEvents(t, h, domain.RunEventEarlyExitTriggered); n != 1 {
		t.Errorf("early-exit-triggered events = %d, want exactly 1", n)
	}
}

func TestRun_HandbackPreToolUse_NeverCompletesNorCutsOff(t *testing.T) {
	state := baseState()
	state.EarlyExitThreshold = 1
	h := hbHarness(t, state, hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	reply, code, _ := run(t, h.Config, domain.PhaseCompletion, hbHandback(t, "PreToolUse", "agent-1", map[string]any{"message": okReply}))

	if code != 0 || !json.Valid(reply) {
		t.Fatalf("exit code %d, reply %q: want a contained neutral reply", code, reply)
	}
	if hbSentinel(h) {
		t.Errorf("hand-back PreToolUse wrote the early-exit sentinel")
	}
	if ends := hbRecords(t, h, domain.RecordEnd); len(ends) != 0 {
		t.Errorf("end records = %+v, want none: PreToolUse fires before delivery", ends)
	}
	// The SubagentStop fallback still completes the dispatch afterwards.
	hbComplete(t, h, hbStop(t, "agent-1", okReply))
	if ends := hbRecords(t, h, domain.RecordEnd); len(ends) != 1 {
		t.Errorf("end records after SubagentStop = %d, want 1", len(ends))
	}
}

// --- edge cases: explicit, reported outcomes ---------------------------------

func TestRun_Handback_EmptyAndNonJSONMessagesStillCompleteWithAnEchoMismatchRecord(t *testing.T) {
	for name, message := range map[string]string{"empty": "", "non-JSON": "All finished, nothing to report."} {
		t.Run(name, func(t *testing.T) {
			h := hbHarness(t, baseState(), hbRegistry(okReply))
			hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

			hbComplete(t, h, hbMessage(t, "agent-1", message))

			ends := hbRecords(t, h, domain.RecordEnd)
			if len(ends) != 1 {
				t.Fatalf("end records = %d, want 1: the message is still the delivery", len(ends))
			}
			if ends[0].Echo == nil || ends[0].Echo.Match {
				t.Errorf("echo = %+v, want an explicit mismatch record", ends[0].Echo)
			}
			if errs := hbRecords(t, h, domain.RecordError); len(errs) != 0 {
				t.Errorf("error records = %+v, want none: this is a delivery, not a failure", errs)
			}
		})
	}
}

func TestRun_Handback_EmptyMessageCountsTowardTheCutoff(t *testing.T) {
	state := baseState()
	state.EarlyExitThreshold = 1
	h := hbHarness(t, state, hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	hbComplete(t, h, hbMessage(t, "agent-1", ""))

	if !hbSentinel(h) {
		t.Errorf("an empty hand-back message did not trigger the cutoff")
	}
}

func recordShape(rs []domain.LogRecord) []string {
	var out []string
	for _, r := range rs {
		s := string(r.Kind) + "/" + string(r.Event)
		if r.Echo != nil {
			s += "/echo"
		}
		out = append(out, s)
	}
	return out
}

func TestRun_Handback_UnstubbedEmptyMessageLeavesTheSameRecordsAsAnEmptySubagentStop(t *testing.T) {
	unstubbed := domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough}

	viaHandback := hbHarness(t, baseState(), unstubbed)
	hbDispatch(t, viaHandback, "Worker#1", "tu-1", "agent-1")
	hbComplete(t, viaHandback, hbMessage(t, "agent-1", ""))

	viaStop := hbHarness(t, baseState(), unstubbed)
	hbDispatch(t, viaStop, "Worker#1", "tu-1", "agent-1")
	hbComplete(t, viaStop, hbStop(t, "agent-1", ""))

	got, want := recordShape(readLog(t, viaHandback.Log)), recordShape(readLog(t, viaStop.Log))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("records via hand-back = %v, want the same as the SubagentStop completion %v", got, want)
	}
	if errs := hbRecords(t, viaHandback, domain.RecordError); len(errs) != 0 {
		t.Errorf("error records = %+v, want none for an unstubbed empty delivery", errs)
	}
}

func TestRun_Handback_UnparseableOrIncompletePayloadsAreReportedAndSubagentStopStillCompletes(t *testing.T) {
	cases := map[string][]byte{
		"not JSON":             []byte(`{"hook_event_name": "PostToolUse", oops`),
		"message key absent":   hbHandback(t, "PostToolUse", "agent-1", map[string]any{"other": "x"}),
		"message not a string": hbHandback(t, "PostToolUse", "agent-1", map[string]any{"message": 12}),
		"tool_input absent":    hbHandback(t, "PostToolUse", "agent-1", nil),
	}
	for name, native := range cases {
		t.Run(name, func(t *testing.T) {
			h := hbHarness(t, baseState(), hbRegistry(okReply))
			hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

			reply, code, diag := run(t, h.Config, domain.PhaseCompletion, native)

			if code != 0 || !json.Valid(reply) {
				t.Fatalf("exit code %d, reply %q: want a contained neutral reply", code, reply)
			}
			if len(diag) == 0 {
				t.Errorf("no diagnostic written for an unusable hand-back payload")
			}
			if errs := hbRecords(t, h, domain.RecordError); len(errs) == 0 {
				t.Errorf("no error record in the invocation log for an unusable hand-back payload")
			}
			if ends := hbRecords(t, h, domain.RecordEnd); len(ends) != 0 {
				t.Errorf("end records = %+v, want none: nothing was delivered", ends)
			}

			hbComplete(t, h, hbStop(t, "agent-1", okReply))
			if ends := hbRecords(t, h, domain.RecordEnd); len(ends) != 1 {
				t.Errorf("end records after the SubagentStop fallback = %d, want 1", len(ends))
			}
		})
	}
}

func TestRun_Handback_FromAnAgentThatWasNeverDispatchedIsContainedAndCompletesOnce(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")

	beforeHelper := len(readLog(t, h.Log))
	hbComplete(t, h, hbMessage(t, "helper-agent", "helper report"))
	afterHandback := len(readLog(t, h.Log))
	if afterHandback <= beforeHelper {
		t.Errorf("log stayed at %d records after a hand-back from an undispatched agent; it must leave an explicit record", beforeHelper)
	}
	hbComplete(t, h, hbStop(t, "helper-agent", "helper wrap-up"))

	if after := len(readLog(t, h.Log)); after != afterHandback {
		t.Errorf("log grew from %d to %d on the helper's later SubagentStop; the hand-back was already its one completion", afterHandback, after)
	}
	// The dispatched agent is unaffected and still completes normally.
	hbComplete(t, h, hbMessage(t, "agent-1", okReply))
	found := false
	for _, e := range hbRecords(t, h, domain.RecordEnd) {
		if e.CorrelationToken == "tu-1" && e.Echo != nil && e.Echo.Match {
			found = true
		}
	}
	if !found {
		t.Errorf("the dispatched agent's completion was lost after a helper's hand-back: %+v", hbRecords(t, h, domain.RecordEnd))
	}
}

func TestRun_SubagentStopThenHandbackForTheSameAgentAddsNothing(t *testing.T) {
	h := hbHarness(t, baseState(), hbRegistry(okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")
	hbComplete(t, h, hbStop(t, "agent-1", okReply))
	before := len(readLog(t, h.Log))

	hbComplete(t, h, hbMessage(t, "agent-1", okReply))

	if after := len(readLog(t, h.Log)); after != before {
		t.Errorf("log grew from %d to %d on a hand-back after the SubagentStop completion; it must add nothing", before, after)
	}
	if ends := hbRecords(t, h, domain.RecordEnd); len(ends) != 1 {
		t.Errorf("end records = %d, want exactly 1", len(ends))
	}
	if n := hbRunEvents(t, h, domain.RunEventUncorrelatedCompletion); n != 0 {
		t.Errorf("uncorrelated-completion events = %d, want 0", n)
	}
}

func TestRun_Handback_SecondOfTwoDispatchesWritesTheSentinelAtThresholdTwo(t *testing.T) {
	state := baseState()
	state.EarlyExitThreshold = 2
	h := hbHarness(t, state, hbRegistry(okReply, okReply))
	hbDispatch(t, h, "Worker#1", "tu-1", "agent-1")
	hbDispatch(t, h, "Worker#2", "tu-2", "agent-2")

	hbComplete(t, h, hbMessage(t, "agent-1", okReply))
	if hbSentinel(h) {
		t.Fatalf("sentinel written after the 1st of 2 hand-backs")
	}
	hbComplete(t, h, hbMessage(t, "agent-2", okReply))

	if !hbSentinel(h) {
		t.Errorf("the 2nd hand-back at threshold 2 did not write the early-exit sentinel")
	}
	if n := hbRunEvents(t, h, domain.RunEventEarlyExitTriggered); n != 1 {
		t.Errorf("early-exit-triggered events = %d, want 1", n)
	}
}

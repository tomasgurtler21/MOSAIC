package claudecode_test

// Replay of the synthetic hook-payload fixture built from the key sets
// observed live on Claude Code 2.1.284 (auto mode): the subagent hand-back
// PreToolUse / PostToolUse pair and the SubagentStart / SubagentStop of the
// same subagent. Fixture: testdata/live_payloads_claudecode_2.1.284.jsonl,
// same shape as the 2.1.240 fixture.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func loadHandbackCapture(t *testing.T) []liveCaptureEntry {
	t.Helper()
	f, err := os.Open("testdata/live_payloads_claudecode_2.1.284.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	var entries []liveCaptureEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e liveCaptureEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("parse fixture line: %v", err)
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return entries
}

func TestLiveCapture284_FixtureCarriesTheObservedKeySets(t *testing.T) {
	entries := loadHandbackCapture(t)

	handbackPre := []string{"agent_id", "agent_type", "cwd", "hook_event_name", "permission_mode", "prompt_id", "session_id", "tool_input", "tool_name", "tool_use_id", "transcript_path"}
	want := map[string][]string{
		"PreToolUse":    handbackPre,
		"PostToolUse":   append(append([]string{}, handbackPre...), "tool_response", "duration_ms"),
		"SubagentStart": {"agent_id", "agent_type", "cwd", "hook_event_name", "prompt_id", "session_id", "transcript_path"},
		"SubagentStop":  {"agent_id", "agent_transcript_path", "agent_type", "background_tasks", "cwd", "hook_event_name", "last_assistant_message", "permission_mode", "prompt_id", "session_crons", "session_id", "stop_hook_active", "transcript_path"},
	}
	for hook, keys := range want {
		got := sortedCopy(liveCaptureEntryFor(t, entries, hook).PayloadKeys)
		if !reflect.DeepEqual(got, sortedCopy(keys)) {
			t.Errorf("%s payload keys = %v, want %v", hook, got, sortedCopy(keys))
		}
	}
}

func TestLiveCapture284_HandbackPayloadStruct_AllFieldsPresentInCapture(t *testing.T) {
	entry := liveCaptureEntryFor(t, loadHandbackCapture(t), "PostToolUse")
	present := map[string]bool{}
	for _, k := range entry.PayloadKeys {
		present[k] = true
	}
	for _, name := range jsonFieldNamesOf(claudecode.HandbackPayload{}) {
		if !present[name] {
			t.Errorf("HandbackPayload declares %q but the live hand-back PostToolUse never carries it (keys: %v)", name, entry.PayloadKeys)
		}
	}
}

func TestLiveCapture284_HandbackPostToolUse_TranslatesToCompletionWithMessageAsReply(t *testing.T) {
	entries := loadHandbackCapture(t)
	post := liveCaptureEntryFor(t, entries, "PostToolUse")
	stop := liveCaptureEntryFor(t, entries, "SubagentStop")

	var payload struct {
		AgentID   string `json:"agent_id"`
		ToolInput struct {
			Message string `json:"message"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(post.Payload, &payload); err != nil {
		t.Fatalf("decode fixture payload: %v", err)
	}
	var stopPayload struct {
		AgentID string `json:"agent_id"`
		Last    string `json:"last_assistant_message"`
	}
	if err := json.Unmarshal(stop.Payload, &stopPayload); err != nil {
		t.Fatalf("decode fixture payload: %v", err)
	}

	call, err := claudecode.New(claudecode.Options{}).TranslateCall(domain.PhaseCompletion, post.Payload)
	if err != nil {
		t.Fatalf("TranslateCall(hand-back PostToolUse): %v", err)
	}

	if call.ObservedResponse != payload.ToolInput.Message || call.ObservedResponse == "" {
		t.Errorf("ObservedResponse = %q, want the hand-back tool_input.message %q", call.ObservedResponse, payload.ToolInput.Message)
	}
	if call.ObservedResponse == stopPayload.Last {
		t.Errorf("ObservedResponse equals the later last_assistant_message; post-hand-back text must never be the reply")
	}
	if call.AgentID != stopPayload.AgentID {
		t.Errorf("AgentID = %q, want %q: the hand-back and SubagentStop of one subagent share agent_id", call.AgentID, stopPayload.AgentID)
	}
}

func TestLiveCapture284_HandbackAndAgentStartShareTheCorrelationKey(t *testing.T) {
	entries := loadHandbackCapture(t)
	a := claudecode.New(claudecode.Options{})

	start, err := a.TranslateCall(domain.PhaseAgentStart, liveCaptureEntryFor(t, entries, "SubagentStart").Payload)
	if err != nil {
		t.Fatalf("TranslateCall(agent-start): %v", err)
	}
	done, err := a.TranslateCall(domain.PhaseCompletion, liveCaptureEntryFor(t, entries, "PostToolUse").Payload)
	if err != nil {
		t.Fatalf("TranslateCall(completion): %v", err)
	}
	if start.AgentID == "" || start.AgentID != done.AgentID {
		t.Errorf("agent-start AgentID %q vs hand-back AgentID %q, want equal and non-empty", start.AgentID, done.AgentID)
	}
}

func TestLiveCapture284_HandbackPreToolUse_IsNotACompletion(t *testing.T) {
	pre := liveCaptureEntryFor(t, loadHandbackCapture(t), "PreToolUse")

	_, err := claudecode.New(claudecode.Options{}).TranslateCall(domain.PhaseCompletion, pre.Payload)

	if !errors.Is(err, claudecode.ErrPayloadUnrecognised) {
		t.Errorf("TranslateCall(hand-back PreToolUse) error = %v, want %v", err, claudecode.ErrPayloadUnrecognised)
	}
}

func TestLiveCapture284_SubagentStop_StillTranslatesAsFallbackCompletion(t *testing.T) {
	stop := liveCaptureEntryFor(t, loadHandbackCapture(t), "SubagentStop")

	call, err := claudecode.New(claudecode.Options{}).TranslateCall(domain.PhaseCompletion, stop.Payload)
	if err != nil {
		t.Fatalf("TranslateCall(SubagentStop): %v", err)
	}
	if call.ObservedResponse != "Report sent. Nothing further." {
		t.Errorf("ObservedResponse = %q, want last_assistant_message", call.ObservedResponse)
	}
}

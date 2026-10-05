package app_test

// Builds on-disk run trees for the Runner-shape coverage tests. A tree is
// written into a temporary directory in one of two shapes:
//
//   - native: one run_start/run_end pair, one session pair, one transcript and
//     orchestrator tool_call events for the subagent dispatches.
//   - runner: one run_start/run_end pair and one session pair per
//     script-orchestrator call, session-scoped transcripts, no orchestrator
//     tool_call events, no adapter_version/model on run_start/run_end, no
//     session_start/session_end in invocation folders, and a dot-prefixed
//     state directory at the run root.
//
// Both shapes carry identical usage-bearing events, so the analysis of the two
// must agree.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const shapeRunID = "20261003T185044Z-07e9"

// shapeAgent is one subagent invocation folder.
type shapeAgent struct {
	id     string
	events []map[string]any // everything between the folder's first and last event, in order
	want   [4]int64         // expected input, cache read, cache creation, output tokens
}

// shapeSpec describes one harness-specific usage shape.
type shapeSpec struct {
	harness   string
	model     string
	orchCalls [2][]map[string]any // usage-bearing orchestrator events per script-orchestrator call
	wantOrch  [4]int64
	agents    []shapeAgent
}

// shapeEvent builds an event envelope for the spec's harness.
func shapeEvent(harness, kind string, fields map[string]any) map[string]any {
	ev := map[string]any{
		"schema_version": "1.1.0",
		"event":          kind,
		"timestamp":      "2026-10-03T18:50:44Z",
		"harness":        harness,
		"session_id":     "sess-1",
		"run_id":         shapeRunID,
	}
	for k, v := range fields {
		ev[k] = v
	}
	return ev
}

// tokenUsage renders [input, cache read, cache creation, output] as a
// token_usage object, omitting categories that are zero.
func tokenUsage(u [4]int64) map[string]any {
	out := map[string]any{}
	for i, key := range []string{"input_tokens", "cache_read_tokens", "cache_creation_tokens", "output_tokens"} {
		if u[i] != 0 {
			out[key] = u[i]
		}
	}
	return out
}

// writeJSONL writes one JSON object per line to path, creating parent folders.
func writeJSONL(t *testing.T, path string, events []map[string]any) {
	t.Helper()
	var data []byte
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal fixture event: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	writeShapeFile(t, path, data)
}

func writeShapeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture folder: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

// writeShapeTree writes the run tree for spec and returns the logs root
// ({tmp}/OrchestrationLogs). runnerShaped selects the Runner shape.
func writeShapeTree(t *testing.T, spec shapeSpec, runnerShaped bool) string {
	t.Helper()
	logsRoot := filepath.Join(t.TempDir(), "OrchestrationLogs")
	runDir := filepath.Join(logsRoot, shapeRunID)
	h := spec.harness

	
	var orch []map[string]any
	if runnerShaped {
		for i, call := range spec.orchCalls {
			orch = append(orch, shapeEvent(h, "run_start", map[string]any{"cwd": "/work"}),
				shapeEvent(h, "session_start", nil))
			orch = append(orch, call...)
			orch = append(orch, shapeEvent(h, "session_end", nil),
				shapeEvent(h, "run_end", map[string]any{"outcome": "completed"}))
			writeShapeFile(t, filepath.Join(runDir,
				"00_orchestrator_session__"+h+"__sess-"+string(rune('1'+i))+".raw"), []byte("\x00not jsonl\n"))
		}
		writeShapeFile(t, filepath.Join(runDir, ".runner-session", "state.json"), []byte("{}"))
	} else {
		orch = append(orch, shapeEvent(h, "run_start", map[string]any{
			"cwd": "/work", "adapter_version": "1.0.0", "model": spec.model,
		}), shapeEvent(h, "session_start", nil))
		for _, agent := range spec.agents {
			orch = append(orch,
				shapeEvent(h, "tool_call_start", map[string]any{"call_id": "c-" + agent.id, "tool_name": "Task"}),
				shapeEvent(h, "tool_call_end", map[string]any{"call_id": "c-" + agent.id, "tool_name": "Task"}))
		}
		for _, call := range spec.orchCalls {
			orch = append(orch, call...)
		}
		orch = append(orch, shapeEvent(h, "session_end", nil),
			shapeEvent(h, "run_end", map[string]any{"outcome": "completed"}))
		writeShapeFile(t, filepath.Join(runDir, "00_orchestrator_session.raw"), []byte("\x00not jsonl\n"))
	}
	writeJSONL(t, filepath.Join(runDir, "00_orchestrator_events.jsonl"), orch)

	for _, agent := range spec.agents {
		var events []map[string]any
		if !runnerShaped {
			events = append(events, shapeEvent(h, "session_start", nil))
		}
		events = append(events, agent.events...)
		if !runnerShaped {
			events = append(events, shapeEvent(h, "session_end", nil))
		}
		writeJSONL(t, filepath.Join(runDir, agent.id, "03_events.jsonl"), events)
	}
	return logsRoot
}

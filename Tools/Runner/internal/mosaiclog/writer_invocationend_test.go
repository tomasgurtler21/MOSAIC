package mosaiclog

import (
	"os"
	"path/filepath"
	"testing"
)

// invocationEndKeys is the complete key set a fallback invocation_end may carry.
var invocationEndKeys = map[string]bool{
	"schema_version": true, "event": true, "timestamp": true, "harness": true, "run_id": true,
	"agent_instance_id": true, "status_code": true, "response": true,
}

// makeInvocationFolder creates {root}/OrchestrationLogs/{runID}/{folder}.
func makeInvocationFolder(t *testing.T, root, folder string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "OrchestrationLogs", testRunID, folder), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureInvocationEnd_AppendsWhenFolderExistsWithoutEnd(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	makeInvocationFolder(t, root, "Research#3")

	res := w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "Research#3", StatusCode: "SUCCESS", Response: "done"})

	if res != EnsureAppended {
		t.Fatalf("result = %q, want %q", res, EnsureAppended)
	}
	events := readEvents(t, invocationLogPath(root, testRunID, "Research#3"))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	want := map[string]any{
		"schema_version": "1.1.0", "event": "invocation_end", "timestamp": "2026-10-03T18:50:44.123Z",
		"harness": "opencode", "run_id": testRunID, "agent_instance_id": "Research#3",
		"status_code": "SUCCESS", "response": "done",
	}
	for k, v := range want {
		if ev[k] != v {
			t.Errorf("%s = %v, want %v", k, ev[k], v)
		}
	}
	for k := range ev {
		if !invocationEndKeys[k] {
			t.Errorf("unexpected field %q: only native OpenCode invocation_end fields are allowed", k)
		}
	}
}

func TestEnsureInvocationEnd_OmitsEmptyOptionalFields(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	makeInvocationFolder(t, root, "Research#3")

	w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "Research#3"})

	ev := readEvents(t, invocationLogPath(root, testRunID, "Research#3"))[0]
	assertAbsent(t, ev, "status_code", "response", "session_id")
}

func TestEnsureInvocationEnd_AppendsAfterExistingEvents(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	path := invocationLogPath(root, testRunID, "Research#3")
	writeFile(t, path, `{"event":"invocation_start","agent_instance_id":"Research#3"}`+"\n"+
		`{"event":"turn","text":"invocation_end is mentioned here"}`+"\n")

	res := w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "Research#3"})

	if res != EnsureAppended {
		t.Fatalf("result = %q, want %q: text that merely mentions invocation_end is not an invocation_end event", res, EnsureAppended)
	}
	events := readEvents(t, path)
	if len(events) != 3 || events[0]["event"] != "invocation_start" || events[2]["event"] != "invocation_end" {
		t.Fatalf("events = %v, want start, turn, then the appended end", events)
	}
}

func TestEnsureInvocationEnd_NeverWritesSecondEnd(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	path := invocationLogPath(root, testRunID, "Research#3")
	original := `{"event":"invocation_start","agent_instance_id":"Research#3"}` + "\n" +
		`{"event":"invocation_end","agent_instance_id":"Research#3","status_code":"SUCCESS"}` + "\n"
	writeFile(t, path, original)

	res := w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "Research#3", StatusCode: "BLOCKED"})

	if res != EnsureAlreadyPresent {
		t.Fatalf("result = %q, want %q", res, EnsureAlreadyPresent)
	}
	if got, _ := os.ReadFile(path); string(got) != original {
		t.Fatalf("file changed:\n%s", got)
	}
}

func TestEnsureInvocationEnd_SecondCallAfterAppendIsAlreadyPresent(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	makeInvocationFolder(t, root, "Research#3")
	end := InvocationEnd{AgentInstanceID: "Research#3"}

	first := w.EnsureInvocationEnd(testRunID, end)
	second := w.EnsureInvocationEnd(testRunID, end)

	if first != EnsureAppended || second != EnsureAlreadyPresent {
		t.Fatalf("results = %q, %q; want appended then already-present", first, second)
	}
	if n := len(readEvents(t, invocationLogPath(root, testRunID, "Research#3"))); n != 1 {
		t.Fatalf("%d events, want exactly 1 invocation_end", n)
	}
}

func TestEnsureInvocationEnd_NoFolderWritesNothing(t *testing.T) {
	w, root := newTestWriter(t, "opencode")

	res := w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "Research#3"})

	if res != EnsureNoFolder {
		t.Fatalf("result = %q, want %q", res, EnsureNoFolder)
	}
	if tree := listTree(root); len(tree) != 0 {
		t.Fatalf("wrote %v, want nothing (the folder must not be created)", tree)
	}
}

func TestEnsureInvocationEnd_InvalidInputYieldsNoFolder(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	makeInvocationFolder(t, root, "_")

	for name, c := range map[string]struct {
		runID string
		end   InvocationEnd
	}{
		"empty run id":      {"", InvocationEnd{AgentInstanceID: "A#1"}},
		"traversal run id":  {"../x", InvocationEnd{AgentInstanceID: "A#1"}},
		"unknown run id":    {"unknown-run", InvocationEnd{AgentInstanceID: "A#1"}},
		"empty instance id": {testRunID, InvocationEnd{}},
	} {
		t.Run(name, func(t *testing.T) {
			if res := w.EnsureInvocationEnd(c.runID, c.end); res != EnsureNoFolder {
				t.Fatalf("result = %q, want %q", res, EnsureNoFolder)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "OrchestrationLogs", testRunID, "_", "03_events.jsonl")); err == nil {
		t.Error("empty instance id wrote into the fallback folder")
	}
}

func TestEnsureInvocationEnd_FolderNameRule(t *testing.T) {
	tests := []struct {
		instanceID string
		folder     string
	}{
		{"Research#3", "Research#3"},
		{"a:b/c. ", "a_b_c"},
		{`x<y>z"w|v?u*t\s`, "x_y_z_w_v_u_t_s"},
		{"ctl\tchar", "ctl_char"},
		{"...", "_"},
	}
	for _, tt := range tests {
		t.Run(tt.folder, func(t *testing.T) {
			w, root := newTestWriter(t, "opencode")
			makeInvocationFolder(t, root, tt.folder)

			res := w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: tt.instanceID})

			if res != EnsureAppended {
				t.Fatalf("result = %q, want %q", res, EnsureAppended)
			}
			ev := readEvents(t, invocationLogPath(root, testRunID, tt.folder))[0]
			if ev["agent_instance_id"] != tt.instanceID {
				t.Errorf("agent_instance_id = %q, want it written verbatim as %q", ev["agent_instance_id"], tt.instanceID)
			}
		})
	}
}

func TestEnsureInvocationEnd_UnreadableEventFileIsFailed(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	if err := os.MkdirAll(invocationLogPath(root, testRunID, "Research#3"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "Research#3"})

	if res != EnsureFailed {
		t.Fatalf("result = %q, want %q", res, EnsureFailed)
	}
}

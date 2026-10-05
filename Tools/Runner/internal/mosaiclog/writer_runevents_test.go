package mosaiclog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriter_RunStart_WritesEnvelopeAndCwd(t *testing.T) {
	w, root := newTestWriter(t, "opencode")

	w.RunStart(testRunID, `C:\work\project`)

	events := readEvents(t, orchestratorLogPath(root, testRunID))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	want := map[string]any{
		"schema_version": "1.1.0",
		"event":          "run_start",
		"timestamp":      "2026-10-03T18:50:44.123Z",
		"harness":        "opencode",
		"run_id":         testRunID,
		"cwd":            `C:\work\project`,
	}
	for k, v := range want {
		if ev[k] != v {
			t.Errorf("%s = %v, want %v", k, ev[k], v)
		}
	}
	assertAbsent(t, ev, "session_id", "model", "adapter_version")
}

func TestWriter_RunStart_OmitsEmptyCwd(t *testing.T) {
	w, root := newTestWriter(t, "claude-code")

	w.RunStart(testRunID, "")

	ev := readEvents(t, orchestratorLogPath(root, testRunID))[0]
	assertAbsent(t, ev, "cwd", "session_id", "model", "adapter_version")
}

func TestWriter_RunStart_CarriesSelectedHarness(t *testing.T) {
	for _, h := range []string{"claude-code", "opencode", "ghcp-cli"} {
		t.Run(h, func(t *testing.T) {
			w, root := newTestWriter(t, h)

			w.RunStart(testRunID, "")

			if got := readEvents(t, orchestratorLogPath(root, testRunID))[0]["harness"]; got != h {
				t.Fatalf("harness = %v, want %s", got, h)
			}
		})
	}
}

func TestWriter_RunEnd_WritesOutcome(t *testing.T) {
	for _, o := range []Outcome{OutcomeCompleted, OutcomeStopped, OutcomeFailed, OutcomeAborted, OutcomeInterrupted} {
		t.Run(string(o), func(t *testing.T) {
			w, root := newTestWriter(t, "ghcp-cli")

			w.RunEnd(testRunID, o)

			ev := readEvents(t, orchestratorLogPath(root, testRunID))[0]
			if ev["event"] != "run_end" || ev["outcome"] != string(o) {
				t.Fatalf("got %v, want run_end with outcome %s", ev, o)
			}
			if ev["schema_version"] != "1.1.0" || ev["run_id"] != testRunID || ev["harness"] != "ghcp-cli" ||
				ev["timestamp"] != "2026-10-03T18:50:44.123Z" {
				t.Errorf("envelope incomplete: %v", ev)
			}
			assertAbsent(t, ev, "session_id", "cwd", "model", "adapter_version")
		})
	}
}

func TestWriter_AppendsToExistingFileInOrder(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	path := orchestratorLogPath(root, testRunID)
	writeFile(t, path, `{"event":"session_start","run_id":"`+testRunID+`"}`+"\n")

	w.RunStart(testRunID, "")
	w.RunEnd(testRunID, OutcomeCompleted)

	events := readEvents(t, path)
	got := []any{}
	for _, ev := range events {
		got = append(got, ev["event"])
	}
	want := []any{"session_start", "run_start", "run_end"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestWriter_CreatesMissingDirectories(t *testing.T) {
	w, root := newTestWriter(t, "opencode")
	if len(listTree(root)) != 0 {
		t.Fatal("fixture workspace is not empty")
	}

	w.RunStart(testRunID, "")

	if _, err := os.Stat(orchestratorLogPath(root, testRunID)); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestWriter_InvalidRunIDWritesNothing(t *testing.T) {
	ids := []string{"", "unknown-run", "../x", "20261003T185044Z-07E9", testRunID + "/x", testRunID + " ", `..\x`}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			w, root := newTestWriter(t, "opencode")

			w.RunStart(id, "cwd")
			w.RunEnd(id, OutcomeCompleted)

			if tree := listTree(filepath.Dir(root)); len(tree) != 0 && !(len(tree) == 1 && tree[0] == root) {
				t.Fatalf("invalid run id %q wrote files: %v", id, tree)
			}
		})
	}
}

func TestWriter_WriteFailuresAreSwallowed(t *testing.T) {
	t.Run("OrchestrationLogs is a regular file", func(t *testing.T) {
		w, root := newTestWriter(t, "opencode")
		writeFile(t, filepath.Join(root, "OrchestrationLogs"), "not a directory")

		w.RunStart(testRunID, "cwd")
		w.RunEnd(testRunID, OutcomeFailed)
	})
	t.Run("event file is a directory", func(t *testing.T) {
		w, root := newTestWriter(t, "opencode")
		if err := os.MkdirAll(orchestratorLogPath(root, testRunID), 0o755); err != nil {
			t.Fatal(err)
		}

		w.RunStart(testRunID, "cwd")
		w.RunEnd(testRunID, OutcomeFailed)
	})
	t.Run("workspace root is a regular file", func(t *testing.T) {
		rootFile := filepath.Join(t.TempDir(), "file")
		writeFile(t, rootFile, "x")
		w := NewWriter(rootFile, "opencode")

		w.RunStart(testRunID, "cwd")
		w.RunEnd(testRunID, OutcomeFailed)
		w.EnsureInvocationEnd(testRunID, InvocationEnd{AgentInstanceID: "A#1"})
	})
}

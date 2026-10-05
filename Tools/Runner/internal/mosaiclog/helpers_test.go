package mosaiclog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testRunID = "20261003T185044Z-07e9"

// testClock is a fixed domain.Clock.
type testClock struct{ t time.Time }

func (c testClock) Now() time.Time { return c.t }

// fixedNow is the instant every test writer stamps events with.
var fixedNow = time.Date(2026, 10, 3, 18, 50, 44, 123000000, time.UTC)

// newTestWriter returns a writer rooted at a fresh temp workspace.
func newTestWriter(t *testing.T, harness string) (*Writer, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return NewWriter(root, harness, WithClock(testClock{fixedNow})), root
}

// orchestratorLogPath is the run-level event file for runID under root.
func orchestratorLogPath(root, runID string) string {
	return filepath.Join(root, "OrchestrationLogs", runID, "00_orchestrator_events.jsonl")
}

// invocationLogPath is the invocation event file for folder under root.
func invocationLogPath(root, runID, folder string) string {
	return filepath.Join(root, "OrchestrationLogs", runID, folder, "03_events.jsonl")
}

// readEvents parses a JSON Lines file; every line must be a complete object.
func readEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var events []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line %q is not a JSON object: %v", sc.Text(), err)
		}
		events = append(events, ev)
	}
	return events
}

// writeFile creates path (and parents) with content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertAbsent fails when any of keys is present in ev.
func assertAbsent(t *testing.T, ev map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := ev[k]; ok {
			t.Errorf("event carries %q, want it omitted: %v", k, ev)
		}
	}
}

// listTree returns every path under dir (empty when dir is absent).
func listTree(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && p != dir {
			out = append(out, p)
		}
		return nil
	})
	return out
}

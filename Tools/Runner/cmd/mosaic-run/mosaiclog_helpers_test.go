package main

// mosaiclog_helpers_test.go holds the shared fixtures of the mosaiclog wiring
// tests: a scripted session, a scripted OpenCode harness and event-file readers.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

const mlRunID = "20261003T185044Z-07e9"

// mlFixedNow is the instant the wiring tests stamp events with.
var mlFixedNow = time.Date(2026, 10, 3, 18, 50, 44, 123000000, time.UTC)

// mlRunFolder returns a run folder under a fresh workspace and the workspace.
func mlRunFolder(t *testing.T) (runFolder, workspace string) {
	t.Helper()
	workspace = t.TempDir()
	return filepath.Join(workspace, "Orchestration-"+mlRunID), workspace
}

// mlLogConfig is a lifecycle config for runFolder with a fixed clock.
func mlLogConfig(runFolder, harnessID string) runLogConfig {
	return runLogConfig{
		RunFolder: runFolder,
		HarnessID: harnessID,
		Debug:     domain.NopDebugLogger{},
		Clock:     mainTestClock{t: mlFixedNow},
	}
}

// mlRunLogPath is the run-level event file of mlRunID under workspace.
func mlRunLogPath(workspace string) string {
	return filepath.Join(workspace, "OrchestrationLogs", mlRunID, "00_orchestrator_events.jsonl")
}

// mlReadEvents parses a JSON Lines file; a missing file yields no events.
func mlReadEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
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
		out = append(out, ev)
	}
	return out
}

// mlEventNames lists the event names of events in order.
func mlEventNames(events []map[string]any) []string {
	names := make([]string, 0, len(events))
	for _, ev := range events {
		s, _ := ev["event"].(string)
		names = append(names, s)
	}
	return names
}

// mlAssertPairs fails unless events are exactly n run_start/run_end pairs.
func mlAssertPairs(t *testing.T, events []map[string]any, n int) {
	t.Helper()
	names := mlEventNames(events)
	if len(names) != 2*n {
		t.Fatalf("events = %v, want %d run_start/run_end pair(s)", names, n)
	}
	for i := 0; i < n; i++ {
		if names[2*i] != "run_start" || names[2*i+1] != "run_end" {
			t.Fatalf("events = %v, want %d run_start/run_end pair(s)", names, n)
		}
	}
}

// mlValidOutcome reports whether v is a run_end outcome vocabulary value.
func mlValidOutcome(v any) bool {
	switch v {
	case "completed", "stopped", "failed", "aborted", "interrupted":
		return true
	}
	return false
}

// mlSession is a scripted session.Session.
type mlSession struct {
	start func(ctx context.Context, cfg domain.RunConfig) (domain.RunOutcome, error)
	calls int
}

func (s *mlSession) Start(ctx context.Context, cfg domain.RunConfig) (domain.RunOutcome, error) {
	s.calls++
	if s.start == nil {
		return domain.RunOutcome{Status: domain.RunCompleted}, nil
	}
	return s.start(ctx, cfg)
}

// MockOpenCodeHarness is a scripted openCodeHarness that records every call.
type MockOpenCodeHarness struct {
	RunIDValue string
	ExecPath   string

	// OnInvoke runs inside Invoke, before it returns (simulates the harness
	// process and its hook writing logs while it runs).
	OnInvoke func(request domain.ProtocolRequest)
	Response domain.ProtocolResponse
	Err      error

	RawReply []byte
	RawErr   error

	InvokeRequests []domain.ProtocolRequest
	RawAgents      []domain.AgentReference
	RawPayloads    [][]byte
}

func (m *MockOpenCodeHarness) Invoke(_ context.Context, _ domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	m.InvokeRequests = append(m.InvokeRequests, request)
	if m.OnInvoke != nil {
		m.OnInvoke(request)
	}
	return m.Response, m.Err
}

func (m *MockOpenCodeHarness) InvokeRaw(_ context.Context, agent domain.AgentReference, payload []byte) ([]byte, error) {
	m.RawAgents = append(m.RawAgents, agent)
	m.RawPayloads = append(m.RawPayloads, payload)
	return m.RawReply, m.RawErr
}

func (m *MockOpenCodeHarness) ExecutablePath() string { return m.ExecPath }
func (m *MockOpenCodeHarness) RunID() string          { return m.RunIDValue }

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Five supported workflows — end-to-end (AC11.6) =====

// integrationRun bundles the wired pieces of a scripted end-to-end run.
type integrationRun struct {
	sess    session.Session
	adapter *harness.MockAdapter
	cfg     domain.RunConfig
}

// newTestIntegrationRun writes the orchestrator, Plan.md and agent files for
// the workflow case into a temp dir, queues the scripted SUCCESS responses,
// and returns a session plus the RunConfig that starts it.
func newTestIntegrationRun(t *testing.T, tc integrationWorkflowCase) integrationRun {
	t.Helper()
	dir := t.TempDir()

	orchPath := writeOrchFile(t, dir, "orchestrator.md", tc.orchContent)

	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(tc.plan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	for _, a := range tc.agents {
		writeAgentFile(t, dir, a)
	}

	f := harness.NewMockAdapter()
	for _, r := range tc.responses {
		f.Queue(r.agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: r.agent + "#scripted",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   r.summary,
		}})
	}

	return integrationRun{
		sess:    newSession(f, filepath.Join(dir, "Orchestration.md")),
		adapter: f,
		cfg: domain.RunConfig{
			RunID: integrationRunID,
			OrchestratorFilePath: orchPath,
			WorkflowID:           domain.WorkflowID(tc.workflowID),
			Task:                 tc.name + " task",
			IsNewRun:             true,
			RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
			RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
		},
	}
}

// assertAllResponsesDispatched verifies the harness was invoked once per
// scripted response and that no queued response is left unconsumed.
func assertAllResponsesDispatched(t *testing.T, f *harness.MockAdapter, wantInvocations int) {
	t.Helper()

	// Verify the correct number of harness invocations.
	invs := f.Invocations()
	if len(invs) != wantInvocations {
		t.Errorf("want %d harness invocations, got %d",
			wantInvocations, len(invs))
	}

	// Verify that all queued responses were consumed. An unconsumed
	// entry means the session dispatched fewer agents than the test
	// expected — a silent under-dispatch would otherwise go unnoticed.
	if remaining := f.RemainingQueueSize(); remaining > 0 {
		t.Errorf("want all queued responses consumed after run; %d entries remain unconsumed",
			remaining)
	}
}

// TestIntegration_FiveWorkflows_EndToEnd verifies that each of the five
// supported MOSAIC workflows — brownfield-tdd, brownfield-tdd-build-verified,
// greenfield-tdd, implementation-only, and quick-fix — runs end-to-end against
// the fake adapter and returns RunCompleted after scripted agent responses.
//
// Each workflow case uses a single stage with Implementation-Only approach
// (where the approach is applicable) to minimise the number of scripted
// responses required while still exercising the full session → engine →
// artifact → fake-harness stack.
//
// This covers AC11.6: "all five supported workflows validate and execute
// end-to-end against the fake adapter."
func TestIntegration_FiveWorkflows_EndToEnd(t *testing.T) {
	for _, tc := range newTestFiveWorkflowCases() {
		t.Run(tc.name, func(t *testing.T) {
			run := newTestIntegrationRun(t, tc)

			got, err := run.sess.Start(context.Background(), run.cfg)
			requireRunStatus(t, got, err, domain.RunCompleted)

			assertAllResponsesDispatched(t, run.adapter, len(tc.responses))
		})
	}
}

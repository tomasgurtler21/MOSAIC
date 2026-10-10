package session_test

// Shared helpers for tests that compare what a dispatch receives on the
// engine-routed and the consultation-routed paths over consult-defaults-orch.md.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// Row indices (zero-based) of consult-defaults-orch.md.
const (
	cdTestWriter  = 1 // EXECUTION.Test, staged
	cdBuildReview = 3 // EXECUTION.Implementation, staged
	cdFinalReview = 4 // REVIEW, non-staged, Stage-* input
	cdImplReview  = 5 // REVIEW, non-staged, input pattern needs a stage
)

// writeDefaultsPlan replaces the rig's Plan.md with a two-stage plan whose
// second stage requires human approval.
func writeDefaultsPlan(t *testing.T, dir string) {
	t.Helper()
	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Stage One | First stage | - | FALSE | TDD |
| 2 | Stage Two | Second stage | 1 | TRUE | TDD |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0o600); err != nil {
		t.Fatalf("writeDefaultsPlan: %v", err)
	}
}

// newDefaultsRig builds a rig over consult-defaults-orch.md with the two-stage
// plan. Every approval check reads as approved.
func newDefaultsRig(t *testing.T, consultant domain.RoutingConsultant, mode domain.ExecutionMode, seed domain.ArtifactState) *rowStageRig {
	t.Helper()
	approvals := &switchingApprovalReader{firstApproval: domain.ApprovalTrue, restApproval: domain.ApprovalTrue}
	rig := newRowStageRigWith(t, consultant, mode, seed,
		rowStageOpts{fixture: "consult-defaults-orch.md", approvals: approvals})
	writeDefaultsPlan(t, rig.cfg.RunFolder)
	return rig
}

// stoppingConsultant returns a consultant that stops the run at its first n
// consultations.
func stoppingConsultant(n int) *scriptedRoutingConsultant {
	c := &scriptedRoutingConsultant{}
	for i := 0; i < n; i++ {
		c.queueStop("done")
	}
	return c
}

// firstRequestTo returns the request of the first invocation of agent.
func firstRequestTo(t *testing.T, f *harness.MockAdapter, agent string) domain.ProtocolRequest {
	t.Helper()
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == agent {
			return inv.Request
		}
	}
	t.Fatalf("agent %q was never invoked; invocations: %d", agent, len(f.Invocations()))
	return domain.ProtocolRequest{}
}

// requestsTo returns the requests of every invocation of agent, in order.
func requestsTo(f *harness.MockAdapter, agent string) []domain.ProtocolRequest {
	var out []domain.ProtocolRequest
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == agent {
			out = append(out, inv.Request)
		}
	}
	return out
}

// bare returns the paths without the run folder prefix of the request's run.
func bare(req domain.ProtocolRequest, paths []string) []string {
	prefix := domain.RunScopedFolder(req.RunID) + "/"
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = strings.TrimPrefix(p, prefix)
	}
	return out
}

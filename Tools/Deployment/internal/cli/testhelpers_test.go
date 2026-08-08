package cli_test

// testhelpers_test.go provides shared stubs and builders used across the CLI test suite.
// Nothing in this file is a test itself.

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// spyService — satisfies app.Service and records which methods were called
// ---------------------------------------------------------------------------

type spyService struct {
	harnessRefs []domain.HarnessRef

	deployReq  *app.DeployRequest
	deployResp domain.RunSummary
	deployErr  error

	updateReq  *app.UpdateRequest
	updateResp domain.RunSummary
	updateErr  error

	workflowsReq  *app.WorkflowUpdateRequest
	workflowsResp domain.RunSummary
	workflowsErr  error

	promoteReq  *app.PromoteRequest
	promoteResp app.PromoteResult
	promoteErr  error
}

func (s *spyService) ListHarnesses() []domain.HarnessRef { return s.harnessRefs }

func (s *spyService) DeployNew(ctx context.Context, req app.DeployRequest) (domain.RunSummary, error) {
	s.deployReq = &req
	return s.deployResp, s.deployErr
}

func (s *spyService) Update(ctx context.Context, req app.UpdateRequest) (domain.RunSummary, error) {
	s.updateReq = &req
	return s.updateResp, s.updateErr
}

func (s *spyService) UpdateWorkflows(ctx context.Context, req app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	s.workflowsReq = &req
	return s.workflowsResp, s.workflowsErr
}

func (s *spyService) Promote(ctx context.Context, req app.PromoteRequest) (app.PromoteResult, error) {
	s.promoteReq = &req
	return s.promoteResp, s.promoteErr
}

// ---------------------------------------------------------------------------
// stubTodo — minimal todo.Collector that records gaps
// ---------------------------------------------------------------------------

type stubTodo struct {
	items []domain.TodoItem
	gaps  []domain.Gap
}

func (c *stubTodo) Add(item domain.TodoItem)  { c.items = append(c.items, item) }
func (c *stubTodo) AddGap(g domain.Gap)       { c.gaps = append(c.gaps, g) }
func (c *stubTodo) Items() []domain.TodoItem  { return c.items }
func (c *stubTodo) Groups() []todo.Group      { return nil }
func (c *stubTodo) Empty() bool               { return len(c.items) == 0 && len(c.gaps) == 0 }

// hasGapKind reports whether any recorded gap has the given kind.
func (c *stubTodo) hasGapKind(kind domain.GapKind) bool {
	for _, g := range c.gaps {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// run helpers
// ---------------------------------------------------------------------------

// successSummary returns a RunSummary for the given outcome that the spy will return.
func successSummary(workspace string) domain.RunSummary {
	return domain.RunSummary{
		Mode:           domain.ModeDeployNew,
		WorkspacePath:  workspace,
		DeploymentRoot: workspace,
		Fallback:       domain.FallbackNone,
		Outcome:        domain.OutcomeSuccess,
	}
}

func skippedSummary(workspace string) domain.RunSummary {
	s := successSummary(workspace)
	s.Outcome = domain.OutcomeCompletedWithGaps
	return s
}

func failedSummary(workspace string) domain.RunSummary {
	s := successSummary(workspace)
	s.Outcome = domain.OutcomeFailed
	return s
}

// mustMarshalJSON marshals v to JSON or fails the test.
func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// writeTempYAML writes content as a YAML file in t.TempDir() and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/selections.yaml"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempYAML: %v", err)
	}
	return path
}

package app_test

// todo_consistency_test.go verifies that each of the four active deploy flows — deploy-new,
// update, workflow-update, and deploy-agents — wires ExecRequest.TodoItems as a live provider
// closure over the todo collector, not as a pre-computed snapshot.
//
// Background:
//
// Before this fix, every flow did:
//
//   req.Todo = s.deps.Todo.Items()   // snapshot taken before Execute is called
//
// Items added to the live collector during Execute (via the Content callback, which forwards
// gaps from transform.Apply) were invisible to writeTodoFile because it only read from the
// snapshot. The on-screen summary read from the live collector after Execute returned, so
// the two diverged: the summary showed GapEmptyInjection entries that the file did not.
//
// The fix changes each call site to:
//
//   req.TodoItems = s.deps.Todo.Items   // function reference, called at write time
//
// The tests here verify this wiring for every flow. Each test uses a capturingTodoExecutor
// that:
//   1. Records the ExecRequest it receives.
//   2. Adds a simulated "late" item to the spyTodo after the request arrives (simulating
//      what Content callbacks do during the real run).
//   3. Asserts that TodoItems, when called at that point, reflects the late-added item.
//
// Expected RED-phase behaviour:
//   - With the current code, the flows do not set req.TodoItems; the field is nil.
//   - Each test's first assertion — "TodoItems must not be nil" — immediately fails.
//   - Once I3.3 wires the four call sites correctly, those assertions pass and the second
//     check (live-item visibility) verifies the provider captures late additions.
//
// Note: the GapEmptyInjection regression (AC3.4 — fresh deploy writes injection gaps to the
// file instead of "No manual actions required") is pinned at the executor level by
// TestTodoFile_ItemsAddedDuringExecuteAppearInFile in internal/deploy/todofile_timing_test.go.
// Testing it end-to-end through the app layer would require the real transform pipeline,
// which is out of scope for unit tests.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// capturingTodoExecutor — captures ExecRequest and simulates late-item addition
// ---------------------------------------------------------------------------

// capturingTodoExecutor satisfies deploy.Executor and records the ExecRequest it receives.
// After recording the request it simulates what the Content callback does in the real app:
// it adds one GapEmptyInjection item to the shared spyTodo. This item would be present when
// TodoItems() is called at write time after all Content invocations; the test verifies that
// the captured TodoItems provider returns it.
type capturingTodoExecutor struct {
	capturedReq *deploy.ExecRequest
	spy         *spyTodo
	workspace   string
}

func (e *capturingTodoExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	cp := req
	e.capturedReq = &cp

	// Simulate a GapEmptyInjection added during the Content callback on a fresh deploy.
	// In the real app, transform.Apply adds this for each empty project-class injection
	// region, then the app-layer closure forwards it to the live s.deps.Todo collector.
	// Use Add (not AddGap) so the item appears in spy.Items() when TodoItems() is called.
	e.spy.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  lateItemSubject,
		Detail:   "project-class injection region is empty on first deploy (simulated)",
	})

	return deploy.ExecResult{DeploymentRoot: e.workspace}, nil
}

// lateItemSubject is the Subject of the simulated late-added item used in all T3.2 tests.
const lateItemSubject = "SimulatedProjectInjection"

// assertTodoItemsWiredCorrectly is the shared assertion used by all four flow tests.
// It verifies that:
//
//	a) TodoItems is non-nil (flow wired a provider, not a snapshot).
//	b) TodoItems() returns items that include the late-added item (provider reads live state).
func assertTodoItemsWiredCorrectly(t *testing.T, exec *capturingTodoExecutor) {
	t.Helper()

	if exec.capturedReq == nil {
		t.Fatal("executor was not called; cannot verify TodoItems wiring")
	}

	// Primary assertion: the flow must set TodoItems to a non-nil provider.
	// With the current code, flows set req.Todo (snapshot) and leave TodoItems nil.
	if exec.capturedReq.TodoItems == nil {
		t.Fatal("ExecRequest.TodoItems is nil; each flow must wire it to the live " +
			"collector's Items accessor (e.g. s.deps.Todo.Items) so the executor can " +
			"call it at write time. Setting req.Todo to a snapshot is insufficient " +
			"because items added during Execute are not captured by the snapshot.")
	}

	// Secondary assertion: the provider must return the live collector's state, including
	// the item added by the capturingTodoExecutor after the request was constructed.
	items := exec.capturedReq.TodoItems()
	found := false
	for _, item := range items {
		if item.Subject == lateItemSubject {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TodoItems() did not return the item %q that was added to the collector "+
			"after ExecRequest construction; the provider must be a live closure (e.g. "+
			"todo.Collector.Items) so it captures items added during Execute, not a "+
			"snapshot taken before Execute was called;\nTodoItems() returned: %v",
			lateItemSubject, items)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — deploy-new flow
// ---------------------------------------------------------------------------

// TestDeployNew_TodoItemsProviderIsWiredFromLiveCollector verifies that the deploy-new flow
// wires ExecRequest.TodoItems to a provider backed by the live todo collector, so items
// added during Execute are visible at write time.
func TestDeployNew_TodoItemsProviderIsWiredFromLiveCollector(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &spyTodo{}
	deps.Todo = spy

	capExec := &capturingTodoExecutor{spy: spy, workspace: workspace}
	deps.Executor = capExec

	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	assertTodoItemsWiredCorrectly(t, capExec)
}

// ---------------------------------------------------------------------------
// T3.2 — update flow
// ---------------------------------------------------------------------------

// TestUpdate_TodoItemsProviderIsWiredFromLiveCollector verifies that the update flow wires
// ExecRequest.TodoItems to a provider backed by the live todo collector.
func TestUpdate_TodoItemsProviderIsWiredFromLiveCollector(t *testing.T) {
	// Arrange — supply a manifest with an existing entry so the update flow has a deployed
	// artifact to compare against when checking for staleness.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				HarnessID: "stub-harness",
				Entries: []domain.ManifestEntry{
					{
						Ref:     domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
						Version: "1.0",
					},
				},
			},
		},
	}
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionUpdate,
				Stale: []domain.VersionDelta{
					{Field: "version", Deployed: "1.0", Source: "2.0"},
				},
			},
		},
		Workflows: []string{"quick-fix"},
	}}

	spy := &spyTodo{}
	deps.Todo = spy

	capExec := &capturingTodoExecutor{spy: spy, workspace: workspace}
	deps.Executor = capExec

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert
	assertTodoItemsWiredCorrectly(t, capExec)
}

// ---------------------------------------------------------------------------
// T3.2 — workflow-update flow
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_TodoItemsProviderIsWiredFromLiveCollector verifies that the
// workflow-update flow wires ExecRequest.TodoItems to a provider backed by the live todo
// collector.
func TestUpdateWorkflows_TodoItemsProviderIsWiredFromLiveCollector(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	// Provide an orchestrator-only plan: the workflow-update flow filters stale non-orchestrator
	// agents, so a plan with only the orchestrator (ActionUpdate) is the minimal realistic shape.
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkflows,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: "orchestrator.md",
				Action:     domain.ActionUpdate,
			},
		},
		Workflows: []string{"quick-fix"},
	}}

	spy := &spyTodo{}
	deps.Todo = spy

	capExec := &capturingTodoExecutor{spy: spy, workspace: workspace}
	deps.Executor = capExec

	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert
	assertTodoItemsWiredCorrectly(t, capExec)
}

// ---------------------------------------------------------------------------
// T3.2 — deploy-agents flow
// ---------------------------------------------------------------------------

// TestDeployAgents_TodoItemsProviderIsWiredFromLiveCollector verifies that the deploy-agents
// flow wires ExecRequest.TodoItems to a provider backed by the live todo collector, so items
// added during Execute (via Content callbacks) are visible when TodoItems() is called at write
// time. This retargets the same invariant the retired utility-infra flow carried.
func TestDeployAgents_TodoItemsProviderIsWiredFromLiveCollector(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	// Supply a planner returning a deploy-agents plan with no items (minimal valid shape).
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeDeployAgents,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
	}}

	spy := &spyTodo{}
	deps.Todo = spy

	capExec := &capturingTodoExecutor{spy: spy, workspace: workspace}
	deps.Executor = capExec

	svc := app.New(deps)

	// Act — pre-answer all four per-class ID slices to empty so no interactive questions are
	// asked for the agent selection steps (CD-6: any non-nil slice bypasses QDeployAgents).
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	})
	if err != nil {
		t.Fatalf("DeployAgents: %v", err)
	}

	// Assert
	assertTodoItemsWiredCorrectly(t, capExec)
}

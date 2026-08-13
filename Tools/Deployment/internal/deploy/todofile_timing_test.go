package deploy_test

// todofile_timing_test.go pins the regression that the written MOSAIC-DEPLOYMENT-TODO.md
// must include items forwarded to the backing collector *during* Execute — not just items
// present at request-construction time.
//
// The root cause of the divergence between the on-screen summary and the written file is a
// snapshot/live mismatch: the four app-layer flows took s.deps.Todo.Items() once before
// calling Execute and stored the result in ExecRequest.Todo. Because items added inside the
// Content callback (e.g. GapEmptyInjection on a fresh deploy) were appended to the live
// collector after that snapshot, writeTodoFile never saw them.
//
// The fix replaces ExecRequest.Todo with ExecRequest.TodoItems, a provider closure. The
// executor calls it exactly once, inside writeTodoFile, after all per-item Content callbacks
// have run. Callers pass their collector's Items method directly so the provider always
// returns the fully-populated collector state at write time.
//
// Tests here verify:
//   - Items supplied via TodoItems appear in the written file.
//   - Items added to the backing collector *after* ExecRequest construction but *before* the
//     write step (i.e. during Execute) also appear — the timing regression pin.
//   - A nil TodoItems is handled gracefully: the executor writes a file without panicking.

import (
	"context"
	"os"
	"strings"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// TestTodoFile_ItemsFromTodoItemsProviderAppearInFile verifies that items returned by the
// TodoItems provider closure appear in the written MOSAIC-DEPLOYMENT-TODO.md. The executor
// must call TodoItems() at write time and render every returned item.
func TestTodoFile_ItemsFromTodoItemsProviderAppearInFile(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	collector := newSpyCollector()
	collector.Add(domain.TodoItem{
		Category: domain.TodoModels,
		Subject:  "provider-item",
		Detail:   "item supplied via TodoItems provider closure",
	})

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		TodoItems:  collector.Items, // function reference — executor must call it at write time
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TodoFilePath == "" {
		t.Fatal("ExecResult.TodoFilePath is empty; cannot verify file content")
	}

	fileContent, err := os.ReadFile(result.TodoFilePath)
	if err != nil {
		t.Fatalf("read TODO file at %q: %v", result.TodoFilePath, err)
	}
	if !strings.Contains(string(fileContent), "provider-item") {
		t.Errorf("TODO file does not contain item %q from the TodoItems provider; "+
			"executor must call TodoItems() at write time and render the returned items;\nfile:\n%s",
			"provider-item", fileContent)
	}
}

// TestTodoFile_ItemsAddedDuringExecuteAppearInFile pins the timing regression. It verifies
// that items added to the backing collector *during* Execute — specifically inside the
// Content callback, which is how transform.Apply forwards GapEmptyInjection entries on a
// fresh deploy — appear in the written file.
//
// The key setup: the collector is empty at ExecRequest construction time, but the Content
// callback adds a GapEmptyInjection item when it is invoked. The provider (TodoItems) is a
// reference to the collector's Items method, so TodoItems() called at write time returns the
// late-added item. The test asserts that item appears in the file.
//
// With the old ExecRequest.Todo snapshot approach, writeTodoFile received the empty snapshot
// and the late-added item was invisible. This test ensures that bug cannot recur.
func TestTodoFile_ItemsAddedDuringExecuteAppearInFile(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// The collector starts empty at request-construction time — matching the state before
	// any Content callback has run on a fresh deploy.
	collector := newSpyCollector()

	var contentCallCount int
	contentFn := func(item domain.PlanItem) ([]byte, error) {
		// Simulate what app.buildContent does via transform.Apply: forward a GapEmptyInjection
		// item into the live collector. On a fresh deploy, this happens once per empty
		// project-class injection region per agent, producing the bulk of TODO output.
		// Use Add (not AddGap) so the item appears in collector.Items() at write time;
		// spyCollector.AddGap only appends to gaps, not items, diverging from real collector semantics.
		contentCallCount++
		collector.Add(domain.TodoItem{
			Category: domain.TodoInjections,
			Subject:  "IdentityExtension",
			Detail:   "project-class injection region is empty on first deploy",
		})
		return []byte("# Agent content"), nil
	}

	planItem := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, planItem),
		MosaicRoot: mosaicRoot,
		Content:    contentFn,
		TodoItems:  collector.Items, // closed over collector — returns live state at call time
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if contentCallCount == 0 {
		t.Fatal("Content function was never called; cannot verify late-added item behaviour")
	}
	if result.TodoFilePath == "" {
		t.Fatal("ExecResult.TodoFilePath is empty; cannot verify file content")
	}

	fileContent, err := os.ReadFile(result.TodoFilePath)
	if err != nil {
		t.Fatalf("read TODO file at %q: %v", result.TodoFilePath, err)
	}
	// IdentityExtension was added to the collector DURING Execute (inside contentFn), after
	// the ExecRequest was constructed. The executor must call TodoItems() at write time —
	// after all Content calls — so this late-added item is included in the file.
	if !strings.Contains(string(fileContent), "IdentityExtension") {
		t.Errorf("TODO file does not contain late-added item %q; "+
			"executor must call TodoItems() at write time (after Content callbacks), "+
			"not snapshot the collector at request-construction time;\nfile:\n%s",
			"IdentityExtension", fileContent)
	}
}

// TestTodoFile_NilTodoItemsProducesNoPreCollectedItems verifies that a nil TodoItems provider
// is handled gracefully. The executor must not panic: only its own executor-generated gaps
// appear in the file, and the run completes without error.
func TestTodoFile_NilTodoItemsProducesNoPreCollectedItems(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		TodoItems:  nil, // explicitly nil — must not panic; only exec gaps rendered
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute with nil TodoItems must not return an error: %v", err)
	}
	if result.TodoFilePath == "" {
		t.Error("ExecResult.TodoFilePath must be set even when TodoItems is nil")
	}
}

// TestTodoFile_TodoItemsCalledAfterContentCallbacks verifies that the executor invokes
// TodoItems() after all per-item Content calls have completed, not before or during the
// item loop. The test proves this by counting how many items the provider returns at the
// time writeTodoFile calls it: the count must reflect additions made during Content.
func TestTodoFile_TodoItemsCalledAfterContentCallbacks(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	collector := newSpyCollector()
	var providerWasCalled bool
	var providerCallItems []domain.TodoItem

	// Wrap the collector's Items method so we can observe exactly what items are present
	// at the moment the executor calls TodoItems().
	wrappedProvider := func() []domain.TodoItem {
		providerWasCalled = true
		items := collector.Items()
		providerCallItems = items // capture what the executor sees at write time
		return items
	}

	contentFn := func(item domain.PlanItem) ([]byte, error) {
		// Add an item during content generation, simulating transform.Apply gap forwarding.
		// Use Add (not AddGap) so the item appears in collector.Items() at write time.
		collector.Add(domain.TodoItem{
			Category: domain.TodoInjections,
			Subject:  "HarnessConstraints",
			Detail:   "added during content callback",
		})
		return []byte("content"), nil
	}

	planItem := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, planItem),
		MosaicRoot: mosaicRoot,
		Content:    contentFn,
		TodoItems:  wrappedProvider,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The provider must have been called (executor must invoke it to write the file).
	if !providerWasCalled {
		t.Fatal("TodoItems provider was never called; executor must invoke it during writeTodoFile")
	}

	// At the moment of the call, the collector must already contain the item added in contentFn.
	found := false
	for _, item := range providerCallItems {
		if item.Subject == "HarnessConstraints" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TodoItems() was called but did not yet contain the item added during contentFn; "+
			"executor must call TodoItems() after all Content callbacks complete, not before or during;\n"+
			"items seen at call time: %v", providerCallItems)
	}
}

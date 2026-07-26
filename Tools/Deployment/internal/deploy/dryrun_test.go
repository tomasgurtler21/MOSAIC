package deploy_test

// dryrun_test.go covers DryRun behavior (ExecRequest.DryRun).
//
// When DryRun is true, the executor performs no filesystem writes: no files are created,
// updated, backed up, or deleted in the workspace or any fallback location, and neither
// the manifest nor the TODO file is written to disk.
//
// The result still carries a fully-populated Actions slice reporting what would have
// happened, so the caller can present a preview to the user.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// TestDryRun_NoFilesWrittenToWorkspace verifies that when DryRun is true, the executor
// does not create or modify any files inside the workspace directory, including deployment
// paths and the .mosaic metadata directory.
func TestDryRun_NoFilesWrittenToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# runner content")),
		DryRun:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The workspace must remain empty after a dry run — no files of any kind written.
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("ReadDir workspace: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("DryRun=true: files written to workspace (must write nothing): %v", names)
	}
}

// TestDryRun_NoManifestFileWritten verifies that the .mosaic directory and manifest file
// are not created when DryRun is true, since writing the manifest is itself a filesystem
// write.
func TestDryRun_NoManifestFileWritten(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		DryRun:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mosaicDir := filepath.Join(workspace, ".mosaic")
	if _, err := os.Stat(mosaicDir); err == nil {
		t.Errorf("DryRun=true: .mosaic directory was created at %q; no writes must occur", mosaicDir)
	}
}

// TestDryRun_ActionsIsPopulated verifies that result.Actions is non-empty when DryRun is
// true, so the caller can display what would have happened without committing any changes.
func TestDryRun_ActionsIsPopulated(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# runner content")),
		DryRun:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Actions) == 0 {
		t.Fatal("DryRun=true: result.Actions is empty; it must still report what would have happened")
	}
	found := false
	for _, a := range result.Actions {
		if a.Ref == item.Ref {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DryRun=true: result.Actions has no record for item %q; actions returned: %v",
			item.Ref.Key, result.Actions)
	}
}

// TestDryRun_MultipleItems_AllActionsReported verifies that when the plan contains multiple
// items, all of them appear in result.Actions for a dry run, not just the first one.
func TestDryRun_MultipleItems_AllActionsReported(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionUpdate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		DryRun:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Actions) != 2 {
		t.Errorf("DryRun=true with 2-item plan: got %d actions; want 2", len(result.Actions))
	}
}

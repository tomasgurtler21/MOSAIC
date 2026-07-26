package deploy_test

// boundary_test.go covers AC17.7: no file outside the resolved deployment location and
// the manifest/backup folders is written.
//
// After a run, the workspace must contain only files and directories under:
//   - the deployment directories named by PlanItem.TargetPath (relative to workspace root)
//   - <workspace>/.mosaic/  (manifest and backups)
//   - <workspace>/MOSAIC-DEPLOYMENT-TODO.md
//
// No stray files — debug output, uncleaned temp files, etc. — may appear at the workspace
// root or in any unrelated directory. An implementation that leaks unexpected files would
// not be caught by the placement tests in other files; this test closes that gap.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/todo"
)

// TestBoundary_NoStrayFilesAtWorkspaceRoot verifies that after a normal run with one agent
// item, only the expected top-level names exist at the workspace root: the deployment
// directory derived from the item's target path, the .mosaic metadata directory, and the
// TODO file (AC17.7).
func TestBoundary_NoStrayFilesAtWorkspaceRoot(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// The plan item's target path is "agents/runner.md"; the executor must write the file
	// under <workspace>/agents/ (relative to the deployment root, which for the workspace
	// tier equals the workspace itself).
	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# runner content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Allowed top-level workspace entries:
	//   "agents"           — deployment directory for this plan item's target path
	//   ".mosaic"          — manifest.Dir: holds manifest.yaml and backups/
	//   todo.FileName      — MOSAIC-DEPLOYMENT-TODO.md written by the executor
	allowed := map[string]bool{
		"agents":        true,
		".mosaic":       true,
		todo.FileName:   true,
	}

	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("ReadDir workspace: %v", err)
	}

	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("unexpected entry %q at workspace root — only %s are permitted (AC17.7)",
				e.Name(), formatAllowed(allowed))
		}
	}
}

// TestBoundary_NoStrayFilesAfterConflictSkip verifies that a skip decision leaves the
// workspace containing only the allowed paths even when the file was not overwritten.
// A skip must not write any additional metadata or temp file outside the allowed locations.
func TestBoundary_NoStrayFilesAfterConflictSkip(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("local content"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new content")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// After a skip, the workspace root must still contain only allowed entries.
	// The pre-existing "agents" directory is permitted because the plan targeted it.
	allowed := map[string]bool{
		"agents":      true,
		".mosaic":     true,
		todo.FileName: true,
	}

	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("ReadDir workspace: %v", err)
	}

	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("unexpected entry %q at workspace root after conflict-skip — only %s are permitted (AC17.7)",
				e.Name(), formatAllowed(allowed))
		}
	}
}

// formatAllowed formats the keys of an allowed map as a human-readable list for error messages.
func formatAllowed(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, fmt.Sprintf("%q", k))
	}
	return fmt.Sprintf("[%s]", joinStrings(keys))
}

// joinStrings joins a slice of strings with ", ".
func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

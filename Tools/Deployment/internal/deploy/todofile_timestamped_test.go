package deploy_test

// todofile_timestamped_test.go covers run isolation and the accurate TODO path in the run
// summary (T8.1 integration, T8.3).
//
// Run isolation (T8.1): two Execute calls with different TodoMeta.GeneratedAt values must
// write two distinct files on disk. After the second run the first file must still be present
// — the second run must never overwrite the first.
//
// Accurate path in result (T8.3): ExecResult.TodoFilePath must name the file actually written,
// constructed as todo.FileNameAt(req.TodoMeta.GeneratedAt). The result must not report a
// fixed constant name; it must match the timestamped file that exists on disk.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// T8.1: Run isolation — each run writes its own timestamped file
// ---------------------------------------------------------------------------

// TestTodoFile_TwoRunsProduceTwoDistinctFiles verifies that two Execute calls with different
// GeneratedAt timestamps each write a separate TODO file. After both runs, two distinct files
// must exist in the workspace — neither run may overwrite the other's file.
func TestTodoFile_TwoRunsProduceTwoDistinctFiles(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)

	runA := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	runB := time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC)

	// First run.
	reqA := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content-a")),
		TodoMeta: todo.Meta{
			Harness:       "test-harness",
			WorkspacePath: workspace,
			Mode:          domain.ModeDeployNew,
			GeneratedAt:   runA,
		},
	}
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	resultA, err := ex.Execute(context.Background(), reqA)
	if err != nil {
		t.Fatalf("Execute (run A): %v", err)
	}
	if resultA.TodoFilePath == "" {
		t.Fatal("run A: ExecResult.TodoFilePath is empty")
	}

	// Second run — different GeneratedAt, same workspace.
	reqB := deploy.ExecRequest{
		Plan:       newPlan(workspace, newAgentItem("runner", "agents/runner.md", domain.ActionUpdate)),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content-b")),
		TodoMeta: todo.Meta{
			Harness:       "test-harness",
			WorkspacePath: workspace,
			Mode:          domain.ModeDeployNew,
			GeneratedAt:   runB,
		},
	}
	resultB, err := ex.Execute(context.Background(), reqB)
	if err != nil {
		t.Fatalf("Execute (run B): %v", err)
	}
	if resultB.TodoFilePath == "" {
		t.Fatal("run B: ExecResult.TodoFilePath is empty")
	}

	// The two runs must have produced different file paths.
	if resultA.TodoFilePath == resultB.TodoFilePath {
		t.Errorf("both runs produced the same TodoFilePath %q; each run must write a distinct file",
			resultA.TodoFilePath)
	}

	// Both files must exist on disk after the second run.
	if !fileExistsAt(t, resultA.TodoFilePath) {
		t.Errorf("run A's TODO file %q was removed after run B; each run must preserve earlier runs' files",
			resultA.TodoFilePath)
	}
	if !fileExistsAt(t, resultB.TodoFilePath) {
		t.Errorf("run B's TODO file %q does not exist on disk", resultB.TodoFilePath)
	}
}

// TestTodoFile_TimestampedFileNameAppearsInWorkspace verifies that the file written to disk
// is named with the run's timestamp suffix (matching FileNamePrefix + timestamp + FileNameExt)
// rather than a fixed constant. The file must be findable by the pattern, not by a fixed string.
func TestTodoFile_TimestampedFileNameAppearsInWorkspace(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	runTime := time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC)
	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		TodoMeta: todo.Meta{
			Harness:       "test-harness",
			WorkspacePath: workspace,
			Mode:          domain.ModeDeployNew,
			GeneratedAt:   runTime,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TodoFilePath == "" {
		t.Fatal("ExecResult.TodoFilePath is empty")
	}

	// The file name (base name of the path) must look like a timestamped TODO file.
	baseName := filepath.Base(result.TodoFilePath)
	if !isTodoFileName(baseName) {
		t.Errorf("TODO file base name %q does not match the pattern %s*%s; "+
			"executor must derive the filename from the run's GeneratedAt timestamp",
			baseName, todo.FileNamePrefix, todo.FileNameExt)
	}

	// Specifically, the base name must be the output of FileNameAt for the same GeneratedAt.
	wantName := todo.FileNameAt(runTime)
	if baseName != wantName {
		t.Errorf("TODO file base name = %q; want %q (FileNameAt(%v))", baseName, wantName, runTime)
	}
}

// ---------------------------------------------------------------------------
// T8.3: Run summary reports the actual written TODO path
// ---------------------------------------------------------------------------

// TestTodoFile_ResultPathMatchesFileNameAt verifies that ExecResult.TodoFilePath is the
// absolute path of the timestamped file actually written, equivalent to joining the workspace
// path with todo.FileNameAt(req.TodoMeta.GeneratedAt). The result must not report a
// fixed legacy constant.
func TestTodoFile_ResultPathMatchesFileNameAt(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	runTime := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		TodoMeta: todo.Meta{
			Harness:       "test-harness",
			WorkspacePath: workspace,
			Mode:          domain.ModeDeployNew,
			GeneratedAt:   runTime,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TodoFilePath == "" {
		t.Fatal("ExecResult.TodoFilePath is empty")
	}

	// The result path must end with the timestamped file name.
	wantName := todo.FileNameAt(runTime)
	if !strings.HasSuffix(result.TodoFilePath, wantName) {
		t.Errorf("TodoFilePath = %q; want it to end with %q (FileNameAt(%v))",
			result.TodoFilePath, wantName, runTime)
	}

	// The file must actually exist at the reported path.
	if !fileExistsAt(t, result.TodoFilePath) {
		t.Errorf("TODO file does not exist at reported path %q", result.TodoFilePath)
	}
}

// TestTodoFile_ResultPathEndsWithTimestampedName verifies that the executor uses the run's
// GeneratedAt from TodoMeta (not a separate clock call) to derive the filename, so the
// reported path and the file header's generated-at timestamp describe the same run instant.
// This is asserted by reading the written file's content and confirming it contains the
// same date and time as the filename's timestamp.
func TestTodoFile_ResultPathEndsWithTimestampedName(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	runTime := time.Date(2026, 8, 14, 14, 24, 5, 0, time.UTC)
	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
		TodoMeta: todo.Meta{
			Harness:       "test-harness",
			WorkspacePath: workspace,
			Mode:          domain.ModeDeployNew,
			GeneratedAt:   runTime,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TodoFilePath == "" {
		t.Fatal("ExecResult.TodoFilePath is empty")
	}

	// The filename must carry the run's timestamp.
	baseName := filepath.Base(result.TodoFilePath)
	wantName := todo.FileNameAt(runTime)
	if baseName != wantName {
		t.Errorf("TODO base name = %q; want %q (derived from TodoMeta.GeneratedAt)", baseName, wantName)
	}

	// The file's content must include the same date/time components embedded in the header.
	content, err := os.ReadFile(result.TodoFilePath)
	if err != nil {
		t.Fatalf("read TODO file: %v", err)
	}
	wantDateInHeader := "2026-08-14"
	if !strings.Contains(string(content), wantDateInHeader) {
		t.Errorf("TODO file header does not contain date %q from GeneratedAt; "+
			"executor must use TodoMeta.GeneratedAt for both the filename and the file header;\nfile:\n%s",
			wantDateInHeader, content)
	}
}

package deploy_test

// conflict_test.go covers T17.3 and T17.4.
//
// T17.3: For every plan item classified ActionConflict, the user supplies a ConflictDecision.
// The executor honours that decision precisely:
//   - DecisionSkip:                the file on disk is left byte-identical; the action record
//                                  shows TakenSkipped.
//   - DecisionOverwrite:           the file is overwritten with the new content; the action
//                                  record shows TakenUpdated.
//   - DecisionBackupThenOverwrite: the existing file is copied to a backup path before the
//                                  new content is written; both the backup and the new file
//                                  exist; the action record shows TakenBackedUp and carries
//                                  BackupPath (AC17.3).
//
// T17.4: Backup naming follows the format
//   <workspace>/.mosaic/backups/<relative-target-path>.<RFC3339-compact>.bak
// Multiple backups of the same file never collide (AC17.3).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Shared helpers for conflict tests
// ---------------------------------------------------------------------------

// writeExisting writes originalContent to <workspace>/<targetPath>, creating directories
// as needed, to simulate a locally-modified file already present on disk.
func writeExisting(t *testing.T, workspace, targetPath string, originalContent []byte) {
	t.Helper()
	fullPath := filepath.Join(workspace, targetPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("writeExisting: mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, originalContent, 0o644); err != nil {
		t.Fatalf("writeExisting: write: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T17.3 — DecisionSkip: file remains byte-identical
// ---------------------------------------------------------------------------

// TestConflict_Skip_FileIsUnchanged verifies that when the user chooses DecisionSkip for
// a locally-modified file, the file on disk retains its exact original content after the run.
func TestConflict_Skip_FileIsUnchanged(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"
	originalContent := []byte("# original locally-modified content")
	newContent := []byte("# new source content")

	writeExisting(t, workspace, targetPath, originalContent)

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(newContent),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	actualContent := readFile(t, filepath.Join(workspace, targetPath))
	if !bytes.Equal(actualContent, originalContent) {
		t.Errorf("skipped file content changed:\ngot:  %q\nwant: %q", actualContent, originalContent)
	}
}

// TestConflict_Skip_ActionRecordIsTakenSkipped verifies that a skipped conflict produces an
// ActionRecord with TakenSkipped as the outcome.
func TestConflict_Skip_ActionRecordIsTakenSkipped(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Actions) == 0 {
		t.Fatal("Actions is empty; expected one action record for the skipped item")
	}
	got := result.Actions[0].Taken
	if got != domain.TakenSkipped {
		t.Errorf("ActionRecord.Taken = %q; want %q", got, domain.TakenSkipped)
	}
}

// ---------------------------------------------------------------------------
// T17.3 — DecisionOverwrite: file is replaced with new content
// ---------------------------------------------------------------------------

// TestConflict_Overwrite_FileContainsNewContent verifies that when the user chooses
// DecisionOverwrite, the file on disk is replaced with the content from the plan.
func TestConflict_Overwrite_FileContainsNewContent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"
	originalContent := []byte("# original locally-modified content")
	newContent := []byte("# new source content")

	writeExisting(t, workspace, targetPath, originalContent)

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(newContent),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionOverwrite},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	actualContent := readFile(t, filepath.Join(workspace, targetPath))
	if !bytes.Equal(actualContent, newContent) {
		t.Errorf("overwritten file content:\ngot:  %q\nwant: %q", actualContent, newContent)
	}
}

// TestConflict_Overwrite_ActionRecordIsTakenUpdated verifies that an overwritten conflict
// produces an ActionRecord with TakenUpdated as the outcome.
func TestConflict_Overwrite_ActionRecordIsTakenUpdated(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionOverwrite},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Actions) == 0 {
		t.Fatal("Actions is empty; expected one action record")
	}
	if got := result.Actions[0].Taken; got != domain.TakenUpdated {
		t.Errorf("ActionRecord.Taken = %q; want %q", got, domain.TakenUpdated)
	}
}

// ---------------------------------------------------------------------------
// T17.3 — DecisionBackupThenOverwrite: backup created, original overwritten
// ---------------------------------------------------------------------------

// TestConflict_BackupThenOverwrite_BackupFileExists verifies that when the user chooses
// DecisionBackupThenOverwrite, the executor creates a backup file containing the original
// content before overwriting (AC17.3).
func TestConflict_BackupThenOverwrite_BackupFileExists(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"
	originalContent := []byte("# original locally-modified content")

	writeExisting(t, workspace, targetPath, originalContent)

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# new content")),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The action record must carry the backup path.
	if len(result.Actions) == 0 {
		t.Fatal("Actions is empty")
	}
	backupPath := result.Actions[0].BackupPath
	if backupPath == "" {
		t.Fatal("ActionRecord.BackupPath is empty; expected a backup to have been created")
	}
	if !fileExistsAt(t, backupPath) {
		t.Errorf("backup file not found at %q", backupPath)
	}
}

// TestConflict_BackupThenOverwrite_BackupContainsOriginalContent verifies that the backup
// file contains the exact bytes of the file as it was on disk before the overwrite.
func TestConflict_BackupThenOverwrite_BackupContainsOriginalContent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"
	originalContent := []byte("# original locally-modified content")

	writeExisting(t, workspace, targetPath, originalContent)

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# new content")),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	backupPath := result.Actions[0].BackupPath
	backupContent := readFile(t, backupPath)
	if !bytes.Equal(backupContent, originalContent) {
		t.Errorf("backup content:\ngot:  %q\nwant: %q", backupContent, originalContent)
	}
}

// TestConflict_BackupThenOverwrite_OriginalFileIsReplaced verifies that the deployment target
// file contains the new content after a backup-then-overwrite, not the original content.
func TestConflict_BackupThenOverwrite_OriginalFileIsReplaced(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"
	newContent := []byte("# new content")

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(newContent),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	actual := readFile(t, filepath.Join(workspace, targetPath))
	if !bytes.Equal(actual, newContent) {
		t.Errorf("deployed file content after backup-then-overwrite:\ngot:  %q\nwant: %q", actual, newContent)
	}
}

// TestConflict_BackupThenOverwrite_ActionRecordIsTakenBackedUp verifies that the action
// record carries TakenBackedUp as the outcome (AC17.4 decision-to-outcome contract).
func TestConflict_BackupThenOverwrite_ActionRecordIsTakenBackedUp(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Actions) == 0 {
		t.Fatal("Actions is empty")
	}
	if got := result.Actions[0].Taken; got != domain.TakenBackedUp {
		t.Errorf("ActionRecord.Taken = %q; want %q", got, domain.TakenBackedUp)
	}
}

// ---------------------------------------------------------------------------
// T17.3 — Missing decision: ErrUndecidedConflict
// ---------------------------------------------------------------------------

// TestConflict_MissingDecision_ReturnsErrUndecidedConflict verifies that Execute returns
// ErrUndecidedConflict when the Conflicts map has no entry for an ActionConflict plan item.
// This is a programming error in the caller and must be surfaced loudly.
func TestConflict_MissingDecision_ReturnsErrUndecidedConflict(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts:  map[string]domain.ConflictDecision{}, // empty — no decision for runner
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("Execute: expected error, got nil")
	}
	if !isErrUndecidedConflict(err) {
		t.Errorf("Execute error = %v; want ErrUndecidedConflict (or wrapping it)", err)
	}
}

// TestConflict_NilConflictsMap_ReturnsErrUndecidedConflict verifies that a nil Conflicts map
// is treated the same as an empty map: any ActionConflict item returns ErrUndecidedConflict.
func TestConflict_NilConflictsMap_ReturnsErrUndecidedConflict(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts:  nil,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("Execute: expected error, got nil")
	}
	if !isErrUndecidedConflict(err) {
		t.Errorf("Execute error = %v; want ErrUndecidedConflict (or wrapping it)", err)
	}
}

// isErrUndecidedConflict reports whether err is or wraps deploy.ErrUndecidedConflict.
func isErrUndecidedConflict(err error) bool {
	return errors.Is(err, deploy.ErrUndecidedConflict)
}

// ---------------------------------------------------------------------------
// T17.4 — Backup naming: RFC3339-compact, no collisions
// ---------------------------------------------------------------------------

// TestBackup_Naming_IsUnderMosaicBackupsDir verifies that backup files are created under
// <workspace>/.mosaic/backups/ as specified.
func TestBackup_Naming_IsUnderMosaicBackupsDir(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	backupPath := result.Actions[0].BackupPath
	expectedDir := filepath.Join(workspace, ".mosaic", "backups")
	if !strings.HasPrefix(backupPath, expectedDir) {
		t.Errorf("backup path %q does not start with %q", backupPath, expectedDir)
	}
}

// TestBackup_Naming_HasBakExtension verifies that backup files use the .bak extension.
func TestBackup_Naming_HasBakExtension(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	backupPath := result.Actions[0].BackupPath
	if !strings.HasSuffix(backupPath, ".bak") {
		t.Errorf("backup path %q does not end with .bak", backupPath)
	}
}

// TestBackup_Naming_ContainsRelativeTargetPath verifies that the backup filename encodes
// the relative target path so the user can identify what was backed up.
func TestBackup_Naming_ContainsRelativeTargetPath(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new")),
		Conflicts: map[string]domain.ConflictDecision{
			targetPath: domain.DecisionBackupThenOverwrite,
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	backupPath := result.Actions[0].BackupPath
	// The backup path must include some representation of the target path components.
	// The exact form is <backupDir>/<relative-target-path>.<timestamp>.bak
	if !strings.Contains(filepath.Base(filepath.Dir(backupPath)), "agents") &&
		!strings.Contains(backupPath, "runner") {
		t.Errorf("backup path %q does not encode the target path %q", backupPath, targetPath)
	}
}

// TestBackup_RepeatedBackups_DoNotCollide verifies that two sequential backup operations on
// the same file produce distinct backup paths (AC17.3: repeated backups of the same file
// never collide).
func TestBackup_RepeatedBackups_DoNotCollide(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("version-one"))

	item := newConflictItem("runner", targetPath)
	conflict := map[string]domain.ConflictDecision{
		targetPath: domain.DecisionBackupThenOverwrite,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())

	// First backup run.
	req1 := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("version-two")),
		Conflicts:  conflict,
	}
	result1, err := ex.Execute(context.Background(), req1)
	if err != nil {
		t.Fatalf("Execute (first run): %v", err)
	}
	backup1 := result1.Actions[0].BackupPath

	// Restore the file to simulate it being modified again before a second run.
	writeExisting(t, workspace, targetPath, []byte("version-two-modified"))

	// Second backup run.
	req2 := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("version-three")),
		Conflicts:  conflict,
	}
	// Give the clock one second to advance, if the naming uses time at second granularity.
	// If the implementation uses sub-second precision or a monotonic counter, this sleep is
	// not required, but it keeps the test reliable for second-granularity naming.
	time.Sleep(1100 * time.Millisecond)

	result2, err := ex.Execute(context.Background(), req2)
	if err != nil {
		t.Fatalf("Execute (second run): %v", err)
	}
	backup2 := result2.Actions[0].BackupPath

	if backup1 == backup2 {
		t.Errorf("two sequential backups of the same file produced identical paths: %q", backup1)
	}
	if !fileExistsAt(t, backup1) {
		t.Errorf("first backup file %q was overwritten or removed by the second backup", backup1)
	}
	if !fileExistsAt(t, backup2) {
		t.Errorf("second backup file %q does not exist", backup2)
	}
}

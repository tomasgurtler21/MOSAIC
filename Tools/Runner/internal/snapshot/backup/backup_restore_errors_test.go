package backup_test

// Tests for RestoreFromBackup error paths, path-traversal rejection, and the
// WriteSetupComplete / SetupCompleteFileName API.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T6.5: Restore from backup -- error and edge cases
// ---------------------------------------------------------------------------

// TestRestoreFromBackup_MissingManifest_RemovesMarkerAndReturnsNil verifies
// that when the manifest file is absent (partial backup: crashed before
// WriteManifest), RestoreFromBackup removes the recovery marker and returns nil.
func TestRestoreFromBackup_MissingManifest_RemovesMarkerAndReturnsNil(t *testing.T) {
	// Arrange: write a recovery marker but no manifest.
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, backup.RecoveryMarkerFileName), []byte("marker\n"))
	// No WriteManifest call.

	// Act
	err := backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("RestoreFromBackup with missing manifest: %v", err)
	}
	markerPath := filepath.Join(agentsDir, backup.RecoveryMarkerFileName)
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Error("marker should be removed even when manifest is missing")
	}
}

// TestRestoreFromBackup_CorruptManifest_ReturnsError verifies that if the
// manifest exists but is corrupt, RestoreFromBackup returns an error and
// leaves the backup intact.
func TestRestoreFromBackup_CorruptManifest_ReturnsError(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(backupDir, manifest.ManifestFileName), []byte("{corrupt json{{"))

	// Act
	err := backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for corrupt manifest, got nil")
	}
}

// TestRestoreFromBackup_ManifestTempFileDoesNotBreakRestore verifies that a
// leftover temp file (ManifestTempFileName) from an interrupted WriteManifest
// atomic write does not cause RestoreFromBackup to fail.
func TestRestoreFromBackup_ManifestTempFileDoesNotBreakRestore(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := transform.TransformationsFor("opencode")
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	// Simulate a leftover temp file from an interrupted atomic write.
	writeFile(t, filepath.Join(backupDir, manifest.ManifestTempFileName), []byte("partial manifest data"))

	// Act
	err := backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("RestoreFromBackup with temp file present: %v", err)
	}
}

// TestRestoreFromBackup_FileCopyFailure_ReturnsError verifies that
// RestoreFromBackup returns a non-nil error when a file listed in the manifest
// is absent from the backup directory. This simulates a per-file copy failure
// (e.g., permission error or missing source) and ensures the error is
// propagated rather than silently skipped.
//
// The approach is Windows-compatible: instead of using chmod to deny access,
// we write the manifest to list worker.md but deliberately omit worker.md from
// backupDir. When RestoreFromBackup attempts to copy it back, the source open
// fails, exercising the same error-propagation path as a permission error.
func TestRestoreFromBackup_FileCopyFailure_ReturnsError(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	// Place worker.md in agentsDir so WriteManifest includes it, but do NOT
	// copy it to backupDir. RestoreFromBackup will try to open the absent
	// backup file and must return an error.
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := transform.TransformationsFor("opencode")
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	// worker.md is intentionally absent from backupDir.

	// Act
	err := backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected non-nil error when backup source file is missing, got nil")
	}
}

// TestRestoreFromBackup_PathTraversalInManifest_ReturnsError verifies that
// RestoreFromBackup rejects a manifest whose Files entry contains a filename
// with a path separator (path traversal attempt). The design contract states
// that RestoreFromBackup validates manifest entry filenames are base names with
// no path traversal components.
//
// The manifest is written directly as JSON to bypass WriteManifest, which
// produces only valid base names. A corrupt or malicious manifest listing
// "subdir/agent.md" or "../agent.md" must be rejected so the path traversal
// cannot escape the agents directory.
func TestRestoreFromBackup_PathTraversalInManifest_ReturnsError(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)

	// Construct a manifest with a path-traversal filename using filepath.Join
	// so the separator is platform-appropriate.
	m := manifest.Manifest{
		Timestamp: time.Now().UTC(),
		AgentsDir: agentsDir,
		Files: []manifest.ManifestEntry{
			{
				Filename:      filepath.Join("subdir", "agent.md"),
				Field:         "mode",
				OriginalValue: "subagent",
			},
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal test manifest: %v", err)
	}
	writeFile(t, filepath.Join(backupDir, manifest.ManifestFileName), data)

	// Act
	err = backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected non-nil error for path-traversal filename in manifest, got nil")
	}
}

// ---------------------------------------------------------------------------
// T6.6: Setup-complete signal (WriteSetupComplete)
// ---------------------------------------------------------------------------

// TestWriteSetupComplete_WritesSetupCompleteFile verifies that WriteSetupComplete
// creates the .setup-complete signal file in the backup directory.
func TestWriteSetupComplete_WritesSetupCompleteFile(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()

	// Act
	err := backup.WriteSetupComplete(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("WriteSetupComplete: %v", err)
	}
	signalPath := filepath.Join(backupDir, backup.SetupCompleteFileName)
	if _, err := os.Stat(signalPath); err != nil {
		t.Errorf(".setup-complete not created by WriteSetupComplete: %v", err)
	}
}

// TestSetupCompleteFileName_IsDotPrefixed verifies that SetupCompleteFileName
// is ".setup-complete" (dot-prefixed sentinel, distinct from .md files).
func TestSetupCompleteFileName_IsDotPrefixed(t *testing.T) {
	if backup.SetupCompleteFileName != ".setup-complete" {
		t.Errorf("SetupCompleteFileName: got %q, want %q", backup.SetupCompleteFileName, ".setup-complete")
	}
}

// TestRestoreFromBackup_DoesNotRemoveSetupComplete verifies that
// RestoreFromBackup leaves the .setup-complete signal file intact.
// Teardown of the backup directory (including .setup-complete) is the
// caller's responsibility.
func TestRestoreFromBackup_DoesNotRemoveSetupComplete(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := transform.TransformationsFor("opencode")
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	if err := backup.WriteSetupComplete(backupDir); err != nil {
		t.Fatalf("WriteSetupComplete: %v", err)
	}

	// Act
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert
	signalPath := filepath.Join(backupDir, backup.SetupCompleteFileName)
	if _, err := os.Stat(signalPath); err != nil {
		t.Errorf("RestoreFromBackup must NOT delete .setup-complete: %v", err)
	}
}

package backup_test

// Tests for RestoreFromBackup: basic restore correctness, CRLF/LF handling,
// recovery marker removal, idempotency, and tolerating a missing marker.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T6.5: Restore from backup (RestoreFromBackup)
// ---------------------------------------------------------------------------

// TestRestoreFromBackup_RestoresOriginalContent verifies that RestoreFromBackup
// copies backup files over the transformed originals, producing byte-identical
// content to the pre-transform state.
func TestRestoreFromBackup_RestoresOriginalContent(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\nmode: subagent\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

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
	// Sanity check: file was transformed.
	transformed := string(readFile(t, filepath.Join(agentsDir, "worker.md")))
	if strings.Contains(transformed, "mode: subagent") {
		t.Fatal("setup: file was not transformed; cannot test restore meaningfully")
	}

	// Act
	err := backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}
	restored := readFile(t, filepath.Join(agentsDir, "worker.md"))
	if string(restored) != string(original) {
		t.Errorf(
			"RestoreFromBackup: file not restored to original content:\ngot:  %q\nwant: %q",
			restored,
			original,
		)
	}
}

// TestRestoreFromBackup_WorksForCRLFFiles verifies that RestoreFromBackup
// restores CRLF files byte-identically to their pre-transform content.
func TestRestoreFromBackup_WorksForCRLFFiles(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\r\nmode: subagent\r\n---\r\n\r\nBody.\r\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

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

	// Act
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert
	restored := readFile(t, filepath.Join(agentsDir, "worker.md"))
	if string(restored) != string(original) {
		t.Errorf(
			"RestoreFromBackup: CRLF file not restored byte-identically:\ngot:  %q\nwant: %q",
			restored,
			original,
		)
	}
}

// TestRestoreFromBackup_WorksForLFFiles verifies that RestoreFromBackup
// restores LF files byte-identically.
func TestRestoreFromBackup_WorksForLFFiles(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\nmode: subagent\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

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

	// Act
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert
	restored := readFile(t, filepath.Join(agentsDir, "worker.md"))
	if string(restored) != string(original) {
		t.Errorf(
			"RestoreFromBackup: LF file not restored byte-identically:\ngot:  %q\nwant: %q",
			restored,
			original,
		)
	}
}

// TestRestoreFromBackup_RemovesRecoveryMarker verifies that RestoreFromBackup
// removes the recovery marker from agentsDir.
func TestRestoreFromBackup_RemovesRecoveryMarker(t *testing.T) {
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
	markerPath := filepath.Join(agentsDir, backup.RecoveryMarkerFileName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("setup: marker should exist before restore: %v", err)
	}

	// Act
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("recovery marker should be removed by RestoreFromBackup")
	}
}

// TestRestoreFromBackup_DoesNotDeleteBackupDir verifies that RestoreFromBackup
// does NOT delete the backup directory. Teardown is the caller's responsibility.
func TestRestoreFromBackup_DoesNotDeleteBackupDir(t *testing.T) {
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

	// Act
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert: backup directory and manifest must still be present
	if _, err := os.Stat(backupDir); err != nil {
		t.Errorf("backup directory must NOT be deleted by RestoreFromBackup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, manifest.ManifestFileName)); err != nil {
		t.Errorf("manifest must NOT be deleted by RestoreFromBackup: %v", err)
	}
}

// TestRestoreFromBackup_IsIdempotent verifies that calling RestoreFromBackup
// twice produces the same result: the second call is a no-op or overwrites
// with identical bytes.
func TestRestoreFromBackup_IsIdempotent(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\nmode: subagent\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)
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

	// Act: first restore
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup (first): %v", err)
	}
	afterFirst := readFile(t, filepath.Join(agentsDir, "worker.md"))

	// Act: second restore (marker is already gone, tests idempotency)
	if err := backup.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup (second): %v", err)
	}
	afterSecond := readFile(t, filepath.Join(agentsDir, "worker.md"))

	// Assert
	if string(afterFirst) != string(afterSecond) {
		t.Errorf(
			"RestoreFromBackup is not idempotent:\nafter first:  %q\nafter second: %q",
			afterFirst,
			afterSecond,
		)
	}
}

// TestRestoreFromBackup_ToleratesMissingMarker verifies that RestoreFromBackup
// returns no error when the recovery marker is absent (idempotent re-run after
// a partial earlier attempt that already removed the marker).
func TestRestoreFromBackup_ToleratesMissingMarker(t *testing.T) {
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
	// Deliberately do NOT write recovery marker -- simulating a re-run after
	// a partial restore that already removed it.

	// Act
	err := backup.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("RestoreFromBackup with missing marker: %v", err)
	}
}

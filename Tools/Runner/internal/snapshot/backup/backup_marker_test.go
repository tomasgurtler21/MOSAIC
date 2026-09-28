package backup_test

// Tests for WriteRecoveryMarker: content validation, naming conventions.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mosaic-run/internal/snapshot/backup"
)

// ---------------------------------------------------------------------------
// T6.3: Recovery marker (WriteRecoveryMarker)
// ---------------------------------------------------------------------------

// TestWriteRecoveryMarker_CreatesMarkerInAgentsDir verifies that
// WriteRecoveryMarker creates RUNNER-RECOVERY.txt in agentsDir.
func TestWriteRecoveryMarker_CreatesMarkerInAgentsDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)

	// Act
	err := backup.WriteRecoveryMarker(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	markerPath := filepath.Join(agentsDir, backup.RecoveryMarkerFileName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("recovery marker not created: %v", err)
	}
}

// TestRecoveryMarkerFileName_IsTxtExtension verifies that RecoveryMarkerFileName
// uses a .txt extension so it is not indexed as an agent file.
func TestRecoveryMarkerFileName_IsTxtExtension(t *testing.T) {
	if !strings.HasSuffix(backup.RecoveryMarkerFileName, ".txt") {
		t.Errorf("RecoveryMarkerFileName should end with .txt, got %q", backup.RecoveryMarkerFileName)
	}
}

// TestRecoveryMarkerFileName_NotDotPrefixed verifies that RecoveryMarkerFileName
// is not dot-prefixed (visible, not hidden on Unix-like systems).
func TestRecoveryMarkerFileName_NotDotPrefixed(t *testing.T) {
	if strings.HasPrefix(backup.RecoveryMarkerFileName, ".") {
		t.Errorf("RecoveryMarkerFileName must not be dot-prefixed, got %q", backup.RecoveryMarkerFileName)
	}
}

// TestRecoveryMarkerFileName_DoesNotConflictWithAgentPattern verifies that the
// marker file name cannot be mistaken for an agent file (no .md extension,
// not dot-prefixed).
func TestRecoveryMarkerFileName_DoesNotConflictWithAgentPattern(t *testing.T) {
	name := backup.RecoveryMarkerFileName
	if strings.HasSuffix(strings.ToLower(name), ".md") {
		t.Errorf("RecoveryMarkerFileName must not have .md extension, got %q", name)
	}
	if strings.HasPrefix(name, ".") {
		t.Errorf("RecoveryMarkerFileName must not be dot-prefixed, got %q", name)
	}
}

// TestWriteRecoveryMarker_ContentIncludesBackupLocation verifies that the
// marker file content contains the backup directory path so the user can
// locate the backup.
func TestWriteRecoveryMarker_ContentIncludesBackupLocation(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	content := string(readFile(t, filepath.Join(agentsDir, backup.RecoveryMarkerFileName)))

	// Assert
	if !strings.Contains(content, backupDir) {
		t.Errorf("marker content must include backup directory path %q; got:\n%s", backupDir, content)
	}
}

// TestWriteRecoveryMarker_ContentInstructsCopyNotRename verifies that the
// marker instructs the user to COPY files from the backup directory
// (overwriting originals), NOT to rename or replace the agents directory.
func TestWriteRecoveryMarker_ContentInstructsCopyNotRename(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	rawContent := string(readFile(t, filepath.Join(agentsDir, backup.RecoveryMarkerFileName)))
	content := strings.ToLower(rawContent)

	// Assert: must contain copy instruction
	if !strings.Contains(content, "copy") {
		t.Errorf("marker must instruct user to COPY files from backup; got:\n%s", rawContent)
	}
	// Assert: must NOT say rename (as an instruction word).
	// Use word-boundary matching so that "rename" embedded inside a path
	// component (e.g. t.TempDir() includes the test function name, which
	// contains "Rename") does not produce a false positive.
	if regexp.MustCompile(`\brename\b`).MatchString(content) {
		t.Errorf("marker must NOT say 'rename'; got:\n%s", rawContent)
	}
	// Assert: must NOT say replace agents directory
	if strings.Contains(content, "replace the agents") || strings.Contains(content, "replace agents") {
		t.Errorf("marker must NOT say 'replace agents directory'; got:\n%s", rawContent)
	}
}

// TestWriteRecoveryMarker_ContentInstructsDeleteBackupDir verifies that the
// marker instructs the user to delete the backup directory after restoring.
func TestWriteRecoveryMarker_ContentInstructsDeleteBackupDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	content := strings.ToLower(string(readFile(t, filepath.Join(agentsDir, backup.RecoveryMarkerFileName))))

	// Assert
	if !strings.Contains(content, "delete") && !strings.Contains(content, "remove") {
		t.Errorf("marker must instruct user to delete the backup directory; got:\n%s", content)
	}
}

// TestWriteRecoveryMarker_ContentReferencesMarkerFileName verifies that the
// marker content references RUNNER-RECOVERY.txt so the user knows which file
// to delete as the final cleanup step.
func TestWriteRecoveryMarker_ContentReferencesMarkerFileName(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	content := string(readFile(t, filepath.Join(agentsDir, backup.RecoveryMarkerFileName)))

	// Assert
	if !strings.Contains(content, backup.RecoveryMarkerFileName) {
		t.Errorf(
			"marker content should reference %q (the file to delete last); got:\n%s",
			backup.RecoveryMarkerFileName,
			content,
		)
	}
}

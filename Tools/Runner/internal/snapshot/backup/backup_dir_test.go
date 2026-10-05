package backup_test

// Tests for NewBackupDir and CopyAgentsToBackup: backup directory creation,
// file copying, and composable-API seam.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/snapshot/backup"
)

// ---------------------------------------------------------------------------
// T6.1: Backup directory creation (NewBackupDir)
// ---------------------------------------------------------------------------

// TestNewBackupDir_CreatesBackupDirAsSiblingOfAgentsDir verifies that
// NewBackupDir creates the backup directory as a sibling of agentsDir,
// names it BackupDirName, and returns alreadyExisted=false on first call.
func TestNewBackupDir_CreatesBackupDirAsSiblingOfAgentsDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("create agentsDir: %v", err)
	}

	// Act
	backupDir, alreadyExisted, err := backup.NewBackupDir(agentsDir)

	// Assert
	if err != nil {
		t.Fatalf("NewBackupDir: %v", err)
	}
	if alreadyExisted {
		t.Error("expected alreadyExisted=false for first call, got true")
	}
	wantBackupDir := filepath.Join(base, backup.BackupDirName)
	if backupDir != wantBackupDir {
		t.Errorf("backupDir: got %q, want %q", backupDir, wantBackupDir)
	}
	if _, err := os.Stat(backupDir); err != nil {
		t.Errorf("backup directory was not created: %v", err)
	}
}

// TestNewBackupDir_AlreadyExists_ReturnsAlreadyExistedTrue verifies that
// NewBackupDir returns alreadyExisted=true without error when the backup
// directory already exists (i.e., from a prior call or concurrent creator).
func TestNewBackupDir_AlreadyExists_ReturnsAlreadyExistedTrue(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("create agentsDir: %v", err)
	}
	backupDir, _, err := backup.NewBackupDir(agentsDir)
	if err != nil {
		t.Fatalf("NewBackupDir (first call): %v", err)
	}

	// Act: second call when directory already exists.
	backupDir2, alreadyExisted, err := backup.NewBackupDir(agentsDir)

	// Assert
	if err != nil {
		t.Fatalf("NewBackupDir (second call): %v", err)
	}
	if !alreadyExisted {
		t.Error("expected alreadyExisted=true for second call, got false")
	}
	if backupDir2 != backupDir {
		t.Errorf("backupDir path mismatch between calls: %q vs %q", backupDir, backupDir2)
	}
}

// TestNewBackupDir_BackupDirNameIsDotPrefixed verifies that BackupDirName
// is ".agents-backup" (dot-prefixed to avoid collision with user directories).
func TestNewBackupDir_BackupDirNameIsDotPrefixed(t *testing.T) {
	if backup.BackupDirName != ".agents-backup" {
		t.Errorf("BackupDirName: got %q, want %q", backup.BackupDirName, ".agents-backup")
	}
}

// TestNewBackupDir_IndependentFromCopyAgentsToBackup verifies that
// NewBackupDir can be called without immediately following it with
// CopyAgentsToBackup. The backup directory is empty after NewBackupDir alone.
// This is the composable seam the lock protocol uses.
func TestNewBackupDir_IndependentFromCopyAgentsToBackup(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	mustMkdir(t, agentsDir)

	// Act: only NewBackupDir, no CopyAgentsToBackup
	backupDir, alreadyExisted, err := backup.NewBackupDir(agentsDir)

	// Assert
	if err != nil {
		t.Fatalf("NewBackupDir: %v", err)
	}
	if alreadyExisted {
		t.Error("expected alreadyExisted=false")
	}
	if _, err := os.Stat(backupDir); err != nil {
		t.Fatalf("backup directory should exist after NewBackupDir: %v", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("ReadDir backupDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("backup directory should be empty before CopyAgentsToBackup, got %d entries", len(entries))
	}
}

// ---------------------------------------------------------------------------
// T6.1: File copying (CopyAgentsToBackup)
// ---------------------------------------------------------------------------

// TestCopyAgentsToBackup_CopiesAllTopLevelMdFiles verifies that
// CopyAgentsToBackup copies all top-level .md files to the backup directory.
func TestCopyAgentsToBackup_CopiesAllTopLevelMdFiles(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "agent-a.md"), []byte("# Agent A\n"))
	writeFile(t, filepath.Join(agentsDir, "agent-b.md"), []byte("# Agent B\n"))

	// Act
	err := backup.CopyAgentsToBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	for _, name := range []string{"agent-a.md", "agent-b.md"} {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Errorf("expected %q in backup, got: %v", name, err)
		}
	}
}

// TestCopyAgentsToBackup_ExcludesTxtRecoveryMarker verifies that the .txt
// recovery marker is NOT copied to the backup directory, only .md files are.
func TestCopyAgentsToBackup_ExcludesTxtRecoveryMarker(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "agent.md"), []byte("# Agent\n"))
	writeFile(t, filepath.Join(agentsDir, backup.RecoveryMarkerFileName), []byte("recovery marker\n"))
	writeFile(t, filepath.Join(agentsDir, "config.yaml"), []byte("key: value\n"))

	// Act
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert: .txt and .yaml must not be copied
	if _, err := os.Stat(filepath.Join(backupDir, backup.RecoveryMarkerFileName)); !os.IsNotExist(err) {
		t.Error("recovery marker (.txt) should NOT be copied to backup")
	}
	if _, err := os.Stat(filepath.Join(backupDir, "config.yaml")); !os.IsNotExist(err) {
		t.Error("config.yaml should NOT be copied to backup")
	}
	// .md must be copied
	if _, err := os.Stat(filepath.Join(backupDir, "agent.md")); err != nil {
		t.Errorf("agent.md should be copied to backup: %v", err)
	}
}

// TestCopyAgentsToBackup_ExcludesSubdirectories verifies that subdirectories
// in agentsDir are NOT copied to the backup directory.
func TestCopyAgentsToBackup_ExcludesSubdirectories(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	subdir := filepath.Join(agentsDir, "subdir")
	mustMkdir(t, subdir)
	writeFile(t, filepath.Join(subdir, "nested.md"), []byte("# Nested\n"))
	writeFile(t, filepath.Join(agentsDir, "top.md"), []byte("# Top\n"))

	// Act
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(backupDir, "subdir")); !os.IsNotExist(err) {
		t.Error("subdirectory should NOT be copied to backup")
	}
	if _, err := os.Stat(filepath.Join(backupDir, "top.md")); err != nil {
		t.Errorf("top-level .md file should be copied: %v", err)
	}
}

// TestCopyAgentsToBackup_FilesAreByteIdentical verifies that copied files
// are byte-identical to their originals (no content modification).
func TestCopyAgentsToBackup_FilesAreByteIdentical(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	// Use CRLF content to confirm byte-identity includes line endings.
	original := []byte("---\r\nmode: subagent\r\ntitle: My Agent\r\n---\r\n\r\n# Body\r\n")
	writeFile(t, filepath.Join(agentsDir, "agent.md"), original)

	// Act
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	got := readFile(t, filepath.Join(backupDir, "agent.md"))
	if string(got) != string(original) {
		t.Errorf("backup file not byte-identical to original:\ngot:  %q\nwant: %q", got, original)
	}
}

// TestCopyAgentsToBackup_CaseInsensitiveUpperMD verifies that a file with an
// uppercase .MD extension is included in the backup copy (case-insensitive
// extension matching).
func TestCopyAgentsToBackup_CaseInsensitiveUpperMD(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "foo.MD"), []byte("# Foo\n"))

	// Act
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(backupDir, "foo.MD")); err != nil {
		t.Errorf("foo.MD (uppercase extension) should be copied to backup: %v", err)
	}
}

// TestCopyAgentsToBackup_AgentMdExtension verifies that .agent.md files are
// included in the backup copy because filepath.Ext returns ".md" for them.
func TestCopyAgentsToBackup_AgentMdExtension(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "my-agent.agent.md"), []byte("# Agent\n"))
	writeFile(t, filepath.Join(agentsDir, "other.md"), []byte("# Other\n"))

	// Act
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(backupDir, "my-agent.agent.md")); err != nil {
		t.Errorf("my-agent.agent.md should be copied to backup: %v", err)
	}
}

// TestCopyAgentsToBackup_CaseInsensitiveUpperAGENTMD verifies that a compound
// uppercase extension (foo.AGENT.MD) is included in the backup copy.
func TestCopyAgentsToBackup_CaseInsensitiveUpperAGENTMD(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "foo.AGENT.MD"), []byte("# Foo\n"))

	// Act
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(backupDir, "foo.AGENT.MD")); err != nil {
		t.Errorf("foo.AGENT.MD should be copied to backup (case-insensitive match): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Local helpers
// ---------------------------------------------------------------------------

// mustMkdir creates a directory, failing the test on error.
// Accessible to all backup_*_test.go files because they share package backup_test.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
}

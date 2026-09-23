package snapshot_test

// Tests for backup-and-transform data layer: backup directory creation, file
// copying, recovery manifest, recovery marker, in-place transformation,
// setup-complete signal, and restore from backup.
//
// All tests use t.TempDir() for filesystem isolation and follow the
// Arrange-Act-Assert pattern. Helper functions writeFile and readFile are
// defined in snapshot_test.go (same package, same test binary).
//
// T6.1 -- Backup directory creation and file copying (composable API):
//   CreateBackupDir creates .agents-backup/ as sibling of agents directory,
//   returns alreadyExisted=false on first call, alreadyExisted=true on second.
//   CopyAgentsToBackup copies top-level .md files (case-insensitive extension),
//   excludes .txt files and subdirectories, produces byte-identical copies.
//   The two functions are independently callable (lock-protocol seam).
//
// T6.2 -- Recovery manifest (dry-run file list, read/write, error handling):
//   WriteManifest computes the file list via dry-run of rules and writes
//   recovery-manifest.json. ReadManifest reads and parses it. ReadManifest
//   returns *ManifestError{Kind: ManifestCorrupt} for unparseable files and
//   *ManifestError{Kind: ManifestMissing} for absent files; these are distinct.
//
// T6.3 -- Recovery marker (copy-back instructions, not rename):
//   WriteRecoveryMarker creates RUNNER-RECOVERY.txt (not dot-prefixed) in
//   agentsDir. Content includes backup path, COPY instructions, delete-backup
//   instruction, delete-marker instruction. Content must NOT say rename or
//   replace the agents directory.
//
// T6.4 -- In-place transformation:
//   TransformInPlace transforms matching .md files in agentsDir. Idempotent.
//   Files without matching rules are not rewritten. CRLF and LF preserved.
//
// T6.5 -- Restore from backup:
//   RestoreFromBackup copies backup files over transformed originals (byte-
//   identical). Works for LF and CRLF. Removes recovery marker. Does NOT
//   delete backup directory, manifest, .setup-complete, or lock files.
//   Idempotent. Tolerates missing marker. Manifest temp file does not break
//   restore.
//
// T6.6 -- Setup-complete signal:
//   WriteSetupComplete writes .setup-complete into backupDir.
//   RestoreFromBackup does NOT remove .setup-complete.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/snapshot"
)

// ---------------------------------------------------------------------------
// T6.1: Backup directory creation (CreateBackupDir)
// ---------------------------------------------------------------------------

// TestCreateBackupDir_CreatesBackupDirAsSiblingOfAgentsDir verifies that
// CreateBackupDir creates the backup directory as a sibling of agentsDir,
// names it BackupDirName, and returns alreadyExisted=false on first call.
func TestCreateBackupDir_CreatesBackupDirAsSiblingOfAgentsDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("create agentsDir: %v", err)
	}

	// Act
	backupDir, alreadyExisted, err := snapshot.CreateBackupDir(agentsDir)

	// Assert
	if err != nil {
		t.Fatalf("CreateBackupDir: %v", err)
	}
	if alreadyExisted {
		t.Error("expected alreadyExisted=false for first call, got true")
	}
	wantBackupDir := filepath.Join(base, snapshot.BackupDirName)
	if backupDir != wantBackupDir {
		t.Errorf("backupDir: got %q, want %q", backupDir, wantBackupDir)
	}
	if _, err := os.Stat(backupDir); err != nil {
		t.Errorf("backup directory was not created: %v", err)
	}
}

// TestCreateBackupDir_AlreadyExists_ReturnsAlreadyExistedTrue verifies that
// CreateBackupDir returns alreadyExisted=true without error when the backup
// directory already exists (i.e., from a prior call or concurrent creator).
func TestCreateBackupDir_AlreadyExists_ReturnsAlreadyExistedTrue(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("create agentsDir: %v", err)
	}
	backupDir, _, err := snapshot.CreateBackupDir(agentsDir)
	if err != nil {
		t.Fatalf("CreateBackupDir (first call): %v", err)
	}

	// Act: second call when directory already exists.
	backupDir2, alreadyExisted, err := snapshot.CreateBackupDir(agentsDir)

	// Assert
	if err != nil {
		t.Fatalf("CreateBackupDir (second call): %v", err)
	}
	if !alreadyExisted {
		t.Error("expected alreadyExisted=true for second call, got false")
	}
	if backupDir2 != backupDir {
		t.Errorf("backupDir path mismatch between calls: %q vs %q", backupDir, backupDir2)
	}
}

// TestCreateBackupDir_BackupDirNameIsDotPrefixed verifies that BackupDirName
// is ".agents-backup" (dot-prefixed to avoid collision with user directories).
func TestCreateBackupDir_BackupDirNameIsDotPrefixed(t *testing.T) {
	if snapshot.BackupDirName != ".agents-backup" {
		t.Errorf("BackupDirName: got %q, want %q", snapshot.BackupDirName, ".agents-backup")
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "agent-a.md"), []byte("# Agent A\n"))
	writeFile(t, filepath.Join(agentsDir, "agent-b.md"), []byte("# Agent B\n"))

	// Act
	err := snapshot.CopyAgentsToBackup(agentsDir, backupDir)

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "agent.md"), []byte("# Agent\n"))
	writeFile(t, filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName), []byte("recovery marker\n"))
	writeFile(t, filepath.Join(agentsDir, "config.yaml"), []byte("key: value\n"))

	// Act
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert: .txt and .yaml must not be copied
	if _, err := os.Stat(filepath.Join(backupDir, snapshot.RecoveryMarkerFileName)); !os.IsNotExist(err) {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	subdir := filepath.Join(agentsDir, "subdir")
	mustMkdir(t, subdir)
	writeFile(t, filepath.Join(subdir, "nested.md"), []byte("# Nested\n"))
	writeFile(t, filepath.Join(agentsDir, "top.md"), []byte("# Top\n"))

	// Act
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	// Use CRLF content to confirm byte-identity includes line endings.
	original := []byte("---\r\nmode: subagent\r\ntitle: My Agent\r\n---\r\n\r\n# Body\r\n")
	writeFile(t, filepath.Join(agentsDir, "agent.md"), original)

	// Act
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	got := readFile(t, filepath.Join(backupDir, "agent.md"))
	if string(got) != string(original) {
		t.Errorf("backup file not byte-identical to original:\ngot:  %q\nwant: %q", got, original)
	}
}

// TestCreateBackupDir_IndependentFromCopyAgentsToBackup verifies that
// CreateBackupDir can be called without immediately following it with
// CopyAgentsToBackup. The backup directory is empty after CreateBackupDir alone.
// This is the composable seam the lock protocol uses.
func TestCreateBackupDir_IndependentFromCopyAgentsToBackup(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	mustMkdir(t, agentsDir)

	// Act: only CreateBackupDir, no CopyAgentsToBackup
	backupDir, alreadyExisted, err := snapshot.CreateBackupDir(agentsDir)

	// Assert
	if err != nil {
		t.Fatalf("CreateBackupDir: %v", err)
	}
	if alreadyExisted {
		t.Error("expected alreadyExisted=false")
	}
	if _, err := os.Stat(backupDir); err != nil {
		t.Fatalf("backup directory should exist after CreateBackupDir: %v", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("ReadDir backupDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("backup directory should be empty before CopyAgentsToBackup, got %d entries", len(entries))
	}
}

// TestCopyAgentsToBackup_CaseInsensitiveUpperMD verifies that a file with an
// uppercase .MD extension is included in the backup copy (case-insensitive
// extension matching).
func TestCopyAgentsToBackup_CaseInsensitiveUpperMD(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "foo.MD"), []byte("# Foo\n"))

	// Act
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "my-agent.agent.md"), []byte("# Agent\n"))
	writeFile(t, filepath.Join(agentsDir, "other.md"), []byte("# Other\n"))

	// Act
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "foo.AGENT.MD"), []byte("# Foo\n"))

	// Act
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(backupDir, "foo.AGENT.MD")); err != nil {
		t.Errorf("foo.AGENT.MD should be copied to backup (case-insensitive match): %v", err)
	}
}

// ---------------------------------------------------------------------------
// T6.2: Recovery manifest (WriteManifest, ReadManifest)
// ---------------------------------------------------------------------------

// TestWriteManifest_CreatesManifestFile verifies that WriteManifest creates
// recovery-manifest.json in the backup directory.
func TestWriteManifest_CreatesManifestFile(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))

	// Act
	err := snapshot.WriteManifest(backupDir, agentsDir, rules)

	// Assert
	if err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	manifestPath := filepath.Join(backupDir, snapshot.ManifestFileName)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest file not created: %v", err)
	}
}

// TestWriteManifest_DryRunIncludesMatchingFiles verifies that WriteManifest
// computes the file list by dry-running rules: files whose content would be
// transformed appear in the manifest; others do not.
func TestWriteManifest_DryRunIncludesMatchingFiles(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	writeFile(t, filepath.Join(agentsDir, "orchestrator.md"), []byte("---\nmode: primary\n---\n"))
	writeFile(t, filepath.Join(agentsDir, "plain.md"), []byte("# No frontmatter\n"))

	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	foundWorker := false
	for _, entry := range manifest.Files {
		switch entry.Filename {
		case "worker.md":
			foundWorker = true
		case "orchestrator.md":
			t.Error("orchestrator.md should not appear in manifest (no matching rule content)")
		case "plain.md":
			t.Error("plain.md should not appear in manifest (no matching rule)")
		}
	}
	if !foundWorker {
		t.Error("worker.md (has matching rule) should appear in manifest")
	}
}

// TestWriteManifest_NoFilesMatchRules_WritesEmptyFilesSlice verifies that
// WriteManifest writes a manifest with an empty Files slice when no files
// match any rule. The manifest is still created.
func TestWriteManifest_NoFilesMatchRules_WritesEmptyFilesSlice(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "orchestrator.md"), []byte("---\nmode: primary\n---\n"))

	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest.Files == nil {
		t.Error("manifest.Files should be an empty slice, not nil")
	}
	if len(manifest.Files) != 0 {
		t.Errorf("expected empty manifest.Files, got %d entries", len(manifest.Files))
	}
}

// TestReadManifest_ReadsBackManifestCorrectly verifies that ReadManifest reads
// and correctly parses a manifest produced by WriteManifest.
func TestReadManifest_ReadsBackManifestCorrectly(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("ReadManifest returned nil manifest")
	}
	if manifest.Timestamp.IsZero() {
		t.Error("manifest.Timestamp should be non-zero")
	}
	if manifest.AgentsDir != agentsDir {
		t.Errorf("manifest.AgentsDir: got %q, want %q", manifest.AgentsDir, agentsDir)
	}
}

// TestReadManifest_ManifestEntryHasCorrectFields verifies that manifest entries
// include Filename, Field, and OriginalValue for transformed files.
func TestReadManifest_ManifestEntryHasCorrectFields(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest.Files) == 0 {
		t.Fatal("expected at least one manifest entry for worker.md")
	}
	entry := manifest.Files[0]
	if entry.Filename != "worker.md" {
		t.Errorf("entry.Filename: got %q, want %q", entry.Filename, "worker.md")
	}
	if entry.Field != "mode" {
		t.Errorf("entry.Field: got %q, want %q", entry.Field, "mode")
	}
	if entry.OriginalValue != "subagent" {
		t.Errorf("entry.OriginalValue: got %q, want %q", entry.OriginalValue, "subagent")
	}
}

// TestWriteManifest_LexicographicOrdering verifies that manifest entries are
// ordered by filename in lexicographic order, enabling deterministic byte-for-byte
// comparison in downstream tests.
func TestWriteManifest_LexicographicOrdering(t *testing.T) {
	// Arrange: write two matching files in reverse alphabetical order to
	// confirm ordering is not dependent on filesystem enumeration order.
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "zebra.md"), []byte("---\nmode: subagent\n---\n"))
	writeFile(t, filepath.Join(agentsDir, "aardvark.md"), []byte("---\nmode: subagent\n---\n"))

	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest.Files) < 2 {
		t.Fatalf("expected at least 2 manifest entries, got %d", len(manifest.Files))
	}
	if manifest.Files[0].Filename >= manifest.Files[1].Filename {
		t.Errorf(
			"manifest entries not in lexicographic order: got %q before %q",
			manifest.Files[0].Filename,
			manifest.Files[1].Filename,
		)
	}
}

// TestReadManifest_CorruptManifest_ReturnsManifestCorruptError verifies that
// ReadManifest returns *ManifestError{Kind: ManifestCorrupt} for a corrupt
// manifest file, and that ManifestError.Path holds the path to the manifest.
func TestReadManifest_CorruptManifest_ReturnsManifestCorruptError(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()
	writeFile(t, filepath.Join(backupDir, snapshot.ManifestFileName), []byte("{corrupt json{{"))

	// Act
	_, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for corrupt manifest, got nil")
	}
	var me *snapshot.ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *snapshot.ManifestError, got %T: %v", err, err)
	}
	if me.Kind != snapshot.ManifestCorrupt {
		t.Errorf("ManifestError.Kind: got %v, want ManifestCorrupt", me.Kind)
	}
	wantPath := filepath.Join(backupDir, snapshot.ManifestFileName)
	if me.Path != wantPath {
		t.Errorf("ManifestError.Path: got %q, want %q", me.Path, wantPath)
	}
}

// TestReadManifest_TruncatedManifest_ReturnsManifestCorruptError verifies that
// a truncated (incomplete) JSON file also returns ManifestCorrupt, and that
// ManifestError.Path holds the path to the manifest.
func TestReadManifest_TruncatedManifest_ReturnsManifestCorruptError(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()
	writeFile(t, filepath.Join(backupDir, snapshot.ManifestFileName), []byte(`{"timestamp":`))

	// Act
	_, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for truncated manifest, got nil")
	}
	var me *snapshot.ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *snapshot.ManifestError, got %T: %v", err, err)
	}
	if me.Kind != snapshot.ManifestCorrupt {
		t.Errorf("ManifestError.Kind: got %v, want ManifestCorrupt", me.Kind)
	}
	wantPath := filepath.Join(backupDir, snapshot.ManifestFileName)
	if me.Path != wantPath {
		t.Errorf("ManifestError.Path: got %q, want %q", me.Path, wantPath)
	}
}

// TestReadManifest_MissingManifest_ReturnsManifestMissingError verifies that
// ReadManifest returns *ManifestError{Kind: ManifestMissing} when the manifest
// file does not exist, and that ManifestError.Path holds the expected path.
func TestReadManifest_MissingManifest_ReturnsManifestMissingError(t *testing.T) {
	// Arrange: empty backup directory, no manifest file written.
	backupDir := t.TempDir()

	// Act
	_, err := snapshot.ReadManifest(backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing manifest, got nil")
	}
	var me *snapshot.ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *snapshot.ManifestError, got %T: %v", err, err)
	}
	if me.Kind != snapshot.ManifestMissing {
		t.Errorf("ManifestError.Kind: got %v, want ManifestMissing", me.Kind)
	}
	wantPath := filepath.Join(backupDir, snapshot.ManifestFileName)
	if me.Path != wantPath {
		t.Errorf("ManifestError.Path: got %q, want %q", me.Path, wantPath)
	}
}

// TestReadManifest_DistinctErrorKindForMissingVsCorrupt verifies that the
// ManifestError.Kind values are distinct for missing vs corrupt manifest files,
// enabling callers to handle each case differently.
func TestReadManifest_DistinctErrorKindForMissingVsCorrupt(t *testing.T) {
	// Arrange
	dir := t.TempDir()

	// Act: missing
	_, missingErr := snapshot.ReadManifest(dir)
	var missingME *snapshot.ManifestError
	if !errors.As(missingErr, &missingME) {
		t.Fatalf("missing: expected *snapshot.ManifestError, got %T", missingErr)
	}

	// Act: corrupt
	writeFile(t, filepath.Join(dir, snapshot.ManifestFileName), []byte("not json"))
	_, corruptErr := snapshot.ReadManifest(dir)
	var corruptME *snapshot.ManifestError
	if !errors.As(corruptErr, &corruptME) {
		t.Fatalf("corrupt: expected *snapshot.ManifestError, got %T", corruptErr)
	}

	// Assert
	if missingME.Kind == corruptME.Kind {
		t.Errorf(
			"missing and corrupt manifest errors must have distinct Kind values, both got %v",
			missingME.Kind,
		)
	}
}

// ---------------------------------------------------------------------------
// T6.3: Recovery marker (WriteRecoveryMarker)
// ---------------------------------------------------------------------------

// TestWriteRecoveryMarker_CreatesMarkerInAgentsDir verifies that
// WriteRecoveryMarker creates RUNNER-RECOVERY.txt in agentsDir.
func TestWriteRecoveryMarker_CreatesMarkerInAgentsDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)

	// Act
	err := snapshot.WriteRecoveryMarker(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	markerPath := filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("recovery marker not created: %v", err)
	}
}

// TestRecoveryMarkerFileName_IsTxtExtension verifies that RecoveryMarkerFileName
// uses a .txt extension so it is not indexed as an agent file.
func TestRecoveryMarkerFileName_IsTxtExtension(t *testing.T) {
	if !strings.HasSuffix(snapshot.RecoveryMarkerFileName, ".txt") {
		t.Errorf("RecoveryMarkerFileName should end with .txt, got %q", snapshot.RecoveryMarkerFileName)
	}
}

// TestRecoveryMarkerFileName_NotDotPrefixed verifies that RecoveryMarkerFileName
// is not dot-prefixed (visible, not hidden on Unix-like systems).
func TestRecoveryMarkerFileName_NotDotPrefixed(t *testing.T) {
	if strings.HasPrefix(snapshot.RecoveryMarkerFileName, ".") {
		t.Errorf("RecoveryMarkerFileName must not be dot-prefixed, got %q", snapshot.RecoveryMarkerFileName)
	}
}

// TestRecoveryMarkerFileName_DoesNotConflictWithAgentPattern verifies that the
// marker file name cannot be mistaken for an agent file (no .md extension,
// not dot-prefixed).
func TestRecoveryMarkerFileName_DoesNotConflictWithAgentPattern(t *testing.T) {
	name := snapshot.RecoveryMarkerFileName
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	content := string(readFile(t, filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName)))

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	rawContent := string(readFile(t, filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName)))
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	content := strings.ToLower(string(readFile(t, filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName))))

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}

	// Act
	content := string(readFile(t, filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName)))

	// Assert
	if !strings.Contains(content, snapshot.RecoveryMarkerFileName) {
		t.Errorf(
			"marker content should reference %q (the file to delete last); got:\n%s",
			snapshot.RecoveryMarkerFileName,
			content,
		)
	}
}

// ---------------------------------------------------------------------------
// T6.4: In-place transformation (TransformInPlace)
// ---------------------------------------------------------------------------

// TestTransformInPlace_AppliesRulesToMatchingFiles verifies that
// TransformInPlace rewrites .md files in agentsDir whose content matches
// the given rules.
func TestTransformInPlace_AppliesRulesToMatchingFiles(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n\nBody.\n"))

	// Act
	err := snapshot.TransformInPlace(agentsDir, rules)

	// Assert
	if err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	content := string(readFile(t, filepath.Join(agentsDir, "worker.md")))
	if !strings.Contains(content, "mode: primary") {
		t.Errorf("TransformInPlace: expected mode:primary after transform; got:\n%s", content)
	}
	if strings.Contains(content, "mode: subagent") {
		t.Errorf("TransformInPlace: mode:subagent should have been replaced; got:\n%s", content)
	}
}

// TestTransformInPlace_IsIdempotent verifies that applying TransformInPlace
// twice produces the same file content as applying it once.
func TestTransformInPlace_IsIdempotent(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := snapshot.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\ntitle: Worker\n---\n\nBody.\n"))

	// Act
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace (first): %v", err)
	}
	afterFirst := readFile(t, filepath.Join(agentsDir, "worker.md"))

	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace (second): %v", err)
	}
	afterSecond := readFile(t, filepath.Join(agentsDir, "worker.md"))

	// Assert
	if string(afterFirst) != string(afterSecond) {
		t.Errorf(
			"TransformInPlace is not idempotent:\nafter first:  %q\nafter second: %q",
			afterFirst,
			afterSecond,
		)
	}
}

// TestTransformInPlace_FilesWithNoMatchingRuleUnchanged verifies that files
// whose content does not match any rule are not modified by TransformInPlace.
func TestTransformInPlace_FilesWithNoMatchingRuleUnchanged(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := snapshot.TransformationsFor("opencode")
	content := []byte("---\nmode: primary\ntitle: Orchestrator\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "orchestrator.md"), content)

	// Act
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Assert
	got := readFile(t, filepath.Join(agentsDir, "orchestrator.md"))
	if string(got) != string(content) {
		t.Errorf(
			"TransformInPlace modified a file with no matching rule:\ngot:  %q\nwant: %q",
			got,
			content,
		)
	}
}

// TestTransformInPlace_PreservesCRLFLineEndings verifies that CRLF line
// endings are preserved after in-place transformation.
func TestTransformInPlace_PreservesCRLFLineEndings(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := snapshot.TransformationsFor("opencode")
	original := []byte("---\r\nmode: subagent\r\ntitle: Worker\r\n---\r\n\r\nBody.\r\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	// Act
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Assert
	got := string(readFile(t, filepath.Join(agentsDir, "worker.md")))
	if !strings.Contains(got, "mode: primary\r\n") {
		t.Errorf("TransformInPlace: CRLF endings not preserved on transformed line; got: %q", got)
	}
	// The file must not have lost its \r on any line.
	if strings.Contains(got, "title: Worker\n") && !strings.Contains(got, "title: Worker\r\n") {
		t.Errorf("TransformInPlace: CRLF file line endings converted to LF; got: %q", got)
	}
}

// TestTransformInPlace_PreservesLFLineEndings verifies that LF-only files
// are not given \r bytes during in-place transformation.
func TestTransformInPlace_PreservesLFLineEndings(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := snapshot.TransformationsFor("opencode")
	original := []byte("---\nmode: subagent\ntitle: Worker\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	// Act
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Assert
	got := readFile(t, filepath.Join(agentsDir, "worker.md"))
	if strings.Contains(string(got), "\r") {
		t.Errorf("TransformInPlace: LF file gained \\r bytes; got: %q", got)
	}
	if !strings.Contains(string(got), "mode: primary\n") {
		t.Errorf("TransformInPlace: expected mode:primary in LF file; got: %q", got)
	}
}

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\nmode: subagent\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	// Sanity check: file was transformed.
	transformed := string(readFile(t, filepath.Join(agentsDir, "worker.md")))
	if strings.Contains(transformed, "mode: subagent") {
		t.Fatal("setup: file was not transformed; cannot test restore meaningfully")
	}

	// Act
	err := snapshot.RestoreFromBackup(agentsDir, backupDir)

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\r\nmode: subagent\r\n---\r\n\r\nBody.\r\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Act
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\nmode: subagent\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Act
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	markerPath := filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("setup: marker should exist before restore: %v", err)
	}

	// Act
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Act
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert: backup directory and manifest must still be present
	if _, err := os.Stat(backupDir); err != nil {
		t.Errorf("backup directory must NOT be deleted by RestoreFromBackup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, snapshot.ManifestFileName)); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	original := []byte("---\nmode: subagent\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Act: first restore
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup (first): %v", err)
	}
	afterFirst := readFile(t, filepath.Join(agentsDir, "worker.md"))

	// Act: second restore (marker is already gone, tests idempotency)
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	// Deliberately do NOT write recovery marker -- simulating a re-run after
	// a partial restore that already removed it.

	// Act
	err := snapshot.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("RestoreFromBackup with missing marker: %v", err)
	}
}

// TestRestoreFromBackup_MissingManifest_RemovesMarkerAndReturnsNil verifies
// that when the manifest file is absent (partial backup: crashed before
// WriteManifest), RestoreFromBackup removes the recovery marker and returns nil.
func TestRestoreFromBackup_MissingManifest_RemovesMarkerAndReturnsNil(t *testing.T) {
	// Arrange: write a recovery marker but no manifest.
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName), []byte("marker\n"))
	// No WriteManifest call.

	// Act
	err := snapshot.RestoreFromBackup(agentsDir, backupDir)

	// Assert
	if err != nil {
		t.Fatalf("RestoreFromBackup with missing manifest: %v", err)
	}
	markerPath := filepath.Join(agentsDir, snapshot.RecoveryMarkerFileName)
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(backupDir, snapshot.ManifestFileName), []byte("{corrupt json{{"))

	// Act
	err := snapshot.RestoreFromBackup(agentsDir, backupDir)

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	// Simulate a leftover temp file from an interrupted atomic write.
	writeFile(t, filepath.Join(backupDir, snapshot.ManifestTempFileName), []byte("partial manifest data"))

	// Act
	err := snapshot.RestoreFromBackup(agentsDir, backupDir)

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	// Place worker.md in agentsDir so WriteManifest includes it, but do NOT
	// copy it to backupDir. RestoreFromBackup will try to open the absent
	// backup file and must return an error.
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	// worker.md is intentionally absent from backupDir.

	// Act
	err := snapshot.RestoreFromBackup(agentsDir, backupDir)

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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)

	// Construct a manifest with a path-traversal filename using filepath.Join
	// so the separator is platform-appropriate.
	m := snapshot.Manifest{
		Timestamp: time.Now().UTC(),
		AgentsDir: agentsDir,
		Files: []snapshot.ManifestEntry{
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
	writeFile(t, filepath.Join(backupDir, snapshot.ManifestFileName), data)

	// Act
	err = snapshot.RestoreFromBackup(agentsDir, backupDir)

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
	err := snapshot.WriteSetupComplete(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("WriteSetupComplete: %v", err)
	}
	signalPath := filepath.Join(backupDir, snapshot.SetupCompleteFileName)
	if _, err := os.Stat(signalPath); err != nil {
		t.Errorf(".setup-complete not created by WriteSetupComplete: %v", err)
	}
}

// TestSetupCompleteFileName_IsDotPrefixed verifies that SetupCompleteFileName
// is ".setup-complete" (dot-prefixed sentinel, distinct from .md files).
func TestSetupCompleteFileName_IsDotPrefixed(t *testing.T) {
	if snapshot.SetupCompleteFileName != ".setup-complete" {
		t.Errorf("SetupCompleteFileName: got %q, want %q", snapshot.SetupCompleteFileName, ".setup-complete")
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
	backupDir := filepath.Join(base, snapshot.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	rules := snapshot.TransformationsFor("opencode")
	if err := snapshot.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("CopyAgentsToBackup: %v", err)
	}
	if err := snapshot.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := snapshot.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("WriteRecoveryMarker: %v", err)
	}
	if err := snapshot.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	if err := snapshot.WriteSetupComplete(backupDir); err != nil {
		t.Fatalf("WriteSetupComplete: %v", err)
	}

	// Act
	if err := snapshot.RestoreFromBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("RestoreFromBackup: %v", err)
	}

	// Assert
	signalPath := filepath.Join(backupDir, snapshot.SetupCompleteFileName)
	if _, err := os.Stat(signalPath); err != nil {
		t.Errorf("RestoreFromBackup must NOT delete .setup-complete: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Local helpers
// ---------------------------------------------------------------------------

// mustMkdir creates a directory, failing the test on error.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
}

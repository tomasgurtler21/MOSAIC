package snapshot

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
)

// Constants for backup-and-transform protocol file names and directory names.
const (
	// BackupDirName is the name of the shared backup directory, placed as a
	// sibling of the agents directory. Dot-prefixed to avoid collision with
	// user-created directories.
	BackupDirName = ".agents-backup"

	// ManifestFileName is the name of the recovery manifest file written into
	// the backup directory.
	ManifestFileName = "recovery-manifest.json"

	// RecoveryMarkerFileName is the name of the human-readable recovery marker
	// file written into the agents directory before in-place transforms. Uses
	// .txt extension so it is not indexed as an agent file.
	RecoveryMarkerFileName = "RUNNER-RECOVERY.txt"

	// SetupCompleteFileName is the name of the signal file written into the
	// backup directory after all transforms complete.
	SetupCompleteFileName = ".setup-complete"

	// ManifestTempFileName is the name of the temp file used by WriteManifest's
	// atomic write. Recognized by the ownership check so a crash between write
	// and rename does not make the directory foreign.
	ManifestTempFileName = ".recovery-manifest.json.tmp"

	// RestoringFileName is the name of the sentinel lock file inside the backup
	// directory that serializes all exit-protocol runs.
	RestoringFileName = ".restoring"

	// RunLockPrefix is the prefix for per-run lock files inside the backup
	// directory.
	RunLockPrefix = ".lock-"
)

// RunLockPath returns the path of the per-run lock file inside backupDir.
func RunLockPath(backupDir, runID string) string {
	return filepath.Join(backupDir, RunLockPrefix+runID)
}

// CreateBackupDir creates the shared backup directory as a sibling of the
// agents directory. The directory name is BackupDirName (".agents-backup").
//
// Returns (backupDir, false, nil) on successful creation.
// Returns (backupDir, true, nil) if the directory already exists.
// Returns ("", false, error) on other filesystem errors.
//
// Uses os.Mkdir (not MkdirAll) so that exactly one concurrent caller wins
// the creation race; losers see alreadyExisted=true.
func CreateBackupDir(agentsDir string) (backupDir string, alreadyExisted bool, err error) {
	backupDir = filepath.Join(filepath.Dir(agentsDir), BackupDirName)
	mkErr := os.Mkdir(backupDir, 0o755)
	if mkErr == nil {
		return backupDir, false, nil
	}
	if errors.Is(mkErr, os.ErrExist) {
		return backupDir, true, nil
	}
	return "", false, mkErr
}

// isMDFile reports whether the file's extension is ".md" using case-insensitive
// comparison. This matches .agent.md files since filepath.Ext returns ".md".
func isMDFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

// CopyAgentsToBackup copies all top-level agent files (.md, case-insensitive,
// including .agent.md) from agentsDir into backupDir. Copied files are
// byte-identical to originals. .txt files and subdirectories are not copied.
// Returns *domain.RefusalError on read/write failures.
func CopyAgentsToBackup(agentsDir, backupDir string) error {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  agentsDir,
			Reason:    fmt.Sprintf("cannot read agents directory: %v", err),
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isMDFile(entry.Name()) {
			continue
		}
		srcPath := filepath.Join(agentsDir, entry.Name())
		dstPath := filepath.Join(backupDir, entry.Name())
		if copyErr := copyFile(srcPath, dstPath); copyErr != nil {
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Name(),
				Reason:    fmt.Sprintf("failed to copy agent file to backup: %v", copyErr),
			}
		}
	}
	return nil
}

// copyFile copies the file at src to dst byte-identically.
func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstF.Close()

	if _, err := io.Copy(dstF, srcF); err != nil {
		return err
	}
	return nil
}

// TransformInPlace applies transformation rules to all top-level .md files
// (case-insensitive extension match, same predicate as CopyAgentsToBackup)
// in agentsDir, modifying files in place. Only files whose content changes
// are rewritten. Idempotent. Preserves line endings per the CRLF fix.
// Returns *domain.RefusalError on read/write failures.
func TransformInPlace(agentsDir string, rules []TransformRule) error {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  agentsDir,
			Reason:    fmt.Sprintf("cannot read agents directory: %v", err),
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isMDFile(entry.Name()) {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Name(),
				Reason:    fmt.Sprintf("cannot read agent file: %v", err),
			}
		}
		transformed := TransformFile(content, rules)
		if string(transformed) == string(content) {
			continue
		}
		if err := os.WriteFile(path, transformed, 0o644); err != nil {
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Name(),
				Reason:    fmt.Sprintf("cannot write agent file: %v", err),
			}
		}
	}
	return nil
}

// WriteSetupComplete writes the .setup-complete signal file into backupDir.
// Its presence signals to joiners that all transforms have been applied.
// Returns *domain.RefusalError on write failure.
func WriteSetupComplete(backupDir string) error {
	path := filepath.Join(backupDir, SetupCompleteFileName)
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  path,
			Reason:    fmt.Sprintf("cannot write setup-complete signal: %v", err),
		}
	}
	return nil
}

// RestoreFromBackup reads the manifest from backupDir, copies each listed
// backup file over its original in agentsDir, and removes the recovery marker.
// Does NOT delete the backup directory, .setup-complete, manifest, or lock files.
//
// Validates manifest entry filenames are base names (no path traversal).
// Uses the agentsDir parameter (not Manifest.AgentsDir) as restore target.
//
// Manifest handling:
//   - ManifestMissing: removes recovery marker, returns nil (no files copied).
//   - ManifestCorrupt: returns *domain.RefusalError (backup left intact).
//
// Tolerates a missing recovery marker (idempotent on re-run).
// Returns *domain.RefusalError on critical file copy failures.
func RestoreFromBackup(agentsDir, backupDir string) error {
	manifest, err := ReadManifest(backupDir)
	if err != nil {
		var me *ManifestError
		if errors.As(err, &me) && me.Kind == ManifestMissing {
			// Partial backup: crashed before WriteManifest. No transforms were
			// applied, so just remove the marker (if present) and return nil.
			_ = os.Remove(filepath.Join(agentsDir, RecoveryMarkerFileName))
			return nil
		}
		// Corrupt manifest: leave backup intact for manual inspection.
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("cannot read manifest for restore: %v", err),
		}
	}

	// Restore each file listed in the manifest. Deduplicate by filename because
	// a file with multiple transformed fields produces one entry per (file, field)
	// pair but needs to be copied only once.
	seen := make(map[string]bool)
	for _, entry := range manifest.Files {
		// Validate: filename must be a bare base name with no path separators.
		if filepath.Base(entry.Filename) != entry.Filename {
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Filename,
				Reason:    "manifest entry filename contains path traversal",
			}
		}
		if seen[entry.Filename] {
			continue
		}
		seen[entry.Filename] = true

		srcPath := filepath.Join(backupDir, entry.Filename)
		dstPath := filepath.Join(agentsDir, entry.Filename)
		if copyErr := copyFile(srcPath, dstPath); copyErr != nil {
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Filename,
				Reason:    fmt.Sprintf("cannot restore file from backup: %v", copyErr),
			}
		}
	}

	// Remove the recovery marker from the agents directory. A missing marker is
	// not an error: it indicates an idempotent re-run after a prior restore that
	// already cleaned it up.
	markerPath := filepath.Join(agentsDir, RecoveryMarkerFileName)
	if removeErr := os.Remove(markerPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  markerPath,
			Reason:    fmt.Sprintf("cannot remove recovery marker: %v", removeErr),
		}
	}

	return nil
}

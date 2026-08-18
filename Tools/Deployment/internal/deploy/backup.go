package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mosaic-deploy/internal/manifest"
)

// createBackup copies the file at srcPath to a timestamped backup path under
// <workspace>/.mosaic/backups/<relative-target-path>.<RFC3339-compact>.bak
// and returns the absolute backup path. The source file is not modified.
func createBackup(workspace, targetPath, srcPath string) (string, error) {
	existing, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("read file for backup: %w", err)
	}

	// Build backup directory mirroring the relative target directory structure.
	relDir := filepath.Dir(targetPath)
	backupDir := filepath.Join(workspace, manifest.Dir, manifest.BackupDir, relDir)
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}

	// Backup filename: <original-filename>.<timestamp>.bak
	// The timestamp uses a compact format with nanosecond precision (underscore-separated
	// sub-second component) so that repeated backups of the same file within the same second
	// produce distinct paths and the second write never silently overwrites the first.
	now := time.Now().UTC()
	timestamp := now.Format("20060102T150405") + fmt.Sprintf("_%09d", now.Nanosecond()) + "Z"
	backupName := filepath.Base(targetPath) + "." + timestamp + ".bak"
	backupPath := filepath.Join(backupDir, backupName)

	if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	return backupPath, nil
}

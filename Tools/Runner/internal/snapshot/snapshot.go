package snapshot

import (
	"fmt"
	"os"
	"path/filepath"

	"mosaic-run/internal/domain"
)

// CreateSnapshot copies the flat agents directory at srcDir to dstDir and
// applies the given transformation rules to every copied file's YAML
// frontmatter.
//
// srcDir must exist and contain .md files. If dstDir already exists, it is
// removed completely and recreated from source files (no collision guard).
//
// Only regular files are copied. Subdirectories in srcDir are ignored (the
// agents directory is flat by convention, matching agentresolve.ResolveAll's
// scan behavior).
//
// Returns *domain.RefusalError with Component "snapshot" if:
//   - srcDir does not exist or cannot be read
//   - any file copy or transformation fails
//
// Returns other errors if removal or creation of dstDir fails (filesystem or
// system errors, not business-logic refusals).
//
// On any error, CreateSnapshot attempts to remove a partially-created dstDir
// (best-effort cleanup of partial state).
func CreateSnapshot(srcDir, dstDir string, rules []TransformRule) error {
	// If snapshot already exists, remove it completely before recreating.
	// This enables recovery from prior failed runs without blocking on collision.
	// Best-effort: if removal fails, return error (not RefusalError) so caller
	// treats it as a system/filesystem error, not a business-logic refusal.
	if _, err := os.Stat(dstDir); err == nil {
		if rmErr := os.RemoveAll(dstDir); rmErr != nil {
			return fmt.Errorf("failed to remove stale snapshot directory %s: %w", dstDir, rmErr)
		}
	}

	// Enumerate source directory. This also validates that srcDir exists.
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  srcDir,
			Reason:    fmt.Sprintf("cannot read agents directory: %v", err),
		}
	}

	// Create the snapshot directory.
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("failed to create snapshot directory %s: %w", dstDir, err)
	}

	// Copy only regular .md files; skip subdirectories and non-.md files.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		content, err := os.ReadFile(srcPath)
		if err != nil {
			_ = os.RemoveAll(dstDir)
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Name(),
				Reason:    fmt.Sprintf("failed to copy agent file %s: %v", entry.Name(), err),
			}
		}

		content = TransformFile(content, rules)

		if err := os.WriteFile(dstPath, content, 0o644); err != nil {
			_ = os.RemoveAll(dstDir)
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Name(),
				Reason:    fmt.Sprintf("failed to copy agent file %s: %v", entry.Name(), err),
			}
		}
	}

	return nil
}

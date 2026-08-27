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
// srcDir must exist and contain .md files. dstDir must not already exist;
// CreateSnapshot creates it.
//
// Only regular files are copied. Subdirectories in srcDir are ignored (the
// agents directory is flat by convention, matching agentresolve.ResolveAll's
// scan behavior).
//
// Returns *domain.RefusalError with Component "snapshot" if:
//   - srcDir does not exist or cannot be read
//   - dstDir already exists (collision guard)
//   - any file copy or transformation fails
//
// On any error, CreateSnapshot attempts to remove a partially-created dstDir
// (best-effort cleanup of partial state).
func CreateSnapshot(srcDir, dstDir string, rules []TransformRule) error {
	// Guard against collisions: dstDir must not already exist.
	if _, err := os.Stat(dstDir); err == nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  dstDir,
			Reason:    fmt.Sprintf("snapshot directory already exists: %s (stale from prior run?)", dstDir),
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
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  dstDir,
			Reason:    fmt.Sprintf("cannot create snapshot directory: %v", err),
		}
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

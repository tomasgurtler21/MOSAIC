package catalog

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolveRoot walks up the directory tree from dir looking for the MOSAIC repository
// markers and returns the root path. If the input is "." or empty it is converted to an
// absolute path first so the returned root is also absolute (satisfying callers that need
// an absolute path). For all other relative inputs the walk is performed using the path as
// supplied, keeping the returned root in the same relative coordinate system so that
// filepath.Rel(root, input) remains computable on all platforms.
func resolveRoot(dir string) (string, error) {
	startPath := dir
	if dir == "" || dir == "." {
		// Convert "." / "" to absolute so the returned root is also absolute.
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("catalog.ResolveRoot: %w", err)
		}
		startPath = abs
	}

	// Verify the starting path is accessible.
	if _, err := os.Stat(startPath); err != nil {
		return "", fmt.Errorf("catalog.ResolveRoot: %w", err)
	}

	current := startPath
	for {
		if isMosaicRoot(current) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached the top of the path (filesystem root for absolute paths,
			// or "." for relative paths that have been fully consumed).
			break
		}
		current = parent
	}
	return "", fmt.Errorf("catalog.ResolveRoot: %w", ErrNotMosaicRoot)
}

// isMosaicRoot returns true when dir contains both required MOSAIC repository marker files.
func isMosaicRoot(dir string) bool {
	markers := []string{
		filepath.Join("Agents", "Generic", "SOURCE-FORMAT.md"),
		filepath.Join("Workflows", "Index.md"),
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err != nil {
			return false
		}
	}
	return true
}

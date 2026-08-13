package catalog

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolveRoot validates that dir is the MOSAIC repository root and returns its absolute path.
// If the input is "." or empty it is converted to an absolute path first so the returned path
// is also absolute. The supplied directory must itself carry the MOSAIC root markers; the
// function does not walk up the tree looking for them.
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

	// Validate that this directory is the MOSAIC root — no upward walk.
	if !isMosaicRoot(startPath) {
		return "", fmt.Errorf("catalog.ResolveRoot: %w", ErrNotMosaicRoot)
	}
	return startPath, nil
}

// isMosaicRoot returns true when dir contains both required MOSAIC repository marker files
// at their Catalog/-prefixed locations:
//
//	Catalog/Agents/Generic/SourceFilesFormat.md
//	Catalog/Workflows/Index.md
//
// A directory carrying the markers only at the legacy (pre-migration) Agents/Generic/ and
// Workflows/ locations is not accepted.
func isMosaicRoot(dir string) bool {
	markers := []string{
		filepath.Join("Catalog", "Agents", "Generic", "SourceFilesFormat.md"),
		filepath.Join("Catalog", "Workflows", "Index.md"),
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err != nil {
			return false
		}
	}
	return true
}

package main

import (
	"fmt"
	"path/filepath"

	"mosaic-deploy/internal/catalog"
)

// resolveCatalogFolder converts a pre-scanned --catalog-folder flag value into the
// absolute catalogue root to pass to catalog.Load.
//
// When flagValue is empty, the default catalogue root ({mosaicRoot}/Catalog) is
// returned as an absolute path without validation. A missing default must not
// introduce a new failure mode for ordinary runs.
//
// When flagValue is non-empty, it is made absolute and then validated with
// catalog.ValidateCatalogRoot. A nonexistent path returns an error wrapping
// ErrCatalogFolderMissing; a path that is a file (not a directory) returns an
// error wrapping ErrCatalogFolderNotDirectory. On success the absolute path is
// returned.
func resolveCatalogFolder(mosaicRoot, flagValue string) (string, error) {
	if flagValue == "" {
		absDefault, err := filepath.Abs(catalog.DefaultCatalogRoot(mosaicRoot))
		if err != nil {
			return "", fmt.Errorf("catalog folder: resolve default: %w", err)
		}
		return absDefault, nil
	}

	absPath, err := filepath.Abs(flagValue)
	if err != nil {
		return "", fmt.Errorf("catalog folder: %w", err)
	}

	if err := catalog.ValidateCatalogRoot(absPath); err != nil {
		return "", err
	}

	return absPath, nil
}

package codex

import "mosaic-deploy/internal/catalog/catalogpaths"

// RepoContentDir is the MOSAIC-root-relative path to the Codex harness injection
// content directory. It is a compile-time constant derived from catalogpaths so the
// import-guard tool can verify it is the single source of truth.
const RepoContentDir = catalogpaths.HarnessContentDirCodex

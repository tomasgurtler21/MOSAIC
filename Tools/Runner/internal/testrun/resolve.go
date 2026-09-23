// Package testrun: harness binary resolution.
//
// This file defines the harness binary resolution function and its helpers.
// All three exported functions live here because their contracts are tightly
// coupled: ResolveHarnessBinaries is the core function, HarnessDisplayOrder
// is the single source of truth for display ordering, and ResolveAndAnnounce
// wraps both with logging and optional stdout output.
package testrun

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
)

// harnessDefaultBinary maps each supported CLI harness ID to the default
// binary name that exec.LookPath should search for.
//
// This mapping duplicates the per-harness defaults in buildAdapter (cmd/mosaic-run/main.go).
// Duplication is accepted; a coverage test guards against drift by verifying
// every CLIHarnesses() ID has an entry here.
//
// Keys are exact-match (case-sensitive), consistent with catalog convention.
var harnessDefaultBinary = map[string]string{
	"claude-code": "claude",
	"opencode":    "opencode",
	"ghcp-cli":    "copilot",
}

// ResolveHarnessBinaries resolves each harness ID in harnesses to a
// guaranteed-absolute filesystem path for its default binary.
//
// lookPath is the resolution function (production: exec.LookPath).
// The "fake" harness ID is silently skipped (no map entry, no error).
//
// Processing order: harness IDs are processed in slice order. For each ID:
//  1. If the ID is "fake", skip it.
//  2. If the ID was already resolved (duplicate), skip it.
//  3. If the ID has no known binary name, return an error immediately.
//  4. Call lookPath for the binary name.
//  5. If err is non-nil and NOT errors.Is(err, exec.ErrDot), return a
//     resolution-failure error immediately (fail-fast).
//  6. If path is empty and err is nil or errors.Is(err, exec.ErrDot),
//     return an empty-path error.
//  7. If errors.Is(err, exec.ErrDot) and path is non-empty, proceed to
//     absolutize.
//  8. If path is non-absolute, absolutize via filepath.Abs. If that fails,
//     return an error.
//  9. Store the guaranteed-absolute path in the result map.
//
// Every returned path is guaranteed absolute and non-empty.
// An empty harnesses slice returns a non-nil empty map and nil error.
// Duplicate harness IDs are resolved once; subsequent duplicates reuse the
// first result (no second lookPath call).
func ResolveHarnessBinaries(
	harnesses []string,
	lookPath func(file string) (string, error),
) (map[string]string, error) {
	result := make(map[string]string)

	for _, id := range harnesses {
		// Step 1: skip "fake".
		if id == "fake" {
			continue
		}

		// Step 2: skip duplicates.
		if _, already := result[id]; already {
			continue
		}

		// Step 3: look up the binary name.
		binaryName, known := harnessDefaultBinary[id]
		if !known {
			return nil, fmt.Errorf("testrun: unknown harness ID %q: no default binary name", id)
		}

		// Step 4: call lookPath.
		path, lookErr := lookPath(binaryName)

		// Step 5: if error is non-nil and NOT ErrDot, fail fast.
		if lookErr != nil && !errors.Is(lookErr, exec.ErrDot) {
			return nil, fmt.Errorf("testrun: harness %q binary %q not found on PATH\nPATH searched: %s\ncause: %w",
				id, binaryName, os.Getenv("PATH"), lookErr)
		}

		// Step 6: check for empty path.
		if path == "" {
			if errors.Is(lookErr, exec.ErrDot) {
				return nil, fmt.Errorf("testrun: harness %q binary %q: lookPath returned empty path with ErrDot", id, binaryName)
			}
			return nil, fmt.Errorf("testrun: harness %q binary %q: lookPath returned empty path", id, binaryName)
		}

		// Steps 7 & 8: absolutize if not already absolute.
		// A path is treated as already absolute when filepath.IsAbs reports
		// true, or when the path starts with "/" (Unix-rooted path). The
		// latter covers cross-platform test scenarios where a Unix-style
		// absolute path is injected via the lookPath seam on Windows; such
		// paths require no absolutization because they are already rooted.
		if !filepath.IsAbs(path) && !strings.HasPrefix(path, "/") {
			absPath, absErr := filepath.Abs(path)
			if absErr != nil {
				return nil, fmt.Errorf("testrun: harness %q binary %q resolved to relative path %q and absolutization failed: %w",
					id, binaryName, path, absErr)
			}
			path = absPath
		}

		// Step 9: store the guaranteed-absolute path.
		result[id] = path
	}

	return result, nil
}

// HarnessDisplayOrder returns the ordered, deduplicated list of harness IDs
// suitable for display, preserving the input slice order and skipping "fake"
// entries. This is the single source of truth for display ordering; both
// ResolveAndAnnounce and TUI message construction use it.
func HarnessDisplayOrder(harnesses []string) []string {
	seen := make(map[string]bool)
	var order []string
	for _, id := range harnesses {
		if id == "fake" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	return order
}

// ResolveAndAnnounce resolves harness binaries and announces the results.
//
// On success, iterates the result using HarnessDisplayOrder to determine
// ordering (skipping "fake" and deduplicating), logs each resolved path to
// logger using domain.EventTestrunResolvePath, and if cliOutput is non-nil,
// writes a human-readable line per resolved harness to it.
//
// On failure, returns the resolution error without logging.
//
// cliOutput may be nil (TUI mode: no stdout output, logging only).
// logger may be nil (untyped nil interface value); if nil, logging is skipped.
func ResolveAndAnnounce(
	harnesses []string,
	lookPath func(file string) (string, error),
	cliOutput io.Writer,
	logger domain.DebugLogger,
) (map[string]string, error) {
	resolved, err := ResolveHarnessBinaries(harnesses, lookPath)
	if err != nil {
		return nil, err
	}

	for _, id := range HarnessDisplayOrder(harnesses) {
		absPath := resolved[id]

		if logger != nil {
			logger.Log(domain.EventTestrunResolvePath, "", domain.F("harness", id), domain.F("path", absPath))
		}

		if cliOutput != nil {
			fmt.Fprintf(cliOutput, "  %s: %s\n", id, absPath)
		}
	}

	return resolved, nil
}

package domain

import "context"

// HarnessVersionReporter is an optional capability of a HarnessAdapter:
// an adapter that can tell which version of its harness will run the
// subject implements it; callers type-assert for it. Absence means
// "version unknown" and is never an error.
type HarnessVersionReporter interface {
	// HarnessVersion returns the harness version string (e.g. "2.1.284"),
	// or "" when it cannot be determined. It never returns an error, never
	// panics, returns within a bounded time even when ctx carries no
	// deadline, and is safe for concurrent use by multiple goroutines.
	HarnessVersion(ctx context.Context) string
}

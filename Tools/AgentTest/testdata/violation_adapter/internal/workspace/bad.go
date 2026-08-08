// Package workspace violates the adapter rule by importing a use case
// (runner) instead of only domain and the pure cores.
package workspace

import "mosaic-agent-test/internal/runner"

var _ = runner.Runner{}

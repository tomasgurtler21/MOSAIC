// Package intercept violates the pure-core layer rule by importing an
// adapter (workspace) instead of only domain and other pure cores.
package intercept

import "mosaic-agent-test/internal/workspace"

var _ = workspace.Manager{}

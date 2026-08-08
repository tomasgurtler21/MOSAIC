// Package claudecode is a conforming fixture standing in for the concrete
// Claude Code harness adapter: it imports only domain.
package claudecode

import "mosaic-agent-test/internal/domain"

var _ domain.HarnessAdapter

// Adapter stands in for the real concrete adapter type.
type Adapter struct{}

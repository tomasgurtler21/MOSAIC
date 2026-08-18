// Package suite violates harness isolation by importing a concrete harness
// adapter package directly instead of going through domain.HarnessAdapter.
package suite

import "mosaic-agent-test/internal/harness/claudecode"

var _ = claudecode.Adapter{}

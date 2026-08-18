// Package main violates harness isolation: it is a cmd/ package, but not
// the module's binary composition root (cmd/mosaic-agent-test), so the
// composition-root exemption must not extend to it. It imports a concrete
// harness adapter package directly instead of going through
// domain.HarnessAdapter.
package main

import "mosaic-agent-test/internal/harness/claudecode"

var _ = claudecode.Adapter{}

func main() {}

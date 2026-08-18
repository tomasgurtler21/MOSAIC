// Package interceptor is a conforming fixture standing in for a use case: it
// imports domain, a pure core and an adapter, but never a frontend.
package interceptor

import (
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
	"mosaic-agent-test/internal/workspace"
)

var (
	_ = domain.VerdictPass
	_ = intercept.Decide
	_ workspace.Manager
)

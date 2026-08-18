// Package cli is a conforming fixture standing in for a frontend: it
// imports domain and a use case, never tui.
package cli

import (
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/interceptor"
)

var (
	_ = domain.VerdictPass
	_ = interceptor.Interceptor{}
)

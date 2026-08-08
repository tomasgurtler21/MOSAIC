// Package workspace is a conforming fixture standing in for an adapter: it
// imports domain and a pure core, and nothing else internal.
package workspace

import (
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

// Manager is a stand-in type using both imports.
type Manager struct {
	Verdict domain.Verdict
}

var _ = intercept.Decide

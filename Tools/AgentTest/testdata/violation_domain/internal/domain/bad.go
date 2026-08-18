// Package domain violates the domain rule by importing another package of
// this module.
package domain

import "mosaic-agent-test/internal/workspace"

var _ = workspace.Manager{}

// Package cli violates the frontend rule by importing the sibling frontend
// (tui).
package cli

import "mosaic-agent-test/internal/tui"

var _ = tui.Model{}

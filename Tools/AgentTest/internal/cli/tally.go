package cli

import (
	"fmt"

	"mosaic-agent-test/internal/runprogress"
)

// FormatTally renders the running/finished/remaining tally as one line.
// Rendered from the shared runprogress fold, so it cannot diverge from
// what the TUI shows.
func FormatTally(t runprogress.Tally) string {
	return fmt.Sprintf("Running: %d | Finished: %d | Remaining: %d", t.Running, t.Finished, t.Remaining)
}

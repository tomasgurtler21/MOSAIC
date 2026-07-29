// Package cli implements the CLI frontend for mosaic-run.
//
// The CLI frontend provides a non-interactive Interaction implementation that
// resolves questions from flags and writes per-step progress to stdout.
// It never blocks on terminal input.
package cli

import (
	"context"
	"fmt"
	"io"

	"mosaic-common/interaction"
)

// NewInteraction returns a non-interactive CLI implementation of interaction.Interaction.
//
// All question methods (SelectOne, SelectMany, AskText, Confirm) return immediately
// without blocking on terminal input. The CLI runner resolves configuration from
// flags and populates RunConfig directly, so these methods are not used to drive
// run setup.
//
// Notify writes structured notices to out. Progress writes per-step events to out.
// Neither method ever blocks.
func NewInteraction(out io.Writer) interaction.Interaction {
	return &cliInteraction{out: out}
}

// cliInteraction is the CLI implementation of interaction.Interaction.
type cliInteraction struct {
	out io.Writer
}

// SelectOne returns Answered immediately. The CLI resolves workflow selection from
// the --workflow flag rather than through the interaction port.
func (c *cliInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}

// SelectMany returns Answered immediately with no selected options.
//
// The session does not call SelectMany in CLI mode; this method exists solely for
// interface compliance. If a future session code path calls it, the caller receives
// an empty OptionIDs slice, which should be treated as a no-op or an error by the
// caller rather than a meaningful selection.
func (c *cliInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}

// AskText returns Answered immediately with an empty text value.
func (c *cliInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}

// Confirm returns Answered immediately with Confirm=false.
//
// The session does not call Confirm in CLI mode; this method exists solely for
// interface compliance. The CLI resolves destructive-action gates from flags before
// the session starts, not through the interaction port. If a future session code path
// calls Confirm for a gate that was not pre-resolved by a flag, it will silently
// decline — callers must not rely on this method to surface an interactive prompt.
func (c *cliInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil
}

// Notify writes a structured notice to the output writer. It never blocks.
//
// Format: "[<level>] <title>: <message>"
//
// Per-step completion notices from the session use this method with:
//   - Level: "info"
//   - Title: the agent instance identifier (e.g. "implementation-tdd#3")
//   - Message: structured key=value pairs for phase, stage, and status
//
// This produces one machine-readable line per completed invocation (FR-5).
func (c *cliInteraction) Notify(_ context.Context, n interaction.Notice) {
	fmt.Fprintf(c.out, "[%s] %s: %s\n", n.Level, n.Title, n.Message)
}

// Progress writes a structured progress event to the output writer. It never blocks.
//
// Format: "progress phase=<phase> current=<n> total=<n> subject=<subject>"
func (c *cliInteraction) Progress(_ context.Context, e interaction.ProgressEvent) {
	fmt.Fprintf(c.out, "progress phase=%s current=%d total=%d subject=%s\n", e.Phase, e.Current, e.Total, e.Subject)
}

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
// SelectOne, SelectMany and AskText return the Unsupported status immediately;
// Confirm returns Answered with Confirm=false. None blocks on terminal input.
// The CLI runner resolves configuration from
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

// SelectOne returns the Unsupported status immediately: a non-interactive run
// cannot ask a choice question. The CLI resolves workflow selection from the
// --workflow flag rather than through the interaction port.
func (c *cliInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Unsupported}, nil
}

// SelectMany returns the Unsupported status immediately, never an empty Answered,
// so a caller cannot mistake the refusal for a real (empty) selection.
func (c *cliInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Unsupported}, nil
}

// AskText returns the Unsupported status immediately, never an empty Answered.
func (c *cliInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Unsupported}, nil
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

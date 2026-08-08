// Package cli is the non-interactive frontend: a command surface with
// flags, a run that streams progress to standard output as it happens,
// stable exit codes an automation caller can branch on, and a
// machine-readable output mode.
//
// This package is a renderer, not a second engine. It learns everything it
// knows about a run from the typed progress-event stream (internal/domain,
// via internal/suite) and the single result model internal/report produces.
// It constructs no infrastructure of its own, performs no scheduling, and
// computes no verdicts.
package cli

// Exit codes. 0, 1 and 3 keep the meanings they carry across the other
// MOSAIC tools, so a user moving between them finds the same numbers. 2 and
// 4 are this tool's own, because no existing code carries their meanings.
//
// Two of these five codes carry the distinction this tool exists to make:
// ExitTestsFailed means the subject regressed; ExitFailure means the tool
// could not measure it. A caller that cannot tell those apart will
// eventually treat one as the other.
const (
	// ExitSuccess: every test passed.
	ExitSuccess = 0

	// ExitFailure: the tool itself could not complete the work — an
	// infrastructure fault, a suite run that returned an error, or a test
	// ended by a repeated state-integrity failure. This is never "the
	// subject misbehaved".
	ExitFailure = 1

	// ExitTestsFailed: the suite ran and some tests failed. This is the
	// subject regressing, and a caller must be able to tell it from
	// ExitFailure by code alone.
	ExitTestsFailed = 2

	// ExitUsage: the invocation was malformed — unknown flag, a flag
	// missing its value, or an unrecognised command.
	ExitUsage = 3

	// ExitPreflight: pre-flight rejected the authored files. No sandbox was
	// created and no agent was spawned.
	ExitPreflight = 4
)

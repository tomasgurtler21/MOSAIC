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

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runprogress"
)

// NewLineSink renders each progress event as one line, written as the event
// arrives rather than buffered. A suite that prints nothing for twenty
// minutes reads as a hang, and users kill hung runs — which skips teardown.
//
// Safe for concurrent use, per the domain.ProgressSink contract.
func NewLineSink(w io.Writer) domain.ProgressSink {
	return &lineSink{w: w}
}

type lineSink struct {
	mu    sync.Mutex
	w     io.Writer
	model runprogress.Model
}

// Emit writes ev's rendered line, followed — for a ProgressTestFinished
// event carrying failed assertions — by one indented line per failed
// assertion, so a failure's reasons are visible in the stream without
// waiting for the final report.
//
// After each lifecycle event (ProgressTestStarted, ProgressTestFinished) a
// tally line is also written, so the running/finished/remaining counts are
// visible in the live stream without waiting for the final report.
func (s *lineSink) Emit(ev domain.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.model = s.model.Fold(ev)
	fmt.Fprintln(s.w, FormatEvent(ev))
	if ev.Kind == domain.ProgressTestFinished {
		for _, fa := range ev.FailedAssertions {
			fmt.Fprintln(s.w, "  - "+fa)
		}
	}
	if ev.Kind == domain.ProgressTestStarted || ev.Kind == domain.ProgressTestFinished {
		fmt.Fprintln(s.w, FormatTally(s.model.Tally()))
	}
}

// FormatEvent is the pure line rendering, without a trailing newline.
// Exported and pure so a run's whole line output can be produced from a
// scripted event sequence with no suite executing at all — which is what
// makes "the frontend is driven purely by the stream" a tested claim.
//
// Grammar (fixed by design, see Frontend Models in ContractsDesign.md):
//
// Per-run events carry a run-attribution prefix in square brackets, followed
// by the per-kind grammar. Suite-level events carry no such prefix, because
// they belong to no single run.
//
//	ProgressSuiteStarted  -> "suite <suite-id> started: <n> tests"
//	ProgressTestStarted   -> "[<runID>] test <test-id> [<rep>/<reps>] started"
//	ProgressInvocation    -> "[<runID>]   #<seq> <identity-key> -> <outcome>"
//	ProgressTestFinished  -> "[<runID>] test <test-id> [<rep>/<reps>] <verdict> (<duration>, <cost>)"
//	ProgressSuiteFinished -> "suite finished: <counts> (<total cost>)"
//
// The prefix is what makes lines from interleaved runs individually readable
// under concurrency. A cost whose attribution is not domain.AttributionAttributed
// renders as its attribution word, never as a currency amount.
func FormatEvent(ev domain.ProgressEvent) string {
	// runPrefix returns the attribution prefix for a per-run event whose
	// run identity is non-empty, or the empty string when none is set.
	// Suite-level events always carry an empty Run, so they receive no prefix.
	runPrefix := func(runID string) string {
		if runID == "" {
			return ""
		}
		return "[" + runID + "] "
	}

	switch ev.Kind {
	case domain.ProgressSuiteStarted:
		return fmt.Sprintf("suite %s started: %d tests", ev.SuiteID, ev.TotalTests)
	case domain.ProgressTestStarted:
		return fmt.Sprintf("%stest %s [%d/%d] started", runPrefix(ev.Run.RunID), ev.TestID, ev.Repetition, ev.Repetitions)
	case domain.ProgressInvocation:
		return fmt.Sprintf("%s  #%d %s -> %s", runPrefix(ev.Run.RunID), ev.Seq, ev.Identity.Key(), ev.Outcome)
	case domain.ProgressTestFinished:
		return fmt.Sprintf("%stest %s [%d/%d] %s (%s, %s)", runPrefix(ev.Run.RunID), ev.TestID, ev.Repetition, ev.Repetitions, ev.Verdict, ev.Duration, formatCost(ev.Cost))
	case domain.ProgressSuiteFinished:
		return fmt.Sprintf("suite finished: %s (%s)", formatCounts(ev.Counts), formatCost(ev.TotalCost))
	default:
		return ""
	}
}

// formatCost renders a cost as a dollar amount when its attribution is
// trustworthy, and as the attribution word otherwise — never a currency
// figure that would read as a misleadingly genuine zero.
func formatCost(c domain.CostReport) string {
	if c.Attribution != domain.AttributionAttributed {
		return string(c.Attribution)
	}
	return fmt.Sprintf("$%.2f", c.TotalUSD)
}

// formatCounts renders a verdict-count map in a stable, alphabetically
// sorted order, so the same counts always render the same line.
func formatCounts(counts map[domain.Verdict]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[domain.Verdict(k)]))
	}
	return strings.Join(parts, " ")
}

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
	mu sync.Mutex
	w  io.Writer
}

// Emit writes ev's rendered line, followed — for a ProgressTestFinished
// event carrying failed assertions — by one indented line per failed
// assertion, so a failure's reasons are visible in the stream without
// waiting for the final report.
func (s *lineSink) Emit(ev domain.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Fprintln(s.w, FormatEvent(ev))
	if ev.Kind == domain.ProgressTestFinished {
		for _, fa := range ev.FailedAssertions {
			fmt.Fprintln(s.w, "  - "+fa)
		}
	}
}

// FormatEvent is the pure line rendering, without a trailing newline.
// Exported and pure so a run's whole line output can be produced from a
// scripted event sequence with no suite executing at all — which is what
// makes "the frontend is driven purely by the stream" a tested claim.
//
// Grammar (fixed by design, see Frontend Models in ContractsDesign.md):
//
//	ProgressSuiteStarted  -> "suite <suite-id> started: <n> tests"
//	ProgressTestStarted   -> "test <test-id> [<rep>/<reps>] started"
//	ProgressInvocation    -> "  #<seq> <identity-key> -> <outcome>"
//	ProgressTestFinished  -> "test <test-id> [<rep>/<reps>] <verdict> (<duration>, <cost>)"
//	ProgressSuiteFinished -> "suite finished: <counts> (<total cost>)"
//
// A cost whose attribution is not domain.AttributionAttributed renders as
// its attribution word, never as a currency amount.
func FormatEvent(ev domain.ProgressEvent) string {
	switch ev.Kind {
	case domain.ProgressSuiteStarted:
		return fmt.Sprintf("suite %s started: %d tests", ev.SuiteID, ev.TotalTests)
	case domain.ProgressTestStarted:
		return fmt.Sprintf("test %s [%d/%d] started", ev.TestID, ev.Repetition, ev.Repetitions)
	case domain.ProgressInvocation:
		return fmt.Sprintf("  #%d %s -> %s", ev.Seq, ev.Identity.Key(), ev.Outcome)
	case domain.ProgressTestFinished:
		return fmt.Sprintf("test %s [%d/%d] %s (%s, %s)", ev.TestID, ev.Repetition, ev.Repetitions, ev.Verdict, ev.Duration, formatCost(ev.Cost))
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

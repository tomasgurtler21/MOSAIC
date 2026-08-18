// Package concurrency reconstructs, per declared parallel group, the peak
// number of collaborator invocations simultaneously in flight during a run.
//
// It replays the invocation log's start and end records as half-open
// intervals [start, end) rather than trusting a counter maintained at
// interception time — that counter is the first-generation defect this
// package exists to fix. Pure — no I/O.
package concurrency

import (
	"sort"
	"time"

	"mosaic-agent-test/internal/domain"
)

// interval is one completed collaborator invocation, reduced to the span it
// occupied.
type interval struct {
	seq   int
	group string
	start time.Time
	end   time.Time
}

// Peaks reconstructs, per declared parallel group, the maximum number of
// collaborator invocations simultaneously in flight during the run.
//
// It replays the log's start and end records as half-open intervals
// [start, end): intervals that merely abut do not count as concurrent.
// Invocations outside a group never contribute to that group's peak.
//
// An invocation with a start record and no end record (crashed or timed
// out) is excluded from the overlap computation and reported in Report. It
// is never silently folded in, because doing so would either inflate the
// peak or hide a broken run.
//
// End records carry no Group of their own (see internal/intercept); each is
// correlated to its declared group through the start record sharing its
// CorrelationToken.
func Peaks(records []domain.LogRecord, groups []domain.ParallelGroup) (map[string]int, Report) {
	declared := make(map[string]bool, len(groups))
	for _, g := range groups {
		declared[g.Name] = true
	}

	type openInvocation struct {
		seq   int
		group string
		start time.Time
	}
	open := map[string]*openInvocation{}
	var completed []interval
	report := Report{}

	for _, rec := range records {
		switch rec.Kind {
		case domain.RecordStart:
			if !declared[rec.Group] {
				report.Ungrouped++
				continue
			}
			open[rec.CorrelationToken] = &openInvocation{
				seq:   rec.Seq,
				group: rec.Group,
				start: rec.Timestamp,
			}
		case domain.RecordEnd:
			ov, ok := open[rec.CorrelationToken]
			if !ok {
				// No matching declared-group start: an ungrouped invocation's
				// end record, or an end with no corresponding start at all.
				continue
			}
			completed = append(completed, interval{
				seq:   ov.seq,
				group: ov.group,
				start: ov.start,
				end:   rec.Timestamp,
			})
			delete(open, rec.CorrelationToken)
		default:
			// Run-level and error records are not interval endpoints.
			continue
		}
	}

	for _, ov := range open {
		report.UnterminatedSeqs = append(report.UnterminatedSeqs, ov.seq)
	}
	sort.Ints(report.UnterminatedSeqs)

	byGroup := map[string][]interval{}
	for _, iv := range completed {
		byGroup[iv.group] = append(byGroup[iv.group], iv)
	}

	peaks := map[string]int{}
	for group, ivs := range byGroup {
		peaks[group] = peakOverlap(ivs)
	}

	return peaks, report
}

// peakOverlap computes the maximum number of half-open intervals
// simultaneously active, via a sweep over start/end points. Intervals that
// merely abut (one ends exactly when another starts) are not concurrent:
// end points are processed before start points at the same instant.
func peakOverlap(ivs []interval) int {
	type point struct {
		t     time.Time
		delta int
	}
	points := make([]point, 0, len(ivs)*2)
	for _, iv := range ivs {
		points = append(points, point{t: iv.start, delta: 1})
		points = append(points, point{t: iv.end, delta: -1})
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].t.Equal(points[j].t) {
			return points[i].delta < points[j].delta // ends (-1) before starts (+1)
		}
		return points[i].t.Before(points[j].t)
	})

	current, peak := 0, 0
	for _, p := range points {
		current += p.delta
		if current > peak {
			peak = current
		}
	}
	return peak
}

// Report carries the exceptional conditions found while reconstructing
// peaks, so they can be surfaced rather than silently absorbed into the
// numbers.
type Report struct {
	// UnterminatedSeqs lists the global sequence numbers of invocations
	// whose end record is missing.
	UnterminatedSeqs []int
	// Ungrouped counts start records that matched no declared group.
	Ungrouped int
}

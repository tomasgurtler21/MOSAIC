package app

import (
	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// mergeEligible reports whether the unknown-run bucket's streams may be
// safely re-attributed to the single named run. This is true only when:
//   - The inventory contains exactly one named run (single-run-isolated source).
//   - The inventory has an unattributable bucket with at least one event file.
//
// When the source is a multi-run tree, the unknown-run bucket may contain
// records belonging to ANY of the runs. Re-attributing all of them to
// whichever run is currently queried would misattribute other runs' costs.
// The merge is therefore skipped entirely for multi-run sources.
func mergeEligible(inv domain.Inventory) bool {
	if len(inv.Runs) != 1 {
		return false
	}
	if inv.Unattributable == nil {
		return false
	}
	unattr := inv.Unattributable
	return unattr.OrchestratorFile != "" || len(unattr.Agents) > 0
}

// reattributeStreams re-labels every stream whose Ref.Run is unattributable
// so that it carries targetRun instead. The returned slice contains ALL
// streams (named-run streams unchanged, unknown-run streams re-labelled).
// The original slice is not mutated; a new slice with copied StreamRefs is
// returned.
//
// This is the merge mechanism: once re-labelled, the aggregator treats the
// formerly-unattributable events as belonging to the named run. Deduplication
// by record_id happens automatically via the aggregator's existing
// recordOwners/recordIndex mechanism.
func reattributeStreams(streams []analysis.Stream, targetRun domain.RunRef) []analysis.Stream {
	result := make([]analysis.Stream, len(streams))
	for i, s := range streams {
		if s.Ref.Run.IsUnattributable() {
			newRef := s.Ref
			newRef.Run = targetRun
			result[i] = analysis.Stream{
				Ref:    newRef,
				Events: s.Events,
			}
		} else {
			result[i] = s
		}
	}
	return result
}

// countUnknownRunRecords returns the number of usage_record events in streams
// that carry an unattributable run reference. These are the streams that will
// be re-attributed to the named run during the merge.
func countUnknownRunRecords(streams []analysis.Stream) int {
	count := 0
	for _, s := range streams {
		if !s.Ref.Run.IsUnattributable() {
			continue
		}
		for _, ev := range s.Events {
			if ev.Type == domain.EventUsageRecord {
				count++
			}
		}
	}
	return count
}

// countCrossStreamDups returns the number of FindingCrossStreamRecord findings
// in findings that are attributed to the given target run. Each such finding
// represents one record_id from the unknown-run bucket that collided with a
// record already owned by a different actor in the named run and was rejected.
func countCrossStreamDups(findings []domain.Finding, targetRun domain.RunRef) int {
	count := 0
	for _, f := range findings {
		if f.Kind == domain.FindingCrossStreamRecord && f.Run == targetRun {
			count++
		}
	}
	return count
}

// mergeMetadata computes the UnknownRunMerged and UnknownRunResidual counts
// after a pre-aggregation stream re-attribution.
//
// merged = totalUnknownRecords - crossStreamDups
//   (records successfully absorbed into the named run's totals)
// residual = 0
//   (in a single-run-isolated source all unknown-run records belong to this
//   run by construction; the field exists for future multi-run extension)
func mergeMetadata(totalUnknownRecords int, crossStreamDups int) (merged int, residual int) {
	return totalUnknownRecords - crossStreamDups, 0
}

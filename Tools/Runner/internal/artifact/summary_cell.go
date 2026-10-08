package artifact

import "mosaic-run/internal/domain"

// renderSummaryCell returns the Summary cell of a log entry. A BLOCKED row
// ends with its error code marker, appended after truncation so the marker is
// always complete; other rows carry the summary unchanged.
func renderSummaryCell(e domain.ExecutionLogEntry) string {
	if e.Status != domain.StatusBLOCKED {
		return e.Summary
	}
	return domain.AppendErrorMarker(e.Summary, e.ErrorCode)
}

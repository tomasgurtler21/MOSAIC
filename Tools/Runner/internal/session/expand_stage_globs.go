package session

import (
	"fmt"
	"strings"

	"mosaic-run/internal/domain"
)

// expandStageGlobs expands Stage-* wildcard patterns in output-artifact paths
// to concrete per-stage paths, mirroring the strings.ReplaceAll substitution
// loop from the engine's resolveArtifacts function for input artifacts.
//
// Unlike resolveArtifacts, expandStageGlobs intentionally does NOT return an
// error when stages is nil or empty -- it returns paths unchanged (safe
// fallback for non-staged workflows). The resolveArtifacts error-on-nil-StageSet
// behavior does not apply here because output-artifact glob expansion is a
// best-effort pre-processing step, not a dispatch-blocking validation.
//
// Pure string substitution -- no filesystem access. When stages is nil or has
// zero entries, paths containing Stage-* are returned unchanged (safe fallback
// for non-staged workflows; the zero-match non-compliance logic is handled by
// the caller, not by this function).
//
// Non-glob paths pass through unchanged. Multiple Stage-* occurrences in a
// single path (e.g. "Stage-*/Sub/Stage-*/Out.md") are all replaced in each
// expansion -- consistent with strings.ReplaceAll as used by the engine.
func expandStageGlobs(artifacts []string, stages *domain.StageSet) []string {
	if len(artifacts) == 0 {
		return artifacts // preserve nil vs empty slice distinction
	}
	if stages == nil || len(stages.Entries) == 0 {
		return artifacts // passthrough unchanged; safe fallback for non-staged workflows
	}

	result := make([]string, 0, len(artifacts))
	for _, art := range artifacts {
		if !strings.Contains(art, "Stage-*") {
			result = append(result, art)
		} else {
			for _, entry := range stages.Entries {
				result = append(result, strings.ReplaceAll(art, "Stage-*", fmt.Sprintf("Stage-%d", entry.Number)))
			}
		}
	}
	return result
}

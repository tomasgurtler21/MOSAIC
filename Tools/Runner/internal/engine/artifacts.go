package engine

import (
	"fmt"
	"strings"

	"mosaic-run/internal/domain"
)

// ResolveArtifacts expands template variables in artifact paths.
// For input artifacts (isInput=true): {StageNumber} is substituted and
// Stage-* is expanded per stage.
// For output artifacts (isInput=false): {StageNumber} is substituted and
// Stage-* is passed through unexpanded.
//
// This is the existing resolveArtifacts function, exported by rename only.
// No signature or logic change.
func ResolveArtifacts(
	arts []string,
	stageNum domain.StageNumber,
	stageStr string,
	stages *domain.StageSet,
	refreshedStages *domain.StageSet,
	isInput bool,
) ([]string, error) {

	isExecution := stageStr != "" && stageNum > 0

	// Effective stage set for Stage-* expansion in input artifacts.
	effectiveStages := stages
	if !isExecution && refreshedStages != nil {
		effectiveStages = refreshedStages
	}

	var result []string
	for _, art := range arts {
		// Substitute {StageNumber}.
		if strings.Contains(art, "{StageNumber}") {
			if !isExecution {
				return nil, fmt.Errorf(
					"unresolvable {StageNumber} in artifact path %q: no stage context at this row", art)
			}
			art = strings.ReplaceAll(art, "{StageNumber}", fmt.Sprintf("%d", stageNum))
		}

		// Handle Stage-* wildcard.
		if strings.Contains(art, "Stage-*") {
			if !isInput {
				// Output artifacts: pass through unexpanded.
				result = append(result, art)
				continue
			}
			// Input artifacts: expand to one path per stage.
			if effectiveStages == nil {
				return nil, fmt.Errorf(
					"Stage-* in artifact %q requires a stage set but none is available", art)
			}
			for _, entry := range effectiveStages.Entries {
				expanded := strings.ReplaceAll(art, "Stage-*", fmt.Sprintf("Stage-%d", entry.Number))
				result = append(result, expanded)
			}
			continue
		}

		result = append(result, art)
	}
	return result, nil
}

// injectReviewArtifacts appends entries from lastOutputArtifacts to tableArts,
// skipping any path already present once run-prefixed and unprefixed forms are
// treated as equal. The result is the table row's entries in their original
// order, followed by the unique new entries in the order they appear in
// lastOutputArtifacts.
func injectReviewArtifacts(runID string, tableArts, lastOutputArtifacts []string) []string {
	if len(lastOutputArtifacts) == 0 {
		return tableArts
	}
	merged := make([]string, 0, len(tableArts)+len(lastOutputArtifacts))
	merged = append(merged, tableArts...)
	merged = append(merged, lastOutputArtifacts...)
	return domain.DedupArtifactPaths(runID, merged)
}

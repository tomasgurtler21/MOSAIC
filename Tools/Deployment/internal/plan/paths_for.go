package plan

import "mosaic-deploy/internal/domain"

// PathsFor returns all target paths for the given ref, in enumeration order.
// Returns nil when no PlannedPath entries match the ref.
//
// This is the multi-entry analogue of Path(), which returns only the first match.
// Build uses PathsFor to correlate skill.Files with their resolved target paths:
// the i-th element of PathsFor(ref) corresponds to the i-th element of skill.Files
// because EnumerateTargetPaths iterates skill.Files in order.
//
func (p PlannedPaths) PathsFor(ref domain.ArtifactRef) []string {
	var result []string
	for _, pp := range p {
		if pp.Ref == ref {
			result = append(result, pp.TargetPath)
		}
	}
	return result
}

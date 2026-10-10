package domain

import "strings"

// StripRunPrefix returns p without a leading "Orchestration-{runID}/" (the
// same rule as the artifact registry key). Backslashes in p are first
// converted to forward slashes. runID == "" returns p with only the slash
// conversion applied.
func StripRunPrefix(runID, p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if runID == "" {
		return p
	}
	return strings.TrimPrefix(p, RunScopedFolder(runID)+"/")
}

// DedupArtifactPaths returns paths in order with every later entry removed
// whose StripRunPrefix(runID, ...) form equals that of an earlier entry. The
// first occurrence is kept in its original form. Nil in, nil out.
func DedupArtifactPaths(runID string, paths []string) []string {
	if paths == nil {
		return nil
	}
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		key := StripRunPrefix(runID, p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// RecordedArtifactPath returns the form of p written to the Execution Log
// Inputs column and the Artifacts registry. When p, with backslashes read as
// forward slashes, starts with "Orchestration-{runID}/", it returns the
// remainder with forward slashes (e.g. "Stage-2/Plan.md"). Otherwise (a
// project file, or runID == "") it returns p unchanged, byte for byte.
func RecordedArtifactPath(runID, p string) string {
	if runID == "" {
		return p
	}
	slashed := strings.ReplaceAll(p, `\`, "/")
	prefix := RunScopedFolder(runID) + "/"
	if !strings.HasPrefix(slashed, prefix) {
		return p
	}
	return strings.TrimPrefix(slashed, prefix)
}
